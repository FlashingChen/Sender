package dev.sender.app.net

/**
 * Maps raw notification fields to the contract shape.
 * chat/sender both come from the notification title (single chat: identical);
 * ts is Unix seconds derived from postTime millis.
 */
object NotificationMapper {

    fun toOutgoing(
        clientMsgId: String,
        app: String,
        appName: String,
        title: String?,
        text: String?,
        postTimeMs: Long,
    ): OutgoingMessage = OutgoingMessage(
        clientMsgId = clientMsgId,
        app = app,
        appName = appName,
        chat = title ?: "",
        sender = title ?: "",
        content = text ?: "",
        ts = postTimeMs / 1000,
    )
}
