import { createError, type H3Event } from "h3";
import { requireSession } from "../auth/session";
import { createCatalogClient } from "./catalog";
import { createUserClient } from "./user";
import { validateServicesConfig, type ServicesConfig } from "./config";

export function publicError(status: number, requestId: string) {
  const errors: Record<number, [string, string]> = {
    400: ["invalid_request", "Invalid request"],
    401: ["unauthorized", "Unauthorized"],
    403: ["forbidden", "Forbidden"],
    404: ["not_found", "Not found"],
    409: ["conflict", "Conflict"],
    503: ["unavailable", "Service unavailable"],
  };
  const [code, message] = errors[status] ?? errors[503]!;
  return createError({
    statusCode: status,
    statusMessage: message,
    data: { error: { code, message, requestId } },
  });
}

export async function upstream<T>(
  task: () => Promise<{ data?: T; response: Response }>,
  service: "user" | "catalog",
  requestId: string,
  allowEmpty = false,
) {
  let result;
  try {
    result = await task();
  } catch {
    throw publicError(503, requestId);
  }
  if (!result.response.ok) {
    const status = result.response.status;
    const allowed = service === "user" ? [400, 401, 403, 404, 409] : [400, 404, 409];
    throw publicError(allowed.includes(status) ? status : 503, requestId);
  }
  if (!allowEmpty && (result.data === undefined || result.data === null))
    throw publicError(503, requestId);
  return { data: result.data as T, status: result.response.status };
}

export class InvalidInput extends Error {}

export interface RequestServices {
  user: ReturnType<typeof createUserClient>;
  catalog: ReturnType<typeof createCatalogClient>;
  requestId: string;
  signal: AbortSignal;
}

export async function withRequestSignal<T>(
  event: H3Event,
  run: (signal: AbortSignal) => Promise<T>,
) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  const close = () => {
    if (!event.node.res.writableEnded) abort();
  };
  event.node.req.once("aborted", abort);
  event.node.res.once("close", close);
  const signal = AbortSignal.any([
    controller.signal,
    ...(event.web?.request?.signal ? [event.web.request.signal] : []),
  ]);
  try {
    signal.throwIfAborted();
    return await run(signal);
  } catch (error) {
    if (error instanceof InvalidInput) throw publicError(400, event.context.requestId);
    if (signal.aborted) throw publicError(503, event.context.requestId);
    throw error;
  } finally {
    event.node.req.off("aborted", abort);
    event.node.res.off("close", close);
  }
}

export async function withServices<T>(
  event: H3Event,
  input: ServicesConfig,
  run: (services: RequestServices) => Promise<T>,
  fetcher?: typeof fetch,
) {
  const auth = requireSession(event, input.appOrigin);
  const config = validateServicesConfig(input);
  return withRequestSignal(event, async (signal) => {
    let token;
    try {
      token = await auth.getToken();
    } catch {
      throw publicError(503, event.context.requestId);
    }
    if (!token) throw publicError(401, event.context.requestId);
    signal.throwIfAborted();
    const common = {
      requestId: event.context.requestId,
      timeoutMs: config.serviceTimeoutMs,
      signal,
      fetch: fetcher,
    };
    return run({
      user: createUserClient({ ...common, baseUrl: config.userApiUrl, token }),
      catalog: createCatalogClient({
        ...common,
        baseUrl: config.catalogApiUrl,
        token: config.catalogServiceToken,
      }),
      requestId: event.context.requestId,
      signal,
    });
  });
}
