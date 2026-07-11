package com.termx.app.transport

import android.util.Log
import com.termx.app.network.BridgeServer
import com.termx.app.transfer.FileTransferManager
import org.json.JSONObject
import org.webrtc.DataChannel
import org.webrtc.PeerConnection
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * ChannelManager — api/events/terminal/file DataChannel 管理
 *
 * 通道 label 格式（machineId 作为 storeKey）：
 *   api:{machineId}
 *   events:{machineId}
 *   terminal:{machineId}:{terminalId}
 *   file:{machineId}:{transferId}
 */
class ChannelManager(
    private val bridge: BridgeServer?,
    private val machineId: String,
) {
    companion object {
        private const val TAG = "TermxChannelMgr"

        // termx API 分块协议（与 JS 侧 rtcApiChannel.ts 对齐）
        private const val API_CHUNK_MAGIC: Byte = 0xC0.toByte()
        private const val FLAG_LAST = 0x02
        private const val STATS_INTERVAL_MS = 1000L
        private const val LARGE_PAYLOAD_BYTES = 64 * 1024
    }

    var fileTransferManager: FileTransferManager? = null

    var apiChannel: DataChannel? = null
        private set
    private var eventsChannel: DataChannel? = null

    private val terminalChannels = ConcurrentHashMap<String, DataChannel>()
    private val fileChannels = ConcurrentHashMap<String, DataChannel>()
    private val pendingEventsData = ConcurrentLinkedQueue<ByteArray>()

    // 等待 API 响应
    private val pendingApiRequests = ConcurrentHashMap<String, PendingApiRequest>()
    private val pendingApiChunks = ConcurrentHashMap<String, MutableList<ByteArray>>()
    private val channelStats = ConcurrentHashMap<String, ChannelStats>()

    private val apiLabel get() = "api:$machineId"
    private val eventsLabel get() = "events:$machineId"
    private fun terminalLabel(id: String) = "terminal:$machineId:$id"
    private fun fileLabel(id: String) = "file:$machineId:$id"

    private fun getApiChannelId(): Int = bridge?.getChannelId(apiLabel) ?: -1
    private fun getEventsChannelId(): Int = bridge?.getChannelId(eventsLabel) ?: -1

    inner class PendingApiRequest {
        val latch = CountDownLatch(1)
        var result: String? = null
        var error: Exception? = null
        var path: String = ""
    }

    private data class ApiResponse(
        val id: String,
        val status: Int,
        val body: ByteArray,
        val error: String,
    )

    private data class ChannelStats(
        var rxFrames: Long = 0,
        var rxBytes: Long = 0,
        var txFrames: Long = 0,
        var txBytes: Long = 0,
        var lastLogAt: Long = System.currentTimeMillis(),
        var lastRxBytes: Long = 0,
        var lastTxBytes: Long = 0,
    )

    fun createInitialChannels(pc: PeerConnection) {
        val apiInit = DataChannel.Init().apply { ordered = true }
        apiChannel = pc.createDataChannel("api", apiInit)?.also { setupApiChannel(it) }

        val eventsInit = DataChannel.Init().apply { ordered = true }
        eventsChannel = pc.createDataChannel("events", eventsInit)?.also { setupEventsChannel(it) }
    }

    fun getOrCreateTerminal(pc: PeerConnection?, terminalId: String, connected: Boolean): DataChannel? {
        if (pc == null || !connected) {
            Log.w(TAG, "getOrCreateTerminal[$terminalId]: pc=${pc != null} connected=$connected [$machineId]")
            return null
        }
        terminalChannels[terminalId]?.let {
            Log.i(TAG, "replace terminal[$terminalId] state=${it.state()} [$machineId]")
            terminalChannels.remove(terminalId, it)
            try { it.close() } catch (_: Exception) {}
        }
        val dc = pc.createDataChannel("terminal:$terminalId", DataChannel.Init().apply { ordered = true }) ?: return null
        terminalChannels[terminalId] = dc
        Log.i(TAG, "created terminal[$terminalId] state=${dc.state()} [$machineId]")
        setupTerminalChannel(dc, terminalId)
        return dc
    }

    fun getOrCreateFile(pc: PeerConnection?, transferId: String, connected: Boolean): DataChannel? {
        if (pc == null || !connected) {
            Log.w(TAG, "getOrCreateFile[$transferId]: pc=${pc != null} connected=$connected [$machineId]")
            return null
        }
        val existing = fileChannels[transferId]
        if (existing?.state() == DataChannel.State.OPEN) {
            Log.i(TAG, "reuse open file[$transferId] [$machineId]")
            return existing
        }
        existing?.let {
            Log.i(TAG, "replace file[$transferId] state=${it.state()} [$machineId]")
            fileChannels.remove(transferId)
        }
        val dc = pc.createDataChannel("file:$transferId", DataChannel.Init().apply { ordered = true }) ?: return null
        fileChannels[transferId] = dc
        Log.i(TAG, "created file[$transferId] state=${dc.state()} [$machineId]")
        setupFileChannel(dc, transferId)
        return dc
    }

    fun closeTerminal(terminalId: String) {
        terminalChannels.remove(terminalId)?.let { try { it.close() } catch (_: Exception) {} }
    }

    fun closeFile(transferId: String) {
        fileChannels.remove(transferId)?.let { try { it.close() } catch (_: Exception) {} }
    }

    fun getFileChannel(transferId: String): DataChannel? = fileChannels[transferId]

    fun sendRawApi(data: ByteArray) {
        val dc = apiChannel
        if (dc?.state() == DataChannel.State.OPEN) {
            noteChannelFrame("api", "tx", data.size, dc.bufferedAmount())
            val sent = dc.send(DataChannel.Buffer(ByteBuffer.wrap(data), false))
            if (!sent) Log.w(TAG, "sendRawApi: send returned false buffered=${dc.bufferedAmount()} [$machineId]")
        } else {
            Log.w(TAG, "sendRawApi: apiChannel not open")
        }
    }

    fun sendRawEvents(data: ByteArray) {
        val dc = eventsChannel
        if (dc?.state() == DataChannel.State.OPEN) {
            noteChannelFrame("events", "tx", data.size, dc.bufferedAmount())
            val sent = dc.send(DataChannel.Buffer(ByteBuffer.wrap(data), false))
            if (!sent) Log.w(TAG, "sendRawEvents: send returned false buffered=${dc.bufferedAmount()} [$machineId]")
        } else {
            pendingEventsData.add(data)
            Log.w(TAG, "sendRawEvents: eventsChannel not open")
        }
    }

    fun sendTerminalData(terminalId: String, data: ByteArray) {
        val dc = terminalChannels[terminalId]
        if (dc?.state() == DataChannel.State.OPEN) {
            noteChannelFrame("terminal:$terminalId", "tx", data.size, dc.bufferedAmount())
            val sent = dc.send(DataChannel.Buffer(ByteBuffer.wrap(data), true))
            if (sent) return
            Log.w(TAG, "sendTerminalData[$terminalId]: send returned false [$machineId]")
            notifyTerminalChannelError(terminalId, "terminal channel send failed")
            return
        }
        Log.w(TAG, "sendTerminalData[$terminalId]: channel not open state=${dc?.state()} [$machineId]")
        notifyTerminalChannelError(terminalId, "terminal channel not open")
    }

    fun sendFileData(transferId: String, data: ByteArray) {
        fileChannels[transferId]?.takeIf { it.state() == DataChannel.State.OPEN }
            ?.let {
                noteChannelFrame("file:$transferId", "tx", data.size, it.bufferedAmount())
                it.send(DataChannel.Buffer(ByteBuffer.wrap(data), true))
            }
    }

    /** Blocking API request (for heartbeat). Returns response body or throws on error/timeout. */
    fun sendApiRequest(method: String, path: String, body: String?, timeoutMs: Long): String {
        val dc = apiChannel
        if (dc?.state() != DataChannel.State.OPEN) {
            throw IllegalStateException("api channel not open")
        }
        val id = UUID.randomUUID().toString()
        val pending = PendingApiRequest()
        pending.path = path
        pendingApiRequests[id] = pending
        val startedAt = System.currentTimeMillis()

        val bytes = encodeApiRequest(id, method, path, encodeApiRequestBody(path, body))
        Log.i(TAG, "native api request send id=$id method=$method path=$path bytes=${bytes.size} [$machineId]")
        noteChannelFrame("api", "tx", bytes.size, dc.bufferedAmount())
        dc.send(DataChannel.Buffer(ByteBuffer.wrap(bytes), false))

        if (!pending.latch.await(timeoutMs, TimeUnit.MILLISECONDS)) {
            pendingApiRequests.remove(id)
            Log.w(TAG, "native api request timeout id=$id method=$method path=$path elapsed=${System.currentTimeMillis() - startedAt}ms [$machineId]")
            throw RuntimeException("api request timed out: $method $path")
        }
        pending.error?.let { throw it }
        Log.i(TAG, "native api request done id=$id method=$method path=$path elapsed=${System.currentTimeMillis() - startedAt}ms bytes=${pending.result?.length ?: 0} [$machineId]")
        return pending.result ?: ""
    }

    fun waitApiOpen(timeoutMs: Int): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (apiChannel?.state() == DataChannel.State.OPEN) return true
            Thread.sleep(100)
        }
        return false
    }

    fun isApiOpen(): Boolean = apiChannel?.state() == DataChannel.State.OPEN

    fun closeAll() {
        apiChannel?.let { try { it.close() } catch (_: Exception) {} }
        apiChannel = null
        eventsChannel?.let { try { it.close() } catch (_: Exception) {} }
        eventsChannel = null
        for (dc in terminalChannels.values) try { dc.close() } catch (_: Exception) {}
        terminalChannels.clear()
        for (dc in fileChannels.values) try { dc.close() } catch (_: Exception) {}
        fileChannels.clear()
        pendingEventsData.clear()

        val err = RuntimeException("disconnected")
        for (pending in pendingApiRequests.values) {
            pending.error = err; pending.latch.countDown()
        }
        pendingApiRequests.clear()
        pendingApiChunks.clear()
    }

    // ─── Channel Observers ───────────────────────────────────────────────────

    private fun setupApiChannel(dc: DataChannel) {
        dc.registerObserver(object : DataChannel.Observer {
            override fun onBufferedAmountChange(l: Long) {}
            override fun onStateChange() {
                Log.i(TAG, "api state: ${dc.state()} [$machineId]")
            }
            override fun onMessage(buffer: DataChannel.Buffer) {
                val data = ByteArray(buffer.data.remaining())
                buffer.data.get(data)
                noteChannelFrame("api", "rx", data.size, dc.bufferedAmount())

                // Forward raw bytes to Bridge (JS handles JSON-RPC)
                val chId = getApiChannelId()
                if (chId >= 0) bridge?.sendDataFrame(chId, data)

                // Parse chunked response for native API requests (heartbeat)
                handleApiResponseChunk(data)
            }
        })
    }

    private fun handleApiResponseChunk(data: ByteArray) {
        if (data.isEmpty() || data[0] != API_CHUNK_MAGIC) return
        try {
            val flags = data[1].toInt() and 0xFF
            val idLen = data[2].toInt() and 0xFF
            if (data.size < 3 + idLen) return
            val id = String(data, 3, idLen, StandardCharsets.UTF_8)
            val chunk = data.copyOfRange(3 + idLen, data.size)
            val isLast = (flags and FLAG_LAST) != 0

            val chunks = pendingApiChunks.getOrPut(id) { mutableListOf() }
            chunks.add(chunk)

            if (isLast) {
                pendingApiChunks.remove(id)
                val pending = pendingApiRequests.remove(id) ?: return
                val response = decodeApiResponse(concatByteArrays(chunks))
                if (response.status >= 400 && pending.path == "/status") {
                    pending.error = RuntimeException(response.error.ifBlank { "api request failed: ${response.status}" })
                } else {
                    pending.result = apiResponseJson(response, pending.path).toString()
                }
                pending.latch.countDown()
            }
        } catch (_: Exception) {}
    }

    private fun encodeApiRequest(id: String, method: String, path: String, body: ByteArray): ByteArray {
        return buildProto {
            writeString(1, id)
            writeString(2, method)
            writeString(3, path)
            if (body.isNotEmpty()) writeBytes(4, body)
        }
    }

    private fun decodeApiResponse(data: ByteArray): ApiResponse {
        val reader = ProtoReader(data)
        var id = ""
        var status = 0
        var body = ByteArray(0)
        var error = ""
        while (!reader.done()) {
            val tag = reader.readTag()
            when (tag.field) {
                1 -> id = reader.readString(tag.wire)
                2 -> status = reader.readInt32(tag.wire)
                3 -> body = reader.readBytes(tag.wire)
                4 -> error = reader.readString(tag.wire)
                else -> reader.skip(tag.wire)
            }
        }
        return ApiResponse(id, status, body, error)
    }

    private fun encodeApiRequestBody(path: String, body: String?): ByteArray {
        if (body.isNullOrBlank()) return ByteArray(0)
        val json = JSONObject(body)
        return when (path) {
            "/files/upload/init" -> buildProto {
                writeString(1, json.optString("path"))
                writeInt64(2, json.optLong("size", 0L))
                writeString(3, json.optString("resume_id"))
            }
            "/files/upload/complete" -> buildProto {
                writeString(1, json.optString("transfer_id"))
            }
            "/files/download/init" -> buildProto {
                writeString(1, json.optString("path"))
                writeInt64(2, json.optLong("offset", 0L))
                writeInt64(3, json.optLong("length", 0L))
                writeString(4, json.optString("transfer_id"))
            }
            else -> body.toByteArray(StandardCharsets.UTF_8)
        }
    }

    private fun apiResponseJson(response: ApiResponse, path: String): JSONObject {
        val body = if (response.status >= 400) {
            JSONObject().put("error", response.error.ifBlank { "api request failed: ${response.status}" })
        } else {
            decodeApiResponseBody(path, response.body)
        }
        return JSONObject()
            .put("id", response.id)
            .put("status", response.status)
            .put("body", body)
    }

    private fun decodeApiResponseBody(path: String, data: ByteArray): JSONObject {
        if (data.isEmpty()) return JSONObject()
        val reader = ProtoReader(data)
        return when (path) {
            "/status" -> decodeStatusResponse(reader)
            "/files/upload/init" -> decodeFileUploadInitResponse(reader)
            "/files/upload/complete" -> decodeFilePathResponse(reader)
            "/files/download/init" -> decodeFileDownloadInitResponse(reader)
            else -> JSONObject()
        }
    }

    private fun decodeStatusResponse(reader: ProtoReader): JSONObject {
        val out = JSONObject()
        while (!reader.done()) {
            val tag = reader.readTag()
            when (tag.field) {
                1 -> out.put("ok", reader.readBool(tag.wire))
                else -> reader.skip(tag.wire)
            }
        }
        return out
    }

    private fun decodeFileUploadInitResponse(reader: ProtoReader): JSONObject {
        val out = JSONObject()
        while (!reader.done()) {
            val tag = reader.readTag()
            when (tag.field) {
                1 -> out.put("transfer_id", reader.readString(tag.wire))
                2 -> out.put("chunk_size", reader.readInt32(tag.wire))
                3 -> out.put("uploaded_offset", reader.readInt64(tag.wire))
                else -> reader.skip(tag.wire)
            }
        }
        return out
    }

    private fun decodeFilePathResponse(reader: ProtoReader): JSONObject {
        val out = JSONObject()
        while (!reader.done()) {
            val tag = reader.readTag()
            when (tag.field) {
                1 -> out.put("path", reader.readString(tag.wire))
                else -> reader.skip(tag.wire)
            }
        }
        return out
    }

    private fun decodeFileDownloadInitResponse(reader: ProtoReader): JSONObject {
        val out = JSONObject()
        while (!reader.done()) {
            val tag = reader.readTag()
            when (tag.field) {
                1 -> out.put("transfer_id", reader.readString(tag.wire))
                2 -> out.put("name", reader.readString(tag.wire))
                3 -> out.put("size", reader.readInt64(tag.wire))
                4 -> out.put("chunk_size", reader.readInt32(tag.wire))
                5 -> out.put("offset", reader.readInt64(tag.wire))
                6 -> out.put("length", reader.readInt64(tag.wire))
                else -> reader.skip(tag.wire)
            }
        }
        return out
    }

    private fun concatByteArrays(chunks: List<ByteArray>): ByteArray {
        val total = chunks.sumOf { it.size }
        val out = ByteArray(total)
        var offset = 0
        for (chunk in chunks) {
            System.arraycopy(chunk, 0, out, offset, chunk.size)
            offset += chunk.size
        }
        return out
    }

    private fun buildProto(block: ProtoWriter.() -> Unit): ByteArray {
        val writer = ProtoWriter()
        writer.block()
        return writer.toByteArray()
    }

    private class ProtoWriter {
        private val out = ArrayList<Byte>()

        fun writeString(field: Int, value: String) {
            if (value.isEmpty()) return
            writeBytes(field, value.toByteArray(StandardCharsets.UTF_8))
        }

        fun writeBytes(field: Int, value: ByteArray) {
            writeTag(field, 2)
            writeVarint(value.size.toLong())
            for (byte in value) out.add(byte)
        }

        fun writeInt64(field: Int, value: Long) {
            if (value == 0L) return
            writeTag(field, 0)
            writeVarint(value)
        }

        fun toByteArray(): ByteArray {
            return ByteArray(out.size) { index -> out[index] }
        }

        private fun writeTag(field: Int, wire: Int) {
            writeVarint(((field shl 3) or wire).toLong())
        }

        private fun writeVarint(input: Long) {
            var value = input
            while ((value and 0x7FL.inv()) != 0L) {
                out.add((((value and 0x7F) or 0x80).toInt()).toByte())
                value = value ushr 7
            }
            out.add(value.toByte())
        }
    }

    private class ProtoReader(private val data: ByteArray) {
        private var offset = 0

        data class Tag(val field: Int, val wire: Int)

        fun done(): Boolean = offset >= data.size

        fun readTag(): Tag {
            val tag = readVarint().toInt()
            return Tag(tag ushr 3, tag and 0x07)
        }

        fun readString(wire: Int): String = String(readBytes(wire), StandardCharsets.UTF_8)

        fun readBytes(wire: Int): ByteArray {
            requireWire(wire, 2)
            val length = readVarint().toInt()
            if (length < 0 || offset + length > data.size) throw IllegalArgumentException("protobuf length out of bounds")
            val out = data.copyOfRange(offset, offset + length)
            offset += length
            return out
        }

        fun readInt32(wire: Int): Int {
            requireWire(wire, 0)
            return readVarint().toInt()
        }

        fun readInt64(wire: Int): Long {
            requireWire(wire, 0)
            return readVarint()
        }

        fun readBool(wire: Int): Boolean {
            requireWire(wire, 0)
            return readVarint() != 0L
        }

        fun skip(wire: Int) {
            when (wire) {
                0 -> readVarint()
                1 -> {
                    offset += 8
                    checkBounds()
                }
                2 -> {
                    val length = readVarint().toInt()
                    offset += length
                    checkBounds()
                }
                5 -> {
                    offset += 4
                    checkBounds()
                }
                else -> throw IllegalArgumentException("unsupported protobuf wire type $wire")
            }
        }

        private fun readVarint(): Long {
            var shift = 0
            var result = 0L
            while (true) {
                if (offset >= data.size) throw IllegalArgumentException("unexpected EOF in protobuf varint")
                val byte = data[offset++].toInt() and 0xFF
                result = result or ((byte and 0x7F).toLong() shl shift)
                if ((byte and 0x80) == 0) return result
                shift += 7
                if (shift > 70) throw IllegalArgumentException("protobuf varint too long")
            }
        }

        private fun requireWire(actual: Int, expected: Int) {
            if (actual != expected) throw IllegalArgumentException("protobuf wire type $actual != $expected")
        }

        private fun checkBounds() {
            if (offset < 0 || offset > data.size) throw IllegalArgumentException("protobuf skip out of bounds")
        }
    }

    private fun setupEventsChannel(dc: DataChannel) {
        dc.registerObserver(object : DataChannel.Observer {
            override fun onBufferedAmountChange(l: Long) {}
            override fun onStateChange() {
                Log.i(TAG, "events state: ${dc.state()} [$machineId]")
                if (dc.state() == DataChannel.State.OPEN) {
                    while (true) {
                        val data = pendingEventsData.poll() ?: break
                        dc.send(DataChannel.Buffer(ByteBuffer.wrap(data), false))
                    }
                }
            }
            override fun onMessage(buffer: DataChannel.Buffer) {
                val data = ByteArray(buffer.data.remaining())
                buffer.data.get(data)
                noteChannelFrame("events", "rx", data.size, dc.bufferedAmount())
                val chId = getEventsChannelId()
                if (chId >= 0) bridge?.sendDataFrame(chId, data)
            }
        })
    }

    private fun setupTerminalChannel(dc: DataChannel, terminalId: String) {
        dc.registerObserver(object : DataChannel.Observer {
            override fun onBufferedAmountChange(l: Long) {}
            override fun onStateChange() {
                Log.i(TAG, "terminal[$terminalId] state: ${dc.state()} [$machineId]")
                when (dc.state()) {
                    DataChannel.State.OPEN -> {
                        val label = terminalLabel(terminalId)
                        val chId = bridge?.getChannelId(label) ?: -1
                        if (chId >= 0) bridge?.sendChanOpened(chId, label)
                    }
                    DataChannel.State.CLOSED -> {
                        if (terminalChannels.remove(terminalId, dc)) {
                            val label = terminalLabel(terminalId)
                            val chId = bridge?.getChannelId(label) ?: -1
                            if (chId >= 0) bridge?.sendCloseChannel(chId)
                        }
                    }
                    else -> {}
                }
            }
            override fun onMessage(buffer: DataChannel.Buffer) {
                val data = ByteArray(buffer.data.remaining())
                buffer.data.get(data)
                noteChannelFrame("terminal:$terminalId", "rx", data.size, dc.bufferedAmount())
                if (bridge?.hasClients() == true) {
                    val chId = bridge.getChannelId(terminalLabel(terminalId))
                    if (chId >= 0) bridge.sendDataFrame(chId, data)
                }
            }
        })
    }

    private fun notifyTerminalChannelError(terminalId: String, message: String) {
        val label = terminalLabel(terminalId)
        val chId = bridge?.getChannelId(label) ?: -1
        if (chId >= 0) bridge?.sendChanError(chId, message)
    }

    private fun setupFileChannel(dc: DataChannel, transferId: String) {
        dc.registerObserver(object : DataChannel.Observer {
            override fun onBufferedAmountChange(previousAmount: Long) {
                fileTransferManager?.onBufferedAmountChange(transferId, dc.bufferedAmount())
            }
            override fun onStateChange() {
                Log.i(TAG, "file[$transferId] state: ${dc.state()} [$machineId]")
                when (dc.state()) {
                    DataChannel.State.OPEN -> {
                        fileTransferManager?.onChannelOpen(transferId)
                        val label = fileLabel(transferId)
                        val chId = bridge?.getChannelId(label) ?: -1
                        if (chId >= 0) bridge?.sendChanOpened(chId, label)
                    }
                    DataChannel.State.CLOSED -> {
                        fileChannels.remove(transferId)
                        val label = fileLabel(transferId)
                        val chId = bridge?.getChannelId(label) ?: -1
                        if (chId >= 0) bridge?.sendCloseChannel(chId)
                    }
                    else -> {}
                }
            }
            override fun onMessage(buffer: DataChannel.Buffer) {
                val data = ByteArray(buffer.data.remaining())
                buffer.data.get(data)
                noteChannelFrame("file:$transferId", "rx", data.size, dc.bufferedAmount())
                // Native transfer manager takes priority for downloads it owns
                if (fileTransferManager?.isHandling(transferId) == true) {
                    fileTransferManager?.onFileData(transferId, data)
                    return
                }
                if (bridge?.hasClients() == true) {
                    val chId = bridge.getChannelId(fileLabel(transferId))
                    if (chId >= 0) bridge.sendDataFrame(chId, data)
                }
            }
        })
    }

    private fun noteChannelFrame(label: String, direction: String, bytes: Int, bufferedAmount: Long) {
        val stats = channelStats.getOrPut(label) { ChannelStats() }
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
                Log.w(TAG, "large datachannel payload direction=$direction label=$label bytes=$bytes buffered=$bufferedAmount [$machineId]")
            }
            if (now - stats.lastLogAt < STATS_INTERVAL_MS) return
            val elapsed = ((now - stats.lastLogAt).coerceAtLeast(1)).toDouble() / 1000.0
            val intervalRx = stats.rxBytes - stats.lastRxBytes
            val intervalTx = stats.txBytes - stats.lastTxBytes
            Log.i(TAG, "datachannel stats label=$label rxFrames=${stats.rxFrames} rxBytes=${stats.rxBytes} txFrames=${stats.txFrames} txBytes=${stats.txBytes} rxBps=${(intervalRx / elapsed).toLong()} txBps=${(intervalTx / elapsed).toLong()} buffered=$bufferedAmount [$machineId]")
            stats.lastLogAt = now
            stats.lastRxBytes = stats.rxBytes
            stats.lastTxBytes = stats.txBytes
        }
    }
}
