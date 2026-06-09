package com.termx.app

import android.util.Log
import android.util.Base64
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import com.termx.app.connection.BridgeRouter
import com.termx.app.connection.ConnectionStoreManager
import com.termx.app.network.BridgeServer
import com.termx.app.network.NetworkStateManager
import com.termx.app.transfer.FileTransferManager
import com.termx.app.transport.WebRTCTransport
import org.json.JSONArray
import org.json.JSONObject
import java.security.SecureRandom
import java.io.File

@CapacitorPlugin(name = "NativeConnection")
class NativeConnectionPlugin : Plugin() {

    companion object {
        private const val TAG = "TermxNativePlugin"
        private const val BRIDGE_TOKEN_BYTES = 32
    }

    private var bridgeServer: BridgeServer? = null
    private var bridgePort: Int = 0
    private var bridgeToken: String = ""
    private var storeManager: ConnectionStoreManager? = null
    private var networkStateManager: NetworkStateManager? = null
    private var bridgeRouter: BridgeRouter? = null
    private var fileTransferManager: FileTransferManager? = null

    override fun load() {
        TermxDebugLog.init(context)
        WebRTCTransport.initFactory(context)
        startBridgeServer()
        Log.i(TAG, "NativeConnectionPlugin loaded")
        TermxDebugLog.i(TAG, "NativeConnectionPlugin loaded")
    }

    // ─── Connection Management ────────────────────────────────────────────────

    @PluginMethod
    fun connect(call: PluginCall) {
        val machineId = call.getString("machineId") ?: run {
            call.reject("machineId required"); return
        }
        val sessionToken = call.getString("sessionToken") ?: run {
            call.reject("sessionToken required"); return
        }
        val localAddresses = call.getArray("localAddresses")?.let { arr ->
            (0 until arr.length()).mapNotNull { arr.optString(it).takeIf { s -> s.isNotBlank() } }
        } ?: emptyList()

        val hubUrls = call.getArray("hubUrls")?.let { arr ->
            (0 until arr.length()).mapNotNull { arr.optString(it).takeIf { s -> s.isNotBlank() } }
        } ?: emptyList()

        val answerProofSecret = call.getString("answerProofSecret")
        val forceRelay = if (call.data.has("forceRelay")) {
            call.getBoolean("forceRelay", false) ?: false
        } else {
            null
        }

        storeManager?.connect(
            machineId = machineId,
            localAddresses = localAddresses,
            hubUrls = hubUrls,
            sessionToken = sessionToken,
            answerProofSecret = answerProofSecret,
            forceRelay = forceRelay,
        )
        call.resolve()
    }

    @PluginMethod
    fun retry(call: PluginCall) {
        val machineId = call.getString("machineId") ?: run {
            call.reject("machineId required"); return
        }
        val forceRelay = if (call.data.has("forceRelay")) {
            call.getBoolean("forceRelay", false) ?: false
        } else {
            null
        }
        storeManager?.retry(machineId, forceRelay)
        call.resolve()
    }

    @PluginMethod
    fun release(call: PluginCall) {
        val machineId = call.getString("machineId") ?: run {
            call.reject("machineId required"); return
        }
        storeManager?.release(machineId)
        call.resolve()
    }

    @PluginMethod
    fun releaseAll(call: PluginCall) {
        storeManager?.releaseAll()
        call.resolve()
    }

    @PluginMethod
    fun handleForegroundResume(call: PluginCall) {
        val durationMs = call.getDouble("backgroundDurationMs")?.toLong() ?: 0L
        storeManager?.handleForegroundResume(durationMs)
        call.resolve()
    }

    @PluginMethod
    fun getBridgeEndpoint(call: PluginCall) {
        if (bridgePort <= 0) {
            call.reject("native bridge server is not ready")
            return
        }
        bridgeToken = generateBridgeToken()
        bridgeServer?.rotateAuthToken(bridgeToken)
        val ret = JSObject()
        ret.put("port", bridgePort)
        ret.put("token", bridgeToken)
        call.resolve(ret)
    }

    @PluginMethod
    fun getSnapshot(call: PluginCall) {
        val machineId = call.getString("machineId") ?: run {
            call.reject("machineId required"); return
        }
        val snapshot = storeManager?.getSnapshot(machineId)
        if (snapshot == null) {
            call.reject("no snapshot for $machineId"); return
        }
        val ret = snapshotToJSObject(snapshot)
        call.resolve(ret)
    }

    @PluginMethod
    fun getConnectionInfo(call: PluginCall) {
        val machineId = call.getString("machineId")
        val info = storeManager?.getConnectionInfo(machineId) ?: JSONObject()
        val ret = JSObject()
        ret.put("type", info.optString("type", "unknown"))
        ret.put("relayInUse", info.optBoolean("relayInUse", false))
        info.optString("localAddr").takeIf { it.isNotBlank() }?.let { ret.put("localAddr", it) }
        info.optString("remoteAddr").takeIf { it.isNotBlank() }?.let { ret.put("remoteAddr", it) }
        info.optString("candidateType").takeIf { it.isNotBlank() }?.let { ret.put("candidateType", it) }
        info.optString("remoteCandidateType").takeIf { it.isNotBlank() }?.let { ret.put("remoteCandidateType", it) }
        info.optLong("rtt").takeIf { it > 0 }?.let { ret.put("rtt", it) }
        call.resolve(ret)
    }

