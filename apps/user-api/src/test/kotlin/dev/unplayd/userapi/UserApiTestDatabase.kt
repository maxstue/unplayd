package dev.unplayd.userapi

import dev.unplayd.userapi.database.DatabaseConfig
import dev.unplayd.userapi.database.dataSource
import dev.unplayd.userapi.database.migrateDatabase
import org.testcontainers.postgresql.PostgreSQLContainer
import org.testcontainers.utility.MountableFile
import java.sql.Connection
import java.sql.DriverManager
import java.util.UUID

internal class UserApiTestDatabase private constructor(
    private val postgres: PostgreSQLContainer,
    private val catalogPassword: String,
    val config: DatabaseConfig,
) : AutoCloseable {
    val dataSource = config.dataSource()

    fun connection(): Connection = DriverManager.getConnection(config.url, config.user, config.password)

    fun catalogConnection(): Connection = DriverManager.getConnection(postgres.jdbcUrl, "catalog", catalogPassword)

    override fun close() {
        dataSource.close()
        postgres.close()
    }

    companion object {
        fun start(target: String? = null): UserApiTestDatabase {
            val userPassword = UUID.randomUUID().toString()
            val catalogPassword = UUID.randomUUID().toString()
            // renovate: datasource=docker depName=postgres
            val image = "postgres:18.6-bookworm"
            val postgres =
                PostgreSQLContainer(image)
                    .withDatabaseName("unplayd")
                    .withUsername("postgres")
                    .withPassword(UUID.randomUUID().toString())
                    .withEnv("USER_API_DB_PASSWORD", userPassword)
                    .withEnv("CATALOG_DB_PASSWORD", catalogPassword)
                    .withCopyFileToContainer(
                        MountableFile.forClasspathResource("init.sh"),
                        "/docker-entrypoint-initdb.d/10-init.sh",
                    )
            try {
                postgres.start()
                val separator = if (postgres.jdbcUrl.contains("?")) "&" else "?"
                val config =
                    DatabaseConfig.load(
                        mapOf(
                            "DATABASE_URL" to "${postgres.jdbcUrl}${separator}currentSchema=user_data",
                            "DATABASE_USER" to "user_api",
                            "DATABASE_PASSWORD" to userPassword,
                        )::get,
                    )
                migrateDatabase(config, target)
                return UserApiTestDatabase(postgres, catalogPassword, config)
            } catch (failure: Throwable) {
                postgres.close()
                throw failure
            }
        }
    }
}
