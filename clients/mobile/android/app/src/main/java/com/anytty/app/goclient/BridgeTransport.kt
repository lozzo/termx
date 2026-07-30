package com.anytty.app.goclient

import org.java_websocket.WebSocketAdapter
import org.java_websocket.WebSocketImpl
import org.java_websocket.WebSocketServerFactory
import org.java_websocket.drafts.Draft
import org.java_websocket.drafts.Draft_6455
import org.java_websocket.enums.HandshakeState
import org.java_websocket.enums.Opcode
import org.java_websocket.exceptions.InvalidDataException
import org.java_websocket.exceptions.InvalidHandshakeException
import org.java_websocket.exceptions.LimitExceededException
import org.java_websocket.extensions.DefaultExtension
import org.java_websocket.framing.CloseFrame
import org.java_websocket.framing.Framedata
import org.java_websocket.handshake.ClientHandshake
import org.java_websocket.handshake.Handshakedata
import org.java_websocket.protocols.Protocol
import java.io.IOException
import java.nio.ByteBuffer
import java.nio.channels.ByteChannel
import java.nio.channels.SelectionKey
import java.nio.channels.SocketChannel
import java.nio.charset.StandardCharsets
import java.util.IdentityHashMap
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

internal const val BRIDGE_PROTOCOL = "anytty.binding.v1"
internal const val BRIDGE_MAX_MESSAGE_BYTES = 4 * 1024 * 1024
internal const val BRIDGE_MAX_HEADER_BYTES = 16 * 1024
internal const val BRIDGE_AUTH_DEADLINE_NANOS = 2_000_000_000L
internal const val BRIDGE_PHYSICAL_LIMIT = 8
internal const val BRIDGE_UPGRADE_LIMIT = 4

/** Owns the exact physical/upgrade/auth lifecycle under one synchronization boundary. */
internal class BridgeConnectionRegistry(
    private val nanoTime: () -> Long = System::nanoTime,
) {
    private data class State(
        val acceptedAtNanos: Long,
        var upgrade: Boolean = false,
        var authenticated: Boolean = false,
        var timeout: ScheduledFuture<*>? = null,
    )

    private val lock = Any()
    private val states = IdentityHashMap<BridgeWebSocketImpl, State>()
    private var upgradeCount = 0
    private var current: BridgeWebSocketImpl? = null
    private var stopped = false
    private val deadlines = Executors.newSingleThreadScheduledExecutor { task ->
        Thread(task, "anytty-bridge-deadline").apply { isDaemon = true }
    }

    fun registerPhysical(connection: BridgeWebSocketImpl): Boolean = synchronized(lock) {
        if (stopped || states.size >= BRIDGE_PHYSICAL_LIMIT) return@synchronized false
        val state = State(acceptedAtNanos = nanoTime())
        states[connection] = state
        state.timeout = deadlines.schedule(
            { expire(connection) },
            BRIDGE_AUTH_DEADLINE_NANOS,
            TimeUnit.NANOSECONDS,
        )
        true
    }

    fun acquireUpgrade(connection: BridgeWebSocketImpl): Boolean = synchronized(lock) {
        val state = states[connection] ?: return@synchronized false
        if (state.upgrade) return@synchronized true
        if (upgradeCount >= BRIDGE_UPGRADE_LIMIT) {
            terminateLocked(connection, state)
            connection.closeForPolicy()
            return@synchronized false
        }
        state.upgrade = true
        upgradeCount += 1
        true
    }

    fun authenticate(
        connection: BridgeWebSocketImpl,
        valid: Boolean,
        acknowledge: () -> Unit,
        afterAcknowledge: () -> Unit,
    ): Boolean = synchronized(lock) {
        val state = states[connection] ?: return@synchronized false
        if (deadlineElapsed(state) || !valid) {
            terminateLocked(connection, state)
            connection.closeForPolicy()
            return@synchronized false
        }

        val replaced = current
        if (replaced != null && replaced !== connection) {
            states[replaced]?.let { terminateLocked(replaced, it) }
            replaced.close(4001, "binding client replaced")
        }
        current = connection
        state.authenticated = true
        try {
            acknowledge()
            afterAcknowledge()
        } catch (_: RuntimeException) {
            terminateLocked(connection, state)
            connection.closeForPolicy()
            return@synchronized false
        }
        state.timeout?.cancel(false)
        state.timeout = null
        true
    }

    fun <T> withAuthenticated(connection: BridgeWebSocketImpl, action: () -> T): T? = synchronized(lock) {
        val state = states[connection]
        if (state?.authenticated != true) return@synchronized null
        action()
    }

    fun <T> withCurrent(action: (BridgeWebSocketImpl) -> T): T? = synchronized(lock) {
        val connection = current ?: return@synchronized null
        if (states[connection]?.authenticated != true || !connection.isOpen) return@synchronized null
        action(connection)
    }

    fun reject(connection: BridgeWebSocketImpl) = synchronized(lock) {
        val state = states[connection] ?: return@synchronized
        terminateLocked(connection, state)
        connection.closeForPolicy()
    }

    fun release(connection: BridgeWebSocketImpl) = synchronized(lock) {
        val state = states[connection] ?: return@synchronized
        terminateLocked(connection, state)
    }

    fun stop() = synchronized(lock) {
        if (stopped) return@synchronized
        stopped = true
        val connections = states.keys.toList()
        for (connection in connections) {
            states[connection]?.let { terminateLocked(connection, it) }
            connection.closeConnection(CloseFrame.GOING_AWAY, "bridge stopped")
        }
        deadlines.shutdownNow()
    }

    internal fun snapshot(): Triple<Int, Int, Boolean> = synchronized(lock) {
        Triple(states.size, upgradeCount, current != null)
    }

    private fun expire(connection: BridgeWebSocketImpl) = synchronized(lock) {
        val state = states[connection] ?: return@synchronized
        if (state.authenticated || !deadlineElapsed(state)) return@synchronized
        terminateLocked(connection, state)
        connection.closeForPolicy()
    }

    private fun deadlineElapsed(state: State): Boolean =
        nanoTime() - state.acceptedAtNanos >= BRIDGE_AUTH_DEADLINE_NANOS

    private fun terminateLocked(connection: BridgeWebSocketImpl, state: State) {
        if (states.remove(connection) == null) return
        state.timeout?.cancel(false)
        state.timeout = null
        if (state.upgrade) upgradeCount -= 1
        if (current === connection) current = null
        state.authenticated = false
    }

}

