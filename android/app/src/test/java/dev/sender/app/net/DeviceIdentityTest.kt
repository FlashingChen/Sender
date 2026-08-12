package dev.sender.app.net

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import javax.crypto.KeyGenerator
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class DeviceIdentityTest {

    /** Robolectric has no AndroidKeyStore provider; use a stable JVM AES key. */
    @Before
    fun setUp() {
        val key = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()
        SecretCipher.keyProvider = { key }
        // SharedPreferences survive between tests in Robolectric; start clean.
        ApplicationProvider.getApplicationContext<Context>()
            .getSharedPreferences("sender_identity", Context.MODE_PRIVATE)
            .edit().clear().commit()
    }

    @Test
    fun secret_is32HexChars() {
        val identity = DeviceIdentity(ApplicationProvider.getApplicationContext<Context>())
        assertTrue(Regex("^[0-9a-f]{32}$").matches(identity.secret))
    }

    @Test
    fun deviceId_isUuid() {
        val identity = DeviceIdentity(ApplicationProvider.getApplicationContext<Context>())
        assertTrue(
            Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
                .matches(identity.deviceId),
        )
    }

    /** Identity is generated once and persisted. */
    @Test
    fun identity_isStableAcrossInstances() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val first = DeviceIdentity(context)
        val second = DeviceIdentity(context)
        assertEquals(first.deviceId, second.deviceId)
        assertEquals(first.secret, second.secret)
    }

    /** The persisted value must be ciphertext, never the plaintext secret. */
    @Test
    fun secret_isStoredEncryptedNotPlaintext() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val identity = DeviceIdentity(context)
        val secret = identity.secret
        val stored = context
            .getSharedPreferences("sender_identity", Context.MODE_PRIVATE)
            .getString("secret", null)
        assertTrue(stored != null)
        assertNotEquals(secret, stored)
        assertTrue(Regex("^[0-9a-f]{32}$").matches(stored!!).not())
    }

    /** A legacy plaintext secret from a previous version is read and re-encrypted. */
    @Test
    fun legacyPlaintextSecret_isMigratedToCiphertext() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val legacy = "0123456789abcdef0123456789abcdef"
        context.getSharedPreferences("sender_identity", Context.MODE_PRIVATE)
            .edit().putString("secret", legacy).commit()
        val identity = DeviceIdentity(context)
        assertEquals(legacy, identity.secret)
        val stored = context
            .getSharedPreferences("sender_identity", Context.MODE_PRIVATE)
            .getString("secret", null)
        assertNotEquals(legacy, stored)
    }
}
