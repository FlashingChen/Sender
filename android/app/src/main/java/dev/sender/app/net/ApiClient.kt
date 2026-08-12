package dev.sender.app.net

import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Minimal HTTP client over HttpURLConnection (no extra dependencies).
 * The server URL is resolved per call so settings changes apply immediately.
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
            } in 200..299
        }

    override suspend fun upload(deviceId: String, secret: String, body: String): UploadResult =
        withContext(Dispatchers.IO) {
            when (val code = request(base(), "POST", "/api/v1/devices/$deviceId/messages", body) { conn ->
                conn.setRequestProperty("Authorization", "Bearer $secret")
            }) {
                in 200..299 -> UploadResult.SUCCESS
                401, 403 -> UploadResult.AUTH_FAILED
                else -> UploadResult.FAILED
            }
        }

    override suspend fun health(baseUrl: String): Boolean = withContext(Dispatchers.IO) {
        request(baseUrl, "GET", "/healthz", null, {}) in 200..299
    }

    private fun base(): String = serverUrl().trim().trimEnd('/')

    /** Returns the HTTP status code, or -1 on network failure. */
    private fun request(
        baseUrl: String,
        method: String,
        path: String,
        payload: String?,
        headers: (HttpURLConnection) -> Unit,
    ): Int {
        if (baseUrl.isEmpty()) return -1
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
                conn.responseCode
            } finally {
                conn.disconnect()
            }
        } catch (_: Exception) {
            -1
        }
    }
}
