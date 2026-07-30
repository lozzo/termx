package com.anytty.app.goclient

import org.java_websocket.WebSocket
import org.java_websocket.WebSocketAdapter
import org.java_websocket.WebSocketImpl
import org.java_websocket.enums.Opcode
import org.java_websocket.exceptions.InvalidDataException
import org.java_websocket.framing.BinaryFrame
import org.java_websocket.framing.CloseFrame
import org.java_websocket.framing.ContinuousFrame
import org.java_websocket.framing.PingFrame
import org.java_websocket.framing.PongFrame
import org.java_websocket.framing.TextFrame
import org.java_websocket.handshake.Handshakedata
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.EOFException
import java.net.Socket
import java.net.SocketTimeoutException
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.concurrent.TimeUnit

class BridgeTransportTest {
    private val servers = mutableListOf<GoClientBridgeServer>()
    private val sockets = mutableListOf<RawSocket>()

    @After
    fun tearDown() {
        sockets.forEach { runCatching { it.close() } }
        servers.forEach { runCatching { it.close() } }
    }

    @Test
    fun `eight slow accepts reserve physical slots ninth is closed and release permits replacement`() {
        val server = startServer()
        repeat(BRIDGE_PHYSICAL_LIMIT) { raw(server) }
        await { server.slotSnapshot().first == BRIDGE_PHYSICAL_LIMIT }

        val rejected = raw(server)
        assertEquals(-1, rejected.readByte())
        assertEquals(BRIDGE_PHYSICAL_LIMIT, server.slotSnapshot().first)

        sockets.first().close()
        await { server.slotSnapshot().first == BRIDGE_PHYSICAL_LIMIT - 1 }
        raw(server)
        await { server.slotSnapshot().first == BRIDGE_PHYSICAL_LIMIT }
    }

    @Test
    fun `fifth in-progress upgrade is rejected before library handshake parsing`() {
        val server = startServer()
        repeat(BRIDGE_UPGRADE_LIMIT) { raw(server).writeAscii("G") }
        await { server.slotSnapshot().second == BRIDGE_UPGRADE_LIMIT }

        val rejected = raw(server)
        rejected.writeAscii("G")
        assertEquals(-1, rejected.readByte())
        assertEquals(BRIDGE_UPGRADE_LIMIT, server.slotSnapshot().second)
    }

    @Test
    fun `complete header accepts exactly 16 KiB and rejects one byte more`() {
        val server = startServer()
        val accepted = raw(server)
        accepted.writeAscii(handshakeBytes(totalBytes = BRIDGE_MAX_HEADER_BYTES))
        assertTrue(accepted.readHttpResponse().startsWith("HTTP/1.1 101"))

        val rejected = raw(server)
        rejected.writeAscii(handshakeBytes(totalBytes = BRIDGE_MAX_HEADER_BYTES + 1))
        assertFalse(rejected.readUntilClose().contains("HTTP/1.1 101"))
    }

    @Test
    fun `upgrade at 1_9 seconds does not reset accept-to-auth deadline`() {
        val server = startServer()
        val socket = raw(server)
        Thread.sleep(1_900)
        socket.writeAscii(handshakeBytes())
        assertTrue(socket.readHttpResponse().startsWith("HTTP/1.1 101"))
        Thread.sleep(150)
        runCatching { socket.sendFrame(Opcode.BINARY, authFrame(TOKEN), true) }
        val frame = socket.readFrameOrNull()
        assertTrue(frame == null || frame.opcode == 8)
        await { server.slotSnapshot() == Triple(0, 0, false) }
    }

