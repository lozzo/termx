package com.termx.app.managed

import com.google.protobuf.CodedOutputStream
import com.google.protobuf.Descriptors
import com.google.protobuf.Message
import com.termx.app.transport.WebRTCTransport
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.bouncycastle.util.encoders.Base64
import org.json.JSONObject
import termx.remote.auth.v1.RemoteAuth
import java.io.ByteArrayOutputStream
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Instant
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * AndroidManagedEndpointAuthorizer 是公开 Android App 的 DataChannel 端到端授权实现。
 * endpoint pin 与 grant 来自本地安全存储，DTLS fingerprint 来自实际 PeerConnection stats；任何校验失败都只关闭当前 managed transport。
 */
class AndroidManagedEndpointAuthorizer(
    private val now: () -> Instant = { Instant.now() },
    private val random: SecureRandom = SecureRandom(),
) : ManagedEndpointAuthorizer {
    /** authorize 在同一 `protocol` DataChannel 完成 DeviceHello/CapabilityOpen，并在成功后切换到 termx wire v3。 */
    override suspend fun authorize(transport: WebRTCTransport, spec: ManagedEndpointSpec, grant: String) = withContext(Dispatchers.IO) {
        val claims = AndroidRemoteAuth.verifyGrant(grant, spec.deviceFingerprint, now())
        if (claims.issuerDeviceId != spec.targetDeviceId) {
            throw ManagedEndpointFailure("device_identity_mismatch", "capability issuer does not match managed endpoint")
        }
        val dtlsFingerprint = transport.remoteCertificateFingerprint(AUTH_TIMEOUT_MS)
            ?: throw ManagedEndpointFailure("device_identity_mismatch", "remote DTLS certificate is unavailable")
        val helloFrame = transport.receiveAuthorizationMessage(AUTH_TIMEOUT_MS)
            ?: throw ManagedEndpointFailure("protocol", "daemon did not send DeviceHello")
        val open = AndroidRemoteAuth.acceptDeviceHello(
            frame = helloFrame,
            expectedDeviceId = spec.targetDeviceId,
            expectedDeviceFingerprint = spec.deviceFingerprint,
            daemonDtlsFingerprint = dtlsFingerprint,
            grant = grant,
            now = now(),
            random = random,
        )
        if (!transport.sendAuthorizationMessage(open.frame)) {
            throw ManagedEndpointFailure("protocol", "CapabilityOpen could not be sent")
        }
        val resultFrame = transport.receiveAuthorizationMessage(AUTH_TIMEOUT_MS)
            ?: throw ManagedEndpointFailure("protocol", "daemon did not finish capability authorization")
        AndroidRemoteAuth.verifyCapabilityResult(resultFrame, open.authSessionId, claims)
        if (!transport.activateTermxProtocol(AUTH_TIMEOUT_MS)) {
            throw ManagedEndpointFailure("protocol", "termx protocol Hello failed")
        }
    }

    private companion object {
        const val AUTH_TIMEOUT_MS = 8_000L
    }
}

/** AndroidRemoteAuthClaims 是 App 授权状态机使用的已验签 grant 摘要，不包含 grant body。 */
data class AndroidRemoteAuthClaims(
    val grantId: String,
    val issuerDeviceId: String,
    val issuerDeviceFingerprint: String,
    val scope: AndroidRemoteAuthScope,
    val notBefore: Instant,
    val expiresAt: Instant,
)

/** AndroidRemoteAuthScope 是 daemon/terminal/machine-events 三种互斥 capability scope。 */
data class AndroidRemoteAuthScope(
    val allowDaemon: Boolean,
    val terminalId: String,
    val machineEventsOnly: Boolean,
)

/** AndroidCapabilityOpen 是完成 DeviceHello 校验后待发送的 auth frame 与 session binding。 */
data class AndroidCapabilityOpen(val authSessionId: String, val frame: ByteArray)

