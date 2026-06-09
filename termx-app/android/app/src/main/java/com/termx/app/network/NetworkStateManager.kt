package com.termx.app.network

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner

class NetworkStateManager(context: Context) {

    companion object {
        private const val TAG = "TermxNetworkState"
        private const val QUICK_THRESHOLD = 30_000L
        private const val NORMAL_THRESHOLD = 300_000L
        private const val DEBOUNCE_MS = 100L
    }

    fun interface Listener {
        fun onNetworkStateChange(current: NetworkState, previous: NetworkState)
    }

    data class NetworkState(
        val phoneOnline: Boolean,
        val connectionType: String,
        val appActive: Boolean,
        val networkReady: Boolean,
        val resumeType: String?,
        val resumeDuration: Long,
    )

    private val ctx: Context = context.applicationContext
    private val mainHandler = Handler(Looper.getMainLooper())

    private var phoneOnline = true
    private var connectionType = ""
    private var appActive = true
    private var resumeType: String? = null
    private var resumeDuration = 0L
    private var backgroundTimestamp = 0L

    var listener: Listener? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var lifecycleObserver: DefaultLifecycleObserver? = null
    var lastSnapshot: NetworkState = snapshot()
        private set

    private var pendingNotify: Runnable? = null

    val state: NetworkState get() = lastSnapshot

    fun init() {
        initConnectivity()
        initLifecycle()
    }

    fun destroy() {
        networkCallback?.let { cb ->
            val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            try { cm?.unregisterNetworkCallback(cb) } catch (_: Exception) {}
            networkCallback = null
        }
        lifecycleObserver?.let { obs ->
            try { ProcessLifecycleOwner.get().lifecycle.removeObserver(obs) } catch (_: Exception) {}
            lifecycleObserver = null
        }
        pendingNotify?.let { mainHandler.removeCallbacks(it) }
        pendingNotify = null
    }

    private fun initConnectivity() {
        val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
        val activeNet = cm.activeNetwork
        if (activeNet != null) {
            val caps = cm.getNetworkCapabilities(activeNet)
            phoneOnline = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true
            connectionType = getConnectionType(caps)
        } else {
            phoneOnline = false
            connectionType = "none"
        }

        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                phoneOnline = true
                connectionType = getConnectionType(cm.getNetworkCapabilities(network))
                scheduleNotify()
            }
            override fun onLost(network: Network) {
                if (cm.activeNetwork == null) {
                    phoneOnline = false
                    connectionType = "none"
                    scheduleNotify()
                }
            }
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                val t = getConnectionType(caps)
                if (t != connectionType) {
                    connectionType = t
                    scheduleNotify()
                }
            }
        }
        networkCallback = callback
        cm.registerNetworkCallback(
            NetworkRequest.Builder()
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build(),
            callback,
        )
    }

    private fun initLifecycle() {
        val obs = object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                val now = System.currentTimeMillis()
                val duration = if (backgroundTimestamp > 0) now - backgroundTimestamp else 0
                backgroundTimestamp = 0
                appActive = true
                resumeType = classifyResume(duration)
                resumeDuration = duration
                scheduleNotify()
            }
            override fun onStop(owner: LifecycleOwner) {
                backgroundTimestamp = System.currentTimeMillis()
                appActive = false
                scheduleNotify()
            }
        }
        lifecycleObserver = obs
        mainHandler.post { ProcessLifecycleOwner.get().lifecycle.addObserver(obs) }
    }

    private fun getConnectionType(caps: NetworkCapabilities?): String = when {
        caps == null -> "unknown"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
        else -> "unknown"
    }

    private fun classifyResume(duration: Long): String = when {
        duration < QUICK_THRESHOLD -> "quick"
        duration < NORMAL_THRESHOLD -> "normal"
        else -> "cold"
    }

    private fun snapshot() = NetworkState(
        phoneOnline = phoneOnline,
        connectionType = connectionType,
        appActive = appActive,
        networkReady = phoneOnline,
        resumeType = resumeType,
        resumeDuration = resumeDuration,
    )

    private fun scheduleNotify() {
        if (pendingNotify != null) return
        val r = Runnable {
            pendingNotify = null
            val prev = lastSnapshot
            val curr = snapshot()
            lastSnapshot = curr
            resumeType = null
            resumeDuration = 0
            try { listener?.onNetworkStateChange(curr, prev) } catch (e: Exception) {
                Log.e(TAG, "listener error", e)
            }
        }
        pendingNotify = r
        mainHandler.postDelayed(r, DEBOUNCE_MS)
    }
}
