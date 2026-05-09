package com.termx.app.connection

import android.content.Context
import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.network.NetworkStateManager
import com.termx.app.transfer.FileTransferManager
import com.termx.app.transport.WebRTCTransport
import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap

class ConnectionStoreManager(
    private val context: Context,
    val bridge: BridgeServer?,
) : NetworkStateManager.Listener {

    companion object {
        private const val TAG = "TermxStoreMgr"
    }

    val stores = ConcurrentHashMap<String, ConnectionStore>()

    var onStateChanged: ((machineId: String, snapshot: JSONObject) -> Unit)? = null
    var onPhaseChanged: ((machineId: String, phase: String) -> Unit)? = null
    var fileTransferManager: FileTransferManager? = null

    fun connect(
        machineId: String,
        localAddresses: List<String>,
        hubUrls: List<String>,
        sessionToken: String,
        answerProofSecret: String?,
        preferredPath: String,
        forceRelay: Boolean?,
    ) {
        Log.i(TAG, "connect: $machineId addresses=${localAddresses.size} hubs=${hubUrls.size} forceRelay=$forceRelay")
        var store = stores[machineId]
        if (store == null) {
            store = ConnectionStore(
                context = context,
                machineId = machineId,
                localAddresses = localAddresses,
                hubUrls = hubUrls,
                sessionToken = sessionToken,
                answerProofSecret = answerProofSecret,
                preferredPath = preferredPath,
                forceRelay = forceRelay == true,
                bridge = bridge,
            ).apply {
                this.fileTransferManager = this@ConnectionStoreManager.fileTransferManager
                stateChangeListener = { mid, snapshot ->
                    onStateChanged?.invoke(mid, snapshot)
                    val phase = snapshot.optString("phase", "")
                    if (phase.isNotEmpty()) onPhaseChanged?.invoke(mid, phase)
                }
            }
            stores[machineId] = store
        } else {
            val forceRelayChanged = forceRelay?.let { store.updateForceRelay(it) } == true
            if (forceRelayChanged &&
                (store.phase is ConnectionStore.Phase.Connected || store.phase is ConnectionStore.Phase.Verifying)) {
                store.retry()
                return
            }
        }
        store.connect()
    }

    fun retry(machineId: String, forceRelay: Boolean? = null) {
        val store = stores[machineId] ?: return
        if (forceRelay != null) store.setForceRelay(forceRelay)
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
