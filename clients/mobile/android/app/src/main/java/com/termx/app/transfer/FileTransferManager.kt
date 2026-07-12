package com.termx.app.transfer

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.os.Handler
import android.os.HandlerThread
import android.provider.MediaStore
import android.util.Log
import com.termx.app.transport.NativeFileTransferOpen
import com.termx.app.transport.WebRTCTransport
import org.json.JSONArray
import org.json.JSONObject
import termx.protocol.wirepb.Terminal
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.locks.ReentrantLock
import java.util.concurrent.locks.Condition

/**
 * FileTransferManager 是 Android 文件任务与本地文件 I/O 的 owner。
 * daemon 文件系统是远端 truth；本类只持有任务投影、部分文件和当前 protocol-session channel。
 */
class FileTransferManager(context: Context) {
    companion object {
        private const val TAG = "TermxFileTransfer"
        private const val FRAME_ERROR = 0x04
        private const val FRAME_FILE_DATA = 0x21
        private const val FRAME_FILE_ACK = 0x22
        private const val FRAME_FILE_FINISH = 0x23
        private const val FRAME_FILE_RESULT = 0x24
        private const val SUB_DIR = "termx"
        private const val DEFAULT_CHUNK_BYTES = 64 * 1024
        private const val PROGRESS_THROTTLE_MS = 200L
        private const val RESUME_CLEANUP_AGE_MS = 7L * 24 * 60 * 60 * 1000
    }

    fun interface SyncListener { fun onTransferUpdated(allSnapshots: JSONObject) }

    private data class DownloadSession(
        var taskId: String,
        var transferId: String = "",
        var channel: Int = 0,
        var fileName: String,
        var filePath: String,
        var totalSize: Long,
        var modifiedAtUnixNano: Long = 0,
        var receivedSize: Long = 0,
        var windowBytes: Long = 0,
        var chunkBytes: Int = DEFAULT_CHUNK_BYTES,
        var bytesSinceAck: Long = 0,
        var status: String = "pending",
        var error: String? = null,
        var startedAt: Long = System.currentTimeMillis(),
        var machineId: String,
        var stream: OutputStream? = null,
        var digest: MessageDigest? = null,
        var partFile: File? = null,
        var mediaStoreUri: Uri? = null,
        var savedPath: String? = null,
        var lastPersistAt: Long = 0,
        var pausedByUser: Boolean = false,
        var transport: WebRTCTransport? = null,
    )

    private data class UploadSession(
        var taskId: String,
        var transferId: String = "",
        var channel: Int = 0,
        var contentUri: String,
        var fileName: String,
        var fileSize: Long,
        var sentSize: Long = 0,
        var acknowledgedOffset: Long = 0,
        var windowBytes: Long = 0,
        var chunkBytes: Int = DEFAULT_CHUNK_BYTES,
        var targetDir: String,
        var targetPath: String,
        var status: String = "pending",
        var error: String? = null,
        var startedAt: Long = System.currentTimeMillis(),
        var machineId: String,
        var lastPersistAt: Long = 0,
        var pausedByUser: Boolean = false,
        @Volatile var cancelled: Boolean = false,
        val ackLock: ReentrantLock = ReentrantLock(),
        val ackCondition: Condition = ackLock.newCondition(),
        var expectedDigest: ByteArray? = null,
        var transport: WebRTCTransport? = null,
    )

    private val context = context.applicationContext
    private val transferPartDirectory = ensureTransferPartDirectory(this.context.filesDir)
    private val taskStore = TransferTaskStore(this.context)
    private val downloadSessions = ConcurrentHashMap<String, DownloadSession>()
    private val uploadSessions = ConcurrentHashMap<String, UploadSession>()
    private val downloadByChannel = ConcurrentHashMap<Int, DownloadSession>()
    private val uploadByChannel = ConcurrentHashMap<Int, UploadSession>()
    private val ioThread = HandlerThread("TermxFileTransfer-IO").apply { start() }
    private val ioHandler = Handler(ioThread.looper)
    private var lastProgressNotifyTime = 0L
    @Volatile private var lastTransferActivityAt = 0L
    @Volatile var transportRef: WebRTCTransport? = null
    var syncListener: SyncListener? = null

