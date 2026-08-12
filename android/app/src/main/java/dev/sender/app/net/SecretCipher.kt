package dev.sender.app.net

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Encrypts the device secret at rest with a 256-bit AES-GCM key held in the
 * AndroidKeyStore, so the credential never leaves the device in plaintext.
 * Format: base64(iv || ciphertext), 12-byte GCM IV, 128-bit tag.
 */
object SecretCipher {

    private const val KEY_ALIAS = "sender_device_secret_key"
    private const val GCM_IV_BYTES = 12
    private const val GCM_TAG_BITS = 128

    /**
     * Production uses the AndroidKeyStore-backed key (persistent and, where
     * the hardware allows, non-exportable). Robolectric has no AndroidKeyStore
     * provider, so unit tests swap in a stable JVM AES key.
     */
    internal var keyProvider: () -> SecretKey = ::androidKeyStoreKey

    private fun androidKeyStoreKey(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val existing = store.getKey(KEY_ALIAS, null) as? SecretKey
        if (existing != null) return existing
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    /** Encrypts plaintext; returns base64(iv || ciphertext). */
    fun encrypt(plain: String): String {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, keyProvider())
        val ciphertext = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(cipher.iv + ciphertext, Base64.NO_WRAP)
    }

    /**
     * True for the plaintext format written by app versions before encryption:
     * exactly 32 lowercase hex characters. Ciphertext is base64 (iv +
     * ciphertext + tag, ~60 chars), so the two can never be confused.
     */
    fun isLegacyPlaintext(stored: String): Boolean =
        stored.length == 32 && stored.all { it in '0'..'9' || it in 'a'..'f' }

    /**
     * Decrypts a stored blob; when the value is a legacy plaintext secret from
     * a previous app version it is returned unchanged so the caller can
     * re-encrypt it.
     */
    fun decryptOrPassThrough(stored: String): String {
        if (isLegacyPlaintext(stored)) return stored
        return try {
            val blob = Base64.decode(stored, Base64.NO_WRAP)
            if (blob.size <= GCM_IV_BYTES) {
                stored
            } else {
                val cipher = Cipher.getInstance("AES/GCM/NoPadding")
                cipher.init(
                    Cipher.DECRYPT_MODE,
                    keyProvider(),
                    GCMParameterSpec(GCM_TAG_BITS, blob.copyOfRange(0, GCM_IV_BYTES)),
                )
                cipher.doFinal(blob.copyOfRange(GCM_IV_BYTES, blob.size)).toString(Charsets.UTF_8)
            }
        } catch (_: Exception) {
            stored
        }
    }
}
