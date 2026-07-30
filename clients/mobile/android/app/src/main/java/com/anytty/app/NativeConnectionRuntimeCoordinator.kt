package com.anytty.app

internal fun interface PendingRuntimeWork {
    fun cancel()
}

internal class NativeConnectionRuntimeCoordinator(
    private val scheduleNetworkRestart: (delayMillis: Long, task: () -> Unit) -> PendingRuntimeWork,
    private val isApplicationStarted: () -> Boolean,
    private val startRuntime: () -> Unit,
    private val restartRuntime: () -> Unit,
    private val suspendRuntime: () -> Unit,
    private val resetRuntime: () -> Unit,
    private val generationChanging: (reason: String, epoch: Long) -> Unit,
    private val generationChanged: (reason: String, epoch: Long) -> Unit,
    private val generationChangeFailed: (reason: String, epoch: Long, failure: Throwable) -> Unit,
) {
    companion object {
        internal const val NETWORK_RESTART_DELAY_MILLIS = 300L
    }

    private enum class State { NEW, READY, DESTROYED }

    private val monitor = Any()
    private var state = State.NEW
    private var activeNetwork: Any? = null
    private var networkEpoch = 0L
    private var pendingNetworkRestart: PendingRuntimeWork? = null

    fun load(initialNetwork: Any?) = synchronized(monitor) {
        check(state == State.NEW) { "native runtime cannot be loaded after destruction" }
        startRuntime()
        activeNetwork = initialNetwork
        state = State.READY
    }

    fun isReady(): Boolean = synchronized(monitor) { state == State.READY }

    fun onNetworkLost(network: Any) = synchronized(monitor) {
        if (state != State.READY || activeNetwork != network) return@synchronized
        val epoch = invalidatePendingNetworkRestartLocked()
        activeNetwork = null
        suspendRuntime()
        generationChanging("network_lost", epoch)
    }

    fun onNetworkAvailable(network: Any) = synchronized(monitor) {
        if (state != State.READY) return@synchronized
        val previous = activeNetwork
        activeNetwork = network
        if (previous == network || !isApplicationStarted()) return@synchronized
        val epoch = invalidatePendingNetworkRestartLocked()
        generationChanging("network_available", epoch)
        pendingNetworkRestart = scheduleNetworkRestart(NETWORK_RESTART_DELAY_MILLIS) {
            finishNetworkAvailable(network, epoch)
        }
    }

    fun suspendForLifecycle() = synchronized(monitor) {
        if (state != State.READY) return@synchronized
        invalidatePendingNetworkRestartLocked()
        suspendRuntime()
    }

    fun restartForForeground() = synchronized(monitor) {
        requireReadyLocked()
        invalidatePendingNetworkRestartLocked()
        restartRuntime()
    }

    fun resetLocalPairings() = synchronized(monitor) {
        requireReadyLocked()
        invalidatePendingNetworkRestartLocked()
        resetRuntime()
    }

    fun destroy() = synchronized(monitor) {
        if (state == State.DESTROYED) return@synchronized
        state = State.DESTROYED
        invalidatePendingNetworkRestartLocked()
        activeNetwork = null
        suspendRuntime()
    }

    private fun finishNetworkAvailable(network: Any, epoch: Long) = synchronized(monitor) {
        if (state != State.READY || networkEpoch != epoch || activeNetwork != network || !isApplicationStarted()) return@synchronized
        pendingNetworkRestart = null
        runCatching { restartRuntime() }
            .onSuccess { generationChanged("network_available", epoch) }
            .onFailure { generationChangeFailed("network_available", epoch, it) }
    }

    private fun invalidatePendingNetworkRestartLocked(): Long {
        pendingNetworkRestart?.cancel()
        pendingNetworkRestart = null
        networkEpoch += 1
        return networkEpoch
    }

    private fun requireReadyLocked() {
        check(state == State.READY) { "native runtime is not available" }
    }
}