    @Test
    fun `strict path origin and protocol matrix`() {
        val invalid = listOf(
            handshakeBytes(path = "/other"),
            handshakeBytes(origin = null),
            handshakeBytes(origin = "http://localhost:80"),
            handshakeBytes(origin = " http://localhost"),
            handshakeBytes(origin = "http://localhost "),
            handshakeBytes(origin = null, extraHeaders = listOf("Origin : http://localhost")),
            handshakeBytes(origin = null, extraHeaders = listOf("Origin:\thttp://localhost")),
            handshakeBytes(protocol = null),
            handshakeBytes(protocol = "$BRIDGE_PROTOCOL,other"),
            handshakeBytes(protocol = " $BRIDGE_PROTOCOL"),
            handshakeBytes(protocol = "$BRIDGE_PROTOCOL "),
            handshakeBytes(protocol = null, extraHeaders = listOf("Sec-WebSocket-Protocol : $BRIDGE_PROTOCOL")),
            handshakeBytes(protocol = null, extraHeaders = listOf("Sec-WebSocket-Protocol:\t$BRIDGE_PROTOCOL")),
            handshakeBytes(extraHeaders = listOf("Origin: http://localhost")),
            handshakeBytes(extraHeaders = listOf("Sec-WebSocket-Protocol: $BRIDGE_PROTOCOL")),
        )
        for (request in invalid) {
            val socket = raw(startServer())
            socket.writeAscii(request)
            assertFalse("invalid request upgraded: $request", socket.readUntilClose().contains("HTTP/1.1 101"))
        }

        val valid = raw(startServer())
        valid.writeAscii(handshakeBytes())
        assertTrue(valid.readHttpResponse().startsWith("HTTP/1.1 101"))
        assertTrue(valid.lastHttpResponse.contains("Sec-WebSocket-Protocol: $BRIDGE_PROTOCOL"))
    }

    @Test
    fun `only exact auth message is acknowledged and failures close 1008`() {
        val valid = upgraded(startServer())
        valid.sendFrame(Opcode.BINARY, authFrame(TOKEN), true)
        val ack = valid.readFrame()
        assertEquals(2, ack.opcode)
        assertEquals(GoClientBridgeServer.OP_ACK, ack.payload[0])

        val invalidFrames = listOf(
            ByteArray(0),
            authFrame(TOKEN).copyOf(43),
            authFrame(TOKEN).copyOf(45),
            authFrame(TOKEN).also { it[0] = 2 },
            authFrame("B".repeat(43)),
        )
        for (invalid in invalidFrames) {
            val socket = upgraded(startServer())
            socket.sendFrame(Opcode.BINARY, invalid, true)
            val close = socket.readFrame()
            assertEquals(8, close.opcode)
            assertEquals(CloseFrame.POLICY_VALIDATION, close.closeCode())
            assertFalse(String(close.payload, StandardCharsets.UTF_8).contains("B".repeat(8)))
        }
    }

    @Test
    fun `continuations aggregate to 1009 controls do not reset and text is 1002`() {
        val overLimit = upgraded(startServer())
        overLimit.sendFrame(Opcode.BINARY, ByteArray(BRIDGE_MAX_MESSAGE_BYTES / 2), false)
        overLimit.sendFrame(Opcode.PING, byteArrayOf(1), true)
        assertEquals(10, overLimit.readFrame().opcode)
        overLimit.sendFrame(Opcode.CONTINUOUS, ByteArray(BRIDGE_MAX_MESSAGE_BYTES / 2 + 1), true)
        assertEquals(CloseFrame.TOOBIG, overLimit.readFrame().closeCode())

        val text = upgraded(startServer())
        text.sendFrame(Opcode.TEXT, "x".toByteArray(), true)
        assertEquals(CloseFrame.PROTOCOL_ERROR, text.readFrame().closeCode())
    }

    @Test
    fun `Kotlin response header is counted before allocation`() {
        assertEquals(
            BRIDGE_MAX_MESSAGE_BYTES,
            bridgeResponseFrameBytes(BRIDGE_MAX_MESSAGE_BYTES - BRIDGE_RESPONSE_HEADER_BYTES),
        )
        assertNull(bridgeResponseFrameBytes(BRIDGE_MAX_MESSAGE_BYTES - BRIDGE_RESPONSE_HEADER_BYTES + 1))
    }

