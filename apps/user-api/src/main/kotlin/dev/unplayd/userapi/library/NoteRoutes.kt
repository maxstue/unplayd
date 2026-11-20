package dev.unplayd.userapi.library

import dev.unplayd.userapi.identity.UserStore
import io.ktor.http.HttpStatusCode
import io.ktor.server.application.call
import io.ktor.server.plugins.callid.callId
import io.ktor.server.request.receive
import io.ktor.server.response.respond
import io.ktor.server.routing.Route
import io.ktor.server.routing.delete
import io.ktor.server.routing.put
import io.ktor.server.routing.route

internal fun Route.noteRoutes(
    users: UserStore,
    service: LibraryService,
) = route("/notes/{gameId}") {
    put {
        val gameId = call.gameId()
        val request = call.receive<SetNote>()
        request.validate()
        call.respond(service.note(call.userId(users), gameId, request.text, requireNotNull(call.callId)))
    }
    delete {
        val gameId = call.gameId()
        service.removeNote(call.userId(users), gameId)
        call.respond(HttpStatusCode.NoContent)
    }
}
