import { randomUUID } from "node:crypto";
import { defineEventHandler, getHeader, setHeader } from "h3";

export default defineEventHandler((event) => {
  const incoming = getHeader(event, "X-Request-ID");
  const id = incoming && /^[a-zA-Z0-9_-]{1,64}$/.test(incoming) ? incoming : randomUUID();
  event.context.requestId = id;
  setHeader(event, "X-Request-ID", id);
});
