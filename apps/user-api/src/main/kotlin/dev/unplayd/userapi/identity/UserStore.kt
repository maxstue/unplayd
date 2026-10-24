package dev.unplayd.userapi.identity

import dev.unplayd.userapi.database.DatabaseConfig
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import java.sql.DriverManager
import java.sql.ResultSet
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
                        """
                        SELECT e.*, s.status, (f.library_entry_id IS NOT NULL) AS favorite, r.value AS rating, n.text AS note
                        FROM user_data.library_entries e
                        JOIN user_data.game_statuses s ON s.library_entry_id=e.id
                        LEFT JOIN user_data.favorites f ON f.library_entry_id=e.id
                        LEFT JOIN user_data.ratings r ON r.library_entry_id=e.id
                        LEFT JOIN user_data.notes n ON n.library_entry_id=e.id
                        WHERE e.user_id=? AND e.game_id=?
                        """.trimIndent(),
                    ).use { query ->
                        query.setObject(1, userId)
                        query.setObject(2, gameId)
                        query.executeQuery().use { rows -> if (rows.next()) rows.entry() else null }
                    }
            }
        }

    private fun <T> connection(block: (java.sql.Connection) -> T): T =
        DriverManager.getConnection(config.url, config.user, config.password).use(block)

    private fun ResultSet.entry() =
        LibraryEntry(
            id = getString("id"),
            gameId = getString("game_id"),
            platformId = getString("platform_id"),
            ownership = getString("ownership"),
            status = getString("status"),
            favorite = getBoolean("favorite"),
            rating = getObject("rating") as Int?,
            note = getString("note"),
            snapshot = GameSnapshot(getString("snapshot_title"), getString("snapshot_cover_url")),
            createdAt = getTimestamp("created_at").toInstant().toString(),
            updatedAt = getTimestamp("updated_at").toInstant().toString(),
        )
}
