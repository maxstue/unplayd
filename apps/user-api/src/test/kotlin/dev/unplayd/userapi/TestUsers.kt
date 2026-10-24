package dev.unplayd.userapi

import dev.unplayd.userapi.identity.LibraryEntry
import dev.unplayd.userapi.identity.LocalUser
import dev.unplayd.userapi.identity.UserStore
import java.util.UUID

internal class TestUsers(
    private val users: List<LocalUser> = emptyList(),
    private val entries: Map<Pair<UUID, UUID>, LibraryEntry> = emptyMap(),
    private val failure: RuntimeException? = null,
) : UserStore {
    val subjects = mutableListOf<String>()
    val lookups = mutableListOf<Pair<UUID, UUID>>()

    override suspend fun resolve(subject: String): LocalUser {
        subjects += subject
        failure?.let { throw it }
        return users.single { it.clerkSubject == subject }
    }

    override suspend fun findEntry(
        userId: UUID,
        gameId: UUID,
    ): LibraryEntry? {
        lookups += userId to gameId
        return entries[userId to gameId]
    }
}
