import { randomBytes } from "node:crypto";
import { describe, expect, it } from "vite-plus/test";
import { validateAuthConfig, type WebAuthConfig } from "../server/auth/config";

function authConfig(environment: "test" | "live" = "test"): WebAuthConfig {
  return {
    appOrigin: "https://app.example",
    clerk: { secretKey: ["sk", environment, randomBytes(24).toString("hex")].join("_") },
    public: {
      clerk: {
        publishableKey: ["pk", environment, Buffer.from("clerk.example$").toString("base64")].join(
          "_",
        ),
      },
    },
  };
}

describe("validateAuthConfig", () => {
  it.each(["test", "live"] as const)("accepts matching %s Clerk keys", (environment) => {
    expect(() => validateAuthConfig(authConfig(environment))).not.toThrow();
  });

  it.each([
    "https://app.example",
    "https://app.example:8443",
    "http://localhost:3000",
    "http://127.0.0.1:3000",
    "http://[::1]:3000",
  ])("accepts the canonical application origin %s", (appOrigin) => {
    expect(() => validateAuthConfig({ ...authConfig(), appOrigin })).not.toThrow();
  });

  it.each([
    "",
    "not-an-origin",
    "https://app.example/",
    "https://app.example/path",
    "https://app.example?private=value",
    "https://app.example#private",
    "https://private:credential@app.example",
    "http://app.example",
    "http://localhost.attacker.example",
    "ftp://app.example",
    "null",
  ])("rejects invalid application origins without reflecting them: %s", (appOrigin) => {
    let error: unknown;
    try {
      validateAuthConfig({ ...authConfig(), appOrigin });
    } catch (caught) {
      error = caught;
    }

    expect(error).toBeInstanceOf(Error);
    expect((error as Error).message).toMatch(/^NUXT_APP_ORIGIN must be/);
    if (appOrigin) expect((error as Error).message).not.toContain(appOrigin);
  });

  it.each([
    {
      name: "missing publishable key",
      setting: "NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY",
      publicKey: undefined,
    },
    { name: "empty publishable key", setting: "NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY", publicKey: "" },
    {
      name: "unrecognized publishable key",
      setting: "NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY",
      publicKey: "invalid-public-value",
    },
    {
      name: "malformed encoded frontend",
      setting: "NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY",
      publicKey: ["pk", "test", Buffer.from("https://clerk.example").toString("base64")].join("_"),
    },
    { name: "missing secret key", setting: "NUXT_CLERK_SECRET_KEY", secretKey: undefined },
    { name: "empty secret key", setting: "NUXT_CLERK_SECRET_KEY", secretKey: "" },
    {
      name: "unrecognized secret key",
      setting: "NUXT_CLERK_SECRET_KEY",
      secretKey: randomBytes(24).toString("hex"),
    },
    {
      name: "publishable key used as secret",
      setting: "NUXT_CLERK_SECRET_KEY",
      secretKey: ["pk", "test", randomBytes(24).toString("hex")].join("_"),
    },
  ])("rejects $name and identifies only the setting", (invalid) => {
    const config = authConfig();
    if ("publicKey" in invalid) config.public.clerk.publishableKey = invalid.publicKey;
    if ("secretKey" in invalid) config.clerk.secretKey = invalid.secretKey;

    let error: unknown;
    try {
      validateAuthConfig(config);
    } catch (caught) {
      error = caught;
    }

    expect(error).toBeInstanceOf(Error);
    const message = (error as Error).message;
    expect(message).toContain(invalid.setting);
    for (const value of [config.clerk.secretKey, config.public.clerk.publishableKey]) {
      if (value) expect(message).not.toContain(value);
    }
  });

  it.each(["test", "live"] as const)(
    "rejects a %s public key paired with the other environment",
    (environment) => {
      const config = authConfig(environment);
      config.clerk = authConfig(environment === "test" ? "live" : "test").clerk;

      expect(() => validateAuthConfig(config)).toThrow("Clerk keys must use the same environment.");
    },
  );
});
