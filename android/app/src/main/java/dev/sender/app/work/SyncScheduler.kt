package dev.sender.app.work

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import dev.sender.app.SenderApp
import java.util.concurrent.TimeUnit

object SyncScheduler {

    const val ONE_TIME_WORK = "sender-upload-once"
    const val PERIODIC_WORK = "sender-upload-periodic"

    /** Min interval between message-triggered uploads. */
    const val MIN_TRIGGER_INTERVAL_MS = 5 * 60_000L

    private const val FALLBACK_INTERVAL_MINUTES = 30L

    /** Pure trigger rule: fire when >= 5 minutes since the last attempt (or never attempted). */
    fun shouldTrigger(nowMs: Long, lastAttemptMs: Long): Boolean =
        nowMs - lastAttemptMs >= MIN_TRIGGER_INTERVAL_MS

    /** Called after a new message is stored: upload promptly, at most once per 5 minutes. */
    fun maybeTrigger(context: Context) {
        val app = context.applicationContext as SenderApp
        val now = System.currentTimeMillis()
        if (!shouldTrigger(now, app.settings.lastSyncAttemptMs)) return
        app.settings.lastSyncAttemptMs = now
        enqueueOneTime(context)
    }

    private fun enqueueOneTime(context: Context) {
        val request = OneTimeWorkRequestBuilder<UploadWorker>()
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 10, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context)
            .enqueueUniqueWork(ONE_TIME_WORK, ExistingWorkPolicy.KEEP, request)
    }

    /** 30-minute fallback sweep; idempotent (KEEP). */
    fun schedulePeriodic(context: Context) {
        val request = PeriodicWorkRequestBuilder<UploadWorker>(FALLBACK_INTERVAL_MINUTES, TimeUnit.MINUTES)
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 10, TimeUnit.SECONDS)
            .build()
        WorkManager.getInstance(context)
            .enqueueUniquePeriodicWork(PERIODIC_WORK, ExistingPeriodicWorkPolicy.KEEP, request)
    }
}
