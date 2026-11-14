package dev.unplayd.userapi

import dev.unplayd.userapi.database.DatabaseConfig
import dev.unplayd.userapi.database.dataSource
import dev.unplayd.userapi.identity.JdbcUserStore
import dev.unplayd.userapi.library.AddLibraryEntry
import dev.unplayd.userapi.library.GameSnapshot
import dev.unplayd.userapi.library.JdbcLibraryStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import java.sql.SQLException
import java.sql.SQLTransientConnectionException
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

class DataSourceIntegrationTest {
    @Test
    fun `pool reuses connections and rolls back unfinished transactions before reuse`() {
        UserApiTestDatabase.start().use { database ->
            val subject = "user_${UUID.randomUUID().toString().replace("-", "")}"
            val backend =
                database.dataSource.connection.use { db ->
                    val id =
                        db.createStatement().use { query ->
                            query.executeQuery("SELECT pg_backend_pid()").use { rows ->
                                rows.next()
                                rows.getInt(1)
                            }
                        }
                    db.autoCommit = false
                    db.prepareStatement("INSERT INTO user_data.users(clerk_subject) VALUES (?)").use {
                        it.setString(1, subject)
                        it.executeUpdate()
                    }
                    id
                }
            database.dataSource.connection.use { db ->
                assertTrue(db.autoCommit)
                db.createStatement().use { query ->
                    query.executeQuery("SELECT pg_backend_pid(),count(*) FROM user_data.users").use { rows ->
                        rows.next()
                        assertEquals(backend, rows.getInt(1))
                        assertEquals(0, rows.getInt(2))
                    }
                    query.executeQuery("SHOW statement_timeout").use { rows ->
                        rows.next()
                        assertEquals("2s", rows.getString(1))
                    }
                    query.executeQuery("SHOW lock_timeout").use { rows ->
                        rows.next()
                        assertEquals("1s", rows.getString(1))
                    }
                }
            }
        }
    }

    @Test
    fun `pool acquisition and row lock waits are bounded while existing user reads stay read only`() =
        runBlocking {
            UserApiTestDatabase.start().use { database ->
                val config =
                    DatabaseConfig.load(
                        mapOf(
                            "DATABASE_URL" to database.config.url,
                            "DATABASE_PASSWORD" to database.config.password,
                            "DATABASE_POOL_SIZE" to "1",
                            "DATABASE_CONNECTION_TIMEOUT_MS" to "250",
                            "DATABASE_LOCK_TIMEOUT_MS" to "100",
                        )::get,
                    )
                config.dataSource().use { pool ->
                    pool.connection.use {
                        withTimeout(5000) {
                            withContext(Dispatchers.IO) { assertFailsWith<SQLTransientConnectionException> { pool.connection } }
                        }
                    }
                    val users = JdbcUserStore(pool)
                    val user = users.resolve("user_${UUID.randomUUID().toString().replace("-", "")}")
                    val library = JdbcLibraryStore(pool)
                    val game = UUID.randomUUID()
                    library.add(user.id, AddLibraryEntry(game.toString(), "owned"), GameSnapshot("Game", null))
                    database.connection().use { blocker ->
                        blocker.autoCommit = false
                        blocker.prepareStatement("SELECT id FROM user_data.users WHERE id=? FOR UPDATE").use {
                            it.setObject(1, user.id)
                            it.executeQuery().close()
                        }
                        assertEquals(user, withTimeout(5000) { users.resolve(user.clerkSubject) })
                        blocker.prepareStatement("SELECT id FROM user_data.library_entries WHERE user_id=? FOR UPDATE").use {
                            it.setObject(1, user.id)
                            it.executeQuery().close()
                        }
                        val error = withTimeout(5000) { assertFailsWith<SQLException> { library.setStatus(user.id, game, "playing") } }
                        assertEquals("55P03", error.sqlState)
                        blocker.rollback()
                    }
                    assertEquals("backlog", library.findEntry(user.id, game)?.status)
                }
            }
        }
}
