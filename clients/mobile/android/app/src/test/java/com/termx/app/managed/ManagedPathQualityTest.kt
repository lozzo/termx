package com.termx.app.managed

import com.google.gson.Gson
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.lang.reflect.Modifier
import java.util.ArrayDeque

class ManagedPathQualityTest {
    private data class Fixture(
        val schema_version: Int,
        val summary_fields: List<String>,
        val forbidden_fragments: List<String>,
        val sample: SampleFixture,
    )

    private data class SampleFixture(
        val managed_session_id: String,
        val observed_path: String,
        val rtt_p50_millis: Long,
        val jitter_millis: Long,
        val loss_basis_points: Int,
        val throughput_bps: Long,
        val connected_millis: Long,
        val network_class: String,
        val region: String,
        val rtt_p95_millis: Long,
        val sample_count: Int,
        val disconnect_count: Int,
        val window_started_at_unix_millis: Long,
        val window_ended_at_unix_millis: Long,
        val packet_count: Long,
        val loss_event_count: Long,
        val carrier_tag: String,
        val provider_tag: String,
    )

    @Test
    fun sharedFixtureMatchesAndroidSummaryAndPrivacyBoundary() {
        val payload = requireNotNull(javaClass.getResourceAsStream("/path_quality_contract.json"))
            .bufferedReader().use { it.readText() }
        val fixture = Gson().fromJson(payload, Fixture::class.java)
        assertEquals(2, fixture.schema_version)
        assertEquals(fixture.summary_fields, ManagedPathQualitySummary.WIRE_FIELDS)
        val actualFields = ManagedPathQualitySummary::class.java.declaredFields
            .filterNot { field -> field.isSynthetic || Modifier.isStatic(field.modifiers) }
            .map { field -> camelCaseToSnakeCase(field.name) }
        assertEquals(fixture.summary_fields.sorted(), actualFields.sorted())
        actualFields.forEach { field ->
            fixture.forbidden_fragments.forEach { fragment ->
                assertFalse("$field contains $fragment", field.contains(fragment))
            }
        }
        val sample = fixture.sample
        val summary = ManagedPathQualitySummary(
            managedSessionId = sample.managed_session_id,
            observedPath = ObservedPath.entries.first { it.wireName == sample.observed_path },
            rttP50Millis = sample.rtt_p50_millis,
            jitterMillis = sample.jitter_millis,
            lossBasisPoints = sample.loss_basis_points,
            throughputBps = sample.throughput_bps,
            connectedMillis = sample.connected_millis,
            networkClass = sample.network_class,
            region = sample.region,
            rttP95Millis = sample.rtt_p95_millis,
            sampleCount = sample.sample_count,
            disconnectCount = sample.disconnect_count,
            windowStartedAtUnixMillis = sample.window_started_at_unix_millis,
            windowEndedAtUnixMillis = sample.window_ended_at_unix_millis,
            packetCount = sample.packet_count,
            lossEventCount = sample.loss_event_count,
            carrierTag = sample.carrier_tag,
            providerTag = sample.provider_tag,
        )
        summary.validate()
    }

    @Test
    fun collectorMatchesGoWindowSemantics() {
        val startedAt = 1_700_000_000_000L
        val collector = ManagedPathQualityCollector(
            ManagedPathQualityMetadata("managed-1", ObservedPath.SINGLE_RELAY, "WiFi", "EU-West", "carrier-a", "provider-a"),
            qualitySample(startedAt, 40, 1_000, 2_000, 100, 2, true),
        )
        collector.observe(qualitySample(startedAt + 10_000, 60, 3_000, 4_000, 200, 4, true))
        collector.observe(qualitySample(startedAt + 20_000, 50, 5_000, 6_000, 300, 6, false))
        collector.observe(qualitySample(startedAt + 30_000, 100, 7_000, 8_000, 400, 8, true))
        val summary = requireNotNull(collector.flush())
        assertEquals(50, summary.rttP50Millis)
        assertEquals(100, summary.rttP95Millis)
        assertEquals(26, summary.jitterMillis)
        assertEquals(196, summary.lossBasisPoints)
        assertEquals(3_200, summary.throughputBps)
        assertEquals(20_000, summary.connectedMillis)
        assertEquals(1, summary.disconnectCount)
        assertEquals(306, summary.packetCount)
        assertEquals(6, summary.lossEventCount)
    }

