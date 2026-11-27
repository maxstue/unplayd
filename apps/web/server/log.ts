type Level = "INFO" | "WARN" | "ERROR";
type Field = string | number | boolean | null;

/**
 * Writes one flat JSON line in the shape of the Go and Kotlin services: `time`, `level`, `msg`,
 * then `fields`. Callers pass only safe values: never tokens, headers, query strings or bodies.
 *
 * @param level - INFO goes to stdout, WARN and ERROR to stderr.
 * @param msg - Stable event name, e.g. `request completed`.
 * @param fields - Flat, typed fields such as `requestId`, `status` or `durationMs`.
 */
export function log(level: Level, msg: string, fields: Record<string, Field> = {}) {
  const line = JSON.stringify({ time: new Date().toISOString(), level, msg, ...fields });
  if (level === "INFO") {
    console.log(line);
  } else {
    console.warn(line);
  }
}

/** Returns `id` when it is a valid correlation ID and `unknown` otherwise, so logs never echo raw input. */
export function safeRequestId(id: unknown) {
  return typeof id === "string" && /^[a-zA-Z0-9_-]{1,64}$/.test(id) ? id : "unknown";
}
