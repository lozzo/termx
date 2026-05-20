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
import com.termx.app.transport.WebRTCTransport
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ConcurrentLinkedQueue

/**
 * FileTransferManager — Native 层文件传输管理（下载 + 上传）
 *
 * 直接在 Native 层操作 WebRTC DataChannel 和文件 I/O，
 * 不经过 JS/Bridge 层，APP 进入后台时仍可继续传输。
 */
class FileTransferManager(context: Context) {

    companion object {
        private const val TAG = "TermxFileTransfer"
        private const val FRAME_DATA: Byte = 0x01
        private const val FRAME_COMPLETE: Byte = 0x02
        private const val FRAME_ERROR: Byte = 0xFF.toByte()
        private const val SUB_DIR = "termx"
        private const val PROGRESS_THROTTLE_MS = 200L
        private const val BACKPRESSURE_HIGH = 1024 * 1024L
        private const val BACKPRESSURE_LOW = 256 * 1024L
        private const val RESUME_CLEANUP_AGE_MS = 7L * 24 * 60 * 60 * 1000
        private const val DEFAULT_DOWNLOAD_CONCURRENCY_PER_MACHINE = 3
    }

    fun interface SyncListener {
        fun onTransferUpdated(allSnapshots: JSONObject)
    }

    private class DownloadSession {
        var transferId = ""
        var fileName = ""
        var filePath = ""
        var totalSize = 0L
        var receivedSize = 0L
        var resumeOffset = 0L
        var status = "transferring"
        var error: String? = null
        var startedAt = 0L
        var storeKey = ""
        var stream: OutputStream? = null
        var mediaStoreUri: Uri? = null
        var file: File? = null
        var partFile: File? = null
        var savedPath: String? = null
        var lastPersistAt = 0L
        @Volatile var pausedByUser = false
    }

    private class UploadSession {
        var transferId = ""
        var contentUri = ""
        var fileName = ""
        var fileSize = 0L
        var sentSize = 0L
        var resumeId = ""
        var chunkSize = 64 * 1024
        var targetDir = ""
        var status = "transferring"
        var error: String? = null
        var startedAt = 0L
        var storeKey = ""
        var lastPersistAt = 0L
        @Volatile var cancelled = false
        @Volatile var pausedByUser = false
        @Volatile var sendingPaused = false
        val sendResumeSignal = Object()
    }

    private val downloadSessions = ConcurrentHashMap<String, DownloadSession>()
    private val uploadSessions = ConcurrentHashMap<String, UploadSession>()
    private val downloadQueues = ConcurrentHashMap<String, ConcurrentLinkedQueue<String>>()
    private val taskStore = TransferTaskStore(context.applicationContext)
    private val context: Context = context.applicationContext
    private val ioThread = HandlerThread("TermxFileTransfer-IO").apply { start() }
    private val ioHandler = Handler(ioThread.looper)
    var syncListener: SyncListener? = null

    private var lastProgressNotifyTime = 0L
    private var progressNotifyPending = false
    @Volatile private var lastTransferActivityAt = 0L

    var transportRef: WebRTCTransport? = null

    init {
        restorePersistedTransfers()
        cleanupOldResumeFiles()
    }

    // ========== 下载控制 ==========

    fun startDownload(
        transport: WebRTCTransport,
        transferId: String,
        fileName: String,
        fileSize: Long,
        filePath: String,
        offset: Long = 0L,
        storeKey: String = "",
    ) {
        Log.i(TAG, "startDownload: $transferId file=$fileName size=$fileSize offset=$offset")
        transportRef = transport

        if (downloadSessions.containsKey(transferId)) {
            val existing = downloadSessions[transferId]
            if (existing?.status == "paused" || existing?.status == "missing" || existing?.status == "failed") {
                resumeDownload(transport, transferId)
            } else {
                Log.w(TAG, "startDownload: session already exists for $transferId")
            }
            return
        }

        val session = DownloadSession().apply {
            this.transferId = transferId
            this.fileName = fileName
            this.filePath = filePath
            this.totalSize = fileSize
            this.resumeOffset = offset.coerceIn(0L, fileSize)
            this.receivedSize = this.resumeOffset
            this.startedAt = System.currentTimeMillis()
            this.storeKey = storeKey
            this.status = "transferring"
            this.pausedByUser = false
        }
        downloadSessions[transferId] = session
        if (!canStartDownloadForMachine(session.storeKey, session.transferId)) {
            session.status = "pending"
            session.pausedByUser = false
            enqueueDownload(session)
            persistDownload(session)
            notifySyncListener()
            return
        }
        markTransferActivity()

        ioHandler.post {
            startDownloadSession(transport, session)
        }
    }

    fun onFileData(transferId: String, rawFrame: ByteArray) {
        val session = downloadSessions[transferId] ?: return
        if (session.status != "transferring") return
        if (rawFrame.isEmpty()) return
        markTransferActivity()

        when (rawFrame[0]) {
            FRAME_DATA -> {
                if (rawFrame.size < 5) return
                val chunkData = rawFrame.copyOfRange(5, rawFrame.size)
                ioHandler.post { writeChunk(session, chunkData) }
            }
            FRAME_COMPLETE -> ioHandler.post { finalizeDownload(session) }
            FRAME_ERROR -> {
                val errMsg = if (rawFrame.size > 1)
                    String(rawFrame, 1, rawFrame.size - 1, Charsets.UTF_8)
                else "远程错误"
                ioHandler.post { failDownload(session, errMsg) }
            }
        }
    }

