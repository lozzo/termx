package com.termx.app.transport

import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.transfer.FileTransferManager
import org.webrtc.DataChannel
import org.webrtc.PeerConnection
import termx.protocol.wirepb.Terminal
import java.nio.ByteBuffer
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong

/**
 * ChannelManager 是 Android 单一 `protocol` DataChannel 的 owner。
 * E2E auth 消息先进入授权队列；CapabilityAccepted 后由本类完成一次 wire v4 Hello。
 * terminal/control 帧投影到 JS bridge，native 文件任务拥有的 transfer channel 则直接路由到 FileTransferManager。
 */
class ChannelManager(
    private val bridge: BridgeServer?,
    private val machineId: String,
) {
    companion object {
        private const val TAG = "TermxChannelMgr"
        private const val PROTOCOL_VERSION = 4
        private const val MAX_FRAME_SIZE = 4 shl 20
        private const val FRAME_HELLO = 0x00
        private const val FRAME_REQUEST = 0x01
        private const val FRAME_RESPONSE = 0x02
        private const val FRAME_ERROR = 0x04
        private const val FRAME_RESPONSE_BINARY = 0x05
        private const val FRAME_FILE_DATA = 0x21
        private const val FRAME_FILE_ACK = 0x22
        private const val FRAME_FILE_FINISH = 0x23
        private const val FRAME_FILE_RESULT = 0x24
    }

    var fileTransferManager: FileTransferManager? = null

    @Volatile private var protocolChannel: DataChannel? = null
    @Volatile private var protocolState = ProtocolState.AUTHORIZING
    @Volatile private var protocolFailure: Exception? = null
    @Volatile private var helloLatch = CountDownLatch(1)
    private val authorizationMessages = LinkedBlockingQueue<ByteArray>(8)
    private val nativeRequestSequence = AtomicLong(0)
    private val pendingNativeRequests = ConcurrentHashMap<Long, PendingNativeRequest>()
    private val nativeFileChannels = ConcurrentHashMap.newKeySet<Int>()
    private val earlyNativeFileFrames = ConcurrentHashMap<Int, ConcurrentLinkedQueue<DecodedTermxProtocolFrame>>()

    private val protocolLabel get() = "protocol:$machineId"

    private class PendingNativeRequest {
        val latch = CountDownLatch(1)
        @Volatile var error: Exception? = null
        @Volatile var result: ByteArray? = null
        @Volatile var claimsFileChannel = false
    }

    /** createInitialChannels 创建 daemon answerer 接受的唯一 ordered/reliable `protocol` DataChannel。 */
    fun createInitialChannels(peer: PeerConnection) {
        val channel = peer.createDataChannel("protocol", DataChannel.Init().apply { ordered = true })
            ?: throw IllegalStateException("protocol DataChannel could not be created")
        protocolChannel = channel
        setupProtocolChannel(channel)
    }

    /** waitChannelOpen 只等待 SCTP DataChannel open；它不代表 capability 或 termx protocol 已授权。 */
    fun waitChannelOpen(timeoutMs: Int): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            when (protocolChannel?.state()) {
                DataChannel.State.OPEN -> return true
                DataChannel.State.CLOSED, DataChannel.State.CLOSING -> return false
                else -> Thread.sleep(50)
            }
        }
        return false
    }

    /** receiveAuthorizationMessage 只在 protocol 激活前读取 daemon auth envelope。 */
    fun receiveAuthorizationMessage(timeoutMs: Long): ByteArray? {
        if (protocolState != ProtocolState.AUTHORIZING) return null
        return authorizationMessages.poll(timeoutMs, TimeUnit.MILLISECONDS)
    }

    /** sendAuthorizationMessage 把 CapabilityOpen 作为完整 binary DataChannel message 发送。 */
    fun sendAuthorizationMessage(frame: ByteArray): Boolean {
        if (protocolState != ProtocolState.AUTHORIZING || frame.isEmpty()) return false
        return sendDataChannelMessage(frame)
    }

    /**
     * activateTermxProtocol 是 auth 到 terminal protocol 的单向切换点。
     * 它发送唯一一次 wire v3 Hello；失败后当前 channel 不允许回到 auth 或旧 api/events 通道。
     */
    fun activateTermxProtocol(timeoutMs: Long): Boolean {
        if (protocolState == ProtocolState.READY) return true
        if (protocolState != ProtocolState.AUTHORIZING) return false
        protocolState = ProtocolState.WAITING_HELLO
        helloLatch = CountDownLatch(1)
        val hello = Terminal.Hello.newBuilder().setVersion(PROTOCOL_VERSION).setClient("termx-android").build()
        if (!sendProtocolFrame(0, FRAME_HELLO, hello.toByteArray())) {
            failProtocol(IllegalStateException("termx protocol Hello send failed"))
            return false
        }
        if (!helloLatch.await(timeoutMs, TimeUnit.MILLISECONDS)) {
            failProtocol(IllegalStateException("termx protocol Hello timed out"))
            return false
        }
        return protocolState == ProtocolState.READY && protocolFailure == null
    }

    /** sendRawProtocol 转发 JS multiplexer 已校验归属的 wire frame。 */
    fun sendRawProtocol(frame: ByteArray) {
        if (!isProtocolOpen()) throw IllegalStateException("termx protocol channel is not open")
        val decoded = decodeTermxProtocolFrame(frame)
        if (decoded.channel < 0) throw IllegalArgumentException("termx protocol channel is invalid")
        if (!sendDataChannelMessage(frame)) throw IllegalStateException("termx protocol send failed")
    }

    /** isProtocolOpen 表示 DTLS auth 与 wire Hello 均完成，不以 PeerConnection connected 状态代替。 */
    fun isProtocolOpen(): Boolean = protocolState == ProtocolState.READY && protocolChannel?.state() == DataChannel.State.OPEN

    /**
     * sendApiRequest 保留 native lifecycle 调用边界，但只允许 `/status` 映射为真实 wire `list` round trip。
     * 文件旧 API 不属于当前 daemon contract，必须失败而不能恢复 legacy DataChannel。
     */
    fun sendApiRequest(method: String, path: String, body: String?, timeoutMs: Long): String {
        if (method != "GET" || path != "/status" || !body.isNullOrBlank()) {
            throw UnsupportedOperationException("legacy runtime API is unavailable on termx protocol")
        }
        requestList(timeoutMs)
        return "{\"ok\":true}"
    }

    /** openFileDownload 在 native 注册 channel 前完成 control round trip，避免首个 data frame 早于 owner 建立。 */
    internal fun openFileDownload(path: String, offset: Long, expectedSize: Long, expectedModifiedAtUnixNano: Long): NativeFileTransferOpen {
        val params = Terminal.FileDownloadOpenParams.newBuilder()
            .setPath(path)
            .setOffset(offset)
            .setExpectedSize(expectedSize)
            .setExpectedModifiedAtUnixNano(expectedModifiedAtUnixNano)
            .build()
        return parseFileTransferOpen(requestProtocol("file.download.open", params.toByteArray(), 60_000, claimsFileChannel = true))
    }

    /** openFileUpload 由 native I/O owner 建立上传 transfer，并可提交 daemon 绑定的 resume id。 */
    internal fun openFileUpload(path: String, size: Long, overwrite: Boolean, resumeTransferId: String): NativeFileTransferOpen {
        val params = Terminal.FileUploadOpenParams.newBuilder()
            .setPath(path)
            .setSize(size)
            .setOverwrite(overwrite)
            .setResumeTransferId(resumeTransferId)
            .build()
        return parseFileTransferOpen(requestProtocol("file.upload.open", params.toByteArray(), 60_000, claimsFileChannel = true))
    }

    /** cancelFileTransfer 请求 owning daemon 幂等取消 transfer；本地状态仍由 FileTransferManager 收口。 */
    fun cancelFileTransfer(transferId: String): Boolean {
        val params = Terminal.FileTransferCancelParams.newBuilder().setTransferId(transferId).build()
        return Terminal.FileTransferCancelResult.parseFrom(
            requestProtocol("file.transfer.cancel", params.toByteArray(), 30_000),
        ).cancelled
    }

    /** sendFileFrame 把 native 状态机生成的 v4 stream payload 发送到 daemon 分配的 channel。 */
    fun sendFileFrame(channel: Int, type: Int, payload: ByteArray) {
        if (type != FRAME_FILE_DATA && type != FRAME_FILE_ACK && type != FRAME_FILE_FINISH) {
            throw IllegalArgumentException("unsupported outbound file frame")
        }
        if (!sendProtocolFrame(channel, type, payload)) throw IllegalStateException("termx file frame send failed")
    }

    /** bindNativeFileChannel 在 FileTransferManager 建立 session 后重放 open 响应之后抢先到达的有界帧。 */
    internal fun bindNativeFileChannel(channel: Int) {
        if (!nativeFileChannels.contains(channel)) throw IllegalStateException("native file channel was not claimed")
        val queued = earlyNativeFileFrames.remove(channel) ?: return
        while (true) {
            val frame = queued.poll() ?: break
            if (fileTransferManager?.onProtocolFrame(frame.channel, frame.type, frame.payload) != true) {
                throw IllegalStateException("native file channel has no transfer owner")
            }
        }
    }

    /** releaseNativeFileChannel 释放 session-local channel，后续同号 frame 不得再进入已结束任务。 */
    internal fun releaseNativeFileChannel(channel: Int) {
        nativeFileChannels.remove(channel)
        earlyNativeFileFrames.remove(channel)
    }

    /** closeAll 关闭当前 transport，并解除 auth、Hello 与 liveness wait。 */
    fun closeAll() {
        failProtocol(IllegalStateException("protocol disconnected"))
        protocolChannel = null
    }

    private fun requestList(timeoutMs: Long) {
        // 旧 ListResult 已随 API Proto 收口删除；迁移前 readiness probe 只以完整成功 response 为准。
        requestProtocol("list", Terminal.Empty.getDefaultInstance().toByteArray(), timeoutMs)
    }

    private fun requestProtocol(method: String, params: ByteArray, timeoutMs: Long, claimsFileChannel: Boolean = false): ByteArray {
        if (!isProtocolOpen()) throw IllegalStateException("termx protocol channel is not open")
        val id = Long.MAX_VALUE - nativeRequestSequence.incrementAndGet()
        val pending = PendingNativeRequest()
        pending.claimsFileChannel = claimsFileChannel
        pendingNativeRequests[id] = pending
        val request = Terminal.RequestEnvelope.newBuilder()
            .setId(id)
            .setMethod(method)
            .setParams(com.google.protobuf.ByteString.copyFrom(params))
            .build()
        if (!sendProtocolFrame(0, FRAME_REQUEST, request.toByteArray())) {
            pendingNativeRequests.remove(id)
            throw IllegalStateException("termx protocol $method send failed")
        }
        if (!pending.latch.await(timeoutMs, TimeUnit.MILLISECONDS)) {
            pendingNativeRequests.remove(id)
            throw IllegalStateException("termx protocol $method timed out")
        }
        pending.error?.let { throw it }
        return pending.result ?: throw IllegalStateException("termx protocol $method returned no result")
    }

    private fun parseFileTransferOpen(bytes: ByteArray): NativeFileTransferOpen {
        val result = Terminal.FileTransferOpenResult.parseFrom(bytes)
        if (result.unknownFields.asMap().isNotEmpty() || result.transferId.isBlank() || result.channel == 0 || result.chunkBytes <= 0 || result.windowBytes <= 0) {
            throw IllegalStateException("termx file transfer open result is invalid")
        }
        return NativeFileTransferOpen(
            result.transferId,
            result.channel,
            result.path,
            result.offset,
            result.size,
            result.modifiedAtUnixNano,
            result.windowBytes,
            result.chunkBytes,
        )
    }

    private fun setupProtocolChannel(channel: DataChannel) {
        channel.registerObserver(object : DataChannel.Observer {
            override fun onBufferedAmountChange(previousAmount: Long) = Unit

            override fun onStateChange() {
                Log.i(TAG, "protocol state: ${channel.state()} [$machineId]")
                if ((channel.state() == DataChannel.State.CLOSED || channel.state() == DataChannel.State.CLOSING) &&
                    protocolState != ProtocolState.FAILED) {
                    failProtocol(IllegalStateException("protocol DataChannel closed"), closeChannel = false)
                }
            }

            override fun onMessage(buffer: DataChannel.Buffer) {
                if (!buffer.binary) {
                    failProtocol(IllegalStateException("termx protocol requires binary DataChannel messages"))
                    return
                }
                val data = ByteArray(buffer.data.remaining())
                buffer.data.get(data)
                if (protocolState == ProtocolState.AUTHORIZING) {
                    if (!authorizationMessages.offer(data)) {
                        failProtocol(IllegalStateException("authorization message queue overflow"))
                    }
                    return
                }
                if (protocolState == ProtocolState.FAILED) return
                handleProtocolMessage(data)
            }
        })
    }

    private fun handleProtocolMessage(frameBytes: ByteArray) {
        if (protocolState == ProtocolState.WAITING_HELLO) {
            try {
                validateTermxProtocolHelloFrame(frameBytes, PROTOCOL_VERSION)
                protocolState = ProtocolState.READY
            } catch (failure: Exception) {
                failProtocol(failure)
            } finally {
                helloLatch.countDown()
            }
            return
        }
        if (protocolState != ProtocolState.READY) return
        val frame = try {
            decodeTermxProtocolFrame(frameBytes)
        } catch (failure: Exception) {
            failProtocol(failure)
            return
        }
        if (frame.channel == 0 && frame.type == FRAME_HELLO) {
            failProtocol(IllegalStateException("daemon sent a duplicate termx protocol Hello"))
            return
        }
        var handledNativeControl = false
        if (frame.channel == 0 && (frame.type == FRAME_RESPONSE || frame.type == FRAME_RESPONSE_BINARY)) {
            runCatching {
                val response = Terminal.ResponseEnvelope.parseFrom(frame.payload)
                if (response.unknownFields.asMap().isNotEmpty()) throw IllegalStateException("termx protocol response contains unknown fields")
                pendingNativeRequests.remove(response.id)?.let { pending ->
                    handledNativeControl = true
                    val resultBytes = response.result.toByteArray()
                    if (pending.claimsFileChannel) {
                        val opened = parseFileTransferOpen(resultBytes)
                        nativeFileChannels.add(opened.channel)
                    }
                    pending.result = resultBytes
                    pending.latch.countDown()
                }
            }.onFailure { failProtocol(it as? Exception ?: IllegalStateException(it.message)) }
        } else if (frame.channel == 0 && frame.type == FRAME_ERROR) {
            runCatching {
                val response = Terminal.ErrorEnvelope.parseFrom(frame.payload)
                if (response.unknownFields.asMap().isNotEmpty() || response.error.unknownFields.asMap().isNotEmpty()) {
                    throw IllegalStateException("termx protocol error response contains unknown fields")
                }
                pendingNativeRequests.remove(response.id)?.let { pending ->
                    handledNativeControl = true
                    pending.error = IllegalStateException(response.error.message.ifBlank { "termx protocol request failed" })
                    pending.latch.countDown()
                }
            }.onFailure { failProtocol(it as? Exception ?: IllegalStateException(it.message)) }
        }
        if (handledNativeControl) return
        if (frame.channel != 0 && nativeFileChannels.contains(frame.channel)) {
            if (fileTransferManager?.onProtocolFrame(frame.channel, frame.type, frame.payload) != true) {
                val queue = earlyNativeFileFrames.getOrPut(frame.channel) { ConcurrentLinkedQueue() }
                if (queue.size >= 8) {
                    failProtocol(IllegalStateException("native file early frame queue overflow"))
                    return
                }
                queue.add(frame)
            }
            return
        }
        if (protocolState == ProtocolState.READY) {
            val channelId = bridge?.getChannelId(protocolLabel) ?: -1
            if (channelId >= 0) bridge?.sendDataFrame(channelId, frameBytes)
        }
    }

    private fun sendProtocolFrame(channel: Int, type: Int, payload: ByteArray): Boolean {
        if (channel !in 0..0xffff || payload.size > MAX_FRAME_SIZE) return false
        val frame = ByteBuffer.allocate(7 + payload.size)
            .putShort(channel.toShort())
            .put(type.toByte())
            .putInt(payload.size)
            .put(payload)
            .array()
        return sendDataChannelMessage(frame)
    }

    private fun sendDataChannelMessage(data: ByteArray): Boolean {
        val channel = protocolChannel ?: return false
        if (channel.state() != DataChannel.State.OPEN) return false
        return channel.send(DataChannel.Buffer(ByteBuffer.wrap(data), true))
    }

    private fun failProtocol(failure: Exception, closeChannel: Boolean = true) {
        protocolFailure = failure
        protocolState = ProtocolState.FAILED
        helloLatch.countDown()
        authorizationMessages.clear()
        for (pending in pendingNativeRequests.values) {
            pending.error = failure
            pending.latch.countDown()
        }
        pendingNativeRequests.clear()
        if (closeChannel) protocolChannel?.let { runCatching { it.close() } }
    }

    private enum class ProtocolState { AUTHORIZING, WAITING_HELLO, READY, FAILED }
}

