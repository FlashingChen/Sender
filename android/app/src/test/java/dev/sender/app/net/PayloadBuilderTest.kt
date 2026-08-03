package dev.sender.app.net

import org.junit.Assert.assertEquals
import org.junit.Test

class PayloadBuilderTest {

    private val contractSample = OutgoingMessage(
        clientMsgId = "com.tencent.mm:notif_key:1780000000123",
        app = "com.tencent.mm",
        appName = "微信",
        chat = "张三",
        sender = "张三",
        content = "今晚吃饭吗",
        ts = 1780000000L,
    )

    /** Contract sample hardcoded: the built payload must match it byte for byte. */
    @Test
    fun buildBatch_matchesContractSample_exactly() {
        val expected = """{"messages":[{"client_msg_id":"com.tencent.mm:notif_key:1780000000123","app":"com.tencent.mm","app_name":"微信","chat":"张三","sender":"张三","content":"今晚吃饭吗","ts":1780000000}]}"""
        assertEquals(expected, PayloadBuilder.buildBatch(listOf(contractSample)))
    }

    /** No fields outside the contract may appear. */
    @Test
    fun buildBatch_containsNoFieldsOutsideContract() {
        val json = PayloadBuilder.buildBatch(listOf(contractSample))
        val keys = Regex("\"([a-zA-Z_]+)\":" ).findAll(json).map { it.groupValues[1] }.toList()
        assertEquals(
            listOf("messages", "client_msg_id", "app", "app_name", "chat", "sender", "content", "ts"),
            keys,
        )
    }

    /** Contract: single batch <= 500. */
    @Test
    fun buildBatches_chunksAt500() {
        val messages = (1..1001).map { contractSample.copy(clientMsgId = "id-$it") }
        val batches = PayloadBuilder.buildBatches(messages)
        assertEquals(listOf(500, 500, 1), batches.map { countClientMsgIds(it) })
    }

    @Test
    fun jsonString_escapesQuotesBackslashAndControlChars() {
        val escaped = PayloadBuilder.jsonString("a\"b\\c\nd\u0001")
        assertEquals("\"a\\\"b\\\\c\\nd\\u0001\"", escaped)
    }

    private fun countClientMsgIds(json: String): Int =
        Regex("\"client_msg_id\"").findAll(json).count()
}
