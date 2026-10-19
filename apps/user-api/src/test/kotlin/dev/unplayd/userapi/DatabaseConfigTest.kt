package dev.unplayd.userapi

import dev.unplayd.userapi.database.DatabaseConfig
import java.util.UUID
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse

class DatabaseConfigTest {
    @Test
    fun `valid configuration keeps credentials separate from the URL and diagnostic output`() {
        val password = UUID.randomUUID().toString()
        val values =
            mapOf(
                "DATABASE_URL" to "jdbc:postgresql://localhost:5432/unplayd?currentSchema=user_data",
                "DATABASE_PASSWORD" to password,
            )
        val config = DatabaseConfig.load(values::get)
        assertEquals("user_api", config.user)
        assertEquals(password, config.password)
        assertFalse(config.toString().contains(password))
    }

    @Test
    fun `invalid or cross-schema configuration is rejected without exposing values`() {
        val password = UUID.randomUUID().toString()
        val base = mapOf("DATABASE_URL" to "jdbc:postgresql://localhost:5432/unplayd", "DATABASE_PASSWORD" to password)
        listOf(
            base - "DATABASE_URL",
            base - "DATABASE_PASSWORD",
            base + ("DATABASE_URL" to "invalid"),
            base + ("DATABASE_URL" to "jdbc:postgresql://localhost:70000/unplayd"),
            base + ("DATABASE_URL" to "jdbc:postgresql://localhost/unplayd?currentSchema=catalog"),
            base + ("DATABASE_URL" to "jdbc:postgresql://localhost/unplayd?password=$password"),
            base + ("DATABASE_USER" to "postgres"),
        ).forEach { values ->
            val failure = assertFailsWith<IllegalArgumentException> { DatabaseConfig.load(values::get) }
            assertFalse(failure.message.orEmpty().contains(password))
        }
    }
}
