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
        val game = activeGame(id, requestId)
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

    suspend fun lookup(
        userId: UUID,
        gameIds: List<UUID>,
    ) = LibraryLookup(store.lookup(userId, gameIds))

    suspend fun favorite(
        userId: UUID,
        gameId: UUID,
        requestId: String,
    ) = store.setFavorite(userId, gameId, snapshotIfAbsent(userId, gameId, requestId))

    suspend fun unfavorite(
        userId: UUID,
        gameId: UUID,
    ) = store.removeFavorite(userId, gameId)

    suspend fun rate(
        userId: UUID,
        gameId: UUID,
        value: Int,
        requestId: String,
    ): LibraryEntry {
        SetRating(value).validate()
        return store.setRating(userId, gameId, value, snapshotIfAbsent(userId, gameId, requestId))
    }

    suspend fun unrate(
        userId: UUID,
        gameId: UUID,
    ) = store.removeRating(userId, gameId)

    suspend fun note(
        userId: UUID,
        gameId: UUID,
        text: String,
        requestId: String,
    ): LibraryEntry {
        SetNote(text).validate()
        return store.setNote(userId, gameId, text, snapshotIfAbsent(userId, gameId, requestId))
    }

    suspend fun removeNote(
        userId: UUID,
        gameId: UUID,
    ) = store.removeNote(userId, gameId)

    /** Existing entries need no catalog call, so personal data stays writable during catalog outages. */
    private suspend fun snapshotIfAbsent(
        userId: UUID,
        gameId: UUID,
        requestId: String,
    ) = if (store.findEntry(userId, gameId) == null) {
        activeGame(gameId, requestId).let { GameSnapshot(it.title, it.coverUrl) }
    } else {
        null
    }

    private suspend fun activeGame(
        id: UUID,
        requestId: String,
    ) = catalog.game(id, requestId).also { if (!it.isActive) throw LibraryProblem(400, "Unknown or inactive game") }
}
