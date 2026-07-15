package com.termx.app.managed

import com.google.protobuf.ByteString
import com.google.protobuf.CodedOutputStream
import com.google.protobuf.Message
import com.google.protobuf.UnknownFieldSet
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import termx.remote.auth.v1.RemoteAuth
import java.io.ByteArrayOutputStream
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Instant
import java.util.Base64

/** AndroidRemoteAuthTest 固定 Android 与 Go remoteauth v2 的 DeviceIdentity、DTLS binding、client-key proof 和 ticket 边界。 */
class AndroidRemoteAuthTest {
    private val now = Instant.parse("2026-07-11T12:34:56.789Z")
    private val daemonPrivateKey = Ed25519PrivateKeyParameters(ByteArray(32) { 0x23 }, 0)
    private val daemonPublicKey = daemonPrivateKey.generatePublicKey().encoded
    private val daemonFingerprint = AndroidRemoteAuth.deviceFingerprint(daemonPublicKey)
    private val clientPrivateKey = Ed25519PrivateKeyParameters(ByteArray(32) { 0x34 }, 0)
    private val clientPublicKey = clientPrivateKey.generatePublicKey().encoded
    private val clientFingerprint = AndroidRemoteAuth.deviceFingerprint(clientPublicKey)
    private val dtlsFingerprint = "sha-256:" + List(32) { "11" }.joinToString(":")
    private val credential = AndroidClientAccessCredential(
        AndroidClientAccessIdentity("endpoint-1", clientPrivateKey.encoded, clientPublicKey, clientFingerprint),
        issueGrant(JSONObject().put("allow_daemon", true).put("manage_client_access", true)),
    )

    @Test
    fun verifiesV2GrantAndSignsCanonicalCapabilityOpenWithClientKey() {
        val claims = AndroidRemoteAuth.verifyGrant(credential.capabilityGrant, daemonFingerprint, now)
        assertEquals(clientFingerprint, claims.subjectKeyFingerprint)
        assertTrue(claims.scope.allowDaemon)
        assertTrue(claims.scope.manageClientAccess)

        val open = AndroidRemoteAuth.acceptDeviceHello(
            frame = deviceHelloFrame(), expectedDeviceId = "device-1", expectedDeviceFingerprint = daemonFingerprint,
            daemonDtlsFingerprint = dtlsFingerprint, credential = credential, now = now, random = FixedSecureRandom(0x66),
        )
        val envelope = decodeEnvelope(open.frame)
        assertEquals("fixture-auth-session-01", open.authSessionId)
        assertEquals(credential.capabilityGrant, envelope.capabilityOpen.grant)
        assertArrayEquals(clientPublicKey, envelope.capabilityOpen.clientPublicKey.toByteArray())
        assertArrayEquals(ByteArray(32) { 0x66 }, envelope.capabilityOpen.clientNonce.toByteArray())
        val proofInput = RemoteAuth.ClientProofInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId(open.authSessionId)
            .setServerNonce(ByteString.copyFrom(ByteArray(32) { 0x55 }))
            .setClientNonce(ByteString.copyFrom(ByteArray(32) { 0x66 }))
            .setChannelBinding(dtlsBinding())
            .setCredentialSha256(ByteString.copyFrom(MessageDigest.getInstance("SHA-256").digest(credential.capabilityGrant.toByteArray(StandardCharsets.UTF_8))))
            .setClientPublicKey(ByteString.copyFrom(clientPublicKey))
            .setOpenKind(RemoteAuth.AuthOpenKind.AUTH_OPEN_KIND_CAPABILITY)
            .build()
        assertTrue(verify(clientPublicKey, deterministicBytes(proofInput), envelope.capabilityOpen.proof.toByteArray()))

        val accepted = RemoteAuth.AuthEnvelope.newBuilder()
            .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId(open.authSessionId)
            .setCapabilityAccepted(
                RemoteAuth.CapabilityAccepted.newBuilder()
                    .setGrantId(claims.grantId).setSubjectKeyFingerprint(clientFingerprint)
                    .setScope(RemoteAuth.ScopeSummary.newBuilder().setKind(RemoteAuth.ScopeKind.SCOPE_KIND_DAEMON).setManageClientAccess(true)),
            ).build()
        AndroidRemoteAuth.verifyCapabilityResult(encodeEnvelope(accepted), open.authSessionId, claims)
    }

