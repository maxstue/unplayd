package dev.unplayd.userapi

import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.identity.JdbcUserStore
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.server.testing.testApplication
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.sql.DriverManager
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class PersistedIdentityTest {
    @Test
    fun `verified sessions enforce ownership in PostgreSQL`() =
        testApplication {
            UserApiTestDatabase.start().use { database ->
                val config = database.config
                val store = JdbcUserStore(config)
                val firstSubject = "user_${UUID.randomUUID().toString().replace("-", "")}"
                val secondSubject = "user_${UUID.randomUUID().toString().replace("-", "")}"
                val first = store.resolve(firstSubject)
                val second = store.resolve(secondSubject)
                val repeated = coroutineScope { List(8) { async { store.resolve(firstSubject) } }.map { it.await() } }
                assertEquals(setOf(first.id), repeated.map { it.id }.toSet())
                val game = UUID.randomUUID()
                val entry = UUID.randomUUID()
                DriverManager.getConnection(config.url, config.user, config.password).use { db ->
                    db
                        .prepareStatement(
                            "INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES (?,?,?,'owned','Private game')",
                        ).use {
                            it.setObject(1, entry)
                            it.setObject(2, first.id)
                            it.setObject(3, game)
                            it.executeUpdate()
                        }
                    db.prepareStatement("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')").use {
                        it.setObject(1, entry)
                        it.executeUpdate()
                    }
                }
                val auth =
                    AuthConfig.load(
                        mapOf("CLERK_ISSUER" to "https://clerk.test", "CLERK_AUTHORIZED_PARTY" to "http://localhost:8444")::get,
                    )
                val tokens = SessionTokens()
                application { module(auth, store, auth.verifier(tokens.provider)) }
                val own = client.get("/library/$game") { header(HttpHeaders.Authorization, "Bearer ${tokens.token(firstSubject)}") }
                assertEquals(HttpStatusCode.OK, own.status)
                assertEquals(
                    entry.toString(),
                    Json
                        .parseToJsonElement(own.bodyAsText())
                        .jsonObject["id"]
                        ?.jsonPrimitive
                        ?.content,
                )
                val other =
                    client.get("/library/$game?userId=${first.id}") {
                        header(HttpHeaders.Authorization, "Bearer ${tokens.token(secondSubject)}")
                        header("X-User-ID", first.id.toString())
                    }
                assertEquals(HttpStatusCode.NotFound, other.status)
                assertFalse(other.bodyAsText().contains("Private game"))
                val me =
                    client.get("/me") {
                        header(HttpHeaders.Authorization, "Bearer ${tokens.token(secondSubject)}")
                    }
                assertEquals(
                    second.id.toString(),
                    Json
                        .parseToJsonElement(me.bodyAsText())
                        .jsonObject["id"]
                        ?.jsonPrimitive
                        ?.content,
                )
            }
        }
}
