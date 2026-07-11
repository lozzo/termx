package com.termx.app.managed

import com.google.gson.Gson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.lang.reflect.Modifier

class ManagedEndpointContractTest {
    @Test
    fun classifiesOnlyNonRetryableAuthenticationFailures() {
        listOf(
            "login_required",
            "device_enrollment_required",
            "unauthenticated",
            "capability_invalid",
            "capability_expired",
            "device_identity_mismatch",
            "scope_invalid",
            "replayed",
        ).forEach { code ->
            assertTrue("expected $code to stop automatic reconnect", isNonRetryableManagedAuthenticationFailure(code))
        }
        assertFalse(isNonRetryableManagedAuthenticationFailure("route_unavailable"))
        assertFalse(isNonRetryableManagedAuthenticationFailure("temporary"))
    }

    private data class Fixture(
        val schema_version: Int,
        val transport: String,
        val phases: List<String>,
        val observed_paths: List<String>,
        val relay_policies: List<RelayPolicy>,
        val route_selection_reasons: List<String>,
        val route_plan_fields: List<String>,
        val forbidden_route_plan_fragments: List<String>,
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
        assertEquals(2, fixture.schema_version)
        assertEquals("hub-p2p", fixture.transport)
        assertEquals(ManagedEndpointPhase.entries.map { it.wireName }, fixture.phases)
        assertEquals(ObservedPath.entries.map { it.wireName }, fixture.observed_paths)
        assertEquals(ManagedRouteSelectionReason.entries.map { it.wireName }, fixture.route_selection_reasons)
        assertEquals(ManagedRoutePlan.WIRE_FIELDS, fixture.route_plan_fields)
        val routePlanFields = ManagedRoutePlan::class.java.declaredFields
            .filterNot { field -> field.isSynthetic || Modifier.isStatic(field.modifiers) }
            .map { field -> managedCamelCaseToSnakeCase(field.name) }
        assertEquals(fixture.route_plan_fields.sorted(), routePlanFields.sorted())
        routePlanFields.forEach { field ->
            fixture.forbidden_route_plan_fragments.forEach { fragment ->
                assertFalse("$field contains $fragment", field.contains(fragment))
            }
        }
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

private fun managedCamelCaseToSnakeCase(value: String): String = buildString {
    value.forEachIndexed { index, character ->
        if (character.isUpperCase() && index > 0) append('_')
        append(character.lowercaseChar())
    }
}
