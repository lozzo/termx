package com.anytty.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.Network
import android.util.Log
import android.util.Base64
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.anytty.app.goclient.AndroidGoClientEngine
import com.anytty.app.goclient.AndroidClientAccessCredentialStore
import com.anytty.app.goclient.AndroidEndpointRegistryStore
import com.anytty.app.goclient.AndroidSSHCredentialStore
import com.anytty.app.goclient.GoClientBridgeServer
import java.security.SecureRandom
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner

@CapacitorPlugin(name = "NativeConnection")
class NativeConnectionPlugin : Plugin(), DefaultLifecycleObserver {

    companion object {
        private const val TAG = "AnyTTYNativePlugin"
        private const val BRIDGE_TOKEN_BYTES = 32
    }

    private var bridgePort: Int = 0
    private var bridgeToken: String = ""
    private var goClientEngine: AndroidGoClientEngine? = null
    private var goBridgeServer: GoClientBridgeServer? = null
    private val runtimeScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var lifecycleReady = false
    private val connectivityManager by lazy { context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager }
    @Volatile private var activeNetwork: Network? = null
    @Volatile private var networkChangeEpoch: Long = 0
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onLost(network: Network) {
            if (activeNetwork != network) return
            val epoch = networkChangeEpoch + 1
            networkChangeEpoch = epoch
            activeNetwork = null
            suspendGoBridgeServer()
            notifyListeners("generationChanging", JSObject().put("reason", "network_lost").put("epoch", epoch))
        }

