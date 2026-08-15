package dev.sender.app.net

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * OAuth contract tests: fixed PKCE challenge vector, authorize URL shape and
 * encoding, session state validation, callback parsing, token exchange and
 * bind request shape (fake JDK connection, same pattern as ApiClientTest).
 */
class OAuthTest {

    private class FakeConnection(url: URL) : HttpURLConnection(url) {
        val output = ByteArrayOutputStream()
        var status = 200
        var responseBody = ""
        val createdUrls = mutableListOf<String>()

        fun header(name: String): String? = requestProperties[name]?.firstOrNull()

        override fun disconnect() {}
        override fun usingProxy(): Boolean = false
        override fun connect() {}
        override fun getResponseCode(): Int = status
        override fun getOutputStream(): OutputStream = output
        override fun getInputStream(): InputStream =
            ByteArrayInputStream(responseBody.toByteArray(Charsets.UTF_8))
    }

    private class FakeFactory : (String) -> HttpURLConnection {
        val connections = mutableListOf<FakeConnection>()
        var status = 200
        var responseBody = ""

        override fun invoke(url: String): HttpURLConnection = FakeConnection(URL(url)).also {
            it.createdUrls += url
            it.status = status
            it.responseBody = responseBody
            connections += it
        }
    }

    private fun oauth(factory: FakeFactory) = OAuth(
        serverUrl = { "http://10.0.2.2:8080" },
        connectionFactory = factory,
    )

    /** Fixed 43-char verifier -> precomputed RFC 7636 S256 challenge (base64url, no padding). */
    @Test
    fun fixedVerifier_producesExpectedChallenge() {
        val verifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
        assertEquals(43, verifier.length)
        assertEquals("g0tuZ6q412zO9IRkeAUs8HN6MQeXPsGce37J3Rsc8wQ", PkceGenerator.challenge(verifier))
    }

    /** Generated verifier must be 43 chars from the contract alphabet. */
    @Test
    fun newVerifier_is43CharsFromContractAlphabet() {
        val verifier = PkceGenerator.newVerifier()
        assertEquals(43, verifier.length)
        assertTrue(verifier.all { it in "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._" })
    }

    /** Contract authorize URL: all params present, redirect_uri percent-encoded, trailing slash trimmed. */
    @Test
    fun authorizeUrl_hasAllParamsWithEncodedRedirectUri() {
        val url = oauth(FakeFactory()).authorizeUrl("challenge123", "state456")
        assertEquals(
            "http://10.0.2.2:8080/authorize" +
                "?response_type=code&client_id=sender-android" +
                "&redirect_uri=sender%3A%2F%2Foauth" +
                "&code_challenge=challenge123&code_challenge_method=S256&state=state456",
            url,
        )
    }

    /** State mismatch must be rejected without consuming the pending session. */
    @Test
    fun session_rejectsMismatchedState() {
        val pending = OAuthSession.begin("http://10.0.2.2:8080")
        assertNull(OAuthSession.consumeVerifier("forged-state"))
        assertEquals(pending.verifier, OAuthSession.consumeVerifier(pending.state))
    }

    /** Matching state consumes the session; a second consume returns null. */
    @Test
    fun session_consumesVerifierOnceOnMatch() {
        val pending = OAuthSession.begin("http://10.0.2.2:8080")
        assertEquals(pending.verifier, OAuthSession.consumeVerifier(pending.state))
        assertNull(OAuthSession.consumeVerifier(pending.state))
    }

    /** Callback with code -> Code(code, state). */
    @Test
    fun callback_parsesCodeBranch() {
        val cb = OAuthCallback.parse("sender://oauth?code=abc123&state=xyz")
        assertTrue(cb is OAuthCallback.Callback.Code)
        assertEquals("abc123", (cb as OAuthCallback.Callback.Code).code)
        assertEquals("xyz", cb.state)
    }

    /** RFC 3986: a literal '+' in the query is data, not a form-encoded space. */
    @Test
    fun callback_keepsLiteralPlusAndDecodesPercentEscapes() {
        val cb = OAuthCallback.parse("sender://oauth?code=a+b%2Bc&state=%E4%B8%AD")
        assertTrue(cb is OAuthCallback.Callback.Code)
        assertEquals("a+b+c", (cb as OAuthCallback.Callback.Code).code)
        assertEquals("中", cb.state)
    }

    /** Callback with error -> Error(error, state), e.g. access_denied (user cancel). */
    @Test
    fun callback_parsesErrorBranch() {
        val cb = OAuthCallback.parse("sender://oauth?error=access_denied&state=xyz")
        assertTrue(cb is OAuthCallback.Callback.Error)
        assertEquals("access_denied", (cb as OAuthCallback.Callback.Error).error)
        assertEquals("xyz", cb.state)
    }

