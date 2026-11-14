import { createError, type H3Event } from "h3";
import { requireSession } from "../auth/session";
import { createCatalogClient } from "./catalog";
import { createUserClient } from "./user";
import { validateServicesConfig, type ServicesConfig } from "./config";

/**
 * Creates an h3 error with a stable, non-reflecting public body (`{ error: { code, message, requestId } }`).
 * Unknown status codes fall back to 503.
 *
 * @param status - HTTP status, one of 400, 401, 403, 404, 409, 413 or 503.
 * @param requestId - Correlation ID echoed to the client (see {@link withRequestSignal}).
 * @returns An error to `throw` from a handler.
 */
export function publicError(status: number, requestId: string) {
  const errors: Record<number, [string, string]> = {
    400: ["invalid_request", "Invalid request"],
    401: ["unauthorized", "Unauthorized"],
    403: ["forbidden", "Forbidden"],
    404: ["not_found", "Not found"],
    409: ["conflict", "Conflict"],
    413: ["payload_too_large", "Payload too large"],
    503: ["unavailable", "Service unavailable"],
  };
  const [code, message] = errors[status] ?? errors[503]!;
  return createError({
    statusCode: status,
    statusMessage: message,
    data: { error: { code, message, requestId } },
  });
}

interface UpstreamOptions {
  allowEmpty?: boolean;
  signal?: AbortSignal;
  allowedStatuses?: readonly number[];
}

function reportUpstreamFailure(
  service: "user" | "catalog",
  requestId: string,
  category: "timeout" | "transport" | "parsing" | "http",
  status: number | null = null,
) {
  console.warn("upstream_failure", {
    requestId: /^[a-zA-Z0-9_-]{1,64}$/.test(requestId) ? requestId : "unknown",
    service,
    category,
    status,
  });
}

function reportUpstreamException(
  cause: unknown,
  service: "user" | "catalog",
  requestId: string,
  signal?: AbortSignal,
) {
  if (signal?.aborted || (cause instanceof Error && cause.name === "AbortError")) {
    return;
  }
  let category: "timeout" | "transport" | "parsing" = "transport";
  if (cause instanceof Error && cause.name === "TimeoutError") {
    category = "timeout";
  } else if (cause instanceof SyntaxError) {
    category = "parsing";
  }
  reportUpstreamFailure(service, requestId, category);
}

/**
 * Runs an openapi-fetch call and maps transport, HTTP and parsing failures to {@link publicError}s.
 * Failures are logged without request or response bodies.
 *
 * @param task - The client call, e.g. `() => user.GET("/me")`.
 * @param service - Which upstream is called, used for logging and the default allowed statuses.
 * @param requestId - Correlation ID for logs and public errors.
 * @param options - {@link UpstreamOptions}: `allowEmpty` for bodiless responses, the request `signal`,
 *   and `allowedStatuses` to pass through instead of 503.
 * @returns The response `data` and HTTP `status`.
 */
export async function upstream<T>(
  task: () => Promise<{ data?: T; response: Response }>,
  service: "user" | "catalog",
  requestId: string,
  { allowEmpty = false, signal, allowedStatuses }: UpstreamOptions = {},
) {
  let result;
  try {
    result = await task();
  } catch (cause) {
    reportUpstreamException(cause, service, requestId, signal);
    throw publicError(503, requestId);
  }
  if (!result.response.ok) {
    const status = result.response.status;
    const allowed =
      allowedStatuses ?? (service === "user" ? [400, 401, 403, 404, 409] : [400, 404, 409]);
    reportUpstreamFailure(service, requestId, "http", status);
    throw publicError(allowed.includes(status) ? status : 503, requestId);
  }
  if (!allowEmpty && (result.data === undefined || result.data === null)) {
    reportUpstreamFailure(service, requestId, "parsing", result.response.status);
    throw publicError(503, requestId);
  }
  return { data: result.data as T, status: result.response.status };
}

/**
 * Thrown by request validation; {@link withRequestSignal} maps it to a 400 {@link publicError}.
 */
export class InvalidInput extends Error {}

interface UserRequestServices {
  user: ReturnType<typeof createUserClient>;
  requestId: string;
  signal: AbortSignal;
  clientSignal: AbortSignal;
}

export interface RequestServices extends UserRequestServices {
  catalog: ReturnType<typeof createCatalogClient>;
}

/**
 * Bounds work that does not accept an {@link AbortSignal} itself, e.g. SDK calls or body reads.
 * The task keeps running after an abort; only the returned promise settles early.
 *
 * @param task - The pending work to race against the signal.
 * @param signal - Signal whose abort rejects the returned promise with its reason.
 * @returns The task's result, unless the signal aborts first.
 */
export async function abortable<T>(task: Promise<T>, signal: AbortSignal): Promise<T> {
  signal.throwIfAborted();
  let abort: () => void = () => {};
  const aborted = new Promise<never>((_resolve, reject) => {
    abort = () => reject(signal.reason);
    signal.addEventListener("abort", abort, { once: true });
  });
  try {
    return await Promise.race([task, aborted]);
  } finally {
    signal.removeEventListener("abort", abort);
  }
}

