package dev.sender.app.ui

import android.Manifest
import android.content.Intent
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import dev.sender.app.SenderApp

/** Three onboarding steps: notification access -> POST_NOTIFICATIONS -> WeChat details hint. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OnboardingScreen(onDone: () -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as SenderApp
    var steps by remember { mutableStateOf(OnboardingState.missingSteps(context, app.settings)) }
    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { steps = OnboardingState.missingSteps(context, app.settings) }

    fun recheck() {
        steps = OnboardingState.missingSteps(context, app.settings)
    }

    Scaffold(topBar = { TopAppBar(title = { Text("引导") }) }) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("三步开启采集", style = MaterialTheme.typography.headlineSmall)
            StepCard(
                done = OnboardingStep.NOTIFICATION_ACCESS !in steps,
                title = "开启通知使用权",
                description = "通知使用权 → 打开「通知采集」。采集依赖系统绑定本服务。",
                actionLabel = "去开启",
            ) {
                context.startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS))
                recheck()
            }
            StepCard(
                done = OnboardingStep.POST_NOTIFICATIONS !in steps,
                title = "通知权限（Android 13+）",
                description = "允许应用发送通知。",
                actionLabel = "去授权",
            ) {
                permissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
            StepCard(
                done = OnboardingStep.WECHAT_DETAILS !in steps,
                title = "微信「显示消息详情」",
                description = "微信 → 我 → 设置 → 新消息通知 → 打开「通知显示消息详情」。\n否则只能采到「你收到了一条消息」，采不到发送者和内容。",
                actionLabel = "我已开启",
            ) {
                app.settings.wechatHintDone = true
                recheck()
            }
            if (steps.isEmpty()) {
                Button(onClick = onDone, modifier = Modifier.fillMaxWidth()) {
                    Text("开始使用")
                }
            }
        }
    }
}

@Composable
private fun StepCard(
    done: Boolean,
    title: String,
    description: String,
    actionLabel: String,
    onAction: () -> Unit,
) {
    Card(Modifier.fillMaxWidth()) {
        Column(
            Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    title,
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.weight(1f),
                )
                if (done) {
                    Text("已完成", color = MaterialTheme.colorScheme.primary)
                } else {
                    Button(onClick = onAction) { Text(actionLabel) }
                }
            }
            Text(description, style = MaterialTheme.typography.bodySmall)
        }
    }
}