    /** Token exchange: POST form to /api/v1/oauth/token, returns parsed access_token. */
    @Test
    fun exchangeToken_postsContractFormAndParsesToken() = runBlocking {
        val factory = FakeFactory()
        factory.responseBody = """{"access_token":"tok123","token_type":"Bearer","expires_in":3600}"""
        val token = oauth(factory).exchangeToken("CODE123", "VERIFIER123")
        assertEquals("tok123", token)
        val conn = factory.connections.single()
        assertEquals("POST", conn.requestMethod)
        assertEquals("http://10.0.2.2:8080/api/v1/oauth/token", conn.createdUrls.single())
        assertEquals("application/x-www-form-urlencoded", conn.header("Content-Type"))
        assertEquals(
            "grant_type=authorization_code&code=CODE123&code_verifier=VERIFIER123" +
                "&client_id=sender-android&redirect_uri=sender://oauth",
            conn.output.toString(Charsets.UTF_8.name()),
        )
    }

    /** Token exchange failure (non-2xx) -> null. */
    @Test
    fun exchangeToken_non2xx_returnsNull() = runBlocking {
        val factory = FakeFactory()
        factory.status = 401
        assertNull(oauth(factory).exchangeToken("CODE", "VERIFIER"))
    }

    /** Bind: Bearer header + device body, returns username. */
    @Test
    fun bind_sendsBearerAndDeviceBody_returnsUsername() = runBlocking {
        val factory = FakeFactory()
        factory.responseBody = """{"ok":true,"username":"alice"}"""
        val result = oauth(factory).bind("tok123", "dev-1", "sec-1")
        assertEquals(BindResult.Ok("alice"), result)
        val conn = factory.connections.single()
        assertEquals("POST", conn.requestMethod)
        assertEquals("http://10.0.2.2:8080/api/v1/devices/bind", conn.createdUrls.single())
        assertEquals("Bearer tok123", conn.header("Authorization"))
        assertEquals("""{"device_id":"dev-1","secret":"sec-1"}""", conn.output.toString(Charsets.UTF_8.name()))
    }

    /** Bind 400 device not found -> displayable reason mentioning registration. */
    @Test
    fun bind_deviceNotFound_reportsRegistration() = runBlocking {
        val factory = FakeFactory()
        factory.status = 400
        factory.responseBody = """{"error":"device not found"}"""
        val result = oauth(factory).bind("tok123", "dev-1", "sec-1")
        assertTrue(result is BindResult.Err)
        assertTrue((result as BindResult.Err).reason.contains("未注册"))
    }

    /** Bind 401 -> token invalid, re-bind needed. */
    @Test
    fun bind_401_reportsExpiredAuthorization() = runBlocking {
        val factory = FakeFactory()
        factory.status = 401
        val result = oauth(factory).bind("tok123", "dev-1", "sec-1")
        assertTrue(result is BindResult.Err)
        assertTrue((result as BindResult.Err).reason.contains("授权已失效"))
    }

    /** Bind 409 -> already owned by another account. */
    @Test
    fun bind_409_reportsAlreadyBound() = runBlocking {
        val factory = FakeFactory()
        factory.status = 409
        val result = oauth(factory).bind("tok123", "dev-1", "sec-1")
        assertTrue(result is BindResult.Err)
        assertTrue((result as BindResult.Err).reason.contains("已被其他账号绑定"))
    }

    /** Bind 429 -> rate limited, retry later. */
    @Test
    fun bind_429_reportsRateLimit() = runBlocking {
        val factory = FakeFactory()
        factory.status = 429
        val result = oauth(factory).bind("tok123", "dev-1", "sec-1")
        assertTrue(result is BindResult.Err)
        assertTrue((result as BindResult.Err).reason.contains("过于频繁"))
    }

    /** Register: X-Device-Secret header + device body, 2xx -> OK. */
    @Test
    fun register_hitsContractPathWithSecretHeader() = runBlocking {
        val factory = FakeFactory()
        factory.responseBody = """{"ok":true}"""
        assertEquals(RegisterResult.OK, oauth(factory).register("dev-1", "sec-1", "Pixel 8"))
        val conn = factory.connections.single()
        assertEquals("POST", conn.requestMethod)
        assertEquals("http://10.0.2.2:8080/api/v1/devices/register", conn.createdUrls.single())
        assertEquals("sec-1", conn.header("X-Device-Secret"))
        assertEquals("""{"device_id":"dev-1","name":"Pixel 8"}""", conn.output.toString(Charsets.UTF_8.name()))
    }

    /** Register 403 -> registration disabled server-side. */
    @Test
    fun register_403_isDisabled() = runBlocking {
        val factory = FakeFactory()
        factory.status = 403
        factory.responseBody = """{"error":"registration disabled"}"""
        assertEquals(RegisterResult.DISABLED, oauth(factory).register("dev-1", "sec-1", "Pixel 8"))
    }

    /** Register network failure -> FAILED. */
    @Test
    fun register_networkError_isFailed() = runBlocking {
        val factory = FakeFactory()
        factory.status = -1
        assertEquals(RegisterResult.FAILED, oauth(factory).register("dev-1", "sec-1", "Pixel 8"))
    }
}
