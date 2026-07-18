package com.termx.app.goclient

import org.java_websocket.WebSocket
import org.java_websocket.handshake.ClientHandshake
import org.java_websocket.server.WebSocketServer
import java.net.InetSocketAddress
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * GoClientBridgeServer 是 WebView 到 Go binding 的唯一二进制数据面。
 * 它只解释固定 operation header 和 opaque Proto bytes，不解析任何 terminal/history/file/storage 字段。
 */
class GoClientBridgeServer(
    private val engine: AndroidGoClientEngine,
    authToken: String,
) : WebSocketServer(InetSocketAddress("127.0.0.1", 0)), AutoCloseable {
    private val active = AtomicBoolean(true)
    private val token = authToken.toByteArray(Charsets.UTF_8)
    private val authenticated = ConcurrentHashMap.newKeySet<WebSocket>()
    private val current = AtomicReference<WebSocket?>(null)
    private val pendingEvents = ArrayBlockingQueue<ByteArray>(EVENT_CAPACITY)
    private val sendLock = Any()
    private val eventThread = Thread(::pumpEvents, "termx-go-events").apply { isDaemon = true }
    private val started = CountDownLatch(1)

    override fun onStart() {
        started.countDown()
        eventThread.start()
    }

    fun awaitStarted(timeoutMillis: Long): Boolean = started.await(timeoutMillis, TimeUnit.MILLISECONDS)

    override fun onOpen(conn: WebSocket, handshake: ClientHandshake) = Unit

    override fun onMessage(conn: WebSocket, message: String) {
        conn.close(CLOSE_PROTOCOL, "binary binding protocol required")
    }

    override fun onMessage(conn: WebSocket, message: ByteBuffer) {
        val bytes = ByteArray(message.remaining()).also(message::get)
        if (bytes.isEmpty()) {
            conn.close(CLOSE_PROTOCOL, "empty binding frame")
            return
        }
        if (!authenticated.contains(conn)) {
            authenticate(conn, bytes)
            return
        }
        try {
            handleRequest(conn, bytes)
        } catch (_: Throwable) {
            val requestId = if (bytes.size >= 9) ByteBuffer.wrap(bytes, 1, 8).order(ByteOrder.BIG_ENDIAN).long else 0L
            sendError(conn, requestId, "native binding request failed")
        }
    }

    override fun onClose(conn: WebSocket, code: Int, reason: String?, remote: Boolean) {
        authenticated.remove(conn)
        current.compareAndSet(conn, null)
    }

    override fun onError(conn: WebSocket?, ex: Exception?) {
        if (conn != null) {
            authenticated.remove(conn)
            current.compareAndSet(conn, null)
        }
    }

    override fun close() {
        if (!active.compareAndSet(true, false)) return
        runCatching { engine.close() }
        runCatching { stop(1_000) }
        eventThread.interrupt()
    }

    private fun authenticate(conn: WebSocket, frame: ByteArray) {
        if (frame[0] != OP_AUTH || !MessageDigest.isEqual(frame.copyOfRange(1, frame.size), token)) {
            conn.close(CLOSE_POLICY, "binding authentication failed")
            return
        }
        synchronized(sendLock) {
            current.getAndSet(conn)?.takeIf { it != conn }?.close(CLOSE_REPLACED, "binding client replaced")
            authenticated.add(conn)
            sendAck(conn, 0)
            while (true) {
                val event = pendingEvents.poll() ?: break
                sendFrame(conn, OP_EVENT, 0, 0, event)
            }
        }
    }

    private fun handleRequest(conn: WebSocket, frame: ByteArray) {
        val input = ByteBuffer.wrap(frame).order(ByteOrder.BIG_ENDIAN)
        val operation = input.get()
        val requestId = input.long
        when (operation) {
            OP_OPEN_SESSION -> accept(conn, requestId, GoClientNative.openSession(engine.handle, remaining(input)))
            OP_EXECUTE -> {
                val session = input.long
                accept(conn, requestId, GoClientNative.execute(engine.handle, session, remaining(input)))
            }
            OP_OPEN_RESOURCE_STREAM -> {
                val session = input.long
                accept(conn, requestId, GoClientNative.openResourceStream(engine.handle, session, remaining(input)))
            }
            OP_SEND_RESOURCE_STREAM_FRAME -> {
                val stream = input.long
                GoClientNative.sendResourceStreamFrame(engine.handle, stream, remaining(input))
                sendAck(conn, requestId)
            }
            OP_CLOSE_RESOURCE_STREAM -> {
                GoClientNative.closeResourceStream(engine.handle, input.long)
                sendAck(conn, requestId)
            }
            OP_ENGINE_COMMAND -> accept(conn, requestId, GoClientNative.engineCommand(engine.handle, remaining(input)))
            OP_CANCEL -> {
                GoClientNative.cancel(engine.handle, input.long)
                sendAck(conn, requestId)
            }
            OP_CLOSE_SESSION -> {
                GoClientNative.closeSession(engine.handle, input.long)
                sendAck(conn, requestId)
            }
            OP_RELEASE -> {
                GoClientNative.release(engine.handle, input.long)
                sendAck(conn, requestId)
            }
            else -> sendError(conn, requestId, "unsupported binding operation")
        }
    }

    private fun pumpEvents() {
        while (active.get()) {
            val event = try {
                GoClientNative.nextEvent(engine.handle, 0)
            } catch (_: IllegalStateException) {
                return
            }
            val delivered = synchronized(sendLock) {
                val client = current.get()
                if (client != null && client.isOpen && authenticated.contains(client)) {
                    runCatching { sendFrame(client, OP_EVENT, 0, 0, event) }.isSuccess
                } else {
                    false
                }
            }
            if (!delivered) {
                try {
                    pendingEvents.put(event)
                } catch (_: InterruptedException) {
                    return
                }
            }
        }
    }

    private fun accept(conn: WebSocket, requestId: Long, operationHandle: Long) =
        sendFrame(conn, OP_ACCEPTED, requestId, operationHandle, ByteArray(0))

    private fun sendAck(conn: WebSocket, requestId: Long) =
        sendFrame(conn, OP_ACK, requestId, 0, ByteArray(0))

    private fun sendError(conn: WebSocket, requestId: Long, message: String) =
        sendFrame(conn, OP_ERROR, requestId, 0, message.toByteArray(Charsets.UTF_8))

    private fun sendFrame(conn: WebSocket, operation: Byte, requestId: Long, handle: Long, payload: ByteArray) {
        val output = ByteBuffer.allocate(1 + 8 + 8 + 4 + payload.size).order(ByteOrder.BIG_ENDIAN)
        output.put(operation).putLong(requestId).putLong(handle).putInt(payload.size).put(payload).flip()
        conn.send(output)
    }

    private fun remaining(input: ByteBuffer): ByteArray = ByteArray(input.remaining()).also(input::get)

    companion object {
        const val OP_AUTH: Byte = 0x01
        const val OP_OPEN_SESSION: Byte = 0x10
        const val OP_EXECUTE: Byte = 0x11
        const val OP_ENGINE_COMMAND: Byte = 0x12
        const val OP_CANCEL: Byte = 0x14
        const val OP_CLOSE_SESSION: Byte = 0x15
        const val OP_RELEASE: Byte = 0x16
        const val OP_OPEN_RESOURCE_STREAM: Byte = 0x17
        const val OP_SEND_RESOURCE_STREAM_FRAME: Byte = 0x18
        const val OP_CLOSE_RESOURCE_STREAM: Byte = 0x19
        const val OP_ACCEPTED: Byte = 0x20
        const val OP_ACK: Byte = 0x21
        const val OP_ERROR: Byte = 0x22
        const val OP_EVENT: Byte = 0x30
        private const val EVENT_CAPACITY = 256
        private const val CLOSE_PROTOCOL = 1002
        private const val CLOSE_POLICY = 1008
        private const val CLOSE_REPLACED = 4001
    }
}
