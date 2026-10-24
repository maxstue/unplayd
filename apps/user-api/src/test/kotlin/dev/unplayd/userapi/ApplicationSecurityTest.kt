package dev.unplayd.userapi

import com.auth0.jwt.algorithms.Algorithm
import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.identity.GameSnapshot
import dev.unplayd.userapi.identity.LibraryEntry
import dev.unplayd.userapi.identity.LocalUser
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.server.testing.testApplication
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.time.Instant
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class ApplicationSecurityTest {
    private val auth =
        AuthConfig.load(
            mapOf("CLERK_ISSUER" to "https://clerk.test", "CLERK_AUTHORIZED_PARTY" to "http://localhost:3000")::get,
        )
    private val tokens = SessionTokens()
    private val first = LocalUser(UUID.randomUUID(), subject())
    private val second = LocalUser(UUID.randomUUID(), subject())

    @Test
    fun `valid sessions without audience resolve only the token subject`() =
        testApplication {
            val users = TestUsers(listOf(first, second))
            application { module(auth, users, auth.verifier(tokens.provider)) }
            listOf(first, second).forEach { user ->
                val other = if (user == first) second else first
                listOf(emptyMap(), mapOf("sts" to "active")).forEach { claims ->
                    val token = tokens.token(user.clerkSubject, claims)
                    val response =
                        client.get("/me?userId=${other.id}") {
                            header(HttpHeaders.Authorization, "Bearer $token")
                            header("X-User-ID", other.id.toString())
                            header(HttpHeaders.ContentType, "application/json")
                            setBody("""{"userId":"${other.id}"}""")
                        }
                    assertEquals(HttpStatusCode.OK, response.status)
                    val body = response.json()
                    assertEquals(user.id.toString(), body["id"]?.jsonPrimitive?.content)
                    assertEquals(user.clerkSubject, body["clerkSubject"]?.jsonPrimitive?.content)
                    response.assertPrivate(token)
                }
            }
            assertEquals(listOf(first.clerkSubject, first.clerkSubject, second.clerkSubject, second.clerkSubject), users.subjects)
        }

    @Test
    fun `invalid JWTs fail before user resolution and never reflect tokens`() =
        testApplication {
            val users = TestUsers()
            application { module(auth, users, auth.verifier(tokens.provider)) }
            val now = Instant.now().epochSecond
            val invalidClaims =
                listOf(
                    "expired" to mapOf("exp" to now - 60),
                    "wrong issuer" to mapOf("iss" to "https://other.test"),
                    "wrong authorized party" to mapOf("azp" to "https://other.test"),
                    "authorized party trailing slash" to mapOf("azp" to "http://localhost:3000/"),
                    "future not before" to mapOf("nbf" to now + 3600),
                    "future issued at" to mapOf("iat" to now + 3600),
                    "pending session" to mapOf("sts" to "pending"),
                    "invalid subject" to mapOf("sub" to UUID.randomUUID().toString()),
                    "invalid session" to mapOf("sid" to UUID.randomUUID().toString()),
                    "empty subject" to mapOf("sub" to ""),
                    "empty session" to mapOf("sid" to ""),
                ) + listOf("exp", "iat", "nbf", "sub", "sid", "azp", "iss").map { "missing $it" to mapOf(it to null) }
            val symmetric = Algorithm.HMAC256(UUID.randomUUID().toString())
            val invalid =
                invalidClaims.map { (name, claims) -> name to tokens.token(first.clerkSubject, claims) } +
                    listOf(
                        "tampered signature" to tokens.tamperedToken(first.clerkSubject),
                        "unknown signing key" to tokens.token(first.clerkSubject, signingKeyId = UUID.randomUUID().toString()),
                        "missing signing key" to tokens.token(first.clerkSubject, signingKeyId = null),
                        "symmetric algorithm" to
                            tokens.token(first.clerkSubject, signingAlgorithm = symmetric),
                        "unsigned" to tokens.token(first.clerkSubject, signingAlgorithm = Algorithm.none()),
                        "malformed" to UUID.randomUUID().toString(),
                    )
            invalid.forEach { (name, token) ->
                val response =
                    client.get("/me") {
                        header(HttpHeaders.Authorization, "Bearer $token")
                        header("X-Request-ID", "rejected_session-123")
                    }
                assertEquals(HttpStatusCode.Unauthorized, response.status, name)
                assertEquals("Unauthorized", response.json()["message"]?.jsonPrimitive?.content, name)
                assertEquals("rejected_session-123", response.headers["X-Request-ID"], name)
                assertEquals("rejected_session-123", response.json()["requestId"]?.jsonPrimitive?.content, name)
                response.assertPrivate(token)
            }
            assertTrue(users.subjects.isEmpty(), "Rejected JWTs must never reach identity storage")
        }

    @Test
    fun `missing duplicate and malformed authorization headers are rejected`() =
        testApplication {
            val users = TestUsers(listOf(first))
            application { module(auth, users, auth.verifier(tokens.provider)) }
            val token = tokens.token(first.clerkSubject)
            listOf(
                emptyList(),
                listOf("Basic $token"),
                listOf(token),
                listOf("Bearer"),
                listOf("Bearer "),
                listOf("Bearer $token", "Bearer $token"),
                listOf("Bearer $token", "Bearer ${UUID.randomUUID()}"),
                listOf("Bearer $token, Bearer $token"),
            ).forEachIndexed { index, headers ->
                val response = client.get("/me") { headers.forEach { header(HttpHeaders.Authorization, it) } }
                assertEquals(HttpStatusCode.Unauthorized, response.status, "header case $index")
                response.assertPrivate(token)
            }
            assertTrue(users.subjects.isEmpty())
        }

    @Test
    fun `library entries ignore spoofed identity and enforce ownership`() =
        testApplication {
            val firstGame = UUID.randomUUID()
            val secondGame = UUID.randomUUID()
            val firstEntry = entry(firstGame, "First private game")
            val secondEntry = entry(secondGame, "Second private game")
            val users =
                TestUsers(
                    listOf(first, second),
                    mapOf(
                        (first.id to firstGame) to firstEntry,
                        (second.id to secondGame) to secondEntry,
                    ),
                )
            application { module(auth, users, auth.verifier(tokens.provider)) }
            listOf(Triple(first, firstGame, firstEntry), Triple(second, secondGame, secondEntry)).forEach { (user, game, expected) ->
                val other = if (user == first) second else first
                val token = tokens.token(user.clerkSubject)
                val own =
                    client.get("/library/$game?userId=${other.id}") {
                        header(HttpHeaders.Authorization, "Bearer $token")
                        header("X-User-ID", other.id.toString())
                    }
                assertEquals(HttpStatusCode.OK, own.status)
                assertEquals(expected.id, own.json()["id"]?.jsonPrimitive?.content)
                assertEquals(expected.gameId, own.json()["gameId"]?.jsonPrimitive?.content)
                own.assertPrivate(token)
                val foreignGame = if (user == first) secondGame else firstGame
                listOf(foreignGame, UUID.randomUUID()).forEach { inaccessible ->
                    val response =
                        client.get("/library/$inaccessible?userId=${other.id}") {
                            header(HttpHeaders.Authorization, "Bearer $token")
                            header("X-User-ID", other.id.toString())
                        }
                    assertEquals(HttpStatusCode.NotFound, response.status)
                    val body = response.bodyAsText()
                    assertFalse(body.contains(firstEntry.id) || body.contains(secondEntry.id))
                    assertEquals("Not found", response.json()["message"]?.jsonPrimitive?.content)
                    response.assertPrivate(token)
                }
            }
            assertEquals(6, users.lookups.size)
            assertTrue(users.lookups.take(3).all { it.first == first.id })
            assertTrue(users.lookups.drop(3).all { it.first == second.id })
        }

    @Test
    fun `protected library invalid IDs and unknown routes remain authenticated and private`() =
        testApplication {
            val users = TestUsers(listOf(first))
            application { module(auth, users, auth.verifier(tokens.provider)) }
            val token = tokens.token(first.clerkSubject)
            listOf("/library/${UUID.randomUUID()}", "/library/invalid", "/unknown").forEach { path ->
                val response = client.get(path)
                assertEquals(HttpStatusCode.Unauthorized, response.status, path)
                response.assertPrivate(token)
            }
            listOf("/library/invalid" to HttpStatusCode.BadRequest, "/unknown" to HttpStatusCode.NotFound).forEach { (path, expected) ->
                val response = client.get(path) { header(HttpHeaders.Authorization, "Bearer $token") }
                assertEquals(expected, response.status, path)
                response.assertPrivate(token)
            }
            assertTrue(users.subjects.isEmpty())
        }

    @Test
    fun `storage failures return a sanitized private error with request correlation`() =
        testApplication {
            val token = tokens.token(first.clerkSubject)
            val users = TestUsers(failure = IllegalStateException("database failure $token"))
            application { module(auth, users, auth.verifier(tokens.provider)) }
            val response =
                client.get("/me") {
                    header(HttpHeaders.Authorization, "Bearer $token")
                    header("X-Request-ID", "storage_failure-123")
                }
            assertEquals(HttpStatusCode.ServiceUnavailable, response.status)
            val problem = response.json()
            assertEquals("Service unavailable", problem["message"]?.jsonPrimitive?.content)
            assertEquals("storage_failure-123", problem["requestId"]?.jsonPrimitive?.content)
            assertFalse(response.bodyAsText().contains("database failure"))
            response.assertPrivate(token)
        }

    private suspend fun HttpResponse.json() =
        Json.parseToJsonElement(bodyAsText()).jsonObject.let { body ->
            body["error"]?.jsonObject ?: body
        }

    private suspend fun HttpResponse.assertPrivate(token: String) {
        assertEquals("no-store", headers[HttpHeaders.CacheControl])
        assertTrue(assertNotNull(headers["X-Request-ID"]).matches(Regex("[a-zA-Z0-9_-]{1,64}")))
        assertFalse(bodyAsText().contains(token), "Response must not expose the bearer token")
        assertFalse(headers.entries().any { (_, values) -> values.any { it.contains(token) } }, "Headers must not expose the bearer token")
    }

    private fun subject() = "user_${UUID.randomUUID().toString().replace("-", "")}"

    private fun entry(
        gameId: UUID,
        title: String,
    ) = LibraryEntry(
        id = UUID.randomUUID().toString(),
        gameId = gameId.toString(),
        platformId = null,
        ownership = "owned",
        status = "backlog",
        favorite = false,
        rating = null,
        note = null,
        snapshot = GameSnapshot(title, null),
        createdAt = "2026-01-01T00:00:00Z",
        updatedAt = "2026-01-01T00:00:00Z",
    )
}
