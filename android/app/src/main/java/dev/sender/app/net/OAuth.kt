package dev.sender.app.net

import java.net.HttpURLConnection
import java.net.URL
import java.net.URLDecoder
import java.net.URLEncoder
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * OAuth device-binding flow (contract v1, shared with the server):
 *
 *  - authorize: GET {server}/authorize?response_type=code&client_id=sender-android
 *    &redirect_uri=sender%3A%2F%2Foauth&code_challenge=<S256>&code_challenge_method=S256&state=<random>
 *  - callback:  sender://oauth?code=…&state=… | error=access_denied&state=…
 *  - token:     POST {server}/api/v1/oauth/token (x-www-form-urlencoded) -> {"access_token":…}
 *  - bind:      POST {server}/api/v1/devices/bind (Bearer token) -> {"ok":true,"username":…}
 *
 * HTTP follows ApiClient's HttpURLConnection pattern; unlike ApiClient it must
 * read response bodies, so the connection handling lives here.
 */

/** RFC 7636 PKCE primitives: 43-char verifier (A-Za-z0-9-._) and unpadded base64url S256 challenge. */
object PkceGenerator {

    private const val VERIFIER_LENGTH = 43
    private const val STATE_LENGTH = 32
    private const val ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._"
    private val random = SecureRandom()

    fun newVerifier(): String = randomString(VERIFIER_LENGTH)

    fun newState(): String = randomString(STATE_LENGTH)

    /** BASE64URL-ENCODE(SHA256(ASCII(verifier))) with padding stripped. */
    fun challenge(verifier: String): String {
        val digest = MessageDigest.getInstance("SHA-256")
            .digest(verifier.toByteArray(Charsets.US_ASCII))
        return Base64.getUrlEncoder().withoutPadding().encodeToString(digest)
    }

    private fun randomString(length: Int): String {
        val sb = StringBuilder(length)
        repeat(length) { sb.append(ALPHABET[random.nextInt(ALPHABET.length)]) }
        return sb.toString()
    }
}

/** Pending authorize state between launching the browser and the callback. */
data class PendingAuth(val state: String, val verifier: String, val challenge: String)

/**
 * Process-wide holder for the in-flight OAuth attempt. Lives outside the
 * Activity so rotation / recreation doesn't lose state before the callback.
 */
object OAuthSession {

    private var pendingState: String? = null
    private var pendingVerifier: String? = null
    private var pendingServer: String? = null

    /** Starts a new attempt, capturing state/verifier/challenge (+ server for the whole flow). */
    fun begin(server: String): PendingAuth {
        val verifier = PkceGenerator.newVerifier()
        val state = PkceGenerator.newState()
        pendingState = state
        pendingVerifier = verifier
        pendingServer = server
        return PendingAuth(state, verifier, PkceGenerator.challenge(verifier))
    }

    /**
     * Returns the verifier only when [state] matches the pending attempt, then
     * consumes the session. A mismatch is rejected and the session is kept so a
     * legitimate callback can still land.
     */
    fun consumeVerifier(state: String): String? {
        if (state != pendingState) return null
        val verifier = pendingVerifier
        pendingState = null
        pendingVerifier = null
        return verifier
    }

    /** Server address captured when the flow started (authorize/token/bind all use it). */
    fun consumeServer(): String? {
        val server = pendingServer
        pendingServer = null
        return server
    }
}

/** Parses the deep-link callback `sender://oauth?code=…&state=…` / `?error=…&state=…`. */
object OAuthCallback {

    sealed interface Callback {
        data class Code(val code: String, val state: String) : Callback
        data class Error(val error: String, val state: String?) : Callback
    }

    fun parse(uri: String): Callback {
        val query = uri.substringAfter('?', "")
        val params = query.split('&')
            .mapNotNull { part ->
                val kv = part.split('=', limit = 2)
                if (kv.size == 2) kv[0] to urlDecode(kv[1]) else null
            }
            .toMap()
        val code = params["code"]
        return if (code != null) {
            Callback.Code(code, params["state"] ?: "")
        } else {
            Callback.Error(params["error"] ?: "unknown", params["state"])
        }
    }

