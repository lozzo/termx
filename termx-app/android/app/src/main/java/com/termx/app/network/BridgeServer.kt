package com.termx.app.network

import android.util.Log
import org.java_websocket.WebSocket
import org.java_websocket.handshake.ClientHandshake
import org.java_websocket.server.WebSocketServer
import java.net.InetSocketAddress
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap

/**
 * BridgeServer — localhost WebSocket 数据面
 *
 * 帧格式：
 * ┌───────────┬──────────────┬──────────────┬──────────────────────────┐
 * │ frameType │ channelId    │ payloadLen   │ payload                  │
 * │ 1 byte    │ 2 bytes BE   │ 4 bytes BE   │ variable                 │
 * └───────────┴──────────────┴──────────────┴──────────────────────────┘
 */
class BridgeServer(port: Int) : WebSocketServer(InetSocketAddress("127.0.0.1", port)) {

    companion object {
        private const val TAG = "TermxBridgeServer"

        const val FRAME_DATA: Byte = 0x01
        const val FRAME_OPEN_CHAN: Byte = 0x02
        const val FRAME_CHAN_OPENED: Byte = 0x03
        const val FRAME_CLOSE_CHAN: Byte = 0x04
        const val FRAME_CHAN_ERROR: Byte = 0x05
        const val FRAME_STATE_UPDATE: Byte = 0x10
        const val FRAME_TRANSFER_SYNC: Byte = 0x11     // Native→JS
        const val FRAME_TRANSFER_REQUEST: Byte = 0x12  // JS→Native
        const val FRAME_SYNC_REQUEST: Byte = 0x22
        const val FRAME_SYNC_RESPONSE: Byte = 0x23

        const val CHAN_CONTROL = 0x0000
        const val CHAN_API = 0x0001
        const val CHAN_EVENTS = 0x0002
        const val CHAN_TERMINAL_BASE = 0x0100
        const val CHAN_FILE_BASE = 0x0200

        const val HEADER_SIZE = 7
    }

    interface FrameListener {
        fun onDataFrame(channelId: Int, payload: ByteArray)
        fun onOpenChannel(channelId: Int, label: String)
        fun onCloseChannel(channelId: Int, label: String?)
        fun onTransferRequest(payload: ByteArray) {}
        fun onSyncRequest() {}
    }

    @Volatile
    private var activeClient: WebSocket? = null

    private val channelLabels = ConcurrentHashMap<Int, String>()
    private val labelToChannel = ConcurrentHashMap<String, Int>()
    private var nextDynamicChannelId = 0x0010
    private var nextTerminalChannelId = CHAN_TERMINAL_BASE
    private var nextFileChannelId = CHAN_FILE_BASE

    var frameListener: FrameListener? = null
    var onClientConnectedCallback: Runnable? = null

    init {
        isReuseAddr = true
        connectionLostTimeout = 0
    }

    override fun onOpen(conn: WebSocket, handshake: ClientHandshake) {
        val old = activeClient
        if (old != null && old !== conn && old.isOpen) {
            old.close(1000, "replaced")
        }
        activeClient = conn
        resetChannels()
        Log.i(TAG, "JS client connected")
        onClientConnectedCallback?.run()
    }

    override fun onClose(conn: WebSocket, code: Int, reason: String?, remote: Boolean) {
        if (activeClient === conn) {
            activeClient = null
            resetChannels()
        }
        Log.i(TAG, "JS client disconnected code=$code")
    }

    override fun onMessage(conn: WebSocket, message: String) {}

    override fun onMessage(conn: WebSocket, buffer: ByteBuffer) {
        if (buffer.remaining() < HEADER_SIZE) return
        val frameType = buffer.get()
        val channelId = ((buffer.get().toInt() and 0xFF) shl 8) or (buffer.get().toInt() and 0xFF)
        val payloadLen = buffer.int
        if (buffer.remaining() < payloadLen) return
        val payload = ByteArray(payloadLen)
        buffer.get(payload)

        when (frameType) {
            FRAME_DATA -> frameListener?.onDataFrame(channelId, payload)
            FRAME_OPEN_CHAN -> handleOpenChannel(channelId, String(payload, StandardCharsets.UTF_8))
            FRAME_CLOSE_CHAN -> handleCloseChannel(channelId)
            FRAME_TRANSFER_REQUEST -> frameListener?.onTransferRequest(payload)
            FRAME_SYNC_REQUEST -> frameListener?.onSyncRequest()
            else -> Log.w(TAG, "Unknown frame 0x${Integer.toHexString(frameType.toInt())}")
        }
    }

