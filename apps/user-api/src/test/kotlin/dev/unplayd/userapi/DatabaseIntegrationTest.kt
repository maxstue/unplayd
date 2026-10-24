package dev.unplayd.userapi

import dev.unplayd.userapi.database.migrateDatabase
import dev.unplayd.userapi.identity.JdbcUserStore
import kotlinx.coroutines.runBlocking
import java.sql.Connection
import java.sql.SQLException
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class DatabaseIntegrationTest {
    @Test
    fun `V2 preserves V1 entries and backfills backlog without repeating changes`() {
        UserApiTestDatabase.start("1").use { database ->
            database.connection().use { db ->
                val user = db.addUser()
                val entry = db.addEntry(user)
                db.execute(
                    "UPDATE user_data.library_entries SET platform_id=?, snapshot_cover_url=?, created_at='2026-01-01T00:00:00Z', updated_at='2026-02-01T00:00:00Z' WHERE id=?",
                    UUID.randomUUID(),
                    "https://images.test/cover.png",
                    entry,
                )
                val before = db.row("SELECT * FROM user_data.library_entries WHERE id=?", entry)

                migrateDatabase(database.config)

                assertEquals(before, db.row("SELECT * FROM user_data.library_entries WHERE id=?", entry))
                assertEquals(listOf("backlog"), db.row("SELECT status FROM user_data.game_statuses WHERE library_entry_id=?", entry))
                db.execute("UPDATE user_data.game_statuses SET status='playing' WHERE library_entry_id=?", entry)
                db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Preserved note')", entry)

                migrateDatabase(database.config)

                assertEquals(before, db.row("SELECT * FROM user_data.library_entries WHERE id=?", entry))
                assertEquals(listOf(1L), db.row("SELECT count(*) FROM user_data.game_statuses"))
                assertEquals(listOf("playing"), db.row("SELECT status FROM user_data.game_statuses WHERE library_entry_id=?", entry))
                assertEquals(listOf("Preserved note"), db.row("SELECT text FROM user_data.notes WHERE library_entry_id=?", entry))
                assertEquals(
                    listOf(2L),
                    db.row("SELECT count(*) FROM user_data.flyway_schema_history WHERE success AND version IS NOT NULL"),
                )
            }
        }
    }

    @Test
    fun `a game is unique per user while different users can track it`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                val first = db.addUser()
                val second = db.addUser()
                val game = UUID.randomUUID()
                db.addEntry(first, game)

                assertSqlState("23505") { db.addEntry(first, game) }
                db.addEntry(second, game)

                assertEquals(
                    listOf(2L, 2L),
                    db.row("SELECT count(*), count(DISTINCT user_id) FROM user_data.library_entries WHERE game_id=?", game),
                )
            }
        }
    }

    @Test
    fun `ratings enforce both bounds and accept the endpoints`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                val entry = db.addEntry(db.addUser())
                for (rating in listOf(0, 11)) {
                    assertSqlState(
                        "23514",
                    ) { db.execute("INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,?)", entry, rating) }
                }
                for (rating in listOf(1, 10)) {
                    db.execute(
                        "INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,?) ON CONFLICT(library_entry_id) DO UPDATE SET value=EXCLUDED.value",
                        entry,
                        rating,
                    )
                    assertEquals(listOf(rating), db.row("SELECT value FROM user_data.ratings WHERE library_entry_id=?", entry))
                }
            }
        }
    }

    @Test
    fun `status ownership and note constraints reject invalid values and preserve valid state`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                val entry = db.addEntry(db.addUser())
                db.execute("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')", entry)
                val note = "a".repeat(10000)
                db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,?)", entry, note)

                for ((sql, value) in listOf(
                    "UPDATE user_data.game_statuses SET status=? WHERE library_entry_id=?" to "invalid",
                    "UPDATE user_data.library_entries SET ownership=? WHERE id=?" to "invalid",
                    "UPDATE user_data.notes SET text=? WHERE library_entry_id=?" to "a".repeat(10001),
                )) {
                    assertSqlState("23514") { db.execute(sql, value, entry) }
                }

                assertEquals(
                    listOf("owned", "backlog", note),
                    db.row(
                        "SELECT e.ownership,s.status,n.text FROM user_data.library_entries e JOIN user_data.game_statuses s ON s.library_entry_id=e.id JOIN user_data.notes n ON n.library_entry_id=e.id WHERE e.id=?",
                        entry,
                    ),
                )
            }
        }
    }

    @Test
    fun `library users and all personal rows require their local parent`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                assertSqlState("23503") { db.addEntry(UUID.randomUUID()) }
                for (sql in listOf(
                    "INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')",
                    "INSERT INTO user_data.favorites(library_entry_id) VALUES (?)",
                    "INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,5)",
                    "INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Orphan note')",
                )) {
                    assertSqlState("23503") { db.execute(sql, UUID.randomUUID()) }
                }
            }
        }
    }

    @Test
    fun `personal state changes remain independent in the persisted library view`() =
        runBlocking {
            UserApiTestDatabase.start().use { database ->
                database.connection().use { db ->
                    val user = db.addUser()
                    val game = UUID.randomUUID()
                    val entry = db.addEntry(user, game)
                    db.execute("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')", entry)
                    val store = JdbcUserStore(database.config)
                    val initial = assertNotNull(store.findEntry(user, game))

                    db.execute("INSERT INTO user_data.favorites(library_entry_id) VALUES (?)", entry)
                    db.execute("INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,10)", entry)
                    db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Personal note')", entry)
                    val enriched = initial.copy(favorite = true, rating = 10, note = "Personal note")
                    assertEquals(enriched, store.findEntry(user, game))

                    db.execute("UPDATE user_data.game_statuses SET status='playing' WHERE library_entry_id=?", entry)
                    assertEquals(enriched.copy(status = "playing"), store.findEntry(user, game))
                    db.execute("DELETE FROM user_data.favorites WHERE library_entry_id=?", entry)
                    assertEquals(enriched.copy(status = "playing", favorite = false), store.findEntry(user, game))
                    db.execute("UPDATE user_data.library_entries SET ownership='subscription' WHERE id=?", entry)
                    assertEquals(
                        enriched.copy(status = "playing", favorite = false, ownership = "subscription"),
                        store.findEntry(user, game),
                    )
                }
            }
        }

    @Test
    fun `failed compound transaction rolls back the entry and every personal row`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                val user = db.addUser()
                val entry = UUID.randomUUID()
                db.autoCommit = false
                try {
                    assertSqlState("23514") {
                        db.addEntry(user, entry = entry)
                        db.execute("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'backlog')", entry)
                        db.execute("INSERT INTO user_data.favorites(library_entry_id) VALUES (?)", entry)
                        db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Rolled back note')", entry)
                        db.execute("INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,0)", entry)
                        db.commit()
                    }
                } finally {
                    db.rollback()
                    db.autoCommit = true
                }
                for ((table, column) in listOf(
                    "library_entries" to "id",
                    "game_statuses" to "library_entry_id",
                    "favorites" to "library_entry_id",
                    "ratings" to "library_entry_id",
                    "notes" to "library_entry_id",
                )) {
                    assertEquals(listOf(0L), db.row("SELECT count(*) FROM user_data.$table WHERE $column=?", entry), table)
                }
                assertEquals(listOf(1L), db.row("SELECT count(*) FROM user_data.users WHERE id=?", user))
            }
        }
    }

    @Test
    fun `deleting an entry cascades its personal rows and preserves another users same game`() =
        runBlocking {
            UserApiTestDatabase.start().use { database ->
                database.connection().use { db ->
                    val first = db.addUser()
                    val second = db.addUser()
                    val game = UUID.randomUUID()
                    val removed = db.addEntry(first, game)
                    val retained = db.addEntry(second, game)
                    for (entry in listOf(removed, retained)) {
                        db.execute("INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES (?,'playing')", entry)
                        db.execute("INSERT INTO user_data.favorites(library_entry_id) VALUES (?)", entry)
                        db.execute("INSERT INTO user_data.ratings(library_entry_id,value) VALUES (?,8)", entry)
                        db.execute("INSERT INTO user_data.notes(library_entry_id,text) VALUES (?,'Kept for its owner')", entry)
                    }
                    val store = JdbcUserStore(database.config)
                    val before = assertNotNull(store.findEntry(second, game))

                    assertEquals(1, db.execute("DELETE FROM user_data.library_entries WHERE id=?", removed))

                    assertNull(store.findEntry(first, game))
                    assertEquals(before, store.findEntry(second, game))
                    for (table in listOf("game_statuses", "favorites", "ratings", "notes")) {
                        assertEquals(listOf(retained), db.row("SELECT library_entry_id FROM user_data.$table"), table)
                    }
                    assertEquals(listOf(2L), db.row("SELECT count(*) FROM user_data.users"))
                }
            }
        }

    @Test
    fun `bootstrap grants only the services own schema and denies foreign schema and table access`() {
        UserApiTestDatabase.start().use { database ->
            database.connection().use { db ->
                assertEquals(
                    listOf("user_api", "user_data", false),
                    db.row("SELECT current_user,current_schema(),rolsuper FROM pg_roles WHERE rolname=current_user"),
                )
                assertEquals(
                    listOf(true, true, false, false),
                    db.row(
                        "SELECT has_schema_privilege(current_user,'user_data','USAGE'),has_schema_privilege(current_user,'user_data','CREATE'),has_schema_privilege(current_user,'catalog','USAGE'),has_schema_privilege(current_user,'catalog','CREATE')",
                    ),
                )
                db.execute("CREATE TABLE user_data.integration_probe(id integer)")
                db.execute("INSERT INTO user_data.integration_probe VALUES (42)")
                assertEquals(listOf(42), db.row("SELECT id FROM user_data.integration_probe"))

                database.catalogConnection().use { catalog ->
                    catalog.execute("CREATE TABLE catalog.integration_probe(id integer)")
                    catalog.execute("INSERT INTO catalog.integration_probe VALUES (99)")
                    assertFalse(
                        catalog.row("SELECT has_table_privilege('user_api','catalog.integration_probe','SELECT')").single() as Boolean,
                    )
                    assertSqlState("42501") { db.row("SELECT * FROM catalog.integration_probe") }

                    // Isolate table permission from schema permission, then test schema denial with a table grant.
                    catalog.execute("GRANT USAGE ON SCHEMA catalog TO user_api")
                    assertTrue(db.row("SELECT has_schema_privilege(current_user,'catalog','USAGE')").single() as Boolean)
                    assertSqlState("42501") { db.row("SELECT * FROM catalog.integration_probe") }
                    catalog.execute("REVOKE USAGE ON SCHEMA catalog FROM user_api")
                    catalog.execute("GRANT SELECT ON catalog.integration_probe TO user_api")
                    assertTrue(
                        catalog.row("SELECT has_table_privilege('user_api','catalog.integration_probe','SELECT')").single() as Boolean,
                    )
                    assertSqlState("42501") { db.row("SELECT * FROM catalog.integration_probe") }
                    assertSqlState("42501") { db.execute("CREATE TABLE catalog.forbidden_probe(id integer)") }
                }
            }
        }
    }

    private fun Connection.addUser(): UUID {
        val user = UUID.randomUUID()
        execute("INSERT INTO user_data.users(id,clerk_subject) VALUES (?,?)", user, "user_$user")
        return user
    }

    private fun Connection.addEntry(
        user: UUID,
        game: UUID = UUID.randomUUID(),
        entry: UUID = UUID.randomUUID(),
    ): UUID {
        execute(
            "INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES (?,?,?,'owned','Fixture game')",
            entry,
            user,
            game,
        )
        return entry
    }

    private fun Connection.execute(
        sql: String,
        vararg values: Any,
    ): Int =
        prepareStatement(sql).use { query ->
            values.forEachIndexed { index, value -> query.setObject(index + 1, value) }
            query.executeUpdate()
        }

    private fun Connection.row(
        sql: String,
        vararg values: Any,
    ): List<Any?> =
        prepareStatement(sql).use { query ->
            values.forEachIndexed { index, value -> query.setObject(index + 1, value) }
            query.executeQuery().use { rows ->
                assertTrue(rows.next(), "Expected one row: $sql")
                val result = (1..rows.metaData.columnCount).map { rows.getObject(it) }
                assertFalse(rows.next(), "Expected only one row: $sql")
                result
            }
        }

    private fun assertSqlState(
        expected: String,
        block: () -> Unit,
    ) {
        assertEquals(expected, assertFailsWith<SQLException>(block = block).sqlState)
    }
}
