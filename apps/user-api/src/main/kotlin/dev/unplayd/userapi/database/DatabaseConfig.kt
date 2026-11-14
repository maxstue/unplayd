package dev.unplayd.userapi.database

import java.net.URI

class DatabaseConfig private constructor(
    val url: String,
    val user: String,
    val password: String,
    val poolSize: Int,
    val connectionTimeoutMs: Long,
    val statementTimeoutMs: Long,
    val lockTimeoutMs: Long,
) {
    companion object {
        fun load(env: (String) -> String? = System::getenv): DatabaseConfig {
            val url = requireNotNull(env("DATABASE_URL")) { "DATABASE_URL is required" }
            require(url.startsWith("jdbc:postgresql://")) { "DATABASE_URL must be a PostgreSQL JDBC URL" }
            val parsed = runCatching { URI(url.removePrefix("jdbc:")) }.getOrNull()
            require(parsed?.host != null && !parsed.path.isNullOrBlank() && parsed.path != "/") { "DATABASE_URL needs a host and database" }
            require(parsed.port == -1 || parsed.port in 1..65535) { "DATABASE_URL has an invalid port" }
            require(parsed.userInfo == null) { "DATABASE_URL must not contain credentials" }
            val query =
                parsed.rawQuery
                    .orEmpty()
                    .split("&")
                    .map { it.substringBefore("=") to it.substringAfter("=", "") }
                    .toMap()
            require(query.keys.none { it.lowercase() in setOf("user", "password") }) { "Use separate database credential settings" }
            require(query["currentSchema"] in listOf(null, "user_data")) { "DATABASE_URL must target the user_data schema" }
            val user = env("DATABASE_USER") ?: "user_api"
            require(user == "user_api") { "DATABASE_USER must be user_api" }
            val password = requireNotNull(env("DATABASE_PASSWORD")?.takeIf { it.isNotEmpty() }) { "DATABASE_PASSWORD is required" }

            fun setting(
                name: String,
                default: Long,
                range: LongRange,
            ): Long {
                val value = env(name)?.let { requireNotNull(it.toLongOrNull()) { "$name must be an integer" } } ?: default
                require(value in range) { "$name is outside the supported range" }
                return value
            }
            return DatabaseConfig(
                url,
                user,
                password,
                setting("DATABASE_POOL_SIZE", 10, 1L..50L).toInt(),
                setting("DATABASE_CONNECTION_TIMEOUT_MS", 1000, 250L..30000L),
                setting("DATABASE_STATEMENT_TIMEOUT_MS", 2000, 1L..30000L),
                setting("DATABASE_LOCK_TIMEOUT_MS", 1000, 1L..30000L),
            )
        }
    }
}
