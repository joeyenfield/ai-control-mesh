package dev.agentproxy.app

import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlertDialog
import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.Bitmap
import android.net.ConnectivityManager
import android.net.Network
import android.net.Uri
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.ProgressBar
import android.widget.TextView

/**
 * One server's console with a native server picker. It opens on Running agents; the console itself is
 * built for phones (its menu, terminal keys and input box). Sign-in is the console's own,
 * kept in the WebView's cookies. Links to other sites open in the browser.
 */
class ConsoleActivity : Activity() {
    private var web: WebView? = null
    private lateinit var base: Uri
    private var openingConsole = true
    private var issuer: String? = null
    private var apis: List<String> = emptyList()
    private lateinit var url: String
    private lateinit var offline: View
    /** Set by a main-frame error; the page that follows (the WebView's error page) keeps the offline view up. */
    private var failed = false
    private var network: ConnectivityManager.NetworkCallback? = null
    /** Whether the main frame shows the console (not the issuer's sign-in): only then may the page post notifications. */
    @Volatile private var onConsole = false
    private var notifications: AgentNotifications? = null

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_console)
        findViewById<View>(android.R.id.content).padForSystemBars()
        url = intent.getStringExtra(EXTRA_URL) ?: return finish()
        CrashLog.note(this, "Console opened (localhost=${isLocalHttpAddress(url)})")
        base = Uri.parse(savedInstanceState?.getString("console_base") ?: url)
        issuer = savedInstanceState?.getString("oidc_issuer")
        apis = savedInstanceState?.getStringArrayList("oidc_apis") ?: listOf(url)
        openingConsole = savedInstanceState == null
        val webView = findViewById<WebView>(R.id.web)
        web = webView
        val progress = findViewById<ProgressBar>(R.id.progress)
        offline = findViewById(R.id.offline)
        val serverName = intent.getStringExtra(EXTRA_NAME) ?: Uri.parse(url).host ?: url
        findViewById<TextView>(R.id.current_server_name).text = serverName
        findViewById<TextView>(R.id.current_server_url).text = url
        findViewById<View>(R.id.server_switcher).apply {
            contentDescription = getString(R.string.switch_server_description, serverName, url)
            setOnClickListener { showServerPicker() }
        }
        findViewById<Button>(R.id.retry).setOnClickListener { retry() }
        findViewById<Button>(R.id.servers).setOnClickListener { showServers() }
        resetOfflineTitle()
        watchNetwork()

        // Debug builds can be inspected from chrome://inspect on the computer.
        if (BuildConfig.DEBUG) WebView.setWebContentsDebuggingEnabled(true)
        CookieManager.getInstance().setAcceptCookie(true)
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            // The console's own text sizes; the terminal has its own A-/A+.
            textZoom = 100
            userAgentString = "$userAgentString AgentProxyAndroid/${BuildConfig.VERSION_NAME}"
        }
        // The one object the page can call: it posts the console's agent alerts as notifications.
        notifications = AgentNotifications(this, url, serverName, { onConsole }, ::notificationsChanged).also {
            webView.addJavascriptInterface(it, "AgentProxyNotifications")
        }
        // A WebView doesn't download: hand files (the app's own APK on Downloads) to the system,
        // which saves them and offers to install. Downloads need no sign-in.
        webView.setDownloadListener { downloadUrl, _, _, _, _ ->
            try {
                startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(downloadUrl)))
            } catch (_: ActivityNotFoundException) {
            }
        }
        webView.webChromeClient = object : WebChromeClient() {
            override fun onProgressChanged(view: WebView, newProgress: Int) {
                progress.progress = newProgress
                progress.visibility = if (newProgress < 100) View.VISIBLE else View.GONE
            }
        }
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val target = request.url
                if (request.isForMainFrame && isOidcNavigation(apis, issuer, target.toString())) return false
                val launchRedirect = openingConsole && request.isForMainFrame && request.isRedirect && !request.hasGesture()
                if (isInternalConsoleUrl(base.toString(), target.toString(), launchRedirect)) {
                    if (launchRedirect) base = target
                    return false
                }
                try {
                    startActivity(Intent(Intent.ACTION_VIEW, target))
                } catch (_: ActivityNotFoundException) {
                }
                return true
            }

            override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
                onConsole = isInternalConsoleUrl(base.toString(), url, false)
                // An error can be reported before its error page starts; that one stays offline.
                if (!failed) offline.visibility = View.GONE
            }

            override fun onPageFinished(view: WebView, url: String) {
                openingConsole = false
                if (failed) offline.visibility = View.VISIBLE
                failed = false
                // Keep the sign-in across restarts of the app.
                CookieManager.getInstance().flush()
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (request.isForMainFrame) showOffline()
            }

            // A proxy or the server answering 502/503 (a restart, a tunnel down) is as offline as no
            // network, and Retry or the network coming back should load the console again.
            override fun onReceivedHttpError(view: WebView, request: WebResourceRequest, errorResponse: WebResourceResponse) {
                if (request.isForMainFrame && errorResponse.statusCode >= 500) showOffline()
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                // Android will close the app unless this dead WebView is removed and handled.
                Log.e("AgentProxyAndroid", "WebView renderer exited (crashed=${detail.didCrash()})")
                try {
                    CrashLog.record(this@ConsoleActivity, "WebView renderer exited (crashed=${detail.didCrash()})")
                } catch (_: Exception) {
                    // The offline panel must still appear when storage is unavailable.
                }
                (view.parent as? ViewGroup)?.removeView(view)
                if (web === view) web = null
                view.destroy()
                openingConsole = false
                failed = true
                progress.visibility = View.GONE
                findViewById<TextView>(R.id.offline_title).text = getString(R.string.renderer_stopped)
                offline.visibility = View.VISIBLE
                return true
            }
        }
        if (savedInstanceState != null) {
            webView.restoreState(savedInstanceState)
        } else {
            openConsole(url)
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString("console_base", base.toString())
        outState.putString("oidc_issuer", issuer)
        outState.putStringArrayList("oidc_apis", ArrayList(apis))
        web?.saveState(outState)
    }

    @Deprecated("Back goes through the console's history first")
    override fun onBackPressed() {
        val current = web
        if (current?.canGoBack() == true) current.goBack() else showServers()
    }

    override fun onPause() {
        CookieManager.getInstance().flush()
        web?.onPause()
        super.onPause()
    }

    override fun onResume() {
        super.onResume()
        web?.onResume()
        // Back from the app's notification settings, perhaps with them changed.
        notificationsChanged()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        notifications?.answered(requestCode)
    }

    /** A tapped notification: open its page here, or this server's console afresh if this activity shows another. */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        val path = intent.getStringExtra(EXTRA_PATH)
        val current = web
        if (intent.getStringExtra(EXTRA_URL) != url || current == null || path == null || !isConsolePath(path)) {
            startActivity(Intent(this, ConsoleActivity::class.java).putExtras(intent))
            finish()
            return
        }
        // The console's router follows popstate, so the page moves without reloading.
        val target = org.json.JSONObject.quote(path)
        current.evaluateJavascript("history.pushState(null, '', $target); window.dispatchEvent(new PopStateEvent('popstate'))", null)
    }

    private fun notificationsChanged() {
        runOnUiThread {
            web?.evaluateJavascript("window.dispatchEvent(new Event('${AgentNotifications.PERMISSION_EVENT}'))", null)
        }
    }

    override fun onDestroy() {
        network?.let { getSystemService(ConnectivityManager::class.java)?.unregisterNetworkCallback(it) }
        network = null
        web?.let { current ->
            (current.parent as? ViewGroup)?.removeView(current)
            web = null
            current.destroy()
        }
        super.onDestroy()
    }

    private fun showOffline() {
        failed = true
        openingConsole = false
        offline.visibility = View.VISIBLE
    }

    private fun resetOfflineTitle() {
        findViewById<TextView>(R.id.offline_title).text = getString(R.string.offline_title, intent.getStringExtra(EXTRA_NAME) ?: base.host)
    }

    /**
     * Load the console again: reload the page that failed once one was loaded (keeping where the
     * user was), else open it from the start, discovering the sign-in first.
     */
    private fun retry() {
        if (web == null) {
            // A terminated renderer makes its WebView unusable. Recreate the activity on demand.
            startActivity(Intent(this, ConsoleActivity::class.java).putExtras(intent))
            finish()
            return
        }
        offline.visibility = View.GONE
        failed = false
        resetOfflineTitle()
        val current = web
        if (issuer != null && current?.url != null) {
            current.reload()
        } else {
            openingConsole = true
            openConsole(url)
        }
    }

    /** Retry by itself when the network comes back while the offline view is up. */
    private fun watchNetwork() {
        val connectivity = getSystemService(ConnectivityManager::class.java) ?: return
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                runOnUiThread {
                    if (!isFinishing && !isDestroyed && web != null && offline.visibility == View.VISIBLE) retry()
                }
            }
        }
        try {
            connectivity.registerDefaultNetworkCallback(callback)
            network = callback
        } catch (_: RuntimeException) {
            // Too many callbacks registered (or no permission): Retry still works by hand.
        }
    }

    private fun openConsole(url: String) {
        Thread {
            try {
                CrashLog.note(this, "Discovering console sign-in")
                val discovered = authIssuer(url) ?: throw java.io.IOException(getString(R.string.signin_unavailable))
                CrashLog.note(this, "Console sign-in discovered")
                runOnUiThread {
                    if (!isFinishing && !isDestroyed) {
                        issuer = discovered.issuer
                        apis = discovered.apis
                        CrashLog.note(this, "Loading console WebView")
                        web?.loadUrl(url + (intent.getStringExtra(EXTRA_PATH) ?: "/terminals"))
                    }
                }
            } catch (error: java.io.IOException) {
                runOnUiThread {
                    if (!isFinishing && !isDestroyed) {
                        findViewById<TextView>(R.id.offline_title).text = getString(R.string.signin_unavailable)
                        findViewById<View>(R.id.offline).visibility = View.VISIBLE
                    }
                }
            }
        }.start()
    }

    private fun showServerPicker() {
        val store = ServerStore(this)
        val servers = store.all()
        val labels = servers.map { "${it.name}\n${it.url}" }.toTypedArray()
        AlertDialog.Builder(this)
            .setTitle(R.string.switch_server)
            .setSingleChoiceItems(labels, servers.indexOfFirst { it.url == url }) { dialog, index ->
                dialog.dismiss()
                val selected = servers[index]
                if (selected.url != url) {
                    store.last = selected.url
                    // A fresh WebView keeps history and sign-in discovery scoped to the chosen server.
                    startActivity(Intent(this, ConsoleActivity::class.java)
                        .putExtra(EXTRA_URL, selected.url)
                        .putExtra(EXTRA_NAME, selected.name))
                    finish()
                }
            }
            .setNeutralButton(R.string.manage_servers) { _, _ -> showServers() }
            .setNegativeButton(R.string.cancel, null)
            .show()
    }

    private fun showServers() {
        startActivity(Intent(this, ServersActivity::class.java).putExtra(ServersActivity.EXTRA_STAY, true).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP))
        finish()
    }

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_NAME = "name"
        /** The console page to open, such as /get (Downloads); Running agents otherwise. */
        const val EXTRA_PATH = "path"
    }
}
