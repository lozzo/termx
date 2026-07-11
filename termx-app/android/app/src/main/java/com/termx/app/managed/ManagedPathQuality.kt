package com.termx.app.managed

import kotlin.math.abs
import java.math.BigInteger

/**
 * ManagedPathQualityMetadata 是 Android 质量窗口允许携带的匿名关联信息。
 * managedSessionId 只用于短期观测关联；network/region/carrier/provider 必须是受控 taxonomy，
 * 不能填写 IP、hostname、endpoint label、terminal identity 或 credential reference。
 */
data class ManagedPathQualityMetadata(
    val managedSessionId: String,
    val observedPath: ObservedPath,
    val networkClass: String,
    val region: String = "",
    val carrierTag: String = "",
    val providerTag: String = "",
) {
    /** normalized 校验并返回 canonical metadata；失败值不能进入 reporter 或官方 cloud adapter。 */
    fun normalized(): ManagedPathQualityMetadata {
        val sessionId = managedSessionId.trim()
        require(sessionId.isNotEmpty() && sessionId.length <= 128 && sessionId.none(Char::isWhitespace)) {
            "invalid managed session correlation id"
        }
        return copy(
            managedSessionId = sessionId,
            networkClass = normalizeQualityTag("network class", networkClass, true),
            region = normalizeQualityTag("region", region, false),
            carrierTag = normalizeQualityTag("carrier tag", carrierTag, false),
            providerTag = normalizeQualityTag("provider tag", providerTag, false),
        )
    }
}

/**
 * ManagedPathQualitySample 是同一 selected candidate pair 的累计 transport stats。
 * bytes/packets/lossEvents 必须单调；lossEvents 只表示 ICE retransmission 与本地 send discard，
 * 不保存原始 packet、SDP、DataChannel payload 或远端地址。
 */
data class ManagedPathQualitySample(
    val pairId: String,
    val observedPath: ObservedPath,
    val sampledAtUnixMillis: Long,
    val roundTripTimeMillis: Long,
    val bytesSent: Long,
    val bytesReceived: Long,
    val packetsSent: Long,
    val lossEvents: Long,
    val connected: Boolean,
    val networkClass: String = "unknown",
) {
    /** validate 拒绝缺失 pair、非正时间、负 RTT 或超出 Long 累计域的 stats。 */
    fun validate() {
        require(pairId.isNotBlank()) { "candidate pair id is required" }
        require(sampledAtUnixMillis > 0) { "sample timestamp is invalid" }
        require(roundTripTimeMillis >= 0) { "sample RTT is invalid" }
        require(bytesSent >= 0 && bytesReceived >= 0 && packetsSent >= 0 && lossEvents >= 0) {
            "sample cumulative counters are invalid"
        }
        normalizeQualityTag("network class", networkClass, true)
    }
}

/**
 * ManagedPathQualitySummary 与公开 PathQualitySummary v2 字段逐项对应。
 * 该 DTO 没有 cost、terminal、grant、payload、address 或 credential 字段；可信成本只能由私有服务
 * 在质量窗口入库后通过 observation reference 异步关联。
 */
