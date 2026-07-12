package com.termx.app.transport

import android.content.Context
import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.managed.ManagedIceServer
import com.termx.app.managed.ManagedPathQualitySample
import com.termx.app.managed.ManagedPathQualitySampleSource
import com.termx.app.managed.ManagedSignalAnswer
import com.termx.app.managed.ManagedSignalOffer
import com.termx.app.managed.ObservedPath
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
) : ManagedPathQualitySampleSource {
    companion object {
        private const val TAG = "TermxWebRTC"

        private const val ICE_GATHER_TIMEOUT_HUB = 8000
        private const val ICE_GATHER_TIMEOUT_LOCAL = 3000
        private const val CONNECT_TIMEOUT = 15000
        private const val DATA_CHANNEL_OPEN_TIMEOUT = 10000
        private const val RESUME_STALE_THRESHOLD_MS = 7000L
        private const val QUALITY_STATS_TIMEOUT_MS = 500L

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
    @Volatile private var observedPath: ObservedPath? = null
    private val generation = AtomicLong(0)
    private val beforeCloseLock = Any()
    private val beforeCloseListeners = mutableListOf<() -> Unit>()
    @Volatile private var latestQualitySample: ManagedPathQualitySample? = null

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
        observedPath = null
        return try {
            val f = factory ?: run { markFailure("init"); return null }
            // ICE URL 与候选类型不含短期凭据，用于区分 TURN 配置、网络可达和 DTLS 失败边界。
            Log.i(TAG, "ICE config relayOnly=$relayOnly urls=${iceServers.flatMap { it.urls }}")
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
        val answerCandidateTypes = answer.sdp.lineSequence()
            .filter { it.startsWith("a=candidate:") }
            .map { candidateType(it.lowercase()) }
            .toList()
        Log.i(TAG, "Remote ICE candidates=$answerCandidateTypes extra=${answer.candidates.size} [$machineId]")
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

        // 这里只等待唯一 protocol DataChannel；capability 与 wire Hello 由公开 authorizer 随后完成。
        val protocolOpen = channelManager.waitChannelOpen(DATA_CHANNEL_OPEN_TIMEOUT)
        if (!protocolOpen) {
            markFailure("timeout")
            disconnect()
            return false
        }

        isConnected = true
        connectedAt = System.currentTimeMillis()
        relayInUse = getConnectionInfo().optString("type") == "relay"
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
            Log.i(TAG, "ICE candidate type=${candidateType(type)} [$machineId]")
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
            Log.i(TAG, "ICE gathering state=$state relay=$hasRelay [$machineId]")
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

    private fun candidateType(candidate: String): String = when {
        candidate.contains("typ relay") -> "relay"
        candidate.contains("typ srflx") -> "srflx"
        candidate.contains("typ prflx") -> "prflx"
        candidate.contains("typ host") -> "host"
        else -> "unknown"
    }

    private fun parseIceServers(servers: List<ManagedIceServer>): List<PeerConnection.IceServer> = servers.mapNotNull { server ->
        if (server.urls.isEmpty()) return@mapNotNull null
        PeerConnection.IceServer.builder(server.urls).apply {
            if (server.username.isNotBlank()) setUsername(server.username)
            if (server.credential.isNotBlank()) setPassword(server.credential)
        }.createIceServer()
    }

    // ─── Public API ──────────────────────────────────────────────────────────

    /** sendToProtocol 把 JS connection-level multiplexer 的 frame 送入唯一 termx protocol DataChannel。 */
    fun sendToProtocol(data: ByteArray) = channelManager.sendRawProtocol(data)

    /** Blocking runtime API request for heartbeat/verification. Throws on timeout or closed channel. */
    fun sendApiRequest(method: String, path: String, body: String?, timeoutMs: Long): String {
        return channelManager.sendApiRequest(method, path, body, timeoutMs)
    }

    fun isPeerConnected(): Boolean =
        pc?.connectionState() == PeerConnection.PeerConnectionState.CONNECTED

    fun hasPeerConnection(): Boolean = pc != null

    /** 旧方法名只保留 native lifecycle 调用兼容；返回值代表已授权的 termx protocol，不代表旧 api DataChannel。 */
    fun isApiChannelOpen(): Boolean = channelManager.isProtocolOpen()

    /** receiveAuthorizationMessage 读取 auth 阶段完整 DataChannel envelope。 */
    fun receiveAuthorizationMessage(timeoutMs: Long): ByteArray? = channelManager.receiveAuthorizationMessage(timeoutMs)

    /** sendAuthorizationMessage 发送 auth 阶段完整 DataChannel envelope。 */
    fun sendAuthorizationMessage(frame: ByteArray): Boolean = channelManager.sendAuthorizationMessage(frame)

    /** activateTermxProtocol 完成 auth 后唯一一次 wire v3 Hello，并在成功时启动 liveness。 */
    fun activateTermxProtocol(timeoutMs: Long): Boolean {
        val activated = channelManager.activateTermxProtocol(timeoutMs)
        if (activated) heartbeat.start()
        return activated
    }

    /**
     * remoteCertificateFingerprint 从实际 selected DTLS transport 的 `remoteCertificateId` stats 读取 SHA-256 fingerprint。
     * SDP fingerprint 或 cloud signaling 字段不能作为 fallback。
     */
    fun remoteCertificateFingerprint(timeoutMs: Long): String? {
        val peer = pc ?: return null
        val latch = CountDownLatch(1)
        var fingerprint: String? = null
        peer.getStats { reports ->
            try {
                fingerprint = remoteCertificateFingerprintFromStats(reports.statsMap)
            } finally {
                latch.countDown()
            }
        }
        if (!latch.await(timeoutMs, TimeUnit.MILLISECONDS)) return null
        return fingerprint
    }

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

    /** currentObservedPath 返回 selected candidate pair 已确认的路径；stats 尚未形成时保持 null。 */
    fun currentObservedPath(): ObservedPath? = observedPath

    /**
     * readPathQualitySample 读取 selected candidate pair 的脱敏累计 stats。
     * 返回值不包含 local/remote address、SDP、DataChannel payload、terminal 或 credential；stats 未就绪时返回 null。
     */
    override fun readPathQualitySample(): ManagedPathQualitySample? {
        val peer = pc ?: return null
        val latch = CountDownLatch(1)
        var sample: ManagedPathQualitySample? = null
        peer.getStats { reports ->
            try {
                val statsMap = reports.statsMap
                val activePairId = statsMap.values.firstNotNullOfOrNull { report ->
                    if (report.type == "transport") report.members["selectedCandidatePairId"]?.toString() else null
                }
                val pairEntry = activePairId?.let { pairId -> statsMap[pairId]?.let { pairId to it } }
                    ?: statsMap.entries.asSequence()
                        .filter { (_, report) ->
                            report.type == "candidate-pair" && report.members["nominated"] == true &&
                                report.members["state"]?.toString() == "succeeded"
                        }
                        .minByOrNull { it.key }
                        ?.let { it.key to it.value }
                val pairId = pairEntry?.first ?: return@getStats
                val pair = pairEntry.second
                val localId = pair.members["localCandidateId"]?.toString().orEmpty()
                val remoteId = pair.members["remoteCandidateId"]?.toString().orEmpty()
                val local = statsMap[localId]
                val remote = statsMap[remoteId]
                if (local == null || remote == null) return@getStats
                val localType = local.members["candidateType"]?.toString().orEmpty()
                val remoteType = remote.members["candidateType"]?.toString().orEmpty()
                val observedPath = if (localType == "relay" || remoteType == "relay") {
                    ObservedPath.SINGLE_RELAY
                } else {
                    ObservedPath.DIRECT
                }
                var rttMillis = statsSecondsMillis(pair.members["currentRoundTripTime"])
                if (rttMillis == 0L) {
                    val sctp = statsMap.values.firstOrNull { it.type == "sctp-transport" || it.type == "sctpTransport" }
                    rttMillis = statsSecondsMillis(sctp?.members?.get("smoothedRoundTripTime"))
                }
                val retransmissions = statsNonNegativeLong(pair.members["retransmissionsSent"])
                val discarded = statsNonNegativeLong(pair.members["packetsDiscardedOnSend"])
                val networkClass = local.members["networkType"]?.toString()?.trim()?.lowercase()
                    ?.takeIf { it.matches(Regex("[a-z0-9._-]{1,64}")) } ?: "unknown"
                val state = peer.connectionState()
                val connected = state == PeerConnection.PeerConnectionState.CONNECTED
                sample = ManagedPathQualitySample(
                    pairId = pairId,
                    observedPath = observedPath,
                    sampledAtUnixMillis = System.currentTimeMillis(),
                    roundTripTimeMillis = rttMillis,
                    bytesSent = statsNonNegativeLong(pair.members["bytesSent"]),
                    bytesReceived = statsNonNegativeLong(pair.members["bytesReceived"]),
                    packetsSent = statsNonNegativeLong(pair.members["packetsSent"]),
                    lossEvents = saturatingStatsAdd(retransmissions, discarded),
                    connected = connected,
                    networkClass = networkClass,
                )
                latestQualitySample = sample
                this.observedPath = observedPath
                relayInUse = observedPath == ObservedPath.SINGLE_RELAY
            } catch (e: Exception) {
                Log.w(TAG, "quality stats unavailable [$machineId]", e)
            } finally {
                latch.countDown()
            }
        }
        if (!latch.await(QUALITY_STATS_TIMEOUT_MS, TimeUnit.MILLISECONDS)) return null
        return sample
    }

    /** readFinalPathQualitySample 非阻塞投影最近累计计数和关闭前连接状态，保证 telemetry 不延迟 transport close。 */
    override fun readFinalPathQualitySample(): ManagedPathQualitySample? {
        val latest = latestQualitySample ?: return null
        val state = pc?.connectionState()
        val connected = state != PeerConnection.PeerConnectionState.DISCONNECTED &&
            state != PeerConnection.PeerConnectionState.FAILED &&
            state != PeerConnection.PeerConnectionState.CLOSED
        return latest.copy(
            sampledAtUnixMillis = System.currentTimeMillis().coerceAtLeast(latest.sampledAtUnixMillis + 1),
            connected = connected,
        )
    }

    /** addBeforeCloseListener 注册一次性 PeerConnection 关闭前 callback；仅用于抓取最终 stats，不允许阻止 transport 关闭。 */
    fun addBeforeCloseListener(listener: () -> Unit) {
        synchronized(beforeCloseLock) { beforeCloseListeners += listener }
    }

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
                    observedPath = if (type == "relay") ObservedPath.SINGLE_RELAY else ObservedPath.DIRECT
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
        val qualityListeners = synchronized(beforeCloseLock) {
            beforeCloseListeners.toList().also { beforeCloseListeners.clear() }
        }
        qualityListeners.forEach { listener -> runCatching(listener) }
        onChannelResetListener?.invoke()
        heartbeat.destroy()
        channelManager.closeAll()
        try { pc?.close() } catch (_: Exception) {}
        pc = null
        latestQualitySample = null
        observedPath = null
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

    private fun statsNonNegativeLong(value: Any?): Long {
        val number = value as? Number ?: return 0L
        val raw = number.toDouble()
        if (!raw.isFinite() || raw <= 0.0) return 0L
        return if (raw >= Long.MAX_VALUE.toDouble()) Long.MAX_VALUE else raw.toLong()
    }

    private fun statsSecondsMillis(value: Any?): Long {
        val number = value as? Number ?: return 0L
        val millis = number.toDouble() * 1000.0
        if (!millis.isFinite() || millis <= 0.0) return 0L
        return if (millis >= Long.MAX_VALUE.toDouble()) Long.MAX_VALUE else millis.toLong()
    }

    private fun saturatingStatsAdd(left: Long, right: Long): Long =
        if (Long.MAX_VALUE - left < right) Long.MAX_VALUE else left + right
}

/** remoteCertificateFingerprintFromStats 只接受 transport 明确引用的 remote certificate。 */
internal fun remoteCertificateFingerprintFromStats(stats: Map<String, RTCStats>): String? {
    val remoteCertificateId = stats.values.asSequence()
        .filter { it.type == "transport" }
        .mapNotNull { it.members["remoteCertificateId"]?.toString()?.takeIf(String::isNotBlank) }
        .firstOrNull() ?: return null
    val certificate = stats[remoteCertificateId] ?: return null
    if (certificate.type != "certificate") return null
    val algorithm = certificate.members["fingerprintAlgorithm"]?.toString()?.trim()?.lowercase() ?: return null
    if (algorithm != "sha-256") return null
    val raw = certificate.members["fingerprint"]?.toString()?.trim()?.lowercase() ?: return null
    val compact = raw.removePrefix("sha-256:").replace(":", "")
    if (!compact.matches(Regex("[0-9a-f]{64}"))) return null
    return "sha-256:" + compact.chunked(2).joinToString(":")
}
