package dev.sender.app.net

import java.io.ByteArrayOutputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * HTTP boundary contract tests: exact paths, header placement
 * (X-Device-Secret on register, Authorization: Bearer on upload),
 * 2xx-only success. The connection is a JDK fake, not our code.
 */
class ApiClientTest {

    private class FakeConnection(url: URL) : HttpURLConnection(url) {
        val output = ByteArrayOutputStream()
        var status = 200
        val createdUrls = mutableListOf<String>()

        fun header(name: String): String? = requestProperties[name]?.firstOrNull()

        override fun disconnect() {}
        override fun usingProxy(): Boolean = false
        override fun connect() {}
        override fun getResponseCode(): Int = status
        override fun getOutputStream(): OutputStream = output
    }

    private class FakeFactory : (String) -> HttpURLConnection {
        val connections = mutableListOf<FakeConnection>()
        var failWith: Exception? = null
        var status = 200

        override fun invoke(url: String): HttpURLConnection {
            if (failWith != null) throw failWith!!
            return FakeConnection(URL(url)).also {
                it.createdUrls += url
                it.status = status
                connections += it
            }
        }
    }

    private val secret = "abcdef0123456789abcdef0123456789"
    private val deviceId = "11111111-1111-1111-1111-111111111111"

    private fun client(factory: FakeFactory) = ApiClient(
        serverUrl = { "http://10.0.2.2:8080" },
        connectionFactory = factory,
    )

    /** Contract: POST /api/v1/devices/register with X-Device-Secret header. */
    @Test
    fun register_hitsContractPathWithSecretHeader() = runBlocking {
        val factory = FakeFactory()
        val ok = client(factory).register(deviceId, secret, "Pixel 8")
        assertTrue(ok)
        val conn = factory.connections.single()
        assertEquals("POST", conn.requestMethod)
        assertEquals("http://10.0.2.2:8080/api/v1/devices/register", conn.createdUrls.single())
        assertEquals(secret, conn.header("X-Device-Secret"))
        assertEquals("""{"device_id":"$deviceId","name":"Pixel 8"}""", conn.output.toString(Charsets.UTF_8.name()))
    }

    /** Contract: POST /api/v1/devices/{device_id}/messages with Authorization: Bearer <secret>. */
    @Test
    fun upload_hitsContractPathWithBearerHeader() = runBlocking {
        val factory = FakeFactory()
        val body = """{"messages":[{"client_msg_id":"com.tencent.mm:notif_key:1780000000123"}]}"""
        val ok = client(factory).upload(deviceId, secret, body)
        assertEquals(UploadResult.SUCCESS, ok)
        val conn = factory.connections.single()
        assertEquals("POST", conn.requestMethod)
        assertEquals("http://10.0.2.2:8080/api/v1/devices/$deviceId/messages", conn.createdUrls.single())
        assertEquals("Bearer $secret", conn.header("Authorization"))
        assertEquals(body, conn.output.toString(Charsets.UTF_8.name()))
    }

    /** 401/403 means the device registration is gone; everything else is a plain failure. */
    @Test
    fun upload_distinguishesAuthFailure() = runBlocking {
        val unauthorized = FakeFactory().also { it.status = 401 }
        assertEquals(UploadResult.AUTH_FAILED, client(unauthorized).upload(deviceId, secret, "{}"))
        val forbidden = FakeFactory().also { it.status = 403 }
        assertEquals(UploadResult.AUTH_FAILED, client(forbidden).upload(deviceId, secret, "{}"))
        val serverError = FakeFactory().also { it.status = 500 }
        assertEquals(UploadResult.FAILED, client(serverError).upload(deviceId, secret, "{}"))
    }

    /** Network failure = upload failure; rows stay synced=0 for retry. */
    @Test
    fun networkError_returnsFailure() = runBlocking {
        val factory = FakeFactory()
        factory.failWith = java.io.IOException("connection refused")
        assertEquals(UploadResult.FAILED, client(factory).upload(deviceId, secret, "{}"))
        assertFalse(client(factory).register(deviceId, secret, "Pixel 8"))
    }
}
