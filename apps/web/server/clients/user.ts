import createClient from "openapi-fetch";
import type { paths } from "./generated/user-api";
import { serviceTransport } from "./transport";
import type { ServiceClientOptions } from "./transport";

/**
 * Creates a typed user-service client. Create one per request so credentials are never shared.
 *
 * @param options - {@link ServiceClientOptions} for {@link serviceTransport}.
 * @returns An openapi-fetch client for the user API.
 */
export function createUserClient(options: ServiceClientOptions) {
  return createClient<paths>(serviceTransport(options));
}
