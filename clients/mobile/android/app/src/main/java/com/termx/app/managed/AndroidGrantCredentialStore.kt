package com.termx.app.managed

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import org.json.JSONObject
import java.security.KeyStore
import java.security.SecureRandom
import java.time.Instant
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * AndroidClientAccessIdentity 是单个 Endpoint 专用的 Ed25519 访问身份。
 * privateKeySeed 只存在于 Android Keystore AES-GCM 保护的 native credential payload；不同 Endpoint 不得复用 key 或 Cloud installation identity。
 */
data class AndroidClientAccessIdentity(
    val endpointId: String,
    val privateKeySeed: ByteArray,
    val publicKey: ByteArray,
    val fingerprint: String,
) {
    /** sign 仅用于 PairingExchange 或 capability channel-bound proof，不允许签 Cloud、route 或 UI payload。 */
    fun sign(message: ByteArray): ByteArray = Ed25519Signer().run {
        init(true, Ed25519PrivateKeyParameters(privateKeySeed, 0))
        update(message, 0, message.size)
        generateSignature()
    }
}

/** AndroidClientAccessCredential 是 native secure store 返回给公开 authorizer 的完整 client-bound credential。 */
data class AndroidClientAccessCredential(
    val identity: AndroidClientAccessIdentity,
    val capabilityGrant: String,
) {
    /** ready 只有在 PairingExchange 已返回并持久化非空 CapabilityGrant v2 后为 true。 */
    fun ready(): Boolean = capabilityGrant.isNotBlank()
}

/**
 * AndroidClientAccessCredentialStore 使用 Android Keystore AES-GCM key 原子保存 per-endpoint ClientAccessIdentity 与 bound grant。
 * 普通 endpoint registry/WebView 只持有 credential ref；解密结果只交给公开 PairingExchange/authorizer，禁止交给 cloud adapter。
 */
