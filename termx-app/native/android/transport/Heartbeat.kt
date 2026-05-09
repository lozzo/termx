package com.termx.app.transport

import android.os.Handler
import android.os.HandlerThread
import android.util.Log

class Heartbeat(
    private val transport: WebRTCTransport,
    private val onDead: () -> Unit,
) {
    companion object {
        private const val TAG = "TermxHeartbeat"
        private const val INTERVAL = 5000L
        private const val RTT_MULTIPLIER = 3
        private const val TIMEOUT_MIN = 500L
        private const val TIMEOUT_MAX = 3000L
        private const val MAX_FAILS = 4
        private const val GRACE_PERIOD = 5000L
        private const val GRACE_PERIOD_EARLY = 12000L
        private const val EARLY_THRESHOLD = 15000L
        private const val PROBE_INTERVAL = 5000L
        private const val TRANSFER_ACTIVITY_WINDOW = 12000L
        private const val TRANSFER_BUSY_RETRY = 8000L
    }

    private val thread = HandlerThread("termx-heartbeat").apply { start() }
    private val handler = Handler(thread.looper)

    private var failCount = 0
    private var heartbeatRunnable: Runnable? = null
    private var graceRunnable: Runnable? = null
    private var probeRunnable: Runnable? = null
    @Volatile private var lastSuccessAt = System.currentTimeMillis()

    fun start() {
        stop(); stopProbe()
        failCount = 0
        markSuccess()
        schedule(INTERVAL)
    }

    fun stop() {
        heartbeatRunnable?.let { handler.removeCallbacks(it) }
        heartbeatRunnable = null
    }

    fun stopGrace() {
        graceRunnable?.let { handler.removeCallbacks(it) }
        graceRunnable = null
    }

    fun stopProbe() {
        probeRunnable?.let { handler.removeCallbacks(it) }
        probeRunnable = null
    }

    fun destroy() {
        stop(); stopGrace(); stopProbe()
        thread.quitSafely()
    }

    fun handleAppResume(): Boolean {
        if (!transport.hasPeerConnection()) return false
        stopProbe()
        graceRunnable?.let {
            handler.removeCallbacks(it)
            graceRunnable = null
        }
        failCount = 0
        return transport.isPeerConnected() && transport.isApiChannelOpen()
    }

    fun onConnectionStateConnected() {
        graceRunnable?.let { handler.removeCallbacks(it); graceRunnable = null }
        stopProbe()
        markSuccess()
        start()
    }

    fun onConnectionStateDisconnected(connectedAt: Long) {
        stop()
        val sinceConnected = System.currentTimeMillis() - connectedAt
        val grace = if (sinceConnected < EARLY_THRESHOLD) GRACE_PERIOD_EARLY else GRACE_PERIOD
        if (graceRunnable == null) {
            val r = Runnable {
                graceRunnable = null
                stopProbe()
                if (!transport.isConnected) return@Runnable
                if (!transport.isPeerConnected()) {
                    Log.i(TAG, "grace expired, triggering disconnect")
                    onDead()
                }
            }
            graceRunnable = r
            handler.postDelayed(r, grace)
        }
        scheduleProbe(0, aggressiveFirst = true)
    }

    fun onConnectionStateFailed() {
        stop(); stopGrace(); stopProbe()
        onDead()
    }

    fun onNetworkTypeChanged() {
        stop(); failCount = 0; schedule(200)
    }

    fun millisSinceLastSuccess(now: Long = System.currentTimeMillis()): Long =
        (now - lastSuccessAt).coerceAtLeast(0L)

    private fun markSuccess() {
        lastSuccessAt = System.currentTimeMillis()
    }

    private fun schedule(delayMs: Long) {
        val r = Runnable {
            heartbeatRunnable = null
            if (!transport.isConnected) return@Runnable
            if (!transport.isApiChannelOpen()) {
                stopGrace(); stopProbe(); onDead(); return@Runnable
            }
            val timeout = (transport.lastRtt * RTT_MULTIPLIER).coerceIn(TIMEOUT_MIN, TIMEOUT_MAX)
            try {
                val start = System.currentTimeMillis()
                transport.sendApiRequest("GET", "/status", null, timeout)
                transport.lastRtt = (System.currentTimeMillis() - start).coerceAtLeast(1L)
                failCount = 0
                markSuccess()
                schedule(INTERVAL)
            } catch (e: Exception) {
                if (transport.hasRecentFileTransferActivity(TRANSFER_ACTIVITY_WINDOW)) {
                    failCount = 0
                    Log.i(TAG, "heartbeat delayed during active file transfer")
                    schedule(TRANSFER_BUSY_RETRY)
                    return@Runnable
                }
                failCount++
                Log.i(TAG, "heartbeat fail #$failCount/$MAX_FAILS: ${e.message}")
                if (failCount >= MAX_FAILS) {
                    stopGrace(); stopProbe(); onDead()
                } else {
                    schedule(failCount * 1000L)
                }
            }
        }
        heartbeatRunnable = r
        handler.postDelayed(r, delayMs)
    }

    private fun scheduleProbe(delayMs: Long, aggressiveFirst: Boolean = false) {
        stopProbe()
        val r = Runnable {
            probeRunnable = null
            if (graceRunnable == null || !transport.isConnected) return@Runnable
            val timeout = (transport.lastRtt * RTT_MULTIPLIER).coerceIn(TIMEOUT_MIN, TIMEOUT_MAX)
            try {
                val start = System.currentTimeMillis()
                transport.sendApiRequest("GET", "/status", null, timeout)
                transport.lastRtt = (System.currentTimeMillis() - start).coerceAtLeast(1L)
                graceRunnable?.let { handler.removeCallbacks(it); graceRunnable = null }
                markSuccess()
                Log.i(TAG, "probe OK, grace cancelled")
                start()
            } catch (e: Exception) {
                if (transport.hasRecentFileTransferActivity(TRANSFER_ACTIVITY_WINDOW)) {
                    scheduleProbe(TRANSFER_BUSY_RETRY)
                    return@Runnable
                }
                if (aggressiveFirst && !transport.isPeerConnected()) {
                    graceRunnable?.let { handler.removeCallbacks(it) }
                    graceRunnable = null
                    onDead()
                } else {
                    Log.d(TAG, "probe failed, retrying in ${PROBE_INTERVAL}ms: ${e.message}")
                    scheduleProbe(PROBE_INTERVAL)
                }
            }
        }
        probeRunnable = r
        handler.postDelayed(r, delayMs)
    }
}
