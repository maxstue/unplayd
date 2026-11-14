import createClient from "openapi-fetch";
import type { paths } from "./generated/catalog-api";
import { serviceTransport } from "./transport";
import type { ServiceClientOptions } from "./transport";

/**
 * Creates a typed catalog-service client.
 * Create one per request: correlation IDs and credentials are never shared globally.
 *
 * @param options - {@link ServiceClientOptions} for {@link serviceTransport}.
 * @returns An openapi-fetch client for the catalog API.
 */
export function createCatalogClient(options: ServiceClientOptions) {
  return createClient<paths>(serviceTransport(options));
}
