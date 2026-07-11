package com.termx.app.managed

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.atomic.AtomicBoolean

/**
 * ManagedPathQualityReporter 按固定周期从 Android WebRTC primitive 读取累计 stats 并上报质量窗口。
 * 上报失败只丢弃当前 telemetry；本类没有 lease、route selection、ICE restart、重连或 endpoint state API。
 */
class ManagedPathQualityReporter(
    private val cloud: ManagedCloudAdapter,
    private val managedSessionId: String,
    private val source: ManagedPathQualitySampleSource,
    private val sampleIntervalMillis: Long = DEFAULT_SAMPLE_INTERVAL_MILLIS,
    private val windowMillis: Long = DEFAULT_WINDOW_MILLIS,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
) {
    companion object {
        /** DEFAULT_SAMPLE_INTERVAL_MILLIS 与 Go desktop/headless 观测周期保持一致。 */
        const val DEFAULT_SAMPLE_INTERVAL_MILLIS = 5_000L

        /** DEFAULT_WINDOW_MILLIS 与 Go desktop/headless summary 窗口保持一致。 */
        const val DEFAULT_WINDOW_MILLIS = 60_000L

        /** REPORT_TIMEOUT_MILLIS 限制 telemetry 慢请求，防止阻塞后续 stats 采样。 */
        const val REPORT_TIMEOUT_MILLIS = 2_000L
    }

    private sealed interface Event {
        data class Stop(val finalSample: ManagedPathQualitySample?) : Event
    }

    private val started = AtomicBoolean(false)
    private val stopped = AtomicBoolean(false)
    private val events = Channel<Event>(capacity = 1)
    private var job: Job? = null
    private var collector: ManagedPathQualityCollector? = null
    private var pairId = ""
    private var path: ObservedPath? = null
    private var windowStartedAt = 0L

    init {
        require(sampleIntervalMillis > 0 && windowMillis >= sampleIntervalMillis) { "invalid quality reporter intervals" }
        ManagedPathQualityMetadata(managedSessionId, ObservedPath.DIRECT, "unknown").normalized()
    }

    /** start 幂等启动 observer；stats 尚未就绪时等待下一采样周期，不伪造窗口。 */
    fun start() {
        if (!started.compareAndSet(false, true)) return
        job = scope.launch {
            readSample()?.let { ingest(it) }
            while (true) {
                val event = withTimeoutOrNull(sampleIntervalMillis) { events.receive() }
                if (event is Event.Stop) {
                    event.finalSample?.let { ingest(it) }
                    flush()
                    return@launch
                }
                readSample()?.let { ingest(it) }
            }
        }
    }

    /** stop 在 PeerConnection 关闭前抓取最终累计样本，并异步完成最后窗口；调用方不等待 cloud telemetry。 */
    fun stop() {
        if (!stopped.compareAndSet(false, true)) return
        events.trySend(Event.Stop(runCatching { source.readFinalPathQualitySample() }.getOrNull()))
    }

    /** awaitStopped 只供 deterministic harness 等待 reporter 收尾；产品连接生命周期不依赖 telemetry 完成。 */
    suspend fun awaitStopped() {
        job?.join()
    }

    private fun readSample(): ManagedPathQualitySample? =
        runCatching { source.readPathQualitySample() }.getOrNull()

    private suspend fun ingest(sample: ManagedPathQualitySample) {
        val current = collector
        if (current == null || pairId != sample.pairId || path != sample.observedPath) {
            flush()
            startWindow(sample)
            return
        }
        val accepted = runCatching { current.observe(sample) }.isSuccess
        if (!accepted) {
            flush()
            startWindow(sample)
            return
        }
        if (sample.sampledAtUnixMillis - windowStartedAt >= windowMillis) flush()
    }

    private fun startWindow(sample: ManagedPathQualitySample) {
        val metadata = ManagedPathQualityMetadata(
            managedSessionId = managedSessionId,
            observedPath = sample.observedPath,
            networkClass = sample.networkClass,
        )
        val next = runCatching { ManagedPathQualityCollector(metadata, sample) }.getOrNull() ?: return
        collector = next
        pairId = sample.pairId
        path = sample.observedPath
        windowStartedAt = sample.sampledAtUnixMillis
    }

    private suspend fun flush() {
        val summary = runCatching { collector?.flush() }.getOrNull() ?: return
        windowStartedAt = summary.windowEndedAtUnixMillis
        // 质量上报不是授权或路由事务；错误不得传回 transport 或触发 fallback。
        withTimeoutOrNull(REPORT_TIMEOUT_MILLIS) {
            runCatching { cloud.reportPathQuality(summary) }
        }
    }
}
