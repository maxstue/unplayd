export interface ServiceClientOptions {
  baseUrl: string;
  token: string;
  requestId: string;
  timeoutMs?: number;
  fetch?: typeof globalThis.fetch;
}

export function serviceTransport(options: ServiceClientOptions) {
  const timeoutMs = options.timeoutMs ?? 3_000;
  let url: URL;
  try {
    url = new URL(options.baseUrl);
  } catch {
    throw new Error("Service base URL must be an absolute HTTP(S) URL.");
  }
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) {
    throw new Error("Service base URL must use HTTP(S) and contain no credentials.");
  }
  if (!options.token) throw new Error("Service authentication token is required.");
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(options.requestId))
    throw new Error("A validated request ID is required.");
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1)
    throw new Error("Service timeout must be a positive integer.");
  const fetcher = options.fetch ?? globalThis.fetch;
  return {
    baseUrl: url.href.replace(/\/$/, ""),
    headers: { Authorization: `Bearer ${options.token}`, "X-Request-ID": options.requestId },
    fetch: (request: Request) =>
      fetcher(request, {
        signal: AbortSignal.any([request.signal, AbortSignal.timeout(timeoutMs)]),
      }),
  };
}
