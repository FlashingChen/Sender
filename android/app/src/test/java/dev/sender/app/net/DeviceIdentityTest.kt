package dev.sender.app.net

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class DeviceIdentityTest {

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
}