    @PluginMethod
    fun getDownloadResumeOffset(call: PluginCall) {
        val machineId = call.getString("machineId") ?: run {
            call.reject("machineId required"); return
        }
        val filePath = call.getString("filePath") ?: run {
            call.reject("filePath required"); return
        }
        val fileSize = call.getDouble("fileSize")?.toLong() ?: 0L
        val offset = fileTransferManager?.getDownloadResumeOffset(machineId, filePath, fileSize) ?: 0L
        val ret = JSObject()
        ret.put("offset", offset)
        call.resolve(ret)
    }

    @PluginMethod
    fun getTransferSnapshot(call: PluginCall) {
        val snapshot = fileTransferManager?.getTransferSnapshots() ?: JSONObject().put("transfers", JSONArray())
        call.resolve(JSObject.fromJSONObject(snapshot))
    }

    @PluginMethod
    fun clearTransfer(call: PluginCall) {
        val transferId = call.getString("transferId") ?: call.getString("transfer_id") ?: run {
            call.reject("transferId required"); return
        }
        fileTransferManager?.clearTransfer(transferId)
        call.resolve()
    }

    @PluginMethod
    fun resumeAllTransfers(call: PluginCall) {
        val machineId = call.getString("machineId") ?: call.getString("machine_id")
        if (!machineId.isNullOrBlank()) {
            fileTransferManager?.resumeAllForMachine(machineId, storeManager?.findTransportForMachine(machineId))
        } else {
            val machines = fileTransferManager?.transferMachineIds() ?: emptySet()
            for (mid in machines) {
                fileTransferManager?.resumeAllForMachine(mid, storeManager?.findTransportForMachine(mid))
            }
        }
        call.resolve()
    }

    @PluginMethod
    fun exportDebugLogs(call: PluginCall) {
        try {
            val archive: File = TermxDebugLog.exportAndShare(activity)
            val ret = JSObject()
            ret.put("path", archive.absolutePath)
            ret.put("name", archive.name)
            ret.put("bytes", archive.length())
            call.resolve(ret)
        } catch (e: Exception) {
            TermxDebugLog.e(TAG, "exportDebugLogs failed", e)
            call.reject("failed to export debug logs: ${e.message}", e)
        }
    }

    @PluginMethod
    fun writeDebugLog(call: PluginCall) {
        val level = call.getString("level") ?: "INFO"
        val tag = call.getString("tag") ?: "JS"
        val message = call.getString("message") ?: ""
        when (level.lowercase()) {
            "error" -> TermxDebugLog.e(tag, message)
            "warn", "warning" -> TermxDebugLog.w(tag, message)
            else -> TermxDebugLog.i(tag, message)
        }
        call.resolve()
    }

    // ─── Bridge Server Setup ─────────────────────────────────────────────────

    private fun startBridgeServer() {
        bridgeToken = generateBridgeToken()
        val bridge = BridgeServer(0, bridgeToken)
        bridgeServer = bridge

        val manager = ConnectionStoreManager(context, bridge)
        storeManager = manager

        val ftm = FileTransferManager(context)
        fileTransferManager = ftm
        manager.fileTransferManager = ftm

        val router = BridgeRouter(bridge, manager, ftm)
        bridgeRouter = router
        router.setup()

        manager.onStateChanged = { machineId, snapshot ->
            notifyStateChange(machineId, snapshot)
        }

        val netManager = NetworkStateManager(context)
        networkStateManager = netManager
        netManager.listener = manager
        netManager.init()

        try {
            bridge.start()
            if (!bridge.awaitStarted(5000)) {
                throw IllegalStateException("BridgeServer start timed out")
            }
            bridgePort = bridge.port
            Log.i(TAG, "BridgeServer started on random loopback port $bridgePort")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to start BridgeServer", e)
        }
    }

    private fun generateBridgeToken(): String {
        val bytes = ByteArray(BRIDGE_TOKEN_BYTES)
        SecureRandom().nextBytes(bytes)
        return Base64.encodeToString(bytes, Base64.URL_SAFE or Base64.NO_PADDING or Base64.NO_WRAP)
    }

    private fun notifyStateChange(@Suppress("UNUSED_PARAMETER") machineId: String, snapshot: JSONObject) {
        bridgeServer?.sendStateUpdate(snapshot.toString())
        val data = snapshotToJSObject(snapshot)
        notifyListeners("stateChange", data)
    }

    private fun snapshotToJSObject(snapshot: JSONObject): JSObject {
        val ret = JSObject()
        ret.put("machineId", snapshot.optString("machineId"))
        ret.put("phase", snapshot.optString("phase"))
        ret.put("statusText", snapshot.optString("statusText"))
        val path = snapshot.opt("path")
        if (path != null && path != JSONObject.NULL) ret.put("path", path.toString())
        ret.put("relayInUse", snapshot.optBoolean("relayInUse", false))
        ret.put("forceRelay", snapshot.optBoolean("forceRelay", false))
        ret.put("version", snapshot.optLong("version", 0L))
        val failReason = snapshot.opt("failReason")
        if (failReason != null && failReason != JSONObject.NULL) ret.put("failReason", failReason.toString())
        return ret
    }
}