    fun onChannelOpen(transferId: String) {
        val uploadSession = uploadSessions[transferId]
        if (uploadSession != null && uploadSession.status == "transferring") {
            onUploadChannelOpen(uploadSession)
        }
    }

    fun cancelDownload(transferId: String) {
        val session = downloadSessions[transferId] ?: return
        ioHandler.post { cancelDownloadSession(session) }
    }

    fun clearTransfer(transferId: String) {
        val dl = downloadSessions.remove(transferId)
        if (dl != null) {
            dl.stream?.let { try { it.close() } catch (_: Exception) {} }
            dl.stream = null
            closeFileChannel(dl.transferId)
            removeQueuedDownload(dl)
            taskStore.delete(transferId)
            notifySyncListener()
            startQueuedDownloads(dl.storeKey, transportRef)
            return
        }
        val ul = uploadSessions.remove(transferId)
        if (ul != null) {
            ul.cancelled = true
            synchronized(ul.sendResumeSignal) { ul.sendResumeSignal.notifyAll() }
            closeFileChannel(ul.transferId)
            taskStore.delete(transferId)
            notifySyncListener()
        }
    }

    fun resumeAllForMachine(machineId: String, transport: WebRTCTransport?) {
        if (transport == null) {
            for (session in downloadSessions.values) {
                if (session.storeKey == machineId && canUserResume(session.status)) {
                    session.status = "paused"
                    session.error = "等待连接"
                    session.pausedByUser = true
                    persistDownload(session)
                }
            }
            for (session in uploadSessions.values) {
                if (session.storeKey == machineId && canUserResume(session.status)) {
                    session.status = "paused"
                    session.error = "等待连接"
                    session.pausedByUser = true
                    persistUpload(session)
                }
            }
            notifySyncListener()
            return
        }
        for (session in downloadSessions.values) {
            if (session.storeKey == machineId && canUserResume(session.status)) {
                resumeDownload(transport, session.transferId)
            }
        }
        for (session in uploadSessions.values) {
            if (session.storeKey == machineId && canUserResume(session.status)) {
                resumeUpload(transport, session.transferId)
            }
        }
    }

    fun transferMachineIds(): Set<String> =
        (downloadSessions.values.map { it.storeKey } + uploadSessions.values.map { it.storeKey })
            .filter { it.isNotEmpty() }
            .toSet()

    fun pauseDownload(transferId: String) {
        val session = downloadSessions[transferId] ?: return
        if (session.status == "pending") {
            session.status = "paused"
            session.error = null
            session.pausedByUser = true
            removeQueuedDownload(session)
            persistDownload(session)
            notifySyncListener()
            return
        }
        ioHandler.post { pauseDownloadSession(session, byUser = true, reason = null) }
    }

    fun resumeDownload(transport: WebRTCTransport, transferId: String) {
        val session = downloadSessions[transferId] ?: return
        if (session.status == "completed") return
        if (session.status == "transferring") return
        if (!canStartDownloadForMachine(session.storeKey, session.transferId)) {
            session.status = "pending"
            session.error = null
            session.pausedByUser = false
            enqueueDownload(session)
            persistDownload(session)
            notifySyncListener()
            return
        }
        Thread({ resumeDownloadSession(transport, session) }, "TermxDownload-Resume-$transferId").start()
    }

    fun onTransportLost() {
        for (session in downloadSessions.values) {
            if (session.status == "transferring") {
                ioHandler.post { pauseDownloadSession(session, byUser = false, reason = "连接中断，等待重连") }
            } else if (session.status == "pending") {
                session.error = "连接中断，等待重连"
                persistDownload(session)
            }
        }
        for (session in uploadSessions.values) {
            if (session.status == "transferring") {
                session.cancelled = true
                synchronized(session.sendResumeSignal) { session.sendResumeSignal.notifyAll() }
                ioHandler.post { pauseUploadSession(session, byUser = false, reason = "连接中断，等待重连") }
            }
        }
    }

    fun resumeInterruptedTransfers(transport: WebRTCTransport) {
        transportRef = transport
        for (session in downloadSessions.values) {
            if (session.storeKey == transport.machineId && session.status == "pending" && !session.pausedByUser) {
                enqueueDownload(session)
            } else if (session.storeKey == transport.machineId && session.status == "paused" && !session.pausedByUser) {
                resumeDownload(transport, session.transferId)
            }
        }
        startQueuedDownloads(transport.machineId, transport)
        for (session in uploadSessions.values) {
            if (session.storeKey == transport.machineId && session.status == "paused" && !session.pausedByUser) {
                resumeUpload(transport, session.transferId)
            }
        }
    }

    fun isHandling(transferId: String): Boolean {
        val dl = downloadSessions[transferId]
        if (dl != null && dl.status == "transferring") return true
        val ul = uploadSessions[transferId]
        if (ul != null && ul.status == "transferring") return true
        return false
    }

    fun hasActiveTransfers(): Boolean =
        downloadSessions.values.any { it.status == "transferring" } ||
            uploadSessions.values.any { it.status == "transferring" }

    fun hasRecentTransferActivity(windowMs: Long): Boolean {
        val last = lastTransferActivityAt
        return hasActiveTransfers() && last > 0 && System.currentTimeMillis() - last <= windowMs
    }

