package com.termx.app.transport

import android.content.Context
import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.util.HttpHelper
import org.json.JSONArray
import org.json.JSONObject
import org.webrtc.*
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.*
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * WebRTCTransport — termx PeerConnection 生命周期
 *
 * 信令协议：
 *   GET  /api/v1/sessions/ice     → ICE 服务器列表
 *   POST /api/v1/sessions         → 发 offer，拿 answer（或 pending sessionId）
 *   POST /api/v1/sessions/{id}/answer → poll answer
 *
 * 认证：sessionToken 在请求体中，Bearer token 形式在 Authorization header。
 */
class WebRTCTransport(
    private val bridge: BridgeServer?,
    val machineId: String,
) {
    companion object {
        private const val TAG = "TermxWebRTC"

        private const val ICE_GATHER_TIMEOUT_HUB = 8000
        private const val ICE_GATHER_TIMEOUT_LOCAL = 3000
        private const val CONNECT_TIMEOUT = 15000
        private const val DATA_CHANNEL_OPEN_TIMEOUT = 10000
        private const val SIGNALING_TIMEOUT = 15000
        private const val ANSWER_POLL_MAX = 30
        private const val ANSWER_POLL_DELAY_MS = 2000L
        private const val RESUME_STALE_THRESHOLD_MS = 7000L

        private var factory: PeerConnectionFactory? = null
        private var factoryInitialized = false

        @Synchronized
        fun initFactory(context: Context) {
            if (factoryInitialized) return
            PeerConnectionFactory.initialize(
                PeerConnectionFactory.InitializationOptions.builder(context.applicationContext)
                    .createInitializationOptions()
            )
            factory = PeerConnectionFactory.builder().createPeerConnectionFactory()
            factoryInitialized = true
            Log.i(TAG, "PeerConnectionFactory initialized")
        }
    }

    private var pc: PeerConnection? = null
    var isConnected = false
        private set
    private var connectedAt = 0L
    @Volatile private var disconnectedAt = 0L
    var lastRtt = 0L
    @Volatile private var relayInUse = false
    private val generation = AtomicLong(0)

    val channelManager = ChannelManager(bridge, machineId)
    private val heartbeat = Heartbeat(this) { triggerDisconnect() }

    var onDisconnectListener: (() -> Unit)? = null
    var onChannelResetListener: (() -> Unit)? = null

    var lastFailureReason: String? = null
        private set

    // ICE gathering
    private val iceGatherLock = Object()
    @Volatile private var hasHostOrSrflx = false
    @Volatile private var hasRelay = false
    @Volatile private var iceGatheringComplete = false

    // ─── Connect ─────────────────────────────────────────────────────────────

    /** Hub 连接（local or public_p2p，都走同一套信令协议，只是 hubUrl 不同） */
    fun connectHub(
        hubUrl: String,
        sessionToken: String,
        iceServers: JSONArray?,
        forceRelay: Boolean = false,
        onProgress: ((String) -> Unit)? = null,
    ): Boolean {
        lastFailureReason = null
        relayInUse = false
        return try {
            val f = factory ?: run { markFailure("init"); return false }
            val servers = parseIceServers(iceServers)
            val config = PeerConnection.RTCConfiguration(servers).apply {
                sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
                if (forceRelay) {
                    iceTransportsType = PeerConnection.IceTransportsType.RELAY
                }
            }
            pc = f.createPeerConnection(config, createObserver()) ?: run {
                markFailure("init"); return false
            }
            channelManager.createInitialChannels(pc!!)

            val offer = createOffer() ?: run { markFailure("offer"); disconnect(); return false }
            if (!setLocalDesc(offer)) { markFailure("offer"); disconnect(); return false }

            onProgress?.invoke("Gathering ICE candidates...")
            val waitRelay = forceRelay || (iceServers != null && iceServers.length() > 0)
            waitForICEGathering(if (waitRelay) ICE_GATHER_TIMEOUT_HUB else ICE_GATHER_TIMEOUT_LOCAL, waitRelay)

            val localSdp = pc!!.localDescription.description
            onProgress?.invoke("Exchanging signals with hub...")

            val sessionId = UUID.randomUUID().toString()
            val body = JSONObject()
                .put("machine_id", machineId)
                .put("session_token", sessionToken)
                .put("offer", JSONObject()
                    .put("session_id", sessionId)
                    .put("sdp", localSdp)
                    .put("ice_candidates", JSONArray()))
                .toString()

            val headers = mapOf(
                "Content-Type" to "application/json",
                "Authorization" to "Bearer $sessionToken",
            )

            val resp = HttpHelper.post("$hubUrl/api/v1/sessions", headers, body, SIGNALING_TIMEOUT)
            Log.i(TAG, "createSession response: ${resp.statusCode}")
            if (!resp.isOk) {
                markFailure(if (resp.statusCode == 401) "auth" else "signal")
                disconnect()
                return false
            }

            val answer = resolveAnswer(hubUrl, sessionToken, JSONObject(resp.bodyString()), onProgress)
                ?: return false

            processAnswer(answer)
        } catch (e: Exception) {
            if (lastFailureReason == null) markFailure("unknown")
            Log.e(TAG, "connectHub failed", e)
            disconnect()
            false
        }
    }

    // ─── Private helpers ─────────────────────────────────────────────────────

    private fun resolveAnswer(
        hubUrl: String,
        sessionToken: String,
        response: JSONObject,
        onProgress: ((String) -> Unit)?,
    ): JSONObject? {
        if (response.optBoolean("pending", false)) {
            val sessionId = response.optString("session_id")
            onProgress?.invoke("Waiting for machine to respond...")
            return pollAnswer(hubUrl, sessionToken, sessionId, onProgress)
        }
        return response
    }

    private fun pollAnswer(
        hubUrl: String,
        sessionToken: String,
        sessionId: String,
        onProgress: ((String) -> Unit)?,
    ): JSONObject? {
        val headers = mapOf(
            "Content-Type" to "application/json",
            "Authorization" to "Bearer $sessionToken",
        )
        val body = JSONObject().put("machine_id", machineId).toString()

        for (attempt in 1..ANSWER_POLL_MAX) {
            Thread.sleep(ANSWER_POLL_DELAY_MS)
            try {
                val resp = HttpHelper.post(
                    "$hubUrl/api/v1/sessions/${sessionId}/answer",
                    headers, body, SIGNALING_TIMEOUT,
                )
                if (resp.statusCode == 202) continue // still pending
                if (!resp.isOk) {
                    markFailure(if (resp.statusCode == 401) "auth" else "signal")
                    disconnect()
                    return null
                }
                return JSONObject(resp.bodyString())
            } catch (e: Exception) {
                Log.w(TAG, "pollAnswer attempt $attempt failed: ${e.message}")
            }
        }
        markFailure("timeout")
        disconnect()
        return null
    }

    private fun processAnswer(answer: JSONObject): Boolean {
        val sdpObj = answer.optJSONObject("answer")
        val sdp = sdpObj?.optString("sdp") ?: answer.optString("sdp")
        if (sdp.isNullOrBlank()) {
            markFailure("signal")
            disconnect()
            return false
        }

        val remoteCandidates = answer.optJSONArray("ice_candidates")
        val remoteSDP = SessionDescription(SessionDescription.Type.ANSWER, sdp)

        val latch = CountDownLatch(1)
        val ok = AtomicBoolean(true)
        pc!!.setRemoteDescription(object : SdpObserver {
            override fun onCreateSuccess(p0: SessionDescription?) {}
            override fun onCreateFailure(p0: String?) {}
            override fun onSetSuccess() { latch.countDown() }
            override fun onSetFailure(err: String?) {
                ok.set(false)
                Log.e(TAG, "setRemoteDescription failed: $err")
                latch.countDown()
            }
        }, remoteSDP)

        if (!latch.await(5000, TimeUnit.MILLISECONDS) || !ok.get()) {
            markFailure("signal")
            disconnect()
            return false
        }

        // Add any extra ICE candidates from answer
        remoteCandidates?.let { arr ->
            for (i in 0 until arr.length()) {
                val candidateSdp = arr.optString(i) ?: continue
                if (candidateSdp.isBlank()) continue
                pc!!.addIceCandidate(IceCandidate("", 0, candidateSdp))
            }
        }

        // Wait for connection
        val connected = waitForConnection(CONNECT_TIMEOUT)
        if (!connected) {
            markFailure("timeout")
            disconnect()
            return false
        }

        // Wait for api channel to open
        val apiOpen = channelManager.waitApiOpen(DATA_CHANNEL_OPEN_TIMEOUT)
        if (!apiOpen) {
            markFailure("timeout")
            disconnect()
            return false
        }

        isConnected = true
        connectedAt = System.currentTimeMillis()
        relayInUse = getConnectionInfo().optString("type") == "relay"
        heartbeat.start()
        Log.i(TAG, "Connected to $machineId")
        return true
    }

    private fun waitForConnection(timeoutMs: Int): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            val state = pc?.connectionState()
            when (state) {
                PeerConnection.PeerConnectionState.CONNECTED -> return true
                PeerConnection.PeerConnectionState.FAILED,
                PeerConnection.PeerConnectionState.CLOSED -> return false
                else -> Thread.sleep(100)
            }
        }
        return false
    }

    private fun waitForICEGathering(timeoutMs: Int, waitRelay: Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        synchronized(iceGatherLock) {
            while (System.currentTimeMillis() < deadline) {
                if (iceGatheringComplete) break
                if (hasHostOrSrflx && !waitRelay) {
                    Thread.sleep(500); break
                }
                if (hasHostOrSrflx && hasRelay) {
                    Thread.sleep(500); break
                }
                iceGatherLock.wait(200)
            }
        }
    }

    private fun createOffer(): SessionDescription? {
        var result: SessionDescription? = null
        val latch = CountDownLatch(1)
        pc!!.createOffer(object : SdpObserver {
            override fun onCreateSuccess(sdp: SessionDescription) { result = sdp; latch.countDown() }
            override fun onCreateFailure(err: String?) { latch.countDown() }
            override fun onSetSuccess() {}
            override fun onSetFailure(p0: String?) {}
        }, MediaConstraints())
        latch.await(5000, TimeUnit.MILLISECONDS)
        return result
    }

    private fun setLocalDesc(sdp: SessionDescription): Boolean {
        val latch = CountDownLatch(1)
        val ok = AtomicBoolean(false)
        pc!!.setLocalDescription(object : SdpObserver {
            override fun onCreateSuccess(p0: SessionDescription?) {}
            override fun onCreateFailure(p0: String?) {}
            override fun onSetSuccess() { ok.set(true); latch.countDown() }
            override fun onSetFailure(err: String?) {
                Log.e(TAG, "setLocalDesc failed: $err"); latch.countDown()
            }
        }, sdp)
        latch.await(5000, TimeUnit.MILLISECONDS)
        return ok.get()
    }

    private fun createObserver() = object : PeerConnection.Observer {
        override fun onIceCandidate(candidate: IceCandidate) {
            val type = candidate.sdp.lowercase()
            synchronized(iceGatherLock) {
                when {
                    type.contains("typ relay") -> { hasRelay = true; iceGatherLock.notifyAll() }
                    type.contains("typ host") || type.contains("typ srflx") -> {
                        hasHostOrSrflx = true; iceGatherLock.notifyAll()
                    }
                }
            }
        }
        override fun onIceGatheringChange(state: PeerConnection.IceGatheringState) {
            if (state == PeerConnection.IceGatheringState.COMPLETE) {
                synchronized(iceGatherLock) { iceGatheringComplete = true; iceGatherLock.notifyAll() }
            }
        }
        override fun onConnectionChange(state: PeerConnection.PeerConnectionState) {
            Log.i(TAG, "connectionState: $state [$machineId]")
            when (state) {
                PeerConnection.PeerConnectionState.CONNECTED -> {
                    disconnectedAt = 0L
                    heartbeat.onConnectionStateConnected()
                }
                PeerConnection.PeerConnectionState.DISCONNECTED -> {
                    disconnectedAt = System.currentTimeMillis()
                    if (isConnected) heartbeat.onConnectionStateDisconnected(connectedAt)
                }
                PeerConnection.PeerConnectionState.FAILED -> {
                    disconnectedAt = System.currentTimeMillis()
                    if (isConnected) heartbeat.onConnectionStateFailed()
                }
                PeerConnection.PeerConnectionState.CLOSED -> {
                    disconnectedAt = System.currentTimeMillis()
                }
                else -> {}
            }
        }
        override fun onDataChannel(dc: DataChannel) {
            Log.i(TAG, "onDataChannel: ${dc.label()} [$machineId]")
        }
        override fun onIceConnectionChange(p0: PeerConnection.IceConnectionState?) {}
        override fun onIceConnectionReceivingChange(p0: Boolean) {}
        override fun onIceCandidatesRemoved(p0: Array<out IceCandidate>?) {}
        override fun onSignalingChange(p0: PeerConnection.SignalingState?) {}
        override fun onAddStream(p0: MediaStream?) {}
        override fun onRemoveStream(p0: MediaStream?) {}
        override fun onRenegotiationNeeded() {}
        override fun onAddTrack(p0: RtpReceiver?, p1: Array<out MediaStream>?) {}
        override fun onSelectedCandidatePairChanged(event: CandidatePairChangeEvent?) {}
    }

    private fun parseIceServers(arr: JSONArray?): List<PeerConnection.IceServer> {
        if (arr == null) return emptyList()
        val result = mutableListOf<PeerConnection.IceServer>()
        for (i in 0 until arr.length()) {
            val obj = arr.optJSONObject(i) ?: continue
            val urlsArr = obj.optJSONArray("urls")
            val urls = if (urlsArr != null) {
                (0 until urlsArr.length()).mapNotNull { urlsArr.optString(it) }
            } else {
                listOfNotNull(obj.optString("url").takeIf { it.isNotBlank() })
            }
            if (urls.isEmpty()) continue
            val username = obj.optString("username").takeIf { it.isNotBlank() }
            val credential = obj.optString("credential").takeIf { it.isNotBlank() }
            val builder = PeerConnection.IceServer.builder(urls)
            if (username != null) builder.setUsername(username)
            if (credential != null) builder.setPassword(credential)
            result.add(builder.createIceServer())
        }
        return result
    }

    // ─── Public API ──────────────────────────────────────────────────────────

    fun sendToApi(data: ByteArray) = channelManager.sendRawApi(data)

    fun sendToEvents(data: ByteArray) = channelManager.sendRawEvents(data)

    fun sendToTerminal(terminalId: String, data: ByteArray) =
        channelManager.sendTerminalData(terminalId, data)

    fun sendToFile(transferId: String, data: ByteArray) =
        channelManager.sendFileData(transferId, data)

    fun openTerminalChannel(terminalId: String) =
        channelManager.getOrCreateTerminal(pc, terminalId, isConnected)

    fun openFileChannel(transferId: String) =
        channelManager.getOrCreateFile(pc, transferId, isConnected)

    /** Blocking runtime API request for heartbeat/verification. Throws on timeout or closed channel. */
    fun sendApiRequest(method: String, path: String, body: String?, timeoutMs: Long): String {
        return channelManager.sendApiRequest(method, path, body, timeoutMs)
    }

    fun isPeerConnected(): Boolean =
        pc?.connectionState() == PeerConnection.PeerConnectionState.CONNECTED

    fun hasPeerConnection(): Boolean = pc != null

    fun isApiChannelOpen(): Boolean = channelManager.isApiOpen()

    fun handleAppResume(): Boolean {
        return heartbeat.handleAppResume()
    }

    fun isStaleAfterResume(backgroundDurationMs: Long): Boolean {
        if (!isConnected || !hasPeerConnection()) return true
        if (!isPeerConnected()) return true
        if (!isApiChannelOpen()) return true
        val quietForMs = heartbeat.millisSinceLastSuccess()
        val disconnectedForMs = if (disconnectedAt > 0L) {
            (System.currentTimeMillis() - disconnectedAt).coerceAtLeast(0L)
        } else {
            0L
        }
        return backgroundDurationMs >= RESUME_STALE_THRESHOLD_MS ||
            quietForMs >= RESUME_STALE_THRESHOLD_MS ||
            disconnectedForMs >= RESUME_STALE_THRESHOLD_MS
    }

    fun generation(): Long = generation.get()

    fun verifyStatus(timeoutMs: Long): Long {
        val start = System.currentTimeMillis()
        sendApiRequest("GET", "/status", null, timeoutMs)
        val elapsed = (System.currentTimeMillis() - start).coerceAtLeast(1L)
        lastRtt = elapsed
        return elapsed
    }

    fun onNetworkTypeChanged() {
        if (!isConnected) return
        heartbeat.onNetworkTypeChanged()
    }

    fun hasActiveFileTransfers(): Boolean =
        channelManager.fileTransferManager?.hasActiveTransfers() == true

    fun hasRecentFileTransferActivity(windowMs: Long): Boolean =
        channelManager.fileTransferManager?.hasRecentTransferActivity(windowMs) == true

    fun currentRelayInUse(): Boolean = relayInUse

    fun getConnectionInfo(): JSONObject {
        val peer = pc
        if (peer == null || !isConnected) return JSONObject().put("type", "unknown").put("relayInUse", relayInUse)

        val latch = CountDownLatch(1)
        val result = JSONObject()
        peer.getStats { reports ->
            try {
                val statsMap = reports.statsMap
                var activePairId: String? = null
                for (report in statsMap.values) {
                    if (report.type == "transport") {
                        activePairId = report.members["selectedCandidatePairId"]?.toString()
                        if (!activePairId.isNullOrBlank()) break
                    }
                }
                val pair = activePairId?.let { statsMap[it] }
                    ?: statsMap.values.firstOrNull { it.type == "candidate-pair" && it.members["nominated"] == true }
                    ?: statsMap.values.firstOrNull { it.type == "candidate-pair" && it.members["state"]?.toString() == "succeeded" }

                if (pair != null) {
                    (pair.members["currentRoundTripTime"] as? Number)?.let { rtt ->
                        val rttMs = Math.round(rtt.toDouble() * 1000)
                        result.put("rtt", rttMs)
                        lastRtt = rttMs
                    }
                    (pair.members["localCandidateId"] as? String)?.let { localId ->
                        statsMap[localId]?.let { candidate ->
                            putCandidateInfo(result, candidate.members, false)
                        }
                    }
                    (pair.members["remoteCandidateId"] as? String)?.let { remoteId ->
                        statsMap[remoteId]?.let { candidate ->
                            putCandidateInfo(result, candidate.members, true)
                        }
                    }
                    val localType = result.optString("candidateType", "")
                    val remoteType = result.optString("remoteCandidateType", "")
                    val type = if (localType == "relay" || remoteType == "relay") "relay" else "p2p"
                    result.put("type", type)
                    relayInUse = type == "relay"
                } else {
                    result.put("type", "unknown")
                }
                result.put("relayInUse", relayInUse)
            } catch (e: Exception) {
                Log.e(TAG, "getConnectionInfo stats error", e)
                result.put("type", "unknown")
                result.put("relayInUse", relayInUse)
            } finally {
                latch.countDown()
            }
        }
        if (!latch.await(3, TimeUnit.SECONDS)) {
            return JSONObject().put("type", "unknown").put("relayInUse", relayInUse)
        }
        return result
    }

    private fun putCandidateInfo(result: JSONObject, members: Map<String, Any>, remote: Boolean) {
        val addr = members["address"] ?: members["ip"]
        val port = members["port"]
        val candidateType = members["candidateType"]
        if (addr != null && port != null) {
            result.put(if (remote) "remoteAddr" else "localAddr", "$addr:$port")
        }
        if (candidateType != null) {
            result.put(if (remote) "remoteCandidateType" else "candidateType", candidateType.toString())
        }
    }

    fun triggerDisconnect() {
        if (!isConnected) return
        Log.i(TAG, "triggerDisconnect [$machineId]")
        val listener = onDisconnectListener
        disconnect()
        listener?.invoke()
    }

    fun disconnect() {
        generation.incrementAndGet()
        onChannelResetListener?.invoke()
        heartbeat.destroy()
        channelManager.closeAll()
        try { pc?.close() } catch (_: Exception) {}
        pc = null
        isConnected = false
        disconnectedAt = 0L
        relayInUse = false
        synchronized(iceGatherLock) {
            hasHostOrSrflx = false
            hasRelay = false
            iceGatheringComplete = false
        }
    }

    private fun markFailure(reason: String) {
        lastFailureReason = reason
        Log.w(TAG, "failure: $reason [$machineId]")
    }
}
