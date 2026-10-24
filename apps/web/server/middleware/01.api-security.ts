import { defineEventHandler, getRequestURL, setHeader } from "h3";
import { enforceMutationOrigin } from "../auth/session";
import { useRuntimeConfig } from "#imports";

export default defineEventHandler((event) => {
  const { pathname } = getRequestURL(event);
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    setHeader(event, "Cache-Control", "no-store");
    enforceMutationOrigin(event, useRuntimeConfig(event).appOrigin);
  }
});
