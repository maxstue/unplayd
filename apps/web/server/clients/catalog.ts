import createClient from "openapi-fetch";
import type { paths } from "./generated/catalog-api";
import { serviceTransport } from "./transport";
import type { ServiceClientOptions } from "./transport";

// Create per request: correlation IDs and credentials are never shared globally.
export function createCatalogClient(options: ServiceClientOptions) {
  return createClient<paths>(serviceTransport(options));
}
