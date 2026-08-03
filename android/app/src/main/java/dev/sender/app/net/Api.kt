package dev.sender.app.net

/** Server contract endpoints, client side. */
interface Api {

    /** POST /api/v1/devices/register with X-Device-Secret header; true = 2xx. */
    suspend fun register(deviceId: String, secret: String, deviceName: String): Boolean

    /** POST /api/v1/devices/{deviceId}/messages with Authorization: Bearer header; true = 2xx. */
    suspend fun upload(deviceId: String, secret: String, body: String): Boolean

    /** GET {baseUrl}/healthz; true = 2xx. */
    suspend fun health(baseUrl: String): Boolean
}
