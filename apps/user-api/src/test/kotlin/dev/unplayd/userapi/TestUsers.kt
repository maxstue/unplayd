package dev.unplayd.userapi

import com.auth0.jwt.interfaces.JWTVerifier
import dev.unplayd.userapi.auth.AuthConfig
import dev.unplayd.userapi.catalog.CatalogClient
import dev.unplayd.userapi.identity.LocalUser
import dev.unplayd.userapi.identity.UserStore
import dev.unplayd.userapi.library.AddLibraryEntry
import dev.unplayd.userapi.library.GameSnapshot
import dev.unplayd.userapi.library.LibraryEntry
import dev.unplayd.userapi.library.LibraryFilter
import dev.unplayd.userapi.library.LibraryService
import dev.unplayd.userapi.library.LibraryStore
import io.ktor.server.application.Application
import java.util.UUID

internal class TestUsers(
    private val users: List<LocalUser> = emptyList(),
    private val entries: Map<Pair<UUID, UUID>, LibraryEntry> = emptyMap(),
    private val failure: RuntimeException? = null,
) : UserStore {
    val subjects = mutableListOf<String>()
    val lookups = mutableListOf<Pair<UUID, UUID>>()
    val library: LibraryStore =
        object : LibraryStore {
            override suspend fun findEntry(
                userId: UUID,
                gameId: UUID,
            ): LibraryEntry? {
                lookups += userId to gameId
                return entries[userId to gameId]
            }

            override suspend fun list(
                userId: UUID,
                filter: LibraryFilter,
            ) = error("Not used by identity tests")

            override suspend fun overview(userId: UUID) = error("Not used by identity tests")

            override suspend fun platforms(userId: UUID) = error("Not used by identity tests")

            override suspend fun add(
                userId: UUID,
                request: AddLibraryEntry,
                snapshot: GameSnapshot,
            ) = error("Not used by identity tests")

            override suspend fun setStatus(
                userId: UUID,
                gameId: UUID,
                status: String,
            ) = error("Not used by identity tests")

            override suspend fun delete(
                userId: UUID,
                gameId: UUID,
            ) = error("Not used by identity tests")

            override suspend fun lookup(
                userId: UUID,
                gameIds: List<UUID>,
            ) = error("Not used by identity tests")

            override suspend fun setFavorite(
                userId: UUID,
                gameId: UUID,
                snapshot: GameSnapshot?,
            ) = error("Not used by identity tests")

            override suspend fun removeFavorite(
                userId: UUID,
                gameId: UUID,
            ) = error("Not used by identity tests")
        }

    override suspend fun resolve(subject: String): LocalUser {
        subjects += subject
        failure?.let { throw it }
        return users.single { it.clerkSubject == subject }
    }
}

internal fun Application.testModule(
    auth: AuthConfig,
    users: TestUsers,
    verifier: JWTVerifier,
) {
    module(auth, users, verifier, LibraryService(users.library, CatalogClient { _, _ -> error("Not used by identity tests") }))
}
