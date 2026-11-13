import type { H3Event } from "h3";
import { upstream, withUserService } from "../clients/request";

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
