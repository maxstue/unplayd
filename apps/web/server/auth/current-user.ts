import { createError, type H3Event } from "h3";
import { createUserClient } from "../clients/user";
import { withRequestSignal } from "../clients/request";
import { requireSession } from "./session";

export async function currentUser(
  event: H3Event,
  config: { appOrigin: string; userApiUrl: string; serviceTimeoutMs?: number },
  fetch?: typeof globalThis.fetch,
) {
  const auth = requireSession(event, config.appOrigin);
  return withRequestSignal(event, async (signal) => {
    let token: string | null;
    try {
      token = await auth.getToken();
    } catch {
      throw createError({ statusCode: 503, statusMessage: "User service unavailable" });
    }
    if (!token) {
      throw createError({ statusCode: 401, statusMessage: "Unauthorized" });
    }
    let result;
    try {
      const client = createUserClient({
        baseUrl: config.userApiUrl,
        token,
        requestId: event.context.requestId,
        fetch,
        signal,
        timeoutMs: config.serviceTimeoutMs,
      });
      result = await client.GET("/me");
    } catch {
      throw createError({ statusCode: 503, statusMessage: "User service unavailable" });
    }
    if (result.response.status === 401) {
      throw createError({ statusCode: 401, statusMessage: "Unauthorized" });
    }
    if (!result.data) {
      throw createError({ statusCode: 503, statusMessage: "User service unavailable" });
    }
    return result.data;
  });
}
