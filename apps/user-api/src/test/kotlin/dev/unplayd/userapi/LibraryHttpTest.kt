package dev.unplayd.userapi

import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.catalog.HttpCatalogClient
import dev.unplayd.userapi.identity.JdbcUserStore
import dev.unplayd.userapi.library.JdbcLibraryStore
import dev.unplayd.userapi.library.LibraryEntry
import dev.unplayd.userapi.library.LibraryPage
import dev.unplayd.userapi.library.LibraryService
import io.ktor.client.request.header
import io.ktor.client.request.request
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.client.statement.bodyAsText
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.server.testing.ApplicationTestBuilder
import io.ktor.server.testing.testApplication
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.sql.Connection
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class LibraryHttpTest {
    private val auth =
        AuthConfig.load(
            mapOf("CLERK_ISSUER" to "https://clerk.test", "CLERK_AUTHORIZED_PARTY" to "http://localhost:8444")::get,
        )
    private val tokens = SessionTokens()
    private val first = "user_${UUID.randomUUID().toString().replace("-", "")}"
    private val second = "user_${UUID.randomUUID().toString().replace("-", "")}"

    @Test
    fun `CRUD uses catalog snapshots and preserves independent state on retry and status changes`() =
        withLibrary { fixture ->
            val platform = UUID.randomUUID()
            val game = fixture.catalog.add(platform)
            val body = """{"gameId":"${game.id}","ownership":"owned","platformId":"$platform"}"""
            val created = request(HttpMethod.Post, "/library", body)
            assertEquals(HttpStatusCode.Created, created.status)
            val entry = created.entry()
            assertEquals("backlog", entry.status)
            assertEquals(game.title, entry.snapshot.title)
            assertEquals(game.coverUrl, entry.snapshot.coverUrl)
            assertEquals(platform.toString(), entry.platformId)
            assertFalse(entry.favorite)
            assertNull(entry.rating)
            assertNull(entry.note)
            assertEquals(listOf<Pair<String?, String?>>("Bearer ${fixture.catalog.token}" to "library-http-123"), fixture.catalog.requests)
            fixture.database.connection().use { db ->
                db.execute("INSERT INTO user_data.favorites(library_entry_id) VALUES (?)", UUID.fromString(entry.id))
                db.execute("INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,9)", UUID.fromString(entry.id))
                db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Personal note')", UUID.fromString(entry.id))
            }
            fixture.catalog.status = 503
            var current = entry.copy(favorite = true, rating = 9, note = "Personal note")
            for (status in listOf("playing", "paused", "completed", "dropped", "backlog")) {
                val changed = request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"$status"}""")
                assertEquals(HttpStatusCode.OK, changed.status)
                val updated = changed.entry()
                assertEquals(current.copy(status = status, updatedAt = updated.updatedAt), updated)
                current = updated
                val repeated = request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"$status"}""")
                assertEquals(current, repeated.entry())
                val replay = request(HttpMethod.Post, "/library", body)
                assertEquals(HttpStatusCode.OK, replay.status)
                assertEquals(current, replay.entry())
            }
            assertEquals(1, fixture.catalog.requests.size, "Existing entries must work during catalog outages")
            for (conflict in listOf(
                """{"gameId":"${game.id}","ownership":"wishlist","platformId":"$platform"}""",
                """{"gameId":"${game.id}","ownership":"owned"}""",
            )) {
                assertEquals(HttpStatusCode.Conflict, request(HttpMethod.Post, "/library", conflict).status)
            }
            assertEquals(current, request(HttpMethod.Get, "/library/${game.id}").entry())
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/library/${game.id}").status)
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/library/${game.id}").status)
            assertEquals(HttpStatusCode.NotFound, request(HttpMethod.Get, "/library/${game.id}").status)
            fixture.database.connection().use { db ->
                for (table in listOf("game_statuses", "favorites", "ratings", "notes")) {
                    assertEquals(0, db.count("SELECT count(*) FROM user_data.$table"))
                }
            }
        }

    @Test
    fun `overview and platform reads aggregate the complete own collection without catalog calls`() =
        withLibrary { fixture ->
            val empty = Json.parseToJsonElement(request(HttpMethod.Get, "/library/overview").bodyAsText()).jsonObject
            assertEquals(0, empty.getValue("total").jsonPrimitive.int)
            assertEquals(5, empty.getValue("statuses").jsonObject.size)
            val user = JdbcUserStore(fixture.database.dataSource).resolve(first)
            val foreign = JdbcUserStore(fixture.database.dataSource).resolve(second)
            val platform = UUID.randomUUID()
            val otherPlatform = UUID.randomUUID()
            fixture.database.connection().use { db ->
                repeat(125) { index ->
                    val entry = UUID.randomUUID()
                    db.execute(
                        "INSERT INTO user_data.library_entries(id,user_id,game_id,platform_id,ownership,snapshot_title) VALUES (?,?,?,?,'owned','Game')",
                        entry,
                        user.id,
                        UUID.randomUUID(),
                        if (index ==
                            124
                        ) {
                            otherPlatform
                        } else {
                            platform
                        },
                    )
                    db.execute(
                        "INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,?)",
                        entry,
                        if (index % 2 ==
                            0
                        ) {
                            "playing"
                        } else {
                            "backlog"
                        },
                    )
                }
                val entry = UUID.randomUUID()
                db.execute(
                    "INSERT INTO user_data.library_entries(id,user_id,game_id,platform_id,ownership,snapshot_title) VALUES (?,?,?,?,'owned','Foreign')",
                    entry,
                    foreign.id,
                    UUID.randomUUID(),
                    UUID.randomUUID(),
                )
                db.execute("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'completed')", entry)
            }
            val overview =
                Json.decodeFromString<dev.unplayd.userapi.library.LibraryOverview>(
                    request(HttpMethod.Get, "/library/overview").bodyAsText(),
                )
            assertEquals(125, overview.total)
            assertEquals(mapOf("backlog" to 62, "playing" to 63, "paused" to 0, "completed" to 0, "dropped" to 0), overview.statuses)
            val platforms =
                Json.decodeFromString<dev.unplayd.userapi.library.LibraryPlatforms>(
                    request(HttpMethod.Get, "/library/platforms").bodyAsText(),
                )
            assertEquals(setOf(platform.toString(), otherPlatform.toString()), platforms.items.map { it.platformId }.toSet())
            assertEquals(2, platforms.items.size)
            platforms.items.forEach { item ->
                assertEquals(item.platformId, request(HttpMethod.Get, "/library/${item.gameId}").entry().platformId)
            }
            assertEquals(0, fixture.catalog.requests.size)
            for (path in listOf("/library/overview", "/library/platforms")) {
                assertEquals(HttpStatusCode.Unauthorized, request(HttpMethod.Get, path, subject = null).status)
            }
        }

    @Test
    fun `list uses stable cursor pagination and filters without exposing another users entries`() =
        withLibrary { fixture ->
            val platform = UUID.randomUUID()
            val games = (1..5).map { fixture.catalog.add(platform) }
            games.forEachIndexed { index, game ->
                assertEquals(
                    HttpStatusCode.Created,
                    request(
                        HttpMethod.Post,
                        "/library",
                        """{"gameId":"${game.id}","ownership":"${if (index % 2 == 0) "owned" else "subscription"}","platformId":"$platform"}""",
                    ).status,
                )
            }
            val other = fixture.catalog.add()
            request(HttpMethod.Post, "/library", """{"gameId":"${other.id}","ownership":"wishlist"}""", second)
            // Equal timestamps exercise the UUID tie-breaker, not just timestamp ordering.
            fixture.database.connection().use { db -> db.execute("UPDATE user_data.library_entries SET created_at='2026-01-01T00:00:00Z'") }
            val seen = mutableListOf<String>()
            var cursor: String? = null
            do {
                val response = request(HttpMethod.Get, "/library?limit=2${cursor?.let { "&cursor=$it" }.orEmpty()}")
                assertEquals(HttpStatusCode.OK, response.status)
                val page = Json.decodeFromString<LibraryPage>(response.bodyAsText())
                seen += page.items.map { it.gameId }
                cursor = page.nextCursor
            } while (cursor != null)
            assertEquals(5, seen.distinct().size)
            assertEquals(games.map { it.id }.toSet(), seen.toSet())
            request(HttpMethod.Put, "/library/${games.first().id}/status", """{"status":"playing"}""")
            val filtered = request(HttpMethod.Get, "/library?status=playing&ownership=owned&platformId=$platform")
            assertEquals(listOf(games.first().id), Json.decodeFromString<LibraryPage>(filtered.bodyAsText()).items.map { it.gameId })
            val empty = request(HttpMethod.Get, "/library?platformId=${UUID.randomUUID()}")
            assertEquals(LibraryPage(emptyList(), null), Json.decodeFromString<LibraryPage>(empty.bodyAsText()))
            val page = Json.decodeFromString<LibraryPage>(request(HttpMethod.Get, "/library?limit=1").bodyAsText())
            assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Get, "/library?status=playing&cursor=${page.nextCursor}").status)
            assertEquals(
                listOf(
                    other.id,
                ),
                Json.decodeFromString<LibraryPage>(request(HttpMethod.Get, "/library", subject = second).bodyAsText()).items.map {
                    it.gameId
                },
            )
        }

    @Test
    fun `authenticated writes remain isolated even for the same game and spoofed identity`() =
        withLibrary { fixture ->
            val game = fixture.catalog.add()
            val body = """{"gameId":"${game.id}","ownership":"owned"}"""
            val firstEntry = request(HttpMethod.Post, "/library", body).entry()
            assertEquals(
                HttpStatusCode.NotFound,
                request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"completed"}""", second).status,
            )
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/library/${game.id}", subject = second).status)
            assertEquals(firstEntry, request(HttpMethod.Get, "/library/${game.id}").entry())
            val secondEntry = request(HttpMethod.Post, "/library", body, second).entry()
            assertTrue(firstEntry.id != secondEntry.id)
            request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"completed"}""", second)
            assertEquals(firstEntry, request(HttpMethod.Get, "/library/${game.id}").entry())
            request(HttpMethod.Delete, "/library/${game.id}")
            assertEquals("completed", request(HttpMethod.Get, "/library/${game.id}", subject = second).entry().status)
            for ((method, path, payload) in listOf(
                Triple(HttpMethod.Get, "/library", null),
                Triple(HttpMethod.Post, "/library", body),
                Triple(HttpMethod.Put, "/library/${game.id}/status", """{"status":"playing"}"""),
                Triple(HttpMethod.Delete, "/library/${game.id}", null),
            )) {
                assertEquals(HttpStatusCode.Unauthorized, request(method, path, payload, subject = null).status)
            }
        }

    @Test
    fun `validation and catalog failures produce controlled errors without partial entries`() =
        withLibrary { fixture ->
            val game = fixture.catalog.add(UUID.randomUUID())
            val invalid =
                listOf(
                    "{}",
                    "{",
                    """{"gameId":"bad","ownership":"owned"}""",
                    """{"gameId":"${game.id}","ownership":"invalid"}""",
                    """{"gameId":"${game.id}","ownership":"owned","platformId":"invalid"}""",
                    """{"gameId":"${game.id}","ownership":"owned","status":"completed"}""",
                )
            invalid.forEach { assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Post, "/library", it).status, it) }
            assertTrue(fixture.catalog.requests.isEmpty())
            for (query in listOf(
                "limit=0",
                "limit=51",
                "limit=no",
                "status=bad",
                "ownership=bad",
                "platformId=bad",
                "cursor=bad",
                "cursor=",
            )) {
                assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Get, "/library?$query").status, query)
            }
            assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"bad"}""").status)
            assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Delete, "/library/1-1-1-1-1").status)
            assertEquals(
                HttpStatusCode.BadRequest,
                request(
                    HttpMethod.Post,
                    "/library",
                    """{"gameId":"${game.id}","ownership":"owned","platformId":"${UUID.randomUUID()}"}""",
                ).status,
            )
            val inactive = fixture.catalog.add(active = false)
            for (id in listOf(UUID.randomUUID().toString(), inactive.id)) {
                assertEquals(
                    HttpStatusCode.BadRequest,
                    request(HttpMethod.Post, "/library", """{"gameId":"$id","ownership":"owned"}""").status,
                )
            }
            val body = """{"gameId":"${game.id}","ownership":"owned"}"""
            for (status in listOf(401, 429, 500, 503, 302)) {
                fixture.catalog.status = status
                val failure = request(HttpMethod.Post, "/library", body)
                assertEquals(HttpStatusCode.ServiceUnavailable, failure.status)
                assertFalse(failure.bodyAsText().contains(fixture.catalog.token))
                assertTrue(failure.bodyAsText().contains("library-http-123"))
            }
            fixture.catalog.status = 200
            fixture.catalog.body = "{}"
            assertEquals(HttpStatusCode.ServiceUnavailable, request(HttpMethod.Post, "/library", body).status)
            fixture.database.connection().use { db -> assertEquals(0, db.count("SELECT count(*) FROM user_data.library_entries")) }
            fixture.catalog.status = null
            fixture.catalog.body = null
            assertEquals(HttpStatusCode.Created, request(HttpMethod.Post, "/library", body).status)
        }

    @Test
    fun `concurrent creates are idempotent and failed status insertion rolls back the entry`() =
        withLibrary { fixture ->
            val game = fixture.catalog.add()
            val body = """{"gameId":"${game.id}","ownership":"owned"}"""
            val results = coroutineScope { List(8) { async { request(HttpMethod.Post, "/library", body) } }.map { it.await() } }
            assertEquals(1, results.count { it.status == HttpStatusCode.Created })
            assertEquals(7, results.count { it.status == HttpStatusCode.OK })
            assertEquals(1, results.map { it.entry().id }.distinct().size)
            // Hold a concurrent status change until the HTTP request is waiting for the entry lock.
            coroutineScope {
                fixture.database.connection().use { writer ->
                    writer.autoCommit = false
                    try {
                        writer.execute("UPDATE user_data.library_entries SET updated_at=now()")
                        writer.execute("UPDATE user_data.game_statuses SET status='playing'")
                        val pending =
                            async {
                                request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"backlog"}""")
                            }
                        fixture.database.connection().use { observer ->
                            withTimeout(5000) {
                                while (observer.count(
                                        "SELECT count(*) FROM pg_stat_activity WHERE usename=current_user AND wait_event_type='Lock'",
                                    ) == 0
                                ) {
                                    delay(10)
                                }
                            }
                        }
                        writer.commit()
                        assertEquals("backlog", pending.await().entry().status)
                    } finally {
                        writer.rollback()
                    }
                }
            }
            val failed = fixture.catalog.add()
            fixture.database.connection().use { db ->
                db.execute("ALTER TABLE user_data.game_statuses ADD CONSTRAINT reject_new_status CHECK(status <> 'backlog') NOT VALID")
            }
            assertEquals(
                HttpStatusCode.ServiceUnavailable,
                request(HttpMethod.Post, "/library", """{"gameId":"${failed.id}","ownership":"wishlist"}""").status,
            )
            fixture.database.connection().use { db ->
                assertEquals(1, db.count("SELECT count(*) FROM user_data.library_entries"))
                assertEquals(1, db.count("SELECT count(*) FROM user_data.game_statuses"))
            }
        }

    @Test
    fun `favorites are idempotent, preserve library state and stay isolated per user`() =
        withLibrary { fixture ->
            val platform = UUID.randomUUID()
            val game = fixture.catalog.add(platform)
            request(HttpMethod.Post, "/library", """{"gameId":"${game.id}","ownership":"owned","platformId":"$platform"}""")
            val playing = request(HttpMethod.Put, "/library/${game.id}/status", """{"status":"playing"}""").entry()
            fixture.catalog.status = 503
            val favorited = request(HttpMethod.Put, "/favorites/${game.id}")
            assertEquals(HttpStatusCode.OK, favorited.status)
            val entry = favorited.entry()
            assertEquals(playing.copy(favorite = true, updatedAt = entry.updatedAt), entry)
            assertEquals(entry, request(HttpMethod.Put, "/favorites/${game.id}").entry())
            assertEquals(1, fixture.catalog.requests.size, "Existing entries must be favorited without the catalog")
            fixture.database.connection().use { db ->
                assertEquals(1, db.count("SELECT count(*) FROM user_data.favorites"))
                val duplicate =
                    kotlin.runCatching {
                        db.execute(
                            "INSERT INTO user_data.favorites(library_entry_id) VALUES (?)",
                            UUID.fromString(entry.id),
                        )
                    }
                assertEquals("23505", (duplicate.exceptionOrNull() as java.sql.SQLException).sqlState)
            }

            fixture.catalog.status = null
            val foreign = request(HttpMethod.Put, "/favorites/${game.id}", subject = second).entry()
            assertEquals(listOf("wishlist", "backlog", null), listOf(foreign.ownership, foreign.status, foreign.platformId))
            assertTrue(foreign.favorite)
            assertEquals(game.title, foreign.snapshot.title)
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/favorites/${game.id}", subject = second).status)
            assertEquals(entry, request(HttpMethod.Get, "/library/${game.id}").entry())
            assertFalse(request(HttpMethod.Get, "/library/${game.id}", subject = second).entry().favorite)

            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/favorites/${game.id}").status)
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/favorites/${game.id}").status)
            assertEquals(HttpStatusCode.NoContent, request(HttpMethod.Delete, "/favorites/${UUID.randomUUID()}").status)
            val unfavorited = request(HttpMethod.Get, "/library/${game.id}").entry()
            assertEquals(entry.copy(favorite = false, updatedAt = unfavorited.updatedAt), unfavorited)

            val inactive = fixture.catalog.add(active = false)
            for (id in listOf(UUID.randomUUID().toString(), inactive.id)) {
                assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Put, "/favorites/$id").status)
            }
            val unavailable = fixture.catalog.add()
            fixture.catalog.status = 503
            assertEquals(HttpStatusCode.ServiceUnavailable, request(HttpMethod.Put, "/favorites/${unavailable.id}").status)
            fixture.database.connection().use { db -> assertEquals(2, db.count("SELECT count(*) FROM user_data.library_entries")) }
            assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Put, "/favorites/bad").status)
            for (method in listOf(HttpMethod.Put, HttpMethod.Delete)) {
                assertEquals(HttpStatusCode.Unauthorized, request(method, "/favorites/${game.id}", subject = null).status)
            }
        }

    @Test
    fun `favorite filter pages through own favorites and lookup returns only own entries`() =
        withLibrary { fixture ->
            val games = (1..4).map { fixture.catalog.add() }
            games.forEach { request(HttpMethod.Post, "/library", """{"gameId":"${it.id}","ownership":"owned"}""") }
            val favorites = games.take(3).map { it.id }
            favorites.forEach { request(HttpMethod.Put, "/favorites/$it") }
            request(HttpMethod.Put, "/favorites/${games.last().id}", subject = second)
            val seen = mutableListOf<String>()
            var cursor: String? = null
            do {
                val page =
                    Json.decodeFromString<LibraryPage>(
                        request(HttpMethod.Get, "/library?favorite=true&limit=2${cursor?.let { "&cursor=$it" }.orEmpty()}").bodyAsText(),
                    )
                seen += page.items.map { it.gameId }
                cursor = page.nextCursor
            } while (cursor != null)
            assertEquals(favorites.toSet(), seen.toSet())
            assertEquals(3, seen.size)
            val unfiltered = Json.decodeFromString<LibraryPage>(request(HttpMethod.Get, "/library?limit=1").bodyAsText())
            assertEquals(
                HttpStatusCode.BadRequest,
                request(HttpMethod.Get, "/library?favorite=true&cursor=${unfiltered.nextCursor}").status,
            )
            for (value in listOf("false", "1", "")) {
                assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Get, "/library?favorite=$value").status, value)
            }

            val catalogRequests = fixture.catalog.requests.size
            val unknown = UUID.randomUUID().toString()
            val lookup =
                Json.decodeFromString<dev.unplayd.userapi.library.LibraryLookup>(
                    request(
                        HttpMethod.Post,
                        "/library/lookup",
                        """{"gameIds":["${games[0].id}","${games[3].id}","$unknown"]}""",
                    ).bodyAsText(),
                )
            assertEquals(mapOf(games[0].id to true, games[3].id to false), lookup.items.associate { it.gameId to it.favorite })
            val foreign =
                Json.decodeFromString<dev.unplayd.userapi.library.LibraryLookup>(
                    request(HttpMethod.Post, "/library/lookup", """{"gameIds":["${games[0].id}"]}""", second).bodyAsText(),
                )
            assertEquals(emptyList(), foreign.items)
            val tooMany = List(51) { "\"${UUID.randomUUID()}\"" }.joinToString(",")
            for (body in listOf(
                """{"gameIds":[]}""",
                """{"gameIds":[$tooMany]}""",
                """{"gameIds":["$unknown","$unknown"]}""",
                """{"gameIds":["bad"]}""",
                "{}",
            )) {
                assertEquals(HttpStatusCode.BadRequest, request(HttpMethod.Post, "/library/lookup", body).status, body)
            }
            assertEquals(catalogRequests, fixture.catalog.requests.size, "Lookups must not call the catalog")
            assertEquals(
                HttpStatusCode.Unauthorized,
                request(HttpMethod.Post, "/library/lookup", """{"gameIds":["$unknown"]}""", null).status,
            )
        }

    @Test
    fun `concurrent favorites create one entry and races with removal stay consistent`() =
        withLibrary { fixture ->
            val game = fixture.catalog.add()
            val results = coroutineScope { List(8) { async { request(HttpMethod.Put, "/favorites/${game.id}") } }.map { it.await() } }
            assertTrue(results.all { it.status == HttpStatusCode.OK })
            assertEquals(1, results.map { it.entry().id }.distinct().size)
            fixture.database.connection().use { db ->
                assertEquals(1, db.count("SELECT count(*) FROM user_data.library_entries"))
                assertEquals(1, db.count("SELECT count(*) FROM user_data.game_statuses"))
                assertEquals(1, db.count("SELECT count(*) FROM user_data.favorites"))
            }
            repeat(10) {
                request(HttpMethod.Post, "/library", """{"gameId":"${game.id}","ownership":"wishlist"}""")
                val (favorite, removal) =
                    coroutineScope {
                        val favorite = async { request(HttpMethod.Put, "/favorites/${game.id}") }
                        val removal = async { request(HttpMethod.Delete, "/library/${game.id}") }
                        favorite.await() to removal.await()
                    }
                assertTrue(favorite.status in setOf(HttpStatusCode.OK, HttpStatusCode.Conflict), favorite.status.toString())
                assertEquals(HttpStatusCode.NoContent, removal.status)
                request(HttpMethod.Delete, "/library/${game.id}")
            }
            fixture.database.connection().use { db -> assertEquals(0, db.count("SELECT count(*) FROM user_data.favorites")) }
        }

    private data class Fixture(
        val database: UserApiTestDatabase,
        val catalog: CatalogStub,
    )

    private fun withLibrary(block: suspend ApplicationTestBuilder.(Fixture) -> Unit) {
        UserApiTestDatabase.start().use { database ->
            CatalogStub().use { catalog ->
                testApplication {
                    val users = JdbcUserStore(database.dataSource)
                    application {
                        module(
                            auth,
                            users,
                            auth.verifier(tokens.provider),
                            LibraryService(JdbcLibraryStore(database.dataSource), HttpCatalogClient(catalog.config())),
                        )
                    }
                    block(Fixture(database, catalog))
                }
            }
        }
    }

    private suspend fun ApplicationTestBuilder.request(
        method: HttpMethod,
        path: String,
        body: String? = null,
        subject: String? = first,
    ): HttpResponse =
        client
            .request(path) {
                this.method = method
                subject?.let { header(HttpHeaders.Authorization, "Bearer ${tokens.token(it)}") }
                header("X-User-ID", UUID.randomUUID().toString())
                header("X-Request-ID", "library-http-123")
                body?.let {
                    header(HttpHeaders.ContentType, "application/json")
                    setBody(it)
                }
            }.also {
                assertEquals("no-store", it.headers[HttpHeaders.CacheControl])
                assertEquals("library-http-123", it.headers["X-Request-ID"])
            }

    private suspend fun HttpResponse.entry() = Json.decodeFromString<LibraryEntry>(bodyAsText())

    private fun Connection.execute(
        sql: String,
        vararg values: Any,
    ) = prepareStatement(sql).use { query ->
        values.forEachIndexed { index, value -> query.setObject(index + 1, value) }
        query.executeUpdate()
    }

    private fun Connection.count(sql: String) =
        createStatement().use { query ->
            query.executeQuery(sql).use { rows ->
                assertTrue(rows.next())
                rows.getInt(1)
            }
        }
}