/** Registers accepted sockets before the selector can read and rejects overflow in wrapChannel. */
internal class BridgeWebSocketFactory(
    private val registry: BridgeConnectionRegistry,
) : WebSocketServerFactory {
    override fun createWebSocket(listener: WebSocketAdapter, draft: Draft): WebSocketImpl =
        create(listener, listOf(draft))

    override fun createWebSocket(listener: WebSocketAdapter, drafts: List<Draft>): WebSocketImpl =
        create(listener, drafts)

    private fun create(listener: WebSocketAdapter, drafts: List<Draft>): BridgeWebSocketImpl {
        val connection = BridgeWebSocketImpl(listener, drafts, registry)
        connection.physicalAccepted = registry.registerPhysical(connection)
        return connection
    }

    @Throws(IOException::class)
    override fun wrapChannel(channel: SocketChannel, key: SelectionKey): ByteChannel {
        val connection = key.attachment() as? BridgeWebSocketImpl
        if (connection != null && !connection.physicalAccepted) {
            key.cancel()
            channel.close()
            connection.closeConnection(CloseFrame.POLICY_VALIDATION, "bridge connection limit")
        }
        return channel
    }

    override fun close() = registry.stop()
}

/** Adds only pre-handshake byte accounting; RFC 6455 parsing remains in WebSocketImpl. */
internal class BridgeWebSocketImpl(
    listener: WebSocketAdapter,
    drafts: List<Draft>,
    private val registry: BridgeConnectionRegistry,
) : WebSocketImpl(listener, drafts) {
    internal var physicalAccepted: Boolean = false
    private var headerComplete = false
    private var headerBytes = 0
    private var delimiterState = 0

    override fun decode(socketBuffer: ByteBuffer) {
        if (!physicalAccepted || isClosed) return
        if (!headerComplete) {
            if (!registry.acquireUpgrade(this)) return
            val input = socketBuffer.duplicate()
            while (input.hasRemaining() && !headerComplete) {
                val byte = input.get()
                headerBytes += 1
                delimiterState = nextDelimiterState(delimiterState, byte)
                if (headerBytes > BRIDGE_MAX_HEADER_BYTES) {
                    registry.reject(this)
                    return
                }
                headerComplete = delimiterState == 4
            }
        }
        super.decode(socketBuffer)
    }

    override fun closeConnection(code: Int, message: String, remote: Boolean) {
        // Java-WebSocket invokes onClose while holding this connection's monitor.
        // Release first so registry -> connection remains the only lock order.
        registry.release(this)
        super.closeConnection(code, message, remote)
    }

    internal fun closeForPolicy() {
        if (isOpen) close(CloseFrame.POLICY_VALIDATION, "binding authentication failed")
        else closeConnection(CloseFrame.POLICY_VALIDATION, "binding authentication failed")
    }

    private fun nextDelimiterState(state: Int, byte: Byte): Int = when (state) {
        0 -> if (byte == CR) 1 else 0
        1 -> if (byte == LF) 2 else if (byte == CR) 1 else 0
        2 -> if (byte == CR) 3 else 0
        3 -> if (byte == LF) 4 else if (byte == CR) 1 else 0
        else -> 4
    }

    private companion object {
        const val CR: Byte = 13
        const val LF: Byte = 10
    }
}

