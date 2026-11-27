package dev.unplayd.userapi.catalog

import dev.unplayd.userapi.library.LibraryProblem
import kotlinx.coroutines.future.await
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.slf4j.LoggerFactory
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.time.Duration
import java.util.UUID

class CatalogConfig private constructor(
    val baseUrl: URI,
    val token: String,
    val timeout: Duration,
) {
    companion object {
        fun load(env: (String) -> String? = System::getenv): CatalogConfig {
            val uri = env("CATALOG_API_URL")?.let { runCatching { URI(it) }.getOrNull() }
            require(
                uri != null &&
                    uri.scheme in setOf("http", "https") &&
                    uri.host != null &&
                    uri.userInfo == null &&
                    uri.rawQuery == null &&
                    uri.rawFragment == null &&
                    uri.path in setOf("", "/") &&
                    (uri.port == -1 || uri.port in 1..65535),
            ) {
                "CATALOG_API_URL must be an HTTP(S) origin without credentials, query or fragment"
            }
            val token = env("CATALOG_SERVICE_TOKEN")
            require(!token.isNullOrBlank() && token.none { it.isWhitespace() || it.isISOControl() }) { "CATALOG_SERVICE_TOKEN is required" }
            val timeout = env("CATALOG_TIMEOUT_MS")?.let { requireNotNull(it.toLongOrNull()) { "Invalid CATALOG_TIMEOUT_MS" } } ?: 3000
            require(timeout in 1..30000) { "CATALOG_TIMEOUT_MS must be between 1 and 30000" }
            return CatalogConfig(uri, token, Duration.ofMillis(timeout))
        }
    }
}

@Serializable
data class CatalogPlatform(
    val id: String,
)

@Serializable
data class CatalogGame(
    val id: String,
    val title: String,
    val coverUrl: String?,
    val platforms: List<CatalogPlatform>,
    val isActive: Boolean,
)

@Serializable
private data class CatalogDetail(
    val game: CatalogGame,
)

fun interface CatalogClient {
    suspend fun game(
        id: UUID,
        requestId: String,
    ): CatalogGame
}

class HttpCatalogClient(
    private val config: CatalogConfig,
) : CatalogClient {
    private val client =
        HttpClient
            .newBuilder()
            .connectTimeout(config.timeout)
            .followRedirects(HttpClient.Redirect.NEVER)
            .build()
    private val json = Json { ignoreUnknownKeys = true }
    private val logger = LoggerFactory.getLogger(HttpCatalogClient::class.java)

    /** Fetches a game and writes one entry with duration, status and outcome; never the token or body. */
    override suspend fun game(
        id: UUID,
        requestId: String,
    ): CatalogGame {
        val started = System.nanoTime()
        var status: Int? = null
        var outcome = "unavailable"
        try {
            return fetch(id, requestId) { status = it }.also { outcome = "ok" }
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            outcome = "cancelled"
            throw cancelled
        } catch (problem: LibraryProblem) {
            if (problem.status == 400) outcome = "unknown_game"
            throw problem
        } finally {
            val entry = if (outcome == "unavailable") logger.atWarn() else logger.atInfo()
            entry
                .addKeyValue("requestId", requestId)
                .addKeyValue("upstream", "catalog-api")
                .addKeyValue("operation", "game")
                .addKeyValue("status", status)
                .addKeyValue("outcome", outcome)
                .addKeyValue("durationMs", (System.nanoTime() - started) / 1_000_000)
                .log("upstream call")
        }
    }

    private suspend fun fetch(
        id: UUID,
        requestId: String,
        onStatus: (Int) -> Unit,
    ): CatalogGame {
        val request =
            HttpRequest
                .newBuilder(config.baseUrl.resolve("/games/$id"))
                .timeout(config.timeout)
                .header("Authorization", "Bearer ${config.token}")
                .header("X-Request-ID", requestId)
                .header("Accept", "application/json")
                .GET()
                .build()
        val response =
            try {
                client.sendAsync(request, HttpResponse.BodyHandlers.ofString()).await()
            } catch (cancelled: kotlinx.coroutines.CancellationException) {
                throw cancelled
            } catch (_: Exception) {
                throw LibraryProblem(503, "Catalog unavailable")
            }
        onStatus(response.statusCode())
        if (response.statusCode() == 404) throw LibraryProblem(400, "Unknown or inactive game")
        if (response.statusCode() != 200) throw LibraryProblem(503, "Catalog unavailable")
        val game =
            try {
                json.decodeFromString<CatalogDetail>(response.body()).game.also {
                    require(it.id == id.toString() && it.title.isNotBlank())
                    it.platforms.forEach { platform -> UUID.fromString(platform.id) }
                }
            } catch (_: Exception) {
                throw LibraryProblem(503, "Catalog unavailable")
            }
        if (!game.isActive) throw LibraryProblem(400, "Unknown or inactive game")
        return game
    }
}
