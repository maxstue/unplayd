import { createError, getHeader, setHeader, type H3Event } from "h3";

/**
 * Disables caching, enforces the mutation origin and requires an end-user Clerk session.
 *
 * @param event - The incoming {@link H3Event}.
 * @param appOrigin - The app's own origin (see {@link enforceMutationOrigin}).
 * @returns The authenticated Clerk auth object.
 * @throws A 401 error if there is no complete session.
 */
export function requireSession(event: H3Event, appOrigin: string) {
  setHeader(event, "Cache-Control", "no-store");
  enforceMutationOrigin(event, appOrigin);
  // Explicitly accept only end-user sessions, including when Clerk also supports machine tokens.
  const auth = event.context.auth?.({ acceptsToken: "session_token" });
  if (!auth?.isAuthenticated || !auth.userId || !auth.sessionId) {
    throw createError({ statusCode: 401, statusMessage: "Unauthorized" });
  }
  return auth;
}

/**
 * Rejects non-safe methods unless they come from the app's own origin and are not cross-site.
 *
 * @param event - The incoming {@link H3Event}.
 * @param appOrigin - The expected `Origin` header value.
 * @throws A 403 error for foreign or cross-site mutations.
 */
export function enforceMutationOrigin(event: H3Event, appOrigin: string) {
  if (["GET", "HEAD", "OPTIONS"].includes(event.method)) {
    return;
  }
  if (
    getHeader(event, "origin") !== appOrigin ||
    getHeader(event, "sec-fetch-site") === "cross-site"
  ) {
    throw createError({ statusCode: 403, statusMessage: "Forbidden" });
  }
}
