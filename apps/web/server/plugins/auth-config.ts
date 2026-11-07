import { validateAuthConfig } from "../auth/config";
import { validateServicesConfig } from "../clients/config";
import { defineNitroPlugin, useRuntimeConfig } from "#imports";

export default defineNitroPlugin(() => {
  validateAuthConfig(useRuntimeConfig());
  validateServicesConfig(useRuntimeConfig());
});
