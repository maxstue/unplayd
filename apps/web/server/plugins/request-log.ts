import { logRequest } from "../request-log";

// defineNitroPlugin is auto-imported in the Nitro context, which types nitroApp.
export default defineNitroPlugin((nitroApp) => {
  nitroApp.hooks.hook("request", logRequest);
});
