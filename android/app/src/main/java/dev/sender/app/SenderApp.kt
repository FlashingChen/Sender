package dev.sender.app

import android.app.Application
import dev.sender.app.data.AppDatabase
import dev.sender.app.net.ApiClient
import dev.sender.app.net.DeviceIdentity
import dev.sender.app.net.RoomSyncRepository
import dev.sender.app.net.SyncEngine
import dev.sender.app.notify.AppLabelCache
import dev.sender.app.repo.CaptureRepository
import dev.sender.app.settings.SettingsStore
import dev.sender.app.work.SyncScheduler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob

class SenderApp : Application() {

    val appScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    val database by lazy { AppDatabase.create(this) }
    val settings by lazy { SettingsStore(this) }
    val identity by lazy { DeviceIdentity(this) }
    val labelCache by lazy { AppLabelCache(this) }
    val captureRepository by lazy { CaptureRepository(database, labelCache) }
    val api by lazy { ApiClient(serverUrl = { settings.serverUrl }) }
    val syncEngine by lazy {
        SyncEngine(
            deviceId = identity.deviceId,
            secret = identity.secret,
            deviceName = identity.deviceName,
            isRegistered = { settings.registered },
            markRegistered = { settings.registered = true },
            resetRegistered = { settings.registered = false },
            repository = RoomSyncRepository(database),
            api = api,
        )
    }

    override fun onCreate() {
        super.onCreate()
        SyncScheduler.schedulePeriodic(this)
    }
}
