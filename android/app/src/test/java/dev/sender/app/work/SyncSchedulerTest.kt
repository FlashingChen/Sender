package dev.sender.app.work

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SyncSchedulerTest {

    @Test
    fun withinFiveMinutes_doesNotTrigger() {
        assertFalse(SyncScheduler.shouldTrigger(nowMs = 10_000_000, lastAttemptMs = 9_700_001))
    }

    @Test
    fun atExactlyFiveMinutes_triggers() {
        assertTrue(SyncScheduler.shouldTrigger(nowMs = 10_000_000, lastAttemptMs = 9_700_000))
    }

    @Test
    fun neverAttempted_triggersImmediately() {
        assertTrue(SyncScheduler.shouldTrigger(nowMs = 10_000_000, lastAttemptMs = 0))
    }
}
