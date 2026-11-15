package dev.agentproxy.app

import java.net.URI

/**
 * The browser sign-in, which stays in the WebView: any page of the issuer, and only the login and
 * callback paths of the APIs (the chosen server, and with split hosting the API its callbacks name).
 */
internal fun isOidcNavigation(apis: List<String>, issuer: String?, target: String): Boolean {
    if (issuer == null) return false
    val destination = try { URI(target) } catch (_: java.net.URISyntaxException) { return false }
    if (destination.rawUserInfo != null) return false
    if (destination.scheme != "https" && !(isLocalHttpAddress(target) && isLocalHttpAddress(issuer) && apis.any(::isLocalHttpAddress))) return false
    if (isInternalConsoleUrl(issuer, target, false)) return true
    return destination.path in listOf("/api/auth/login", "/api/auth/callback") &&
        apis.any { isInternalConsoleUrl(it, target, false) }
}

/** The local instance uses HTTP only on the same phone, including its OIDC issuer. */
internal fun isLocalHttpAddress(address: String): Boolean {
    val uri = try { URI(address) } catch (_: java.net.URISyntaxException) { return false }
    return uri.scheme == "http" && uri.host?.equals("localhost", ignoreCase = true) == true && uri.rawUserInfo == null
}

/** An HTTPS issuer is safe for any server; HTTP is limited to the selected local instance. */
internal fun isAllowedAuthAddress(address: String, localServer: Boolean): Boolean {
    val uri = try { URI(address) } catch (_: java.net.URISyntaxException) { return false }
    if (uri.host.isNullOrEmpty() || uri.rawUserInfo != null || uri.rawQuery != null || uri.rawFragment != null) return false
    return uri.scheme == "https" || (localServer && isLocalHttpAddress(address))
}

/** A server may send the initial console navigation to its separately hosted HTTPS UI. */
internal fun isInternalConsoleUrl(source: String, target: String, launchRedirect: Boolean): Boolean {
    val base = try { URI(source) } catch (_: java.net.URISyntaxException) { return false }
    val destination = try { URI(target) } catch (_: java.net.URISyntaxException) { return false }
    val host = destination.host ?: return false
    if (destination.rawUserInfo != null) return false
    val scheme = destination.scheme?.lowercase() ?: return false
    if (launchRedirect && scheme == "https") return true
    if (!host.equals(base.host ?: return false, ignoreCase = true)) return false
    val sourceScheme = base.scheme?.lowercase() ?: return false
    val sourcePort = base.port.takeIf { it >= 0 } ?: if (sourceScheme == "https") 443 else 80
    val targetPort = destination.port.takeIf { it >= 0 } ?: if (scheme == "https") 443 else 80
    if (sourceScheme == scheme) return scheme in listOf("http", "https") && sourcePort == targetPort
    return sourceScheme == "http" && scheme == "https" &&
        (sourcePort == targetPort || sourcePort == 80 && targetPort == 443)
}
