package dev.unplayd.userapi

import ch.qos.logback.classic.Level
import ch.qos.logback.classic.LoggerContext
import ch.qos.logback.classic.spi.LoggingEvent
import dev.unplayd.userapi.logging.FlatJsonEncoder
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.slf4j.event.KeyValuePair
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class FlatJsonEncoderTest {
    @Test
    fun `events become one flat typed JSON line without exception messages`() {
        val logger = LoggerContext().getLogger("dev.unplayd.test")
        val event =
            LoggingEvent(
                "fqcn",
                logger,
                Level.WARN,
                "request failed",
                IllegalStateException("password=secret"),
                null,
            )
        listOf(
            KeyValuePair("requestId", "req_1"),
            KeyValuePair("status", 503),
            KeyValuePair("failed", true),
            KeyValuePair("sqlState", null),
            KeyValuePair("msg", "must not override"),
        ).forEach(event::addKeyValuePair)

        val line = FlatJsonEncoder().encode(event).toString(Charsets.UTF_8)

        assertEquals(1, line.count { it == '\n' })
        assertEquals('\n', line.last())
        val json = Json.parseToJsonElement(line).jsonObject
        assertEquals(JsonPrimitive("WARN"), json["level"])
        assertEquals(JsonPrimitive("dev.unplayd.test"), json["logger"])
        assertEquals(JsonPrimitive("request failed"), json["msg"])
        assertEquals(JsonPrimitive("req_1"), json["requestId"])
        assertEquals(JsonPrimitive(503), json["status"])
        assertEquals(JsonPrimitive(true), json["failed"])
        assertEquals(JsonNull, json["sqlState"])
        assertEquals(JsonPrimitive("java.lang.IllegalStateException"), json["errorType"])
        assertFalse(line.contains("secret"), line)
    }
}
