import { getRequestURL, type H3Event } from "h3";
import { log, safeRequestId } from "./log";

/** Paths that are not logged: liveness probes and fingerprinted build assets. */
const quiet = /^\/(?:api\/health(?:\/|$)|_nuxt\/)/;

/**
 * Writes one `request completed` entry with request ID, method, path, status and duration once the
 * response is finished, including error responses. Only the pathname is logged: the query string
 * can carry search text. Requests the client abandons before the response finishes are not logged.
 */
export function logRequest(event: H3Event) {
  const path = getRequestURL(event).pathname;
  if (quiet.test(path)) {
    return;
  }
  const startedAt = performance.now();
  event.node.res.once("finish", () => {
    log("INFO", "request completed", {
      requestId: safeRequestId(event.context.requestId),
      method: event.method,
      path,
      status: event.node.res.statusCode,
      durationMs: Math.round(performance.now() - startedAt),
    });
  });
}
