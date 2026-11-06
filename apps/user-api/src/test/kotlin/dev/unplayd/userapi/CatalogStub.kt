package dev.unplayd.userapi

import com.sun.net.httpserver.HttpServer
import dev.unplayd.userapi.catalog.CatalogConfig
import dev.unplayd.userapi.catalog.CatalogGame
import dev.unplayd.userapi.catalog.CatalogPlatform
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.net.InetSocketAddress
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executors

internal class CatalogStub : AutoCloseable {
    private val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
    private val executor = Executors.newCachedThreadPool()
    val games = ConcurrentHashMap<UUID, CatalogGame>()
    val requests = CopyOnWriteArrayList<Pair<String?, String?>>()
    val token = UUID.randomUUID().toString()

    @Volatile var status: Int? = null

    @Volatile var body: String? = null

    @Volatile var delayMs = 0L

    init {
        server.executor = executor
        server.createContext("/games/") { exchange ->
            exchange.use {
                requests += exchange.requestHeaders.getFirst("Authorization") to exchange.requestHeaders.getFirst("X-Request-ID")
                if (delayMs > 0) Thread.sleep(delayMs)
                val id = runCatching { UUID.fromString(exchange.requestURI.path.substringAfterLast('/')) }.getOrNull()
                val game = id?.let(games::get)
                val response =
                    body ?: game?.let {
                        buildJsonObject {
                            put("game", Json.encodeToJsonElement(CatalogGame.serializer(), it))
                            put("children", kotlinx.serialization.json.JsonArray(emptyList()))
                        }.toString()
                    } ?: "{}"
                exchange.responseHeaders.set("Content-Type", "application/json")
                exchange.responseHeaders.set("Location", "http://127.0.0.1:${server.address.port}/games/$id")
                val bytes = response.toByteArray()
                exchange.sendResponseHeaders(status ?: if (game != null) 200 else 404, bytes.size.toLong())
                exchange.responseBody.write(bytes)
            }
        }
        server.start()
    }

    fun config(timeout: Int = 2000) =
        CatalogConfig.load(
            mapOf(
                "CATALOG_API_URL" to "http://127.0.0.1:${server.address.port}",
                "CATALOG_SERVICE_TOKEN" to token,
                "CATALOG_TIMEOUT_MS" to timeout.toString(),
            )::get,
        )

    fun add(
        platform: UUID? = null,
        active: Boolean = true,
    ): CatalogGame {
        val id = UUID.randomUUID()
        return CatalogGame(
            id.toString(),
            "Catalog title $id",
            "https://images.test/$id.jpg",
            listOfNotNull(platform?.let { CatalogPlatform(it.toString()) }),
            active,
        ).also { games[id] = it }
    }

    override fun close() {
        server.stop(0)
        executor.shutdownNow()
    }
}
