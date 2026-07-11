package com.termx.app.managed

import com.google.gson.Gson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ManagedEndpointContractTest {
    private data class Fixture(
        val schema_version: Int,
        val transport: String,
        val phases: List<String>,
        val observed_paths: List<String>,
        val relay_policies: List<RelayPolicy>,
        val cloud_errors: List<String>,
        val authorization_cases: List<AuthorizationCase>,
    )

    private data class RelayPolicy(
        val relay_mode: String,
        val route_preference: String,
        val relay_only: Boolean,
    )

    private data class AuthorizationCase(
        val endpoint_id: String,
        val target_device_id: String,
        val device_fingerprint: String,
        val grant_ref: String,
        val relay_mode: String,
        val valid: Boolean,
    )

    @Test
    fun sharedFixtureMatchesAndroidDomain() {
        val payload = requireNotNull(javaClass.getResourceAsStream("/managed_endpoint_contract.json"))
            .bufferedReader().use { it.readText() }
        val fixture = Gson().fromJson(payload, Fixture::class.java)
        assertEquals(1, fixture.schema_version)
        assertEquals("hub-p2p", fixture.transport)
        assertEquals(ManagedEndpointPhase.entries.map { it.wireName }, fixture.phases)
        assertEquals(ObservedPath.entries.map { it.wireName }, fixture.observed_paths)
        assertEquals(
            listOf(
                "companion_missing", "companion_not_running", "companion_incompatible", "companion_untrusted",
                "login_required", "device_enrollment_required", "unauthenticated", "device_not_found",
                "entitlement_denied", "quota_exhausted", "region_unavailable", "route_unavailable",
                "backpressure", "protocol", "temporary",
            ),
            fixture.cloud_errors,
        )

        fixture.relay_policies.forEach { expected ->
            val actual = ManagedEndpointContract.dialPolicy(RelayMode.fromWire(expected.relay_mode))
            assertEquals(expected.route_preference, actual.routePreference)
            assertEquals(expected.relay_only, actual.relayOnly)
        }
        fixture.authorization_cases.forEach { expected ->
            val valid = runCatching {
                val relayMode = RelayMode.fromWire(expected.relay_mode)
                ManagedEndpointContract.validate(
                    ManagedEndpointSpec(
                        expected.endpoint_id,
                        expected.target_device_id,
                        expected.device_fingerprint,
                        expected.grant_ref,
                        relayMode,
                    ),
                )
            }.isSuccess
            if (expected.valid) assertTrue(valid) else assertFalse(valid)
        }
    }
}
