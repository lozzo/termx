package com.termx.app.managed

import org.junit.Assert.assertTrue
import org.junit.Test

class ManagedRoutePlanTest {
    private val spec = ManagedEndpointSpec("studio", "daemon-1", "SHA256:daemon", "grant-1", RelayMode.SMART_ROUTE)
    private val resolution = ManagedEndpointResolution("managed-1", "daemon-1", emptyList())

    @Test
    fun directAndSingleRelayPlansEnforceSelectedIcePolicy() {
        ManagedRoutePlan(
            "plan-direct", "managed-1", "daemon-1", ObservedPath.DIRECT,
            ManagedRouteSelectionReason.COST_GUARD, 1_700_000_060L,
            listOf(ManagedIceServer(listOf("stun:stun.example.com"))), false, "",
        ).validate(spec, resolution, 1_700_000_000L)

        ManagedRoutePlan(
            "plan-relay", "managed-1", "daemon-1", ObservedPath.SINGLE_RELAY,
            ManagedRouteSelectionReason.DIRECT_UNSTABLE, 1_700_000_060L,
            listOf(ManagedIceServer(listOf("turns:relay.example.com"), "user", "credential")), true, "eu-west",
        ).validate(spec, resolution, 1_700_000_000L)

        val invalid = runCatching {
            ManagedRoutePlan(
                "plan-invalid", "managed-1", "daemon-1", ObservedPath.DIRECT,
                ManagedRouteSelectionReason.INITIAL_BEST, 1_700_000_060L,
                listOf(ManagedIceServer(listOf("turn:relay.example.com"), "user", "credential")), false, "",
            ).validate(spec, resolution, 1_700_000_000L)
        }
        assertTrue(invalid.isFailure)
    }

    @Test
    fun relayMeshIsRejectedDuringSingleRelayPhase() {
        val result = runCatching {
            ManagedRoutePlan(
                "plan-mesh", "managed-1", "daemon-1", ObservedPath.RELAY_MESH,
                ManagedRouteSelectionReason.LOWER_SCORE, 1_700_000_060L,
                listOf(ManagedIceServer(listOf("turn:relay.example.com"), "user", "credential")), true, "eu-west",
            ).validate(spec, resolution, 1_700_000_000L)
        }
        assertTrue(result.isFailure)
    }

    @Test
    fun nonCanonicalRegionAndUnsafeIceUrlsAreRejected() {
        val plans = listOf(
            ManagedRoutePlan(
                "plan-region", "managed-1", "daemon-1", ObservedPath.SINGLE_RELAY,
                ManagedRouteSelectionReason.LOWER_LATENCY, 1_700_000_060L,
                listOf(ManagedIceServer(listOf("turn:relay.example.com"), "user", "credential")), true, " EU-West ",
            ),
            ManagedRoutePlan(
                "plan-url", "managed-1", "daemon-1", ObservedPath.DIRECT,
                ManagedRouteSelectionReason.INITIAL_BEST, 1_700_000_060L,
                listOf(ManagedIceServer(listOf("https://relay.example.com"))), false, "",
            ),
            ManagedRoutePlan(
                "plan-credential", "managed-1", "daemon-1", ObservedPath.SINGLE_RELAY,
                ManagedRouteSelectionReason.ONLY_VIABLE, 1_700_000_060L,
                listOf(ManagedIceServer(listOf("turn:relay.example.com"))), true, "eu-west",
            ),
        )
        for (plan in plans) {
            val failure = runCatching { plan.validate(spec, resolution, 1_700_000_000L) }.exceptionOrNull()
            assertTrue(failure is ManagedEndpointFailure && failure.code == "protocol")
        }
    }
}
