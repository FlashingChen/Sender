package dev.sender.app.net

import org.junit.Assert.assertEquals
import org.junit.Test

class NotificationMapperTest {

    /** Contract sample: postTime 1780000000123 ms -> ts 1780000000 s. */
    @Test
    fun postTimeMillis_convertedToUnixSeconds() {
        val out = NotificationMapper.toOutgoing(
            clientMsgId = "com.tencent.mm:notif_key:1780000000123",
            app = "com.tencent.mm",
            appName = "微信",
            title = "张三",
            text = "今晚吃饭吗",
            postTimeMs = 1780000000123L,
        )
        assertEquals(1780000000L, out.ts)
    }

    /** chat/sender both come from the title; single chat they are identical. */
    @Test
    fun chatAndSender_bothComeFromTitle_singleChatIdentical() {
        val out = NotificationMapper.toOutgoing("id", "com.tencent.mm", "微信", "张三", "内容", 1000L)
        assertEquals("张三", out.chat)
        assertEquals("张三", out.sender)
    }

    @Test
    fun missingTitleOrText_becomesEmptyString() {
        val out = NotificationMapper.toOutgoing("id", "com.tencent.mm", "微信", null, null, 1000L)
        assertEquals("", out.chat)
        assertEquals("", out.sender)
        assertEquals("", out.content)
    }
}
