package com.termx.app.connection

import android.content.Context
import android.util.Log
import com.termx.app.connectors.ManagedWebRTCConnector
import com.termx.app.network.BridgeServer
import com.termx.app.network.NetworkStateManager
import com.termx.app.managed.RelayMode
import com.termx.app.transfer.FileTransferManager
import com.termx.app.transport.WebRTCTransport
import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap

class ConnectionStoreManager(
    private val context: Context,
    val bridge: BridgeServer?,
    private val managedConnector: ManagedWebRTCConnector = ManagedWebRTCConnector.community(),
) : NetworkStateManager.Listener {

    companion object {
        private const val TAG = "TermxStoreMgr"
    }

    val stores = ConcurrentHashMap<String, ConnectionStore>()

    var onStateChanged: ((machineId: String, snapshot: JSONObject) -> Unit)? = null
    var onPhaseChanged: ((machineId: String, phase: String) -> Unit)? = null
    var fileTransferManager: FileTransferManager? = null

    fun connect(
        endpointId: String,
        targetDeviceId: String,
        deviceFingerprint: String,
        grantRef: String,
        relayMode: RelayMode,
    ) {
        Log.i(TAG, "connect managed endpoint: $endpointId target=$targetDeviceId relayMode=${relayMode.wireName}")
        var store = stores[endpointId]
        if (store != null && !store.matchesConnectionInput(targetDeviceId, deviceFingerprint, grantRef, relayMode)) {
            Log.i(TAG, "recreating store after connection input changed: $endpointId")
            stores.remove(endpointId)?.release()
            store = null
        }
        if (store == null) {
            store = ConnectionStore(
                context = context,
                endpointId = endpointId,
                targetDeviceId = targetDeviceId,
                deviceFingerprint = deviceFingerprint,
                grantRef = grantRef,
                relayMode = relayMode,
                bridge = bridge,
                connector = managedConnector,
            ).apply {
                this.fileTransferManager = this@ConnectionStoreManager.fileTransferManager
                stateChangeListener = { mid, snapshot ->
                    onStateChanged?.invoke(mid, snapshot)
                    val phase = snapshot.optString("phase", "")
                    if (phase.isNotEmpty()) onPhaseChanged?.invoke(mid, phase)
                }
            }
            stores[endpointId] = store
        }
        store.connect()
    }

    fun retry(machineId: String) {
        val store = stores[machineId] ?: return
        store.retry()
    }

    fun release(machineId: String) {
        stores.remove(machineId)?.release()
    }

    fun releaseAll() {
        for (store in stores.values) store.release()
        stores.clear()
    }

    fun handleForegroundResume(backgroundDurationMs: Long) {
        for (store in stores.values) {
            store.handleForegroundResume(backgroundDurationMs, "App resumed")
        }
    }

    fun getSnapshot(machineId: String): JSONObject? =
        stores[machineId]?.getSnapshot()

    fun findTransportForMachine(machineId: String): WebRTCTransport? =
        stores[machineId]?.transport

    fun findConnectedTransport(): WebRTCTransport? =
        stores.values.firstOrNull { it.phase is ConnectionStore.Phase.Connected }?.transport

    fun allMachineIds(): Set<String> = stores.keys.toSet()

    fun getConnectionInfo(machineId: String?): JSONObject {
        val transport = if (machineId != null) findTransportForMachine(machineId)
        else findConnectedTransport()
        return transport?.getConnectionInfo() ?: JSONObject().put("type", "unknown")
    }

    // ─── NetworkStateManager.Listener ────────────────────────────────────────

    override fun onNetworkStateChange(
        current: NetworkStateManager.NetworkState,
        previous: NetworkStateManager.NetworkState,
    ) {
        for (store in stores.values) {
            store.onNetworkStateChange(current, previous)
        }
    }
}