    @Test
    fun `draft copy and reset isolate aggregate counters`() {
        val draft = BridgeDraft6455()
        assertNotSame(draft, draft.copyInstance())
        val listener = object : WebSocketAdapter() {
            override fun onWebsocketMessage(conn: WebSocket, message: String) = Unit

            override fun onWebsocketMessage(conn: WebSocket, blob: ByteBuffer) = Unit

            override fun onWebsocketOpen(conn: WebSocket, handshake: Handshakedata) = Unit

            override fun onWebsocketClose(conn: WebSocket, code: Int, reason: String, remote: Boolean) = Unit

            override fun onWebsocketClosing(conn: WebSocket, code: Int, reason: String, remote: Boolean) = Unit

            override fun onWebsocketCloseInitiated(conn: WebSocket, code: Int, reason: String) = Unit

            override fun onWebsocketError(conn: WebSocket, ex: Exception) = Unit

            override fun onWriteDemand(conn: WebSocket) = Unit

            override fun getLocalSocketAddress(conn: WebSocket) = null

            override fun getRemoteSocketAddress(conn: WebSocket) = null
        }
        val socket = WebSocketImpl(listener, BridgeDraft6455())
        val half = ByteBuffer.wrap(ByteArray(BRIDGE_MAX_MESSAGE_BYTES / 2 + 1))
        draft.processFrame(socket, BinaryFrame().apply { setPayload(half); isFin = false })
        draft.processFrame(socket, PongFrame().apply { setPayload(ByteBuffer.wrap(byteArrayOf(1))) })
        try {
            draft.processFrame(
                socket,
                ContinuousFrame().apply {
                    setPayload(ByteBuffer.wrap(ByteArray(BRIDGE_MAX_MESSAGE_BYTES / 2)))
                    isFin = true
                },
            )
            fail("aggregate payload unexpectedly accepted")
        } catch (error: InvalidDataException) {
            assertEquals(CloseFrame.TOOBIG, error.closeCode)
        }
        draft.reset()
        val aggregateField = BridgeDraft6455::class.java.getDeclaredField("messageBytes").apply { isAccessible = true }
        assertEquals(0L, aggregateField.getLong(draft))

        val resetDraft = draft.copyInstance() as BridgeDraft6455
        val resetSocket = WebSocketImpl(listener, BridgeDraft6455())
        resetDraft.processFrame(resetSocket, BinaryFrame().apply { setPayload(ByteBuffer.wrap(byteArrayOf(1))); isFin = true })

        try {
            resetDraft.processFrame(resetSocket, TextFrame().apply { setPayload(ByteBuffer.wrap(byteArrayOf(1))) })
            fail("text frame unexpectedly accepted")
        } catch (error: InvalidDataException) {
            assertEquals(CloseFrame.PROTOCOL_ERROR, error.closeCode)
        }
    }

    @Test
    fun `server stop releases all physical and upgrade slots`() {
        val server = startServer()
        repeat(3) { raw(server).writeAscii("G") }
        await { server.slotSnapshot() == Triple(3, 3, false) }
        server.close()
        await { server.slotSnapshot() == Triple(0, 0, false) }
    }

    @Test
    fun `auth deadline uses elapsed nano time from negative values and across wrap`() {
        assertElapsedDeadline(
            acceptedAt = -5_000_000_000L,
            beforeDeadline = -3_000_000_001L,
            atDeadline = -3_000_000_000L,
        )

        val acceptedBeforeWrap = Long.MAX_VALUE - 1_000_000_000L
        assertElapsedDeadline(
            acceptedAt = acceptedBeforeWrap,
            beforeDeadline = acceptedBeforeWrap + BRIDGE_AUTH_DEADLINE_NANOS - 1,
            atDeadline = acceptedBeforeWrap + BRIDGE_AUTH_DEADLINE_NANOS,
        )
    }

    private fun startServer(): GoClientBridgeServer {
        val server = GoClientBridgeServer(FakeEngine(), TOKEN)
        servers += server
        server.start()
        assertTrue(server.awaitStarted(2_000))
        return server
    }

    private fun raw(server: GoClientBridgeServer): RawSocket {
        val socket = RawSocket(server.port)
        sockets += socket
        return socket
    }

    private fun upgraded(server: GoClientBridgeServer): RawSocket = raw(server).also {
        it.writeAscii(handshakeBytes())
        assertTrue(it.readHttpResponse().startsWith("HTTP/1.1 101"))
    }