    // ========== 上传控制 ==========

    fun startUpload(
        transport: WebRTCTransport,
        contentUri: String,
        fileName: String,
        fileSize: Long,
        targetDir: String,
        storeKey: String = "",
    ) {
        Log.i(TAG, "startUpload: file=$fileName size=$fileSize uri=$contentUri")

        Thread({
            try {
                val targetPath = if (targetDir.endsWith("/")) "$targetDir$fileName" else "$targetDir/$fileName"
                val resumeId = uploadResumeId(transport.machineId, contentUri, fileName, fileSize, targetDir)
                val existing = uploadSessions[resumeId]
                if (existing != null) {
                    if (existing.status == "paused") {
                        resumeUpload(transport, resumeId)
                    } else {
                        Log.w(TAG, "startUpload: session already exists for $resumeId")
                    }
                    return@Thread
                }
                val initBody = JSONObject().apply {
                    put("path", targetPath)
                    put("size", fileSize)
                    put("resume_id", resumeId)
                }
                val initResp = transport.sendApiRequest("POST", "/files/upload/init", initBody.toString(), 60_000)
                val respJson = JSONObject(initResp)
                val status = respJson.optInt("status", 0)
                if (status >= 400) {
                    val errorBody = respJson.optJSONObject("body")
                    val errorMsg = errorBody?.optString("error") ?: "上传初始化失败 (status=$status)"
                    throw Exception(errorMsg)
                }
                val body = respJson.optJSONObject("body") ?: throw Exception("上传初始化响应格式错误")

                val transferId = body.getString("transfer_id")
                val chunkSize = body.optInt("chunk_size", 64 * 1024)
                val uploadedOffset = body.optLong("uploaded_offset", 0L).coerceIn(0L, fileSize)

                if (uploadSessions.containsKey(transferId)) {
                    Log.w(TAG, "startUpload: session already exists for $transferId")
                    return@Thread
                }

                val session = UploadSession().apply {
                    this.transferId = transferId
                    this.contentUri = contentUri
                    this.fileName = fileName
                    this.fileSize = fileSize
                    this.sentSize = uploadedOffset
                    this.resumeId = resumeId
                    this.chunkSize = chunkSize
                    this.targetDir = targetDir
                    this.startedAt = System.currentTimeMillis()
                    this.storeKey = storeKey
                    this.status = "transferring"
                    this.cancelled = false
                    this.pausedByUser = false
                }
                uploadSessions[transferId] = session
                transportRef = transport
                markTransferActivity()
                persistUpload(session)
                notifySyncListener()

                val dc = transport.openFileChannel(transferId)
                if (dc != null && dc.state() == org.webrtc.DataChannel.State.OPEN) {
                    onChannelOpen(transferId)
                }
            } catch (e: Exception) {
                Log.e(TAG, "startUpload init failed", e)
                notifySyncListener()
            }
        }, "TermxUpload-Init-$fileName").start()
    }

    fun cancelUpload(transferId: String) {
        val session = uploadSessions[transferId] ?: return
        session.cancelled = true
        synchronized(session.sendResumeSignal) { session.sendResumeSignal.notifyAll() }
        ioHandler.post { cancelUploadSession(session) }
    }

    fun pauseUpload(transferId: String) {
        val session = uploadSessions[transferId] ?: return
        session.cancelled = true
        synchronized(session.sendResumeSignal) { session.sendResumeSignal.notifyAll() }
        ioHandler.post { pauseUploadSession(session, byUser = true, reason = null) }
    }

    fun resumeUpload(transport: WebRTCTransport, transferId: String) {
        val session = uploadSessions[transferId] ?: return
        if (session.status == "completed") return
        if (session.status == "transferring") return
        session.cancelled = false
        Thread({ resumeUploadSession(transport, session) }, "TermxUpload-Resume-$transferId").start()
    }

    fun onBufferedAmountChange(transferId: String, bufferedAmount: Long) {
        if (bufferedAmount < BACKPRESSURE_LOW) {
            val session = uploadSessions[transferId]
            if (session != null && session.sendingPaused) {
                synchronized(session.sendResumeSignal) { session.sendResumeSignal.notifyAll() }
            }
        }
    }

    private fun onUploadChannelOpen(session: UploadSession) {
        Thread({ doUploadSend(session) }, "TermxUpload-${session.transferId}").start()
    }

