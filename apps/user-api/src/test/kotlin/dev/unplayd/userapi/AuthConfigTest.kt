package dev.unplayd.userapi

import dev.unplayd.userapi.auth.AuthConfig
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse

class AuthConfigTest {
    private val base = mapOf("CLERK_ISSUER" to "https://clerk.test", "CLERK_AUTHORIZED_PARTY" to "http://localhost:3000")

    @Test
    fun `valid HTTPS origins and local authorized parties retain exact values`() {
        val parties =
            listOf("https://app.test", "https://app.test:8443", "http://localhost:3000", "http://127.0.0.1:3000", "http://[::1]:3000")
        parties.forEach { party ->
            val config = AuthConfig.load((base + ("CLERK_AUTHORIZED_PARTY" to party))::get)
            assertEquals("https://clerk.test", config.issuer)
            assertEquals(party, config.authorizedParty)
        }
    }

    @Test
    fun `missing or unsafe origins fail without exposing configuration values`() {
        val credential = UUID.randomUUID().toString()
        val invalid =
            listOf(
                "",
                "invalid",
                "https://",
                "https://clerk.test/",
                "https://clerk.test/path",
                "https://clerk.test?value=$credential",
                "https://clerk.test#$credential",
                "https://user:$credential@clerk.test",
                "https://clerk.test:0",
                "https://clerk.test:65536",
            )
        listOf("CLERK_ISSUER", "CLERK_AUTHORIZED_PARTY").forEach { name ->
            (invalid.map { base + (name to it) } + (base - name)).forEachIndexed { index, values ->
                val failure = assertFailsWith<IllegalArgumentException>("$name case $index") { AuthConfig.load(values::get) }
                assertFalse(failure.message.orEmpty().contains(credential))
            }
        }
        listOf(
            base + ("CLERK_ISSUER" to "http://localhost:3000"),
            base + ("CLERK_AUTHORIZED_PARTY" to "http://app.test"),
            base + ("CLERK_AUTHORIZED_PARTY" to "http://localhost.evil.test"),
        ).forEach { values -> assertFailsWith<IllegalArgumentException> { AuthConfig.load(values::get) } }
    }
}
