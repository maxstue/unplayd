package dev.unplayd.userapi.logging

import io.ktor.server.application.createApplicationPlugin
import io.ktor.server.application.hooks.CallSetup
import io.ktor.server.application.hooks.ResponseSent
import io.ktor.server.plugins.callid.callId
import io.ktor.server.request.httpMethod
import io.ktor.server.request.path
import io.ktor.util.AttributeKey
import org.slf4j.LoggerFactory

private val startedAt = AttributeKey<Long>("RequestLoggingStartedAt")

/**
 * Writes one structured entry per request with request ID, method, path, status and duration.
 * Query strings, headers and bodies are left out because they can carry tokens or private notes.
 * Health probes log at debug level.
 */
val RequestLogging =
    createApplicationPlugin("RequestLogging") {
        val logger = LoggerFactory.getLogger("dev.unplayd.userapi.requests")
        on(CallSetup) { call -> call.attributes.put(startedAt, System.nanoTime()) }
        on(ResponseSent) { call ->
            val started = call.attributes.getOrNull(startedAt) ?: return@on
            val path = call.request.path()
            val entry = if (path.startsWith("/health/")) logger.atDebug() else logger.atInfo()
            entry
                .addKeyValue("requestId", call.callId)
                .addKeyValue("method", call.request.httpMethod.value)
                .addKeyValue("path", path)
                .addKeyValue("status", call.response.status()?.value)
                .addKeyValue("durationMs", (System.nanoTime() - started) / 1_000_000)
                .log("request completed")
        }
    }