/** Enforces the bridge handshake and aggregate message limit around Draft_6455. */
internal class BridgeDraft6455 : Draft_6455(
    listOf(DefaultExtension()),
    listOf(Protocol(BRIDGE_PROTOCOL)),
    BRIDGE_MAX_MESSAGE_BYTES,
) {
    private var messageBytes = 0L

    override fun translateHandshake(buffer: ByteBuffer): Handshakedata {
        val raw = buffer.duplicate()
        val handshake = super.translateHandshake(buffer)
        validateExactRequest(raw)
        return handshake
    }

    override fun acceptHandshakeAsServer(handshakedata: ClientHandshake): HandshakeState {
        if (handshakedata.resourceDescriptor != "/") return HandshakeState.NOT_MATCHED
        if (!handshakedata.hasFieldValue("Origin") || handshakedata.getFieldValue("Origin") != "http://localhost") {
            return HandshakeState.NOT_MATCHED
        }
        if (!handshakedata.hasFieldValue("Sec-WebSocket-Protocol") ||
            handshakedata.getFieldValue("Sec-WebSocket-Protocol") != BRIDGE_PROTOCOL
        ) {
            return HandshakeState.NOT_MATCHED
        }
        return super.acceptHandshakeAsServer(handshakedata)
    }

    override fun processFrame(webSocketImpl: WebSocketImpl, frame: Framedata) {
        val opcode = frame.opcode
        if (opcode == Opcode.TEXT) {
            messageBytes = 0
            throw InvalidDataException(CloseFrame.PROTOCOL_ERROR, "text frames are not supported")
        }
        val data = opcode == Opcode.BINARY || opcode == Opcode.CONTINUOUS
        if (data) {
            val payloadBytes = frame.payloadData.remaining().toLong()
            if (messageBytes > BRIDGE_MAX_MESSAGE_BYTES.toLong() - payloadBytes) {
                messageBytes = 0
                throw LimitExceededException("binding message exceeds limit", BRIDGE_MAX_MESSAGE_BYTES)
            }
            messageBytes += payloadBytes
        }
        try {
            super.processFrame(webSocketImpl, frame)
        } catch (error: InvalidDataException) {
            messageBytes = 0
            throw error
        } catch (error: RuntimeException) {
            messageBytes = 0
            throw error
        } catch (error: Error) {
            messageBytes = 0
            throw error
        } finally {
            if ((data && frame.isFin) || opcode == Opcode.CLOSING) messageBytes = 0
        }
    }

    override fun reset() {
        messageBytes = 0
        super.reset()
    }

    override fun copyInstance(): Draft = BridgeDraft6455()

    private fun validateExactRequest(raw: ByteBuffer) {
        var delimiter = 0
        var length = 0
        while (raw.hasRemaining() && delimiter != 4) {
            val byte = raw.get()
            length += 1
            delimiter = when (delimiter) {
                0 -> if (byte == 13.toByte()) 1 else 0
                1 -> if (byte == 10.toByte()) 2 else if (byte == 13.toByte()) 1 else 0
                2 -> if (byte == 13.toByte()) 3 else 0
                3 -> if (byte == 10.toByte()) 4 else if (byte == 13.toByte()) 1 else 0
                else -> 4
            }
        }
        if (delimiter != 4 || length > BRIDGE_MAX_HEADER_BYTES) throw InvalidHandshakeException("invalid bridge header")
        raw.position(raw.position() - length)
        val bytes = ByteArray(length)
        raw.get(bytes)
        val lines = String(bytes, StandardCharsets.US_ASCII).split("\r\n")
        if (lines.firstOrNull() != "GET / HTTP/1.1") throw InvalidHandshakeException("invalid bridge path")
        requireExactHeader(lines, "Origin", "http://localhost")
        requireExactHeader(lines, "Sec-WebSocket-Protocol", BRIDGE_PROTOCOL)
    }

    private fun requireExactHeader(lines: List<String>, name: String, value: String) {
        val matching = lines.filter { line ->
            val separator = line.indexOf(':')
            separator >= 0 && line.substring(0, separator).equals(name, ignoreCase = true)
        }
        if (matching.size != 1 || matching.single() != "${matching.single().substringBefore(':')}: $value") {
            throw InvalidHandshakeException("invalid bridge header")
        }
    }
}