/**
 * AndroidRemoteAuth 是 Go `shared/remoteauth` 的 Android wire 对等实现。
 * 它只处理公开 grant 与 DataChannel auth；Cloud adapter、Hub 和 Relay 不得调用或接收这些值。
 */
object AndroidRemoteAuth {
    private const val AUTH_PROTOCOL = "termx-remote-auth"
    private const val AUTH_VERSION = 1
    private const val GRANT_PREFIX = "termx-grant-v1"
    private const val MAX_AUTH_FRAME_SIZE = 64 * 1024
    private const val CLOCK_SKEW_SECONDS = 120L
    private val AUTH_MAGIC = byteArrayOf('T'.code.toByte(), 'X'.code.toByte(), 'R'.code.toByte(), 'A'.code.toByte())
    private val SESSION_ID = Regex("[A-Za-z0-9_-]{16,128}")
    private val DTLS_FINGERPRINT = Regex("sha-256:(?:[0-9a-f]{2}:){31}[0-9a-f]{2}")

    /** verifyGrant 严格验证 envelope、Ed25519 签名、endpoint pin、有效期和互斥 scope。 */
    fun verifyGrant(grant: String, expectedFingerprint: String, now: Instant): AndroidRemoteAuthClaims {
        val normalizedGrant = grant.trim()
        val parts = normalizedGrant.split('.')
        if (parts.size != 4 || parts[0] != GRANT_PREFIX) fail("capability grant envelope is invalid")
        val publicKey = decodeBase64Url(parts[2])
        if (publicKey.size != 32) fail("capability grant public key is invalid")
        val fingerprint = deviceFingerprint(publicKey)
        if (!constantTimeEquals(fingerprint, expectedFingerprint.trim())) fail("capability grant fingerprint does not match endpoint pin")
        val signature = decodeBase64Url(parts[3])
        if (signature.size != 64 || !verifyEd25519(publicKey, parts.take(3).joinToString(".").toByteArray(StandardCharsets.UTF_8), signature)) {
            fail("capability grant signature is invalid")
        }
        val claims = JSONObject(String(decodeBase64Url(parts[1]), StandardCharsets.UTF_8))
        requireKeys(claims, setOf(
            "version", "grant_id", "issuer_device_id", "issuer_device_fingerprint", "scope",
            "issued_at", "not_before", "expires_at", "revocation_id", "nonce",
        ), "capability grant")
        val scopeObject = claims.requiredObject("scope")
        requireKeys(scopeObject, setOf("allow_daemon", "terminal_id", "machine_events_only"), "capability scope")
        val scope = AndroidRemoteAuthScope(
            allowDaemon = scopeObject.optionalBoolean("allow_daemon"),
            terminalId = scopeObject.optionalString("terminal_id"),
            machineEventsOnly = scopeObject.optionalBoolean("machine_events_only"),
        )
        val scopeCount = listOf(scope.allowDaemon, scope.terminalId.isNotEmpty(), scope.machineEventsOnly).count { it }
        val issuedAt = claims.requiredInstant("issued_at")
        val notBefore = claims.requiredInstant("not_before")
        val expiresAt = claims.requiredInstant("expires_at")
        val grantId = claims.requiredString("grant_id")
        val issuerDeviceId = claims.requiredString("issuer_device_id")
        val issuerFingerprint = claims.requiredString("issuer_device_fingerprint")
        if (claims.requiredInteger("version") != 1 || grantId.isEmpty() || issuerDeviceId.isEmpty() ||
            !constantTimeEquals(issuerFingerprint, fingerprint) || claims.requiredString("revocation_id").isEmpty() ||
            claims.requiredString("nonce").isEmpty() || scopeCount != 1 || notBefore.isBefore(issuedAt) ||
            !expiresAt.isAfter(notBefore)) {
            fail("capability grant claims are invalid")
        }
        if (now.isBefore(notBefore)) fail("capability grant is not active", "capability_invalid")
        if (!now.isBefore(expiresAt)) fail("capability grant has expired", "capability_expired")
        return AndroidRemoteAuthClaims(grantId, issuerDeviceId, issuerFingerprint, scope, notBefore, expiresAt)
    }

