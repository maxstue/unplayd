package dev.unplayd.userapi.identity

import dev.unplayd.userapi.database.DatabaseConfig
import dev.unplayd.userapi.library.ENTRY_SELECT
import dev.unplayd.userapi.library.entry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import java.sql.DriverManager
import java.util.UUID

data class LocalUser(
    val id: UUID,
    val clerkSubject: String,
)

@Serializable
data class MeResponse(
    val id: String,
    val clerkSubject: String,
)

@Serializable
data class GameSnapshot(
    val title: String,
    val coverUrl: String?,
)

@Serializable
data class LibraryEntry(
    val id: String,
    val gameId: String,
    val platformId: String?,
    val ownership: String,
    val status: String,
    val favorite: Boolean,
    val rating: Int?,
    val note: String?,
    val snapshot: GameSnapshot,
    val createdAt: String,
    val updatedAt: String,
)

interface UserStore {
    suspend fun resolve(subject: String): LocalUser

    suspend fun findEntry(
        userId: UUID,
        gameId: UUID,
    ): LibraryEntry?
}

class JdbcUserStore(
    private val config: DatabaseConfig,
) : UserStore {
    override suspend fun resolve(subject: String): LocalUser =
        withContext(Dispatchers.IO) {
            connection { db ->
                db
                    .prepareStatement(
                        "INSERT INTO user_data.users(clerk_subject) VALUES (?) ON CONFLICT(clerk_subject) DO UPDATE SET clerk_subject=EXCLUDED.clerk_subject RETURNING id,clerk_subject",
                    ).use { query ->
                        query.setString(1, subject)
                        query.executeQuery().use { rows ->
                            check(rows.next()) { "Unable to resolve local user" }
                            LocalUser(rows.getObject("id", UUID::class.java), rows.getString("clerk_subject"))
                        }
                    }
            }
        }

    override suspend fun findEntry(
        userId: UUID,
        gameId: UUID,
    ): LibraryEntry? =
        withContext(Dispatchers.IO) {
            connection { db ->
                db
                    .prepareStatement(
                        "$ENTRY_SELECT WHERE e.user_id=? AND e.game_id=?",
                    ).use { query ->
                        query.setObject(1, userId)
                        query.setObject(2, gameId)
                        query.executeQuery().use { rows -> if (rows.next()) rows.entry() else null }
                    }
            }
        }

    private fun <T> connection(block: (java.sql.Connection) -> T): T =
        DriverManager.getConnection(config.url, config.user, config.password).use(block)
}
