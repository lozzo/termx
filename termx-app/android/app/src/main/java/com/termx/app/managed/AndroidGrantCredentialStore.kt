package com.termx.app.managed

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * AndroidGrantCredentialStore 使用 Android Keystore AES-GCM key 保存 capability grant。
 * 普通 endpoint 配置和 Web storage 只持有 grant_ref；解密后的 grant 只返回公开 authorizer，禁止交给 cloud adapter。
 */
class AndroidGrantCredentialStore(context: Context) : GrantCredentialStore {
    private val preferences = context.applicationContext.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)

    /** put 原子保存 grant_ref 对应的密文；空值或非法引用 fail closed，不提供明文 SharedPreferences fallback。 */
    fun put(grantRef: String, grant: String) {
        val normalizedRef = validateRef(grantRef)
        val normalizedGrant = grant.trim()
        if (normalizedGrant.isEmpty()) throw ManagedEndpointFailure("unauthenticated", "capability grant is empty")
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val ciphertext = cipher.doFinal(normalizedGrant.toByteArray(Charsets.UTF_8))
        val encoded = Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + "." +
            Base64.encodeToString(ciphertext, Base64.NO_WRAP)
        if (!preferences.edit().putString(preferenceKey(normalizedRef), encoded).commit()) {
            throw ManagedEndpointFailure("temporary", "failed to persist capability grant")
        }
    }

    /** resolve 解密 grant_ref；缺失、损坏或 Keystore 失败都作为当前 endpoint 授权失败，不读取旧 session token。 */
    override suspend fun resolve(grantRef: String): String {
        val normalizedRef = validateRef(grantRef)
        val encoded = preferences.getString(preferenceKey(normalizedRef), null)
            ?: throw ManagedEndpointFailure("unauthenticated", "capability grant is missing")
        val parts = encoded.split('.', limit = 2)
        if (parts.size != 2) throw ManagedEndpointFailure("unauthenticated", "capability grant ciphertext is malformed")
        return try {
            val iv = Base64.decode(parts[0], Base64.NO_WRAP)
            val ciphertext = Base64.decode(parts[1], Base64.NO_WRAP)
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, iv))
            String(cipher.doFinal(ciphertext), Charsets.UTF_8).trim().also {
                if (it.isEmpty()) throw ManagedEndpointFailure("unauthenticated", "capability grant is empty")
            }
        } catch (failure: ManagedEndpointFailure) {
            throw failure
        } catch (_: Exception) {
            throw ManagedEndpointFailure("unauthenticated", "capability grant could not be decrypted")
        }
    }

    /** delete 删除本地 grant 密文；它不撤销 daemon-local capability，撤销仍由 owning daemon 负责。 */
    fun delete(grantRef: String) {
        preferences.edit().remove(preferenceKey(validateRef(grantRef))).commit()
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
        if (!GRANT_REF.matches(normalized)) throw ManagedEndpointFailure("unauthenticated", "grant_ref is invalid")
        return normalized
    }

    private fun preferenceKey(grantRef: String): String = "grant." +
        Base64.encodeToString(grantRef.toByteArray(Charsets.UTF_8), Base64.NO_WRAP or Base64.URL_SAFE)

    companion object {
        private const val KEYSTORE = "AndroidKeyStore"
        private const val KEY_ALIAS = "termx.managed.grants.v1"
        private const val PREFERENCES_NAME = "termx_managed_grants_v1"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private val GRANT_REF = Regex("^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
    }
}
