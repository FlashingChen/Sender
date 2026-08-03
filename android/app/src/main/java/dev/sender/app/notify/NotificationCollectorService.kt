package dev.sender.app.notify

import android.app.Notification
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import dev.sender.app.SenderApp
import dev.sender.app.work.SyncScheduler
import kotlinx.coroutines.launch

/**
 * System-bound listener (no foreground service needed — the system binds us).
 * Extracts EXTRA_TITLE (sender/group), EXTRA_TEXT (content) and postTime;
 * the toggle filter and dedup happen before/at insert.
 */
class NotificationCollectorService : NotificationListenerService() {

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        val app = application as SenderApp
        val extras = sbn.notification.extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT)?.toString()
        if (title.isNullOrBlank() && text.isNullOrBlank()) return

        val message = app.captureRepository.buildMessage(
            packageName = sbn.packageName,
            notificationKey = sbn.key,
            postTimeMs = sbn.postTime,
            title = title,
            text = text,
        )
        app.appScope.launch {
            val stored = app.captureRepository.insertIfEnabled(message)
            if (stored) SyncScheduler.maybeTrigger(this@NotificationCollectorService)
        }
    }
}
