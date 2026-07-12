package com.termx.app.managed

import com.google.protobuf.ByteString
import com.google.protobuf.CodedOutputStream
import com.google.protobuf.Message
import com.google.protobuf.UnknownFieldSet
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test
import termx.remote.auth.v1.RemoteAuth
import java.io.ByteArrayOutputStream
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Instant
import java.util.Base64
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/** AndroidRemoteAuthTest 固定 Android 与 Go remoteauth 的签名、DTLS binding 和 pairing 安全边界。 */
class AndroidRemoteAuthTest {
    private val now = Instant.parse("2026-07-11T12:34:56.789Z")
    private val privateKey = Ed25519PrivateKeyParameters(ByteArray(32) { 0x23 }, 0)
    private val publicKey = privateKey.generatePublicKey().encoded
    private val fingerprint = AndroidRemoteAuth.deviceFingerprint(publicKey)
    private val dtlsFingerprint = "sha-256:" + List(32) { "11" }.joinToString(":")

    @Test
    fun verifiesGrantAndCompletesCanonicalCapabilityHandshake() {
        val grant = issueGrant(JSONObject()
            .put("allow_daemon", true)
            .put("file_read_metadata", true)
            .put("file_read_content", true)
            .put("file_write_content", true)
            .put("file_mutate", true))
        val claims = AndroidRemoteAuth.verifyGrant(grant, fingerprint, now)
        assertTrue(claims.scope.allowDaemon)
        assertTrue(claims.scope.fileReadMetadata)
        assertTrue(claims.scope.fileReadContent)
        assertTrue(claims.scope.fileWriteContent)
        assertTrue(claims.scope.fileMutate)
        assertEquals("device-1", claims.issuerDeviceId)

        val hello = deviceHelloFrame()
        val open = AndroidRemoteAuth.acceptDeviceHello(
            frame = hello,
            expectedDeviceId = "device-1",
            expectedDeviceFingerprint = fingerprint,
            daemonDtlsFingerprint = dtlsFingerprint,
            grant = grant,
            now = now,
            random = FixedSecureRandom(0x66),
        )
        val envelope = decodeEnvelope(open.frame)
        assertEquals("fixture-auth-session-01", open.authSessionId)
        assertEquals(grant, envelope.capabilityOpen.grant)
        assertArrayEquals(ByteArray(32) { 0x66 }, envelope.capabilityOpen.clientNonce.toByteArray())
        assertArrayEquals(
            capabilityProof(grant, envelope.authSessionId, ByteArray(32) { 0x55 }, ByteArray(32) { 0x66 }),
            envelope.capabilityOpen.proof.toByteArray(),
        )

        val accepted = RemoteAuth.AuthEnvelope.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId(open.authSessionId)
            .setCapabilityAccepted(
                RemoteAuth.CapabilityAccepted.newBuilder()
                    .setGrantId(claims.grantId)
                    .setScope(RemoteAuth.ScopeSummary.newBuilder().setKind(RemoteAuth.ScopeKind.SCOPE_KIND_DAEMON)),
            )
            .build()
        AndroidRemoteAuth.verifyCapabilityResult(encodeEnvelope(accepted), open.authSessionId, claims)
    }

    @Test
    fun rejectsDifferentDtlsCertificateAndNestedUnknownField() {
        val grant = issueGrant(JSONObject().put("allow_daemon", true))
        val mismatch = assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(
                frame = deviceHelloFrame(),
                expectedDeviceId = "device-1",
                expectedDeviceFingerprint = fingerprint,
                daemonDtlsFingerprint = "sha-256:" + List(32) { "22" }.joinToString(":"),
                grant = grant,
                now = now,
                random = FixedSecureRandom(0x66),
            )
        }
        assertEquals("device_identity_mismatch", mismatch.code)

