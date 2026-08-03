package dev.sender.app.net

import android.content.Context
import android.os.Build
import java.security.SecureRandom
import java.util.UUID

/**
 * First-launch identity: UUID device id + 32-hex secret, generated once and persisted.
 */
class DeviceIdentity(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("sender_identity", Context.MODE_PRIVATE)

    val deviceId: String by lazy {
        prefs.getString(KEY_DEVICE_ID, null) ?: UUID.randomUUID().toString().also {
            prefs.edit().putString(KEY_DEVICE_ID, it).apply()
        }
    }

    val secret: String by lazy {
        prefs.getString(KEY_SECRET, null) ?: randomHex32().also {
            prefs.edit().putString(KEY_SECRET, it).apply()
        }
    }

    val deviceName: String
        get() = Build.MODEL

    private fun randomHex32(): String {
        val bytes = ByteArray(16)
        SecureRandom().nextBytes(bytes)
        return bytes.joinToString("") { "%02x".format(it.toInt() and 0xFF) }
    }

    private companion object {
        const val KEY_DEVICE_ID = "device_id"
        const val KEY_SECRET = "secret"
    }
}