data class ManagedPathQualitySummary(
    val managedSessionId: String,
    val observedPath: ObservedPath,
    val rttP50Millis: Long,
    val jitterMillis: Long,
    val lossBasisPoints: Int,
    val throughputBps: Long,
    val connectedMillis: Long,
    val networkClass: String,
    val region: String,
    val rttP95Millis: Long,
    val sampleCount: Int,
    val disconnectCount: Int,
    val windowStartedAtUnixMillis: Long,
    val windowEndedAtUnixMillis: Long,
    val packetCount: Long,
    val lossEventCount: Long,
    val carrierTag: String,
    val providerTag: String,
) {
    companion object {
        /** WIRE_FIELDS 是共享 fixture 与官方移动 cloud mapper 使用的唯一字段顺序。 */
        val WIRE_FIELDS = listOf(
            "managed_session_id", "observed_path", "rtt_p50_millis", "jitter_millis",
            "loss_basis_points", "throughput_bps", "connected_millis", "network_class", "region",
            "rtt_p95_millis", "sample_count", "disconnect_count", "window_started_at_unix_millis",
            "window_ended_at_unix_millis", "packet_count", "loss_event_count", "carrier_tag", "provider_tag",
        )
    }

    /** validate 固化跨平台窗口关系；无效 summary 不得交给 ManagedCloudAdapter。 */
    fun validate() {
        ManagedPathQualityMetadata(
            managedSessionId, observedPath, networkClass, region, carrierTag, providerTag,
        ).normalized()
        require(windowStartedAtUnixMillis > 0 && windowEndedAtUnixMillis > windowStartedAtUnixMillis) {
            "quality window bounds are invalid"
        }
        require(sampleCount >= 2 && disconnectCount in 0 until sampleCount) { "quality counts are invalid" }
        require(rttP50Millis >= 0 && rttP95Millis >= rttP50Millis && jitterMillis >= 0) {
            "quality latency summary is invalid"
        }
        require(lossBasisPoints in 0..10_000 && lossEventCount in 0..packetCount) {
            "quality loss summary is invalid"
        }
        require(lossBasisPoints == qualityBasisPoints(lossEventCount, packetCount)) {
            "quality loss ratio does not match counters"
        }
        require(throughputBps >= 0 && connectedMillis in 0..(windowEndedAtUnixMillis - windowStartedAtUnixMillis)) {
            "quality transport summary is invalid"
        }
    }
}

/** ManagedPathQualitySampleSource 是 reporter 读取平台 WebRTC stats 的最小 primitive。 */
fun interface ManagedPathQualitySampleSource {
    /** readPathQualitySample 返回当前 selected candidate pair 的脱敏累计样本；stats 未就绪时返回 null。 */
    fun readPathQualitySample(): ManagedPathQualitySample?

    /** readFinalPathQualitySample 必须非阻塞返回关闭前缓存；默认实现只供无异步 primitive 的 harness。 */
    fun readFinalPathQualitySample(): ManagedPathQualitySample? = readPathQualitySample()
}

/**
 * ManagedPathQualityCollector 是单个 managed session、单个 observed path 的窗口 owner。
 * flush 保留最后累计样本作为下一窗口基线，避免 bytes、loss 和 connected duration 重复计算。
 */
