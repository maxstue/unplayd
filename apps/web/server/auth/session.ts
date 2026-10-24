import { createError, getHeader, setHeader, type H3Event } from "h3";

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

export function enforceMutationOrigin(event: H3Event, appOrigin: string) {
  if (["GET", "HEAD", "OPTIONS"].includes(event.method)) return;
  if (
    getHeader(event, "origin") !== appOrigin ||
    getHeader(event, "sec-fetch-site") === "cross-site"
  ) {
    throw createError({ statusCode: 403, statusMessage: "Forbidden" });
  }
}
