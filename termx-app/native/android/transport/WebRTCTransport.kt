package com.termx.app.transport

import android.content.Context
import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.managed.ManagedIceServer
import com.termx.app.managed.ManagedSignalAnswer
import com.termx.app.managed.ManagedSignalOffer
import org.json.JSONObject
import org.webrtc.*
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * WebRTCTransport — termx PeerConnection 生命周期
 *
 * 该类只拥有 Android PeerConnection、ICE/DTLS 和 DataChannel primitive。
 * Cloud adapter 在外部交换 SDP/ICE；本类不访问 Hub HTTP，不接收账号 token，也不解释 terminal capability。
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

    /** startManaged 创建 PeerConnection/DataChannel 和完整 offer；调用方负责通过 ManagedCloudAdapter 交换该 offer。 */
    fun startManaged(iceServers: List<ManagedIceServer>, relayOnly: Boolean): ManagedSignalOffer? {
        lastFailureReason = null
        relayInUse = false
        return try {
            val f = factory ?: run { markFailure("init"); return null }
            val servers = parseIceServers(iceServers)
            val config = PeerConnection.RTCConfiguration(servers).apply {
                sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
                if (relayOnly) {
                    iceTransportsType = PeerConnection.IceTransportsType.RELAY
                }
            }
            pc = f.createPeerConnection(config, createObserver()) ?: run {
                markFailure("init"); return null
            }
            channelManager.createInitialChannels(pc!!)

            val offer = createOffer() ?: run { markFailure("offer"); disconnect(); return null }
            if (!setLocalDesc(offer)) { markFailure("offer"); disconnect(); return null }

            val waitRelay = relayOnly || iceServers.isNotEmpty()
            waitForICEGathering(if (waitRelay) ICE_GATHER_TIMEOUT_HUB else ICE_GATHER_TIMEOUT_LOCAL, waitRelay)
            ManagedSignalOffer(pc!!.localDescription.description)
        } catch (e: Exception) {
            if (lastFailureReason == null) markFailure("unknown")
            Log.e(TAG, "startManaged failed", e)
            disconnect()
            null
        }
    }

    /** finishManaged 消费 Cloud adapter 返回的 answer/ICE 并等待 PeerConnection 与初始 DataChannel 就绪。 */
    fun finishManaged(answer: ManagedSignalAnswer): Boolean {
        if (answer.sdp.isBlank()) {
            markFailure("signal")
            disconnect()
            return false
        }
        val remoteSDP = SessionDescription(SessionDescription.Type.ANSWER, answer.sdp)

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
        answer.candidates.forEach { candidateSdp ->
            if (candidateSdp.isNotBlank()) pc!!.addIceCandidate(IceCandidate("", 0, candidateSdp))
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

    private fun parseIceServers(servers: List<ManagedIceServer>): List<PeerConnection.IceServer> = servers.mapNotNull { server ->
        if (server.urls.isEmpty()) return@mapNotNull null
        PeerConnection.IceServer.builder(server.urls).apply {
            if (server.username.isNotBlank()) setUsername(server.username)
            if (server.credential.isNotBlank()) setPassword(server.credential)
        }.createIceServer()
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
