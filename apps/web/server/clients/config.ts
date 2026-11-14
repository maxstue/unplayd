export interface ServicesConfig {
  appOrigin: string;
  userApiUrl: string;
  catalogApiUrl: string;
  catalogServiceToken: string;
  serviceTimeoutMs: number | string;
  requestTimeoutMs?: number | string;
}

/**
 * Validates the service settings. Errors name the setting, never its value.
 *
 * @param config - Raw runtime {@link ServicesConfig}.
 * @returns The config with numeric `serviceTimeoutMs` and `requestTimeoutMs` (default 5000).
 * @throws If a URL is not a bare HTTP(S) origin, the token is too short or a timeout is out of range.
 */
export function validateServicesConfig(config: ServicesConfig) {
  for (const [name, value] of [
    ["NUXT_USER_API_URL", config.userApiUrl],
    ["NUXT_CATALOG_API_URL", config.catalogApiUrl],
  ]) {
    let url: URL;
    try {
      url = new URL(value!);
    } catch {
      throw new Error(`${name} must be an HTTP(S) origin.`);
    }
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.pathname !== "/" ||
      url.search ||
      url.hash
    ) {
      throw new Error(
        `${name} must be an HTTP(S) origin without credentials, path, query or fragment.`,
      );
    }
  }
  if (
    !config.catalogServiceToken ||
    config.catalogServiceToken.length < 32 ||
    config.catalogServiceToken.length > 8192 ||
    /\s/.test(config.catalogServiceToken)
  ) {
    throw new Error(
      "NUXT_CATALOG_SERVICE_TOKEN must contain at least 32 characters without whitespace.",
    );
  }
  const timeoutMs = Number(config.serviceTimeoutMs);
  if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 30_000) {
    throw new Error("NUXT_SERVICE_TIMEOUT_MS must be an integer between 1 and 30000.");
  }
  const requestTimeoutMs = Number(config.requestTimeoutMs ?? 5_000);
  if (!Number.isInteger(requestTimeoutMs) || requestTimeoutMs < 1 || requestTimeoutMs > 30_000) {
    throw new Error("NUXT_REQUEST_TIMEOUT_MS must be an integer between 1 and 30000.");
  }
  return { ...config, serviceTimeoutMs: timeoutMs, requestTimeoutMs };
}
