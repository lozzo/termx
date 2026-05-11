package com.termx.app.network

import android.util.Log
import org.java_websocket.WebSocket
import org.java_websocket.handshake.ClientHandshake
import org.java_websocket.server.WebSocketServer
import java.net.InetSocketAddress
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * BridgeServer — localhost WebSocket 数据面
 *
 * 帧格式：
 * ┌───────────┬──────────────┬──────────────┬──────────────────────────┐
 * │ frameType │ channelId    │ payloadLen   │ payload                  │
 * │ 1 byte    │ 2 bytes BE   │ 4 bytes BE   │ variable                 │
 * └───────────┴──────────────┴──────────────┴──────────────────────────┘
 *
 * 客户端连接后必须先发送 FRAME_AUTH，payload 为 NativeConnection plugin
 * 通过 Capacitor 返回的 bridge token。认证完成前不处理任何业务帧。
 */
class BridgeServer(port: Int, authToken: String) : WebSocketServer(InetSocketAddress("127.0.0.1", port)) {

    companion object {
        private const val TAG = "TermxBridgeServer"

        const val FRAME_DATA: Byte = 0x01
        const val FRAME_OPEN_CHAN: Byte = 0x02
        const val FRAME_CHAN_OPENED: Byte = 0x03
        const val FRAME_CLOSE_CHAN: Byte = 0x04
        const val FRAME_CHAN_ERROR: Byte = 0x05
        const val FRAME_AUTH: Byte = 0x06
        const val FRAME_AUTH_OK: Byte = 0x07
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
        private const val STATS_INTERVAL_MS = 1000L
        private const val LARGE_PAYLOAD_BYTES = 64 * 1024
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
    @Volatile
    private var currentAuthToken: String = authToken
    private val startLatch = CountDownLatch(1)
    private val authenticatedClients = ConcurrentHashMap.newKeySet<WebSocket>()

    private val channelLabels = ConcurrentHashMap<Int, String>()
    private val labelToChannel = ConcurrentHashMap<String, Int>()
    private val frameStats = ConcurrentHashMap<String, FrameStats>()
    private var nextDynamicChannelId = 0x0010
    private var nextTerminalChannelId = CHAN_TERMINAL_BASE
    private var nextFileChannelId = CHAN_FILE_BASE

    var frameListener: FrameListener? = null
    var onClientConnectedCallback: Runnable? = null

    private data class FrameStats(
        var rxFrames: Long = 0,
        var rxBytes: Long = 0,
        var txFrames: Long = 0,
        var txBytes: Long = 0,
        var lastLogAt: Long = System.currentTimeMillis(),
        var lastRxBytes: Long = 0,
        var lastTxBytes: Long = 0,
    )

    init {
        isReuseAddr = true
        connectionLostTimeout = 0
    }

    override fun onOpen(conn: WebSocket, handshake: ClientHandshake) {
        Log.i(TAG, "bridge client connected, awaiting auth")
    }

    override fun onClose(conn: WebSocket, code: Int, reason: String?, remote: Boolean) {
        authenticatedClients.remove(conn)
        if (activeClient === conn) {
            activeClient = null
            resetChannels()
        }
        Log.i(TAG, "JS client disconnected code=$code")
    }

    override fun onMessage(conn: WebSocket, message: String) {}

    override fun onMessage(conn: WebSocket, buffer: ByteBuffer) {
        if (buffer.remaining() < HEADER_SIZE) {
            Log.w(TAG, "short frame bytes=${buffer.remaining()}")
            return
        }
        val frameType = buffer.get()
        val channelId = ((buffer.get().toInt() and 0xFF) shl 8) or (buffer.get().toInt() and 0xFF)
        val payloadLen = buffer.int
        if (payloadLen < 0 || buffer.remaining() < payloadLen) {
            Log.w(TAG, "invalid frame type=0x${Integer.toHexString(frameType.toInt() and 0xff)} channel=$channelId payloadLen=$payloadLen remaining=${buffer.remaining()}")
            return
        }
        val payload = ByteArray(payloadLen)
        buffer.get(payload)

        if (!authenticatedClients.contains(conn)) {
            if (frameType == FRAME_AUTH && channelId == CHAN_CONTROL && verifyToken(payload)) {
                authenticateClient(conn)
            } else {
                Log.w(TAG, "rejecting unauthenticated bridge client")
                conn.send(buildFrame(FRAME_CHAN_ERROR, CHAN_CONTROL,
                    "bridge authentication failed".toByteArray(StandardCharsets.UTF_8)))
                conn.close(1008, "unauthorized")
            }
            return
        }

        if (activeClient !== conn) {
            conn.close(1000, "inactive")
            return
        }

        when (frameType) {
            FRAME_DATA -> {
                noteFrame("rx", channelLabels[channelId] ?: "chan:$channelId", channelId, payload.size)
                frameListener?.onDataFrame(channelId, payload)
            }
            FRAME_OPEN_CHAN -> handleOpenChannel(channelId, String(payload, StandardCharsets.UTF_8))
            FRAME_CLOSE_CHAN -> handleCloseChannel(channelId)
            FRAME_TRANSFER_REQUEST -> frameListener?.onTransferRequest(payload)
            FRAME_SYNC_REQUEST -> frameListener?.onSyncRequest()
            FRAME_AUTH -> sendToClient(buildFrame(FRAME_AUTH_OK, CHAN_CONTROL, ByteArray(0)))
            else -> Log.w(TAG, "Unknown frame 0x${Integer.toHexString(frameType.toInt())}")
        }
    }

    override fun onError(conn: WebSocket?, ex: Exception) {
        Log.e(TAG, "WS error", ex)
    }

    override fun onStart() {
        Log.i(TAG, "Bridge started on port $port")
        startLatch.countDown()
    }

    fun awaitStarted(timeoutMs: Long): Boolean =
        startLatch.await(timeoutMs, TimeUnit.MILLISECONDS)

    fun rotateAuthToken(token: String) {
        currentAuthToken = token
    }

    private fun authenticateClient(conn: WebSocket) {
        val old = activeClient
        if (old != null && old !== conn && old.isOpen) {
            authenticatedClients.remove(old)
            old.close(1000, "replaced")
        }
        activeClient = conn
        authenticatedClients.add(conn)
        resetChannels()
        conn.send(buildFrame(FRAME_AUTH_OK, CHAN_CONTROL, ByteArray(0)))
        Log.i(TAG, "JS bridge client authenticated")
        onClientConnectedCallback?.run()
    }

    private fun verifyToken(payload: ByteArray): Boolean {
        val expected = currentAuthToken.toByteArray(StandardCharsets.UTF_8)
        return MessageDigest.isEqual(payload, expected)
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
        Log.i(TAG, "open channel requested label=$label assigned=$channelId requested=$requestedChannelId")
        // CHAN_OPENED is sent by BridgeRouter/ChannelManager when the channel is ready
        frameListener?.onOpenChannel(channelId, label)
    }

    private fun handleCloseChannel(channelId: Int) {
        val label = channelLabels.remove(channelId)
        label?.let { labelToChannel.remove(it) }
        Log.i(TAG, "close channel requested channel=$channelId label=$label")
        frameListener?.onCloseChannel(channelId, label)
    }

    fun sendDataFrame(channelId: Int, payload: ByteArray) {
        noteFrame("tx", channelLabels[channelId] ?: "chan:$channelId", channelId, payload.size)
        sendToClient(buildFrame(FRAME_DATA, channelId, payload))
    }

    fun sendChanOpened(channelId: Int, label: String) {
        Log.i(TAG, "send channel opened label=$label channel=$channelId")
        sendToClient(buildFrame(FRAME_CHAN_OPENED, channelId, label.toByteArray(StandardCharsets.UTF_8)))
    }

    fun sendCloseChannel(channelId: Int) {
        Log.i(TAG, "send channel close channel=$channelId label=${channelLabels[channelId]}")
        sendToClient(buildFrame(FRAME_CLOSE_CHAN, channelId, ByteArray(0)))
    }

    fun sendChanError(channelId: Int, errorMessage: String) {
        Log.w(TAG, "send channel error channel=$channelId label=${channelLabels[channelId]} error=$errorMessage")
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
        if (client != null && client.isOpen) {
            client.send(frame)
        } else {
            Log.w(TAG, "sendToClient dropped: no active JS client bytes=${frame.remaining()}")
        }
    }

    private fun noteFrame(direction: String, label: String, channelId: Int, bytes: Int) {
        val key = "$channelId:$label"
        val stats = frameStats.getOrPut(key) { FrameStats() }
        val now = System.currentTimeMillis()
        synchronized(stats) {
            if (direction == "rx") {
                stats.rxFrames += 1
                stats.rxBytes += bytes.toLong()
            } else {
                stats.txFrames += 1
                stats.txBytes += bytes.toLong()
            }
            if (bytes >= LARGE_PAYLOAD_BYTES) {
                Log.w(TAG, "large payload direction=$direction label=$label channel=$channelId bytes=$bytes rxBytes=${stats.rxBytes} txBytes=${stats.txBytes}")
            }
            if (now - stats.lastLogAt < STATS_INTERVAL_MS) return
            val elapsed = ((now - stats.lastLogAt).coerceAtLeast(1)).toDouble() / 1000.0
            val intervalRx = stats.rxBytes - stats.lastRxBytes
            val intervalTx = stats.txBytes - stats.lastTxBytes
            Log.i(TAG, "bridge stats label=$label channel=$channelId rxFrames=${stats.rxFrames} rxBytes=${stats.rxBytes} txFrames=${stats.txFrames} txBytes=${stats.txBytes} rxBps=${(intervalRx / elapsed).toLong()} txBps=${(intervalTx / elapsed).toLong()}")
            stats.lastLogAt = now
            stats.lastRxBytes = stats.rxBytes
            stats.lastTxBytes = stats.txBytes
        }
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
        val toRemove = channelLabels.entries.filter { (_, label) ->
            label == "api:$machineId" ||
                label == "events:$machineId" ||
                label.startsWith("terminal:$machineId:") ||
                label.startsWith("file:$machineId:")
        }
        for ((id, label) in toRemove) {
            sendCloseChannel(id)
            channelLabels.remove(id)
            labelToChannel.remove(label)
        }
    }

    fun hasClients(): Boolean = activeClient?.isOpen == true
}
