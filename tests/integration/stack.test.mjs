import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

// The caller owns Compose startup and cleanup; these are disposable database tests.
test("HTTP and PostgreSQL integration", async (t) => {
  assert.match(process.env.COMPOSE_PROJECT_NAME ?? "", /^unplayd-(ci|test)-/);
  assert.equal(process.env.COMPOSE_FILE, "compose.yaml:compose.test.yaml");

  function compose(args, { allowFailure = false } = {}) {
    const result = spawnSync("docker", ["compose", "--env-file", "/dev/null", ...args], {
      cwd: root,
      encoding: "utf8",
      timeout: 120_000,
    });
    if (!allowFailure) assert.equal(result.status, 0, result.stderr);
    return result;
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

  async function check(condition, label) {
    await t.test(label, () => assert.ok(condition, label));
  }

  for (const [service, port, path] of [
    ["web", 3000, "/api/health"],
    ["user-api", 8081, "/health/live"],
    ["catalog-api", 8082, "/health/live"],
  ]) {
    const address = compose(["port", service, String(port)]).stdout.trim();
    const url = `http://${address}${path}`;
    const ready = spawnSync(
      "curl",
      [
        "--fail",
        "--silent",
        "--show-error",
        "--retry",
        "30",
        "--retry-all-errors",
        "--retry-delay",
        "1",
        "--max-time",
        "5",
        url,
      ],
      { encoding: "utf8", timeout: 180_000 },
    );
    await t.test(`${service} HTTP health and service identity`, () => {
      assert.equal(ready.status, 0, ready.stderr);
      const body = JSON.parse(ready.stdout);
      assert.equal(body.status, "ok");
      assert.equal(body.service, service);
    });
  }
  const webAddress = compose(["port", "web", "3000"]).stdout.trim();
  await t.test("Nuxt renders the application page", async () => {
    const response = await fetch(`http://${webAddress}/`, { signal: AbortSignal.timeout(5_000) });
    assert.equal(response.status, 200);
    assert.match(await response.text(), /Unplayd/);
  });

  function migrate(service, target) {
    const args = ["run", "--rm", "--no-deps"];
    if (service === "catalog-api") args.push("--entrypoint", "/migrate", service);
    else args.push(service, "bin/user-api", "migrate");
    if (target) args.push(String(target));
    compose(args);
  }
  function execute(role, query) {
    const result = sql(role, query);
    if (result.status !== 0) throw new Error(`${role} database fixture failed: ${result.stderr}`);
    return result.stdout.trim();
  }
  async function truth(role, query, label) {
    await check(execute(role, query).endsWith("t"), label);
  }
  async function rejected(role, query, label) {
    await check(sql(role, query).status !== 0, label);
  }
  const game = "10000000-0000-4000-8000-000000000001";
  const otherGame = "10000000-0000-4000-8000-000000000002";
  const userA = "20000000-0000-4000-8000-000000000001";
  const userB = "20000000-0000-4000-8000-000000000002";
  const entryA = "30000000-0000-4000-8000-000000000001";
  const entryB = "30000000-0000-4000-8000-000000000002";
  const rollbackEntry = "30000000-0000-4000-8000-000000000003";

  for (const service of ["user-api", "catalog-api"]) migrate(service, 1);
  execute("catalog", `INSERT INTO catalog.games(id,title) VALUES ('${game}','Fixture game');`);
  execute(
    "user_api",
    `INSERT INTO user_data.users(id,clerk_subject) VALUES ('${userA}','fixture-user-a'); INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES ('${entryA}','${userA}','${game}','owned','Fixture game');`,
  );
  await truth(
    "catalog",
    "SELECT count(*)=1 FROM catalog.games;",
    "catalog migration V1 accepts initial data",
  );
  await truth(
    "user_api",
    "SELECT count(*)=1 FROM user_data.library_entries;",
    "user migration V1 accepts initial data",
  );

  for (const service of ["user-api", "catalog-api"]) migrate(service);
  await truth(
    "catalog",
    `SELECT title='Fixture game' FROM catalog.games WHERE id='${game}';`,
    "catalog upgrade preserves stable game identity and data",
  );
  await truth(
    "user_api",
    `SELECT status='backlog' FROM user_data.game_statuses WHERE library_entry_id='${entryA}';`,
    "user upgrade preserves entries and backfills their separate status",
  );
  for (const service of ["user-api", "catalog-api"]) migrate(service);
  await truth(
    "user_api",
    "SELECT count(*)=1 FROM user_data.library_entries;",
    "repeating user migrations preserves data",
  );
  await truth(
    "catalog",
    "SELECT count(*)=1 FROM catalog.games;",
    "repeating catalog migrations preserves data",
  );

  await rejected(
    "user_api",
    `INSERT INTO user_data.library_entries(user_id,game_id,ownership,snapshot_title) VALUES ('${userA}','${game}','wishlist','Duplicate');`,
    "duplicate user/game library entry is rejected",
  );
  execute(
    "user_api",
    `BEGIN; INSERT INTO user_data.users(id,clerk_subject) VALUES ('${userB}','fixture-user-b'); INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES ('${entryB}','${userB}','${game}','wishlist','Fixture game'); INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES ('${entryB}','backlog'); COMMIT;`,
  );
  await truth(
    "user_api",
    `SELECT count(*)=2 FROM user_data.library_entries WHERE game_id='${game}';`,
    "different users can independently track the same game",
  );
  for (const value of [0, 11])
    await rejected(
      "user_api",
      `INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${entryA}',${value});`,
      `rating ${value} is rejected by the database constraint`,
    );
  await rejected(
    "user_api",
    `UPDATE user_data.game_statuses SET status='invalid' WHERE library_entry_id='${entryA}';`,
    "invalid play status is rejected",
  );
  await rejected(
    "user_api",
    `INSERT INTO user_data.favorites(library_entry_id) VALUES ('${rollbackEntry}');`,
    "personal relationship requires a local library entry",
  );
  await rejected(
    "user_api",
    `UPDATE user_data.library_entries SET ownership='invalid' WHERE id='${entryA}';`,
    "invalid ownership is rejected independently of status",
  );
  await rejected(
    "user_api",
    `INSERT INTO user_data.notes(library_entry_id,text) VALUES ('${entryA}',repeat('a',10001));`,
    "oversized note is rejected",
  );

  execute(
    "user_api",
    `BEGIN; INSERT INTO user_data.favorites(library_entry_id) VALUES ('${entryA}'); INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${entryA}',10); COMMIT;`,
  );
  await truth(
    "user_api",
    `SELECT status='backlog' FROM user_data.game_statuses WHERE library_entry_id='${entryA}';`,
    "favoriting and rating preserve play status",
  );
  execute(
    "user_api",
    `UPDATE user_data.game_statuses SET status='playing' WHERE library_entry_id='${entryA}'; DELETE FROM user_data.favorites WHERE library_entry_id='${entryA}';`,
  );
  await truth(
    "user_api",
    `SELECT s.status='playing' AND r.value=10 AND l.ownership='owned' FROM user_data.game_statuses s JOIN user_data.ratings r USING(library_entry_id) JOIN user_data.library_entries l ON l.id=s.library_entry_id WHERE l.id='${entryA}';`,
    "status and favorite changes preserve rating and ownership",
  );
  await rejected(
    "user_api",
    `BEGIN; INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES ('${rollbackEntry}','${userA}','${otherGame}','wishlist','Rollback'); INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES ('${rollbackEntry}','backlog'); INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${rollbackEntry}',0); COMMIT;`,
    "invalid compound write fails atomically",
  );
  await truth(
    "user_api",
    `SELECT count(*)=0 FROM user_data.library_entries WHERE id='${rollbackEntry}';`,
    "failed compound write leaves no partial library entry",
  );

  execute(
    "catalog",
    `INSERT INTO catalog.external_game_ids(source,external_id,game_id) VALUES ('igdb','42','${game}');`,
  );
  await rejected(
    "catalog",
    `INSERT INTO catalog.external_game_ids(source,external_id,game_id) VALUES ('igdb','42','${game}');`,
    "external game identity is unique",
  );
  await rejected(
    "catalog",
    `INSERT INTO catalog.game_platforms(game_id,platform_id) VALUES ('${game}','${otherGame}');`,
    "catalog platform association enforces its local foreign key",
  );
  await rejected(
    "catalog",
    `DELETE FROM catalog.games WHERE id='${game}';`,
    "catalog hard deletion is rejected",
  );
  execute("catalog", `UPDATE catalog.games SET is_active=false WHERE id='${game}';`);
  await truth(
    "catalog",
    `SELECT NOT is_active FROM catalog.games WHERE id='${game}';`,
    "deactivation preserves the stable catalog identity",
  );

  execute(
    "user_api",
    `INSERT INTO user_data.favorites(library_entry_id) VALUES ('${entryA}'); INSERT INTO user_data.notes(library_entry_id,text) VALUES ('${entryA}','Fixture note'); DELETE FROM user_data.library_entries WHERE id='${entryA}';`,
  );
  await truth(
    "user_api",
    `SELECT NOT EXISTS(SELECT FROM user_data.game_statuses WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.favorites WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.ratings WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.notes WHERE library_entry_id='${entryA}') AND EXISTS(SELECT FROM user_data.library_entries WHERE id='${entryB}');`,
    "deleting a library entry removes only its personal dependent records",
  );
  await truth(
    "catalog",
    `SELECT count(*)=1 FROM catalog.games WHERE id='${game}';`,
    "removing a personal entry preserves the catalog game",
  );
  await rejected(
    "user_api",
    "SELECT * FROM catalog.games;",
    "user_api cannot read actual catalog tables",
  );
  await rejected(
    "catalog",
    "SELECT * FROM user_data.users;",
    "catalog cannot read actual personal tables",
  );
  for (const [role, own, foreign] of [
    ["user_api", "user_data", "catalog"],
    ["catalog", "catalog", "user_data"],
  ]) {
    const ownResult = sql(
      role,
      `CREATE TABLE ${own}.smoke_probe(id integer); INSERT INTO ${own}.smoke_probe VALUES (1); SELECT count(*) FROM ${own}.smoke_probe;`,
    );
    await t.test(`${role} can create/write/read its own schema`, () => {
      assert.equal(ownResult.status, 0, ownResult.stderr);
      assert.ok(ownResult.stdout.trim().endsWith("1"));
    });
    const foreignResult = sql(role, `SELECT * FROM ${foreign}.smoke_probe;`);
    await t.test(`${role} is denied access to the other schema`, () => {
      assert.notEqual(foreignResult.status, 0);
      assert.match(foreignResult.stderr, /permission denied/);
    });
  }
});