    private fun assertElapsedDeadline(acceptedAt: Long, beforeDeadline: Long, atDeadline: Long) {
        var now = acceptedAt
        val registry = BridgeConnectionRegistry { now }
        try {
            val before = bridgeConnection(registry)
            assertTrue(registry.registerPhysical(before))
            now = beforeDeadline
            assertTrue(registry.authenticate(before, true, {}, {}))
            registry.release(before)

            now = acceptedAt
            val expired = bridgeConnection(registry)
            assertTrue(registry.registerPhysical(expired))
            now = atDeadline
            assertFalse(registry.authenticate(expired, true, {}, {}))
            assertEquals(Triple(0, 0, false), registry.snapshot())
        } finally {
            registry.stop()
        }
    }

    private fun bridgeConnection(registry: BridgeConnectionRegistry): BridgeWebSocketImpl =
        BridgeWebSocketImpl(testWebSocketListener(), listOf(BridgeDraft6455()), registry)

    private fun testWebSocketListener(): WebSocketAdapter = object : WebSocketAdapter() {
        override fun onWebsocketMessage(conn: WebSocket, message: String) = Unit
        override fun onWebsocketMessage(conn: WebSocket, blob: ByteBuffer) = Unit
        override fun onWebsocketOpen(conn: WebSocket, handshake: Handshakedata) = Unit
        override fun onWebsocketClose(conn: WebSocket, code: Int, reason: String, remote: Boolean) = Unit
        override fun onWebsocketClosing(conn: WebSocket, code: Int, reason: String, remote: Boolean) = Unit
        override fun onWebsocketCloseInitiated(conn: WebSocket, code: Int, reason: String) = Unit
        override fun onWebsocketError(conn: WebSocket, ex: Exception) = Unit
        override fun onWriteDemand(conn: WebSocket) = Unit
        override fun getLocalSocketAddress(conn: WebSocket) = null
        override fun getRemoteSocketAddress(conn: WebSocket) = null
    }

