package dev.sender.app.net

import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Minimal HTTP client over HttpURLConnection (no extra dependencies).
 * Success = any 2xx. The server URL is resolved per call so settings changes apply immediately.
 */
class ApiClient(
    private val serverUrl: () -> String,
    private val connectionFactory: (String) -> HttpURLConnection = { url ->
        URL(url).openConnection() as HttpURLConnection
    },
) : Api {

    override suspend fun register(deviceId: String, secret: String, deviceName: String): Boolean =
        withContext(Dispatchers.IO) {
            val body = """{"device_id":${PayloadBuilder.jsonString(deviceId)},"name":${PayloadBuilder.jsonString(deviceName)}}"""
            request(base(), "POST", "/api/v1/devices/register", body) { conn ->
                conn.setRequestProperty("X-Device-Secret", secret)
            }
        }

    override suspend fun upload(deviceId: String, secret: String, body: String): Boolean =
        withContext(Dispatchers.IO) {
            request(base(), "POST", "/api/v1/devices/$deviceId/messages", body) { conn ->
                conn.setRequestProperty("Authorization", "Bearer $secret")
            }
        }

    override suspend fun health(baseUrl: String): Boolean = withContext(Dispatchers.IO) {
        request(baseUrl, "GET", "/healthz", null, {})
    }

    private fun base(): String = serverUrl().trim().trimEnd('/')

    private fun request(
        baseUrl: String,
        method: String,
        path: String,
        payload: String?,
        headers: (HttpURLConnection) -> Unit,
    ): Boolean {
        if (baseUrl.isEmpty()) return false
        return try {
            val conn = connectionFactory("$baseUrl$path")
            try {
                conn.requestMethod = method
                conn.connectTimeout = 10_000
                conn.readTimeout = 15_000
                headers(conn)
                if (payload != null) {
                    conn.doOutput = true
                    conn.setRequestProperty("Content-Type", "application/json")
                    conn.outputStream.use { it.write(payload.toByteArray(Charsets.UTF_8)) }
                }
                conn.responseCode in 200..299
            } finally {
                conn.disconnect()
            }
        } catch (_: Exception) {
            false
        }
    }
}