class AndroidClientAccessCredentialStore(context: Context) : ClientAccessCredentialStore {
    private val preferences = context.applicationContext.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)
    private val random = SecureRandom()
    private val lock = Any()

    /**
     * loadOrCreateIdentity 在首次 pairing 前生成并持久化 Endpoint 专用 key。
     * 已存在 ref 必须绑定同一 endpointId；先写 key 使响应丢失后的重试仍能证明同一 subject possession。
     */
    fun loadOrCreateIdentity(credentialRef: String, endpointId: String): AndroidClientAccessCredential = synchronized(lock) {
        val normalizedRef = validateRef(credentialRef)
        val normalizedEndpoint = endpointId.trim().ifBlank {
            throw ManagedEndpointFailure("protocol", "endpoint_id is required for ClientAccessIdentity")
        }
        readCredential(normalizedRef, requireGrant = false)?.let { existing ->
            if (existing.identity.endpointId != normalizedEndpoint) {
                throw ManagedEndpointFailure("identity_conflict", "credential ref belongs to another endpoint")
            }
            return@synchronized existing
        }
        val privateKey = Ed25519PrivateKeyParameters(random)
        val publicKey = privateKey.generatePublicKey().encoded
        val identity = AndroidClientAccessIdentity(
            endpointId = normalizedEndpoint,
            privateKeySeed = privateKey.encoded,
            publicKey = publicKey,
            fingerprint = AndroidRemoteAuth.deviceFingerprint(publicKey),
        )
        AndroidClientAccessCredential(identity, "").also { persist(normalizedRef, it) }
    }

    /** bindGrant 验证 daemon issuer、v2 subject 与已保存 key 后原子写入 grant；失败时保留原 key 供同 ticket 幂等重试。 */
    fun bindGrant(
        credentialRef: String,
        grant: String,
        expectedDaemonFingerprint: String,
        now: Instant = Instant.now(),
        allowScopeExpansion: Boolean = false,
    ): AndroidClientAccessCredential = synchronized(lock) {
        val normalizedRef = validateRef(credentialRef)
        val current = readCredential(normalizedRef, requireGrant = false)
            ?: throw ManagedEndpointFailure("unauthenticated", "ClientAccessIdentity is missing")
        val claims = AndroidRemoteAuth.verifyGrant(grant, expectedDaemonFingerprint, now)
        if (claims.subjectKeyFingerprint != current.identity.fingerprint) {
            throw ManagedEndpointFailure("subject_key_mismatch", "capability subject does not match ClientAccessIdentity")
        }
        if (current.ready()) {
            val currentClaims = AndroidRemoteAuth.verifyGrantEnvelope(current.capabilityGrant, expectedDaemonFingerprint)
            if (!AndroidRemoteAuth.scopeContains(currentClaims.scope, claims.scope) && !allowScopeExpansion) {
                throw ManagedEndpointFailure("scope_expansion_required", "broader capability scope requires explicit confirmation")
            }
            if (current.capabilityGrant.trim() == grant.trim()) return@synchronized current
        }
        current.copy(capabilityGrant = grant.trim()).also { persist(normalizedRef, it) }
    }

    /** resolve 解密 ready credential；缺失、损坏、空 grant 或 Keystore 失败都 fail closed，不读取旧 session token。 */
    override suspend fun resolve(credentialRef: String): AndroidClientAccessCredential =
        synchronized(lock) {
            readCredential(validateRef(credentialRef), requireGrant = true)
                ?: throw ManagedEndpointFailure("unauthenticated", "client access credential is missing")
        }

    /** delete 删除本地 ClientAccessIdentity/grant 密文；它不撤销 daemon-local grant。 */
    fun delete(credentialRef: String) = synchronized(lock) {
        if (!preferences.edit().remove(preferenceKey(validateRef(credentialRef))).commit()) {
            throw ManagedEndpointFailure("temporary", "failed to delete client access credential")
        }
    }

    private fun persist(credentialRef: String, credential: AndroidClientAccessCredential) {
        validateCredential(credential)
        val payload = JSONObject()
            .put("version", CREDENTIAL_VERSION)
            .put("endpoint_id", credential.identity.endpointId)
            .put("private_key_seed", encodeUrl(credential.identity.privateKeySeed))
            .put("capability_grant", credential.capabilityGrant.trim())
            .toString()
            .toByteArray(Charsets.UTF_8)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        cipher.updateAAD(credentialRef.toByteArray(Charsets.UTF_8))
        val ciphertext = cipher.doFinal(payload)
        val encoded = Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + "." +
            Base64.encodeToString(ciphertext, Base64.NO_WRAP)
        if (!preferences.edit().putString(preferenceKey(credentialRef), encoded).commit()) {
            throw ManagedEndpointFailure("temporary", "failed to persist client access credential")
        }
    }

    private fun readCredential(credentialRef: String, requireGrant: Boolean): AndroidClientAccessCredential? {
        val encoded = preferences.getString(preferenceKey(credentialRef), null) ?: return null
        val parts = encoded.split('.', limit = 2)
        if (parts.size != 2) throw ManagedEndpointFailure("unauthenticated", "client access credential ciphertext is malformed")
        return try {
            val iv = Base64.decode(parts[0], Base64.NO_WRAP)
            val ciphertext = Base64.decode(parts[1], Base64.NO_WRAP)
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, iv))
            cipher.updateAAD(credentialRef.toByteArray(Charsets.UTF_8))
            val value = JSONObject(String(cipher.doFinal(ciphertext), Charsets.UTF_8))
            if (value.keys().asSequence().toSet() != setOf("version", "endpoint_id", "private_key_seed", "capability_grant") ||
                value.getInt("version") != CREDENTIAL_VERSION) {
                throw ManagedEndpointFailure("unauthenticated", "client access credential schema is invalid")
            }
            val endpointId = value.getString("endpoint_id").trim()
            val seed = decodeUrl(value.getString("private_key_seed"))
            val privateKey = Ed25519PrivateKeyParameters(seed, 0)
            val publicKey = privateKey.generatePublicKey().encoded
            val credential = AndroidClientAccessCredential(
                identity = AndroidClientAccessIdentity(endpointId, seed, publicKey, AndroidRemoteAuth.deviceFingerprint(publicKey)),
                capabilityGrant = value.getString("capability_grant").trim(),
            )
            validateCredential(credential)
            if (requireGrant && !credential.ready()) {
                throw ManagedEndpointFailure("unauthenticated", "client access credential is awaiting pairing")
            }
            credential
        } catch (failure: ManagedEndpointFailure) {
            throw failure
        } catch (_: Exception) {
            throw ManagedEndpointFailure("unauthenticated", "client access credential could not be decrypted")
        }
    }

    private fun validateCredential(credential: AndroidClientAccessCredential) {
        if (credential.identity.endpointId.isBlank() || credential.identity.privateKeySeed.size != 32 || credential.identity.publicKey.size != 32 ||
            credential.identity.fingerprint != AndroidRemoteAuth.deviceFingerprint(credential.identity.publicKey)) {
            throw ManagedEndpointFailure("unauthenticated", "ClientAccessIdentity is invalid")
        }
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            ).setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build(),
        )
        return generator.generateKey()
    }

    private fun validateRef(value: String): String {
        val normalized = value.trim()
        if (!CREDENTIAL_REF.matches(normalized)) throw ManagedEndpointFailure("unauthenticated", "credential ref is invalid")
        return normalized
    }

    private fun preferenceKey(credentialRef: String): String = "access." + encodeUrl(credentialRef.toByteArray(Charsets.UTF_8))

    private fun encodeUrl(value: ByteArray): String = Base64.encodeToString(value, Base64.NO_WRAP or Base64.URL_SAFE or Base64.NO_PADDING)
    private fun decodeUrl(value: String): ByteArray = Base64.decode(value, Base64.NO_WRAP or Base64.URL_SAFE or Base64.NO_PADDING)

    companion object {
        private const val CREDENTIAL_VERSION = 1
        private const val KEYSTORE = "AndroidKeyStore"
        private const val KEY_ALIAS = "termx.client-access.credentials.v1"
        private const val PREFERENCES_NAME = "termx_client_access_credentials_v1"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private val CREDENTIAL_REF = Regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
    }
}