    init {
        restorePersistedTransfers()
        cleanupOldResumeFiles()
    }

    /** startDownload 创建稳定本地 task，并由 native 发起 download.open，确保首帧不会越过 channel owner。 */
    fun startDownload(
        transport: WebRTCTransport,
        taskId: String,
        fileName: String,
        fileSize: Long,
        filePath: String,
        offset: Long = 0,
        storeKey: String = "",
    ) {
        val machineId = storeKey.ifBlank { transport.machineId }
        val existing = downloadSessions[taskId]
        if (existing != null) {
            resumeDownload(transport, taskId)
            return
        }
        val session = DownloadSession(
            taskId = taskId,
            fileName = fileName,
            filePath = filePath,
            totalSize = fileSize,
            receivedSize = offset.coerceIn(0, fileSize),
            machineId = machineId,
        )
        downloadSessions[taskId] = session
        persistDownload(session)
        notifySyncListener()
        transportRef = transport
        ioHandler.post { openDownload(transport, session) }
    }

    /** startUpload 保留 content URI，native 自行计算摘要并按 daemon window 发送。 */
    fun startUpload(
        transport: WebRTCTransport,
        contentUri: String,
        fileName: String,
        fileSize: Long,
        targetDir: String,
        storeKey: String = "",
    ) {
        val machineId = storeKey.ifBlank { transport.machineId }
        val targetPath = if (targetDir.endsWith('/')) "$targetDir$fileName" else "$targetDir/$fileName"
        val taskId = uploadTaskId(machineId, contentUri, fileName, fileSize, targetDir)
        val existing = uploadSessions[taskId]
        if (existing != null) {
            resumeUpload(transport, taskId)
            return
        }
        val session = UploadSession(
            taskId = taskId,
            contentUri = contentUri,
            fileName = fileName,
            fileSize = fileSize,
            targetDir = targetDir,
            targetPath = targetPath,
            machineId = machineId,
        )
        uploadSessions[taskId] = session
        persistUpload(session)
        notifySyncListener()
        transportRef = transport
        Thread({ openUpload(transport, session) }, "TermxUpload-Open").start()
    }

    /** onProtocolFrame 只消费已经由 ChannelManager 声明为 native-owned 的文件 channel。 */
    fun onProtocolFrame(channel: Int, type: Int, payload: ByteArray): Boolean {
        downloadByChannel[channel]?.let { session ->
            when (type) {
                FRAME_FILE_DATA -> ioHandler.post { receiveDownloadData(session, payload) }
                FRAME_FILE_FINISH -> ioHandler.post { finishDownload(session, payload) }
                FRAME_ERROR -> ioHandler.post { failDownload(session, protocolError(payload)) }
                else -> ioHandler.post { failDownload(session, "unexpected download frame type $type") }
            }
            return true
        }
        uploadByChannel[channel]?.let { session ->
            when (type) {
                FRAME_FILE_ACK -> receiveUploadAck(session, payload)
                FRAME_FILE_RESULT -> ioHandler.post { finishUpload(session, payload) }
                FRAME_ERROR -> ioHandler.post { failUpload(session, protocolError(payload)) }
                else -> ioHandler.post { failUpload(session, "unexpected upload frame type $type") }
            }
            return true
        }
        return false
    }

