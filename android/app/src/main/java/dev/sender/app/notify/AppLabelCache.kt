package dev.sender.app.notify

import android.content.Context
import android.content.pm.PackageManager
import java.util.concurrent.ConcurrentHashMap

/** Package label resolver via PackageManager, cached in memory (contract requirement). */
class AppLabelCache(context: Context) {

    private val pm = context.applicationContext.packageManager
    private val cache = ConcurrentHashMap<String, String>()

    fun label(packageName: String): String =
        cache.getOrPut(packageName) {
            try {
                val appInfo = pm.getApplicationInfo(packageName, 0)
                pm.getApplicationLabel(appInfo).toString()
            } catch (_: PackageManager.NameNotFoundException) {
                packageName
            }
        }
}
