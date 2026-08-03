package dev.sender.app.ui

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.provider.Settings
import androidx.core.content.ContextCompat
import dev.sender.app.notify.NotificationCollectorService
import dev.sender.app.settings.SettingsStore

enum class OnboardingStep { NOTIFICATION_ACCESS, POST_NOTIFICATIONS, WECHAT_DETAILS }

object OnboardingState {

    /** The system "enabled_notification_listeners" setting contains our component. */
    fun notificationListenerEnabled(context: Context): Boolean {
        val component = ComponentName(context, NotificationCollectorService::class.java).flattenToString()
        val enabled = Settings.Secure.getString(context.contentResolver, "enabled_notification_listeners")
            ?: return false
        return enabled.split(":").any { it == component }
    }

    /** Android 13+ runtime permission; below 13 it is always granted. */
    fun postNotificationsGranted(context: Context): Boolean =
        Build.VERSION.SDK_INT < 33 ||
            ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED

    /** Steps still missing; the WeChat hint can only be confirmed by the user. */
    fun missingSteps(context: Context, settings: SettingsStore): List<OnboardingStep> = buildList {
        if (!notificationListenerEnabled(context)) add(OnboardingStep.NOTIFICATION_ACCESS)
        if (!postNotificationsGranted(context)) add(OnboardingStep.POST_NOTIFICATIONS)
        if (!settings.wechatHintDone) add(OnboardingStep.WECHAT_DETAILS)
    }
}