    @Test
    fun collectorLossRatioMatchesGoAtLargeCounters() {
        val startedAt = 1_700_000_000_000L
        val collector = ManagedPathQualityCollector(
            ManagedPathQualityMetadata("managed-1", ObservedPath.SINGLE_RELAY, "wifi"),
            qualitySample(startedAt, 1, 0, 0, 0, 0, true),
        )
        collector.observe(
            qualitySample(
                startedAt + 1_000,
                1,
                0,
                0,
                Long.MAX_VALUE / 2,
                Long.MAX_VALUE / 2,
                true,
            ),
        )
        assertEquals(5_000, requireNotNull(collector.flush()).lossBasisPoints)
    }

    @Test
    fun reporterOnlySubmitsQualityWindows() = runBlocking {
        val startedAt = 1_700_000_000_000L
        val source = SequenceSource(
            qualitySample(startedAt, 20, 100, 200, 10, 0, true),
            qualitySample(startedAt + 5_000, 30, 200, 300, 20, 1, true),
            qualitySample(startedAt + 10_000, 40, 300, 400, 30, 1, true),
            qualitySample(startedAt + 15_000, 50, 400, 500, 40, 2, true),
        )
        val cloud = RecordingCloudAdapter()
        val reporter = ManagedPathQualityReporter(
            cloud = cloud,
            managedSessionId = "managed-1",
            source = source,
            sampleIntervalMillis = 5,
            windowMillis = 10,
            scope = CoroutineScope(SupervisorJob() + Dispatchers.Default),
        )
        reporter.start()
        withTimeout(2_000) {
            while (cloud.qualityReportCount() == 0) delay(5)
        }
        reporter.stop()
        reporter.awaitStopped()
        val reports = cloud.qualityReportSnapshot()
        assertTrue(reports.isNotEmpty())
        assertEquals(0, cloud.resolveCalls)
        assertEquals(0, cloud.signalingCalls)
        reports.forEach(ManagedPathQualitySummary::validate)
    }

    private fun qualitySample(
        at: Long,
        rtt: Long,
        bytesSent: Long,
        bytesReceived: Long,
        packetsSent: Long,
        lossEvents: Long,
        connected: Boolean,
    ) = ManagedPathQualitySample(
        pairId = "pair-1",
        observedPath = ObservedPath.SINGLE_RELAY,
        sampledAtUnixMillis = at,
        roundTripTimeMillis = rtt,
        bytesSent = bytesSent,
        bytesReceived = bytesReceived,
        packetsSent = packetsSent,
        lossEvents = lossEvents,
        connected = connected,
        networkClass = "wifi",
    )
}

private fun camelCaseToSnakeCase(value: String): String = buildString {
    value.forEachIndexed { index, character ->
        if (character.isUpperCase() && index > 0) append('_')
        append(character.lowercaseChar())
    }
}

private class SequenceSource(vararg samples: ManagedPathQualitySample) : ManagedPathQualitySampleSource {
    private val samples = ArrayDeque(samples.toList())

    override fun readPathQualitySample(): ManagedPathQualitySample? = synchronized(samples) {
        if (samples.isEmpty()) null else samples.removeFirst()
    }
}

private class RecordingCloudAdapter : ManagedCloudAdapter {
    override suspend fun beginLogin(): ManagedCloudLoginFlow = error("not used")
    override suspend fun completeLogin(flowId: String): ManagedCloudAccount = error("not used")
    override suspend fun currentAccount(): ManagedCloudAccount? = null
    override suspend fun logout() = Unit
    var resolveCalls = 0
    var signalingCalls = 0
    private val qualityReports = mutableListOf<ManagedPathQualitySummary>()

    fun qualityReportCount(): Int = synchronized(qualityReports) { qualityReports.size }

    fun qualityReportSnapshot(): List<ManagedPathQualitySummary> = synchronized(qualityReports) { qualityReports.toList() }

    override suspend fun resolve(spec: ManagedEndpointSpec): ManagedEndpointResolution {
        resolveCalls++
        throw ManagedEndpointFailure("protocol", "unexpected resolve")
    }

    override suspend fun createSignalingSession(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        offer: ManagedSignalOffer,
        policy: ManagedDialPolicy,
    ): ManagedSignalAnswer {
        signalingCalls++
        throw ManagedEndpointFailure("protocol", "unexpected signaling")
    }

    override suspend fun reportPathQuality(summary: ManagedPathQualitySummary) {
        synchronized(qualityReports) { qualityReports += summary }
    }

    override suspend fun planManagedRoute(
        spec: ManagedEndpointSpec,
        resolution: ManagedEndpointResolution,
        policy: ManagedDialPolicy,
    ): ManagedRoutePlan {
        throw ManagedEndpointFailure("protocol", "unexpected route planning")
    }
}