/**
 * Runs request work under one aggregation deadline that also aborts when the client disconnects.
 * Stores the combined signal as `event.context.requestSignal` so body reads share the deadline.
 *
 * @param event - The incoming {@link H3Event}.
 * @param run - Work receiving the deadline `signal` and the disconnect-only `clientSignal`.
 * @param timeoutMs - Aggregation deadline in milliseconds.
 * @returns The result of `run`.
 * @throws A 400 {@link publicError} for {@link InvalidInput}, a 503 once the signal has aborted.
 */
export async function withRequestSignal<T>(
  event: H3Event,
  run: (signal: AbortSignal, clientSignal: AbortSignal) => Promise<T>,
  timeoutMs = 5_000,
) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  const close = () => {
    if (!event.node.res.writableEnded) {
      abort();
    }
  };
  event.node.req.once("aborted", abort);
  event.node.res.once("close", close);
  const clientSignal = AbortSignal.any([
    controller.signal,
    ...(event.web?.request?.signal ? [event.web.request.signal] : []),
  ]);
  const signal = AbortSignal.any([clientSignal, AbortSignal.timeout(timeoutMs)]);
  event.context.requestSignal = signal;
  try {
    signal.throwIfAborted();
    return await run(signal, clientSignal);
  } catch (error) {
    if (error instanceof InvalidInput) {
      throw publicError(400, event.context.requestId);
    }
    if (signal.aborted) {
      throw publicError(503, event.context.requestId);
    }
    throw error;
  } finally {
    event.node.req.off("aborted", abort);
    event.node.res.off("close", close);
  }
}

/**
 * Requires a session, fetches its token and runs `run` with a per-request user-service client.
 * See {@link withRequestSignal} for deadline handling.
 *
 * @param event - The incoming {@link H3Event}.
 * @param input - Origin, user-service URL and optional timeouts (subset of {@link ServicesConfig}).
 * @param run - Work receiving the {@link UserRequestServices}.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The result of `run`.
 */
export async function withUserService<T>(
  event: H3Event,
  input: {
    appOrigin: string;
    userApiUrl: string;
    serviceTimeoutMs?: number | string;
    requestTimeoutMs?: number | string;
  },
  run: (services: UserRequestServices) => Promise<T>,
  fetcher?: typeof fetch,
) {
  const auth = requireSession(event, input.appOrigin);
  return withRequestSignal(
    event,
    async (signal, clientSignal) => {
      let token: string | null;
      try {
        token = await abortable<string | null>(auth.getToken(), signal);
      } catch {
        throw publicError(503, event.context.requestId);
      }
      if (!token) {
        throw publicError(401, event.context.requestId);
      }
      signal.throwIfAborted();
      let user;
      try {
        user = createUserClient({
          requestId: event.context.requestId,
          timeoutMs:
            input.serviceTimeoutMs === undefined ? undefined : Number(input.serviceTimeoutMs),
          signal,
          fetch: fetcher,
          baseUrl: input.userApiUrl,
          token,
        });
      } catch {
        throw publicError(503, event.context.requestId);
      }
      return run({
        user,
        requestId: event.context.requestId,
        signal,
        clientSignal,
      });
    },
    Number(input.requestTimeoutMs ?? 5_000),
  );
}

/**
 * Requires a session and runs `run` with a per-request catalog client authenticated by the service token.
 * See {@link withRequestSignal} for deadline handling.
 *
 * @param event - The incoming {@link H3Event}.
 * @param input - Runtime {@link ServicesConfig}, validated via {@link validateServicesConfig}.
 * @param run - Work receiving the catalog part of {@link RequestServices}.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The result of `run`.
 */
export function withCatalogService<T>(
  event: H3Event,
  input: ServicesConfig,
  run: (
    services: Pick<RequestServices, "catalog" | "requestId" | "signal" | "clientSignal">,
  ) => Promise<T>,
  fetcher?: typeof fetch,
) {
  requireSession(event, input.appOrigin);
  const config = validateServicesConfig(input);
  return withRequestSignal(
    event,
    (signal, clientSignal) =>
      run({
        catalog: createCatalogClient({
          requestId: event.context.requestId,
          timeoutMs: config.serviceTimeoutMs,
          signal,
          fetch: fetcher,
          baseUrl: config.catalogApiUrl,
          token: config.catalogServiceToken,
        }),
        requestId: event.context.requestId,
        signal,
        clientSignal,
      }),
    config.requestTimeoutMs,
  );
}

/**
 * Like {@link withUserService}, but also provides a catalog client for the same request.
 *
 * @param event - The incoming {@link H3Event}.
 * @param input - Runtime {@link ServicesConfig}, validated via {@link validateServicesConfig}.
 * @param run - Work receiving the full {@link RequestServices}.
 * @param fetcher - Optional `fetch` override, used in tests.
 * @returns The result of `run`.
 */
export async function withServices<T>(
  event: H3Event,
  input: ServicesConfig,
  run: (services: RequestServices) => Promise<T>,
  fetcher?: typeof fetch,
) {
  return withUserService(
    event,
    input,
    async (services) => {
      const config = validateServicesConfig(input);
      return run({
        ...services,
        catalog: createCatalogClient({
          requestId: services.requestId,
          timeoutMs: config.serviceTimeoutMs,
          signal: services.signal,
          fetch: fetcher,
          baseUrl: config.catalogApiUrl,
          token: config.catalogServiceToken,
        }),
      });
    },
    fetcher,
  );
}
