package dev.unplayd.userapi

import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpStatusCode
import io.ktor.server.testing.testApplication
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class ApplicationTest {
    @Test
    fun `liveness returns service identity and a generated request ID`() =
        testApplication {
            application { module() }
            val response = client.get("/health/live")
            val body = Json.parseToJsonElement(response.bodyAsText()).jsonObject
            assertEquals(HttpStatusCode.OK, response.status)
            assertEquals("ok", body["status"]?.jsonPrimitive?.content)
            assertEquals("user-api", body["service"]?.jsonPrimitive?.content)
            assertTrue(assertNotNull(response.headers["X-Request-ID"]).matches(Regex("[a-zA-Z0-9_-]{1,64}")))
        }

    @Test
    fun `valid request IDs are preserved including the length limit`() =
        testApplication {
            application { module() }
            listOf("trace_123-ABC", "a".repeat(64)).forEach { id ->
                val response = client.get("/health/live") { header("X-Request-ID", id) }
                assertEquals(HttpStatusCode.OK, response.status)
                assertEquals(id, response.headers["X-Request-ID"])
            }
        }

    @Test
    fun `invalid request IDs are replaced`() =
        testApplication {
            application { module() }
            listOf("invalid request id", "a".repeat(65)).forEach { id ->
                val response = client.get("/health/live") { header("X-Request-ID", id) }
                assertEquals(HttpStatusCode.OK, response.status)
                assertNotEquals(id, response.headers["X-Request-ID"])
                assertTrue(assertNotNull(response.headers["X-Request-ID"]).matches(Regex("[a-zA-Z0-9_-]{1,64}")))
            }
        }
}