        override fun onAvailable(network: Network) {
            val previous = activeNetwork
            activeNetwork = network
            if (previous == network || !lifecycleReady) return
            if (!ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) return
            val epoch = networkChangeEpoch + 1
            networkChangeEpoch = epoch
            notifyListeners("generationChanging", JSObject().put("reason", "network_available").put("epoch", epoch))
            runtimeScope.launch {
                // Wi-Fi -> cellular -> Wi-Fi 会连续发布多个 onAvailable。只允许最终 active network
                // 创建 generation，避免后一个 bridge 在 JS 正读取前一个 registry 时将其关闭。
                delay(300)
                if (networkChangeEpoch != epoch || activeNetwork != network || !lifecycleReady) return@launch
                if (!ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) return@launch
                runCatching { restartGoBridgeServer() }
                    .onSuccess {
                        notifyListeners("generationChanged", JSObject().put("reason", "network_available").put("epoch", epoch))
                    }
                    .onFailure {
                        AnyTTYDebugLog.e(TAG, "Go client engine could not follow Android network epoch", it)
                        notifyListeners("generationChangeFailed", JSObject().put("reason", "network_available").put("epoch", epoch))
                    }
            }
        }
    }
    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(receiverContext: Context?, intent: Intent?) {
            if (intent?.action == Intent.ACTION_SCREEN_OFF) suspendGoBridgeServer()
        }
    }

    override fun load() {
        AnyTTYDebugLog.init(context)
        startGoBridgeServer()
        ProcessLifecycleOwner.get().lifecycle.addObserver(this)
        context.registerReceiver(screenReceiver, IntentFilter(Intent.ACTION_SCREEN_OFF))
        activeNetwork = connectivityManager.activeNetwork
        connectivityManager.registerDefaultNetworkCallback(networkCallback)
        lifecycleReady = true
        Log.i(TAG, "NativeConnectionPlugin loaded")
        AnyTTYDebugLog.i(TAG, "NativeConnectionPlugin loaded")
    }

    override fun onStart(owner: LifecycleOwner) {
        if (!lifecycleReady) return
        // WebView 的 appStateChange 会调用 handleForegroundResume，并在新 bridge 就绪后原子替换 JS generation。
        // 这里不得抢先启动临时 bridge，否则旧 JS 请求可能连入随后立即被关闭的 generation。
    }

    override fun onStop(owner: LifecycleOwner) {
        if (!lifecycleReady) return
        suspendGoBridgeServer()
    }

    override fun handleOnDestroy() {
        lifecycleReady = false
        ProcessLifecycleOwner.get().lifecycle.removeObserver(this)
        runCatching { context.unregisterReceiver(screenReceiver) }
        runCatching { connectivityManager.unregisterNetworkCallback(networkCallback) }
        suspendGoBridgeServer()
        super.handleOnDestroy()
    }

    // ─── Connection Management ────────────────────────────────────────────────

    @PluginMethod
    fun handleForegroundResume(call: PluginCall) {
        try {
            // WebView 恢复时无条件切换 generation；冻结前的 JS handle 即使 socket 尚未观察到 close 也必须失效。
            restartGoBridgeServer()
            call.resolve()
        } catch (failure: Exception) {
            call.reject("Go client engine could not resume", failure)
        }
    }

    @PluginMethod
    fun resetLocalPairings(call: PluginCall) {
        runtimeScope.launch {
            try {
                resetLocalPairingState()
                call.resolve()
            } catch (failure: Exception) {
                AnyTTYDebugLog.e(TAG, "resetLocalPairings failed", failure)
                runCatching { restartGoBridgeServer() }
                call.reject("failed to reset local pairings", failure)
            }
        }
    }

    @PluginMethod
    fun getBridgeEndpoint(call: PluginCall) {
        if (bridgePort <= 0) {
            call.reject("native bridge server is not ready")
            return
        }
        val ret = JSObject()
        ret.put("port", bridgePort)
        ret.put("token", bridgeToken)
        call.resolve(ret)
    }

    @PluginMethod
    fun exportDebugLogs(call: PluginCall) {
        try {
            val archive: File = AnyTTYDebugLog.exportAndShare(activity)
            val ret = JSObject()
            ret.put("path", archive.absolutePath)
            ret.put("name", archive.name)
            ret.put("bytes", archive.length())
            call.resolve(ret)
        } catch (e: Exception) {
            AnyTTYDebugLog.e(TAG, "exportDebugLogs failed", e)
            call.reject("failed to export debug logs: ${e.message}", e)
        }
    }

    @PluginMethod
    fun writeDebugLog(call: PluginCall) {
        val level = call.getString("level") ?: "INFO"
        val tag = call.getString("tag") ?: "JS"
        val message = call.getString("message") ?: ""
        when (level.lowercase()) {
            "error" -> AnyTTYDebugLog.e(tag, message)
            "warn", "warning" -> AnyTTYDebugLog.w(tag, message)
            else -> AnyTTYDebugLog.i(tag, message)
        }
        call.resolve()
    }

    // ─── Bridge Server Setup ─────────────────────────────────────────────────

    @Synchronized
    private fun startGoBridgeServer() {
        bridgeToken = generateBridgeToken()
        val engine = AndroidGoClientEngine(context.applicationContext)
        val bridge = GoClientBridgeServer(engine, bridgeToken)
        goClientEngine = engine
        goBridgeServer = bridge
        try {
            bridge.start()
            if (!bridge.awaitStarted(5_000)) {
                throw IllegalStateException("Go binding bridge start timed out")
            }
            bridgePort = bridge.port
            Log.i(TAG, "Go binding bridge started on loopback port $bridgePort")
        } catch (failure: Exception) {
            bridgePort = 0
            runCatching { bridge.close() }
            goBridgeServer = null
            goClientEngine = null
            throw failure
        }
    }

    @Synchronized
    private fun restartGoBridgeServer() {
        suspendGoBridgeServer()
        startGoBridgeServer()
    }

    @Synchronized
    private fun resetLocalPairingState() {
        suspendGoBridgeServer()
        AndroidEndpointRegistryStore(context).clear()
        AndroidClientAccessCredentialStore(context).clearAll()
        AndroidSSHCredentialStore().clearAll()
        startGoBridgeServer()
    }

    @Synchronized
    private fun suspendGoBridgeServer() {
        bridgePort = 0
        goBridgeServer?.close()
        goBridgeServer = null
        goClientEngine = null
    }

    private fun generateBridgeToken(): String {
        val bytes = ByteArray(BRIDGE_TOKEN_BYTES)
        SecureRandom().nextBytes(bytes)
        return Base64.encodeToString(bytes, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)
    }

}
