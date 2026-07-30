package com.anytty.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.Network
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
import java.util.concurrent.CancellationException
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner

@CapacitorPlugin(name = "NativeConnection")
class NativeConnectionPlugin : Plugin(), DefaultLifecycleObserver {

    companion object {
        private const val BRIDGE_TOKEN_BYTES = 32
    }

    private var bridgePort: Int = 0
    private var bridgeToken: String = ""
    private var goClientEngine: AndroidGoClientEngine? = null
    private var goBridgeServer: GoClientBridgeServer? = null
    private val runtimeScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val connectivityManager by lazy { context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager }
    private val runtimeCoordinator = NativeConnectionRuntimeCoordinator(
        scheduleNetworkRestart = { delayMillis, task ->
            val job = runtimeScope.launch {
                delay(delayMillis)
                task()
            }
            PendingRuntimeWork { job.cancel() }
        },
        isApplicationStarted = {
            ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)
        },
        startRuntime = ::startGoBridgeServer,
        restartRuntime = ::restartGoBridgeServer,
        suspendRuntime = ::suspendGoBridgeServer,
        resetRuntime = ::resetLocalPairingState,
        generationChanging = { reason, epoch ->
            notifyListeners("generationChanging", JSObject().put("reason", reason).put("epoch", epoch))
        },
        generationChanged = { reason, epoch ->
            notifyListeners("generationChanged", JSObject().put("reason", reason).put("epoch", epoch))
        },
        generationChangeFailed = { reason, epoch, _ ->
            AnyTTYDebugLog.event(AnyTTYDebugEvent.GENERATION_CHANGE_FAILED)
            notifyListeners("generationChangeFailed", JSObject().put("reason", reason).put("epoch", epoch))
        },
    )
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onLost(network: Network) {
            runtimeCoordinator.onNetworkLost(network)
        }

        override fun onAvailable(network: Network) {
            runtimeCoordinator.onNetworkAvailable(network)
        }
    }
    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(receiverContext: Context?, intent: Intent?) {
            if (intent?.action == Intent.ACTION_SCREEN_OFF) runtimeCoordinator.suspendForLifecycle()
        }
    }

    override fun load() {
        runtimeCoordinator.load(connectivityManager.activeNetwork)
        ProcessLifecycleOwner.get().lifecycle.addObserver(this)
        context.registerReceiver(screenReceiver, IntentFilter(Intent.ACTION_SCREEN_OFF))
        connectivityManager.registerDefaultNetworkCallback(networkCallback)
        AnyTTYDebugLog.event(AnyTTYDebugEvent.CONNECTION_PLUGIN_LOADED)
    }

    override fun onStart(owner: LifecycleOwner) {
        if (!runtimeCoordinator.isReady()) return
        // WebView 的 appStateChange 会调用 handleForegroundResume，并在新 bridge 就绪后原子替换 JS generation。
        // 这里不得抢先启动临时 bridge，否则旧 JS 请求可能连入随后立即被关闭的 generation。
    }

    override fun onStop(owner: LifecycleOwner) {
        runtimeCoordinator.suspendForLifecycle()
    }

    override fun handleOnDestroy() {
        runtimeCoordinator.destroy()
        ProcessLifecycleOwner.get().lifecycle.removeObserver(this)
        runCatching { context.unregisterReceiver(screenReceiver) }
        runCatching { connectivityManager.unregisterNetworkCallback(networkCallback) }
        runtimeScope.cancel("NativeConnectionPlugin destroyed")
        super.handleOnDestroy()
    }

    // ─── Connection Management ────────────────────────────────────────────────

    @PluginMethod
    fun handleForegroundResume(call: PluginCall) {
        try {
            // WebView 恢复时无条件切换 generation；冻结前的 JS handle 即使 socket 尚未观察到 close 也必须失效。
            runtimeCoordinator.restartForForeground()
            call.resolve()
        } catch (failure: Exception) {
            call.reject("Go client engine could not resume", failure)
        }
    }

    @PluginMethod
    fun resetLocalPairings(call: PluginCall) {
        if (!runtimeCoordinator.isReady()) {
            call.reject("native runtime is not available")
            return
        }
        val settled = AtomicBoolean(false)
        val reset = runtimeScope.launch {
            try {
                runtimeCoordinator.resetLocalPairings()
                if (settled.compareAndSet(false, true)) call.resolve()
            } catch (failure: Exception) {
                AnyTTYDebugLog.event(AnyTTYDebugEvent.RESET_PAIRINGS_FAILED)
                runCatching { runtimeCoordinator.restartForForeground() }
                if (settled.compareAndSet(false, true)) call.reject("failed to reset local pairings", failure)
            }
        }
        reset.invokeOnCompletion { failure ->
            if (failure is CancellationException && settled.compareAndSet(false, true)) {
                call.reject("native runtime was destroyed during reset", failure)
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
            AnyTTYDebugLog.event(AnyTTYDebugEvent.BRIDGE_STARTED)
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
