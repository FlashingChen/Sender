package dev.sender.app.work

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import dev.sender.app.SenderApp
import dev.sender.app.net.SyncResult

/**
 * Runs the upload pipeline; failures return retry() so WorkManager re-runs
 * with exponential backoff (10s, 20s, 40s, ...). The 30-minute periodic
 * fallback keeps draining anything left behind.
 */
class UploadWorker(appContext: Context, params: WorkerParameters) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        val app = applicationContext as SenderApp
        return when (app.syncEngine.sync()) {
            SyncResult.UPLOADED, SyncResult.NOTHING_PENDING -> Result.success()
            SyncResult.REGISTER_FAILED, SyncResult.UPLOAD_FAILED -> Result.retry()
        }
    }
}
