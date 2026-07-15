package com.termx.app.managed

import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.time.Instant
import java.util.Base64

/** ManagedPairingImport 是 native 验签 canonical bootstrap 并持久化 ClientAccessIdentity 后返回给 UI 的非秘密 metadata。 */
data class ManagedPairingImport(
    val endpointId: String,
    val label: String,
    val targetDeviceId: String,
    val deviceFingerprint: String,
    val grantRef: String,
    val ticketId: String,
    val clientKeyFingerprint: String,
    val expiresAt: Instant,
    val authorizationRequired: Boolean,
)

/**
 * ManagedPairingImporter 严格导入 daemon 生成的 EndpointBootstrapBundleV2。
 * 当前切片只准备 per-endpoint ClientAccessIdentity；ticket 必须由后续 route connector 兑换，不能把“已扫码”投影为“已授权”。
 */
class ManagedPairingImporter internal constructor(private val prepareIdentity: (String, String) -> String) {
    /** 使用 Android Keystore credential store 创建生产导入器；lambda 构造只供同 module 纯领域测试。 */
    constructor(credentials: AndroidClientAccessCredentialStore) : this({ ref, endpointId ->
        credentials.loadOrCreateIdentity(ref, endpointId).identity.fingerprint
    })

    /** import 接受 CLI 二维码使用的 termx bootstrap URI；二进制 protobuf 不经过 JSON 或 WebView object parser。 */
    fun import(
        payload: String,
        now: Instant = Instant.now(),
        expectedEndpointId: String? = null,
    ): ManagedPairingImport = importBytes(decodePortablePayload(payload), now, expectedEndpointId)

    /** importBytes 供 native scanner 和跨语言 fixture 直接提交 canonical protobuf bytes。 */
    internal fun importBytes(
        payload: ByteArray,
        now: Instant = Instant.now(),
        expectedEndpointId: String? = null,
    ): ManagedPairingImport {
        val verified = AndroidRemoteAuth.verifyPairingBundle(payload, now)
        val bundle = verified.bundle
        val ticket = verified.ticket
        val deviceId = bundle.identity.deviceId
        val fingerprint = bundle.identity.deviceFingerprint
        val endpointId = normalizeEndpointId(expectedEndpointId ?: deviceId)
        val label = bundle.suggestedLabel.ifBlank { deviceId }
        val grantRef = credentialRef(deviceId, fingerprint)
        val clientFingerprint = prepareIdentity(grantRef, endpointId)
        return ManagedPairingImport(
            endpointId = endpointId,
            label = label,
            targetDeviceId = deviceId,
            deviceFingerprint = fingerprint,
            grantRef = grantRef,
            ticketId = ticket.ticketId,
            clientKeyFingerprint = clientFingerprint,
            expiresAt = ticket.expiresAt,
            authorizationRequired = true,
        )
    }

    private fun decodePortablePayload(value: String): ByteArray {
        val normalized = value.trim()
        val encoded = when {
            normalized.startsWith(BOOTSTRAP_URI_PREFIX) -> normalized.removePrefix(BOOTSTRAP_URI_PREFIX)
            BASE64URL.matches(normalized) -> normalized
            else -> throw ManagedEndpointFailure("pairing_ticket_invalid", "managed pairing payload must be a TermX bootstrap URI")
        }
        return try {
            Base64.getUrlDecoder().decode(encoded)
        } catch (_: Exception) {
            throw ManagedEndpointFailure("pairing_ticket_invalid", "managed pairing bootstrap payload is invalid")
        }
    }

    private fun credentialRef(deviceId: String, fingerprint: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest("$deviceId\n$fingerprint".toByteArray(StandardCharsets.UTF_8))
        return "android-access-${Base64.getUrlEncoder().withoutPadding().encodeToString(digest)}"
    }

    private fun normalizeEndpointId(value: String): String {
        val normalized = value.trim()
        if (!ENDPOINT_ID.matches(normalized)) {
            throw ManagedEndpointFailure("protocol", "managed pairing endpoint id is invalid")
        }
        return normalized
    }

    private companion object {
        const val BOOTSTRAP_URI_PREFIX = "termx://bootstrap?payload="
        val ENDPOINT_ID = Regex("^[A-Za-z0-9._-]{1,128}$")
        val BASE64URL = Regex("^[A-Za-z0-9_-]+$")
    }
}
