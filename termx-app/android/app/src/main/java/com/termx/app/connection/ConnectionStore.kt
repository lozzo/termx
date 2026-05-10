package com.termx.app.connection

import android.content.Context
import android.os.Handler
import android.os.HandlerThread
import android.util.Log
import com.termx.app.connectors.RaceConnector
import com.termx.app.network.BridgeServer
import com.termx.app.network.NetworkStateManager
import com.termx.app.transfer.FileTransferManager
import com.termx.app.transport.WebRTCTransport
import kotlinx.coroutines.*
import org.json.JSONObject

/**
 * ConnectionStore — 单 machine 连接状态机
 *
 * 使用 sealed class Phase 明确状态，协程管理连接生命周期。
 */
class ConnectionStore(
    private val context: Context,
    val machineId: String,
    private val localAddresses: List<String>,
    private val hubUrls: List<String>,
    private val sessionToken: String,
    private val answerProofSecret: String?,
    private val preferredPath: String,
    private var forceRelay: Boolean,
    private val bridge: BridgeServer?,
) {
    companion object {
        private const val TAG = "TermxConnStore"
        private val RECONNECT_DELAYS = doubleArrayOf(0.5, 2.0, 4.0, 8.0, 15.0)
        private val RESUME_RECONNECT_DELAYS = doubleArrayOf(0.0, 0.5, 2.0, 4.0, 8.0)
        private const val MAX_RECONNECT_ATTEMPTS = 20
        private const val RESUME_VERIFY_TIMEOUT_MS = 2000L
        private const val RESUME_RECOVERY_WINDOW_MS = 5000L
        private const val ACTIVE_CONNECT_STALE_MS = 90_000L
        private const val RESUME_CONNECT_STALE_MS = 15_000L
    }

    sealed class Phase {
        object Idle : Phase()
        object Probing : Phase()
        data class Connecting(val path: String) : Phase()
        data class Connected(val path: String, val relayInUse: Boolean) : Phase()
        data class Verifying(val path: String, val relayInUse: Boolean) : Phase()
        data class Reconnecting(val attempt: Int) : Phase()
        object WaitingNetwork : Phase()
        data class Failed(val reason: String?) : Phase()

        val name: String get() = when (this) {
            Idle -> "idle"
            Probing -> "probing"
            is Connecting -> "connecting"
            is Connected -> "connected"
            is Verifying -> "verifying"
            is Reconnecting -> "reconnecting"
            WaitingNetwork -> "waiting_network"
            is Failed -> "failed"
        }
    }

    var phase: Phase = Phase.Idle
        private set
    private var statusText = "Ready"
    private var reconnectAttempt = 0
    private var resumeReconnect = false
    private var version = 0L
    private var connectStartedAt = 0L
    private var connectGeneration = 0L
    private var verifyGeneration = 0L

    var transport: WebRTCTransport? = null
        private set

    var fileTransferManager: FileTransferManager? = null

    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    private var connectJob: Job? = null
    private var reconnectRunnable: Runnable? = null
    private var released = false

    var stateChangeListener: ((machineId: String, snapshot: JSONObject) -> Unit)? = null

    private val workerThread = HandlerThread("TermxStore-$machineId").apply { start() }
    private val workerHandler = Handler(workerThread.looper)

    fun setForceRelay(value: Boolean) {
        forceRelay = value
    }

    fun isForceRelay(): Boolean = forceRelay

    fun updateForceRelay(value: Boolean): Boolean {
        val changed = forceRelay != value
        forceRelay = value
        return changed
    }

    fun connect() {
        if (released) return
        val p = phase
        if (p is Phase.Connecting || p is Phase.Probing) {
            if (!isActiveConnectStale()) return
            Log.i(TAG, "restarting stale connect request [$machineId]")
            cancelConnect()
        } else if (p is Phase.Connected || p is Phase.Verifying) {
            return
        }
        clearReconnectTimer()
        verifyGeneration += 1
        launchConnect()
    }

    fun retry() {
        if (released) return
        cancelConnect()
        clearReconnectTimer()
        verifyGeneration += 1
        transport?.let { fileTransferManager?.onTransportLost() }
        transport?.let { it.onDisconnectListener = null; it.disconnect() }
        transport = null
        reconnectAttempt = 0
        resumeReconnect = false
        launchConnect()
    }

    fun waitForNetwork() {
        if (released) return
        cancelConnect()
        clearReconnectTimer()
        verifyGeneration += 1
        transport?.let { fileTransferManager?.onTransportLost() }
        transport?.let { it.onDisconnectListener = null; it.disconnect() }
        transport = null
        setPhase(Phase.WaitingNetwork, "Waiting for network...")
    }

    fun release() {
        released = true
        cancelConnect()
        clearReconnectTimer()
        verifyGeneration += 1
        transport?.let { fileTransferManager?.onTransportLost() }
        transport?.let { it.onDisconnectListener = null; it.disconnect() }
        transport = null
        scope.cancel()
        workerThread.quitSafely()
    }

    fun onNetworkStateChange(current: NetworkStateManager.NetworkState, previous: NetworkStateManager.NetworkState) {
        if (released) return
        workerHandler.post { handleNetworkStateChange(current, previous) }
    }

    fun handleForegroundResume(backgroundDurationMs: Long = 0L, reason: String = "App resumed") {
        if (released) return
        workerHandler.post { handleAppResume(backgroundDurationMs, reason) }
    }

    fun getSnapshot(): JSONObject {
        val p = phase
        return JSONObject().apply {
            put("machineId", machineId)
            put("phase", p.name)
            put("statusText", statusText)
            put("path", when (p) {
                is Phase.Connected -> p.path
                is Phase.Verifying -> p.path
                is Phase.Connecting -> p.path
                else -> null
            })
            put("relayInUse", when (p) {
                is Phase.Connected -> p.relayInUse
                is Phase.Verifying -> p.relayInUse
                else -> false
            })
            put("failReason", if (p is Phase.Failed) p.reason else null)
            put("forceRelay", forceRelay)
            put("version", version)
        }
    }

    private fun launchConnect() {
        connectJob?.cancel()
        connectGeneration += 1
        val generation = connectGeneration
        connectStartedAt = System.currentTimeMillis()
        setPhase(Phase.Probing, "Probing...")

        connectJob = scope.launch {
            var transport: WebRTCTransport? = null
            try {
                val result = RaceConnector.race(
                    machineId = machineId,
                    localAddresses = localAddresses,
                    hubUrls = hubUrls,
                    sessionToken = sessionToken,
                    bridge = bridge,
                    preferredPath = preferredPath,
                    forceRelay = forceRelay,
                    onProgress = { msg -> setStatus(msg) },
                )
                if (!isCurrentConnect(generation)) {
                    if (result is RaceConnector.Result.Success) result.transport.disconnect()
                    return@launch
                }
                ensureActive()

                when (result) {
                    is RaceConnector.Result.Success -> {
                        transport = result.transport
                        attachTransportResetHandler(transport)
                        transport.onDisconnectListener = { onTransportDisconnected() }
                        transport.channelManager.fileTransferManager = fileTransferManager
                        this@ConnectionStore.transport = transport
                        reconnectAttempt = 0
                        resumeReconnect = false
                        clearConnectStartedAt(generation)
                        val path = result.path
                        setPhase(Phase.Connected(path, result.relayInUse), "Connected via $path")
                        fileTransferManager?.resumeInterruptedTransfers(transport)
                    }
                    is RaceConnector.Result.Failure -> {
                        if (!isCurrentConnect(generation)) return@launch
                        clearConnectStartedAt(generation)
                        if (result.reason == "auth") {
                            setPhase(Phase.Failed("auth"), "Authentication failed")
                        } else {
                            scheduleReconnect()
                        }
                    }
                }
            } catch (e: CancellationException) {
                transport?.disconnect()
                throw e
            } catch (e: Exception) {
                if (!isCurrentConnect(generation)) return@launch
                Log.e(TAG, "connect error", e)
                transport?.disconnect()
                clearConnectStartedAt(generation)
                scheduleReconnect()
            }
        }
    }

    private fun onTransportDisconnected() {
        if (released) return
        Log.i(TAG, "transport disconnected [$machineId]")
        fileTransferManager?.onTransportLost()
        transport = null
        verifyGeneration += 1
        scheduleReconnect()
    }

    private fun handleNetworkStateChange(
        current: NetworkStateManager.NetworkState,
        previous: NetworkStateManager.NetworkState,
    ) {
        if (released) return

        if (previous.phoneOnline && !current.phoneOnline) {
            val p = phase
            if (p is Phase.Connected || p is Phase.Verifying || p is Phase.Connecting ||
                p is Phase.Probing || p is Phase.Reconnecting) {
                cancelConnect()
                clearReconnectTimer()
                verifyGeneration += 1
                setPhase(Phase.WaitingNetwork, "Waiting for network...")
            }
            return
        }

        if (!previous.phoneOnline && current.phoneOnline) {
            val p = phase
            if (p is Phase.WaitingNetwork && transport != null) {
                verifyExistingTransport("Network recovered")
            } else if (p is Phase.WaitingNetwork || p is Phase.Failed || p is Phase.Reconnecting) {
                reconnectFromResume()
            }
            return
        }

        if (current.resumeType != null) {
            handleAppResume(current.resumeDuration, "App resumed")
            return
        }

        if (previous.phoneOnline && current.phoneOnline && current.connectionType != previous.connectionType) {
            Log.i(TAG, "network type changed: ${previous.connectionType} -> ${current.connectionType} [$machineId]")
            val p = phase
            if (p is Phase.Connected || p is Phase.Verifying) {
                transport?.onNetworkTypeChanged()
            } else if (p is Phase.WaitingNetwork || p is Phase.Reconnecting) {
                retry()
            }
        }
    }

    private fun handleAppResume(backgroundDurationMs: Long = 0L, reason: String = "App resumed") {
        if (released) return
        val p = phase
        clearReconnectTimer()
        if (p is Phase.Connecting || p is Phase.Probing) {
            val staleThreshold = if (backgroundDurationMs > 0L) {
                RESUME_CONNECT_STALE_MS.coerceAtMost(backgroundDurationMs)
            } else {
                RESUME_CONNECT_STALE_MS
            }
            if (activeConnectAgeMs() >= staleThreshold) {
                Log.i(TAG, "$reason, restarting stale connection attempt after ${activeConnectAgeMs()}ms [$machineId]")
                retry()
            }
            return
        }
        if (p is Phase.Connected && transport != null) {
            val currentTransport = transport ?: return
            val resumed = currentTransport.handleAppResume()
            val generation = nextVerifyGeneration()
            setPhase(Phase.Verifying(p.path, p.relayInUse), "$reason, verifying connection...")
            if (resumed && !currentTransport.isStaleAfterResume(backgroundDurationMs)) {
                verifyConnection(currentTransport, p.path, p.relayInUse, generation, immediateReconnect = true)
            } else {
                waitForPeerRecoveryThenVerify(currentTransport, p.path, p.relayInUse, generation, RESUME_RECOVERY_WINDOW_MS)
            }
            return
        }

        if (p is Phase.Verifying && transport != null) {
            reconnectAfterVerificationFailure(transport ?: return, immediate = true)
            return
        }

        if (p is Phase.WaitingNetwork || p is Phase.Failed || p is Phase.Reconnecting) {
            reconnectFromResume()
        }
    }

    private fun verifyExistingTransport(status: String) {
        val p = phase
        val currentTransport = transport
        if (currentTransport == null) {
            retry()
            return
        }
        val path = when (p) {
            is Phase.Connected -> p.path
            is Phase.Verifying -> p.path
            else -> preferredPath
        }
        val relayInUse = when (p) {
            is Phase.Connected -> p.relayInUse
            is Phase.Verifying -> p.relayInUse
            else -> currentTransport.currentRelayInUse()
        }
        val resumed = currentTransport.handleAppResume()
        val generation = nextVerifyGeneration()
        setPhase(Phase.Verifying(path, relayInUse), "$status, verifying connection...")
        if (resumed) {
            verifyConnection(currentTransport, path, relayInUse, generation, immediateReconnect = true)
        } else {
            waitForPeerRecoveryThenVerify(currentTransport, path, relayInUse, generation, RESUME_RECOVERY_WINDOW_MS)
        }
    }

    private fun waitForPeerRecoveryThenVerify(
        currentTransport: WebRTCTransport,
        path: String,
        relayInUse: Boolean,
        generation: Long,
        timeoutMs: Long,
    ) {
        val transportGeneration = currentTransport.generation()
        val deadline = System.currentTimeMillis() + timeoutMs
        val check = object : Runnable {
            override fun run() {
                if (!isCurrentVerification(currentTransport, generation, transportGeneration)) return
                if (currentTransport.isPeerConnected()) {
                    verifyConnection(currentTransport, path, relayInUse, generation, immediateReconnect = true)
                    return
                }
                if (System.currentTimeMillis() < deadline && currentTransport.hasPeerConnection()) {
                    workerHandler.postDelayed(this, 200)
                    return
                }
                reconnectAfterVerificationFailure(currentTransport, immediate = true)
            }
        }
        workerHandler.post(check)
    }

    private fun verifyConnection(
        currentTransport: WebRTCTransport,
        path: String,
        relayInUse: Boolean,
        generation: Long,
        immediateReconnect: Boolean,
    ) {
        val transportGeneration = currentTransport.generation()
        scope.launch {
            try {
                withContext(Dispatchers.IO) {
                    currentTransport.verifyStatus(RESUME_VERIFY_TIMEOUT_MS)
                }
                workerHandler.post {
                    if (!isCurrentVerification(currentTransport, generation, transportGeneration)) return@post
                    reconnectAttempt = 0
                    resumeReconnect = false
                    setPhase(Phase.Connected(path, relayInUse), "Connected via $path")
                }
            } catch (e: Exception) {
                workerHandler.post {
                    if (!isCurrentVerification(currentTransport, generation, transportGeneration)) return@post
                    Log.i(TAG, "resume verify failed [$machineId]")
                    reconnectAfterVerificationFailure(currentTransport, immediateReconnect)
                }
            }
        }
    }

    private fun reconnectAfterVerificationFailure(currentTransport: WebRTCTransport, immediate: Boolean = false) {
        if (released || transport !== currentTransport) return
        currentTransport.onDisconnectListener = null
        currentTransport.disconnect()
        transport = null
        reconnectAttempt = 0
        resumeReconnect = true
        scheduleReconnect(immediate)
    }

    private fun scheduleReconnect(immediate: Boolean = false) {
        if (released) return
        clearReconnectTimer()
        reconnectAttempt++
        if (reconnectAttempt > MAX_RECONNECT_ATTEMPTS) {
            resumeReconnect = false
            setPhase(Phase.Failed("max_retries"), "Connection failed after $reconnectAttempt attempts")
            return
        }
        val delays = if (resumeReconnect) RESUME_RECONNECT_DELAYS else RECONNECT_DELAYS
        val delayIdx = (reconnectAttempt - 1).coerceAtMost(delays.size - 1)
        val delaySec = if (immediate) 0.0 else delays[delayIdx]
        val delayMs = (delaySec * 1000).toLong()
        val status = if (resumeReconnect) {
            "Restoring connection..."
        } else {
            "Reconnecting (attempt $reconnectAttempt)..."
        }
        setPhase(Phase.Reconnecting(reconnectAttempt), status)

        reconnectRunnable = Runnable {
            reconnectRunnable = null
            if (!released && phase !is Phase.Connected && phase !is Phase.Verifying &&
                phase !is Phase.Connecting && phase !is Phase.Probing) {
                launchConnect()
            }
        }.also { workerHandler.postDelayed(it, delayMs) }
    }

    private fun clearReconnectTimer() {
        reconnectRunnable?.let { workerHandler.removeCallbacks(it) }
        reconnectRunnable = null
    }

    private fun cancelConnect() {
        connectJob?.cancel()
        connectJob = null
        connectGeneration += 1
        connectStartedAt = 0L
    }

    private fun attachTransportResetHandler(currentTransport: WebRTCTransport) {
        currentTransport.onChannelResetListener = {
            bridge?.resetChannelsForMachine(machineId)
        }
    }

    private fun reconnectFromResume() {
        if (released) return
        cancelConnect()
        clearReconnectTimer()
        transport?.let { fileTransferManager?.onTransportLost() }
        transport?.let { it.onDisconnectListener = null; it.disconnect() }
        transport = null
        reconnectAttempt = 0
        resumeReconnect = true
        scheduleReconnect(immediate = true)
    }

    private fun nextVerifyGeneration(): Long {
        verifyGeneration += 1
        return verifyGeneration
    }

    private fun isCurrentVerification(
        currentTransport: WebRTCTransport,
        generation: Long,
        transportGeneration: Long,
    ): Boolean {
        return !released &&
            transport === currentTransport &&
            phase is Phase.Verifying &&
            verifyGeneration == generation &&
            currentTransport.generation() == transportGeneration
    }

    private fun activeConnectAgeMs(now: Long = System.currentTimeMillis()): Long {
        return if (connectStartedAt > 0L) (now - connectStartedAt).coerceAtLeast(0L) else 0L
    }

    private fun isActiveConnectStale(now: Long = System.currentTimeMillis()): Boolean {
        val p = phase
        if (p !is Phase.Connecting && p !is Phase.Probing) return false
        return connectStartedAt > 0L && now - connectStartedAt >= ACTIVE_CONNECT_STALE_MS
    }

    private fun isCurrentConnect(generation: Long): Boolean {
        return generation == connectGeneration
    }

    private fun clearConnectStartedAt(generation: Long) {
        if (isCurrentConnect(generation)) connectStartedAt = 0L
    }

    private fun setPhase(newPhase: Phase, text: String) {
        phase = newPhase
        statusText = text
        version += 1
        Log.i(TAG, "phase: ${newPhase.name} [$machineId]")
        notifyStateChange()
    }

    private fun setStatus(text: String) {
        statusText = text
        version += 1
        notifyStateChange()
    }

    private fun notifyStateChange() {
        stateChangeListener?.invoke(machineId, getSnapshot())
    }
}