    override fun onError(conn: WebSocket?, ex: Exception) {
        Log.e(TAG, "WS error", ex)
    }

    override fun onStart() {
        Log.i(TAG, "Bridge started on port $port")
    }

    private fun handleOpenChannel(requestedChannelId: Int, label: String) {
        val channelId = when {
            label.startsWith("api:") || label.startsWith("events:") ->
                labelToChannel[label] ?: nextDynamicChannelId++
            label.startsWith("terminal:") -> labelToChannel[label] ?: nextTerminalChannelId++
            label.startsWith("file:") -> labelToChannel[label] ?: nextFileChannelId++
            label == "api" -> CHAN_API
            label == "events" -> CHAN_EVENTS
            else -> {
                Log.w(TAG, "Unknown channel label: $label")
                sendToClient(buildFrame(FRAME_CHAN_ERROR, requestedChannelId,
                    "unknown label".toByteArray(StandardCharsets.UTF_8)))
                return
            }
        }
        channelLabels[channelId] = label
        labelToChannel[label] = channelId
        // CHAN_OPENED is sent by BridgeRouter/ChannelManager when the channel is ready
        frameListener?.onOpenChannel(channelId, label)
    }

    private fun handleCloseChannel(channelId: Int) {
        val label = channelLabels.remove(channelId)
        label?.let { labelToChannel.remove(it) }
        frameListener?.onCloseChannel(channelId, label)
    }

    fun sendDataFrame(channelId: Int, payload: ByteArray) {
        sendToClient(buildFrame(FRAME_DATA, channelId, payload))
    }

    fun sendChanOpened(channelId: Int, label: String) {
        sendToClient(buildFrame(FRAME_CHAN_OPENED, channelId, label.toByteArray(StandardCharsets.UTF_8)))
    }

    fun sendCloseChannel(channelId: Int) {
        sendToClient(buildFrame(FRAME_CLOSE_CHAN, channelId, ByteArray(0)))
    }

    fun sendChanError(channelId: Int, errorMessage: String) {
        sendToClient(buildFrame(FRAME_CHAN_ERROR, channelId,
            errorMessage.toByteArray(StandardCharsets.UTF_8)))
    }

    fun sendTransferSync(jsonPayload: String) {
        sendToClient(buildFrame(FRAME_TRANSFER_SYNC, CHAN_CONTROL,
            jsonPayload.toByteArray(StandardCharsets.UTF_8)))
    }

    fun sendStateUpdate(jsonPayload: String) {
        sendToClient(buildFrame(FRAME_STATE_UPDATE, CHAN_CONTROL,
            jsonPayload.toByteArray(StandardCharsets.UTF_8)))
    }

    fun sendSyncResponse(jsonPayload: String) {
        sendToClient(buildFrame(FRAME_SYNC_RESPONSE, CHAN_CONTROL,
            jsonPayload.toByteArray(StandardCharsets.UTF_8)))
    }

    private fun buildFrame(frameType: Byte, channelId: Int, payload: ByteArray): ByteBuffer {
        val buf = ByteBuffer.allocate(HEADER_SIZE + payload.size)
        buf.put(frameType)
        buf.putShort(channelId.toShort())
        buf.putInt(payload.size)
        buf.put(payload)
        buf.flip()
        return buf
    }

    private fun sendToClient(frame: ByteBuffer) {
        val client = activeClient
        if (client != null && client.isOpen) client.send(frame)
    }

    fun getChannelLabel(channelId: Int): String? = channelLabels[channelId]

    fun getChannelId(label: String): Int = labelToChannel[label] ?: -1

    fun resetChannels() {
        channelLabels.clear()
        labelToChannel.clear()
        nextDynamicChannelId = 0x0010
        nextTerminalChannelId = CHAN_TERMINAL_BASE
        nextFileChannelId = CHAN_FILE_BASE
    }

    fun resetChannelsForMachine(machineId: String) {
        val prefixes = listOf("terminal:$machineId:", "file:$machineId:")
        val toRemove = channelLabels.entries.filter { (_, label) ->
            prefixes.any { label.startsWith(it) }
        }
        for ((id, label) in toRemove) {
            channelLabels.remove(id)
            labelToChannel.remove(label)
        }
    }

    fun hasClients(): Boolean = activeClient?.isOpen == true
}
