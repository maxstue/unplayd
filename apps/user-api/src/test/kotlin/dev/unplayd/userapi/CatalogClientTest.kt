package dev.unplayd.userapi

import dev.unplayd.userapi.catalog.CatalogConfig
import dev.unplayd.userapi.catalog.HttpCatalogClient
import dev.unplayd.userapi.library.LibraryProblem
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class CatalogClientTest {
    @Test
    fun `configuration rejects invalid origins credentials and timeout values`() {
        val valid = mapOf("CATALOG_API_URL" to "http://catalog-api:8446", "CATALOG_SERVICE_TOKEN" to UUID.randomUUID().toString())
        assertEquals(3000L, CatalogConfig.load(valid::get).timeout.toMillis())
        for (url in listOf(
            "",
            "bad",
            "file:///tmp/catalog",
            "https://user:password@catalog.test",
            "https://catalog.test/path",
            "https://catalog.test?query",
            "https://catalog.test#fragment",
            "http://catalog.test:99999",
        )) {
            assertFailsWith<IllegalArgumentException> { CatalogConfig.load((valid + ("CATALOG_API_URL" to url))::get) }
        }
        for (name in valid.keys) assertFailsWith<IllegalArgumentException> { CatalogConfig.load((valid - name)::get) }
        for (token in listOf("", " ", "bad\ntoken")) {
            assertFailsWith<IllegalArgumentException> { CatalogConfig.load((valid + ("CATALOG_SERVICE_TOKEN" to token))::get) }
        }
        for (timeout in listOf("0", "30001", "invalid")) {
            assertFailsWith<IllegalArgumentException> { CatalogConfig.load((valid + ("CATALOG_TIMEOUT_MS" to timeout))::get) }
        }
    }

    @Test
    fun `upstream timeout and connection failure are sanitized and cancellation propagates`() =
        runBlocking {
            val id: UUID
            val client: HttpCatalogClient
            CatalogStub().use { server ->
                id = UUID.fromString(server.add().id)
                client = HttpCatalogClient(server.config(100))
                server.delayMs = 1000
                val failure = assertFailsWith<LibraryProblem> { client.game(id, "timeout-123") }
                assertEquals(503, failure.status)
                assertEquals("Catalog unavailable", failure.message)
                val cancellable = HttpCatalogClient(server.config(5000))
                val count = server.requests.size
                val pending = async { cancellable.game(id, "cancel-123") }
                withTimeout(3000) { while (server.requests.size == count) delay(10) }
                pending.cancelAndJoin()
                assertFailsWith<CancellationException> { pending.await() }
            }
            assertEquals(503, assertFailsWith<LibraryProblem> { client.game(id, "closed-123") }.status)
        }

    @Test
    fun `invalid successful responses and redirects fail closed`() =
        runBlocking {
            CatalogStub().use { server ->
                val game = server.add()
                val id = UUID.fromString(game.id)
                val client = HttpCatalogClient(server.config())
                for (body in listOf(
                    "not-json",
                    "{}",
                    """{"game":{"id":"${UUID.randomUUID()}","title":"Wrong game","coverUrl":null,"platforms":[],"isActive":true}}""",
                )) {
                    server.body = body
                    assertEquals(503, assertFailsWith<LibraryProblem> { client.game(id, "invalid-123") }.status)
                }
                server.body = null
                server.status = 302
                val before = server.requests.size
                assertEquals(503, assertFailsWith<LibraryProblem> { client.game(id, "redirect-123") }.status)
                assertEquals(before + 1, server.requests.size)
                assertTrue(server.requests.all { it.first == "Bearer ${server.token}" })
            }
        }
}
