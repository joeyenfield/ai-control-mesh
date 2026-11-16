package dev.agentproxy.app

import android.app.Application
import android.content.Context
import android.os.Build
import android.os.Process
import java.io.File
import java.util.Date

/** Keep recent app steps and failures private until the user chooses where to export them. */
object CrashLog {
    private const val FILE = "app-log.txt"
    private const val MAX_CHARS = 128_000

    fun latest(context: Context): File? = File(context.filesDir, FILE).takeIf { it.isFile }

    @Synchronized
    fun record(context: Context, event: String, error: Throwable? = null) {
        val report = buildString {
            appendLine(Date())
            appendLine("AI Control Mesh ${BuildConfig.VERSION_NAME}; Android API ${Build.VERSION.SDK_INT}")
            appendLine(event)
            if (error != null) append(error.stackTraceToString())
        }
        val file = File(context.filesDir, FILE)
        val previous = if (file.isFile) file.readText() else ""
        file.writeText((previous + report + "\n").takeLast(MAX_CHARS))
    }

    fun note(context: Context, event: String) {
        try {
            record(context, event)
        } catch (_: Exception) {
            // Diagnostics cannot interrupt the app.
        }
    }
}

class AgentProxyApp : Application() {
    override fun onCreate() {
        super.onCreate()
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            try {
                CrashLog.record(this, "Uncaught exception on ${thread.name}", error)
            } catch (_: Throwable) {
                // Logging must never hide the original failure.
            }
            if (previous != null) previous.uncaughtException(thread, error)
            else Process.killProcess(Process.myPid())
        }
        CrashLog.note(this, "App started")
    }
}