    private fun doUploadSend(session: UploadSession) {
        val transferId = session.transferId
        var inputStream: InputStream? = null
        try {
            val uri = Uri.parse(session.contentUri)
            inputStream = context.contentResolver.openInputStream(uri)
                ?: throw Exception("无法打开文件: ${session.contentUri}")

            val chunkSize = session.chunkSize
            val buffer = ByteArray(chunkSize)
            var offset = session.sentSize.coerceIn(0L, session.fileSize)
            if (offset > 0) {
                skipFully(inputStream, offset)
            }
            var chunkNum = (offset / chunkSize).toInt()

            while (offset < session.fileSize && !session.cancelled) {
                val bytesToRead = minOf(chunkSize.toLong(), session.fileSize - offset).toInt()
                val bytesRead = readFully(inputStream, buffer, bytesToRead)
                if (bytesRead <= 0) break

                val frame = ByteArray(5 + bytesRead)
                frame[0] = FRAME_DATA
                ByteBuffer.wrap(frame, 1, 4).putInt(chunkNum)
                System.arraycopy(buffer, 0, frame, 5, bytesRead)

                waitForBackpressure(session, transferId)
                if (session.cancelled) break

                val cm = transportRef?.channelManager ?: run { failUpload(session, "传输通道丢失"); return }
                cm.sendFileData(transferId, frame)
                markTransferActivity()

                offset += bytesRead
                session.sentSize = offset
                chunkNum++
                persistUploadThrottled(session)
                throttledProgressNotify()
            }

            if (session.cancelled) return

            val completeFrame = ByteArray(5)
            completeFrame[0] = FRAME_COMPLETE
            ByteBuffer.wrap(completeFrame, 1, 4).putInt(chunkNum)
            transportRef?.channelManager?.sendFileData(transferId, completeFrame)

            val transport = transportRef ?: run { failUpload(session, "transport 丢失"); return }
            val completeBody = JSONObject().apply { put("transfer_id", transferId) }
            val completeResp = transport.sendApiRequest("POST", "/files/upload/complete", completeBody.toString(), 60_000)
            val completeJson = JSONObject(completeResp)
            if (completeJson.optInt("status", 0) >= 400) {
                val errorBody = completeJson.optJSONObject("body")
                throw Exception(errorBody?.optString("error") ?: "上传完成确认失败")
            }

            session.status = "completed"
            session.sentSize = session.fileSize
            persistUpload(session)
            Log.i(TAG, "upload completed: $transferId")
            notifySyncListener()

        } catch (e: Exception) {
            if (!session.cancelled) {
                Log.e(TAG, "doUploadSend error", e)
                failUpload(session, e.message ?: "上传失败")
            }
        } finally {
            try { inputStream?.close() } catch (_: Exception) {}
        }
    }

    private fun readFully(input: InputStream, buffer: ByteArray, len: Int): Int {
        var totalRead = 0
        while (totalRead < len) {
            val n = input.read(buffer, totalRead, len - totalRead)
            if (n < 0) break
            totalRead += n
        }
        return totalRead
    }

    private fun waitForBackpressure(session: UploadSession, transferId: String) {
        val cm = transportRef?.channelManager ?: return
        val dc = cm.getFileChannel(transferId) ?: return
        while (dc.bufferedAmount() > BACKPRESSURE_HIGH && !session.cancelled) {
            session.sendingPaused = true
            synchronized(session.sendResumeSignal) {
                if (dc.bufferedAmount() > BACKPRESSURE_LOW && !session.cancelled) {
                    try { session.sendResumeSignal.wait(500) } catch (_: InterruptedException) {}
                }
            }
        }
        session.sendingPaused = false
    }

    private fun closeFileChannel(transferId: String) {
        try {
            transportRef?.channelManager?.closeFile(transferId)
        } catch (_: Exception) {}
    }

    private fun failUpload(session: UploadSession, error: String) {
        if (session.status == "completed" || session.status == "failed" || session.status == "cancelled") return
        session.status = "failed"
        session.error = error
        Log.w(TAG, "upload failed: ${session.transferId} error=$error")
        persistUpload(session)
        notifySyncListener()
    }

    private fun pauseUploadSession(session: UploadSession, byUser: Boolean, reason: String?) {
        if (session.status == "completed" || session.status == "failed" || session.status == "cancelled" || session.status == "missing") return
        session.status = "paused"
        session.error = reason
        session.pausedByUser = byUser
        session.sendingPaused = false
        closeFileChannel(session.transferId)
        Log.i(TAG, "upload paused: ${session.transferId} user=$byUser")
        persistUpload(session)
        notifySyncListener()
    }

    private fun cancelUploadSession(session: UploadSession) {
        if (session.status == "completed" || session.status == "cancelled") return
        session.status = "cancelled"
        session.error = "用户取消"
        session.pausedByUser = true
        session.sendingPaused = false
        closeFileChannel(session.transferId)
        persistUpload(session)
        notifySyncListener()
    }

    private fun resumeUploadSession(transport: WebRTCTransport, session: UploadSession) {
        try {
            transportRef = transport
            val targetPath = if (session.targetDir.endsWith("/")) "${session.targetDir}${session.fileName}" else "${session.targetDir}/${session.fileName}"
            val initBody = JSONObject().apply {
                put("path", targetPath)
                put("size", session.fileSize)
                put("resume_id", session.resumeId)
            }
            val initResp = transport.sendApiRequest("POST", "/files/upload/init", initBody.toString(), 60_000)
            val respJson = JSONObject(initResp)
            val status = respJson.optInt("status", 0)
            if (status >= 400) {
                val errorBody = respJson.optJSONObject("body")
                throw Exception(errorBody?.optString("error") ?: "上传续传初始化失败 (status=$status)")
            }
            val body = respJson.optJSONObject("body") ?: throw Exception("上传续传响应格式错误")
            val resumedTransferId = body.getString("transfer_id")
            if (resumedTransferId != session.transferId) {
                taskStore.delete(session.transferId)
                uploadSessions.remove(session.transferId)
                session.transferId = resumedTransferId
                uploadSessions[resumedTransferId] = session
            }
            session.chunkSize = body.optInt("chunk_size", session.chunkSize)
            session.sentSize = body.optLong("uploaded_offset", session.sentSize).coerceIn(0L, session.fileSize)
            session.status = "transferring"
            session.error = null
            session.cancelled = false
            session.pausedByUser = false
            markTransferActivity()
            persistUpload(session)
            notifySyncListener()
            val dc = transport.openFileChannel(session.transferId)
            if (dc != null && dc.state() == org.webrtc.DataChannel.State.OPEN) {
                onChannelOpen(session.transferId)
            }
        } catch (e: Exception) {
            Log.e(TAG, "resumeUpload failed", e)
            ioHandler.post { pauseUploadSession(session, byUser = false, reason = e.message ?: "上传续传失败") }
        }
    }

