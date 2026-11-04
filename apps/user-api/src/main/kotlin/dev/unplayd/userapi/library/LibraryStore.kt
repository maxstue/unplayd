package dev.unplayd.userapi.library

import dev.unplayd.userapi.database.DatabaseConfig
import dev.unplayd.userapi.identity.GameSnapshot
import dev.unplayd.userapi.identity.LibraryEntry
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.sql.Connection
import java.sql.DriverManager
import java.sql.ResultSet
import java.sql.Timestamp
import java.time.Instant
import java.util.UUID

internal const val ENTRY_SELECT = """
    SELECT e.*, s.status, (f.library_entry_id IS NOT NULL) AS favorite, r.value AS rating, n.text AS note
    FROM user_data.library_entries e
    JOIN user_data.game_statuses s ON s.library_entry_id=e.id
    LEFT JOIN user_data.favorites f ON f.library_entry_id=e.id
    LEFT JOIN user_data.ratings r ON r.library_entry_id=e.id
    LEFT JOIN user_data.notes n ON n.library_entry_id=e.id
"""

internal fun ResultSet.entry() =
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

data class AddedEntry(
    val entry: LibraryEntry,
    val created: Boolean,
)

class JdbcLibraryStore(
    private val config: DatabaseConfig,
) {
    suspend fun list(
        userId: UUID,
        filter: LibraryFilter,
    ): LibraryPage =
        read { db ->
            val conditions = mutableListOf("e.user_id=?")
            val values = mutableListOf<Any>(userId)
            filter.status?.let {
                conditions += "s.status=?"
                values += it
            }
            filter.ownership?.let {
                conditions += "e.ownership=?"
                values += it
            }
            filter.platformId?.let {
                conditions += "e.platform_id=?"
                values += uuid(it)
            }
            filter.after?.let {
                conditions += "(e.created_at,e.id) < (?,?)"
                values += Timestamp.from(Instant.parse(it.createdAt))
                values += uuid(it.id)
            }
            val items =
                db
                    .prepareStatement(
                        "$ENTRY_SELECT WHERE ${conditions.joinToString(" AND ")} ORDER BY e.created_at DESC,e.id DESC LIMIT ?",
                    ).use { query ->
                        values.forEachIndexed { index, value -> query.setObject(index + 1, value) }
                        query.setInt(values.size + 1, filter.limit + 1)
                        query.executeQuery().use { rows -> buildList { while (rows.next()) add(rows.entry()) } }
                    }
            val page = items.take(filter.limit)
            val cursor =
                if (items.size > filter.limit) {
                    page.last().let {
                        LibraryCursor(it.createdAt, it.id, filter.status, filter.ownership, filter.platformId).encode()
                    }
                } else {
                    null
                }
            LibraryPage(page, cursor)
        }

    suspend fun add(
        userId: UUID,
        request: AddLibraryEntry,
        snapshot: GameSnapshot,
    ): AddedEntry =
        transaction { db ->
            val created =
                db
                    .prepareStatement(
                        """
                        INSERT INTO user_data.library_entries(user_id,game_id,platform_id,ownership,snapshot_title,snapshot_cover_url)
                        VALUES (?,?,?,?,?,?) ON CONFLICT(user_id,game_id) DO NOTHING RETURNING id
                        """.trimIndent(),
                    ).use { query ->
                        query.setObject(1, userId)
                        query.setObject(2, uuid(request.gameId))
                        query.setObject(3, request.platformId?.let(::uuid))
                        query.setString(4, request.ownership)
                        query.setString(5, snapshot.title)
                        query.setString(6, snapshot.coverUrl)
                        query.executeQuery().use { rows -> if (rows.next()) rows.getObject(1, UUID::class.java) else null }
                    }
            if (created != null) {
                db.prepareStatement("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')").use {
                    it.setObject(1, created)
                    it.executeUpdate()
                }
            }
            val entry = requireNotNull(db.findLocked(userId, uuid(request.gameId)))
            ensureMatching(entry, request)
            AddedEntry(entry, created != null)
        }

    suspend fun setStatus(
        userId: UUID,
        gameId: UUID,
        status: String,
    ): LibraryEntry =
        transaction { db ->
            val entry = db.findLocked(userId, gameId) ?: throw LibraryProblem(404, "Not found")
            if (entry.status != status) {
                db.prepareStatement("UPDATE user_data.game_statuses SET status=?,updated_at=now() WHERE library_entry_id=?").use {
                    it.setString(1, status)
                    it.setObject(2, uuid(entry.id))
                    it.executeUpdate()
                }
                db.prepareStatement("UPDATE user_data.library_entries SET updated_at=now() WHERE id=?").use {
                    it.setObject(1, uuid(entry.id))
                    it.executeUpdate()
                }
            }
            requireNotNull(db.findLocked(userId, gameId))
        }

    suspend fun delete(
        userId: UUID,
        gameId: UUID,
    ) = read { db ->
        db.prepareStatement("DELETE FROM user_data.library_entries WHERE user_id=? AND game_id=?").use {
            it.setObject(1, userId)
            it.setObject(2, gameId)
            it.executeUpdate()
        }
        Unit
    }

    private fun Connection.findLocked(
        userId: UUID,
        gameId: UUID,
    ): LibraryEntry? {
        // Lock the parent first, then read its status with a fresh READ COMMITTED snapshot.
        // A joined SELECT FOR UPDATE can otherwise retain a status read before waiting for the parent lock.
        val exists =
            prepareStatement("SELECT id FROM user_data.library_entries WHERE user_id=? AND game_id=? FOR UPDATE").use {
                it.setObject(1, userId)
                it.setObject(2, gameId)
                it.executeQuery().use { rows -> rows.next() }
            }
        if (!exists) return null
        return prepareStatement("$ENTRY_SELECT WHERE e.user_id=? AND e.game_id=?").use {
            it.setObject(1, userId)
            it.setObject(2, gameId)
            it.executeQuery().use { rows -> if (rows.next()) rows.entry() else null }
        }
    }

    private suspend fun <T> read(block: (Connection) -> T): T =
        withContext(Dispatchers.IO) {
            DriverManager.getConnection(config.url, config.user, config.password).use(block)
        }

    private suspend fun <T> transaction(block: (Connection) -> T): T =
        read { db ->
            db.autoCommit = false
            try {
                block(db).also { db.commit() }
            } catch (failure: Throwable) {
                db.rollback()
                throw failure
            }
        }
}

internal fun ensureMatching(
    entry: LibraryEntry,
    request: AddLibraryEntry,
) {
    if (entry.ownership != request.ownership || entry.platformId != request.platformId?.let { uuid(it).toString() }) {
        throw LibraryProblem(409, "Game already exists with different ownership or platform")
    }
}
