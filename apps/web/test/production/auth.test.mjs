import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { readdir, readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { once } from "node:events";
import { createServer } from "node:net";
import { before, after, test } from "node:test";

const build = resolve(".output");
const privateValues = {
  NUXT_CLERK_SECRET_KEY: ["sk", "test", randomBytes(24).toString("hex")].join("_"),
  NUXT_CATALOG_SERVICE_TOKEN: randomBytes(32).toString("hex"),
};
let server;
let baseUrl;

before(async () => {
  await readFile(join(build, "server/index.mjs")); // Build first, as in CI and the documented check/build/test flow.
  const probe = createServer();
  probe.listen(0, "127.0.0.1");
  await once(probe, "listening");
  const port = probe.address().port;
  await new Promise((resolveClosed) => probe.close(resolveClosed));
  server = spawn(process.execPath, [join(build, "server/index.mjs")], {
    env: {
      ...process.env,
      ...privateValues,
      NODE_ENV: "production",
      HOST: "127.0.0.1",
      PORT: String(port),
      NITRO_PORT: String(port),
      NUXT_APP_ORIGIN: "http://localhost:8444",
      NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY: [
        "pk",
        "test",
        Buffer.from("clerk.example$").toString("base64"),
      ].join("_"),
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let diagnostic = "";
  server.stderr.on("data", (data) => {
    diagnostic += data.toString();
    for (const value of Object.values(privateValues))
      diagnostic = diagnostic.replaceAll(value, "[redacted]");
  });
  await new Promise((resolveReady, reject) => {
    const timeout = setTimeout(
      () => reject(new Error("Built Nuxt server did not become ready")),
      15_000,
    );
    server.once("exit", (code) => {
      clearTimeout(timeout);
      reject(new Error(`Built Nuxt server exited (${code}): ${diagnostic}`));
    });
    server.once("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    server.stdout.on("data", (data) => {
      const address = /Listening on (http:\/\/127\.0\.0\.1:\d+)/.exec(data.toString());
      if (address) {
        baseUrl = address[1];
        clearTimeout(timeout);
        resolveReady();
      }
    });
    // Consume output without putting SDK or server internals into test logs.
    server.stderr.resume();
  });
});

after(async () => {
  if (server && server.exitCode === null) {
    const exited = once(server, "exit");
    server.kill("SIGTERM");
    await exited;
  }
});

function excludesSecrets(text) {
  for (const value of Object.values(privateValues))
    assert.equal(text.includes(value), false, "Private runtime value crossed the browser boundary");
}

test("built app keeps public HTML, redirects and API errors free of private runtime credentials", async () => {
  for (const path of ["/", "/sign-in", "/sign-up", "/dashboard", "/api/health", "/api/me"]) {
    const response = await fetch(`${baseUrl}${path}`, {
      redirect: "manual",
      signal: AbortSignal.timeout(5_000),
    });
    const text = await response.text();
    excludesSecrets(text);
    excludesSecrets(JSON.stringify([...response.headers]));
    if (path === "/dashboard") {
      assert.equal(response.status, 302);
      assert.equal(new URL(response.headers.get("location"), baseUrl).pathname, "/sign-in");
    } else if (path === "/api/me") assert.equal(response.status, 401);
    else assert.equal(response.status, 200);
    assert.equal(response.headers.get("cache-control"), "no-store");
    assert.doesNotMatch(text, /"secretKey"\s*:/);
    assert.doesNotMatch(text, /"catalogServiceToken"\s*:/);
  }
});

test("built BFF rejects forged sessions and cross-origin mutations without reflecting credentials", async () => {
  for (const options of [
    { headers: { Authorization: `Bearer ${randomBytes(24).toString("hex")}` }, expected: 401 },
    { method: "POST", headers: { Origin: "https://foreign.example" }, expected: 403 },
    { method: "POST", expected: 403 },
  ]) {
    const { expected, ...request } = options;
    const response = await fetch(`${baseUrl}/api/me`, {
      ...request,
      signal: AbortSignal.timeout(5_000),
    });
    assert.equal(response.status, expected);
    assert.equal(response.headers.get("cache-control"), "no-store");
    excludesSecrets(await response.text());
  }
});

test("built browser files contain no private runtime credentials", async () => {
  async function inspect(directory) {
    for (const item of await readdir(directory, { withFileTypes: true })) {
      const file = join(directory, item.name);
      if (item.isDirectory()) await inspect(file);
      else excludesSecrets(await readFile(file, "utf8"));
    }
  }
  await inspect(join(build, "public"));
});
