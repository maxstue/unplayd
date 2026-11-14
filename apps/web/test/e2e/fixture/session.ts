import { defineEventHandler, getCookie } from "h3";

export default defineEventHandler((event) => {
  const userId = getCookie(event, "fixture-user");
  // This cookie/session adapter exists only in the separate test layer.
  event.context.auth = (() => ({
    isAuthenticated: Boolean(userId),
    userId,
    sessionId: userId ? `session-${userId}` : null,
    getToken: async () => userId ?? null,
  })) as typeof event.context.auth;
});