        val unknown = UnknownFieldSet.newBuilder()
            .addField(99, UnknownFieldSet.Field.newBuilder().addVarint(1).build())
            .build()
        val nestedUnknown = assertThrows(ManagedEndpointFailure::class.java) {
            AndroidRemoteAuth.acceptDeviceHello(
                frame = deviceHelloFrame(unknown),
                expectedDeviceId = "device-1",
                expectedDeviceFingerprint = fingerprint,
                daemonDtlsFingerprint = dtlsFingerprint,
                grant = grant,
                now = now,
                random = FixedSecureRandom(0x66),
            )
        }
        assertEquals("protocol", nestedUnknown.code)
    }

    @Test
    fun rejectsCoercedJsonTypesAndOnlyWritesVerifiedDaemonPairing() {
        val invalidGrant = issueGrant(JSONObject().put("allow_daemon", "true"))
        assertEquals(
            "protocol",
            assertThrows(ManagedEndpointFailure::class.java) {
                AndroidRemoteAuth.verifyGrant(invalidGrant, fingerprint, now)
            }.code,
        )

        val writes = mutableListOf<Pair<String, String>>()
        val importer = ManagedPairingImporter { grantRef, grant -> writes += grantRef to grant }
        val daemonGrant = issueGrant(JSONObject().put("allow_daemon", true))
        assertEquals(
            "endpoint_mismatch",
            assertThrows(ManagedEndpointFailure::class.java) {
                importer.import(pairingBundle(1, daemonGrant), now, expectedEndpointId = "device-2")
            }.code,
        )
        assertEquals(0, writes.size)

        val imported = importer.import(pairingBundle(1, daemonGrant), now, expectedEndpointId = "device-1")
        assertEquals("device-1", imported.endpointId)
        assertEquals("Lab daemon", imported.label)
        assertEquals(1, writes.size)
        assertEquals(daemonGrant, writes.single().second)

        val terminalFileGrant = issueGrant(JSONObject()
            .put("terminal_id", "terminal-1")
            .put("file_read_content", true))
        assertEquals(
            "protocol",
            assertThrows(ManagedEndpointFailure::class.java) {
                AndroidRemoteAuth.verifyGrant(terminalFileGrant, fingerprint, now)
            }.code,
        )

        val terminalGrant = issueGrant(JSONObject().put("terminal_id", "terminal-1"))
        assertEquals(
            "scope_invalid",
            assertThrows(ManagedEndpointFailure::class.java) {
                importer.import(pairingBundle(1, terminalGrant), now)
            }.code,
        )
        assertEquals(1, writes.size)

        assertEquals(
            "protocol",
            assertThrows(ManagedEndpointFailure::class.java) {
                importer.import(pairingBundle("1", daemonGrant), now)
            }.code,
        )
        assertEquals(1, writes.size)
    }

    private fun issueGrant(scope: JSONObject): String {
        val claims = JSONObject()
            .put("version", 1)
            .put("grant_id", "grant-1")
            .put("issuer_device_id", "device-1")
            .put("issuer_device_fingerprint", fingerprint)
            .put("scope", scope)
            .put("issued_at", now.minusSeconds(60).toString())
            .put("not_before", now.minusSeconds(60).toString())
            .put("expires_at", now.plusSeconds(3600).toString())
            .put("revocation_id", "grant-1")
            .put("nonce", "fixture-grant-nonce")
        val payload = base64Url(claims.toString().toByteArray(StandardCharsets.UTF_8))
        val publicPart = base64Url(publicKey)
        val signingInput = "$GRANT_PREFIX.$payload.$publicPart"
        return "$signingInput.${base64Url(sign(signingInput.toByteArray(StandardCharsets.UTF_8)))}"
    }

    private fun deviceHelloFrame(unknownFields: UnknownFieldSet = UnknownFieldSet.getDefaultInstance()): ByteArray {
        val unsigned = RemoteAuth.DeviceHello.newBuilder()
            .setDeviceId("device-1")
            .setDevicePublicKey(ByteString.copyFrom(publicKey))
            .setDeviceFingerprint(fingerprint)
            .setServerNonce(ByteString.copyFrom(ByteArray(32) { 0x55 }))
            .setDaemonDtlsCertificateFingerprint(dtlsFingerprint)
            .setIssuedAtUnixNano(now.epochSecond * 1_000_000_000L + now.nano)
            .build()
        val signingInput = RemoteAuth.DeviceHelloSignatureInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId("fixture-auth-session-01")
            .setDeviceId(unsigned.deviceId)
            .setDevicePublicKey(unsigned.devicePublicKey)
            .setDeviceFingerprint(unsigned.deviceFingerprint)
            .setServerNonce(unsigned.serverNonce)
            .setDaemonDtlsCertificateFingerprint(dtlsFingerprint)
            .setIssuedAtUnixNano(unsigned.issuedAtUnixNano)
            .build()
        val hello = unsigned.toBuilder()
            .setSignature(ByteString.copyFrom(sign(deterministicBytes(signingInput))))
            .setUnknownFields(unknownFields)
            .build()
        return encodeEnvelope(
            RemoteAuth.AuthEnvelope.newBuilder()
                .setProtocol(AUTH_PROTOCOL)
                .setVersion(AUTH_VERSION)
                .setAuthSessionId("fixture-auth-session-01")
                .setDeviceHello(hello)
                .build(),
        )
    }

    private fun capabilityProof(grant: String, sessionId: String, serverNonce: ByteArray, clientNonce: ByteArray): ByteArray {
        val input = RemoteAuth.CapabilityProofInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId(sessionId)
            .setServerNonce(ByteString.copyFrom(serverNonce))
            .setClientNonce(ByteString.copyFrom(clientNonce))
            .setDaemonDtlsCertificateFingerprint(dtlsFingerprint)
            .setGrantSha256(ByteString.copyFrom(MessageDigest.getInstance("SHA-256").digest(grant.toByteArray(StandardCharsets.UTF_8))))
            .build()
        return Mac.getInstance("HmacSHA256").run {
            init(SecretKeySpec(grant.toByteArray(StandardCharsets.UTF_8), "HmacSHA256"))
            doFinal(deterministicBytes(input))
        }
    }

    private fun pairingBundle(version: Any, grant: String): String = JSONObject()
        .put("version", version)
        .put("label", "Lab daemon")
        .put("device_id", "device-1")
        .put("device_fingerprint", fingerprint)
        .put("capability_grant", grant)
        .toString()

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

    private fun sign(message: ByteArray): ByteArray = Ed25519Signer().run {
        init(true, privateKey)
        update(message, 0, message.size)
        generateSignature()
    }

    private fun base64Url(value: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(value)

    private class FixedSecureRandom(private val octet: Byte) : SecureRandom() {
        override fun nextBytes(bytes: ByteArray) = bytes.fill(octet)
    }

    private companion object {
        const val AUTH_PROTOCOL = "termx-remote-auth"
        const val AUTH_VERSION = 1
        const val GRANT_PREFIX = "termx-grant-v1"
        val AUTH_MAGIC = byteArrayOf('T'.code.toByte(), 'X'.code.toByte(), 'R'.code.toByte(), 'A'.code.toByte())
    }
}
