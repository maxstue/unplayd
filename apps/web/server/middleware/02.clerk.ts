import { clerkMiddleware } from "@clerk/nuxt/server";
import { createError, defineEventHandler, getRequestURL, type EventHandler } from "h3";
import { useRuntimeConfig } from "#imports";

export default defineEventHandler(async (event) => {
  if (getRequestURL(event).pathname === "/api/health") return;
  const config = useRuntimeConfig(event);
  try {
    // SDK 3.1.17 bundles duplicate H3 declarations; its runtime uses Nuxt's H3.
    const authenticate = clerkMiddleware({ authorizedParties: [config.appOrigin] }) as EventHandler;
    return await authenticate(event);
  } catch {
    throw createError({ statusCode: 503, statusMessage: "Authentication service unavailable" });
  }
});
