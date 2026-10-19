import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { checkDatabase } from "./database-checks.mjs";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const project = `unplayd-smoke-${process.pid}-${randomBytes(4).toString("hex")}`;
const temporary = mkdtempSync(join(tmpdir(), "unplayd-smoke-"));
const override = join(temporary, "ports.yaml");
const credentials = Object.fromEntries(
  ["POSTGRES_PASSWORD", "USER_API_DB_PASSWORD", "CATALOG_DB_PASSWORD", "CATALOG_SERVICE_TOKEN"].map(
    (key) => [key, randomBytes(24).toString("hex")],
  ),
);
const env = { ...process.env, ...credentials };
let passed = 0;

writeFileSync(
  override,
  `services:
  postgres:
    ports: !reset []
  web:
    ports: !override ["127.0.0.1::3000"]
  user-api:
    ports: !override ["127.0.0.1::8081"]
  catalog-api:
    ports: !override ["127.0.0.1::8082"]
`,
);

function compose(args, { inherit = false, allowFailure = false } = {}) {
  const result = spawnSync(
    "docker",
    [
      "compose",
      "--env-file",
      "/dev/null",
      "--project-directory",
      root,
      "--project-name",
      project,
      "-f",
      join(root, "compose.yaml"),
      "-f",
      override,
      ...args,
    ],
    {
      cwd: root,
      env,
      encoding: "utf8",
      stdio: inherit ? "inherit" : "pipe",
      timeout: 600_000,
    },
  );
  if (!allowFailure && (result.error || result.status !== 0)) {
    throw new Error(`Compose ${args[0]} failed (${result.error?.message ?? result.status}).`);
  }
  return result;
}

function check(condition, label) {
  if (!condition) throw new Error(label);
  passed += 1;
  console.log(`PASS ${passed}: ${label}`);
}

async function getReady(service, port, path) {
  const address = compose(["port", service, String(port)]).stdout.trim();
  const url = `http://${address}${path}`;
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(2_000) });
      if (response.ok) return response;
      await response.body?.cancel();
    } catch {
      // Services can accept connections after Compose has started the process.
    }
    await delay(1_000);
  }
  throw new Error(`${service} did not become ready.`);
}

function sql(role, query) {
  const variable = role === "user_api" ? "USER_API_DB_PASSWORD" : "CATALOG_DB_PASSWORD";
  return compose(
    [
      "exec",
      "-T",
      "postgres",
      "sh",
      "-c",
      `PGPASSWORD="$${variable}" exec psql -h 127.0.0.1 -U "$1" -d unplayd -v ON_ERROR_STOP=1 -Atc "$2"`,
      "--",
      role,
      query,
    ],
    { allowFailure: true },
  );
}

try {
  console.log(
    `Starting isolated smoke stack ${project}; credentials stay in process/container environment.`,
  );
  compose(["up", "--build", "-d", "--wait", "--wait-timeout", "120"], { inherit: true });
  for (const [service, port, path] of [
    ["web", 3000, "/api/health"],
    ["user-api", 8081, "/health/live"],
    ["catalog-api", 8082, "/health/live"],
  ]) {
    const response = await getReady(service, port, path);
    const body = await response.json();
    check(
      body.status === "ok" && body.service === service,
      `${service} HTTP health and service identity`,
    );
  }
  const home = await getReady("web", 3000, "/");
  check((await home.text()).includes("Unplayd"), "Nuxt renders the application page");
  checkDatabase({ compose, sql, check });
  for (const [role, own, foreign] of [
    ["user_api", "user_data", "catalog"],
    ["catalog", "catalog", "user_data"],
  ]) {
    const ownResult = sql(
      role,
      `CREATE TABLE ${own}.smoke_probe(id integer); INSERT INTO ${own}.smoke_probe VALUES (1); SELECT count(*) FROM ${own}.smoke_probe;`,
    );
    check(
      ownResult.status === 0 && ownResult.stdout.trim().endsWith("1"),
      `${role} can create/write/read its own schema`,
    );
    const foreignResult = sql(role, `SELECT * FROM ${foreign}.smoke_probe;`);
    check(
      foreignResult.status !== 0 && foreignResult.stderr.includes("permission denied"),
      `${role} is denied access to the other schema`,
    );
  }
  console.log(`HTTP/database integration checks passed: ${passed}.`);
} catch (error) {
  console.error(error.message);
  const logs = compose(
    ["logs", "--no-color", "--tail", "80", "web", "user-api", "catalog-api", "postgres"],
    { allowFailure: true },
  );
  let output = `${logs.stdout ?? ""}${logs.stderr ?? ""}`;
  for (const value of Object.values(credentials)) output = output.replaceAll(value, "[REDACTED]");
  console.error(output);
  process.exitCode = 1;
} finally {
  const cleanup = compose(["down", "--volumes", "--remove-orphans"], { allowFailure: true });
  if (cleanup.status !== 0) {
    console.error(
      `Cleanup failed for ${project}; run docker compose --project-name ${project} down --volumes.`,
    );
    process.exitCode = 1;
  }
  rmSync(temporary, { recursive: true, force: true });
}
