export interface WebAuthConfig {
  appOrigin: string;
  clerk: { secretKey?: string };
  public: { clerk: { publishableKey?: string } };
}

// This module is server-only. Errors identify settings, never their values.
export function validateAuthConfig(config: WebAuthConfig) {
  let origin: URL;
  try {
    origin = new URL(config.appOrigin);
  } catch {
    throw new Error("NUXT_APP_ORIGIN must be an HTTP(S) origin.");
  }
  if (
    origin.origin !== config.appOrigin ||
    origin.username ||
    origin.password ||
    (origin.protocol !== "https:" &&
      !(
        origin.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname)
      ))
  )
    throw new Error("NUXT_APP_ORIGIN must be an HTTPS origin (HTTP is allowed for localhost).");
  const publicKey = config.public.clerk.publishableKey;
  const privateKey = config.clerk.secretKey;
  if (!publicKey || !/^pk_(test|live)_/.test(publicKey))
    throw new Error("NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY is required.");
  let frontend: string;
  try {
    frontend = Buffer.from(publicKey.replace(/^pk_(test|live)_/, ""), "base64").toString("utf8");
  } catch {
    throw new Error("NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY is invalid.");
  }
  if (!/^[a-zA-Z0-9.-]+\$$/.test(frontend))
    throw new Error("NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY is invalid.");
  if (!privateKey || !/^sk_(test|live)_[a-zA-Z0-9_-]+$/.test(privateKey))
    throw new Error("NUXT_CLERK_SECRET_KEY is required.");
  if (publicKey.split("_")[1] !== privateKey.split("_")[1])
    throw new Error("Clerk keys must use the same environment.");
}
