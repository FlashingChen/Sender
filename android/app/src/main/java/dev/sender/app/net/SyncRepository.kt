package dev.sender.app.net

import dev.sender.app.data.CapturedMessage

/** Boundary over the local store, so the upload flow is unit-testable without Room. */
interface SyncRepository {

    suspend fun pending(limit: Int): List<CapturedMessage>

    suspend fun markSynced(clientMsgIds: List<String>)
}

/** Room-backed implementation of [SyncRepository]. */
class RoomSyncRepository(private val db: dev.sender.app.data.AppDatabase) : SyncRepository {

    override suspend fun pending(limit: Int): List<CapturedMessage> =
        db.capturedDao().pending(limit)

    override suspend fun markSynced(clientMsgIds: List<String>) =
        db.capturedDao().markSynced(clientMsgIds)
}
