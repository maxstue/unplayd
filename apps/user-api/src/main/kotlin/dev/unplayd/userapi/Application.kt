package dev.unplayd.userapi

import com.auth0.jwt.interfaces.JWTVerifier
import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.database.DatabaseConfig
import dev.unplayd.userapi.database.migrateDatabase
import dev.unplayd.userapi.identity.JdbcUserStore
import dev.unplayd.userapi.identity.MeResponse
import dev.unplayd.userapi.identity.UserStore
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.auth.HttpAuthHeader
import io.ktor.serialization.kotlinx.json.json
import io.ktor.server.application.Application
import io.ktor.server.application.ApplicationCallPipeline
import io.ktor.server.application.call
import io.ktor.server.application.install
import io.ktor.server.application.log
import io.ktor.server.auth.Authentication
import io.ktor.server.auth.authenticate
import io.ktor.server.auth.jwt.JWTPrincipal
import io.ktor.server.auth.jwt.jwt
import io.ktor.server.auth.principal
import io.ktor.server.engine.embeddedServer
import io.ktor.server.netty.Netty
import io.ktor.server.plugins.callid.CallId
import io.ktor.server.plugins.callid.callId
import io.ktor.server.plugins.contentnegotiation.ContentNegotiation
import io.ktor.server.plugins.statuspages.StatusPages
import io.ktor.server.response.header
import io.ktor.server.response.respond
import io.ktor.server.routing.get
import io.ktor.server.routing.route
import io.ktor.server.routing.routing
import kotlinx.serialization.Serializable
import java.util.UUID

fun main(args: Array<String>) {
    val database = DatabaseConfig.load()
    if (args.firstOrNull() == "migrate") {
        require(args.size <= 2) { "Usage: user-api migrate [targetVersion]" }
        migrateDatabase(database, args.getOrNull(1))
        return
    }
    require(args.isEmpty()) { "Usage: user-api [migrate [targetVersion]]" }
    val port = System.getenv("PORT")?.let { requireNotNull(it.toIntOrNull()) { "PORT must be an integer" } } ?: 8445
    require(port in 1..65535) { "PORT must be between 1 and 65535" }
    embeddedServer(Netty, host = "0.0.0.0", port = port) {
        module(users = JdbcUserStore(database))
    }.start(wait = true)
}

@Serializable
data class ErrorResponse(
    val error: ErrorDetails,
)

@Serializable
data class ErrorDetails(
    val code: String,
    val message: String,
    val requestId: String,
)

private fun errorResponse(
    message: String,
    status: Int,
    requestId: String?,
) = ErrorResponse(
    ErrorDetails(
        when (status) {
            400 -> "invalid_request"
            401 -> "unauthorized"
            404 -> "not_found"
            else -> "unavailable"
        },
        message,
        requestId.orEmpty(),
    ),
)

fun Application.module(
    auth: AuthConfig = AuthConfig.load(),
    users: UserStore = JdbcUserStore(DatabaseConfig.load()),
    verifier: JWTVerifier = auth.verifier(),
) {
    install(ContentNegotiation) { json() }
    install(CallId) {
        retrieveFromHeader("X-Request-ID")
        verify { it.matches(Regex("[a-zA-Z0-9_-]{1,64}")) }
        generate { UUID.randomUUID().toString() }
        replyToHeader("X-Request-ID")
    }
    intercept(ApplicationCallPipeline.Call) {
        call.response.header(HttpHeaders.CacheControl, "no-store")
    }
    install(StatusPages) {
        exception<Throwable> { call, cause ->
            if (cause is kotlinx.coroutines.CancellationException) throw cause
            this@module.log.error("User API request failed: {} (requestId={})", cause::class.simpleName, call.callId)
            call.respond(
                HttpStatusCode.ServiceUnavailable,
                errorResponse(message = "Service unavailable", status = 503, requestId = call.callId),
            )
        }
    }
    install(Authentication) {
        jwt("clerk") {
            realm = "unplayd"
            authHeader { call ->
                val value =
                    call.request.headers
                        .getAll(HttpHeaders.Authorization)
                        ?.singleOrNull()
                val token =
                    value?.takeIf { it.length <= 8192 }?.let {
                        Regex(
                            "Bearer ([a-zA-Z0-9_-]+\\.[a-zA-Z0-9_-]+\\.[a-zA-Z0-9_-]+)",
                            RegexOption.IGNORE_CASE,
                        ).matchEntire(it)?.groupValues?.get(1)
                    }
                token?.let { HttpAuthHeader.Single("Bearer", it) }
            }
            verifier(verifier)
            validate { credentials -> JWTPrincipal(credentials.payload) }
            challenge {
                _,
                _,
                ->
                call.respond(HttpStatusCode.Unauthorized, errorResponse(message = "Unauthorized", status = 401, requestId = call.callId))
            }
        }
    }
    routing {
        get("/health/live") { call.respond(mapOf("status" to "ok", "service" to "user-api")) }
        authenticate("clerk") {
            get("/me") {
                val user = users.resolve(requireNotNull(call.principal<JWTPrincipal>()).subject!!)
                call.respond(MeResponse(user.id.toString(), user.clerkSubject))
            }
            get("/library/{gameId}") {
                val gameId = runCatching { UUID.fromString(call.parameters["gameId"]) }.getOrNull()
                if (gameId == null) {
                    call.respond(
                        HttpStatusCode.BadRequest,
                        errorResponse(message = "Invalid game ID", status = 400, requestId = call.callId),
                    )
                    return@get
                }
                val user = users.resolve(requireNotNull(call.principal<JWTPrincipal>()).subject!!)
                val entry = users.findEntry(user.id, gameId)
                if (entry ==
                    null
                ) {
                    call.respond(HttpStatusCode.NotFound, errorResponse(message = "Not found", status = 404, requestId = call.callId))
                } else {
                    call.respond(entry)
                }
            }
            route("/{path...}") {
                handle {
                    call.respond(
                        HttpStatusCode.NotFound,
                        errorResponse(message = "Not found", status = 404, requestId = call.callId),
                    )
                }
            }
        }
    }
}
