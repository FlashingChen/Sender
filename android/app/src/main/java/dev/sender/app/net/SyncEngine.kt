package dev.sender.app.net

import dev.sender.app.data.CapturedMessage

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
 */
class SyncEngine(
    private val deviceId: String,
    private val secret: String,
    private val deviceName: String,
    private val isRegistered: () -> Boolean,
    private val markRegistered: () -> Unit,
    private val repository: SyncRepository,
    private val api: Api,
) {

    suspend fun sync(): SyncResult {
        if (!isRegistered()) {
            if (!api.register(deviceId, secret, deviceName)) return SyncResult.REGISTER_FAILED
            markRegistered()
        }
        var uploadedAny = false
        while (true) {
            val batch = repository.pending(PayloadBuilder.MAX_BATCH)
            if (batch.isEmpty()) {
                return if (uploadedAny) SyncResult.UPLOADED else SyncResult.NOTHING_PENDING
            }
            val body = PayloadBuilder.buildBatch(batch.map { it.toOutgoing() })
            if (!api.upload(deviceId, secret, body)) return SyncResult.UPLOAD_FAILED
            repository.markSynced(batch.map { it.clientMsgId })
            uploadedAny = true
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
