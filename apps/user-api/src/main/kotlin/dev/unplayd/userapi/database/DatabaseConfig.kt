package dev.unplayd.userapi.database

import java.net.URI

class DatabaseConfig private constructor(
    val url: String,
    val user: String,
    val password: String,
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
            return DatabaseConfig(url, user, password)
        }
    }
}