    private fun await(condition: () -> Boolean) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(3)
        while (!condition()) {
            if (System.nanoTime() >= deadline) fail("condition did not become true")
            Thread.sleep(10)
        }
    }

    private class FakeEngine : GoClientBridgeEngine {
        @Volatile private var closed = false
        override fun openSession(payload: ByteArray) = 1L
        override fun execute(session: Long, payload: ByteArray) = 2L
        override fun openResourceStream(session: Long, payload: ByteArray) = 3L
        override fun sendResourceStreamFrame(stream: Long, payload: ByteArray) = Unit
        override fun closeResourceStream(stream: Long) = Unit
        override fun engineCommand(payload: ByteArray) = 4L
        override fun cancel(operation: Long) = Unit
        override fun closeSession(session: Long) = Unit
        override fun release(handle: Long) = Unit
        override fun nextEvent(): ByteArray {
            while (!closed) Thread.sleep(25)
            throw IllegalStateException("closed")
        }
        override fun close() { closed = true }
    }

    private class RawSocket(port: Int) : Closeable {
        private val socket = Socket("127.0.0.1", port).apply { soTimeout = 2_500 }
        private val input = socket.getInputStream()
        private val output = socket.getOutputStream()
        var lastHttpResponse: String = ""
            private set

        fun writeAscii(value: String) {
            output.write(value.toByteArray(StandardCharsets.US_ASCII))
            output.flush()
        }

        fun readByte(): Int = try { input.read() } catch (_: SocketTimeoutException) { Int.MIN_VALUE }

        fun readHttpResponse(): String {
            val bytes = ByteArrayOutputStream()
            var delimiter = 0
            while (delimiter != 4) {
                val byte = input.read()
                if (byte < 0) throw EOFException("connection closed before HTTP response")
                bytes.write(byte)
                delimiter = when (delimiter) {
                    0 -> if (byte == 13) 1 else 0
                    1 -> if (byte == 10) 2 else if (byte == 13) 1 else 0
                    2 -> if (byte == 13) 3 else 0
                    3 -> if (byte == 10) 4 else if (byte == 13) 1 else 0
                    else -> 4
                }
            }
            lastHttpResponse = bytes.toString(StandardCharsets.US_ASCII.name())
            return lastHttpResponse
        }

        fun readUntilClose(): String {
            val bytes = ByteArrayOutputStream()
            return try {
                while (true) {
                    val byte = input.read()
                    if (byte < 0) break
                    bytes.write(byte)
                }
                bytes.toString(StandardCharsets.US_ASCII.name())
            } catch (_: SocketTimeoutException) {
                bytes.toString(StandardCharsets.US_ASCII.name())
            }
        }

        fun sendFrame(opcode: Opcode, payload: ByteArray, fin: Boolean) {
            val opcodeValue = when (opcode) {
                Opcode.CONTINUOUS -> 0
                Opcode.TEXT -> 1
                Opcode.BINARY -> 2
                Opcode.PING -> 9
                else -> error("unsupported test opcode")
            }
            output.write((if (fin) 0x80 else 0) or opcodeValue)
            when {
                payload.size <= 125 -> output.write(0x80 or payload.size)
                payload.size <= 0xffff -> {
                    output.write(0x80 or 126)
                    output.write(payload.size ushr 8)
                    output.write(payload.size)
                }
                else -> {
                    output.write(0x80 or 127)
                    repeat(4) { output.write(0) }
                    output.write(payload.size ushr 24)
                    output.write(payload.size ushr 16)
                    output.write(payload.size ushr 8)
                    output.write(payload.size)
                }
            }
            val mask = byteArrayOf(0x13, 0x37, 0x42, 0x55)
            output.write(mask)
            val masked = ByteArray(payload.size) { index -> (payload[index].toInt() xor mask[index % 4].toInt()).toByte() }
            output.write(masked)
            output.flush()
        }

        fun readFrameOrNull(): Frame? = try { readFrame() } catch (_: EOFException) { null } catch (_: SocketTimeoutException) { null }

        fun readFrame(): Frame {
            val first = input.read()
            if (first < 0) throw EOFException()
            val second = input.read()
            if (second < 0) throw EOFException()
            var length = second and 0x7f
            if (length == 126) length = (readRequired() shl 8) or readRequired()
            if (length == 127) {
                repeat(4) { if (readRequired() != 0) fail("unexpected large server frame") }
                length = (readRequired() shl 24) or (readRequired() shl 16) or (readRequired() shl 8) or readRequired()
            }
            val payload = ByteArray(length)
            var offset = 0
            while (offset < payload.size) {
                val read = input.read(payload, offset, payload.size - offset)
                if (read < 0) throw EOFException()
                offset += read
            }
            return Frame(first and 0x0f, payload)
        }

        private fun readRequired(): Int = input.read().also { if (it < 0) throw EOFException() }

        override fun close() = socket.close()
    }

    private data class Frame(val opcode: Int, val payload: ByteArray) {
        fun closeCode(): Int = if (payload.size >= 2) {
            ((payload[0].toInt() and 0xff) shl 8) or (payload[1].toInt() and 0xff)
        } else {
            CloseFrame.NOCODE
        }
    }

    private companion object {
        const val TOKEN = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

        fun authFrame(token: String): ByteArray = byteArrayOf(GoClientBridgeServer.OP_AUTH) + token.toByteArray()

        fun handshakeBytes(
            path: String = "/",
            origin: String? = "http://localhost",
            protocol: String? = BRIDGE_PROTOCOL,
            extraHeaders: List<String> = emptyList(),
            totalBytes: Int? = null,
        ): String {
            val lines = mutableListOf(
                "GET $path HTTP/1.1",
                "Host: 127.0.0.1",
                "Upgrade: websocket",
                "Connection: Upgrade",
                "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==",
                "Sec-WebSocket-Version: 13",
            )
            if (origin != null) lines += "Origin: $origin"
            if (protocol != null) lines += "Sec-WebSocket-Protocol: $protocol"
            lines += extraHeaders
            var request = lines.joinToString("\r\n", postfix = "\r\n")
            if (totalBytes != null) {
                val overhead = "X-Pad: \r\n\r\n".length
                val padding = totalBytes - request.length - overhead
                require(padding >= 0)
                request += "X-Pad: ${"x".repeat(padding)}\r\n"
            }
            return request + "\r\n"
        }
    }
}
