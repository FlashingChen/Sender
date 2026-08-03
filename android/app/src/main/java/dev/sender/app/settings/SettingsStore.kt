package dev.sender.app.settings

import android.content.Context

class SettingsStore(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("sender_settings", Context.MODE_PRIVATE)

    var serverUrl: String
        get() = prefs.getString(KEY_SERVER_URL, DEFAULT_SERVER_URL)!!
        set(value) = prefs.edit().putString(KEY_SERVER_URL, value).apply()

    var registered: Boolean
        get() = prefs.getBoolean(KEY_REGISTERED, false)
        set(value) = prefs.edit().putBoolean(KEY_REGISTERED, value).apply()

    /** Gate for "trigger on new message": at most once per 5 minutes. */
    var lastSyncAttemptMs: Long
        get() = prefs.getLong(KEY_LAST_SYNC, 0L)
        set(value) = prefs.edit().putLong(KEY_LAST_SYNC, value).apply()

    /** User confirmed the WeChat "show message details" hint. */
    var wechatHintDone: Boolean
        get() = prefs.getBoolean(KEY_WECHAT_HINT, false)
        set(value) = prefs.edit().putBoolean(KEY_WECHAT_HINT, value).apply()

    /** Account username bound via OAuth (null = not bound yet). */
    var boundUsername: String?
        get() = prefs.getString(KEY_BOUND_USERNAME, null)
        set(value) = prefs.edit().putString(KEY_BOUND_USERNAME, value).apply()

    companion object {
        const val DEFAULT_SERVER_URL = "http://10.0.2.2:8080"
        private const val KEY_SERVER_URL = "server_url"
        private const val KEY_REGISTERED = "registered"
        private const val KEY_LAST_SYNC = "last_sync_attempt_ms"
        private const val KEY_WECHAT_HINT = "wechat_hint_done"
        private const val KEY_BOUND_USERNAME = "bound_username"
    }
}
