import createClient from "openapi-fetch";
import type { paths } from "./generated/user-api";
import { serviceTransport } from "./transport";
import type { ServiceClientOptions } from "./transport";

export function createUserClient(options: ServiceClientOptions) {
  return createClient<paths>(serviceTransport(options));
}
