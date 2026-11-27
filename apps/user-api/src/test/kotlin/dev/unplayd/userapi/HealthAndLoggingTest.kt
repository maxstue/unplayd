package dev.unplayd.userapi

import ch.qos.logback.classic.Level
import ch.qos.logback.classic.Logger
import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.read.ListAppender
import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.health.Readiness
import dev.unplayd.userapi.identity.LocalUser
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.server.testing.testApplication
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.slf4j.LoggerFactory
import java.util.UUID
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class HealthAndLoggingTest {
    private val auth =
        AuthConfig.load(
            mapOf("CLERK_ISSUER" to "https://clerk.test", "CLERK_AUTHORIZED_PARTY" to "http://localhost:8444")::get,
        )
    private val tokens = SessionTokens()
    private val requests = LoggerFactory.getLogger("dev.unplayd.userapi.requests") as Logger
    private val appender = ListAppender<ILoggingEvent>()

    @BeforeTest
    fun capture() {
        appender.start()
        requests.addAppender(appender)
        requests.level = Level.DEBUG
    }

    @AfterTest
    fun release() {
        requests.detachAppender(appender)
        requests.level = null
    }

    @Test
    fun `readiness reports the database without authentication`() =
        testApplication {
            var ready = true
            application { testModule(auth, TestUsers(), auth.verifier(tokens.provider), Readiness { ready }) }
            val ok = client.get("/health/ready")
            assertEquals(HttpStatusCode.OK, ok.status)
            assertEquals("ok", database(ok.bodyAsText()))

            ready = false
            val down = client.get("/health/ready")
            assertEquals(HttpStatusCode.ServiceUnavailable, down.status)
            assertEquals("unavailable", database(down.bodyAsText()))
            assertEquals(HttpStatusCode.OK, client.get("/health/live").status, "liveness must not depend on the database")
        }

    @Test
    fun `each request is logged with request ID status and duration but without query or token`() =
        testApplication {
            val user = LocalUser(UUID.randomUUID(), "user_logging")
            application { testModule(auth, TestUsers(listOf(user)), auth.verifier(tokens.provider)) }
            val token = tokens.token(user.clerkSubject)
            client.get("/me?note=private-note") {
                header("X-Request-ID", "log_request_1")
                header(HttpHeaders.Authorization, "Bearer $token")
            }
            val entry = appender.list.single { it.keyValue("requestId") == "log_request_1" }
            assertEquals(Level.INFO, entry.level)
            assertEquals("request completed", entry.formattedMessage)
            assertEquals("GET", entry.keyValue("method"))
            assertEquals("/me", entry.keyValue("path"))
            assertEquals(200, entry.keyValue("status"))
            assertEquals(true, entry.keyValue("durationMs") is Long)
            val logged = appender.list.joinToString { "${it.formattedMessage} ${it.keyValuePairs}" }
            assertFalse(logged.contains(token) || logged.contains("private-note"), logged)
        }

    @Test
    fun `health probes log at debug level`() =
        testApplication {
            application { testModule(auth, TestUsers(), auth.verifier(tokens.provider)) }
            client.get("/health/live") { header("X-Request-ID", "probe_1") }
            assertEquals(Level.DEBUG, appender.list.single { it.keyValue("requestId") == "probe_1" }.level)
        }

    private fun database(body: String) =
        Json
            .parseToJsonElement(body)
            .jsonObject["checks"]
            ?.jsonObject
            ?.get("database")
            ?.jsonPrimitive
            ?.content

    private fun ILoggingEvent.keyValue(key: String) = keyValuePairs.orEmpty().firstOrNull { it.key == key }?.value
}
