package dev.unplayd.userapi.library

import dev.unplayd.userapi.identity.UserStore
import io.ktor.http.HttpStatusCode
import io.ktor.server.application.ApplicationCall
import io.ktor.server.application.call
import io.ktor.server.auth.jwt.JWTPrincipal
import io.ktor.server.auth.principal
import io.ktor.server.plugins.callid.callId
import io.ktor.server.request.receive
import io.ktor.server.response.respond
import io.ktor.server.routing.Route
import io.ktor.server.routing.delete
import io.ktor.server.routing.get
import io.ktor.server.routing.post
import io.ktor.server.routing.put

fun Route.libraryRoutes(
    users: UserStore,
    service: LibraryService,
) {
    suspend fun ApplicationCall.userId() = users.resolve(requireNotNull(principal<JWTPrincipal>()).subject!!).id

    get("/library/overview") { call.respond(service.overview(call.userId())) }
    get("/library/platforms") { call.respond(service.platforms(call.userId())) }
    get("/library/{gameId}") {
        val gameId = uuid(call.parameters["gameId"])
        val entry = service.findEntry(call.userId(), gameId) ?: throw LibraryProblem(404, "Not found")
        call.respond(entry)
    }
    get("/library") {
        val query = call.request.queryParameters
        val limit = query["limit"]?.let { it.toIntOrNull() ?: throw LibraryProblem(400, "Invalid limit") } ?: 20
        val filter = LibraryFilter(query["status"], query["ownership"], query["platformId"], limit, query["cursor"])
        call.respond(service.list(call.userId(), filter))
    }
    post("/library") {
        val request = call.receive<AddLibraryEntry>()
        request.validate()
        val result = service.add(call.userId(), request, requireNotNull(call.callId))
        call.respond(if (result.created) HttpStatusCode.Created else HttpStatusCode.OK, result.entry)
    }
    put("/library/{gameId}/status") {
        val gameId = uuid(call.parameters["gameId"])
        val status = call.receive<SetStatus>().status
        if (status !in statuses) throw LibraryProblem(400, "Invalid status")
        call.respond(service.setStatus(call.userId(), gameId, status))
    }
    delete("/library/{gameId}") {
        val gameId = uuid(call.parameters["gameId"])
        service.delete(call.userId(), gameId)
        call.respond(HttpStatusCode.NoContent)
    }
}
