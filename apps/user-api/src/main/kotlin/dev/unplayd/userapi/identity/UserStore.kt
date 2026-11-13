package dev.unplayd.userapi.identity

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import java.util.UUID
import javax.sql.DataSource

data class LocalUser(
    val id: UUID,
    val clerkSubject: String,
)

@Serializable
data class MeResponse(
    val id: String,
    val clerkSubject: String,
)

interface UserStore {
    suspend fun resolve(subject: String): LocalUser
}

class JdbcUserStore(
    private val dataSource: DataSource,
) : UserStore {
    override suspend fun resolve(subject: String): LocalUser =
        withContext(Dispatchers.IO) {
            connection { db ->
                db.prepareStatement("SELECT id,clerk_subject FROM user_data.users WHERE clerk_subject=?").use { query ->
                    query.setString(1, subject)
                    query.executeQuery().use { rows ->
                        if (rows.next()) {
                            return@connection LocalUser(
                                rows.getObject("id", UUID::class.java),
                                rows.getString("clerk_subject"),
                            )
                        }
                    }
                }
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

    private fun <T> connection(block: (java.sql.Connection) -> T): T = dataSource.connection.use(block)
}
