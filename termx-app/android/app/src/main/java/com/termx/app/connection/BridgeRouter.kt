package com.termx.app.connection

import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.transfer.FileTransferManager
import org.json.JSONArray
import org.json.JSONObject
import org.webrtc.DataChannel
import java.nio.charset.StandardCharsets

/**
 * BridgeRouter — 将 JS→Native Bridge 帧路由到对应的 DataChannel
 *
 * Label 格式：
 *   api:{machineId}
 *   events:{machineId}
 *   terminal:{machineId}:{terminalId}
 *   file:{machineId}:{transferId}
 */
class BridgeRouter(
    private val bridge: BridgeServer,
    private val storeManager: ConnectionStoreManager,
    private val fileTransferManager: FileTransferManager,
) {
    companion object {
        private const val TAG = "TermxBridgeRouter"
    }

    private data class PendingChannel(val channelId: Int, val label: String)
    private val pendingChannels = mutableListOf<PendingChannel>()

    fun setup() {
        setupBridgeListener()
        setupPhaseListener()
        setupFileTransferSync()
    }

    private fun setupFileTransferSync() {
        fileTransferManager.syncListener = FileTransferManager.SyncListener { snapshots ->
            if (bridge.hasClients()) {
                bridge.sendTransferSync(snapshots.toString())
            }
        }
        bridge.onClientConnectedCallback = Runnable {
            synchronized(pendingChannels) { pendingChannels.clear() }
        }
    }

    private fun setupPhaseListener() {
        storeManager.onPhaseChanged = { machineId, phase ->
            when (phase) {
                "connected" -> replayPendingChannels(machineId)
                "failed" -> drainPendingChannels(machineId)
                else -> {}
            }
        }
    }

    private fun setupBridgeListener() {
        bridge.frameListener = object : BridgeServer.FrameListener {
            override fun onDataFrame(channelId: Int, payload: ByteArray) {
                val label = bridge.getChannelLabel(channelId) ?: run {
                    Log.w(TAG, "No label for channelId $channelId")
                    return
                }
                routeDataToTransport(label, payload)
            }

            override fun onOpenChannel(channelId: Int, label: String) {
                val machineId = extractMachineId(label) ?: run {
                    Log.w(TAG, "Cannot extract machineId from label: $label")
                    bridge.sendChanError(channelId, "invalid channel label")
                    return
                }
                if (label.startsWith("api:") || label.startsWith("events:")) {
                    bridge.sendChanOpened(channelId, label)
                    return
                }
                val transport = storeManager.findTransportForMachine(machineId)
                if (transport != null && transport.isConnected) {
                    openTransportChannel(transport, channelId, label, machineId)
                } else {
                    Log.i(TAG, "transport not ready for $machineId, queuing channel open: $label")
                    synchronized(pendingChannels) {
                        pendingChannels.add(PendingChannel(channelId, label))
                    }
                }
            }

            override fun onCloseChannel(channelId: Int, label: String?) {
                if (label == null) return
                val machineId = extractMachineId(label) ?: return
                val transport = storeManager.findTransportForMachine(machineId) ?: return
                when {
                    label.startsWith("terminal:") -> {
                        val terminalId = parseSubId(label) ?: return
                        transport.channelManager.closeTerminal(terminalId)
                    }
                    label.startsWith("file:") -> {
                        val transferId = parseSubId(label) ?: return
                        // Only close channel if native transfer manager isn't handling it
                        if (!fileTransferManager.isHandling(transferId)) {
                            transport.channelManager.closeFile(transferId)
                        }
                    }
                }
            }

            override fun onTransferRequest(payload: ByteArray) {
                handleTransferRequest(payload)
            }

            override fun onSyncRequest() {
                handleSyncRequest()
            }
        }
    }

    private fun handleSyncRequest() {
        try {
            val stores = JSONArray()
            for (machineId in storeManager.allMachineIds()) {
                val snapshot = storeManager.getSnapshot(machineId) ?: continue
                stores.put(snapshot)
            }
            val transferState = fileTransferManager.getTransferSnapshots()
            val resp = JSONObject()
                .put("stores", stores)
                .put("transfers", transferState.opt("transfers"))
            bridge.sendSyncResponse(resp.toString())
        } catch (e: Exception) {
            Log.e(TAG, "handleSyncRequest error", e)
        }
    }

    private fun findTransportForRequest(req: JSONObject): com.termx.app.transport.WebRTCTransport? {
        val machineId = req.optString("machine_id", "")
        if (machineId.isNotEmpty()) {
            return storeManager.findTransportForMachine(machineId)
        }
        return storeManager.findConnectedTransport()
    }

    private fun handleTransferRequest(payload: ByteArray) {
        try {
            val req = JSONObject(String(payload, StandardCharsets.UTF_8))
            val action = req.getString("action")
            Log.i(TAG, "handleTransferRequest: action=$action machine=${req.optString("machine_id", "")} transfer=${req.optString("transfer_id", "")}")
            when (action) {
                "start_download" -> {
                    val transport = findTransportForRequest(req)
                    if (transport != null) {
                        fileTransferManager.startDownload(
                            transport,
                            req.getString("transfer_id"),
                            req.getString("file_name"),
                            req.getLong("file_size"),
                            req.optString("file_path", ""),
                            req.optLong("offset", 0L),
                            req.optString("machine_id", ""),
                        )
                    } else {
                        Log.w(TAG, "handleTransferRequest: no connected transport for start_download")
                    }
                }
                "cancel_download" -> fileTransferManager.cancelDownload(req.getString("transfer_id"))
                "pause_download" -> fileTransferManager.pauseDownload(req.getString("transfer_id"))
                "resume_download" -> {
                    val transport = findTransportForRequest(req)
                    if (transport != null) {
                        fileTransferManager.resumeDownload(transport, req.getString("transfer_id"))
                    } else {
                        Log.w(TAG, "handleTransferRequest: no connected transport for resume_download")
                    }
                }
                "clear_transfer" -> fileTransferManager.clearTransfer(req.getString("transfer_id"))
                "resume_all" -> {
                    val machineId = req.optString("machine_id", "")
                    if (machineId.isNotEmpty()) {
                        fileTransferManager.resumeAllForMachine(
                            machineId,
                            storeManager.findTransportForMachine(machineId),
                        )
                    } else {
                        for (mid in fileTransferManager.transferMachineIds()) {
                            fileTransferManager.resumeAllForMachine(
                                mid,
                                storeManager.findTransportForMachine(mid),
                            )
                        }
                    }
                }
                "start_upload" -> {
                    val transport = findTransportForRequest(req)
                    if (transport != null) {
                        fileTransferManager.transportRef = transport
                        fileTransferManager.startUpload(
                            transport,
                            req.getString("content_uri"),
                            req.getString("file_name"),
                            req.getLong("file_size"),
                            req.optString("target_dir", "/"),
                            req.optString("machine_id", ""),
                        )
                    } else {
                        Log.w(TAG, "handleTransferRequest: no connected transport for start_upload")
                    }
                }
                "cancel_upload" -> fileTransferManager.cancelUpload(req.getString("transfer_id"))
                "pause_upload" -> fileTransferManager.pauseUpload(req.getString("transfer_id"))
                "resume_upload" -> {
                    val transport = findTransportForRequest(req)
                    if (transport != null) {
                        fileTransferManager.resumeUpload(transport, req.getString("transfer_id"))
                    } else {
                        Log.w(TAG, "handleTransferRequest: no connected transport for resume_upload")
                    }
                }
                "sync" -> bridge.sendTransferSync(fileTransferManager.getTransferSnapshots().toString())
                else -> Log.w(TAG, "handleTransferRequest: unknown action=${req.optString("action")}")
            }
        } catch (e: Exception) {
            Log.e(TAG, "handleTransferRequest error", e)
        }
    }

    private fun routeDataToTransport(label: String, payload: ByteArray) {
        val machineId = extractMachineId(label) ?: return
        val transport = storeManager.findTransportForMachine(machineId) ?: run {
            Log.w(TAG, "No transport for machine $machineId")
            return
        }
        when {
            label.startsWith("api:") -> transport.sendToApi(payload)
            label.startsWith("events:") -> transport.sendToEvents(payload)
            label.startsWith("terminal:") -> {
                val terminalId = parseSubId(label) ?: return
                transport.sendToTerminal(terminalId, payload)
            }
            label.startsWith("file:") -> {
                val transferId = parseSubId(label) ?: return
                transport.sendToFile(transferId, payload)
            }
        }
    }

    private fun openTransportChannel(
        transport: com.termx.app.transport.WebRTCTransport,
        channelId: Int,
        label: String,
        machineId: String,
    ) {
        if (extractMachineId(label) != machineId) {
            bridge.sendChanError(channelId, "machine mismatch")
            return
        }
        when {
            label.startsWith("api:") -> bridge.sendChanOpened(channelId, label)
            label.startsWith("events:") -> bridge.sendChanOpened(channelId, label)
            label.startsWith("terminal:") -> {
                val terminalId = parseSubId(label) ?: return
                val dc = transport.openTerminalChannel(terminalId)
                when (dc?.state()) {
                    DataChannel.State.OPEN -> bridge.sendChanOpened(channelId, label)
                    null -> bridge.sendChanError(channelId, "terminal channel unavailable")
                    else -> {}
                }
            }
            label.startsWith("file:") -> {
                val transferId = parseSubId(label) ?: return
                val dc = transport.openFileChannel(transferId)
                when (dc?.state()) {
                    DataChannel.State.OPEN -> bridge.sendChanOpened(channelId, label)
                    null -> bridge.sendChanError(channelId, "file channel unavailable")
                    else -> {}
                }
            }
        }
    }

    private fun replayPendingChannels(machineId: String) {
        val toReplay = synchronized(pendingChannels) {
            val list = pendingChannels.filter { extractMachineId(it.label) == machineId }
            pendingChannels.removeAll(list.toSet())
            list
        }
        if (toReplay.isEmpty()) return
        Log.i(TAG, "replaying ${toReplay.size} pending channels for $machineId")
        val transport = storeManager.findTransportForMachine(machineId) ?: return
        for (pending in toReplay) {
            openTransportChannel(transport, pending.channelId, pending.label, machineId)
        }
    }

    private fun drainPendingChannels(machineId: String) {
        val drained = synchronized(pendingChannels) {
            val list = pendingChannels.filter { extractMachineId(it.label) == machineId }
            pendingChannels.removeAll(list.toSet())
            list
        }
        for (pending in drained) {
            bridge.sendChanError(pending.channelId, "connection failed")
        }
    }

    // ─── Label parsing ────────────────────────────────────────────────────────

    private fun extractMachineId(label: String): String? {
        return when {
            label.startsWith("api:") -> label.removePrefix("api:")
            label.startsWith("events:") -> label.removePrefix("events:")
            label.startsWith("terminal:") || label.startsWith("file:") -> {
                val parts = label.split(":", limit = 3)
                if (parts.size >= 3) parts[1].takeIf { it.isNotBlank() } else null
            }
            else -> null
        }
    }

    private fun parseSubId(label: String): String? {
        val parts = label.split(":", limit = 3)
        return if (parts.size >= 3) parts[2] else null
    }

    fun closeAll() {
        synchronized(pendingChannels) { pendingChannels.clear() }
    }
}
