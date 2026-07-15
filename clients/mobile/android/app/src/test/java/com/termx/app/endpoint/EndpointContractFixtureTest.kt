package com.termx.app.endpoint

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test
import com.google.protobuf.ByteString
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import termx.remote.auth.v1.RemoteAuth

/** EndpointContractFixtureTest 让 Android native 与 Go/TypeScript 读取同一份 registry/assembler testdata。 */
class EndpointContractFixtureTest {
    @Test
    fun sharedFixtureCoversStrictRoundTripStoreCommutativityAndConflict() {
        val fixture = loadFixture()
        assertEquals(1, fixture.getInt("schema_version"))

        val emptyRegistry = EndpointRegistryCodec.parseRegistry(fixture.getJSONObject("empty_registry"))
        assertEquals(SavedEndpointRegistry(), emptyRegistry)
        val blank = assertThrows(EndpointContractException::class.java) { EndpointRegistryCodec.decode(ByteArray(0)) }
        assertEquals("config_invalid", blank.code)

        val validPayload = fixture.getJSONObject("valid_registry").toString().toByteArray(StandardCharsets.UTF_8)
        val registry = EndpointRegistryCodec.decode(validPayload)
        val roundTripped = EndpointRegistryCodec.decode(EndpointRegistryCodec.encode(registry))
        assertEquals(registry, roundTripped)
        val defaultedManaged = EndpointRegistryCodec.parseRegistry(fixture.getJSONObject("defaulted_managed_registry"))
        assertEquals(EndpointRelayMode.AUTO, defaultedManaged.endpoints.getValue("cloud").routes.getValue("cloud").relayMode)

        val store = AndroidSavedEndpointRegistryStore(Files.createTempDirectory("termx-endpoints").resolve("endpoints.json").toFile())
        store.save(registry)
        assertEquals(registry, store.load())
        val updatedEndpoint = registry.endpoints.getValue(registry.defaultEndpointId).copy(label = "Updated Studio")
        val updatedRegistry = registry.copy(endpoints = registry.endpoints + (registry.defaultEndpointId to updatedEndpoint))
        store.save(updatedRegistry)
        assertEquals(updatedRegistry, store.load())
        val invalidRegistry = updatedRegistry.copy(
            endpoints = updatedRegistry.endpoints + (updatedRegistry.defaultEndpointId to updatedEndpoint.copy(routes = emptyMap())),
        )
        assertThrows(EndpointContractException::class.java) { store.save(invalidRegistry) }
        assertEquals(updatedRegistry, store.load())

        val unknown = assertThrows(EndpointContractException::class.java) {
            EndpointRegistryCodec.decode(fixture.getJSONObject("unknown_field_registry").toString().toByteArray(StandardCharsets.UTF_8))
        }
        assertEquals("config_invalid", unknown.code)
        val missingField = assertThrows(EndpointContractException::class.java) {
            EndpointRegistryCodec.decode(fixture.getJSONObject("missing_field_registry").toString().toByteArray(StandardCharsets.UTF_8))
        }
        assertEquals("config_invalid", missingField.code)
        val wrongType = assertThrows(EndpointContractException::class.java) {
            EndpointRegistryCodec.decode(fixture.getJSONObject("wrong_type_registry").toString().toByteArray(StandardCharsets.UTF_8))
        }
        assertEquals("config_invalid", wrongType.code)
        val whitespaceKey = assertThrows(EndpointContractException::class.java) {
            EndpointRegistryCodec.decode(fixture.getJSONObject("whitespace_key_registry").toString().toByteArray(StandardCharsets.UTF_8))
        }
        assertEquals("config_invalid", whitespaceKey.code)
        val invalidRegistryCases = fixture.getJSONArray("invalid_registry_cases")
        for (index in 0 until invalidRegistryCases.length()) {
            val testCase = invalidRegistryCases.getJSONObject(index)
            val failure = assertThrows(EndpointContractException::class.java) {
                EndpointRegistryCodec.decode(testCase.getJSONObject("registry").toString().toByteArray(StandardCharsets.UTF_8))
            }
            assertEquals(testCase.getString("name"), testCase.getString("expected_error"), failure.code)
        }
        val oversize = assertThrows(EndpointContractException::class.java) {
            EndpointRegistryCodec.decode(ByteArray(fixture.getInt("oversize_bytes")) { 'x'.code.toByte() })
        }
        assertEquals("size_limit", oversize.code)

        val discoveryFixture = fixture.getJSONObject("local_discovery_candidate")
        val discovery = RemoteAuth.LocalDiscoveryCandidate.newBuilder()
            .setClaimedIdentity(
                RemoteAuth.EndpointDaemonIdentity.newBuilder()
                    .setDeviceId(discoveryFixture.getString("claimed_device_id"))
                    .setDeviceFingerprint(discoveryFixture.getString("claimed_device_fingerprint")),
            )
            .setAddress(discoveryFixture.getString("address"))
            .setPort(discoveryFixture.getInt("port"))
            .setProtocolVersion(discoveryFixture.getInt("protocol_version"))
            .setAnnouncementExpiresAtUnixNano(System.currentTimeMillis() * 1_000_000 + discoveryFixture.getLong("ttl_millis") * 1_000_000)
            .setAnnouncementSignature(ByteString.copyFrom(ByteArray(discoveryFixture.getInt("signature_bytes"))))
            .build()
        assertEquals(64, discovery.announcementSignature.size())
        assertFalse(fixture.getJSONObject("valid_registry").toString().contains(discovery.address))

        val assemblerFixture = fixture.getJSONObject("assembler")
        val initial = EndpointRegistryCodec.parseRegistry(assemblerFixture.getJSONObject("initial_registry"))
        val candidateArray = assemblerFixture.getJSONArray("commutative_candidates")
        val candidates = (0 until candidateArray.length()).map { EndpointRegistryCodec.parseCandidate(candidateArray.getJSONObject(it)) }
        val bindingArray = assemblerFixture.getJSONArray("confirmed_identity_bindings")
        val bindings = (0 until bindingArray.length()).map { index ->
            val binding = bindingArray.getJSONObject(index)
            val identity = binding.getJSONObject("identity")
            ConfirmedEndpointIdentityBinding(
                endpointId = binding.getString("endpoint_id"),
                identity = EndpointDaemonIdentity(identity.getString("device_id"), identity.getString("device_fingerprint")),
            )
        }
        val forward = assembleSequence(initial, candidates, bindings)
        val reverse = assembleSequence(initial, candidates.reversed(), bindings)
        assertEquals(forward, reverse)
        assertEquals(1, forward.endpoints.size)
        assertEquals(assemblerFixture.getString("expected_endpoint_id"), forward.defaultEndpointId)
        val fromEmpty = assembleSequence(emptyRegistry, candidates, emptyList())
        assertEquals(1, fromEmpty.endpoints.size)
        assertEquals(assemblerFixture.getString("expected_new_endpoint_id"), fromEmpty.defaultEndpointId)

        val endpoint = forward.endpoints.getValue(assemblerFixture.getString("expected_endpoint_id"))
        assertEquals(assemblerFixture.getString("expected_label"), endpoint.label)
        assertEquals(EndpointConnectMode.parse(assemblerFixture.getString("expected_connect_mode")), endpoint.connectMode)
        val expectedRouteIds = assemblerFixture.getJSONArray("expected_route_ids").let { values ->
            (0 until values.length()).map(values::getString)
        }
        assertEquals(expectedRouteIds, endpoint.routes.keys.sorted())
        val expectedPriorities = assemblerFixture.getJSONObject("expected_route_priorities")
        endpoint.routes.forEach { (routeId, route) -> assertEquals(expectedPriorities.getInt(routeId), route.priority) }

        val conflictCandidate = EndpointRegistryCodec.parseCandidate(assemblerFixture.getJSONObject("identity_conflict_candidate"))
        val conflict = assertThrows(EndpointContractException::class.java) {
            AndroidEndpointAssembler.assemble(forward, listOf(conflictCandidate))
        }
        assertEquals(assemblerFixture.getString("expected_conflict_error"), conflict.code)
    }