    fun cancelDownload(taskId: String) { downloadSessions[taskId]?.let { ioHandler.post { cancelDownloadSession(it) } } }
    fun pauseDownload(taskId: String) { downloadSessions[taskId]?.let { ioHandler.post { pauseDownloadSession(it, true, null) } } }
    fun resumeDownload(transport: WebRTCTransport, taskId: String) {
        downloadSessions[taskId]?.let { session ->
            if (session.status != "completed" && session.status != "transferring") ioHandler.post { openDownload(transport, session) }
        }
    }
    fun cancelUpload(taskId: String) { uploadSessions[taskId]?.let { cancelUploadSession(it, "用户取消") } }
    fun pauseUpload(taskId: String) { uploadSessions[taskId]?.let { pauseUploadSession(it, "用户暂停") } }
    fun resumeUpload(transport: WebRTCTransport, taskId: String) {
        uploadSessions[taskId]?.let { session ->
            if (session.status != "completed" && session.status != "transferring") {
                session.cancelled = false
                Thread({ openUpload(transport, session) }, "TermxUpload-Resume").start()
            }
        }
    }

    fun clearTransfer(taskId: String) {
        downloadSessions.remove(taskId)?.let { releaseDownload(it); taskStore.delete(taskId) }
        uploadSessions.remove(taskId)?.let { releaseUpload(it); taskStore.delete(taskId) }
        notifySyncListener()
    }

    fun resumeAllForMachine(machineId: String, transport: WebRTCTransport?) {
        if (transport == null) return
        downloadSessions.values.filter { it.machineId == machineId && resumable(it.status) && !it.pausedByUser }.forEach { openDownload(transport, it) }
        uploadSessions.values.filter { it.machineId == machineId && resumable(it.status) && !it.pausedByUser }.forEach { openUpload(transport, it) }
    }

    fun transferMachineIds(): Set<String> = (downloadSessions.values.map { it.machineId } + uploadSessions.values.map { it.machineId }).toSet()
    fun isHandling(taskId: String): Boolean = downloadSessions[taskId]?.status == "transferring" || uploadSessions[taskId]?.status == "transferring"
    fun hasActiveTransfers(): Boolean = downloadSessions.values.any { it.status == "transferring" } || uploadSessions.values.any { it.status == "transferring" }
    fun hasRecentTransferActivity(windowMs: Long): Boolean = hasActiveTransfers() && lastTransferActivityAt > 0 && System.currentTimeMillis() - lastTransferActivityAt <= windowMs

    /** onTransportLost 把当前 session channel 全部失效；部分文件和 daemon resume id 保留为恢复依据。 */
    fun onTransportLost(machineId: String) {
        downloadSessions.values.filter { it.machineId == machineId && it.status == "transferring" }.forEach { session ->
            session.stream?.runCatching { flush(); close() }
            session.stream = null
            session.status = "paused"
            session.error = "等待连接恢复"
            session.pausedByUser = false
            if (session.channel != 0) downloadByChannel.remove(session.channel)
            session.channel = 0
            persistDownload(session)
        }
        uploadSessions.values.filter { it.machineId == machineId && it.status == "transferring" }.forEach { session ->
            session.cancelled = true
            signalUpload(session)
            session.status = "paused"
            session.error = "等待连接恢复"
            session.pausedByUser = false
            if (session.channel != 0) uploadByChannel.remove(session.channel)
            session.channel = 0
            persistUpload(session)
        }
        if (transportRef?.machineId == machineId) transportRef = null
        notifySyncListener()
    }

    /** resumeInterruptedTransfers 只恢复 transport loss 导致的暂停，用户主动暂停保持不动。 */
    fun resumeInterruptedTransfers(transport: WebRTCTransport) {
        transportRef = transport
        downloadSessions.values.filter { it.machineId == transport.machineId && it.status == "paused" && !it.pausedByUser }
            .forEach { ioHandler.post { openDownload(transport, it) } }
        uploadSessions.values.filter { it.machineId == transport.machineId && it.status == "paused" && !it.pausedByUser }
            .forEach { session -> session.cancelled = false; Thread({ openUpload(transport, session) }, "TermxUpload-Reconnect").start() }
    }

    fun getDownloadResumeOffset(machineId: String, filePath: String, fileSize: Long): Long {
        if (fileSize <= 0) return 0
        val part = downloadPartFile(machineId, filePath, fileSize)
        return part.length().takeIf { it in 1 until fileSize } ?: 0
    }

