package dev.unplayd.userapi.library

import dev.unplayd.userapi.identity.UserStore
import io.ktor.http.HttpStatusCode
import io.ktor.server.application.call
import io.ktor.server.plugins.callid.callId
import io.ktor.server.response.respond
import io.ktor.server.routing.Route
import io.ktor.server.routing.delete
import io.ktor.server.routing.put
import io.ktor.server.routing.route

internal fun Route.favoriteRoutes(
    users: UserStore,
    service: LibraryService,
) = route("/favorites/{gameId}") {
    put {
        val gameId = call.gameId()
        call.respond(service.favorite(call.userId(users), gameId, requireNotNull(call.callId)))
    }
    delete {
        val gameId = call.gameId()
        service.unfavorite(call.userId(users), gameId)
        call.respond(HttpStatusCode.NoContent)
    }
}
