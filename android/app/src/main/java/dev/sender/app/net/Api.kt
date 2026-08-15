package dev.sender.app.net

/**
 * Result of one upload batch. AUTH_FAILED (401/403) means the server no
 * longer recognizes this device registration and a re-registration is needed.
 */
enum class UploadResult {
    SUCCESS,
    AUTH_FAILED,
    FAILED,
}

/**
 * Result of device registration. DISABLED (403) means the server has closed
 * registration (ALLOW_REGISTRATION=false) — the only remediable-by-user case.
 */
enum class RegisterResult {
    OK,
    DISABLED,
    FAILED,
}

/** Server contract endpoints, client side. */
interface Api {

    /** POST /api/v1/devices/register with X-Device-Secret header. */
    suspend fun register(deviceId: String, secret: String, deviceName: String): RegisterResult

    /** POST /api/v1/devices/{deviceId}/messages with Authorization: Bearer header. */
    suspend fun upload(deviceId: String, secret: String, body: String): UploadResult

    /** GET {baseUrl}/healthz; true = 2xx. */
    suspend fun health(baseUrl: String): Boolean
}
