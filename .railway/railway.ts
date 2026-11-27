// Railway infrastructure for Unplayd: web, user-api, catalog-api and PostgreSQL from this repository.
// Only web gets a public domain; the backends and the database are reachable over the
// private network only. Secrets use preserve() and are set in Railway, never here.
// Workflow: `railway config plan`, review, then `railway config apply` (runbook: Unplayd Operations in Linear).
import { database, defineRailway, github, group, preserve, project, service } from "railway/iac";

const repository = "maxstue/unplayd";
const ports = { web: 8444, userApi: 8445, catalogApi: 8446 } as const;

// Deploy only after the GitHub checks of the commit (CI incl. the `quality` gate) succeeded.
const source = (rootDirectory?: string) =>
  github(repository, {
    branch: "main",
    checkSuites: true,
    ...(rootDirectory ? { rootDirectory } : {}),
  });

// Railway's default policy, stated explicitly: crashed containers restart, a crash loop stops.
const restart = { restartPolicyType: "ON_FAILURE", restartPolicyMaxRetries: 10 } as const;

// Railway's private network and healthchecks use IPv6; "::" also accepts IPv4.
const host = "::";

export default defineRailway(() => {
  // The postgres() helper adds a public TCP proxy when creating a database.
  const db = database("postgres", "private", {
    image: "ghcr.io/railwayapp-templates/postgres-ssl:18",
    defaultMountPath: "/var/lib/postgresql/data",
  });
  const privateHost = "${{postgres.RAILWAY_PRIVATE_DOMAIN}}:5432/${{postgres.PGDATABASE}}";

  const catalogApi = service("catalog-api", {
    source: source("apps/catalog-api"),
    // Railway builds the Dockerfile in the root directory; it stores no explicit builder for it.
    build: {
      // The image only contains apps/catalog-api; contracts are verified in CI, not built in.
      watchPatterns: ["/apps/catalog-api/**"],
    },
    // Runs once per deployment in its own container, before the new version starts.
    preDeploy: "/migrate",
    deploy: { preDeployTimeoutSeconds: 300, ...restart },
    healthcheck: "/health/live",
    healthcheckTimeout: 60,
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
    deploy: { preDeployTimeoutSeconds: 300, ...restart },
    healthcheck: "/health/live",
    healthcheckTimeout: 120,
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
        "/package.json",
        "/pnpm-lock.yaml",
        "/pnpm-workspace.yaml",
        "/.dockerignore",
        // Copied into the web build so the frozen workspace install resolves.
        "/apps/user-api/package.json",
        "/apps/catalog-api/package.json",
      ],
    },
    deploy: restart,
    healthcheck: "/api/health",
    healthcheckTimeout: 60,
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

  return project("unplayd", {
    resources: [group("Backend", [db, userApi, catalogApi]), web],
  });
});
