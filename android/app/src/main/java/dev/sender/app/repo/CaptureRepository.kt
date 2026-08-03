package dev.sender.app.repo

import dev.sender.app.data.AppDatabase
import dev.sender.app.data.AppToggle
import dev.sender.app.data.CapturedMessage
import dev.sender.app.net.NotificationMapper
import dev.sender.app.notify.AppLabelCache
import java.time.Instant
import java.time.ZoneId

/**
 * Capture path: toggle filter BEFORE insert, dedup via the unique client_msg_id index.
 */
class CaptureRepository(
    private val db: AppDatabase,
    private val labelCache: AppLabelCache,
) {

    /** Absence of a toggle row = default enabled. */
    suspend fun isEnabled(packageName: String): Boolean =
        db.appToggleDao().enabled(packageName) ?: true

    suspend fun setEnabled(packageName: String, enabled: Boolean) {
        if (enabled) {
            db.appToggleDao().remove(packageName)
        } else {
            db.appToggleDao().upsert(AppToggle(packageName = packageName, enabled = false))
        }
    }

    /**
     * @return true when the row was actually stored (toggle on AND not a duplicate).
     */
    suspend fun insertIfEnabled(message: CapturedMessage): Boolean {
        if (!isEnabled(message.app)) return false
        return db.capturedDao().insert(message) > 0
    }

    /** client_msg_id = "<package>:<notification key>:<postTime ms>". */
    fun buildMessage(
        packageName: String,
        notificationKey: String,
        postTimeMs: Long,
        title: String?,
        text: String?,
    ): CapturedMessage {
        val clientMsgId = "$packageName:$notificationKey:$postTimeMs"
        val outgoing = NotificationMapper.toOutgoing(
            clientMsgId = clientMsgId,
            app = packageName,
            appName = labelCache.label(packageName),
            title = title,
            text = text,
            postTimeMs = postTimeMs,
        )
        val day = Instant.ofEpochMilli(postTimeMs)
            .atZone(ZoneId.systemDefault())
            .toLocalDate()
            .toString()
        return CapturedMessage(
            clientMsgId = outgoing.clientMsgId,
            app = outgoing.app,
            appName = outgoing.appName,
            chat = outgoing.chat,
            sender = outgoing.sender,
            content = outgoing.content,
            ts = outgoing.ts,
            day = day,
        )
    }
}
