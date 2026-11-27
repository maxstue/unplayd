// Railway infrastructure for Unplayd: web, user-api, catalog-api and PostgreSQL from this repository.
// Only web gets a public domain; the backends and the database are reachable over the
// private network only. Secrets use preserve() and are set in Railway, never here.
// Workflow: `railway config plan`, review, then `railway config apply` (runbook: Unplayd Operations in Linear).
import {
  bucket,
  defineRailway,
  github,
  group,
  image,
  postgres,
  preserve,
  project,
  ref,
  service,
} from "railway/iac";

const repository = "maxstue/unplayd";
const ports = { web: 8444, userApi: 8445, catalogApi: 8446 } as const;

// Deploy only after the GitHub checks of the commit (CI incl. the `quality` gate) succeeded.
const source = (rootDirectory?: string) =>
  github(repository, { branch: "main", checkSuites: true, rootDirectory });

// Railway's private network and healthchecks use IPv6; "::" also accepts IPv4.
const host = "::";

// Railway probes the healthcheck only while a deployment starts, so the APIs use readiness:
// a new version takes traffic only once it reaches its database. Readiness never checks IGDB.
const readiness = "/health/ready";

// Hobby plans have no Railway Postgres backups. pg_dump runs as a cron job into a dedicated bucket
// (github.com/maxstue/postgres-s3-backups-cleanup, as in kijk). Keep both images on the same release.
const backupRelease = "2026.11.0";

export default defineRailway(() => {
  const db = postgres("postgres");
  const privateHost = "${{postgres.RAILWAY_PRIVATE_DOMAIN}}:5432/${{postgres.PGDATABASE}}";

  const catalogApi = service("catalog-api", {
    source: source("apps/catalog-api"),
    // The Dockerfile in the root directory is used automatically. The image contains only
    // apps/catalog-api, so only that directory triggers a deploy.
    build: { watchPatterns: ["/apps/catalog-api/**"] },
    // Runs once per deployment in its own container, before the new version starts.
    preDeploy: "/migrate",
    deploy: { preDeployTimeoutSeconds: 300 },
    healthcheck: readiness,
    env: {
      PORT: String(ports.catalogApi),
      DATABASE_URL: `postgresql://${privateHost}?search_path=catalog&sslmode=require`,
      DATABASE_USER: "catalog",
      DATABASE_PASSWORD: preserve(),
      CATALOG_SERVICE_TOKEN: preserve(),
      IGDB_CLIENT_ID: preserve(),
      IGDB_CLIENT_SECRET: preserve(),
    },
  });

  const userApi = service("user-api", {
    source: source("apps/user-api"),
    build: { watchPatterns: ["/apps/user-api/**"] },
    preDeploy: "/app/bin/user-api migrate",
    deploy: { preDeployTimeoutSeconds: 300 },
    healthcheck: readiness,
    env: {
      HOST: host,
      PORT: String(ports.userApi),
      DATABASE_URL: `jdbc:postgresql://${privateHost}?currentSchema=user_data&sslmode=require`,
      DATABASE_USER: "user_api",
      DATABASE_PASSWORD: preserve(),
      CLERK_ISSUER: preserve(),
      CLERK_AUTHORIZED_PARTY: "https://${{web.RAILWAY_PUBLIC_DOMAIN}}",
      CATALOG_API_URL: `http://\${{catalog-api.RAILWAY_PRIVATE_DOMAIN}}:${ports.catalogApi}`,
      CATALOG_SERVICE_TOKEN: catalogApi.env.CATALOG_SERVICE_TOKEN,
    },
  });

  const web = service("web", {
    // The web image builds from the workspace root: it needs the root manifests and lockfile.
    source: source(),
    build: {
      builder: "DOCKERFILE",
      dockerfilePath: "apps/web/Dockerfile",
      watchPatterns: [
        "/apps/web/**",
        // Web consumes both contracts (generated types in apps/web/server/clients/generated).
        "/contracts/**",
        "/package.json",
        "/pnpm-lock.yaml",
        "/pnpm-workspace.yaml",
        "/.dockerignore",
        // Copied into the web build so the frozen workspace install resolves.
        "/apps/user-api/package.json",
        "/apps/catalog-api/package.json",
      ],
    },
    healthcheck: "/api/health",
    env: {
      HOST: host,
      PORT: String(ports.web),
      NUXT_APP_ORIGIN: "https://${{RAILWAY_PUBLIC_DOMAIN}}",
      NUXT_USER_API_URL: `http://\${{user-api.RAILWAY_PRIVATE_DOMAIN}}:${ports.userApi}`,
      NUXT_CATALOG_API_URL: `http://\${{catalog-api.RAILWAY_PRIVATE_DOMAIN}}:${ports.catalogApi}`,
      NUXT_CATALOG_SERVICE_TOKEN: catalogApi.env.CATALOG_SERVICE_TOKEN,
      NUXT_PUBLIC_CLERK_PUBLISHABLE_KEY: preserve(),
      NUXT_CLERK_SECRET_KEY: preserve(),
    },
  });

  // Dedicated to backups: the cleanup retention applies to every object in it.
  const backups = bucket("backups", { region: "ams" });

  const backup = service("backup", {
    source: image(`ghcr.io/maxstue/postgres-s3-backups-cleanup-backup:${backupRelease}`),
    // Monthly on day 1 at 02:00 UTC, as in kijk; the job runs once and exits.
    deploy: { cronSchedule: "0 2 1 * *", restartPolicyType: "NEVER" },
    env: {
      // Superuser URL over the private network: the dump covers user_data and catalog.
      BACKUP_DATABASE_URL: ref(db, "DATABASE_URL"),
      AWS_ACCESS_KEY_ID: ref(backups, "ACCESS_KEY_ID"),
      AWS_SECRET_ACCESS_KEY: ref(backups, "SECRET_ACCESS_KEY"),
      AWS_S3_BUCKET: ref(backups, "BUCKET"),
      AWS_S3_ENDPOINT: ref(backups, "ENDPOINT"),
      AWS_S3_REGION: "auto",
      AWS_S3_FORCE_PATH_STYLE: "true",
      RUN_ON_STARTUP: "true",
      OTEL_LOGS_EXPORTER: "none",
      OTEL_METRICS_EXPORTER: "none",
    },
  });

  const backupCleanup = service("backup-cleanup", {
    source: image(`ghcr.io/maxstue/postgres-s3-backups-cleanup-cleanup:${backupRelease}`),
    // January and July on day 1 at 03:00 UTC, after that month's backup.
    deploy: { cronSchedule: "0 3 1 1,7 *", restartPolicyType: "NEVER" },
    env: {
      ACCESS_KEY_ID: ref(backups, "ACCESS_KEY_ID"),
      SECRET_ACCESS_KEY: ref(backups, "SECRET_ACCESS_KEY"),
      BUCKET: ref(backups, "BUCKET"),
      ENDPOINT: ref(backups, "ENDPOINT"),
      REGION: "auto",
      AWS_S3_FORCE_PATH_STYLE: "true",
      // About six monthly backups; the newest one is always kept.
      RETENTION_DAYS: "183",
      KEEP_MIN: "1",
      // Preview only until the first runs' logs are reviewed (runbook: Unplayd Operations).
      DRY_RUN: "true",
    },
  });

  return project("unplayd", {
    resources: [
      group("Backend", [db, userApi, catalogApi]),
      group("Backups", [backups, backup, backupCleanup]),
      web,
    ],
  });
});
