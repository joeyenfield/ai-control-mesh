package dev.agentproxy.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentNotificationsTest {
    @Test
    fun aNotificationOpensOnlyAPathOnTheConsole() {
        assertTrue(isConsolePath("/terminals/abc-123"))
        assertTrue(isConsolePath("/terminals/a%20b"))
        assertFalse(isConsolePath("terminals/abc"))
        assertFalse(isConsolePath("//evil.test/terminals"))
        assertFalse(isConsolePath("https://evil.test/"))
        assertFalse(isConsolePath("/terminals/a b"))
        assertFalse(isConsolePath("/\\evil.test"))
        assertFalse(isConsolePath("/" + "a".repeat(600)))
    }
}
