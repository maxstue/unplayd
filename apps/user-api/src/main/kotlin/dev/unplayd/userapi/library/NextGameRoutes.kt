package dev.unplayd.userapi.library

import dev.unplayd.userapi.identity.UserStore
import io.ktor.server.application.call
import io.ktor.server.response.respond
import io.ktor.server.routing.Route
import io.ktor.server.routing.get

internal fun Route.nextGameRoutes(
    users: UserStore,
    service: LibraryService,
) = get("/next-game") {
    val query = call.request.queryParameters
    val platformId = query["platformId"]?.let(::uuid)
    val excludeGameId = query["excludeGameId"]?.let(::uuid)
    call.respond(service.drawNextGame(call.userId(users), platformId, excludeGameId))
}
