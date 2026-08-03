package dev.sender.app.net

/**
 * Hand-rolled JSON construction for the fixed contract payload shapes
 * (no JSON library dependency; exact field control, no extra fields).
 */
object PayloadBuilder {

    /** Contract: single batch <= 500 messages. */
    const val MAX_BATCH = 500

    /** Chunk messages into contract-sized batches. */
    fun buildBatches(messages: List<OutgoingMessage>): List<String> =
        messages.chunked(MAX_BATCH).map(::buildBatch)

    fun buildBatch(messages: List<OutgoingMessage>): String {
        val body = messages.joinToString(",", prefix = "[", postfix = "]") { it.toJson() }
        return """{"messages":$body}"""
    }

    private fun OutgoingMessage.toJson(): String =
        """{"client_msg_id":${jsonString(clientMsgId)},"app":${jsonString(app)},""" +
            """"app_name":${jsonString(appName)},"chat":${jsonString(chat)},"sender":${jsonString(sender)},""" +
            """"content":${jsonString(content)},"ts":$ts}"""

    /** RFC 8259 JSON string literal with full escaping. */
    fun jsonString(value: String): String {
        val sb = StringBuilder(value.length + 8).append('"')
        for (c in value) {
            when (c) {
                '"' -> sb.append("\\\"")
                '\\' -> sb.append("\\\\")
                '\b' -> sb.append("\\b")
                '\u000C' -> sb.append("\\f")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                '\t' -> sb.append("\\t")
                else -> if (c < ' ') {
                    sb.append("\\u").append(String.format("%04x", c.code))
                } else {
                    sb.append(c)
                }
            }
        }
        return sb.append('"').toString()
    }
}
