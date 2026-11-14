import type { H3Event } from "h3";
import { upstream, withUserService } from "../clients/request";

/**
 * Loads the signed-in user's profile from the user service.
 *
 * @param event - The incoming {@link H3Event}.
 * @param config - Origin, user-service URL and optional timeout (see {@link withUserService}).
 * @param fetch - Optional `fetch` override, used in tests.
 * @returns The user service's `/me` response.
 */
export async function currentUser(
  event: H3Event,
  config: { appOrigin: string; userApiUrl: string; serviceTimeoutMs?: number },
  fetch?: typeof globalThis.fetch,
) {
  return withUserService(
    event,
    config,
    async ({ user, requestId, signal }) =>
      (await upstream(() => user.GET("/me"), "user", requestId, { signal, allowedStatuses: [401] }))
        .data,
    fetch,
  );
}
