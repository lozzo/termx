package com.termx.app.managed

import org.bouncycastle.util.encoders.Base64
import org.json.JSONObject
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.time.Instant

/** ManagedPairingImport 是 native secure-store 写入成功后返回给 UI 的非秘密 endpoint metadata。 */
data class ManagedPairingImport(
    val endpointId: String,
    val label: String,
    val targetDeviceId: String,
    val deviceFingerprint: String,
    val grantRef: String,
    val expiresAt: Instant,
)

/**
 * ManagedPairingImporter 严格导入 daemon 生成的 v1 pairing bundle。
 * 原始 grant 只写入 AndroidGrantCredentialStore；返回值与 WebView storage 只能包含 grant_ref 和 endpoint identity。
 */
class ManagedPairingImporter internal constructor(private val writeGrant: (String, String) -> Unit) {
    /** 使用 Android Keystore credential store 创建生产导入器；lambda 构造只供同 module 纯领域测试。 */
    constructor(credentials: AndroidGrantCredentialStore) : this(credentials::put)

    /**
     * import 验签完整 bundle，并在写 credential 前校验当前 UI 授权目标。
     * expectedEndpointId 为空表示新增 endpoint；不匹配或验签失败时不得写入任何 secret。
     */
    fun import(
        payload: String,
        now: Instant = Instant.now(),
        expectedEndpointId: String? = null,
    ): ManagedPairingImport {
        val bundle = try {
            JSONObject(payload.trim())
        } catch (_: Exception) {
            throw ManagedEndpointFailure("protocol", "managed pairing bundle is not valid JSON")
        }
        val keys = bundle.keys().asSequence().toSet()
        if (!setOf("version", "label", "device_id", "device_fingerprint", "capability_grant").containsAll(keys) ||
            bundle.requiredInteger("version") != 1) {
            throw ManagedEndpointFailure("protocol", "managed pairing bundle is unsupported")
        }
        val deviceId = bundle.requiredString("device_id")
        val fingerprint = bundle.requiredString("device_fingerprint")
        val grant = bundle.requiredString("capability_grant")
        val claims = AndroidRemoteAuth.verifyGrant(grant, fingerprint, now)
        if (claims.issuerDeviceId != deviceId || claims.issuerDeviceFingerprint != fingerprint || !claims.scope.allowDaemon) {
            throw ManagedEndpointFailure("scope_invalid", "managed pairing bundle does not grant daemon access")
        }
        if (expectedEndpointId != null && expectedEndpointId != deviceId) {
            throw ManagedEndpointFailure("endpoint_mismatch", "managed pairing bundle belongs to a different endpoint")
        }
        val grantRef = grantRef(deviceId, fingerprint)
        writeGrant(grantRef, grant)
        return ManagedPairingImport(
            endpointId = deviceId,
            label = bundle.optionalString("label").ifBlank { deviceId },
            targetDeviceId = deviceId,
            deviceFingerprint = fingerprint,
            grantRef = grantRef,
            expiresAt = claims.expiresAt,
        )
    }

    private fun grantRef(deviceId: String, fingerprint: String): String {
        val digest = MessageDigest.getInstance("SHA-256").digest("$deviceId\n$fingerprint".toByteArray(StandardCharsets.UTF_8))
        val encoded = Base64.toBase64String(digest).trimEnd('=').replace('+', '-').replace('/', '_')
        return "android-managed-$encoded"
    }

    private fun JSONObject.requiredString(key: String): String {
        if (!has(key) || get(key) !is String) throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is invalid")
        return getString(key).trim().ifBlank {
            throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is required")
        }
    }

    private fun JSONObject.requiredInteger(key: String): Int {
        if (!has(key)) throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is invalid")
        val value = get(key)
        if (value !is Int && value !is Long) {
            throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is invalid")
        }
        val number = (value as Number).toLong()
        if (number !in Int.MIN_VALUE..Int.MAX_VALUE) {
            throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is invalid")
        }
        return number.toInt()
    }

    private fun JSONObject.optionalString(key: String): String {
        if (!has(key)) return ""
        val value = get(key)
        if (value !is String) throw ManagedEndpointFailure("protocol", "managed pairing bundle $key is invalid")
        return value.trim()
    }
}
