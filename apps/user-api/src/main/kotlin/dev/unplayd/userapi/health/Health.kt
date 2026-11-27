package dev.unplayd.userapi.health

import io.ktor.http.HttpStatusCode
import io.ktor.server.application.log
import io.ktor.server.plugins.callid.callId
import io.ktor.server.response.respond
import io.ktor.server.routing.Route
import io.ktor.server.routing.application
import io.ktor.server.routing.get
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runInterruptible
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.Serializable
import javax.sql.DataSource

private const val SERVICE = "user-api"

/** Time budget of the readiness database check. */
const val READINESS_TIMEOUT_MS = 1_000L

/**
 * Reports whether the user database answers. The catalog is deliberately not checked:
 * a catalog or IGDB outage must not make stored library data unreachable.
 */
fun interface Readiness {
    suspend fun databaseReady(): Boolean
}

/** Readiness that borrows a pooled connection and validates it within [READINESS_TIMEOUT_MS]. */
fun DataSource.readiness() =
    Readiness {
        withTimeoutOrNull(READINESS_TIMEOUT_MS) {
            runInterruptible(Dispatchers.IO) {
                runCatching { connection.use { it.isValid(1) } }.getOrDefault(false)
            }
        } ?: false
    }

@Serializable
data class HealthResponse(
    val status: String,
    val service: String,
    val checks: Map<String, String>? = null,
)

/**
 * `GET /health/live` answers while the process serves HTTP; `GET /health/ready` additionally
 * requires the database and answers 503 without it. Both need no authentication.
 */
fun Route.healthRoutes(readiness: Readiness) {
    get("/health/live") { call.respond(HealthResponse("ok", SERVICE)) }
    get("/health/ready") {
        if (readiness.databaseReady()) {
            call.respond(HealthResponse("ok", SERVICE, mapOf("database" to "ok")))
        } else {
            application.log
                .atWarn()
                .addKeyValue("requestId", call.callId)
                .addKeyValue("check", "database")
                .log("readiness check failed")
            call.respond(
                HttpStatusCode.ServiceUnavailable,
                HealthResponse("unavailable", SERVICE, mapOf("database" to "unavailable")),
            )
        }
    }
}
