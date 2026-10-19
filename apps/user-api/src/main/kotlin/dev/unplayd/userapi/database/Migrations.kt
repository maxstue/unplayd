package dev.unplayd.userapi.database

import org.flywaydb.core.Flyway

fun migrateDatabase(
    config: DatabaseConfig,
    target: String? = null,
) {
    val settings =
        Flyway
            .configure()
            .dataSource(config.url, config.user, config.password)
            .defaultSchema("user_data")
            .schemas("user_data")
            .createSchemas(false)
            .cleanDisabled(true)
            .validateMigrationNaming(true)
    if (target != null) {
        require(target.toIntOrNull()?.let { it > 0 } == true) { "Migration target must be a positive version" }
        settings.target(target)
    }
    settings.load().migrate()
}
