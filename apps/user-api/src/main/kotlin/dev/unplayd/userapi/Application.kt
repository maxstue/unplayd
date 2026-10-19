package dev.unplayd.userapi

import io.ktor.serialization.kotlinx.json.json
import io.ktor.server.application.Application
import io.ktor.server.application.call
import io.ktor.server.application.install
import io.ktor.server.engine.embeddedServer
import io.ktor.server.netty.Netty
import io.ktor.server.plugins.callid.CallId
import io.ktor.server.plugins.contentnegotiation.ContentNegotiation
import io.ktor.server.response.respond
import io.ktor.server.routing.get
import io.ktor.server.routing.routing
import java.util.UUID

fun main() {
    embeddedServer(Netty, host = "0.0.0.0", port = System.getenv("PORT")?.toInt() ?: 8081) {
        module()
    }.start(wait = true)
}

fun Application.module() {
    install(ContentNegotiation) { json() }
    install(CallId) {
        retrieveFromHeader("X-Request-ID")
        verify { it.matches(Regex("[a-zA-Z0-9_-]{1,64}")) }
        generate { UUID.randomUUID().toString() }
        replyToHeader("X-Request-ID")
    }
    routing {
        get("/health/live") { call.respond(mapOf("status" to "ok", "service" to "user-api")) }
    }
}
