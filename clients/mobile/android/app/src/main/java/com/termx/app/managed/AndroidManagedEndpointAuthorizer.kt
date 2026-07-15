package com.termx.app.managed

import com.google.protobuf.ByteString
import com.google.protobuf.CodedInputStream
import com.google.protobuf.CodedOutputStream
import com.google.protobuf.Descriptors
import com.google.protobuf.Message
import com.google.protobuf.WireFormat
import com.termx.app.transport.WebRTCTransport
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.json.JSONObject
import termx.remote.auth.v1.RemoteAuth
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.charset.StandardCharsets
import java.nio.charset.CodingErrorAction
import java.security.MessageDigest
import java.security.SecureRandom
import java.time.Instant
import java.time.Duration
import java.util.Base64

/**
 * AndroidManagedEndpointAuthorizer 是公开 Android App 的 DataChannel 端到端授权实现。
 * endpoint pin 与 ClientAccessCredential 来自 native secure store，DTLS binding 来自实际 PeerConnection；任何校验失败都只关闭当前 managed transport。
 */
class AndroidManagedEndpointAuthorizer(
    private val now: () -> Instant = { Instant.now() },
    private val random: SecureRandom = SecureRandom(),
) : ManagedEndpointAuthorizer {
    /** authorize 完成 DeviceHello/v2 client-key proof，并在 CapabilityAccepted 后才切换到 termx wire。 */
    override suspend fun authorize(
        transport: WebRTCTransport,
        spec: ManagedEndpointSpec,
        credential: AndroidClientAccessCredential,
    ) = withContext(Dispatchers.IO) {
        val claims = AndroidRemoteAuth.verifyGrant(credential.capabilityGrant, spec.deviceFingerprint, now())
        AndroidRemoteAuth.verifyEndpointBinding(claims, spec.targetDeviceId, credential.identity.fingerprint)
        val dtlsFingerprint = transport.remoteCertificateFingerprint(AUTH_TIMEOUT_MS)
            ?: throw ManagedEndpointFailure("device_identity_mismatch", "remote DTLS certificate is unavailable")
        val helloFrame = transport.receiveAuthorizationMessage(AUTH_TIMEOUT_MS)
            ?: throw ManagedEndpointFailure("protocol", "daemon did not send DeviceHello")
        val open = AndroidRemoteAuth.acceptDeviceHello(
            frame = helloFrame,
            expectedDeviceId = spec.targetDeviceId,
            expectedDeviceFingerprint = spec.deviceFingerprint,
            daemonDtlsFingerprint = dtlsFingerprint,
            credential = credential,
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

/** AndroidRemoteAuthClaims 是 App 授权状态机使用的已验签 v2 grant 摘要，不包含 grant body。 */
data class AndroidRemoteAuthClaims(
    val grantId: String,
    val issuerDeviceId: String,
    val issuerDeviceFingerprint: String,
    val subjectKeyFingerprint: String,
    val scope: AndroidRemoteAuthScope,
    val notBefore: Instant,
    val expiresAt: Instant,
)

/** AndroidRemoteAuthScope 是互斥基础 scope、显式文件权限与独立 ManageClientAccess capability。 */
data class AndroidRemoteAuthScope(
    val allowDaemon: Boolean,
    val terminalId: String,
    val machineEventsOnly: Boolean,
    val fileReadMetadata: Boolean,
    val fileReadContent: Boolean,
    val fileWriteContent: Boolean,
    val fileMutate: Boolean,
    val manageClientAccess: Boolean,
)

/** AndroidCapabilityOpen 是完成 DeviceHello 校验后待发送的 auth frame 与 session binding。 */
data class AndroidCapabilityOpen(val authSessionId: String, val frame: ByteArray)

private data class AndroidChannelBinding(val kind: RemoteAuth.ChannelBindingKind, val hash: ByteArray)

/**
 * AndroidRemoteAuth 是 Go `shared/remoteauth` v2 的 Android wire 对等实现。
 * 它只处理公开 DeviceIdentity、client-bound grant 与 DataChannel proof；Cloud adapter、Hub 和 Relay 不得调用或接收这些值。
 */
object AndroidRemoteAuth {
    private const val AUTH_PROTOCOL = "termx-remote-auth"
    private const val AUTH_VERSION = 2
    private const val GRANT_PREFIX = "termx-grant-v2"
    private const val MAX_AUTH_FRAME_SIZE = 64 * 1024
    private const val MAX_PORTABLE_CONTRACT_SIZE = 256 * 1024
    private const val CLOCK_SKEW_SECONDS = 120L
    private const val PAIRING_TICKET_SIGNATURE_PROTOCOL = "termx.pairing-ticket.signature"
    private const val ENDPOINT_BOOTSTRAP_SIGNATURE_PROTOCOL = "termx.endpoint-bootstrap.signature"
    private const val PORTABLE_SIGNATURE_VERSION = 1
    private val AUTH_MAGIC = byteArrayOf('T'.code.toByte(), 'X'.code.toByte(), 'R'.code.toByte(), 'A'.code.toByte())
    private val SESSION_ID = Regex("[A-Za-z0-9_-]{16,128}")
    private val PORTABLE_ID = Regex("[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
    private val DTLS_FINGERPRINT = Regex("sha-256:(?:[0-9a-f]{2}:){31}[0-9a-f]{2}")

    /** verifyGrant 严格验证 v2 envelope、DeviceIdentity signature、endpoint pin、subject、有效期和互斥 scope。 */
    fun verifyGrant(grant: String, expectedFingerprint: String, now: Instant): AndroidRemoteAuthClaims {
        val claims = try {
            verifyGrantEnvelope(grant, expectedFingerprint)
        } catch (failure: ManagedEndpointFailure) {
            if (failure.code == "protocol") throw ManagedEndpointFailure("capability_invalid", failure.message ?: "capability grant is invalid")
            throw failure
        }
        if (now.isBefore(claims.notBefore)) fail("capability grant is not active", "capability_expired")
        if (!now.isBefore(claims.expiresAt)) fail("capability grant has expired", "capability_expired")
        return claims
    }

    /** verifyEndpointBinding 区分 daemon issuer identity 与 ClientAccessIdentity subject，不把两种修复路径压成同一错误。 */
    fun verifyEndpointBinding(claims: AndroidRemoteAuthClaims, expectedDeviceId: String, clientKeyFingerprint: String) {
        if (claims.issuerDeviceId != expectedDeviceId.trim()) {
            fail("capability issuer does not match managed endpoint target", "device_identity_mismatch")
        }
        if (claims.subjectKeyFingerprint != clientKeyFingerprint.trim()) {
            fail("capability subject does not match ClientAccessIdentity", "subject_key_mismatch")
        }
    }

    /** verifyGrantEnvelope 供 secure store 比较旧/新 scope；它验签全部 claims，但不把旧 grant 的过期时间当作无法比较。 */
    internal fun verifyGrantEnvelope(grant: String, expectedFingerprint: String): AndroidRemoteAuthClaims {
        val normalizedGrant = grant.trim()
        val parts = normalizedGrant.split('.')
        if (parts.size != 4 || parts[0] != GRANT_PREFIX) fail("capability grant envelope is invalid", "capability_invalid")
        val publicKey = decodeBase64Url(parts[2])
        if (publicKey.size != 32) fail("capability grant public key is invalid", "capability_invalid")
        val fingerprint = deviceFingerprint(publicKey)
        if (!constantTimeEquals(fingerprint, expectedFingerprint.trim())) fail("capability grant fingerprint does not match endpoint pin", "device_identity_mismatch")
        val signature = decodeBase64Url(parts[3])
        if (signature.size != 64 || !verifyEd25519(publicKey, parts.take(3).joinToString(".").toByteArray(StandardCharsets.UTF_8), signature)) {
            fail("capability grant signature is invalid", "capability_invalid")
        }
        val claims = strictObject(decodeBase64Url(parts[1]), "capability grant")
        requireKeys(claims, setOf(
            "version", "grant_id", "issuer_device_id", "issuer_device_fingerprint", "subject_key_fingerprint", "scope",
            "issued_at", "not_before", "expires_at", "revocation_id", "nonce",
        ), "capability grant")
        val scopeObject = claims.requiredObject("scope")
        requireKeys(scopeObject, setOf(
            "allow_daemon", "terminal_id", "machine_events_only", "file_read_metadata", "file_read_content",
            "file_write_content", "file_mutate", "manage_client_access",
        ), "capability scope")
        val scope = AndroidRemoteAuthScope(
            allowDaemon = scopeObject.optionalBoolean("allow_daemon"),
            terminalId = scopeObject.optionalString("terminal_id"),
            machineEventsOnly = scopeObject.optionalBoolean("machine_events_only"),
            fileReadMetadata = scopeObject.optionalBoolean("file_read_metadata"),
            fileReadContent = scopeObject.optionalBoolean("file_read_content"),
            fileWriteContent = scopeObject.optionalBoolean("file_write_content"),
            fileMutate = scopeObject.optionalBoolean("file_mutate"),
            manageClientAccess = scopeObject.optionalBoolean("manage_client_access"),
        )
        val scopeCount = listOf(scope.allowDaemon, scope.terminalId.isNotEmpty(), scope.machineEventsOnly).count { it }
        val hasFilePermission = scope.fileReadMetadata || scope.fileReadContent || scope.fileWriteContent || scope.fileMutate
        val issuedAt = claims.requiredInstant("issued_at")
        val notBefore = claims.requiredInstant("not_before")
        val expiresAt = claims.requiredInstant("expires_at")
        val grantId = claims.requiredString("grant_id")
        val issuerDeviceId = claims.requiredString("issuer_device_id")
        val issuerFingerprint = claims.requiredString("issuer_device_fingerprint")
        val subjectFingerprint = claims.requiredString("subject_key_fingerprint")
        if (claims.requiredInteger("version") != 2 || grantId.isEmpty() || issuerDeviceId.isEmpty() || subjectFingerprint.isEmpty() ||
            !constantTimeEquals(issuerFingerprint, fingerprint) || claims.requiredString("revocation_id").isEmpty() ||
            claims.requiredString("nonce").isEmpty() || scopeCount != 1 || (hasFilePermission && !scope.allowDaemon) ||
            !expiresAt.isAfter(issuedAt) || !expiresAt.isAfter(notBefore)) {
            fail("capability grant claims are invalid", "capability_invalid")
        }
        return AndroidRemoteAuthClaims(grantId, issuerDeviceId, issuerFingerprint, subjectFingerprint, scope, notBefore, expiresAt)
    }

    /** verifyPairingBundle 验证与 Go connection owner 相同的 deterministic EndpointBootstrapBundleV2 和内层 ticket。 */
    fun verifyPairingBundle(
        payload: ByteArray,
        now: Instant,
        expectedFingerprint: String? = null,
    ): AndroidPairingBundleClaims {
        if (payload.isEmpty() || payload.size > MAX_PORTABLE_CONTRACT_SIZE) {
            fail("pairing bootstrap size is invalid", "pairing_ticket_invalid")
        }
        val bundle = try {
            RemoteAuth.EndpointBootstrapBundleV2.parseFrom(payload)
        } catch (_: Exception) {
            fail("pairing bootstrap protobuf is invalid", "pairing_ticket_invalid")
        }
        if (hasUnknownFields(bundle) || !MessageDigest.isEqual(payload, deterministicBytes(bundle)) || bundle.schemaVersion != 2 ||
            !PORTABLE_ID.matches(bundle.bundleId)) {
            fail("pairing bootstrap contract is invalid", "pairing_ticket_invalid")
        }
        val identity = bundle.identity
        val publicKey = identity.devicePublicKey.toByteArray()
        val fingerprint = deviceFingerprint(publicKey)
        if (identity.deviceId != identity.deviceId.trim() || identity.deviceId.isEmpty() || publicKey.size != 32 ||
            identity.deviceFingerprint != identity.deviceFingerprint.trim() || !constantTimeEquals(fingerprint, identity.deviceFingerprint)) {
            fail("pairing bootstrap daemon identity is invalid", "device_identity_mismatch")
        }
        expectedFingerprint?.trim()?.takeIf { it.isNotEmpty() }?.let { expected ->
            if (!constantTimeEquals(fingerprint, expected)) {
                fail("pairing bootstrap daemon identity does not match endpoint pin", "device_identity_mismatch")
            }
        }
        val issuedAt = instantFromUnixNanos(bundle.issuedAtUnixNano, "pairing bootstrap issued_at")
        val expiresAt = instantFromUnixNanos(bundle.expiresAtUnixNano, "pairing bootstrap expires_at")
        if (!expiresAt.isAfter(issuedAt)) fail("pairing bootstrap time window is invalid", "pairing_ticket_invalid")
        if (now.isBefore(issuedAt) || !now.isBefore(expiresAt)) fail("pairing ticket has expired", "pairing_ticket_expired")
        val label = bundle.suggestedLabel.trim()
        if (label != bundle.suggestedLabel || label.toByteArray(StandardCharsets.UTF_8).size > 128 || label.any(Character::isISOControl)) {
            fail("pairing bootstrap label is invalid", "pairing_ticket_invalid")
        }
        validateBootstrapRoutes(bundle.routesList, identity)
        if (bundle.authorization.payloadCase != RemoteAuth.EndpointAuthorizationBootstrap.PayloadCase.PAIRING_TICKET) {
            fail("pairing bootstrap does not contain a one-time ticket", "pairing_ticket_invalid")
        }
        val ticket = bundle.authorization.pairingTicket
        val ticketIssuedAt = instantFromUnixNanos(ticket.issuedAtUnixNano, "pairing ticket issued_at")
        val ticketExpiresAt = instantFromUnixNanos(ticket.expiresAtUnixNano, "pairing ticket expires_at")
        if (!PORTABLE_ID.matches(ticket.ticketId) || ticket.scopeCeilingList.isEmpty() ||
            ticket.scopeCeilingList != ticket.scopeCeilingList.distinct().sorted() || ticket.nonce.size() < 16 ||
            ticket.maxRedemptions != 1 || ticket.grantLifetimeSeconds !in 1..31_536_000L ||
            ticket.issuedAtUnixNano != bundle.issuedAtUnixNano || !ticketExpiresAt.isAfter(ticketIssuedAt) || ticketExpiresAt.isAfter(expiresAt) ||
            Duration.between(ticketIssuedAt, ticketExpiresAt) > Duration.ofHours(24) || ticket.signature.size() != 64) {
            fail("pairing ticket claims are invalid", "pairing_ticket_invalid")
        }
        if (now.isBefore(ticketIssuedAt) || !now.isBefore(ticketExpiresAt)) fail("pairing ticket has expired", "pairing_ticket_expired")
        val unsignedTicket = ticket.toBuilder().clearSignature().build()
        val ticketInput = RemoteAuth.PairingTicketSignatureInput.newBuilder()
            .setProtocol(PAIRING_TICKET_SIGNATURE_PROTOCOL).setVersion(PORTABLE_SIGNATURE_VERSION)
            .setIssuerDeviceId(identity.deviceId).setIssuerDeviceFingerprint(identity.deviceFingerprint)
            .setTicket(unsignedTicket).build()
        if (!verifyEd25519(publicKey, deterministicBytes(ticketInput), ticket.signature.toByteArray())) {
            fail("pairing ticket signature is invalid", "pairing_ticket_invalid")
        }
        val unsignedBundle = bundle.toBuilder().clearBundleSignature().build()
        val bundleInput = RemoteAuth.EndpointBootstrapSignatureInput.newBuilder()
            .setProtocol(ENDPOINT_BOOTSTRAP_SIGNATURE_PROTOCOL).setVersion(PORTABLE_SIGNATURE_VERSION)
            .setBundle(unsignedBundle).build()
        if (bundle.bundleSignature.size() != 64 || !verifyEd25519(publicKey, deterministicBytes(bundleInput), bundle.bundleSignature.toByteArray())) {
            fail("pairing bootstrap signature is invalid", "pairing_ticket_invalid")
        }
        return AndroidPairingBundleClaims(
            bundle = bundle,
            ticket = AndroidPairingTicketClaims(
                ticketId = ticket.ticketId,
                issuerDeviceId = identity.deviceId,
                issuerDeviceFingerprint = identity.deviceFingerprint,
                scopeCeiling = pairingScope(ticket.scopeCeilingList),
                notBefore = ticketIssuedAt,
                expiresAt = ticketExpiresAt,
                grantLifetimeSeconds = ticket.grantLifetimeSeconds,
            ),
        )
    }

    private fun validateBootstrapRoutes(
        routes: List<RemoteAuth.EndpointAccessRoute>,
        identity: RemoteAuth.EndpointDaemonIdentity,
    ) {
        val routeIds = mutableSetOf<String>()
        routes.forEach { route ->
            if (!PORTABLE_ID.matches(route.routeId) || !routeIds.add(route.routeId) || route.credentialRef.isNotEmpty() ||
                route.source != RemoteAuth.EndpointSource.ENDPOINT_SOURCE_UNSPECIFIED ||
                route.policySource != RemoteAuth.EndpointSource.ENDPOINT_SOURCE_UNSPECIFIED || route.manualOnly || route.hasPriority()) {
                fail("pairing bootstrap route is not portable", "pairing_ticket_invalid")
            }
            val textFields = listOf(
                route.socket, route.host, route.user, route.proxyJump, route.remoteSocket,
                route.serverName, route.targetDeviceId, route.accountProfile,
            )
            if (textFields.any { !isCanonicalRouteText(it) } ||
                !isCanonicalRouteList(route.hostKeyFingerprintsList, false) ||
                !isCanonicalRouteList(route.addressesList, route.kind == RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_DIRECT_TLS) ||
                Integer.toUnsignedLong(route.port) > 65_535L ||
                (route.kind != RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_MANAGED_WEBRTC &&
                    route.relayMode != RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_UNSPECIFIED)) {
                fail("pairing bootstrap route fields are invalid", "pairing_ticket_invalid")
            }
            val hasSshFields = route.host.isNotEmpty() || route.port != 0 || route.user.isNotEmpty() ||
                route.proxyJump.isNotEmpty() || route.remoteSocket.isNotEmpty() || route.hostKeyFingerprintsCount != 0
            val hasDirectFields = route.addressesCount != 0 || route.serverName.isNotEmpty()
            val hasManagedFields = route.targetDeviceId.isNotEmpty() || route.accountProfile.isNotEmpty() ||
                route.relayMode != RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_UNSPECIFIED
            when (route.kind) {
                RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_SSH_STDIO ->
                    if (route.host.isEmpty() || route.socket.isNotEmpty() || hasDirectFields || hasManagedFields) {
                        fail("pairing bootstrap SSH route is invalid", "pairing_ticket_invalid")
                    }
                RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_DIRECT_TLS ->
                    if (route.addressesList.isEmpty() || route.socket.isNotEmpty() || hasSshFields || hasManagedFields) {
                        fail("pairing bootstrap direct route is invalid", "pairing_ticket_invalid")
                    }
                RemoteAuth.EndpointRouteKind.ENDPOINT_ROUTE_KIND_MANAGED_WEBRTC ->
                    if (route.targetDeviceId != identity.deviceId || route.socket.isNotEmpty() || hasSshFields || hasDirectFields ||
                        route.relayMode !in setOf(
                            RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_UNSPECIFIED,
                            RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_AUTO,
                            RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_DIRECT,
                            RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_RELAY_ONLY,
                            RemoteAuth.EndpointRelayMode.ENDPOINT_RELAY_MODE_SMART_ROUTE,
                        )) {
                        fail("pairing bootstrap managed route is invalid", "pairing_ticket_invalid")
                    }
                else -> fail("pairing bootstrap route kind is invalid", "pairing_ticket_invalid")
            }
        }
    }

    private fun isCanonicalRouteText(value: String): Boolean =
        value == value.trim() && value.none(Character::isISOControl)

    private fun isCanonicalRouteList(values: List<String>, required: Boolean): Boolean {
        if (required && values.isEmpty()) return false
        val seen = mutableSetOf<String>()
        return values.all { it.isNotEmpty() && isCanonicalRouteText(it) && seen.add(it) }
    }

    private fun pairingScope(values: List<String>): AndroidRemoteAuthScope {
        var allowDaemon = false
        var terminalId = ""
        var machineEventsOnly = false
        var fileReadMetadata = false
        var fileReadContent = false
        var fileWriteContent = false
        var fileMutate = false
        var manageClientAccess = false
        values.forEach { value ->
            when (value) {
                "base:daemon" -> allowDaemon = true
                "base:machine_events" -> machineEventsOnly = true
                "file:read_metadata" -> fileReadMetadata = true
                "file:read_content" -> fileReadContent = true
                "file:write_content" -> fileWriteContent = true
                "file:mutate" -> fileMutate = true
                "manage:client_access" -> manageClientAccess = true
                else -> {
                    if (!value.startsWith("base:terminal:")) fail("pairing ticket scope is invalid", "scope_invalid")
                    val encodedTerminalId = value.removePrefix("base:terminal:")
                    terminalId = try {
                        val decoded = Base64.getUrlDecoder().decode(encodedTerminalId)
                        if (Base64.getUrlEncoder().withoutPadding().encodeToString(decoded) != encodedTerminalId) {
                            fail("pairing ticket terminal scope is invalid", "scope_invalid")
                        }
                        StandardCharsets.UTF_8.newDecoder()
                            .onMalformedInput(CodingErrorAction.REPORT)
                            .onUnmappableCharacter(CodingErrorAction.REPORT)
                            .decode(ByteBuffer.wrap(decoded)).toString()
                    } catch (_: Exception) {
                        fail("pairing ticket terminal scope is invalid", "scope_invalid")
                    }
                    if (terminalId.isBlank() || terminalId != terminalId.trim()) fail("pairing ticket terminal scope is invalid", "scope_invalid")
                }
            }
        }
        val scope = AndroidRemoteAuthScope(
            allowDaemon, terminalId, machineEventsOnly, fileReadMetadata, fileReadContent,
            fileWriteContent, fileMutate, manageClientAccess,
        )
        val baseCount = listOf(scope.allowDaemon, scope.terminalId.isNotEmpty(), scope.machineEventsOnly).count { it }
        val files = scope.fileReadMetadata || scope.fileReadContent || scope.fileWriteContent || scope.fileMutate
        if (baseCount != 1 || (files && !scope.allowDaemon)) fail("pairing ticket scope is invalid", "scope_invalid")
        return scope
    }

    private fun instantFromUnixNanos(value: Long, label: String): Instant {
        if (value <= 0) fail("$label is invalid", "pairing_ticket_invalid")
        return try {
            Instant.ofEpochSecond(Math.floorDiv(value, 1_000_000_000L), Math.floorMod(value, 1_000_000_000L))
        } catch (_: Exception) {
            fail("$label is invalid", "pairing_ticket_invalid")
        }
    }

    /** acceptDeviceHello 验证 daemon identity 与实际 DTLS binding，并用 ClientAccessIdentity 生成 Ed25519 CapabilityOpen。 */
    fun acceptDeviceHello(
        frame: ByteArray,
        expectedDeviceId: String,
        expectedDeviceFingerprint: String,
        daemonDtlsFingerprint: String,
        credential: AndroidClientAccessCredential,
        now: Instant,
        random: SecureRandom,
    ): AndroidCapabilityOpen {
        if (!credential.ready()) fail("client access credential is awaiting pairing", "unauthenticated")
        val claims = verifyGrant(credential.capabilityGrant, expectedDeviceFingerprint, now)
        if (claims.subjectKeyFingerprint != credential.identity.fingerprint) fail("capability subject does not match ClientAccessIdentity", "subject_key_mismatch")
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
        val expectedBinding = dtlsChannelBinding(daemonDtlsFingerprint)
        val helloBinding = channelBinding(hello.channelBinding)
        if (!constantTimeEquals(fingerprint, hello.deviceFingerprint) || !constantTimeEquals(fingerprint, expectedDeviceFingerprint.trim()) ||
            hello.deviceId.trim() != expectedDeviceId.trim() || !channelBindingsEqual(helloBinding, expectedBinding)) {
            fail("DeviceHello identity binding does not match endpoint", "device_identity_mismatch")
        }
        val signingInput = RemoteAuth.DeviceHelloSignatureInput.newBuilder()
            .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId(envelope.authSessionId)
            .setDeviceId(hello.deviceId.trim()).setDevicePublicKey(hello.devicePublicKey)
            .setDeviceFingerprint(hello.deviceFingerprint.trim()).setServerNonce(hello.serverNonce)
            .setChannelBinding(channelBindingMessage(expectedBinding)).setIssuedAtUnixNano(hello.issuedAtUnixNano)
            .build()
        if (!verifyEd25519(publicKey, deterministicBytes(signingInput), hello.signature.toByteArray())) {
            fail("DeviceHello signature is invalid", "device_identity_mismatch")
        }
        val clientNonce = ByteArray(32).also(random::nextBytes)
        val proofInput = clientProofInput(
            openKind = RemoteAuth.AuthOpenKind.AUTH_OPEN_KIND_CAPABILITY,
            credential = credential.capabilityGrant,
            clientPublicKey = credential.identity.publicKey,
            authSessionId = envelope.authSessionId,
            serverNonce = hello.serverNonce.toByteArray(),
            clientNonce = clientNonce,
            binding = expectedBinding,
        )
        val open = RemoteAuth.AuthEnvelope.newBuilder()
            .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId(envelope.authSessionId)
            .setCapabilityOpen(
                RemoteAuth.CapabilityOpen.newBuilder()
                    .setGrant(credential.capabilityGrant.trim())
                    .setClientPublicKey(ByteString.copyFrom(credential.identity.publicKey))
                    .setClientNonce(ByteString.copyFrom(clientNonce))
                    .setProof(ByteString.copyFrom(credential.identity.sign(deterministicBytes(proofInput)))),
            ).build()
        return AndroidCapabilityOpen(envelope.authSessionId, encodeEnvelope(open))
    }

    /** verifyCapabilityResult 确认 daemon 接受同一 grant、subject 与 scope，随后才允许进入 terminal protocol。 */
    fun verifyCapabilityResult(frame: ByteArray, authSessionId: String, claims: AndroidRemoteAuthClaims) {
        val envelope = parseEnvelope(frame)
        if (!constantTimeEquals(envelope.authSessionId, authSessionId)) fail("daemon changed auth session", "replayed")
        if (envelope.payloadCase == RemoteAuth.AuthEnvelope.PayloadCase.CAPABILITY_REJECTED) {
            fail(envelope.capabilityRejected.message.ifBlank { "daemon rejected capability" }, errorCode(envelope.capabilityRejected.code))
        }
        if (envelope.payloadCase != RemoteAuth.AuthEnvelope.PayloadCase.CAPABILITY_ACCEPTED) fail("daemon did not finish capability authorization")
        val accepted = envelope.capabilityAccepted
        if (accepted.grantId != claims.grantId || accepted.subjectKeyFingerprint != claims.subjectKeyFingerprint || !scopeMatches(accepted.scope, claims.scope)) {
            fail("daemon accepted a different capability subject or scope", "scope_invalid")
        }
    }

    /** deviceFingerprint 与 Go remoteauth.Fingerprint 使用相同 Ed25519 SHA-256 base64url 编码。 */
    fun deviceFingerprint(publicKey: ByteArray): String = "ed25519-sha256:" +
        Base64.getUrlEncoder().withoutPadding().encodeToString(MessageDigest.getInstance("SHA-256").digest(publicKey))

    private fun parseScope(scopeObject: JSONObject): AndroidRemoteAuthScope {
        requireKeys(scopeObject, setOf(
            "allow_daemon", "terminal_id", "machine_events_only", "file_read_metadata", "file_read_content",
            "file_write_content", "file_mutate", "manage_client_access",
        ), "capability scope")
        val scope = AndroidRemoteAuthScope(
            scopeObject.optionalBoolean("allow_daemon"), scopeObject.optionalString("terminal_id"),
            scopeObject.optionalBoolean("machine_events_only"), scopeObject.optionalBoolean("file_read_metadata"),
            scopeObject.optionalBoolean("file_read_content"), scopeObject.optionalBoolean("file_write_content"),
            scopeObject.optionalBoolean("file_mutate"), scopeObject.optionalBoolean("manage_client_access"),
        )
        val count = listOf(scope.allowDaemon, scope.terminalId.isNotEmpty(), scope.machineEventsOnly).count { it }
        val files = scope.fileReadMetadata || scope.fileReadContent || scope.fileWriteContent || scope.fileMutate
        if (count != 1 || (files && !scope.allowDaemon)) fail("capability scope is invalid", "scope_invalid")
        return scope
    }

    private fun clientProofInput(
        openKind: RemoteAuth.AuthOpenKind,
        credential: String,
        clientPublicKey: ByteArray,
        authSessionId: String,
        serverNonce: ByteArray,
        clientNonce: ByteArray,
        binding: AndroidChannelBinding,
    ): RemoteAuth.ClientProofInput = RemoteAuth.ClientProofInput.newBuilder()
        .setProtocol(AUTH_PROTOCOL).setVersion(AUTH_VERSION).setAuthSessionId(authSessionId)
        .setServerNonce(ByteString.copyFrom(serverNonce)).setClientNonce(ByteString.copyFrom(clientNonce))
        .setChannelBinding(channelBindingMessage(binding))
        .setCredentialSha256(ByteString.copyFrom(MessageDigest.getInstance("SHA-256").digest(credential.trim().toByteArray(StandardCharsets.UTF_8))))
        .setClientPublicKey(ByteString.copyFrom(clientPublicKey)).setOpenKind(openKind).build()

    private fun dtlsChannelBinding(fingerprint: String): AndroidChannelBinding {
        val normalized = normalizeDtlsFingerprint(fingerprint)
        val bytes = normalized.removePrefix("sha-256:").replace(":", "").chunked(2).map { it.toInt(16).toByte() }.toByteArray()
        return AndroidChannelBinding(RemoteAuth.ChannelBindingKind.CHANNEL_BINDING_KIND_DTLS, bytes)
    }

    private fun channelBinding(message: RemoteAuth.ChannelBinding): AndroidChannelBinding {
        if (message.bindingHash.size() != 32 || message.kind == RemoteAuth.ChannelBindingKind.CHANNEL_BINDING_KIND_UNSPECIFIED) fail("channel binding is malformed")
        return AndroidChannelBinding(message.kind, message.bindingHash.toByteArray())
    }

    private fun channelBindingMessage(binding: AndroidChannelBinding): RemoteAuth.ChannelBinding =
        RemoteAuth.ChannelBinding.newBuilder().setKind(binding.kind).setBindingHash(ByteString.copyFrom(binding.hash)).build()

    private fun channelBindingsEqual(left: AndroidChannelBinding, right: AndroidChannelBinding): Boolean =
        left.kind == right.kind && MessageDigest.isEqual(left.hash, right.hash)

    private fun scopeMatches(summary: RemoteAuth.ScopeSummary, scope: AndroidRemoteAuthScope): Boolean {
        val kindMatches = when {
            scope.allowDaemon -> summary.kind == RemoteAuth.ScopeKind.SCOPE_KIND_DAEMON && summary.terminalId.isEmpty()
            scope.terminalId.isNotEmpty() -> summary.kind == RemoteAuth.ScopeKind.SCOPE_KIND_TERMINAL && summary.terminalId == scope.terminalId
            scope.machineEventsOnly -> summary.kind == RemoteAuth.ScopeKind.SCOPE_KIND_MACHINE_EVENTS && summary.terminalId.isEmpty()
            else -> false
        }
        return kindMatches && summary.manageClientAccess == scope.manageClientAccess
    }

    /** scopeContains 与 Go CredentialStore 使用相同偏序，防止 App 重配对时静默扩大已有 Endpoint 权限。 */
    internal fun scopeContains(current: AndroidRemoteAuthScope, candidate: AndroidRemoteAuthScope): Boolean {
        val baseContains = when {
            current.allowDaemon -> candidate.allowDaemon || candidate.terminalId.isNotEmpty() || candidate.machineEventsOnly
            current.terminalId.isNotEmpty() -> candidate.terminalId == current.terminalId
            current.machineEventsOnly -> candidate.machineEventsOnly
            else -> false
        }
        return baseContains &&
            (!candidate.fileReadMetadata || current.fileReadMetadata) &&
            (!candidate.fileReadContent || current.fileReadContent) &&
            (!candidate.fileWriteContent || current.fileWriteContent) &&
            (!candidate.fileMutate || current.fileMutate) &&
            (!candidate.manageClientAccess || current.manageClientAccess)
    }

    private fun parseEnvelope(frame: ByteArray): RemoteAuth.AuthEnvelope {
        if (frame.size <= AUTH_MAGIC.size || frame.size > MAX_AUTH_FRAME_SIZE || !MessageDigest.isEqual(frame.copyOfRange(0, AUTH_MAGIC.size), AUTH_MAGIC)) fail("remote auth frame is invalid")
        val payload = frame.copyOfRange(AUTH_MAGIC.size, frame.size)
        requireSingleAuthPayload(payload)
        val envelope = try { RemoteAuth.AuthEnvelope.parseFrom(payload) } catch (_: Exception) { fail("remote auth frame could not be decoded") }
        if (envelope.protocol != AUTH_PROTOCOL || envelope.version != AUTH_VERSION || !SESSION_ID.matches(envelope.authSessionId) ||
            envelope.payloadCase == RemoteAuth.AuthEnvelope.PayloadCase.PAYLOAD_NOT_SET || hasUnknownFields(envelope)) fail("remote auth envelope is unsupported")
        return envelope
    }

    private fun requireSingleAuthPayload(payload: ByteArray) {
        val input = CodedInputStream.newInstance(payload)
        var payloadCount = 0
        try {
            while (!input.isAtEnd) {
                val tag = input.readTag()
                if (tag == 0) break
                if (WireFormat.getTagFieldNumber(tag) in 4..9) payloadCount++
                if (!input.skipField(tag)) break
            }
        } catch (_: Exception) {
            fail("remote auth frame could not be decoded")
        }
        if (payloadCount != 1) fail("remote auth envelope must contain exactly one payload")
    }

    private fun encodeEnvelope(envelope: RemoteAuth.AuthEnvelope): ByteArray = AUTH_MAGIC + deterministicBytes(envelope)

    private fun deterministicBytes(message: Message): ByteArray {
        val output = ByteArrayOutputStream(message.serializedSize)
        val coded = CodedOutputStream.newInstance(output)
        coded.useDeterministicSerialization()
        message.writeTo(coded)
        coded.flush()
        return output.toByteArray()
    }

    private fun hasUnknownFields(message: Message): Boolean {
        if (!message.unknownFields.asMap().isNullOrEmpty()) return true
        for (field in message.allFields.keys) {
            if (field.javaType != Descriptors.FieldDescriptor.JavaType.MESSAGE) continue
            val value = message.getField(field)
            if (field.isRepeated) {
                if ((value as List<*>).filterIsInstance<Message>().any(::hasUnknownFields)) return true
            } else if (value is Message && hasUnknownFields(value)) return true
        }
        return false
    }

    private fun normalizeDtlsFingerprint(value: String): String {
        val normalized = value.trim().lowercase()
        if (!DTLS_FINGERPRINT.matches(normalized)) fail("daemon DTLS fingerprint is malformed")
        return normalized
    }

    private fun verifyEd25519(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean = try {
        Ed25519Signer().run {
            init(false, Ed25519PublicKeyParameters(publicKey, 0))
            update(message, 0, message.size)
            verifySignature(signature)
        }
    } catch (_: Exception) { false }

    private fun strictObject(payload: ByteArray, label: String): JSONObject = try {
        JSONObject(String(payload, StandardCharsets.UTF_8))
    } catch (_: Exception) {
        fail("$label JSON is invalid")
    }

    private fun requireKeys(value: JSONObject, allowed: Set<String>, label: String) {
        val actual = value.keys().asSequence().toSet()
        if (!allowed.containsAll(actual)) fail("$label contains unknown fields")
    }

    private fun JSONObject.requiredString(key: String): String {
        if (!has(key) || get(key) !is String) fail("$key is invalid")
        return getString(key).trim().ifBlank { fail("$key is required") }
    }

    private fun JSONObject.requiredObject(key: String): JSONObject {
        if (!has(key) || get(key) !is JSONObject) fail("$key is invalid")
        return getJSONObject(key)
    }

    private fun JSONObject.optionalString(key: String): String {
        if (!has(key)) return ""
        if (get(key) !is String) fail("$key is invalid")
        return getString(key).trim()
    }

    private fun JSONObject.optionalBoolean(key: String): Boolean {
        if (!has(key)) return false
        if (get(key) !is Boolean) fail("$key is invalid")
        return getBoolean(key)
    }

    private fun JSONObject.requiredInteger(key: String): Int {
        val value = if (has(key)) get(key) else fail("$key is required")
        if (value !is Int && value !is Long) fail("$key is invalid")
        val number = (value as Number).toLong()
        if (number !in Int.MIN_VALUE..Int.MAX_VALUE) fail("$key is invalid")
        return number.toInt()
    }

    private fun JSONObject.requiredLong(key: String): Long {
        val value = if (has(key)) get(key) else fail("$key is required")
        if (value !is Int && value !is Long) fail("$key is invalid")
        return (value as Number).toLong()
    }

    private fun JSONObject.requiredInstant(key: String): Instant = try {
        Instant.parse(requiredString(key))
    } catch (_: Exception) { fail("$key is invalid") }

    private fun decodeBase64Url(value: String): ByteArray = try {
        Base64.getUrlDecoder().decode(value)
    } catch (_: Exception) { fail("base64url value is invalid") }

    private fun constantTimeEquals(left: String, right: String): Boolean =
        MessageDigest.isEqual(left.toByteArray(StandardCharsets.UTF_8), right.toByteArray(StandardCharsets.UTF_8))

    private fun errorCode(code: RemoteAuth.AuthErrorCode): String = when (code) {
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_DEVICE_IDENTITY_MISMATCH -> "device_identity_mismatch"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_CAPABILITY_EXPIRED -> "capability_expired"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_CAPABILITY_REVOKED -> "capability_revoked"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_CAPABILITY_PROOF_INVALID -> "capability_proof_invalid"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_SUBJECT_KEY_MISMATCH -> "subject_key_mismatch"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_PAIRING_TICKET_INVALID -> "pairing_ticket_invalid"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_PAIRING_TICKET_EXPIRED -> "pairing_ticket_expired"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_PAIRING_TICKET_CONSUMED -> "pairing_ticket_consumed"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_SCOPE_INVALID -> "scope_invalid"
        RemoteAuth.AuthErrorCode.AUTH_ERROR_CODE_REPLAYED -> "replayed"
        else -> "protocol"
    }

    private fun fail(message: String, code: String = "protocol"): Nothing = throw ManagedEndpointFailure(code, message)
}

/** AndroidPairingTicketClaims 是 scanner/importer 可展示的已验签短期 ticket metadata，不代表授权已完成。 */
data class AndroidPairingTicketClaims(
    val ticketId: String,
    val issuerDeviceId: String,
    val issuerDeviceFingerprint: String,
    val scopeCeiling: AndroidRemoteAuthScope,
    val notBefore: Instant,
    val expiresAt: Instant,
    val grantLifetimeSeconds: Long,
)

/** AndroidPairingBundleClaims 是已按 connection canonical contract 验签的 bootstrap 与 ticket 投影。 */
data class AndroidPairingBundleClaims(
    val bundle: RemoteAuth.EndpointBootstrapBundleV2,
    val ticket: AndroidPairingTicketClaims,
)
