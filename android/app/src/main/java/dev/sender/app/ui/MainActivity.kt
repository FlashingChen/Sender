package dev.sender.app.ui

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.lifecycle.lifecycleScope
import dev.sender.app.SenderApp
import dev.sender.app.net.BindResult
import dev.sender.app.net.OAuth
import dev.sender.app.net.OAuthCallback
import dev.sender.app.net.OAuthSession
import dev.sender.app.net.RegisterResult
import kotlinx.coroutines.launch

sealed interface Screen {
    data object Onboarding : Screen
    data object AppList : Screen
    data class Messages(val packageName: String, val appName: String) : Screen
    data object Settings : Screen
}

class MainActivity : ComponentActivity() {

    /** Bumped on every onResume so onboarding re-checks system state after returning from settings. */
    private val resumeTick = mutableIntStateOf(0)

    /** OAuth bind flow feedback shown in the settings screen. */
    private val oauthUi = mutableStateOf<OAuthUi>(OAuthUi.Idle)

    /** Guards against double-processing a callback while token exchange/bind is running. */
    private var oauthProcessing = false

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleOAuthIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        resumeTick.intValue++
        val consumed = handleOAuthIntent(intent)
        // Returning from the browser without a redirect (e.g. user backed out)
        // leaves the flow stuck in "binding…" — reset it so the button re-enables.
        if (!consumed && !oauthProcessing && oauthUi.value is OAuthUi.InProgress) {
            oauthUi.value = OAuthUi.Idle
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            val app = applicationContext as SenderApp
            var screen by remember {
                mutableStateOf(
                    if (OnboardingState.missingSteps(this@MainActivity, app.settings).isEmpty()) {
                        Screen.AppList
                    } else {
                        Screen.Onboarding
                    }
                )
            }
            LaunchedEffect(resumeTick.intValue) {
                if (screen is Screen.Onboarding) {
                    screen = if (OnboardingState.missingSteps(this@MainActivity, app.settings).isEmpty()) {
                        Screen.AppList
                    } else {
                        Screen.Onboarding
                    }
                }
            }
            SenderTheme {
                when (val s = screen) {
                    Screen.Onboarding -> OnboardingScreen(
                        onDone = { screen = Screen.AppList },
                    )

                    Screen.AppList -> AppListScreen(
                        onOpenApp = { packageName, appName -> screen = Screen.Messages(packageName, appName) },
                        onOpenSettings = { screen = Screen.Settings },
                    )

                    is Screen.Messages -> MessageListScreen(
                        packageName = s.packageName,
                        appName = s.appName,
                        onBack = { screen = Screen.AppList },
                    )

                    Screen.Settings -> SettingsScreen(
                        onBack = { screen = Screen.AppList },
                        oauthUi = oauthUi.value,
                        onBindAccount = ::startOAuthFlow,
                    )
                }
            }
        }
    }

    /** Opens the OAuth authorize page in the system browser (PKCE + state captured first). */
    private fun startOAuthFlow(serverUrl: String) {
        val url = serverUrl.trim().trimEnd('/')
        if (url.isEmpty()) return
        val pending = OAuthSession.begin(url)
        val oauth = OAuth(serverUrl = { url })
        oauthUi.value = OAuthUi.InProgress
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(oauth.authorizeUrl(pending.challenge, pending.state))))
    }

    /**
     * Handles the `sender://oauth` deep-link callback: state check -> token exchange -> bind.
     * Returns true when the intent was an OAuth callback (consumed or already processing).
     */
    private fun handleOAuthIntent(intent: Intent): Boolean {
        val data = intent.data ?: return false
        if (data.scheme != "sender" || data.host != "oauth") return false
        if (oauthProcessing) return true
        oauthProcessing = true
        setIntent(Intent(intent).apply { this.data = null })

        val app = applicationContext as SenderApp
        when (val callback = OAuthCallback.parse(data.toString())) {
            is OAuthCallback.Callback.Code -> {
                val verifier = OAuthSession.consumeVerifier(callback.state)
                if (verifier == null) {
                    oauthUi.value = OAuthUi.Failed("授权状态不匹配，请重新绑定")
                    oauthProcessing = false
                } else {
                    val server = OAuthSession.consumeServer() ?: app.settings.serverUrl.trim().trimEnd('/')
                    val oauth = OAuth(serverUrl = { server })
                    lifecycleScope.launch {
                        val token = oauth.exchangeToken(callback.code, verifier)
                        if (token == null) {
                            oauthUi.value = OAuthUi.Failed("授权码无效或已过期，请重新绑定")
                            oauthProcessing = false
                            return@launch
                        }
                        // 设备注册只发生在首次上报；新手机可能从未注册，绑定前先幂等注册，
                        // 否则服务端 bind 会因「设备不存在」失败。
                        when (oauth.register(app.identity.deviceId, app.identity.secret, app.identity.deviceName)) {
                            RegisterResult.OK -> app.settings.registered = true
                            RegisterResult.DISABLED -> {
                                oauthUi.value = OAuthUi.Failed("设备注册被服务器拒绝：请在服务端设置 ALLOW_REGISTRATION=true 并重启，再重新绑定")
                                oauthProcessing = false
                                return@launch
                            }
                            RegisterResult.FAILED -> {
                                oauthUi.value = OAuthUi.Failed("设备注册失败，请检查网络与服务器地址后重试")
                                oauthProcessing = false
                                return@launch
                            }
                        }
                        when (val result = oauth.bind(token, app.identity.deviceId, app.identity.secret)) {
                            is BindResult.Ok -> {
                                app.settings.boundUsername = result.username
                                oauthUi.value = OAuthUi.Success(result.username)
                            }
                            is BindResult.Err -> oauthUi.value = OAuthUi.Failed(result.reason)
                        }
                        oauthProcessing = false
                    }
                }
            }

            is OAuthCallback.Callback.Error -> {
                oauthUi.value = if (callback.error == "access_denied") {
                    OAuthUi.Canceled
                } else {
                    OAuthUi.Failed("授权失败：${callback.error}")
                }
                oauthProcessing = false
            }
        }
        return true
    }
}