class ManagedPathQualityCollector(
    metadata: ManagedPathQualityMetadata,
    initial: ManagedPathQualitySample,
) {
    private val metadata = metadata.normalized()
    private val samples = mutableListOf<ManagedPathQualitySample>()

    init {
        initial.validate()
        require(initial.observedPath == this.metadata.observedPath) { "sample path does not match metadata" }
        samples += initial.copy(networkClass = normalizeQualityTag("network class", initial.networkClass, true))
    }

    /** observe 追加同一 candidate pair 的时间递增累计样本；pair/path/counter 变化必须由 reporter 切新窗口。 */
    fun observe(sample: ManagedPathQualitySample) {
        sample.validate()
        val previous = samples.last()
        require(sample.pairId == previous.pairId && sample.observedPath == metadata.observedPath) {
            "candidate pair changed inside quality window"
        }
        require(sample.sampledAtUnixMillis > previous.sampledAtUnixMillis) { "sample time did not advance" }
        require(
            sample.bytesSent >= previous.bytesSent && sample.bytesReceived >= previous.bytesReceived &&
                sample.packetsSent >= previous.packetsSent && sample.lossEvents >= previous.lossEvents,
        ) { "quality cumulative counter rolled back" }
        samples += sample.copy(networkClass = normalizeQualityTag("network class", sample.networkClass, true))
    }

    /** flush 完成当前窗口；样本不足时返回 null，不伪造单点质量。 */
    fun flush(): ManagedPathQualitySummary? {
        if (samples.size < 2) return null
        val first = samples.first()
        val last = samples.last()
        val rtts = samples.map { it.roundTripTimeMillis }.sorted()
        var jitterTotal = 0L
        var connectedMillis = 0L
        var disconnects = 0
        samples.zipWithNext().forEach { (previous, current) ->
            jitterTotal = saturatingQualityAdd(jitterTotal, abs(current.roundTripTimeMillis - previous.roundTripTimeMillis))
            if (previous.connected) {
                connectedMillis = saturatingQualityAdd(connectedMillis, current.sampledAtUnixMillis - previous.sampledAtUnixMillis)
            }
            if (previous.connected && !current.connected) disconnects++
        }
        val packetDelta = last.packetsSent - first.packetsSent
        val lossDelta = last.lossEvents - first.lossEvents
        val packetCount = saturatingQualityAdd(packetDelta, lossDelta)
        val byteCount = saturatingQualityAdd(last.bytesSent - first.bytesSent, last.bytesReceived - first.bytesReceived)
        val durationMillis = last.sampledAtUnixMillis - first.sampledAtUnixMillis
        val summary = ManagedPathQualitySummary(
            managedSessionId = metadata.managedSessionId,
            observedPath = metadata.observedPath,
            rttP50Millis = nearestQualityRank(rtts, 50),
            jitterMillis = jitterTotal / (samples.size - 1),
            lossBasisPoints = qualityBasisPoints(lossDelta, packetCount),
            throughputBps = qualityBitrate(byteCount, durationMillis),
            connectedMillis = connectedMillis,
            networkClass = metadata.networkClass,
            region = metadata.region,
            rttP95Millis = nearestQualityRank(rtts, 95),
            sampleCount = samples.size,
            disconnectCount = disconnects,
            windowStartedAtUnixMillis = first.sampledAtUnixMillis,
            windowEndedAtUnixMillis = last.sampledAtUnixMillis,
            packetCount = packetCount,
            lossEventCount = lossDelta,
            carrierTag = metadata.carrierTag,
            providerTag = metadata.providerTag,
        )
        summary.validate()
        samples.clear()
        samples += last
        return summary
    }
}

private val QUALITY_TAG = Regex("[a-z0-9._-]{1,64}")
private val IPV4_TAG = Regex("(?:[0-9]{1,3}\\.){3}[0-9]{1,3}")

private fun normalizeQualityTag(name: String, value: String, required: Boolean): String {
    val normalized = value.trim().lowercase()
    if (normalized.isEmpty()) {
        require(!required) { "$name is required" }
        return ""
    }
    require(QUALITY_TAG.matches(normalized) && !IPV4_TAG.matches(normalized) && !normalized.contains(':')) {
        "invalid $name"
    }
    return normalized
}

private fun nearestQualityRank(values: List<Long>, percentile: Int): Long {
    if (values.isEmpty()) return 0
    val index = ((percentile * values.size + 99) / 100 - 1).coerceAtLeast(0)
    return values[index]
}

private fun qualityBasisPoints(lossEvents: Long, packetCount: Long): Int {
    if (lossEvents <= 0 || packetCount <= 0) return 0
    if (lossEvents >= packetCount) return 10_000
    val denominator = BigInteger.valueOf(packetCount)
    val scaled = BigInteger.valueOf(lossEvents).multiply(BigInteger.valueOf(10_000))
    val quotientAndRemainder = scaled.divideAndRemainder(denominator)
    var quotient = quotientAndRemainder[0].toInt()
    if (quotientAndRemainder[1].shiftLeft(1) >= denominator) quotient++
    return quotient.coerceIn(0, 10_000)
}

private fun qualityBitrate(bytes: Long, durationMillis: Long): Long {
    if (bytes <= 0 || durationMillis <= 0) return 0
    val bitrate = bytes.toDouble() * 8.0 * 1000.0 / durationMillis.toDouble()
    return if (!bitrate.isFinite() || bitrate >= Long.MAX_VALUE.toDouble()) Long.MAX_VALUE else bitrate.toLong()
}

private fun saturatingQualityAdd(left: Long, right: Long): Long {
    if (right <= 0) return left
    return if (Long.MAX_VALUE - left < right) Long.MAX_VALUE else left + right
}
