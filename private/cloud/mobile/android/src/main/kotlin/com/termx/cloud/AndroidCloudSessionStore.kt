package com.termx.cloud

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import com.termx.app.managed.ManagedEndpointFailure
import org.json.JSONObject
import java.net.URI
import java.security.KeyStore
import java.time.Instant
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** AccountSession 是 Official cloud adapter 的短期账号边缘凭据与签名 HubDirectory 投影。 */
internal data class AccountSession(
    val token: ByteArray,
    val expiresAt: Instant,
    val accountId: String,
    val deviceId: String,
    val hubId: String,
    val hubURL: String,
    val region: String,
    val directoryVersion: Long,
)

/** CloudSessionStore 只持久化 Control Plane 签发的短期 edge session，不接触 CapabilityGrant 或 terminal 数据。 */
internal interface CloudSessionStore {
    fun load(now: Instant): AccountSession?
    fun save(session: AccountSession)
}

/** MemoryCloudSessionStore 是 JVM contract harness 的进程重建夹具；产品装配必须使用 AndroidCloudSessionStore。 */
internal class MemoryCloudSessionStore : CloudSessionStore {
    private var session: AccountSession? = null

    override fun load(now: Instant): AccountSession? = session?.takeIf { now.isBefore(it.expiresAt) }

    override fun save(session: AccountSession) {
        validateReplacement(this.session, session)
        this.session = session.copy(token = session.token.copyOf())
    }
}

/**
 * AndroidCloudSessionStore 使用独立 Android Keystore AES-GCM key 保存短期 edge token 与 HubDirectory。
 * SharedPreferences 只保存密文；解密失败、过期、Hub 变更或目录回滚均 fail closed，不回退明文或旧地址。
 */
internal class AndroidCloudSessionStore(context: Context) : CloudSessionStore {
    private val preferences = context.applicationContext.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    override fun load(now: Instant): AccountSession? {
        val encoded = preferences.getString(SESSION_KEY, null) ?: return null
        val session = try {
            decode(decrypt(encoded))
        } catch (_: Exception) {
            preferences.edit().remove(SESSION_KEY).commit()
            throw ManagedEndpointFailure("login_required", "cached cloud session could not be verified")
        }
        if (!now.isBefore(session.expiresAt)) {
            preferences.edit().remove(SESSION_KEY).commit()
            return null
        }
        return session
    }

    override fun save(session: AccountSession) {
        validateSession(session)
        val current = load(Instant.EPOCH)
        validateReplacement(current, session)
        if (!preferences.edit().putString(SESSION_KEY, encrypt(encode(session))).commit()) {
            throw ManagedEndpointFailure("temporary", "failed to persist cloud session")
        }
    }

    private fun encrypt(value: ByteArray): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val ciphertext = cipher.doFinal(value)
        value.fill(0)
        return Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + "." + Base64.encodeToString(ciphertext, Base64.NO_WRAP)
    }

    private fun decrypt(value: String): ByteArray {
        val parts = value.split('.', limit = 2)
        require(parts.size == 2)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, Base64.decode(parts[0], Base64.NO_WRAP)))
        return cipher.doFinal(Base64.decode(parts[1], Base64.NO_WRAP))
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setRandomizedEncryptionRequired(true)
            .build())
        return generator.generateKey()
    }

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val KEY_ALIAS = "termx.official.cloud.session.v1"
        const val PREFERENCES_NAME = "termx_official_cloud_session_v1"
        const val SESSION_KEY = "account"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}

private fun encode(session: AccountSession): ByteArray = JSONObject()
    .put("version", 1)
    .put("token", Base64.encodeToString(session.token, Base64.NO_WRAP))
    .put("expires_at_unix", session.expiresAt.epochSecond)
    .put("account_id", session.accountId)
    .put("device_id", session.deviceId)
    .put("hub_id", session.hubId)
    .put("hub_url", session.hubURL)
    .put("region", session.region)
    .put("directory_version", session.directoryVersion)
    .toString().toByteArray(Charsets.UTF_8)

private fun decode(value: ByteArray): AccountSession = try {
    val json = JSONObject(String(value, Charsets.UTF_8))
    value.fill(0)
    require(json.getInt("version") == 1)
    AccountSession(
        Base64.decode(json.getString("token"), Base64.NO_WRAP),
        Instant.ofEpochSecond(json.getLong("expires_at_unix")),
        json.getString("account_id"), json.getString("device_id"), json.getString("hub_id"),
        json.getString("hub_url"), json.getString("region"), json.getLong("directory_version"),
    ).also(::validateSession)
} catch (failure: Exception) {
    value.fill(0)
    throw failure
}

private fun validateSession(session: AccountSession) {
    val uri = URI(session.hubURL)
    require(session.token.size >= 16 && session.accountId.isNotBlank() && session.deviceId.isNotBlank())
    require(session.hubId.isNotBlank() && session.region.isNotBlank() && session.directoryVersion > 0)
    require(uri.scheme in setOf("http", "https") && !uri.host.isNullOrBlank() && uri.userInfo == null && uri.query == null && uri.fragment == null)
}

private fun validateReplacement(current: AccountSession?, next: AccountSession) {
    validateSession(next)
    if (current != null && (current.hubId != next.hubId || next.directoryVersion < current.directoryVersion)) {
        throw ManagedEndpointFailure("unauthenticated", "cloud HubDirectory replacement was rejected")
    }
}