    @Test
    fun rejectsV1GrantWrongClientKeyDifferentDtlsAndUnknownField() {
        assertEquals("capability_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyGrant("termx-grant-v1.legacy.key.signature", daemonFingerprint, now)
        }.code)

        val wrongPrivate = Ed25519PrivateKeyParameters(ByteArray(32) { 0x44 }, 0)
        val wrongPublic = wrongPrivate.generatePublicKey().encoded
        val copied = credential.copy(identity = AndroidClientAccessIdentity("endpoint-1", wrongPrivate.encoded, wrongPublic, AndroidRemoteAuth.deviceFingerprint(wrongPublic)))
        assertEquals("subject_key_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(deviceHelloFrame(), "device-1", daemonFingerprint, dtlsFingerprint, copied, now, FixedSecureRandom(0x66))
        }.code)

        assertEquals("device_identity_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(
                deviceHelloFrame(), "device-1", daemonFingerprint,
                "sha-256:" + List(32) { "22" }.joinToString(":"), credential, now, FixedSecureRandom(0x66),
            )
        }.code)

        val unknown = UnknownFieldSet.newBuilder().addField(99, UnknownFieldSet.Field.newBuilder().addVarint(1).build()).build()
        assertEquals("protocol", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(deviceHelloFrame(unknown), "device-1", daemonFingerprint, dtlsFingerprint, credential, now, FixedSecureRandom(0x66))
        }.code)

        assertEquals("protocol", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(duplicatePayloadFrame(), "device-1", daemonFingerprint, dtlsFingerprint, credential, now, FixedSecureRandom(0x66))
        }.code)
    }

    @Test
    fun pairingImporterPreparesClientIdentityButDoesNotClaimAuthorization() {
        val writes = mutableListOf<Pair<String, String>>()
        val importer = ManagedPairingImporter { ref, endpointId ->
            writes += ref to endpointId
            clientFingerprint
        }
        val payload = pairingBundle()
        val portablePayload = "termx://bootstrap?payload=${base64Url(payload)}"
        val imported = importer.import(portablePayload, now, expectedEndpointId = "lab-endpoint")
        assertEquals("lab-endpoint", imported.endpointId)
        assertEquals("Lab daemon", imported.label)
        assertEquals(clientFingerprint, imported.clientKeyFingerprint)
        assertTrue(imported.authorizationRequired)
        assertEquals(1, writes.size)
        assertEquals("lab-endpoint", writes.single().second)
        assertFalse(portablePayload.contains("capability_grant"))

        assertEquals("protocol", assertThrows(ManagedEndpointFailure::class.java) {
            importer.import(portablePayload, now, expectedEndpointId = "bad endpoint")
        }.code)
        assertEquals("pairing_ticket_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            importer.import("termx://bootstrap?payload=${base64Url(payload + byteArrayOf(0x01))}", now)
        }.code)
        assertEquals("pairing_ticket_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            importer.import("{\"version\":2}", now)
        }.code)
        assertEquals(1, writes.size)
    }

    @Test
    fun rejectsCoercedJsonScopeTypes() {
        val invalid = issueGrant(JSONObject().put("allow_daemon", "true"))
        assertEquals("capability_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyGrant(invalid, daemonFingerprint, now)
        }.code)
    }

    @Test
    fun mapsPermanentGrantAndPairingFailuresAtExactTimeBoundaries() {
        assertEquals("device_identity_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyGrant(credential.capabilityGrant, "ed25519-sha256:wrong", now)
        }.code)
        val grantParts = credential.capabilityGrant.split('.').toMutableList()
        grantParts[3] = base64Url(ByteArray(64) { 0x7f })
        assertEquals("capability_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyGrant(grantParts.joinToString("."), daemonFingerprint, now)
        }.code)

        val wrongIssuerClaims = AndroidRemoteAuth.verifyGrant(
            issueGrant(JSONObject().put("allow_daemon", true), issuerDeviceId = "device-other"), daemonFingerprint, now,
        )
        assertEquals("device_identity_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyEndpointBinding(wrongIssuerClaims, "device-1", clientFingerprint)
        }.code)
        assertEquals("subject_key_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyEndpointBinding(wrongIssuerClaims.copy(issuerDeviceId = "device-1"), "device-1", "wrong-subject")
        }.code)

        val payload = pairingBundle()
        val verified = AndroidRemoteAuth.verifyPairingBundle(payload, now, daemonFingerprint)
        assertEquals("ticket-1", verified.ticket.ticketId)
        assertEquals("pairing_ticket_expired", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(payload, now.plusSeconds(600), daemonFingerprint)
        }.code)
        assertEquals("pairing_ticket_expired", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(payload, now.minusSeconds(61), daemonFingerprint)
        }.code)
        assertEquals("device_identity_mismatch", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(payload, now, "ed25519-sha256:wrong")
        }.code)
        val bundle = RemoteAuth.EndpointBootstrapBundleV2.parseFrom(payload)
        val tampered = bundle.toBuilder().setBundleSignature(ByteString.copyFrom(ByteArray(64) { 0x6a })).build()
        assertEquals("pairing_ticket_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(deterministicBytes(tampered), now, daemonFingerprint)
        }.code)
    }

    @Test
    fun consumesGoGeneratedCanonicalBootstrapFixture() {
        val fixture = JSONObject(requireNotNull(javaClass.getResourceAsStream("/pairing_bootstrap_v2.json"))
            .bufferedReader().use { it.readText() })
        val fixtureNow = Instant.parse(fixture.getString("now"))
        val payload = Base64.getUrlDecoder().decode(fixture.getString("payload_base64url"))
        val verified = AndroidRemoteAuth.verifyPairingBundle(payload, fixtureNow, fixture.getString("device_fingerprint"))
        assertEquals(fixture.getString("device_id"), verified.bundle.identity.deviceId)
        assertEquals(fixture.getString("ticket_id"), verified.ticket.ticketId)
        assertEquals(fixture.getString("label"), verified.bundle.suggestedLabel)

        val imported = ManagedPairingImporter { _, _ -> clientFingerprint }.importBytes(payload, fixtureNow, "fixture-endpoint")
        assertEquals("fixture-endpoint", imported.endpointId)
        assertEquals(fixture.getString("device_fingerprint"), imported.deviceFingerprint)
    }

    @Test
    fun rejectsSignedNonCanonicalPairingScopeAndRoute() {
        val invalidUtf8Scope = "base:terminal:${base64Url(byteArrayOf(0xff.toByte()))}"
        assertEquals("scope_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(pairingBundle(listOf(invalidUtf8Scope)), now, daemonFingerprint)
        }.code)

        val nonCanonicalRoute = RemoteAuth.EndpointAccessRoute.newBuilder()
            .setRouteId("ssh").setKind(RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_SSH_STDIO)
            .setHost(" studio").build()
        assertEquals("pairing_ticket_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(pairingBundle(routes = listOf(nonCanonicalRoute)), now, daemonFingerprint)
        }.code)

        val crossKindRelay = nonCanonicalRoute.toBuilder().setHost("studio")
            .setRelayMode(RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_RELAY_ONLY).build()
        assertEquals("pairing_ticket_invalid", assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.verifyPairingBundle(pairingBundle(routes = listOf(crossKindRelay)), now, daemonFingerprint)
        }.code)
    }

    @Test
    fun scopeExpansionUsesSameExplicitContainmentAsDesktopStore() {
        val narrow = AndroidRemoteAuthScope(true, "", false, false, false, false, false, false)
        val expanded = narrow.copy(fileReadMetadata = true)
        val terminal = AndroidRemoteAuthScope(false, "term-1", false, false, false, false, false, false)
        assertFalse(AndroidRemoteAuth.scopeContains(narrow, expanded))
        assertTrue(AndroidRemoteAuth.scopeContains(expanded, narrow))
        assertTrue(AndroidRemoteAuth.scopeContains(narrow, terminal))
        assertFalse(AndroidRemoteAuth.scopeContains(terminal, narrow))
    }

    private fun issueGrant(scope: JSONObject, issuerDeviceId: String = "device-1"): String {
        val claims = JSONObject()
            .put("version", 2).put("grant_id", "grant-1").put("issuer_device_id", issuerDeviceId)
            .put("issuer_device_fingerprint", daemonFingerprint).put("subject_key_fingerprint", clientFingerprint)
            .put("scope", scope).put("issued_at", now.minusSeconds(60).toString()).put("not_before", now.minusSeconds(60).toString())
            .put("expires_at", now.plusSeconds(3600).toString()).put("revocation_id", "grant-1").put("nonce", "fixture-grant-nonce")
        return signedToken(GRANT_PREFIX, claims, daemonPrivateKey)
    }

    private fun signedToken(prefix: String, claims: JSONObject, privateKey: Ed25519PrivateKeyParameters): String {
        val payload = base64Url(claims.toString().toByteArray(StandardCharsets.UTF_8))
        val publicPart = base64Url(privateKey.generatePublicKey().encoded)
        val input = "$prefix.$payload.$publicPart"
        return "$input.${base64Url(sign(privateKey, input.toByteArray(StandardCharsets.UTF_8)))}"
    }

    private fun pairingBundle(
        scopeCeiling: List<String> = listOf("base:daemon"),
        routes: List<RemoteAuth.EndpointAccessRoute> = emptyList(),
    ): ByteArray {
        val issuedAt = now.minusSeconds(60)
        val expiresAt = now.plusSeconds(600)
        val identity = RemoteAuth.EndpointDaemonIdentity.newBuilder()
            .setDeviceId("device-1").setDevicePublicKey(ByteString.copyFrom(daemonPublicKey))
            .setDeviceFingerprint(daemonFingerprint).build()
        val unsignedTicket = RemoteAuth.PairingTicketDescriptor.newBuilder()
            .setTicketId("ticket-1").addAllScopeCeiling(scopeCeiling)
            .setIssuedAtUnixNano(unixNanos(issuedAt)).setExpiresAtUnixNano(unixNanos(expiresAt))
            .setNonce(ByteString.copyFrom(ByteArray(18) { 0x45 })).setMaxRedemptions(1)
            .setGrantLifetimeSeconds(86_400).build()
        val ticketInput = RemoteAuth.PairingTicketSignatureInput.newBuilder()
            .setProtocol(PAIRING_TICKET_SIGNATURE_PROTOCOL).setVersion(PORTABLE_SIGNATURE_VERSION)
            .setIssuerDeviceId(identity.deviceId).setIssuerDeviceFingerprint(identity.deviceFingerprint)
            .setTicket(unsignedTicket).build()
        val ticket = unsignedTicket.toBuilder()
            .setSignature(ByteString.copyFrom(sign(daemonPrivateKey, deterministicBytes(ticketInput)))).build()
        val unsignedBundle = RemoteAuth.EndpointBootstrapBundleV2.newBuilder()
            .setSchemaVersion(2).setBundleId("bundle-1").setIdentity(identity).setSuggestedLabel("Lab daemon")
            .addAllRoutes(routes)
            .setAuthorization(RemoteAuth.EndpointAuthorizationBootstrap.newBuilder().setPairingTicket(ticket))
            .setIssuedAtUnixNano(unixNanos(issuedAt)).setExpiresAtUnixNano(unixNanos(expiresAt)).build()
        val bundleInput = RemoteAuth.EndpointBootstrapSignatureInput.newBuilder()
            .setProtocol(ENDPOINT_BOOTSTRAP_SIGNATURE_PROTOCOL).setVersion(PORTABLE_SIGNATURE_VERSION)
            .setBundle(unsignedBundle).build()
        return deterministicBytes(unsignedBundle.toBuilder()
            .setBundleSignature(ByteString.copyFrom(sign(daemonPrivateKey, deterministicBytes(bundleInput)))).build())
    }

    private fun unixNanos(value: Instant): Long = value.epochSecond * 1_000_000_000L + value.nano

    private fun deviceHelloFrame(unknownFields: UnknownFieldSet = UnknownFieldSet.getDefaultInstance()): ByteArray {
        val unsigned = RemoteAuth.DeviceHello.newBuilder()
            .setDeviceId("device-1").setDevicePublicKey(ByteString.copyFrom(daemonPublicKey)).setDeviceFingerprint(daemonFingerprint)
            .setServerNonce(ByteString.copyFrom(ByteArray(32) { 0x55 })).setChannelBinding(dtlsBinding())
            .setIssuedAtUnixNano(now.epochSecond * 1_000_000_000L + now.nano).build()
        val signingInput = RemoteAuth.DeviceHelloSignatureInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId("fixture-auth-session-01")
            .setDeviceId(unsigned.deviceId).setDevicePublicKey(unsigned.devicePublicKey).setDeviceFingerprint(unsigned.deviceFingerprint)
            .setServerNonce(unsigned.serverNonce).setChannelBinding(unsigned.channelBinding).setIssuedAtUnixNano(unsigned.issuedAtUnixNano).build()
        val hello = unsigned.toBuilder().setSignature(ByteString.copyFrom(sign(daemonPrivateKey, deterministicBytes(signingInput))))
            .setUnknownFields(unknownFields).build()
        return encodeEnvelope(
            RemoteAuth.AuthEnvelope.newBuilder().setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION)
                .setAuthSessionId("fixture-auth-session-01").setDeviceHello(hello).build(),
        )
    }

    private fun duplicatePayloadFrame(): ByteArray {
        val payload = deviceHelloFrame().copyOfRange(AUTH_MAGIC.size, deviceHelloFrame().size)
        val output = ByteArrayOutputStream()
        output.write(payload)
        val coded = CodedOutputStream.newInstance(output)
        coded.writeByteArray(9, ByteArray(0))
        coded.flush()
        return AUTH_MAGIC + output.toByteArray()
    }

    private fun dtlsBinding(): RemoteAuth.ChannelBinding = RemoteAuth.ChannelBinding.newBuilder()
        .setKind(RemoteAuth.ChannelBindingKind.CHANNEL_BINDING_KIND_DTLS)
        .setBindingHash(ByteString.copyFrom(ByteArray(32) { 0x11 })).build()

    private fun encodeEnvelope(envelope: RemoteAuth.AuthEnvelope): ByteArray = AUTH_MAGIC + deterministicBytes(envelope)
    private fun decodeEnvelope(frame: ByteArray): RemoteAuth.AuthEnvelope = RemoteAuth.AuthEnvelope.parseFrom(frame.copyOfRange(AUTH_MAGIC.size, frame.size))

    private fun deterministicBytes(message: Message): ByteArray {
        val output = ByteArrayOutputStream(message.serializedSize)
        val coded = CodedOutputStream.newInstance(output)
        coded.useDeterministicSerialization()
        message.writeTo(coded)
        coded.flush()
        return output.toByteArray()
    }

    private fun sign(privateKey: Ed25519PrivateKeyParameters, message: ByteArray): ByteArray = Ed25519Signer().run {
        init(true, privateKey)
        update(message, 0, message.size)
        generateSignature()
    }

    private fun verify(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean = Ed25519Signer().run {
        init(false, Ed25519PublicKeyParameters(publicKey, 0))
        update(message, 0, message.size)
        verifySignature(signature)
    }

    private fun base64Url(value: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(value)

    private class FixedSecureRandom(private val octet: Byte) : SecureRandom() {
        override fun nextBytes(bytes: ByteArray) = bytes.fill(octet)
    }

    private companion object {
        const val AUTH_PROTOCOL = "termx-remote-auth"
        const val AUTH_VERSION = 2
        const val GRANT_PREFIX = "termx-grant-v2"
        const val PAIRING_TICKET_SIGNATURE_PROTOCOL = "termx.pairing-ticket.signature"
        const val ENDPOINT_BOOTSTRAP_SIGNATURE_PROTOCOL = "termx.endpoint-bootstrap.signature"
        const val PORTABLE_SIGNATURE_VERSION = 1
        val AUTH_MAGIC = byteArrayOf('T'.code.toByte(), 'X'.code.toByte(), 'R'.code.toByte(), 'A'.code.toByte())
    }
}