private const val TERMX_PROTOCOL_FRAME_HEADER_SIZE = 7
private const val TERMX_PROTOCOL_MAX_FRAME_SIZE = 4 shl 20
private const val TERMX_PROTOCOL_HELLO_FRAME = 0x00

/** NativeFileTransferOpen 是 daemon 文件 transfer 分配结果；channel 只在当前 protocol session 内有效。 */
internal data class NativeFileTransferOpen(
    val transferId: String,
    val channel: Int,
    val path: String,
    val offset: Long,
    val size: Long,
    val modifiedAtUnixNano: Long,
    val windowBytes: Long,
    val chunkBytes: Int,
)

/** DecodedTermxProtocolFrame 是 Android transport 对公开 wire frame header 的严格投影。 */
internal data class DecodedTermxProtocolFrame(val channel: Int, val type: Int, val payload: ByteArray)

/** decodeTermxProtocolFrame 严格校验完整 DataChannel message；尾随或截断 bytes 都是当前 endpoint 的协议失败。 */
internal fun decodeTermxProtocolFrame(frame: ByteArray): DecodedTermxProtocolFrame {
    if (frame.size < TERMX_PROTOCOL_FRAME_HEADER_SIZE) throw IllegalArgumentException("termx protocol frame is too short")
    val buffer = ByteBuffer.wrap(frame)
    val channel = buffer.short.toInt() and 0xffff
    val type = buffer.get().toInt() and 0xff
    val length = buffer.int
    if (length < 0 || length > TERMX_PROTOCOL_MAX_FRAME_SIZE || length != buffer.remaining()) {
        throw IllegalArgumentException("termx protocol frame length is invalid")
    }
    return DecodedTermxProtocolFrame(channel, type, ByteArray(length).also(buffer::get))
}

/** validateTermxProtocolHelloFrame 固定 auth 后只接受 control channel 上无未知字段的目标版本 Hello。 */
internal fun validateTermxProtocolHelloFrame(frameBytes: ByteArray, expectedVersion: Int) {
    val frame = decodeTermxProtocolFrame(frameBytes)
    if (frame.channel != 0 || frame.type != TERMX_PROTOCOL_HELLO_FRAME) {
        throw IllegalStateException("daemon did not answer with termx protocol Hello")
    }
    val hello = Terminal.Hello.parseFrom(frame.payload)
    if (hello.version != expectedVersion || hello.unknownFields.asMap().isNotEmpty()) {
        throw IllegalStateException("unsupported termx protocol Hello")
    }
}
