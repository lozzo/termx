package com.muxvia.cloud

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import com.muxvia.app.managed.ManagedEndpointFailure
import org.json.JSONObject
import java.net.URI
import java.security.KeyStore
import java.time.Instant
import java.util.UUID
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** AccountSession 是 Official cloud adapter 的短期账号边缘凭据与签名 HubDirectory 投影。 */
internal data class AccountSession(
    val token: ByteArray,
    val expiresAt: Instant,
    val refreshToken: ByteArray,
    val refreshExpiresAt: Instant,
    val accountId: String,
    val accountLabel: String,
    val deviceId: String,
    val hubId: String,
    val hubURL: String,
    val region: String,
    val directoryVersion: Long,
    val planId: String,
    val planName: String,
    val subscriptionStatus: String,
    val subscriptionRevision: Long,
)

/** CloudSessionStore 只持久化 Control Plane 签发的短期 edge session，不接触 CapabilityGrant 或 terminal 数据。 */
internal interface CloudSessionStore {
    /** installationDeviceID 返回登出和 session 轮换都不会改变的 Official App 安装身份。 */
    fun installationDeviceID(): String
    fun load(now: Instant): AccountSession?
    fun loadRefreshable(now: Instant): AccountSession?
    fun save(session: AccountSession)
    fun clear()
}

/** MemoryCloudSessionStore 是 JVM contract harness 的进程重建夹具；产品装配必须使用 AndroidCloudSessionStore。 */
internal class MemoryCloudSessionStore : CloudSessionStore {
    private var session: AccountSession? = null
    private val deviceID = newInstallationDeviceID()

    override fun installationDeviceID(): String = deviceID

    override fun load(now: Instant): AccountSession? = session?.takeIf { now.isBefore(it.expiresAt) }

    override fun loadRefreshable(now: Instant): AccountSession? = session?.takeIf { now.isBefore(it.refreshExpiresAt) }

    override fun save(session: AccountSession) {
        validateReplacement(this.session, session)
        this.session = session.copy(token = session.token.copyOf(), refreshToken = session.refreshToken.copyOf())
    }

    override fun clear() { session = null }
}

/**
 * AndroidCloudSessionStore 使用独立 Android Keystore AES-GCM key 保存短期 edge token 与 HubDirectory。
 * SharedPreferences 只保存密文；解密失败、过期、Hub 变更或目录回滚均 fail closed，不回退明文或旧地址。
 */
internal class AndroidCloudSessionStore(context: Context) : CloudSessionStore {
    private val preferences = context.applicationContext.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    override fun installationDeviceID(): String {
        preferences.getString(INSTALLATION_DEVICE_ID_KEY, null)?.let { stored ->
            if (validInstallationDeviceID(stored)) return stored
            throw ManagedEndpointFailure("login_required", "cached cloud installation identity is invalid")
        }
        val created = newInstallationDeviceID()
        if (!preferences.edit().putString(INSTALLATION_DEVICE_ID_KEY, created).commit()) {
            throw ManagedEndpointFailure("temporary", "failed to persist cloud installation identity")
        }
        return created
    }

    override fun load(now: Instant): AccountSession? {
		return loadDecoded()?.takeIf { now.isBefore(it.expiresAt) }
    }

    override fun loadRefreshable(now: Instant): AccountSession? {
		return loadDecoded()?.takeIf { now.isBefore(it.refreshExpiresAt) }
    }

    private fun loadDecoded(): AccountSession? {
        val encoded = preferences.getString(SESSION_KEY, null) ?: return null
        val session = try {
            decode(decrypt(encoded))
        } catch (_: Exception) {
            preferences.edit().remove(SESSION_KEY).commit()
            throw ManagedEndpointFailure("login_required", "cached cloud session could not be verified")
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

    override fun clear() {
        preferences.edit().remove(SESSION_KEY).commit()
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
        const val KEY_ALIAS = "muxvia.official.cloud.session.v1"
        const val PREFERENCES_NAME = "muxvia_official_cloud_session_v1"
        const val SESSION_KEY = "account"
        const val INSTALLATION_DEVICE_ID_KEY = "installation_device_id"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}

private fun newInstallationDeviceID(): String = "client-${UUID.randomUUID()}"

private fun validInstallationDeviceID(value: String): Boolean =
    value.length in 16..96 && value.startsWith("client-") && value.all { it in 'a'..'z' || it in '0'..'9' || it == '-' }

private fun encode(session: AccountSession): ByteArray = JSONObject()
    .put("version", 4)
    .put("token", Base64.encodeToString(session.token, Base64.NO_WRAP))
    .put("expires_at_unix", session.expiresAt.epochSecond)
	.put("refresh_token", Base64.encodeToString(session.refreshToken, Base64.NO_WRAP))
	.put("refresh_expires_at_unix", session.refreshExpiresAt.epochSecond)
    .put("account_id", session.accountId)
    .put("account_label", session.accountLabel)
    .put("device_id", session.deviceId)
    .put("hub_id", session.hubId)
    .put("hub_url", session.hubURL)
    .put("region", session.region)
    .put("directory_version", session.directoryVersion)
    .put("plan_id", session.planId)
    .put("plan_name", session.planName)
    .put("subscription_status", session.subscriptionStatus)
    .put("subscription_revision", session.subscriptionRevision)
    .toString().toByteArray(Charsets.UTF_8)

private fun decode(value: ByteArray): AccountSession = try {
    val json = JSONObject(String(value, Charsets.UTF_8))
    value.fill(0)
    require(json.getInt("version") == 4)
    AccountSession(
        Base64.decode(json.getString("token"), Base64.NO_WRAP),
        Instant.ofEpochSecond(json.getLong("expires_at_unix")),
		Base64.decode(json.getString("refresh_token"), Base64.NO_WRAP),
		Instant.ofEpochSecond(json.getLong("refresh_expires_at_unix")),
        json.getString("account_id"), json.getString("account_label"), json.getString("device_id"), json.getString("hub_id"),
        json.getString("hub_url"), json.getString("region"), json.getLong("directory_version"),
        json.getString("plan_id"), json.getString("plan_name"), json.getString("subscription_status"), json.getLong("subscription_revision"),
    ).also(::validateSession)
} catch (failure: Exception) {
    value.fill(0)
    throw failure
}

private fun validateSession(session: AccountSession) {
    val uri = URI(session.hubURL)
    require(session.token.size >= 16 && session.refreshToken.size >= 32 && session.refreshExpiresAt.isAfter(session.expiresAt) && session.accountId.isNotBlank() && session.accountLabel.isNotBlank() && session.deviceId.isNotBlank())
    require(session.hubId.isNotBlank() && session.region.isNotBlank() && session.directoryVersion > 0)
    require(session.planId.isNotBlank() && session.planName.isNotBlank() && session.subscriptionStatus.isNotBlank() && session.subscriptionRevision > 0)
    require(uri.scheme in setOf("http", "https") && !uri.host.isNullOrBlank() && uri.userInfo == null && uri.query == null && uri.fragment == null)
}

private fun validateReplacement(current: AccountSession?, next: AccountSession) {
    validateSession(next)
    if (current != null && (current.hubId != next.hubId || next.directoryVersion < current.directoryVersion)) {
        throw ManagedEndpointFailure("unauthenticated", "cloud HubDirectory replacement was rejected")
    }
}
