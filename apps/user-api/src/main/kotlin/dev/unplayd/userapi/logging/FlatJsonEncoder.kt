package dev.unplayd.userapi.logging

import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.encoder.EncoderBase
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

/**
 * Encodes each event as one flat JSON line in the shape of the Go catalog's slog output: `time`,
 * `level`, `logger`, `msg`, then every SLF4J key-value pair as a top-level field, keeping numbers
 * and booleans typed so log platforms can filter on them. Throwables contribute only their class name as `errorType`: exception messages
 * can echo SQL parameters, tokens or upstream bodies.
 */
class FlatJsonEncoder : EncoderBase<ILoggingEvent>() {
    override fun headerBytes(): ByteArray? = null

    override fun footerBytes(): ByteArray? = null

    override fun encode(event: ILoggingEvent): ByteArray {
        val fields = linkedMapOf<String, JsonElement>()
        fields["time"] = JsonPrimitive(Instant.ofEpochMilli(event.timeStamp).toString())
        fields["level"] = JsonPrimitive(event.level.toString())
        fields["logger"] = JsonPrimitive(event.loggerName)
        fields["msg"] = JsonPrimitive(event.formattedMessage)
        event.keyValuePairs.orEmpty().forEach { pair ->
            if (pair.key !in fields) fields[pair.key] = pair.value.toJson()
        }
        event.throwableProxy?.let { fields["errorType"] = JsonPrimitive(it.className) }
        return (JsonObject(fields).toString() + "\n").toByteArray(Charsets.UTF_8)
    }

    private fun Any?.toJson(): JsonElement =
        when (this) {
            null -> JsonNull
            is Number -> JsonPrimitive(this)
            is Boolean -> JsonPrimitive(this)
            else -> JsonPrimitive(toString())
        }
}
