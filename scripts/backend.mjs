import { spawnSync } from "node:child_process";
import { mkdirSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const [app, task] = process.argv.slice(2);
if (
  !["user-api", "catalog-api"].includes(app) ||
  !["check", "lint", "build", "test", "format", "dev"].includes(task)
) {
  console.error(
    "Usage: node scripts/backend.mjs <user-api|catalog-api> <check|lint|build|test|format|dev>",
  );
  process.exit(1);
}
const cwd = join(root, "apps", app);
const env = { ...process.env };
if (task === "dev") {
  env.DATABASE_URL =
    app === "user-api"
      ? (env.USER_API_DATABASE_URL ??
        "jdbc:postgresql://localhost:54329/unplayd?currentSchema=user_data")
      : (env.CATALOG_DATABASE_URL ??
        "postgresql://localhost:54329/unplayd?search_path=catalog&sslmode=disable");
  env.DATABASE_USER = app === "user-api" ? "user_api" : "catalog";
  env.DATABASE_PASSWORD = app === "user-api" ? env.USER_API_DB_PASSWORD : env.CATALOG_DB_PASSWORD;
}

function run(command, args, capture = false) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    env,
    stdio: capture ? "pipe" : "inherit",
  });
  if (capture && capture !== "quiet") {
    if (result.stdout) process.stdout.write(result.stdout);
    if (result.stderr) process.stderr.write(result.stderr);
  }
  if (result.error || result.status !== 0) {
    if (capture === "quiet") process.stderr.write(result.stdout ?? "");
    throw new Error(
      `${command} failed. Install the documented toolchain and retry (${result.error?.message ?? `exit ${result.status}`}).`,
    );
  }
  return result.stdout ?? "";
}

function hasTests(directory, suffix) {
  return readdirSync(directory, { withFileTypes: true }).some((entry) => {
    if (["build", ".gradle", "node_modules"].includes(entry.name)) return false;
    return entry.isDirectory()
      ? hasTests(join(directory, entry.name), suffix)
      : entry.name.endsWith(suffix);
  });
}

function gradle(tasks) {
  const args = ["--no-daemon", ...tasks];
  const java = spawnSync("java", ["-version"], { stdio: "ignore" });
  if (java.status === 0 && process.env.UNPLAYD_DOCKER_JDK !== "1") {
    run("./gradlew", args);
    return;
  }
  if (task === "dev")
    throw new Error(
      "Native user-api development needs JDK 25. Use docker compose up --build for the container stack.",
    );
  // Use the same build image as Docker/CI; no duplicate JDK image version.
  const image = readFileSync(join(cwd, "Dockerfile"), "utf8").match(/^FROM (\S+) AS build/m)?.[1];
  if (!image) throw new Error("Missing JDK build image in apps/user-api/Dockerfile.");
  console.log(
    "Using the documented Docker JDK fallback (local JDK unavailable or UNPLAYD_DOCKER_JDK=1).",
  );
  run("docker", [
    "run",
    "--rm",
    "--mount",
    `type=bind,src=${cwd},dst=/app`,
    "--mount",
    "type=volume,src=unplayd-gradle-cache,dst=/root/.gradle",
    "--workdir",
    "/app",
    image,
    "./gradlew",
    ...args,
  ]);
}

try {
  if (app === "user-api") {
    if (task === "test" && !hasTests(join(cwd, "src"), "Test.kt"))
      throw new Error("No Kotlin tests found; a test run must execute tests.");
    const tasks = {
      check: ["check"],
      lint: ["ktlintCheck"],
      build: ["installDist"],
      test: ["test", "jacocoTestReport"],
      format: ["ktlintFormat"],
      dev: ["run"],
    };
    gradle(tasks[task]);
    if (task === "test") {
      const reports = join(cwd, "build/test-results/test");
      let executed = 0;
      for (const name of readdirSync(reports).filter((name) => /^TEST-.*\.xml$/.test(name))) {
        const report = readFileSync(join(reports, name), "utf8");
        executed +=
          Number(report.match(/<testsuite[^>]* tests="(\d+)"/)?.[1] ?? 0) -
          Number(report.match(/<testsuite[^>]* skipped="(\d+)"/)?.[1] ?? 0);
      }
      if (executed < 1) throw new Error("Kotlin did not execute any tests.");
      if (readFileSync(join(cwd, "build/reports/jacoco/test/jacocoTestReport.xml")).length === 0) {
        throw new Error("JaCoCo did not produce a coverage report.");
      }
      console.log(`Kotlin tests executed: ${executed}. JaCoCo XML generated.`);
    }
  } else {
    if (task === "dev") run("go", ["run", "./cmd/catalog-api"]);
    if (task === "format") run("gofmt", ["-w", "."]);
    if (["lint", "check"].includes(task)) {
      const unformatted = run("gofmt", ["-l", "."], true).trim();
      if (unformatted) throw new Error("Go formatting differs. Run vp run format:catalog-api.");
      run("go", ["vet", "./..."]);
    }
    if (task === "build") {
      mkdirSync(join(cwd, "build"), { recursive: true });
      run("go", ["build", "-trimpath", "-o", "build/catalog-api", "./cmd/catalog-api"]);
      run("go", ["build", "-trimpath", "-o", "build/migrate", "./cmd/migrate"]);
    }
    if (task === "test") {
      if (!hasTests(cwd, "_test.go"))
        throw new Error("No Go tests found; a test run must execute tests.");
      const output = run(
        "go",
        ["test", "-race", "-json", "-coverprofile=coverage.out", "./..."],
        "quiet",
      );
      const events = output
        .trim()
        .split("\n")
        .map((line) => JSON.parse(line));
      const passed = events.filter((event) => event.Action === "pass" && event.Test);
      const topLevel = passed.filter((event) => !event.Test.includes("/"));
      if (topLevel.length < 1) throw new Error("Go did not execute any tests.");
      console.log(
        `Go tests passed: ${topLevel.length}, subtests passed: ${passed.length - topLevel.length}. Race check and coverage completed.`,
      );
    }
  }
} catch (error) {
  console.error(error.message);
  process.exit(1);
}
