package dev.sender.app.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import dev.sender.app.SenderApp
import dev.sender.app.settings.SettingsStore
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** OAuth bind flow feedback shown under the bind button. */
sealed interface OAuthUi {
    data object Idle : OAuthUi
    data object InProgress : OAuthUi
    data class Success(val username: String) : OAuthUi
    data object Canceled : OAuthUi
    data class Failed(val detail: String) : OAuthUi
}

/** Server address (editable, default per contract) + test-connection button hitting /healthz. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    onBack: () -> Unit,
    oauthUi: OAuthUi = OAuthUi.Idle,
    onBindAccount: (String) -> Unit = {},
) {
    val context = LocalContext.current
    val app = context.applicationContext as SenderApp
    val scope = rememberCoroutineScope()
    var url by remember { mutableStateOf(app.settings.serverUrl) }
    var testing by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("设置") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("服务端", style = MaterialTheme.typography.titleMedium)
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("服务端地址") },
                placeholder = { Text(SettingsStore.DEFAULT_SERVER_URL) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(onClick = {
                    app.settings.serverUrl = url.trim().trimEnd('/')
                    result = "已保存"
                }) {
                    Text("保存")
                }
                OutlinedButton(
                    onClick = {
                        testing = true
                        result = null
                        scope.launch {
                            result = if (app.api.health(url.trim().trimEnd('/'))) {
                                "连接成功"
                            } else {
                                "连接失败（请检查地址与 /healthz）"
                            }
                            testing = false
                        }
                    },
                    enabled = !testing,
                ) {
                    Text(if (testing) "测试中…" else "测试连接")
                }
            }
            result?.let {
                Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Button(
                onClick = { onBindAccount(url) },
                enabled = url.isNotBlank() && oauthUi !is OAuthUi.InProgress,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("登录并绑定账号")
            }
            app.settings.boundUsername?.let { username ->
                Text("已绑定：$username", color = MaterialTheme.colorScheme.primary)
            }
            when (oauthUi) {
                OAuthUi.Idle -> {}
                OAuthUi.InProgress -> Text("绑定中…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                is OAuthUi.Success -> Text(
                    "已绑定：${oauthUi.username}",
                    color = MaterialTheme.colorScheme.primary,
                )
                OAuthUi.Canceled -> Text("已取消授权", color = MaterialTheme.colorScheme.onSurfaceVariant)
                is OAuthUi.Failed -> Text(oauthUi.detail, color = MaterialTheme.colorScheme.error)
            }
            HorizontalDivider()
            Text("设备 ID：${app.identity.deviceId}", style = MaterialTheme.typography.bodySmall)
            Text(
                "注册状态：${if (app.settings.registered) "已注册" else "未注册（首次上报时自动注册）"}",
                style = MaterialTheme.typography.bodySmall,
            )
            HorizontalDivider()
            Text("设备信息", style = MaterialTheme.typography.titleMedium)
            Text(
                "在网页端绑定本设备需要用到以下两个值",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            var copiedLabel by remember { mutableStateOf<String?>(null) }
            LaunchedEffect(copiedLabel) {
                if (copiedLabel != null) {
                    delay(1500)
                    copiedLabel = null
                }
            }
            DeviceInfoRow(
                label = "设备 ID",
                value = app.identity.deviceId,
                copied = copiedLabel == "deviceId",
                onCopy = {
                    clipboard.setPrimaryClip(ClipData.newPlainText("device_id", app.identity.deviceId))
                    copiedLabel = "deviceId"
                },
            )
            DeviceInfoRow(
                label = "设备密钥",
                value = app.identity.secret,
                copied = copiedLabel == "secret",
                onCopy = {
                    clipboard.setPrimaryClip(ClipData.newPlainText("secret", app.identity.secret))
                    copiedLabel = "secret"
                },
            )
        }
    }
}

/** Read-only label+value row with a copy button (system ClipboardManager). */
@Composable
private fun DeviceInfoRow(
    label: String,
    value: String,
    copied: Boolean,
    onCopy: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                label,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(value, style = MaterialTheme.typography.bodyMedium)
        }
        OutlinedButton(onClick = onCopy) {
            Text(if (copied) "已复制" else "复制")
        }
    }
}