    fun getTransferSnapshots(): JSONObject {
        val transfers = JSONArray()
        downloadSessions.values.forEach { session ->
            transfers.put(JSONObject()
                .put("id", session.taskId).put("machineId", session.machineId).put("name", session.fileName)
                .put("direction", "download").put("totalSize", session.totalSize).put("transferredSize", session.receivedSize)
                .put("status", session.status).put("startedAt", session.startedAt).put("updatedAt", System.currentTimeMillis())
                .put("filePath", session.filePath)
                .apply { session.savedPath?.let { put("savedPath", it) }; session.mediaStoreUri?.let { put("savedUri", it.toString()) }; session.error?.let { put("error", it) } })
        }
        uploadSessions.values.forEach { session ->
            transfers.put(JSONObject()
                .put("id", session.taskId).put("machineId", session.machineId).put("name", session.fileName)
                .put("direction", "upload").put("totalSize", session.fileSize).put("transferredSize", session.sentSize)
                .put("status", session.status).put("startedAt", session.startedAt).put("updatedAt", System.currentTimeMillis())
                .put("localUri", session.contentUri).put("targetDir", session.targetDir)
                .apply { session.error?.let { put("error", it) } })
        }
        return JSONObject().put("transfers", transfers)
    }

    private fun openDownload(transport: WebRTCTransport, session: DownloadSession) {
        try {
            releaseDownloadChannel(session)
            val part = downloadPartFile(session.machineId, session.filePath, session.totalSize)
            val offset = part.length().coerceIn(0, session.totalSize)
            val opened = transport.channelManager.openFileDownload(session.filePath, offset, session.totalSize, session.modifiedAtUnixNano)
            session.transferId = opened.transferId
            session.transport = transport
            session.channel = opened.channel
            session.totalSize = opened.size
            session.modifiedAtUnixNano = opened.modifiedAtUnixNano
            session.receivedSize = opened.offset
            session.windowBytes = opened.windowBytes
            session.chunkBytes = opened.chunkBytes
            session.bytesSinceAck = 0
            session.partFile = part
            session.stream = FileOutputStream(part, opened.offset > 0)
            session.digest = digestExistingPrefix(part, opened.offset)
            session.status = "transferring"
            session.error = null
            session.pausedByUser = false
            downloadByChannel[opened.channel] = session
            persistDownload(session)
            markActivity()
            notifySyncListener()
            transport.channelManager.bindNativeFileChannel(opened.channel)
        } catch (failure: Exception) {
            failDownload(session, failure.message ?: "下载初始化失败")
        }
    }

    private fun receiveDownloadData(session: DownloadSession, payload: ByteArray) {
        if (session.status != "transferring") return
        try {
            val data = decodeDownloadChunk(payload, session.receivedSize, session.chunkBytes)
            val bytes = data.data
            session.stream?.write(bytes) ?: throw IllegalStateException("download output is closed")
            session.digest?.update(bytes)
            session.receivedSize += bytes.size
            session.bytesSinceAck += bytes.size
            if (session.bytesSinceAck >= session.windowBytes) {
                val ack = Terminal.FileTransferAck.newBuilder().setOffset(session.receivedSize).setWindowBytes(session.bytesSinceAck).build()
                session.transport?.channelManager?.sendFileFrame(session.channel, FRAME_FILE_ACK, ack.toByteArray())
                    ?: throw IllegalStateException("download transport is missing")
                session.bytesSinceAck = 0
            }
            persistDownloadThrottled(session)
            progressNotify()
            markActivity()
        } catch (failure: Exception) {
            failDownload(session, failure.message ?: "下载数据无效")
        }
    }

