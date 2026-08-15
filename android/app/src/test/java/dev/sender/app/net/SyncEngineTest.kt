package dev.sender.app.net

import dev.sender.app.data.CapturedMessage
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SyncEngineTest {

    private class FakeRepository : SyncRepository {
        val synced = LinkedHashMap<String, Boolean>()

        fun add(clientMsgId: String) {
            synced[clientMsgId] = false
        }

        fun pendingCount(): Int = synced.values.count { !it }

        override suspend fun pending(limit: Int): List<CapturedMessage> =
            synced.filterValues { !it }.keys.take(limit).map(::message)

        override suspend fun markSynced(clientMsgIds: List<String>) {
            clientMsgIds.forEach { synced[it] = true }
        }

        private fun message(id: String) = CapturedMessage(
            clientMsgId = id,
            app = "com.tencent.mm",
            appName = "微信",
            chat = "张三",
            sender = "张三",
            content = "内容",
            ts = 1780000000L,
            day = "2026-08-03",
        )
    }

    private class FakeApi : Api {
        var registerCalls = 0
        var registerResult = RegisterResult.OK
        var uploadCalls = 0
        var uploadResult = UploadResult.SUCCESS
        val uploadBodies = mutableListOf<String>()

        override suspend fun register(deviceId: String, secret: String, deviceName: String): RegisterResult {
            registerCalls++
            return registerResult
        }

        override suspend fun upload(deviceId: String, secret: String, body: String): UploadResult {
            uploadCalls++
            uploadBodies += body
            return uploadResult
        }

        override suspend fun health(baseUrl: String): Boolean = true
    }

    private var registered = false
    private val repo = FakeRepository()
    private val api = FakeApi()

    private fun engine() = SyncEngine(
        deviceId = "11111111-1111-1111-1111-111111111111",
        secret = "abcdef0123456789abcdef0123456789",
        deviceName = "Pixel 8",
        isRegistered = { registered },
        markRegistered = { registered = true },
        repository = repo,
        api = api,
    )

    /** Nothing uploads before registration succeeds. */
    @Test
    fun notRegistered_registersBeforeUpload() = runBlocking {
        repo.add("m1")
        repo.add("m2")
        val result = engine().sync()
        assertEquals(SyncResult.UPLOADED, result)
        assertEquals(1, api.registerCalls)
        assertEquals(1, api.uploadCalls)
        assertTrue(registered)
        assertEquals(0, repo.pendingCount())
    }

    /** Register failure blocks upload entirely and leaves rows synced=0. */
    @Test
    fun registerFailure_blocksUpload_keepsSyncedFalse() = runBlocking {
        repo.add("m1")
        api.registerResult = RegisterResult.FAILED
        val result = engine().sync()
        assertEquals(SyncResult.REGISTER_FAILED, result)
        assertEquals(0, api.uploadCalls)
        assertFalse(registered)
        assertEquals(1, repo.pendingCount())
    }

    /** Upload failure keeps synced=0 for retry. */
    @Test
    fun uploadFailure_keepsSyncedFalse() = runBlocking {
        registered = true
        repo.add("m1")
        api.uploadResult = UploadResult.FAILED
        val result = engine().sync()
        assertEquals(SyncResult.UPLOAD_FAILED, result)
        assertEquals(1, repo.pendingCount())
    }

    /** 401/403 resets the registered flag so the next sync re-registers. */
    @Test
    fun authFailure_resetsRegisteredFlag() = runBlocking {
        registered = true
        var resetCalls = 0
        repo.add("m1")
        api.uploadResult = UploadResult.AUTH_FAILED
        val engine = SyncEngine(
            deviceId = "11111111-1111-1111-1111-111111111111",
            secret = "abcdef0123456789abcdef0123456789",
            deviceName = "Pixel 8",
            isRegistered = { registered },
            markRegistered = { registered = true },
            resetRegistered = { resetCalls++; registered = false },
            repository = repo,
            api = api,
        )
        assertEquals(SyncResult.UPLOAD_FAILED, engine.sync())
        assertFalse(registered)
        assertEquals(1, resetCalls)
        assertEquals(1, repo.pendingCount())
    }

    /** 2xx upload marks the batch synced. */
    @Test
    fun uploadSuccess_marksSyncedTrue() = runBlocking {
        registered = true
        repo.add("m1")
        assertEquals(SyncResult.UPLOADED, engine().sync())
        assertEquals(0, repo.pendingCount())
        assertTrue(repo.synced["m1"]!!)
    }

    @Test
    fun nothingPending_returnsWithoutUpload() = runBlocking {
        registered = true
        assertEquals(SyncResult.NOTHING_PENDING, engine().sync())
        assertEquals(0, api.uploadCalls)
    }

    /** Contract: single batch <= 500; 700 pending -> 2 uploads of 500 + 200. */
    @Test
    fun over500Pending_chunksIntoBatchesOfAtMost500() = runBlocking {
        registered = true
        repeat(700) { repo.add("m$it") }
        assertEquals(SyncResult.UPLOADED, engine().sync())
        assertEquals(2, api.uploadCalls)
        val sizes = api.uploadBodies.map { Regex("\"client_msg_id\"").findAll(it).count() }
        assertEquals(listOf(500, 200), sizes)
        assertEquals(0, repo.pendingCount())
    }

    /** Registration is done once, not per upload. */
    @Test
    fun alreadyRegistered_skipsRegisterOnNextSync() = runBlocking {
        repo.add("m1")
        engine().sync()
        assertEquals(1, api.registerCalls)
        repo.add("m2")
        engine().sync()
        assertEquals(1, api.registerCalls)
    }
}
