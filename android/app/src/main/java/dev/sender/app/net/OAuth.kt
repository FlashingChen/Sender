package dev.sender.app.net

import java.net.HttpURLConnection
import java.net.URL
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

/** Device-bind outcome: Ok carries the bound account name, Err a displayable reason. */
sealed interface BindResult {
    data class Ok(val username: String) : BindResult
    data class Err(val reason: String) : BindResult
}

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
                if (kv.size == 2) kv[0] to uriDecode(kv[1]) else null
            }
            .toMap()
        val code = params["code"]
        return if (code != null) {
            Callback.Code(code, params["state"] ?: "")
        } else {
            Callback.Error(params["error"] ?: "unknown", params["state"])
        }
    }

    /**
     * RFC 3986 percent-decoding. Unlike URLDecoder (form semantics), a literal
     * '+' in a URI query is data and must not become a space.
     */
    private fun uriDecode(value: String): String {
        val output = StringBuilder(value.length)
        val bytes = ArrayList<Byte>(4)
        var i = 0
        while (i < value.length) {
            val c = value[i]
            if (c == '%' && i + 2 < value.length) {
                val code = value.substring(i + 1, i + 3).toIntOrNull(16)
                if (code != null) {
                    bytes.add(code.toByte())
                    i += 3
                    continue
                }
            }
            if (bytes.isNotEmpty()) {
                output.append(String(bytes.toByteArray(), Charsets.UTF_8))
                bytes.clear()
            }
            output.append(c)
            i++
        }
        if (bytes.isNotEmpty()) {
            output.append(String(bytes.toByteArray(), Charsets.UTF_8))
        }
        return output.toString()
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

    /**
     * POST /api/v1/devices/register with X-Device-Secret; OK=2xx, DISABLED=403
     * (server closed registration), FAILED=anything else (network or 5xx).
     */
    suspend fun register(deviceId: String, secret: String, deviceName: String): RegisterResult =
        withContext(Dispatchers.IO) {
            val body = """{"device_id":${PayloadBuilder.jsonString(deviceId)},"name":${PayloadBuilder.jsonString(deviceName)}}"""
            val conn = connectionFactory("${serverUrl().trim().trimEnd('/')}/api/v1/devices/register")
            try {
                conn.requestMethod = "POST"
                conn.connectTimeout = 10_000
                conn.readTimeout = 15_000
                conn.doOutput = true
                conn.setRequestProperty("X-Device-Secret", secret)
                conn.setRequestProperty("Content-Type", "application/json")
                conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                when (conn.responseCode) {
                    in 200..299 -> RegisterResult.OK
                    403 -> RegisterResult.DISABLED
                    else -> RegisterResult.FAILED
                }
            } catch (_: Exception) {
                RegisterResult.FAILED
            } finally {
                conn.disconnect()
            }
        }

    /**
     * POST /api/v1/devices/bind with Bearer token. Ok carries the account
     * name; Err carries a user-displayable reason so the UI can say what
     * actually went wrong instead of a generic retry hint.
     */
    suspend fun bind(accessToken: String, deviceId: String, secret: String): BindResult =
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
                val code = conn.responseCode
                val response = responseBody(conn)
                when {
                    code in 200..299 && response != null && jsonBoolean(response, "ok") ->
                        BindResult.Ok(jsonString(response, "username") ?: return@withContext BindResult.Err("服务器响应缺少用户名"))
                    code == 400 -> BindResult.Err(bindBadRequestReason(response))
                    code == 401 -> BindResult.Err("授权已失效，请重新绑定")
                    code == 403 -> BindResult.Err("服务器拒绝了绑定请求")
                    code == 409 -> BindResult.Err("设备已被其他账号绑定")
                    code == 429 -> BindResult.Err("操作过于频繁，请稍后再试")
                    code in 500..599 -> BindResult.Err("服务器错误（HTTP $code），请稍后重试")
                    else -> BindResult.Err("绑定失败（HTTP $code）")
                }
            } catch (_: Exception) {
                BindResult.Err("网络请求失败，请检查网络与服务器地址")
            } finally {
                conn.disconnect()
            }
        }

    private fun bindBadRequestReason(response: String?): String {
        val error = response?.let { jsonString(it, "error") }
        return when (error) {
            "device not found" -> "设备未注册：请先让应用完成注册（上报一次）后再绑定"
            "invalid device secret" -> "设备密钥不匹配，请检查设备信息"
            "device already bound to another account" -> "设备已被其他账号绑定"
            else -> "绑定请求无效（${error ?: "HTTP 400"}）"
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