    /** acceptDeviceHello 验证 daemon identity 与实际 DTLS binding，并生成一次性 CapabilityOpen。 */
    fun acceptDeviceHello(
        frame: ByteArray,
        expectedDeviceId: String,
        expectedDeviceFingerprint: String,
        daemonDtlsFingerprint: String,
        grant: String,
        now: Instant,
        random: SecureRandom,
    ): AndroidCapabilityOpen {
        val envelope = parseEnvelope(frame)
        val hello = if (envelope.payloadCase == RemoteAuth.AuthEnvelope.PayloadCase.DEVICE_HELLO) envelope.deviceHello else fail("daemon did not start with DeviceHello")
        if (!SESSION_ID.matches(envelope.authSessionId) || hello.devicePublicKey.size() != 32 || hello.serverNonce.size() != 32 || hello.signature.size() != 64) {
            fail("DeviceHello is malformed", "device_identity_mismatch")
        }
        val issuedAt = Instant.ofEpochSecond(0, hello.issuedAtUnixNano)
        if (issuedAt.isBefore(now.minusSeconds(CLOCK_SKEW_SECONDS)) || issuedAt.isAfter(now.plusSeconds(CLOCK_SKEW_SECONDS))) {
            fail("DeviceHello is outside the accepted time window", "device_identity_mismatch")
        }
        val publicKey = hello.devicePublicKey.toByteArray()
        val fingerprint = deviceFingerprint(publicKey)
        val actualDtls = normalizeDtlsFingerprint(daemonDtlsFingerprint)
        if (!constantTimeEquals(fingerprint, hello.deviceFingerprint) ||
            !constantTimeEquals(fingerprint, expectedDeviceFingerprint.trim()) ||
            hello.deviceId.trim() != expectedDeviceId.trim() ||
            !constantTimeEquals(normalizeDtlsFingerprint(hello.daemonDtlsCertificateFingerprint), actualDtls)) {
            fail("DeviceHello identity binding does not match endpoint", "device_identity_mismatch")
        }
        val signingInput = RemoteAuth.DeviceHelloSignatureInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId(envelope.authSessionId)
            .setDeviceId(hello.deviceId.trim())
            .setDevicePublicKey(hello.devicePublicKey)
            .setDeviceFingerprint(hello.deviceFingerprint.trim())
            .setServerNonce(hello.serverNonce)
            .setDaemonDtlsCertificateFingerprint(actualDtls)
            .setIssuedAtUnixNano(hello.issuedAtUnixNano)
            .build()
        if (!verifyEd25519(publicKey, deterministicBytes(signingInput), hello.signature.toByteArray())) {
            fail("DeviceHello signature is invalid", "device_identity_mismatch")
        }
        val clientNonce = ByteArray(32).also(random::nextBytes)
        val normalizedGrant = grant.trim()
        val proofInput = RemoteAuth.CapabilityProofInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId(envelope.authSessionId)
            .setServerNonce(hello.serverNonce)
            .setClientNonce(com.google.protobuf.ByteString.copyFrom(clientNonce))
            .setDaemonDtlsCertificateFingerprint(actualDtls)
            .setGrantSha256(com.google.protobuf.ByteString.copyFrom(MessageDigest.getInstance("SHA-256").digest(normalizedGrant.toByteArray(StandardCharsets.UTF_8))))
            .build()
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(normalizedGrant.toByteArray(StandardCharsets.UTF_8), "HmacSHA256"))
        val capabilityOpen = RemoteAuth.CapabilityOpen.newBuilder()
            .setGrant(normalizedGrant)
            .setClientNonce(com.google.protobuf.ByteString.copyFrom(clientNonce))
            .setProof(com.google.protobuf.ByteString.copyFrom(mac.doFinal(deterministicBytes(proofInput))))
            .build()
        val openEnvelope = RemoteAuth.AuthEnvelope.newBuilder()
            .setProtocol(AUTH_PROTOCOL)
            .setVersion(AUTH_VERSION)
            .setAuthSessionId(envelope.authSessionId)
            .setCapabilityOpen(capabilityOpen)
            .build()
        return AndroidCapabilityOpen(envelope.authSessionId, marshalEnvelope(openEnvelope))
    }

    /** verifyCapabilityResult 确认 daemon 接受的是同一 grant 与同一 scope，随后才允许进入 terminal protocol。 */
    fun verifyCapabilityResult(frame: ByteArray, authSessionId: String, claims: AndroidRemoteAuthClaims) {
        val envelope = parseEnvelope(frame)
        if (!constantTimeEquals(envelope.authSessionId, authSessionId)) fail("daemon changed auth session", "replayed")
        if (envelope.payloadCase == RemoteAuth.AuthEnvelope.PayloadCase.CAPABILITY_REJECTED) {
            fail(envelope.capabilityRejected.message.ifBlank { "daemon rejected capability" }, "capability_invalid")
        }
        if (envelope.payloadCase != RemoteAuth.AuthEnvelope.PayloadCase.CAPABILITY_ACCEPTED) fail("daemon did not accept capability")
        val accepted = envelope.capabilityAccepted
        val expectedKind = when {
            claims.scope.allowDaemon -> RemoteAuth.ScopeKind.SCOPE_KIND_DAEMON
            claims.scope.terminalId.isNotEmpty() -> RemoteAuth.ScopeKind.SCOPE_KIND_TERMINAL
            claims.scope.machineEventsOnly -> RemoteAuth.ScopeKind.SCOPE_KIND_MACHINE_EVENTS
            else -> RemoteAuth.ScopeKind.SCOPE_KIND_UNSPECIFIED
        }
        if (accepted.grantId != claims.grantId || accepted.scope.kind != expectedKind ||
            accepted.scope.terminalId != claims.scope.terminalId) {
            fail("daemon accepted a different capability scope", "scope_invalid")
        }
    }

    /** deviceFingerprint 计算与 Go remoteauth.Fingerprint 相同的 Ed25519 trust anchor。 */
    fun deviceFingerprint(publicKey: ByteArray): String {
        if (publicKey.size != 32) fail("Ed25519 public key is invalid")
        return "ed25519-sha256:" + encodeBase64Url(MessageDigest.getInstance("SHA-256").digest(publicKey))
    }

    private fun parseEnvelope(frame: ByteArray): RemoteAuth.AuthEnvelope {
        if (frame.size <= AUTH_MAGIC.size || frame.size > MAX_AUTH_FRAME_SIZE || !MessageDigest.isEqual(frame.copyOfRange(0, AUTH_MAGIC.size), AUTH_MAGIC)) {
            fail("remote auth frame is invalid")
        }
        val envelope = try {
            RemoteAuth.AuthEnvelope.parseFrom(frame.copyOfRange(AUTH_MAGIC.size, frame.size))
        } catch (_: Exception) {
            fail("remote auth protobuf is invalid")
        }
        if (envelope.protocol != AUTH_PROTOCOL || envelope.version != AUTH_VERSION || !SESSION_ID.matches(envelope.authSessionId) ||
            envelope.payloadCase == RemoteAuth.AuthEnvelope.PayloadCase.PAYLOAD_NOT_SET || messageHasUnknown(envelope)) {
            fail("remote auth envelope is unsupported")
        }
        return envelope
    }

    private fun marshalEnvelope(envelope: RemoteAuth.AuthEnvelope): ByteArray {
        val payload = deterministicBytes(envelope)
        if (payload.size > MAX_AUTH_FRAME_SIZE - AUTH_MAGIC.size) fail("remote auth frame is too large")
        return AUTH_MAGIC + payload
    }

    private fun deterministicBytes(message: Message): ByteArray {
        val output = ByteArrayOutputStream(message.serializedSize)
        val coded = CodedOutputStream.newInstance(output)
        coded.useDeterministicSerialization()
        message.writeTo(coded)
        coded.flush()
        return output.toByteArray()
    }

    private fun normalizeDtlsFingerprint(value: String): String {
        val normalized = value.trim().lowercase()
        if (!DTLS_FINGERPRINT.matches(normalized)) fail("daemon DTLS fingerprint is invalid")
        return normalized
    }

    private fun verifyEd25519(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean {
        return try {
            val verifier = Ed25519Signer()
            verifier.init(false, Ed25519PublicKeyParameters(publicKey, 0))
            verifier.update(message, 0, message.size)
            verifier.verifySignature(signature)
        } catch (_: Exception) {
            false
        }
    }

    private fun encodeBase64Url(value: ByteArray): String = Base64.toBase64String(value)
        .trimEnd('=').replace('+', '-').replace('/', '_')

    private fun decodeBase64Url(value: String): ByteArray {
        if (!value.matches(Regex("[A-Za-z0-9_-]*"))) fail("base64url value is invalid")
        val standard = value.replace('-', '+').replace('_', '/').padEnd(value.length + (4 - value.length % 4) % 4, '=')
        return try {
            Base64.decode(standard)
        } catch (_: Exception) {
            fail("base64url value is invalid")
        }
    }

    private fun constantTimeEquals(left: String, right: String): Boolean = MessageDigest.isEqual(
        left.toByteArray(StandardCharsets.UTF_8),
        right.toByteArray(StandardCharsets.UTF_8),
    )

    private fun requireKeys(value: JSONObject, allowed: Set<String>, label: String) {
        val keys = value.keys().asSequence().toSet()
        if (!allowed.containsAll(keys)) fail("$label contains unsupported fields")
    }

    private fun JSONObject.requiredString(key: String): String {
        if (!has(key) || get(key) !is String) fail("capability grant $key is invalid")
        return getString(key).trim()
    }

    private fun JSONObject.requiredObject(key: String): JSONObject {
        if (!has(key) || get(key) !is JSONObject) fail("capability grant $key is invalid")
        return getJSONObject(key)
    }

    private fun JSONObject.requiredInteger(key: String): Int {
        if (!has(key)) fail("capability grant $key is invalid")
        val value = get(key)
        if (value !is Int && value !is Long) fail("capability grant $key is invalid")
        val number = (value as Number).toLong()
        if (number !in Int.MIN_VALUE..Int.MAX_VALUE) fail("capability grant $key is invalid")
        return number.toInt()
    }

    private fun JSONObject.optionalBoolean(key: String): Boolean {
        if (!has(key)) return false
        val value = get(key)
        if (value !is Boolean) fail("capability grant $key is invalid")
        return value
    }

    private fun JSONObject.optionalString(key: String): String {
        if (!has(key)) return ""
        val value = get(key)
        if (value !is String) fail("capability grant $key is invalid")
        return value.trim()
    }

    private fun JSONObject.requiredInstant(key: String): Instant = try {
        Instant.parse(requiredString(key))
    } catch (_: Exception) {
        fail("capability grant $key is invalid")
    }

    private fun messageHasUnknown(message: Message): Boolean {
        if (message.unknownFields.asMap().isNotEmpty()) return true
        for ((field, value) in message.allFields) {
            if (field.javaType != Descriptors.FieldDescriptor.JavaType.MESSAGE) continue
            if (field.isRepeated) {
                @Suppress("UNCHECKED_CAST")
                val children = value as List<Message>
                if (children.any(::messageHasUnknown)) return true
            } else if (messageHasUnknown(value as Message)) {
                return true
            }
        }
        return false
    }

    private fun fail(message: String, code: String = "protocol"): Nothing = throw ManagedEndpointFailure(code, message)
}
