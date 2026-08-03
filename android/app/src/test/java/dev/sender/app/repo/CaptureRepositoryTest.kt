package dev.sender.app.repo

import android.app.Application
import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import dev.sender.app.data.AppDatabase
import dev.sender.app.notify.AppLabelCache
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Real Room + real SQLite on the JVM (Robolectric): dedup and toggle filtering. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
class CaptureRepositoryTest {

    private lateinit var db: AppDatabase
    private lateinit var repo: CaptureRepository

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java).build()
        repo = CaptureRepository(db, AppLabelCache(context))
    }

    @After
    fun tearDown() {
        db.close()
    }

    private fun message(clientMsgId: String, postTimeMs: Long = 1780000000123L) =
        repo.buildMessage(
            packageName = "com.tencent.mm",
            notificationKey = "notif_key",
            postTimeMs = postTimeMs,
            title = "张三",
            text = "今晚吃饭吗",
        ).copy(clientMsgId = clientMsgId)

    /** Dedup: same client_msg_id is stored only once. */
    @Test
    fun sameClientMsgId_storedOnlyOnce() = runBlocking {
        val m = message("com.tencent.mm:notif_key:1780000000123")
        assertTrue(repo.insertIfEnabled(m))
        assertFalse(repo.insertIfEnabled(m.copy(id = 0)))
        assertEquals(1, db.capturedDao().count())
        assertEquals(1, db.capturedDao().countByClientMsgId("com.tencent.mm:notif_key:1780000000123"))
    }

    /** Notification update carries a new postTime -> new client_msg_id -> new row (content capturable). */
    @Test
    fun sameKeyNewPostTime_storedAsNewRow() = runBlocking {
        assertTrue(repo.insertIfEnabled(message("com.tencent.mm:notif_key:1780000000123")))
        assertTrue(repo.insertIfEnabled(message("com.tencent.mm:notif_key:1780000000999", postTimeMs = 1780000000999L)))
        assertEquals(2, db.capturedDao().count())
    }

    /** Toggle filter: a disabled app's message is blocked before insert. */
    @Test
    fun appToggledOff_insertBlockedBeforeStorage() = runBlocking {
        repo.setEnabled("com.tencent.mm", enabled = false)
        assertFalse(repo.insertIfEnabled(message("com.tencent.mm:notif_key:1780000000123")))
        assertEquals(0, db.capturedDao().count())
    }

    /** Default state (no toggle row) is enabled. */
    @Test
    fun defaultState_noToggleRow_enabled() = runBlocking {
        assertTrue(repo.isEnabled("com.tencent.mm"))
        assertTrue(repo.insertIfEnabled(message("com.tencent.mm:notif_key:1780000000123")))
        assertEquals(1, db.capturedDao().count())
    }

    /** Re-enabling after off restores capture. */
    @Test
    fun reEnabled_afterOff_insertWorksAgain() = runBlocking {
        repo.setEnabled("com.tencent.mm", enabled = false)
        repo.setEnabled("com.tencent.mm", enabled = true)
        assertTrue(repo.insertIfEnabled(message("com.tencent.mm:notif_key:1780000000123")))
        assertEquals(1, db.capturedDao().count())
    }
}