    private fun finishDownload(session: DownloadSession, payload: ByteArray) {
        if (session.status != "transferring") return
        try {
            val actual = session.digest?.digest() ?: throw IllegalStateException("download digest is missing")
            verifyTransferFinish(payload, session.totalSize, session.receivedSize, actual)
            session.stream?.flush()
            session.stream?.close()
            session.stream = null
            val part = session.partFile ?: throw IllegalStateException("download part file is missing")
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) publishPartToMediaStore(session, part) else publishPartToLegacyDownloads(session, part)
            session.status = "completed"
            session.error = null
            releaseDownloadChannel(session)
            persistDownload(session)
            notifySyncListener()
        } catch (failure: Exception) {
            failDownload(session, failure.message ?: "下载完成校验失败")
        }
    }

    private fun openUpload(transport: WebRTCTransport, session: UploadSession) {
        try {
            releaseUploadChannel(session)
            session.expectedDigest = digestContentUri(session.contentUri)
            val resumeId = session.transferId
            val opened = transport.channelManager.openFileUpload(session.targetPath, session.fileSize, true, resumeId)
            session.transferId = opened.transferId
            session.transport = transport
            session.channel = opened.channel
            session.sentSize = opened.offset
            session.acknowledgedOffset = opened.offset
            session.windowBytes = opened.windowBytes
            session.chunkBytes = opened.chunkBytes
            session.status = "transferring"
            session.error = null
            session.pausedByUser = false
            session.cancelled = false
            uploadByChannel[opened.channel] = session
            persistUpload(session)
            notifySyncListener()
            transportRef = transport
            transport.channelManager.bindNativeFileChannel(opened.channel)
            Thread({ sendUpload(session) }, "TermxUpload-Data").start()
        } catch (failure: Exception) {
            failUpload(session, failure.message ?: "上传初始化失败")
        }
    }

    private fun sendUpload(session: UploadSession) {
        try {
            context.contentResolver.openInputStream(Uri.parse(session.contentUri)).use { input ->
                if (input == null) throw IllegalStateException("无法打开上传文件")
                skipFully(input, session.sentSize)
                val buffer = ByteArray(session.chunkBytes)
                while (session.sentSize < session.fileSize && !session.cancelled) {
                    waitForUploadWindow(session)
                    val count = input.read(buffer, 0, minOf(buffer.size.toLong(), session.fileSize - session.sentSize).toInt())
                    if (count <= 0) throw IllegalStateException("上传源文件提前结束")
                    val payload = Terminal.FileTransferData.newBuilder()
                        .setOffset(session.sentSize)
                        .setData(com.google.protobuf.ByteString.copyFrom(buffer, 0, count))
                        .build()
                    session.transport?.channelManager?.sendFileFrame(session.channel, FRAME_FILE_DATA, payload.toByteArray())
                        ?: throw IllegalStateException("上传 transport 丢失")
                    session.sentSize += count
                    persistUploadThrottled(session)
                    progressNotify()
                    markActivity()
                }
            }
            if (session.cancelled) return
            val digest = session.expectedDigest ?: throw IllegalStateException("上传摘要缺失")
            val finish = Terminal.FileTransferFinish.newBuilder().setSize(session.fileSize)
                .setSha256(com.google.protobuf.ByteString.copyFrom(digest)).build()
            session.transport?.channelManager?.sendFileFrame(session.channel, FRAME_FILE_FINISH, finish.toByteArray())
                ?: throw IllegalStateException("上传 transport 丢失")
        } catch (failure: Exception) {
            if (!session.cancelled) failUpload(session, failure.message ?: "上传失败")
        }
    }

    private fun receiveUploadAck(session: UploadSession, payload: ByteArray) {
        try {
            val ack = decodeUploadAck(payload, session.acknowledgedOffset, session.sentSize)
            session.acknowledgedOffset = ack.offset
            session.windowBytes = ack.windowBytes
            signalUpload(session)
        } catch (failure: Exception) {
            failUpload(session, failure.message ?: "上传 ACK 无效")
        }
    }

    private fun waitForUploadWindow(session: UploadSession) {
        session.ackLock.lock()
        try {
            while (!session.cancelled && session.sentSize - session.acknowledgedOffset >= session.windowBytes) {
                session.ackCondition.awaitNanos(5_000_000_000)
                if (session.sentSize - session.acknowledgedOffset >= session.windowBytes && session.status != "transferring") return
            }
        } finally {
            session.ackLock.unlock()
        }
    }

    private fun finishUpload(session: UploadSession, payload: ByteArray) {
        if (session.status != "transferring") return
        try {
            val expected = session.expectedDigest ?: throw IllegalStateException("上传摘要缺失")
            verifyUploadResult(payload, session.fileSize, expected)
            session.sentSize = session.fileSize
            session.status = "completed"
            session.error = null
            releaseUploadChannel(session)
            persistUpload(session)
            notifySyncListener()
        } catch (failure: Exception) {
            failUpload(session, failure.message ?: "上传完成校验失败")
        }
    }

    private fun pauseDownloadSession(session: DownloadSession, byUser: Boolean, reason: String?) {
        if (session.status == "completed") return
        runCatching { session.transport?.channelManager?.cancelFileTransfer(session.transferId) }
        session.stream?.runCatching { flush(); close() }
        session.stream = null
        session.status = "paused"
        session.error = reason
        session.pausedByUser = byUser
        releaseDownloadChannel(session)
        persistDownload(session)
        notifySyncListener()
    }

    private fun cancelDownloadSession(session: DownloadSession) {
        pauseDownloadSession(session, true, "用户取消")
        session.status = "cancelled"
        session.partFile?.delete()
        persistDownload(session)
        notifySyncListener()
    }

    private fun cancelUploadSession(session: UploadSession, reason: String) {
        session.cancelled = true
        signalUpload(session)
        runCatching { session.transport?.channelManager?.cancelFileTransfer(session.transferId) }
        session.status = "cancelled"
        session.error = reason
        releaseUploadChannel(session)
        persistUpload(session)
        notifySyncListener()
    }

    private fun pauseUploadSession(session: UploadSession, reason: String) {
        session.cancelled = true
        signalUpload(session)
        runCatching { session.transport?.channelManager?.cancelFileTransfer(session.transferId) }
        session.status = "paused"
        session.error = reason
        session.pausedByUser = true
        releaseUploadChannel(session)
        persistUpload(session)
        notifySyncListener()
    }

    private fun failDownload(session: DownloadSession, message: String) {
        if (session.status == "completed") return
        session.stream?.runCatching { close() }
        session.stream = null
        session.status = "failed"
        session.error = message
        releaseDownloadChannel(session)
        persistDownload(session)
        notifySyncListener()
        Log.w(TAG, "download ${session.taskId} failed: $message")
    }

    private fun failUpload(session: UploadSession, message: String) {
        if (session.status == "completed") return
        session.cancelled = true
        signalUpload(session)
        session.status = "failed"
        session.error = message
        releaseUploadChannel(session)
        persistUpload(session)
        notifySyncListener()
        Log.w(TAG, "upload ${session.taskId} failed: $message")
    }

    private fun releaseDownload(session: DownloadSession) { session.stream?.runCatching { close() }; releaseDownloadChannel(session) }
    private fun releaseUpload(session: UploadSession) { session.cancelled = true; releaseUploadChannel(session) }
    private fun signalUpload(session: UploadSession) {
        session.ackLock.lock()
        try { session.ackCondition.signalAll() } finally { session.ackLock.unlock() }
    }
    private fun releaseDownloadChannel(session: DownloadSession) {
        if (session.channel != 0) {
            downloadByChannel.remove(session.channel)
            session.transport?.channelManager?.releaseNativeFileChannel(session.channel)
            session.channel = 0
            session.transport = null
        }
    }
    private fun releaseUploadChannel(session: UploadSession) {
        if (session.channel != 0) {
            uploadByChannel.remove(session.channel)
            session.transport?.channelManager?.releaseNativeFileChannel(session.channel)
            session.channel = 0
            session.transport = null
        }
    }

    private fun digestExistingPrefix(file: File, length: Long): MessageDigest {
        val digest = MessageDigest.getInstance("SHA-256")
        if (length <= 0) return digest
        FileInputStream(file).use { input ->
            val buffer = ByteArray(DEFAULT_CHUNK_BYTES)
            var remaining = length
            while (remaining > 0) {
                val count = input.read(buffer, 0, minOf(buffer.size.toLong(), remaining).toInt())
                if (count <= 0) throw IllegalStateException("download resume prefix is incomplete")
                digest.update(buffer, 0, count)
                remaining -= count
            }
        }
        return digest
    }

    private fun digestContentUri(uriText: String): ByteArray {
        val digest = MessageDigest.getInstance("SHA-256")
        context.contentResolver.openInputStream(Uri.parse(uriText)).use { input ->
            if (input == null) throw IllegalStateException("无法打开上传文件")
            val buffer = ByteArray(DEFAULT_CHUNK_BYTES)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                if (count > 0) digest.update(buffer, 0, count)
            }
        }
        return digest.digest()
    }

    private fun skipFully(input: InputStream, offset: Long) {
        var remaining = offset
        while (remaining > 0) {
            val skipped = input.skip(remaining)
            if (skipped > 0) remaining -= skipped else if (input.read() < 0) throw IllegalStateException("上传续传位置超出源文件") else remaining--
        }
    }

    private fun protocolError(payload: ByteArray): String = runCatching {
        Terminal.ErrorEnvelope.parseFrom(payload).error.message.ifBlank { "termx protocol error" }
    }.getOrDefault("termx protocol error")

    private fun persistDownload(session: DownloadSession) {
        session.lastPersistAt = System.currentTimeMillis()
        taskStore.upsert(PersistedTransfer(session.taskId, "download", session.machineId, session.fileName, session.totalSize,
            session.receivedSize, session.status, session.startedAt, System.currentTimeMillis(), session.filePath, "", "", session.transferId,
            DEFAULT_CHUNK_BYTES, session.savedPath, session.mediaStoreUri?.toString(), session.error, session.pausedByUser))
    }
    private fun persistUpload(session: UploadSession) {
        session.lastPersistAt = System.currentTimeMillis()
        taskStore.upsert(PersistedTransfer(session.taskId, "upload", session.machineId, session.fileName, session.fileSize,
            session.sentSize, session.status, session.startedAt, System.currentTimeMillis(), session.targetPath, session.contentUri,
            session.targetDir, session.transferId, session.chunkBytes, null, null, session.error, session.pausedByUser))
    }
    private fun persistDownloadThrottled(session: DownloadSession) { if (System.currentTimeMillis() - session.lastPersistAt >= PROGRESS_THROTTLE_MS) persistDownload(session) }
    private fun persistUploadThrottled(session: UploadSession) { if (System.currentTimeMillis() - session.lastPersistAt >= PROGRESS_THROTTLE_MS) persistUpload(session) }

    private fun restorePersistedTransfers() {
        taskStore.loadAll().forEach { record ->
            if (record.direction == "download") {
                val part = downloadPartFile(record.machineId, record.remotePath, record.totalSize)
                downloadSessions[record.id] = DownloadSession(record.id, fileName = record.name, filePath = record.remotePath,
                    totalSize = record.totalSize, receivedSize = part.length().coerceIn(0, record.totalSize), status = startupStatus(record),
                    error = record.error, startedAt = record.startedAt, machineId = record.machineId, partFile = part,
                    mediaStoreUri = record.savedUri?.let { Uri.parse(it) }, savedPath = record.savedPath, pausedByUser = true)
            } else if (record.direction == "upload") {
                uploadSessions[record.id] = UploadSession(record.id, transferId = record.resumeId, contentUri = record.localUri,
                    fileName = record.name, fileSize = record.totalSize, sentSize = record.transferredSize.coerceIn(0, record.totalSize),
                    targetDir = record.targetDir, targetPath = record.remotePath, status = startupStatus(record), error = record.error,
                    startedAt = record.startedAt, machineId = record.machineId, pausedByUser = true, cancelled = true)
            }
        }
    }

    private fun startupStatus(record: PersistedTransfer): String = when {
        record.status == "completed" -> "completed"
        record.status == "cancelled" -> "cancelled"
        else -> "paused"
    }
    private fun resumable(status: String): Boolean = status == "paused" || status == "failed" || status == "pending"
    private fun markActivity() { lastTransferActivityAt = System.currentTimeMillis() }
    private fun progressNotify() {
        val now = System.currentTimeMillis()
        if (now - lastProgressNotifyTime >= PROGRESS_THROTTLE_MS) { lastProgressNotifyTime = now; notifySyncListener() }
    }
    private fun notifySyncListener() { syncListener?.onTransferUpdated(getTransferSnapshots()) }

    private fun downloadPartFile(machineId: String, filePath: String, fileSize: Long): File {
        val key = MessageDigest.getInstance("SHA-256").digest("$machineId\u0000$filePath\u0000$fileSize".toByteArray())
            .joinToString("") { "%02x".format(it) }
        return File(transferPartDirectory, "$key.part")
    }

    private fun cleanupOldResumeFiles() {
        val cutoff = System.currentTimeMillis() - RESUME_CLEANUP_AGE_MS
        transferPartDirectory.listFiles()?.filter { it.lastModified() < cutoff }?.forEach { it.delete() }
    }

    private fun uploadTaskId(machineId: String, uri: String, name: String, size: Long, targetDir: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest("$machineId\u0000$uri\u0000$name\u0000$size\u0000$targetDir".toByteArray())
        return "upload-" + digest.take(12).joinToString("") { "%02x".format(it) }
    }

    private fun resolveUniqueFileName(fileName: String, dir: File): String {
        val dot = fileName.lastIndexOf('.')
        val base = if (dot > 0) fileName.substring(0, dot) else fileName
        val extension = if (dot > 0) fileName.substring(dot) else ""
        var candidate = fileName
        var suffix = 1
        while (File(dir, candidate).exists()) candidate = "$base (${suffix++})$extension"
        return candidate
    }

    private fun publishPartToMediaStore(session: DownloadSession, part: File) {
        val directory = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), SUB_DIR).apply { mkdirs() }
        val name = resolveUniqueFileName(session.fileName, directory)
        val relativePath = "${Environment.DIRECTORY_DOWNLOADS}/$SUB_DIR"
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, name)
            put(MediaStore.Downloads.RELATIVE_PATH, relativePath)
            getMimeType(name)?.let { put(MediaStore.Downloads.MIME_TYPE, it) }
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val uri = context.contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw IllegalStateException("无法创建下载文件")
        context.contentResolver.openOutputStream(uri)?.use { output -> FileInputStream(part).use { it.copyTo(output) } }
            ?: throw IllegalStateException("无法写入下载文件")
        context.contentResolver.update(uri, ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }, null, null)
        session.mediaStoreUri = uri
        session.savedPath = "$relativePath/$name"
        part.delete()
    }

    private fun publishPartToLegacyDownloads(session: DownloadSession, part: File) {
        val directory = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), SUB_DIR).apply { mkdirs() }
        val target = File(directory, resolveUniqueFileName(session.fileName, directory))
        FileInputStream(part).use { input -> FileOutputStream(target).use { input.copyTo(it) } }
        session.savedPath = target.absolutePath
        part.delete()
    }

    private fun getMimeType(name: String): String? = when (name.substringAfterLast('.', "").lowercase()) {
        "txt", "log" -> "text/plain"; "json" -> "application/json"; "pdf" -> "application/pdf"
        "png" -> "image/png"; "jpg", "jpeg" -> "image/jpeg"; "zip" -> "application/zip"; else -> null
    }
}
