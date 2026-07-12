package com.termx.app.transfer

import com.google.protobuf.ByteString
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import termx.protocol.wirepb.Terminal

/** FileTransferProtocolTest 固定 Android 与 daemon 共用的 v4 文件流失败边界。 */
class FileTransferProtocolTest {
    @Test fun validatesContiguousDataAndUploadAck() {
        val data = Terminal.FileTransferData.newBuilder().setOffset(12).setData(ByteString.copyFromUtf8("chunk")).build()
        val decoded = decodeDownloadChunk(data.toByteArray(), 12, 64 * 1024)
        assertEquals(12, decoded.offset)
        assertArrayEquals("chunk".toByteArray(), decoded.data)
        val ack = Terminal.FileTransferAck.newBuilder().setOffset(17).setWindowBytes(262144).build()
        assertEquals(17, decodeUploadAck(ack.toByteArray(), 12, 17).offset)
        assertThrows(IllegalArgumentException::class.java) { decodeUploadAck(ack.toByteArray(), 18, 20) }
    }

    @Test fun rejectsOffsetSizeAndDigestMismatch() {
        val data = Terminal.FileTransferData.newBuilder().setOffset(13).setData(ByteString.copyFromUtf8("x")).build()
        assertThrows(IllegalArgumentException::class.java) { decodeDownloadChunk(data.toByteArray(), 12, 1024) }
        val digest = ByteArray(32) { it.toByte() }
        val finish = Terminal.FileTransferFinish.newBuilder().setSize(5).setSha256(ByteString.copyFrom(digest)).build()
        verifyTransferFinish(finish.toByteArray(), 5, 5, digest)
        assertThrows(IllegalArgumentException::class.java) { verifyTransferFinish(finish.toByteArray(), 5, 4, digest) }
        val result = Terminal.FileTransferResult.newBuilder().setPath("/tmp/a").setSize(5).setSha256(ByteString.copyFrom(digest)).build()
        verifyUploadResult(result.toByteArray(), 5, digest)
        assertThrows(IllegalArgumentException::class.java) { verifyUploadResult(result.toByteArray(), 5, ByteArray(32)) }
    }
}
