package dev.sender.app.notify

import android.app.Notification
import android.content.ComponentName
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

    /** One-shot: after the first connect with no active notifications, ask the
     *  system to rebind so notifications already on screen are re-delivered
     *  (several OEMs do not replay them otherwise after boot/access grant). */
    private var rebindRequested = false

    override fun onListenerConnected() {
        if (!rebindRequested && activeNotifications.isEmpty()) {
            rebindRequested = true
            runCatching { requestRebind(ComponentName(this, NotificationCollectorService::class.java)) }
        }
    }

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
