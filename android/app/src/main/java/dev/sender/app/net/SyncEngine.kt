package dev.sender.app.net

import dev.sender.app.data.CapturedMessage
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

enum class SyncResult {
    /** At least one batch uploaded; all drained rows marked synced. */
    UPLOADED,

    /** Nothing pending (after registration). */
    NOTHING_PENDING,

    /** Registration failed; nothing was uploaded. */
    REGISTER_FAILED,

    /** Upload failed; rows stay synced=0 for retry. */
    UPLOAD_FAILED,
}

/**
 * Upload pipeline:
 * 1. Register first — nothing uploads before registration succeeds (200).
 * 2. Drain synced=0 rows in contract batches of <=500.
 * 3. Only a 2xx response marks a batch synced; failures leave synced=0 (server is idempotent, resend is harmless).
 * 4. A 401/403 upload response means the server no longer recognizes this
 *    device (DB reset or revocation): reset the registered flag so the next
 *    sync re-registers instead of failing forever.
 *
 * A mutex serializes concurrent syncs (message-triggered work and the
 * periodic sweep can otherwise drain the same batch twice).
 */
class SyncEngine(
    private val deviceId: String,
    private val secret: String,
    private val deviceName: String,
    private val isRegistered: () -> Boolean,
    private val markRegistered: () -> Unit,
    private val resetRegistered: () -> Unit = {},
    private val repository: SyncRepository,
    private val api: Api,
) {

    private val syncMutex = Mutex()

    suspend fun sync(): SyncResult = syncMutex.withLock { syncUnlocked() }

    private suspend fun syncUnlocked(): SyncResult {
        if (!isRegistered()) {
            if (api.register(deviceId, secret, deviceName) != RegisterResult.OK) return SyncResult.REGISTER_FAILED
            markRegistered()
        }
        var uploadedAny = false
        while (true) {
            val batch = repository.pending(PayloadBuilder.MAX_BATCH)
            if (batch.isEmpty()) {
                return if (uploadedAny) SyncResult.UPLOADED else SyncResult.NOTHING_PENDING
            }
            val body = PayloadBuilder.buildBatch(batch.map { it.toOutgoing() })
            when (api.upload(deviceId, secret, body)) {
                UploadResult.SUCCESS -> {
                    repository.markSynced(batch.map { it.clientMsgId })
                    uploadedAny = true
                }
                UploadResult.AUTH_FAILED -> {
                    resetRegistered()
                    return SyncResult.UPLOAD_FAILED
                }
                UploadResult.FAILED -> return SyncResult.UPLOAD_FAILED
            }
        }
    }

    private fun CapturedMessage.toOutgoing(): OutgoingMessage = OutgoingMessage(
        clientMsgId = clientMsgId,
        app = app,
        appName = appName,
        chat = chat,
        sender = sender,
        content = content,
        ts = ts,
    )
}
