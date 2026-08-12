package dev.sender.app.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.core.graphics.drawable.toBitmap
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import dev.sender.app.SenderApp
import java.time.LocalDate
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Screen 1: per-app list — icon, name, today's count, capture switch. Data all from local DB. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppListScreen(
    onOpenApp: (packageName: String, appName: String) -> Unit,
    onOpenSettings: () -> Unit,
) {
    val context = LocalContext.current
    val app = context.applicationContext as SenderApp
    val scope = rememberCoroutineScope()
    var today by remember { mutableStateOf(LocalDate.now().toString()) }
    // Refresh the "today" bucket when the app resumes (e.g. across midnight).
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                today = LocalDate.now().toString()
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    val summaries by app.database.capturedDao().appSummaries(today).collectAsState(initial = emptyList())
    val pm = context.packageManager

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("已采集消息") },
                actions = {
                    IconButton(onClick = onOpenSettings) {
                        Icon(Icons.Filled.Settings, contentDescription = "设置")
                    }
                },
            )
        },
    ) { padding ->
        if (summaries.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text("还没有采集到消息。请先在引导页开启通知使用权。")
            }
        } else {
            LazyColumn(Modifier.fillMaxSize().padding(padding)) {
                items(summaries, key = { it.app }) { summary ->
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onOpenApp(summary.app, summary.appName) }
                            .padding(horizontal = 16.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        val icon = rememberAppIcon(pm, summary.app)
                        if (icon != null) {
                            Image(icon, contentDescription = null, modifier = Modifier.size(40.dp))
                        } else {
                            Box(Modifier.size(40.dp), contentAlignment = Alignment.Center) {
                                Text(summary.appName.take(1), style = MaterialTheme.typography.titleMedium)
                            }
                        }
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(summary.appName, style = MaterialTheme.typography.bodyLarge)
                            Text(
                                "今日 ${summary.todayCount} 条 · 共 ${summary.totalCount} 条",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Switch(
                            checked = summary.enabled,
                            onCheckedChange = { enabled ->
                                scope.launch { app.captureRepository.setEnabled(summary.app, enabled) }
                            },
                        )
                    }
                    HorizontalDivider()
                }
            }
        }
    }
}

@Composable
private fun rememberAppIcon(pm: android.content.pm.PackageManager, packageName: String): ImageBitmap? {
    val icon = produceState<ImageBitmap?>(initialValue = null, packageName) {
        value = withContext(Dispatchers.IO) {
            runCatching {
                pm.getApplicationIcon(packageName).toBitmap(width = 80, height = 80).asImageBitmap()
            }.getOrNull()
        }
    }
    return icon.value
}
