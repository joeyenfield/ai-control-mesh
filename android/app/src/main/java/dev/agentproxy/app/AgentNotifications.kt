package dev.agentproxy.app

import android.Manifest
import android.app.Activity
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.provider.Settings
import android.webkit.JavascriptInterface

/**
 * The console's agent alerts as Android notifications. A WebView has no Notification API, so the
 * console (components/Attention.tsx) calls this object, `window.AgentProxyNotifications`, instead.
 * It only posts notifications: it reads nothing and holds no credential. Calls from a page other
 * than the console (the issuer's sign-in pages load in the same WebView) are ignored.
 */
class AgentNotifications(
    private val activity: Activity,
    private val serverUrl: String,
    private val serverName: String,
    /** Whether the WebView shows the console now; set on the UI thread, read on the bridge's thread. */
    private val onConsole: () -> Boolean,
    /** Tells the page the permission may have changed. */
    private val changed: () -> Unit,
) {
    private val manager = activity.getSystemService(NotificationManager::class.java)
    private val prefs = activity.getSharedPreferences("notifications", Activity.MODE_PRIVATE)

    init {
        manager?.createNotificationChannel(
            NotificationChannel(CHANNEL, activity.getString(R.string.notify_channel), NotificationManager.IMPORTANCE_HIGH).apply {
                description = activity.getString(R.string.notify_channel_description)
            },
        )
    }

    /** "granted", "denied" or "default", as the Notification API's permission. */
    @JavascriptInterface
    fun permission(): String {
        if (manager?.areNotificationsEnabled() == true) return "granted"
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return "denied"
        val askAgain = !prefs.getBoolean(ASKED, false) || activity.shouldShowRequestPermissionRationale(Manifest.permission.POST_NOTIFICATIONS)
        return if (askAgain) "default" else "denied"
    }

    /** Asks for the permission; the page hears `agentproxy-notifications` once the answer is in. */
    @JavascriptInterface
    fun requestPermission() {
        if (!onConsole()) return
        activity.runOnUiThread {
            if (permission() == "default" && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                activity.requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST)
            } else if (permission() == "denied") {
                // Android won't ask again: the switch is in the app's notification settings. onResume tells the page.
                activity.startActivity(Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, activity.packageName))
            } else {
                changed()
            }
        }
    }

    /** Called from the activity's onRequestPermissionsResult. */
    fun answered(requestCode: Int) {
        if (requestCode != REQUEST) return
        prefs.edit().putBoolean(ASKED, true).apply()
        changed()
    }

    /** Shows one notification; tag replaces an earlier one for the same agent. Tapping opens path in the console. */
    @JavascriptInterface
    fun show(title: String, body: String, tag: String, path: String): Boolean {
        if (!onConsole() || permission() != "granted" || !isConsolePath(path)) return false
        val open = Intent(activity, ConsoleActivity::class.java)
            .putExtra(ConsoleActivity.EXTRA_URL, serverUrl)
            .putExtra(ConsoleActivity.EXTRA_NAME, serverName)
            .putExtra(ConsoleActivity.EXTRA_PATH, path)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        val key = "$serverUrl#$tag"
        // A distinct request code per agent, so each notification keeps its own path.
        val pending = PendingIntent.getActivity(activity, key.hashCode(), open, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val notification = Notification.Builder(activity, CHANNEL)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title.take(200))
            .setContentText(body.take(500))
            .setStyle(Notification.BigTextStyle().bigText(body.take(2000)))
            .setSubText(serverName)
            .setContentIntent(pending)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_MESSAGE)
            .build()
        return try {
            manager?.notify(key, ID, notification)
            true
        } catch (_: SecurityException) {
            false
        }
    }

    companion object {
        const val CHANNEL = "agents"
        const val REQUEST = 41
        private const val ID = 1
        private const val ASKED = "asked"
        const val PERMISSION_EVENT = "agentproxy-notifications"
    }
}

/** A path within the console: absolute, not protocol-relative, nothing that could leave the origin. */
fun isConsolePath(path: String): Boolean =
    path.startsWith("/") && !path.startsWith("//") && !path.contains('\\') && path.length <= 512 && path.none { it.isWhitespace() || it.isISOControl() }
