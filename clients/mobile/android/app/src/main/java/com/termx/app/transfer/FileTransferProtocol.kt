package com.termx.app.transfer

import termx.protocol.wirepb.Terminal

/** DownloadChunk 是完成 protobuf 与连续 offset 校验后的下载数据。 */
internal data class DownloadChunk(val offset: Long, val data: ByteArray)

/** decodeDownloadChunk 拒绝未知字段、空块、跳跃 offset 和超过 daemon chunk 边界的数据。 */
internal fun decodeDownloadChunk(payload: ByteArray, expectedOffset: Long, maxChunkBytes: Int): DownloadChunk {
    val value = Terminal.FileTransferData.parseFrom(payload)
    require(value.unknownFields.asMap().isEmpty()) { "download data contains unknown fields" }
    require(value.offset == expectedOffset) { "download offset is not contiguous" }
    require(!value.data.isEmpty && value.data.size() <= maxChunkBytes) { "download chunk size is invalid" }
    return DownloadChunk(value.offset, value.data.toByteArray())
}

/** decodeUploadAck 验证 ACK 只推进当前已发送窗口，不能倒退或确认未来 bytes。 */
internal fun decodeUploadAck(payload: ByteArray, previousOffset: Long, sentOffset: Long): Terminal.FileTransferAck {
    val value = Terminal.FileTransferAck.parseFrom(payload)
    require(value.unknownFields.asMap().isEmpty()) { "upload ACK contains unknown fields" }
    require(value.offset in previousOffset..sentOffset && value.windowBytes > 0) { "upload ACK is invalid" }
    return value
}

/** verifyTransferFinish 验证下载最终大小和 SHA-256；actualDigest 来自本地完整内容。 */
internal fun verifyTransferFinish(payload: ByteArray, expectedSize: Long, receivedSize: Long, actualDigest: ByteArray) {
    val value = Terminal.FileTransferFinish.parseFrom(payload)
    require(value.unknownFields.asMap().isEmpty()) { "download finish contains unknown fields" }
    require(value.size == expectedSize && receivedSize == expectedSize) { "download size mismatch" }
    require(value.sha256.size() == 32 && actualDigest.contentEquals(value.sha256.toByteArray())) { "download SHA-256 mismatch" }
}

/** verifyUploadResult 验证 daemon 已原子提交与本地上传内容一致。 */
internal fun verifyUploadResult(payload: ByteArray, expectedSize: Long, expectedDigest: ByteArray): Terminal.FileTransferResult {
    val value = Terminal.FileTransferResult.parseFrom(payload)
    require(value.unknownFields.asMap().isEmpty()) { "upload result contains unknown fields" }
    require(value.size == expectedSize) { "upload result size mismatch" }
    require(value.sha256.size() == 32 && expectedDigest.contentEquals(value.sha256.toByteArray())) { "upload result SHA-256 mismatch" }
    return value
}