    fun getTransferSnapshots(): JSONObject {
        val result = JSONObject()
        try {
            val transfers = JSONArray()
            for (session in downloadSessions.values) {
                transfers.put(JSONObject().apply {
                    put("id", session.transferId)
                    put("name", session.fileName)
                    put("direction", "download")
                    put("totalSize", session.totalSize)
                    put("transferredSize", session.receivedSize)
                    put("status", session.status)
                    put("startedAt", session.startedAt)
                    put("updatedAt", System.currentTimeMillis())
                    if (session.storeKey.isNotEmpty()) put("storeKey", session.storeKey)
                    if (session.filePath.isNotEmpty()) put("filePath", session.filePath)
                    session.savedPath?.let { put("savedPath", it) }
                    session.mediaStoreUri?.let { put("savedUri", it.toString()) }
                    session.error?.let { put("error", it) }
                })
            }
            for (session in uploadSessions.values) {
                transfers.put(JSONObject().apply {
                    put("id", session.transferId)
                    put("name", session.fileName)
                    put("direction", "upload")
                    put("totalSize", session.fileSize)
                    put("transferredSize", session.sentSize)
                    put("status", session.status)
                    put("startedAt", session.startedAt)
                    put("updatedAt", System.currentTimeMillis())
                    if (session.storeKey.isNotEmpty()) put("storeKey", session.storeKey)
                    if (session.contentUri.isNotEmpty()) put("localUri", session.contentUri)
                    if (session.targetDir.isNotEmpty()) put("targetDir", session.targetDir)
                    session.error?.let { put("error", it) }
                })
            }
            result.put("transfers", transfers as Any)
        } catch (e: Exception) {
            Log.e(TAG, "getTransferSnapshots error", e)
        }
        return result
    }

    // ========== 内部文件操作（下载） ==========

    fun getDownloadResumeOffset(machineId: String, filePath: String, fileSize: Long): Long {
        cleanupOldResumeFiles()
        if (fileSize <= 0) return 0L
        val session = downloadSessions.values.firstOrNull {
            it.storeKey == machineId && it.filePath == filePath && it.totalSize == fileSize
        }
        val file = downloadPartFile(machineId, filePath, fileSize)
        val size = if (file.exists()) file.length() else session?.receivedSize ?: 0L
        return if (size in 1 until fileSize) size else 0L
    }

    private fun startDownloadSession(transport: WebRTCTransport, session: DownloadSession) {
        try {
            session.status = "transferring"
            session.error = null
            session.pausedByUser = false
            openOutputStream(session)
            markTransferActivity()
            persistDownload(session)
            transport.openFileChannel(session.transferId)
            notifySyncListener()
        } catch (e: Exception) {
            Log.e(TAG, "startDownload openStream failed", e)
            session.status = "failed"
            session.error = "打开文件失败: ${e.message}"
            persistDownload(session)
            notifySyncListener()
            startQueuedDownloads(session.storeKey, transport)
        }
    }

    private fun enqueueDownload(session: DownloadSession) {
        val queue = downloadQueues.getOrPut(session.storeKey) { ConcurrentLinkedQueue() }
        if (!queue.contains(session.transferId)) queue.add(session.transferId)
    }

    private fun removeQueuedDownload(session: DownloadSession) {
        downloadQueues[session.storeKey]?.remove(session.transferId)
    }

    private fun canStartDownloadForMachine(machineId: String, transferId: String): Boolean {
        val running = downloadSessions.values.count {
            it.storeKey == machineId && it.transferId != transferId && it.status == "transferring"
        }
        return running < DEFAULT_DOWNLOAD_CONCURRENCY_PER_MACHINE
    }

    private fun startQueuedDownloads(machineId: String, transport: WebRTCTransport?) {
        if (transport == null || transport.machineId != machineId) return
        val queue = downloadQueues[machineId] ?: return
        while (canStartDownloadForMachine(machineId, "")) {
            val transferId = queue.poll() ?: return
            val session = downloadSessions[transferId] ?: continue
            if (session.status != "pending") continue
            ioHandler.post { resumeDownloadSession(transport, session) }
        }
    }

    private fun openOutputStream(session: DownloadSession) {
        val part = downloadPartFile(session.storeKey, session.filePath, session.totalSize)
        part.parentFile?.mkdirs()
        if (session.resumeOffset > 0 && part.length() != session.resumeOffset) {
            session.resumeOffset = 0L
            session.receivedSize = 0L
        }
        session.partFile = part
        session.stream = FileOutputStream(part, session.resumeOffset > 0)
        session.savedPath = part.absolutePath
        persistDownload(session)
    }

