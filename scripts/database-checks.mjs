// Runs only against the isolated smoke database, never the development stack.
export function checkDatabase({ compose, sql, check }) {
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
  function truth(role, query, label) {
    check(execute(role, query).endsWith("t"), label);
  }
  function rejected(role, query, label) {
    check(sql(role, query).status !== 0, label);
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
  truth(
    "catalog",
    "SELECT count(*)=1 FROM catalog.games;",
    "catalog migration V1 accepts initial data",
  );
  truth(
    "user_api",
    "SELECT count(*)=1 FROM user_data.library_entries;",
    "user migration V1 accepts initial data",
  );

  for (const service of ["user-api", "catalog-api"]) migrate(service);
  truth(
    "catalog",
    `SELECT title='Fixture game' FROM catalog.games WHERE id='${game}';`,
    "catalog upgrade preserves stable game identity and data",
  );
  truth(
    "user_api",
    `SELECT status='backlog' FROM user_data.game_statuses WHERE library_entry_id='${entryA}';`,
    "user upgrade preserves entries and backfills their separate status",
  );
  for (const service of ["user-api", "catalog-api"]) migrate(service);
  truth(
    "user_api",
    "SELECT count(*)=1 FROM user_data.library_entries;",
    "repeating user migrations preserves data",
  );
  truth(
    "catalog",
    "SELECT count(*)=1 FROM catalog.games;",
    "repeating catalog migrations preserves data",
  );

  rejected(
    "user_api",
    `INSERT INTO user_data.library_entries(user_id,game_id,ownership,snapshot_title) VALUES ('${userA}','${game}','wishlist','Duplicate');`,
    "duplicate user/game library entry is rejected",
  );
  execute(
    "user_api",
    `BEGIN; INSERT INTO user_data.users(id,clerk_subject) VALUES ('${userB}','fixture-user-b'); INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES ('${entryB}','${userB}','${game}','wishlist','Fixture game'); INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES ('${entryB}','backlog'); COMMIT;`,
  );
  truth(
    "user_api",
    `SELECT count(*)=2 FROM user_data.library_entries WHERE game_id='${game}';`,
    "different users can independently track the same game",
  );
  for (const value of [0, 11])
    rejected(
      "user_api",
      `INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${entryA}',${value});`,
      `rating ${value} is rejected by the database constraint`,
    );
  rejected(
    "user_api",
    `UPDATE user_data.game_statuses SET status='invalid' WHERE library_entry_id='${entryA}';`,
    "invalid play status is rejected",
  );
  rejected(
    "user_api",
    `INSERT INTO user_data.favorites(library_entry_id) VALUES ('${rollbackEntry}');`,
    "personal relationship requires a local library entry",
  );
  rejected(
    "user_api",
    `UPDATE user_data.library_entries SET ownership='invalid' WHERE id='${entryA}';`,
    "invalid ownership is rejected independently of status",
  );
  rejected(
    "user_api",
    `INSERT INTO user_data.notes(library_entry_id,text) VALUES ('${entryA}',repeat('a',10001));`,
    "oversized note is rejected",
  );

  execute(
    "user_api",
    `BEGIN; INSERT INTO user_data.favorites(library_entry_id) VALUES ('${entryA}'); INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${entryA}',10); COMMIT;`,
  );
  truth(
    "user_api",
    `SELECT status='backlog' FROM user_data.game_statuses WHERE library_entry_id='${entryA}';`,
    "favoriting and rating preserve play status",
  );
  execute(
    "user_api",
    `UPDATE user_data.game_statuses SET status='playing' WHERE library_entry_id='${entryA}'; DELETE FROM user_data.favorites WHERE library_entry_id='${entryA}';`,
  );
  truth(
    "user_api",
    `SELECT s.status='playing' AND r.value=10 AND l.ownership='owned' FROM user_data.game_statuses s JOIN user_data.ratings r USING(library_entry_id) JOIN user_data.library_entries l ON l.id=s.library_entry_id WHERE l.id='${entryA}';`,
    "status and favorite changes preserve rating and ownership",
  );
  rejected(
    "user_api",
    `BEGIN; INSERT INTO user_data.library_entries(id,user_id,game_id,ownership,snapshot_title) VALUES ('${rollbackEntry}','${userA}','${otherGame}','wishlist','Rollback'); INSERT INTO user_data.game_statuses(library_entry_id,status) VALUES ('${rollbackEntry}','backlog'); INSERT INTO user_data.ratings(library_entry_id,value) VALUES ('${rollbackEntry}',0); COMMIT;`,
    "invalid compound write fails atomically",
  );
  truth(
    "user_api",
    `SELECT count(*)=0 FROM user_data.library_entries WHERE id='${rollbackEntry}';`,
    "failed compound write leaves no partial library entry",
  );

  execute(
    "catalog",
    `INSERT INTO catalog.external_game_ids(source,external_id,game_id) VALUES ('igdb','42','${game}');`,
  );
  rejected(
    "catalog",
    `INSERT INTO catalog.external_game_ids(source,external_id,game_id) VALUES ('igdb','42','${game}');`,
    "external game identity is unique",
  );
  rejected(
    "catalog",
    `INSERT INTO catalog.game_platforms(game_id,platform_id) VALUES ('${game}','${otherGame}');`,
    "catalog platform association enforces its local foreign key",
  );
  rejected(
    "catalog",
    `DELETE FROM catalog.games WHERE id='${game}';`,
    "catalog hard deletion is rejected",
  );
  execute("catalog", `UPDATE catalog.games SET is_active=false WHERE id='${game}';`);
  truth(
    "catalog",
    `SELECT NOT is_active FROM catalog.games WHERE id='${game}';`,
    "deactivation preserves the stable catalog identity",
  );

  execute(
    "user_api",
    `INSERT INTO user_data.favorites(library_entry_id) VALUES ('${entryA}'); INSERT INTO user_data.notes(library_entry_id,text) VALUES ('${entryA}','Fixture note'); DELETE FROM user_data.library_entries WHERE id='${entryA}';`,
  );
  truth(
    "user_api",
    `SELECT NOT EXISTS(SELECT FROM user_data.game_statuses WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.favorites WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.ratings WHERE library_entry_id='${entryA}') AND NOT EXISTS(SELECT FROM user_data.notes WHERE library_entry_id='${entryA}') AND EXISTS(SELECT FROM user_data.library_entries WHERE id='${entryB}');`,
    "deleting a library entry removes only its personal dependent records",
  );
  truth(
    "catalog",
    `SELECT count(*)=1 FROM catalog.games WHERE id='${game}';`,
    "removing a personal entry preserves the catalog game",
  );
  rejected(
    "user_api",
    "SELECT * FROM catalog.games;",
    "user_api cannot read actual catalog tables",
  );
  rejected(
    "catalog",
    "SELECT * FROM user_data.users;",
    "catalog cannot read actual personal tables",
  );
}