    @Test
    fun emptyRegistryPublishesFirstEndpointAndReturnsDeterministicCredentialDescriptors() {
        val identity = EndpointDaemonIdentity("device-studio", "SHA256:studio")
        val result = AndroidEndpointAssembler.assemble(
            SavedEndpointRegistry(),
            listOf(
                AndroidEndpointCandidate(
                    source = EndpointSource.SHARE,
                    identity = identity,
                    routes = listOf(
                        SavedAccessRoute(
                            "ssh", EndpointRouteKind.SSH_STDIO, enabled = true, source = EndpointSource.SHARE,
                            policySource = EndpointSource.SHARE, host = "studio",
                        ),
                    ),
                    credentialDescriptors = listOf(
                        AndroidCredentialDescriptor("ssh-password", EndpointCredentialKind.SSH_PASSWORD, exportable = true),
                        AndroidCredentialDescriptor("ssh-key", EndpointCredentialKind.SSH_PRIVATE_KEY),
                        AndroidCredentialDescriptor("ssh-key", EndpointCredentialKind.SSH_PRIVATE_KEY),
                    ),
                ),
            ),
        )
        assertEquals(result.resolvedEndpointIds.single(), result.registry.defaultEndpointId)
        assertEquals(listOf("ssh-key", "ssh-password"), result.credentialDescriptors.map(AndroidCredentialDescriptor::descriptorId))

        val conflict = assertThrows(EndpointContractException::class.java) {
            AndroidEndpointAssembler.assemble(
                SavedEndpointRegistry(),
                listOf(
                    AndroidEndpointCandidate(
                        source = EndpointSource.SHARE,
                        identity = identity,
                        routes = listOf(
                            SavedAccessRoute(
                                "ssh", EndpointRouteKind.SSH_STDIO, enabled = true, source = EndpointSource.SHARE,
                                policySource = EndpointSource.SHARE, host = "studio",
                            ),
                        ),
                        credentialDescriptors = listOf(
                            AndroidCredentialDescriptor("credential", EndpointCredentialKind.SSH_PRIVATE_KEY),
                            AndroidCredentialDescriptor("credential", EndpointCredentialKind.SSH_PASSWORD),
                        ),
                    ),
                ),
            )
        }
        assertEquals("config_invalid", conflict.code)
    }