    private fun writeChunk(session: DownloadSession, data: ByteArray) {
        if (session.status != "transferring" || session.stream == null) return
        try {
            session.stream!!.write(data)
            session.receivedSize += data.size
            markTransferActivity()
            persistDownloadThrottled(session)
            throttledProgressNotify()
        } catch (e: Exception) {
            Log.e(TAG, "writeChunk failed", e)
            failDownload(session, "写入失败: ${e.message}")
        }
    }

    private fun throttledProgressNotify() {
        val now = System.currentTimeMillis()
        if (now - lastProgressNotifyTime >= PROGRESS_THROTTLE_MS) {
            lastProgressNotifyTime = now
            progressNotifyPending = false
            notifySyncListener()
        } else if (!progressNotifyPending) {
            progressNotifyPending = true
            val delay = PROGRESS_THROTTLE_MS - (now - lastProgressNotifyTime)
            ioHandler.postDelayed({
                progressNotifyPending = false
                lastProgressNotifyTime = System.currentTimeMillis()
                notifySyncListener()
            }, delay)
        }
    }

    private fun finalizeDownload(session: DownloadSession) {
        if (session.status != "transferring") return
        try {
            session.stream?.flush()
            session.stream?.close()
            session.stream = null
            val part = session.partFile ?: throw Exception("missing download part file")
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                publishPartToMediaStore(session, part)
            } else {
                publishPartToLegacyDownloads(session, part)
            }
            session.status = "completed"
            session.receivedSize = session.totalSize
            session.error = null
            persistDownload(session)
            Log.i(TAG, "download completed: ${session.transferId} received=${session.receivedSize}")
            notifySyncListener()
            startQueuedDownloads(session.storeKey, transportRef)
        } catch (e: Exception) {
            Log.e(TAG, "finalizeDownload failed", e)
            failDownload(session, "保存文件失败: ${e.message}")
        }
    }

    private fun failDownload(session: DownloadSession, error: String) {
        if (session.status == "completed" || session.status == "failed") return
        session.status = "failed"
        session.error = error
        Log.w(TAG, "download failed: ${session.transferId} error=$error")
        session.stream?.let { try { it.close() } catch (_: Exception) {} }
        session.stream = null
        closeFileChannel(session.transferId)
        persistDownload(session)
        notifySyncListener()
        startQueuedDownloads(session.storeKey, transportRef)
    }

    private fun pauseDownloadSession(session: DownloadSession, byUser: Boolean, reason: String?) {
        if (session.status == "completed" || session.status == "failed" || session.status == "missing") return
        session.status = "paused"
        session.error = reason
        session.pausedByUser = byUser
        removeQueuedDownload(session)
        session.stream?.let {
            try { it.flush() } catch (_: Exception) {}
            try { it.close() } catch (_: Exception) {}
        }
        session.stream = null
        closeFileChannel(session.transferId)
        Log.i(TAG, "download paused: ${session.transferId} user=$byUser received=${session.receivedSize}")
        persistDownload(session)
        notifySyncListener()
        startQueuedDownloads(session.storeKey, transportRef)
    }

    private fun cancelDownloadSession(session: DownloadSession) {
        if (session.status == "completed" || session.status == "cancelled") return
        session.status = "cancelled"
        session.error = "用户取消"
        removeQueuedDownload(session)
        session.stream?.let {
            try { it.flush() } catch (_: Exception) {}
            try { it.close() } catch (_: Exception) {}
        }
        session.stream = null
        closeFileChannel(session.transferId)
        persistDownload(session)
        notifySyncListener()
        startQueuedDownloads(session.storeKey, transportRef)
    }

    private fun resumeDownloadSession(transport: WebRTCTransport, session: DownloadSession) {
        if (!canStartDownloadForMachine(session.storeKey, session.transferId)) {
            session.status = "pending"
            session.error = null
            session.pausedByUser = false
            enqueueDownload(session)
            persistDownload(session)
            notifySyncListener()
            return
        }
        try {
            transportRef = transport
            val part = downloadPartFile(session.storeKey, session.filePath, session.totalSize)
            val offset = if (part.exists()) part.length().coerceIn(0L, session.totalSize) else session.receivedSize.coerceIn(0L, session.totalSize)
            val initBody = JSONObject().apply {
                put("path", session.filePath)
                put("offset", offset)
                put("transfer_id", session.transferId)
            }
            val initResp = transport.sendApiRequest("POST", "/files/download/init", initBody.toString(), 60_000)
            val respJson = JSONObject(initResp)
            val status = respJson.optInt("status", 0)
            if (status >= 400) {
                val errorBody = respJson.optJSONObject("body")
                throw Exception(errorBody?.optString("error") ?: "下载续传初始化失败 (status=$status)")
            }
            val body = respJson.optJSONObject("body") ?: throw Exception("下载续传响应格式错误")
            val resumedTransferId = body.getString("transfer_id")
            if (resumedTransferId != session.transferId) {
                taskStore.delete(session.transferId)
                downloadSessions.remove(session.transferId)
                session.transferId = resumedTransferId
                downloadSessions[resumedTransferId] = session
            }
            session.totalSize = body.optLong("size", session.totalSize)
            session.fileName = body.optString("name", session.fileName)
            session.resumeOffset = offset
            session.receivedSize = offset
            session.status = "transferring"
            session.error = null
            session.pausedByUser = false
            removeQueuedDownload(session)
            persistDownload(session)
            ioHandler.post {
                try {
                    openOutputStream(session)
                    markTransferActivity()
                    notifySyncListener()
                    transport.openFileChannel(session.transferId)
                } catch (e: Exception) {
                    Log.e(TAG, "resumeDownload openStream failed", e)
                    pauseDownloadSession(session, byUser = false, reason = "打开续传文件失败: ${e.message}")
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "resumeDownload failed", e)
            ioHandler.post { pauseDownloadSession(session, byUser = false, reason = e.message ?: "下载续传失败") }
        }
    }

    private fun notifySyncListener() {
        syncListener?.onTransferUpdated(getTransferSnapshots())
    }

    private fun markTransferActivity() {
        lastTransferActivityAt = System.currentTimeMillis()
    }

    private fun canUserResume(status: String): Boolean =
        status == "paused" || status == "failed" || status == "missing" || status == "pending"

    private fun persistDownload(session: DownloadSession) {
        session.lastPersistAt = System.currentTimeMillis()
        taskStore.upsert(
            PersistedTransfer(
                id = session.transferId,
                direction = "download",
                machineId = session.storeKey,
                name = session.fileName,
                totalSize = session.totalSize,
                transferredSize = session.receivedSize,
                status = session.status,
                startedAt = session.startedAt,
                updatedAt = System.currentTimeMillis(),
                remotePath = session.filePath,
                localUri = "",
                targetDir = "",
                resumeId = "",
                chunkSize = 64 * 1024,
                savedPath = session.savedPath,
                savedUri = session.mediaStoreUri?.toString(),
                error = session.error,
                pausedByUser = session.pausedByUser,
            ),
        )
    }

    private fun persistDownloadThrottled(session: DownloadSession) {
        if (System.currentTimeMillis() - session.lastPersistAt >= PROGRESS_THROTTLE_MS) {
            persistDownload(session)
        }
    }

    private fun persistUpload(session: UploadSession) {
        session.lastPersistAt = System.currentTimeMillis()
        taskStore.upsert(
            PersistedTransfer(
                id = session.transferId,
                direction = "upload",
                machineId = session.storeKey,
                name = session.fileName,
                totalSize = session.fileSize,
                transferredSize = session.sentSize,
                status = session.status,
                startedAt = session.startedAt,
                updatedAt = System.currentTimeMillis(),
                remotePath = "",
                localUri = session.contentUri,
                targetDir = session.targetDir,
                resumeId = session.resumeId,
                chunkSize = session.chunkSize,
                savedPath = null,
                savedUri = null,
                error = session.error,
                pausedByUser = session.pausedByUser,
            ),
        )
    }

    private fun persistUploadThrottled(session: UploadSession) {
        if (System.currentTimeMillis() - session.lastPersistAt >= PROGRESS_THROTTLE_MS) {
            persistUpload(session)
        }
    }

    private fun restorePersistedTransfers() {
        for (record in taskStore.loadAll()) {
            when (record.direction) {
                "download" -> restoreDownload(record)
                "upload" -> restoreUpload(record)
            }
        }
    }

    private fun restoreDownload(record: PersistedTransfer) {
        val status = startupStatus(record)
        val session = DownloadSession().apply {
            transferId = record.id
            fileName = record.name
            filePath = record.remotePath
            totalSize = record.totalSize
            receivedSize = restoredDownloadBytes(record)
            resumeOffset = receivedSize
            startedAt = record.startedAt
            storeKey = record.machineId
            this.status = status.first
            error = status.second
            pausedByUser = true
            savedPath = record.savedPath
            mediaStoreUri = record.savedUri?.let { runCatching { Uri.parse(it) }.getOrNull() }
            partFile = downloadPartFile(record.machineId, record.remotePath, record.totalSize)
        }
        downloadSessions[session.transferId] = session
        persistDownload(session)
    }

    private fun restoreUpload(record: PersistedTransfer) {
        val sourceExists = record.status == "completed" || canOpenUri(record.localUri)
        val session = UploadSession().apply {
            transferId = record.id
            contentUri = record.localUri
            fileName = record.name
            fileSize = record.totalSize
            sentSize = record.transferredSize.coerceIn(0L, record.totalSize)
            resumeId = record.resumeId.ifEmpty {
                uploadResumeId(record.machineId, record.localUri, record.name, record.totalSize, record.targetDir)
            }
            chunkSize = if (record.chunkSize > 0) record.chunkSize else 64 * 1024
            targetDir = record.targetDir
            startedAt = record.startedAt
            storeKey = record.machineId
            status = if (sourceExists) startupStatus(record).first else "missing"
            error = if (sourceExists) startupStatus(record).second else "本地文件不存在"
            pausedByUser = true
            cancelled = true
        }
        uploadSessions[session.transferId] = session
        persistUpload(session)
    }

    private fun startupStatus(record: PersistedTransfer): Pair<String, String?> {
        if (record.status == "completed") {
            if (record.direction == "download" && !downloadOutputExists(record)) {
                return "missing" to "本地文件不存在"
            }
            return "completed" to record.error
        }
        if (record.status == "cancelled") return "cancelled" to record.error
        return "paused" to record.error
    }

    private fun restoredDownloadBytes(record: PersistedTransfer): Long {
        if (record.status == "completed") return record.totalSize
        val part = downloadPartFile(record.machineId, record.remotePath, record.totalSize)
        val partSize = if (part.exists()) part.length() else 0L
        return if (partSize in 1 until record.totalSize) partSize else record.transferredSize.coerceIn(0L, record.totalSize)
    }

    private fun downloadOutputExists(record: PersistedTransfer): Boolean {
        val savedUri = record.savedUri
        if (!savedUri.isNullOrBlank()) {
            return canOpenUri(savedUri)
        }
        val savedPath = record.savedPath ?: return true
        if (savedPath.startsWith("${Environment.DIRECTORY_DOWNLOADS}/")) return true
        if (savedPath.startsWith("${Environment.DIRECTORY_DOWNLOADS}/$SUB_DIR/")) return true
        return File(savedPath).exists()
    }

    private fun canOpenUri(uriText: String): Boolean {
        if (uriText.isBlank()) return false
        return try {
            context.contentResolver.openInputStream(Uri.parse(uriText))?.use { true } ?: false
        } catch (_: Exception) {
            false
        }
    }

    private fun resolveUniqueFileName(fileName: String, dir: File): String {
        val dotIdx = fileName.lastIndexOf('.')
        val name = if (dotIdx > 0) fileName.substring(0, dotIdx) else fileName
        val ext = if (dotIdx > 0) fileName.substring(dotIdx) else ""
        var candidate = fileName
        var counter = 1
        while (File(dir, candidate).exists()) {
            candidate = "$name ($counter)$ext"
            counter++
        }
        return candidate
    }

    private fun publishPartToMediaStore(session: DownloadSession, part: File) {
        val downloadsDir = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS)
        val targetDir = File(downloadsDir, SUB_DIR)
        val uniqueName = resolveUniqueFileName(session.fileName, targetDir)
        val relativePath = "${Environment.DIRECTORY_DOWNLOADS}/$SUB_DIR"
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, uniqueName)
            put(MediaStore.Downloads.RELATIVE_PATH, relativePath)
            getMimeType(uniqueName)?.let { put(MediaStore.Downloads.MIME_TYPE, it) }
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val uri = context.contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw Exception("Failed to create MediaStore entry")
        context.contentResolver.openOutputStream(uri)?.use { out ->
            FileInputStream(part).use { input -> input.copyTo(out) }
        } ?: run {
            context.contentResolver.delete(uri, null, null)
            throw Exception("Failed to open output stream")
        }
        val doneValues = ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }
        context.contentResolver.update(uri, doneValues, null, null)
        session.mediaStoreUri = uri
        session.savedPath = "$relativePath/$uniqueName"
        part.delete()
    }

    private fun publishPartToLegacyDownloads(session: DownloadSession, part: File) {
        val downloadsDir = File(
            Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS),
            SUB_DIR,
        )
        if (!downloadsDir.exists()) downloadsDir.mkdirs()
        val uniqueName = resolveUniqueFileName(session.fileName, downloadsDir)
        val outFile = File(downloadsDir, uniqueName)
        part.copyTo(outFile, overwrite = true)
        part.delete()
        session.file = outFile
        session.savedPath = outFile.absolutePath
    }

    private fun downloadPartFile(machineId: String, filePath: String, fileSize: Long): File =
        File(downloadResumeDir(), "${sha256("$machineId|$filePath|$fileSize")}.part")

    private fun downloadResumeDir(): File = File(context.cacheDir, "termx-transfers/downloads").apply { mkdirs() }

    private fun uploadResumeId(machineId: String, contentUri: String, fileName: String, fileSize: Long, targetDir: String): String =
        "up-${sha256("$machineId|$contentUri|$fileName|$fileSize|$targetDir")}"

    private fun skipFully(input: InputStream, bytes: Long) {
        var remaining = bytes
        while (remaining > 0) {
            val skipped = input.skip(remaining)
            if (skipped > 0) {
                remaining -= skipped
                continue
            }
            if (input.read() < 0) break
            remaining--
        }
    }

    private fun cleanupOldResumeFiles() {
        val cutoff = System.currentTimeMillis() - RESUME_CLEANUP_AGE_MS
        for (dir in listOf(downloadResumeDir())) {
            dir.listFiles()?.forEach { file ->
                if (file.isFile && file.lastModified() < cutoff) {
                    try { file.delete() } catch (_: Exception) {}
                }
            }
        }
    }

    private fun sha256(value: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(value.toByteArray(Charsets.UTF_8))
        return digest.joinToString("") { "%02x".format(it) }
    }

    private fun getMimeType(fileName: String): String? {
        val lower = fileName.lowercase()
        return when {
            lower.endsWith(".pdf") -> "application/pdf"
            lower.endsWith(".zip") -> "application/zip"
            lower.endsWith(".tar") -> "application/x-tar"
            lower.endsWith(".gz") -> "application/gzip"
            lower.endsWith(".jpg") || lower.endsWith(".jpeg") -> "image/jpeg"
            lower.endsWith(".png") -> "image/png"
            lower.endsWith(".gif") -> "image/gif"
            lower.endsWith(".mp4") -> "video/mp4"
            lower.endsWith(".mp3") -> "audio/mpeg"
            lower.endsWith(".txt") -> "text/plain"
            lower.endsWith(".doc") -> "application/msword"
            lower.endsWith(".docx") -> "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
            lower.endsWith(".xls") -> "application/vnd.ms-excel"
            lower.endsWith(".xlsx") -> "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            else -> "application/octet-stream"
        }
    }
}
