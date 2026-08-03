package dev.sender.app.net

/**
 * One message in the upload payload — exactly the contract fields, nothing else.
 */
data class OutgoingMessage(
    val clientMsgId: String,
    val app: String,
    val appName: String,
    val chat: String,
    val sender: String,
    val content: String,
    val ts: Long,
)
