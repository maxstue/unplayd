package dev.unplayd.userapi.library

import dev.unplayd.userapi.catalog.CatalogClient
import java.util.UUID

class LibraryService(
    private val store: LibraryStore,
    private val catalog: CatalogClient,
) {
    suspend fun add(
        userId: UUID,
        request: AddLibraryEntry,
        requestId: String,
    ): AddedEntry {
        request.validate()
        val id = uuid(request.gameId)
        store.findEntry(userId, id)?.let {
            ensureMatching(it, request)
            return AddedEntry(it, false)
        }
        val game = catalog.game(id, requestId)
        if (!game.isActive) throw LibraryProblem(400, "Unknown or inactive game")
        if (request.platformId != null && game.platforms.none { uuid(it.id) == uuid(request.platformId) }) {
            throw LibraryProblem(400, "Platform is not available for this game")
        }
        return store.add(userId, request, GameSnapshot(game.title, game.coverUrl))
    }

    suspend fun findEntry(
        userId: UUID,
        gameId: UUID,
    ) = store.findEntry(userId, gameId)

    suspend fun overview(userId: UUID) = store.overview(userId)

    suspend fun platforms(userId: UUID) = store.platforms(userId)

    suspend fun list(
        userId: UUID,
        filter: LibraryFilter,
    ) = store.list(userId, filter)

    suspend fun setStatus(
        userId: UUID,
        gameId: UUID,
        status: String,
    ) = if (status in statuses) store.setStatus(userId, gameId, status) else throw LibraryProblem(400, "Invalid status")

    suspend fun delete(
        userId: UUID,
        gameId: UUID,
    ) = store.delete(userId, gameId)
}
