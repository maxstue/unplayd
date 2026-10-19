import { spawnSync } from "node:child_process";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const app = process.argv[2];
if (app && !["user-api", "catalog-api"].includes(app)) {
  console.error("Usage: node scripts/migrate.mjs [user-api|catalog-api]");
  process.exit(1);
}
for (const service of app ? [app] : ["user-api", "catalog-api"]) {
  const args = ["compose", "run", "--build", "--rm", "--no-deps"];
  if (service === "catalog-api") args.push("--entrypoint", "/migrate", service);
  else args.push(service, "bin/user-api", "migrate");
  const result = spawnSync("docker", args, { cwd: root, stdio: "inherit" });
  if (result.error || result.status !== 0) {
    console.error(
      `${service} migration failed. Start PostgreSQL and configure the local .env first.`,
    );
    process.exit(1);
  }
}