    private fun urlDecode(value: String): String = try {
        URLDecoder.decode(value, Charsets.UTF_8.name())
    } catch (_: IllegalArgumentException) {
        value
    }
}

/**
 * OAuth HTTP client: authorize URL building, token exchange, device bind.
 * Success returns parsed values, anything else returns null (never throws).
 */
class OAuth(
    private val serverUrl: () -> String,
    private val connectionFactory: (String) -> HttpURLConnection = { url ->
        URL(url).openConnection() as HttpURLConnection
    },
) {

    /** Contract authorize URL with redirect_uri percent-encoded. */
    fun authorizeUrl(challenge: String, state: String): String {
        val base = serverUrl().trim().trimEnd('/')
        val query = "response_type=code&client_id=sender-android" +
            "&redirect_uri=${URLEncoder.encode("sender://oauth", Charsets.UTF_8.name())}" +
            "&code_challenge=$challenge&code_challenge_method=S256&state=$state"
        return "$base/authorize?$query"
    }

    /** POST /api/v1/oauth/token; returns access_token or null. */
    suspend fun exchangeToken(code: String, verifier: String): String? = withContext(Dispatchers.IO) {
        val body = "grant_type=authorization_code" +
            "&code=${formEncode(code)}" +
            "&code_verifier=${formEncode(verifier)}" +
            "&client_id=sender-android" +
            "&redirect_uri=sender://oauth"
        val conn = connectionFactory("${serverUrl().trim().trimEnd('/')}/api/v1/oauth/token")
        try {
            conn.requestMethod = "POST"
            conn.connectTimeout = 10_000
            conn.readTimeout = 15_000
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/x-www-form-urlencoded")
            conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            if (conn.responseCode !in 200..299) return@withContext null
            jsonString(responseBody(conn) ?: return@withContext null, "access_token")
        } catch (_: Exception) {
            null
        } finally {
            conn.disconnect()
        }
    }

    /** POST /api/v1/devices/bind with Bearer token; returns username or null. */
    suspend fun bind(accessToken: String, deviceId: String, secret: String): String? =
        withContext(Dispatchers.IO) {
            val body = """{"device_id":${PayloadBuilder.jsonString(deviceId)},"secret":${PayloadBuilder.jsonString(secret)}}"""
            val conn = connectionFactory("${serverUrl().trim().trimEnd('/')}/api/v1/devices/bind")
            try {
                conn.requestMethod = "POST"
                conn.connectTimeout = 10_000
                conn.readTimeout = 15_000
                conn.doOutput = true
                conn.setRequestProperty("Authorization", "Bearer $accessToken")
                conn.setRequestProperty("Content-Type", "application/json")
                conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                if (conn.responseCode !in 200..299) return@withContext null
                val response = responseBody(conn) ?: return@withContext null
                if (!jsonBoolean(response, "ok")) null else jsonString(response, "username")
            } catch (_: Exception) {
                null
            } finally {
                conn.disconnect()
            }
        }

    private fun formEncode(value: String): String = URLEncoder.encode(value, Charsets.UTF_8.name())

    private fun responseBody(conn: HttpURLConnection): String? = try {
        conn.inputStream.bufferedReader(Charsets.UTF_8).use { it.readText() }
    } catch (_: Exception) {
        null
    }

    /** Minimal JSON string-value extraction, e.g. "access_token":"…". */
    internal fun jsonString(json: String, key: String): String? {
        val pattern = Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"((?:[^\"\\\\]|\\\\.)*)\"")
        return pattern.find(json)?.groupValues?.get(1)
    }

    /** Minimal JSON boolean extraction, e.g. "ok":true. */
    internal fun jsonBoolean(json: String, key: String): Boolean {
        val pattern = Regex("\"${Regex.escape(key)}\"\\s*:\\s*(true|false)")
        return pattern.find(json)?.groupValues?.get(1) == "true"
    }
}
