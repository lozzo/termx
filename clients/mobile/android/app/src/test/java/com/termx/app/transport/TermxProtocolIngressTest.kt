package com.termx.app.transport

import com.google.protobuf.UnknownFieldSet
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import termx.protocol.wirepb.Terminal
import java.nio.ByteBuffer

/** TermxProtocolIngressTest 固定 Android auth 完成后的 wire Hello 门和完整帧边界。 */
class TermxProtocolIngressTest {
    @Test
    fun acceptsOnlyCompleteWireV3Hello() {
        val hello = Terminal.Hello.newBuilder().setVersion(3).setServer("termx-daemon").build()
        validateTermxProtocolHelloFrame(frame(0, 0x00, hello.toByteArray()), 3)

        val decoded = decodeTermxProtocolFrame(frame(7, 0x14, byteArrayOf(1, 2, 3)))
        assertEquals(7, decoded.channel)
        assertEquals(0x14, decoded.type)
        assertEquals(listOf<Byte>(1, 2, 3), decoded.payload.toList())
    }

    @Test
    fun rejectsOutOfOrderUnknownAndMalformedHelloFrames() {
        assertThrows(IllegalStateException::class.java) {
            validateTermxProtocolHelloFrame(frame(0, 0x02, ByteArray(0)), 3)
        }
        assertThrows(IllegalStateException::class.java) {
            validateTermxProtocolHelloFrame(
                frame(0, 0x00, Terminal.Hello.newBuilder().setVersion(2).build().toByteArray()),
                3,
            )
        }
        val unknown = UnknownFieldSet.newBuilder()
            .addField(99, UnknownFieldSet.Field.newBuilder().addVarint(1).build())
            .build()
        assertThrows(IllegalStateException::class.java) {
            validateTermxProtocolHelloFrame(
                frame(0, 0x00, Terminal.Hello.newBuilder().setVersion(3).setUnknownFields(unknown).build().toByteArray()),
                3,
            )
        }
        assertThrows(IllegalArgumentException::class.java) {
            decodeTermxProtocolFrame(frame(0, 0x00, ByteArray(0)) + byteArrayOf(1))
        }
    }

    private fun frame(channel: Int, type: Int, payload: ByteArray): ByteArray = ByteBuffer.allocate(7 + payload.size)
        .putShort(channel.toShort())
        .put(type.toByte())
        .putInt(payload.size)
        .put(payload)
        .array()
}