    @Test
    fun confirmedIdentityBindingPreservesSshEndpointAndUnconfirmedSharePreservesLabel() {
        val identity = EndpointDaemonIdentity("device-studio", "SHA256:studio")
        val ssh = SavedEndpoint(
            endpointId = "studio",
            label = "Local Studio",
            daemonIdentity = EndpointDaemonIdentity(),
            routes = mapOf(
                "ssh" to SavedAccessRoute(
                    routeId = "ssh",
                    kind = EndpointRouteKind.SSH_STDIO,
                    host = "studio-host",
                    remoteSocket = "auto",
                    source = EndpointSource.MANUAL,
                    policySource = EndpointSource.MANUAL,
                ),
            ),
        )
        val initial = SavedEndpointRegistry(defaultEndpointId = "studio", endpoints = mapOf("studio" to ssh))
        val bootstrap = AndroidEndpointCandidate(
            source = EndpointSource.BOOTSTRAP,
            identity = identity,
            routes = listOf(
                SavedAccessRoute(
                    routeId = "lan",
                    kind = EndpointRouteKind.DIRECT_TLS,
                    addresses = listOf("studio.local:41120"),
                    source = EndpointSource.BOOTSTRAP,
                    policySource = EndpointSource.BOOTSTRAP,
                ),
            ),
        )
        val bound = AndroidEndpointAssembler.assemble(
            initial,
            listOf(bootstrap),
            listOf(ConfirmedEndpointIdentityBinding("studio", identity)),
        ).registry
        assertEquals(setOf("studio"), bound.endpoints.keys)
        assertEquals(identity, bound.endpoints.getValue("studio").daemonIdentity)

        val unconfirmedShare = AndroidEndpointCandidate(
            source = EndpointSource.SHARE,
            identity = identity,
            suggestedLabel = "Shared Label",
            routes = listOf(
                SavedAccessRoute(
                    routeId = "ssh",
                    kind = EndpointRouteKind.SSH_STDIO,
                    host = "shared-host",
                    remoteSocket = "auto",
                    source = EndpointSource.SHARE,
                    policySource = EndpointSource.SHARE,
                ),
            ),
        )
        val shared = AndroidEndpointAssembler.assemble(bound, listOf(unconfirmedShare)).registry.endpoints.getValue("studio")
        assertEquals("Local Studio", shared.label)
        assertEquals("shared-host", shared.routes.getValue("ssh").host)

        val missingCandidate = assertThrows(EndpointContractException::class.java) {
            AndroidEndpointAssembler.assemble(
                initial,
                emptyList(),
                listOf(ConfirmedEndpointIdentityBinding("studio", identity)),
            )
        }
        assertEquals("config_invalid", missingCandidate.code)

        val invalidPolicy = initial.copy(
            endpoints = mapOf("studio" to ssh.copy(selectionPolicy = EndpointSelectionPolicy(1, false))),
        )
        val policyFailure = assertThrows(EndpointContractException::class.java) { EndpointRegistryCodec.encode(invalidPolicy) }
        assertEquals("config_invalid", policyFailure.code)
    }

    private fun assembleSequence(
        initial: SavedEndpointRegistry,
        candidates: List<AndroidEndpointCandidate>,
        bindings: List<ConfirmedEndpointIdentityBinding>,
    ): SavedEndpointRegistry {
        var registry = initial
        candidates.forEachIndexed { index, candidate ->
            registry = AndroidEndpointAssembler.assemble(
                registry,
                listOf(candidate),
                if (index == 0) bindings else emptyList(),
            ).registry
        }
        return registry
    }

    private fun loadFixture(): JSONObject {
        val payload = checkNotNull(javaClass.classLoader?.getResourceAsStream("endpoint-contract-v1.json")) {
            "shared endpoint fixture is missing"
        }.use { it.readBytes() }
        return JSONObject(payload.toString(StandardCharsets.UTF_8))
    }
}
