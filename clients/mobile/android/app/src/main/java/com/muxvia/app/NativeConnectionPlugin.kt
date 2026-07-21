package com.muxvia.app

import android.os.Build
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
import com.muxvia.app.managed.ManagedCloudAssembly
import com.muxvia.app.managed.ManagedCloudAccount
import com.muxvia.app.managed.ManagedCloudClientMetadata
import com.muxvia.app.managed.ManagedCloudLoginFlow
import com.muxvia.app.managed.ManagedEndpointFailure
import com.muxvia.app.goclient.AndroidGoClientEngine
import com.muxvia.app.goclient.GoClientBridgeServer
import org.json.JSONArray
import org.json.JSONObject
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
        private const val TAG = "MuxviaNativePlugin"
        private const val BRIDGE_TOKEN_BYTES = 32
    }

    private var bridgePort: Int = 0
    private var bridgeToken: String = ""
    private var goClientEngine: AndroidGoClientEngine? = null
    private var goBridgeServer: GoClientBridgeServer? = null
    private val cloudAdapter by lazy { ManagedCloudAssembly.create(context) }
    private val cloudScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val cloudLoginLock = Any()
    private var activeCloudLoginFlow: ManagedCloudLoginFlow? = null
    private var lifecycleReady = false
    private val connectivityManager by lazy { context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager }
    @Volatile private var activeNetwork: Network? = null
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onLost(network: Network) {
            if (activeNetwork != network) return
            activeNetwork = null
            suspendGoBridgeServer()
        }

        override fun onAvailable(network: Network) {
            val previous = activeNetwork
            activeNetwork = network
            if (previous == network || !lifecycleReady) return
            if (!ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) return
            runCatching { restartGoBridgeServer() }
                .onSuccess {
                    notifyListeners("generationChanged", JSObject().put("reason", "network_available"))
                }
                .onFailure { MuxviaDebugLog.e(TAG, "Go client engine could not follow Android network epoch", it) }
        }
    }
    private val screenReceiver = object : BroadcastReceiver() {
        override fun onReceive(receiverContext: Context?, intent: Intent?) {
            if (intent?.action == Intent.ACTION_SCREEN_OFF) suspendGoBridgeServer()
        }
    }

    override fun load() {
        MuxviaDebugLog.init(context)
        startGoBridgeServer()
        ProcessLifecycleOwner.get().lifecycle.addObserver(this)
        context.registerReceiver(screenReceiver, IntentFilter(Intent.ACTION_SCREEN_OFF))
        activeNetwork = connectivityManager.activeNetwork
        connectivityManager.registerDefaultNetworkCallback(networkCallback)
        lifecycleReady = true
        Log.i(TAG, "NativeConnectionPlugin loaded")
        MuxviaDebugLog.i(TAG, "NativeConnectionPlugin loaded")
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

    /** cloudBeginActivation 创建 App 可展示的短码；高熵 flow ID 只保存在原生内存。 */
    @PluginMethod
    fun cloudBeginActivation(call: PluginCall) {
        cloudScope.launch {
            try {
                val flow = cloudAdapter.beginLogin(cloudClientMetadata())
                synchronized(cloudLoginLock) { activeCloudLoginFlow = flow }
                call.resolve(flowToJSObject(flow))
            } catch (failure: ManagedEndpointFailure) {
                call.reject(failure.message ?: failure.code, failure.code)
            } catch (_: Exception) {
                call.reject("cloud activation could not start", "temporary")
            }
        }
    }

    /** cloudClaimActivation 认领 Web 二维码 locator；二维码不包含 flow ID 或账号 Session。 */
    @PluginMethod
    fun cloudClaimActivation(call: PluginCall) {
        val rawPayload = call.getString("payload")?.trim().orEmpty()
        val userCode = rawPayload.removePrefix("muxvia-cloud-activate:v1:").trim().uppercase()
        if (userCode.isBlank() || !userCode.matches(Regex("[23456789ABCDEFGHJKMNPQRSTVWXYZ]{5}-[23456789ABCDEFGHJKMNPQRSTVWXYZ]{5}"))) {
            call.reject("This is not a Muxvia Cloud activation code", "protocol")
            return
        }
        cloudScope.launch {
            try {
                val flow = cloudAdapter.claimLogin(userCode, cloudClientMetadata())
                synchronized(cloudLoginLock) { activeCloudLoginFlow = flow }
                call.resolve(flowToJSObject(flow))
            } catch (failure: ManagedEndpointFailure) {
                call.reject(failure.message ?: failure.code, failure.code)
            } catch (failure: Exception) {
                Log.e(TAG, "Cloud activation claim failed unexpectedly", failure)
                MuxviaDebugLog.e(TAG, "Cloud activation claim failed unexpectedly", failure)
                call.reject("Muxvia Cloud activation failed unexpectedly. Export debug logs and try again.", "temporary")
            }
        }
    }

    /** cloudAwaitActivation 轮询当前原生 Flow，批准后把 edge session 直接写入 Keystore。 */
    @PluginMethod
    fun cloudAwaitActivation(call: PluginCall) {
        val flow = synchronized(cloudLoginLock) { activeCloudLoginFlow }
        if (flow == null) {
            call.reject("No cloud activation is active", "login_required")
            return
        }
        cloudScope.launch {
            while (System.currentTimeMillis() / 1000 < flow.expiresAtUnix) {
                if (synchronized(cloudLoginLock) { activeCloudLoginFlow?.flowId } != flow.flowId) {
                    call.reject("cloud activation was cancelled", "cancelled")
                    return@launch
                }
                try {
                    val account = cloudAdapter.completeLogin(flow.flowId)
                    synchronized(cloudLoginLock) {
                        if (activeCloudLoginFlow?.flowId == flow.flowId) activeCloudLoginFlow = null
                    }
                    call.resolve(accountToJSObject(account))
                    return@launch
                } catch (failure: ManagedEndpointFailure) {
                    if (failure.code != "temporary") {
                        synchronized(cloudLoginLock) {
                            if (activeCloudLoginFlow?.flowId == flow.flowId) activeCloudLoginFlow = null
                        }
                        call.reject(failure.message ?: failure.code, failure.code)
                        return@launch
                    }
                } catch (_: Exception) {
                    call.reject("cloud activation failed", "temporary")
                    return@launch
                }
                delay(flow.pollIntervalMillis.coerceIn(250, 60_000))
            }
            synchronized(cloudLoginLock) {
                if (activeCloudLoginFlow?.flowId == flow.flowId) activeCloudLoginFlow = null
            }
            call.reject("cloud activation expired", "login_required")
        }
    }

    /** cloudCancelActivation 只丢弃当前短期 Flow，不影响既有账号 Session。 */
    @PluginMethod
    fun cloudCancelActivation(call: PluginCall) {
        synchronized(cloudLoginLock) { activeCloudLoginFlow = null }
        call.resolve()
    }

    /** getCloudAccount 返回 Keystore 中仍有效的账号摘要。 */
    @PluginMethod
    fun getCloudAccount(call: PluginCall) {
        cloudScope.launch {
            try {
                val account = cloudAdapter.currentAccount()
                call.resolve(account?.let(::accountToJSObject) ?: JSObject())
            } catch (failure: Exception) {
                call.reject(failure.message ?: "cloud account is unavailable")
            }
        }
    }

    /** cloudListDevices 返回账号目录 metadata；是否已配对仍由独立 native grant store 决定。 */
    @PluginMethod
    fun cloudListDevices(call: PluginCall) {
        cloudScope.launch {
            try {
                val devices = JSONArray()
                cloudAdapter.listDevices().forEach { device ->
                    devices.put(JSONObject()
                        .put("deviceId", device.deviceId)
                        .put("deviceFingerprint", device.deviceFingerprint)
                        .put("displayName", device.displayName)
                        .put("platform", device.platform)
                        .put("kind", device.kind)
                        .put("online", device.online)
                        .put("revoked", device.revoked))
                }
                call.resolve(JSObject().put("devices", devices))
            } catch (failure: ManagedEndpointFailure) {
                if (failure.code == "unauthenticated") {
                    // Hub 的设备投影是客户端 Cloud 准入真值；被 Web 移除后必须清除本机账号会话。
                    runCatching { cloudAdapter.logout() }
                }
                call.reject(failure.message ?: failure.code, failure.code)
            } catch (failure: Exception) {
                MuxviaDebugLog.e(TAG, "Cloud device directory failed", failure)
                call.reject("Muxvia Cloud device directory is unavailable", "temporary")
            }
        }
    }

    /** cloudLogout 只清理账号 edge session，保留独立 pairing grant。 */
    @PluginMethod
    fun cloudLogout(call: PluginCall) {
        cloudScope.launch {
            try {
                cloudAdapter.logout()
                call.resolve()
            } catch (failure: Exception) {
                call.reject(failure.message ?: "cloud logout failed")
            }
        }
    }

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
            val archive: File = MuxviaDebugLog.exportAndShare(activity)
            val ret = JSObject()
            ret.put("path", archive.absolutePath)
            ret.put("name", archive.name)
            ret.put("bytes", archive.length())
            call.resolve(ret)
        } catch (e: Exception) {
            MuxviaDebugLog.e(TAG, "exportDebugLogs failed", e)
            call.reject("failed to export debug logs: ${e.message}", e)
        }
    }

    @PluginMethod
    fun writeDebugLog(call: PluginCall) {
        val level = call.getString("level") ?: "INFO"
        val tag = call.getString("tag") ?: "JS"
        val message = call.getString("message") ?: ""
        when (level.lowercase()) {
            "error" -> MuxviaDebugLog.e(tag, message)
            "warn", "warning" -> MuxviaDebugLog.w(tag, message)
            else -> MuxviaDebugLog.i(tag, message)
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
    private fun suspendGoBridgeServer() {
        bridgePort = 0
        goBridgeServer?.close()
        goBridgeServer = null
        goClientEngine = null
    }

    private fun accountToJSObject(account: ManagedCloudAccount): JSObject = JSObject().apply {
        put("accountId", account.accountId)
        put("accountLabel", account.accountLabel)
        put("expiresAtUnix", account.expiresAtUnix)
    }

    private fun flowToJSObject(flow: ManagedCloudLoginFlow): JSObject = JSObject().apply {
        put("userCode", flow.userCode)
        put("expiresAtUnix", flow.expiresAtUnix)
    }

    private fun cloudClientMetadata(): ManagedCloudClientMetadata {
        val manufacturer = Build.MANUFACTURER.trim()
        val model = Build.MODEL.trim()
        val displayName = listOf(manufacturer, model).filter(String::isNotBlank).joinToString(" ").ifBlank { "Android device" }
        return ManagedCloudClientMetadata(displayName, "android", BuildConfig.VERSION_NAME)
    }

    private fun generateBridgeToken(): String {
        val bytes = ByteArray(BRIDGE_TOKEN_BYTES)
        SecureRandom().nextBytes(bytes)
        return Base64.encodeToString(bytes, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)
    }

}
