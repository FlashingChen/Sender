package dev.sender.app.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import dev.sender.app.SenderApp
import dev.sender.app.data.CapturedMessage
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Screen 2: one app's messages, grouped by day (time / sender / content). Data all from local DB. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MessageListScreen(
    packageName: String,
    appName: String,
    onBack: () -> Unit,
) {
    val context = LocalContext.current
    val app = context.applicationContext as SenderApp
    val messages by app.database.capturedDao().messagesForApp(packageName).collectAsState(initial = emptyList())

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(appName) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { padding ->
        if (messages.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text("该 App 暂无已采集消息")
            }
        } else {
            val grouped = messages.groupBy { it.day }
            LazyColumn(Modifier.fillMaxSize().padding(padding)) {
                grouped.forEach { (day, dayMessages) ->
                    item(key = "day-$day") { DayHeader(day) }
                    items(dayMessages, key = { it.id }) { message -> MessageRow(message) }
                }
            }
        }
    }
}

@Composable
private fun DayHeader(day: String) {
    val label = remember(day) {
        val date = LocalDate.parse(day)
        if (date == LocalDate.now()) {
            "今天"
        } else {
            date.format(DateTimeFormatter.ofPattern("M月d日 EEEE", Locale.CHINESE))
        }
    }
    Text(
        label,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
    )
}

@Composable
private fun MessageRow(message: CapturedMessage) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                message.sender,
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier.weight(1f),
            )
            Text(
                formatTime(message.ts),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (message.content.isNotEmpty()) {
            Text(message.content, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

private fun formatTime(ts: Long): String =
    Instant.ofEpochSecond(ts)
        .atZone(ZoneId.systemDefault())
        .toLocalTime()
        .format(DateTimeFormatter.ofPattern("HH:mm"))
