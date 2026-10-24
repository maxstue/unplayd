import { validateAuthConfig } from "../auth/config";
import { defineNitroPlugin, useRuntimeConfig } from "#imports";

export default defineNitroPlugin(() => {
  validateAuthConfig(useRuntimeConfig());
});
