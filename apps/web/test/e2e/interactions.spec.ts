import { test, expect } from "@playwright/test";

const knight = "10000000-0000-4000-8000-000000000001";
const celeste = "10000000-0000-4000-8000-000000000002";
const fixture = "http://127.0.0.1:9347/control";

test.beforeEach(async ({ context, request }) => {
  await request.post(`${fixture}/reset`);
  await context.addCookies([
    { name: "fixture-user", value: "alice", url: "http://127.0.0.1:9346" },
  ]);
});

test("keyboard suggestion navigation retains the draft through details and return", async ({
  page,
}) => {
  await page.goto("/?query=Celeste");
  await expect(page.getByRole("heading", { name: "Suchergebnisse für „Celeste“" })).toBeVisible();
  const input = page.getByRole("searchbox");
  await input.fill("Hollow Knight");
  await expect(page.getByRole("button", { name: "Hollow Knight" })).toBeVisible();
  await input.press("ArrowDown");
  await page.getByRole("button", { name: "Hollow Knight" }).press("Enter");
  await expect(page).toHaveURL(new RegExp(`/games/${knight}\\?query=Hollow\\+Knight`));
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Zurück zur Suche" }).click();
  await expect(page.getByRole("searchbox")).toHaveValue("Hollow Knight");
  await page.reload();
  await expect(page.getByRole("searchbox")).toHaveValue("Hollow Knight");
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).toBeVisible();
});

test("a status mutation finishing after a filter change reconciles the current view", async ({
  page,
  request,
}) => {
  await request.post(`${fixture}/config`, { data: { holdStatus: true } });
  await page.goto("/library?status=backlog");
  await page.getByLabel("Spielstatus für Hollow Knight").selectOption("completed");
  await expect
    .poll(async () => (await (await request.get(`${fixture}/state`)).json()).statusPending)
    .toBe(true);
  const filtered = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/library" && url.searchParams.get("status") === "completed";
  });
  await page.getByLabel("Status", { exact: true }).selectOption("completed");
  await expect(page).toHaveURL(/status=completed/);
  await expect((await filtered).json()).resolves.toMatchObject({ items: [] });
  await request.post(`${fixture}/release-status`);
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).toBeVisible();
  await expect(page.getByLabel("Spielstatus für Hollow Knight")).toHaveValue("completed");
  await page.reload();
  await expect(page.getByLabel("Status", { exact: true })).toHaveValue("completed");
  await expect(page.getByLabel("Spielstatus für Hollow Knight")).toHaveValue("completed");
});

test("library load and removal failures allow recovery without losing the entry", async ({
  page,
  request,
}) => {
  await request.post(`${fixture}/config`, { data: { failLibrary: true } });
  await page.goto("/library");
  await expect(page.getByText("Die Bibliothek konnte nicht geladen werden.")).toBeVisible();
  await request.post(`${fixture}/config`, { data: { failLibrary: false } });
  await page.getByRole("button", { name: "Erneut versuchen", exact: true }).click();
  const remove = page.getByRole("button", { name: "Hollow Knight entfernen" });
  await remove.click();
  await page.getByRole("dialog").press("Escape");
  await expect(remove).toBeFocused();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await request.post(`${fixture}/config`, { data: { failDelete: true } });
  await remove.click();
  await page.getByRole("button", { name: "Spiel entfernen", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText(
    "Die Änderung konnte nicht gespeichert werden. Bitte versuche es erneut.",
  );
  await expect(remove).toBeVisible();
  await request.post(`${fixture}/config`, { data: { failDelete: false } });
  await remove.click();
  await page.getByRole("button", { name: "Spiel entfernen", exact: true }).click();
  await expect(remove).not.toBeVisible();
  await expect(page.getByRole("heading", { name: "Meine Bibliothek", exact: true })).toBeFocused();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Deine Bibliothek wartet auf das erste Spiel" }),
  ).toBeVisible();
});

test("logout clears cached ownership before another user signs in", async ({ page }) => {
  await page.goto(`/games/${knight}`);
  await expect(page.getByText("In deiner Bibliothek", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Abmelden" }).click();
  await expect(page).toHaveURL("http://127.0.0.1:9346/");
  await page.getByRole("link", { name: "Anmelden", exact: true }).first().click();
  await page.getByLabel("E-Mail-Adresse").fill("bob@example.com");
  await page.getByLabel("Passwort", { exact: true }).fill("test-password-only");
  await page.getByRole("button", { name: "Anmelden", exact: true }).click();
  await expect(page).toHaveURL("http://127.0.0.1:9346/dashboard");
  await page.getByRole("link", { name: "Bibliothek", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Celeste", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).not.toBeVisible();
});

test("favorites from search and the library stay separate from library state", async ({
  page,
  request,
}) => {
  await page.goto("/?query=Celeste");
  const favorite = page.getByRole("button", { name: "Celeste als Favorit markieren" });
  await expect(favorite).toHaveAttribute("aria-pressed", "false");
  // Pointer devices reveal the toggle on hover and keyboard focus; touch devices always show it.
  const card = page.locator("article", { has: page.getByRole("heading", { name: "Celeste" }) });
  await card.hover();
  await expect(favorite).toHaveCSS("opacity", "1");
  await page.mouse.move(0, 0);
  await favorite.focus();
  await expect(favorite).toHaveCSS("opacity", "1");
  await favorite.click();
  await expect(favorite).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText("In deiner Bibliothek")).toBeVisible();
  const state = await (await request.get(`${fixture}/state`)).json();
  expect(state.libraries.alice).toContainEqual(
    expect.objectContaining({ ownership: "wishlist", status: "backlog", favorite: true }),
  );

  await page.goto("/library");
  await page.getByLabel("Nur Favoriten").check();
  await expect(page).toHaveURL(/favorite=true/);
  await expect(page.getByRole("heading", { name: "Celeste", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).toBeHidden();
  await page.getByRole("button", { name: "Celeste als Favorit markieren" }).click();
  await expect(page.getByText("„Celeste“ aus Favoriten entfernt.")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Celeste", exact: true })).toBeHidden();
  await page.getByLabel("Nur Favoriten").uncheck();
  await expect(page.getByLabel("Spielstatus für Celeste")).toHaveValue("backlog");
});

test("the detail page favorites an existing entry without changing its status", async ({
  page,
}) => {
  await page.goto(`/games/${knight}`);
  await expect(page.getByText("In deiner Bibliothek")).toBeVisible();
  const favorite = page.getByRole("button", { name: "Favorit" });
  await favorite.click();
  await expect(favorite).toHaveAttribute("aria-pressed", "true");
  await page.reload();
  await expect(page.getByRole("button", { name: "Favorit" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await page.goto("/library?favorite=true");
  await expect(page.getByLabel("Spielstatus für Hollow Knight")).toHaveValue("backlog");
});

test("ratings and notes are saved in the detail view and notes render as plain text", async ({
  page,
  request,
}) => {
  page.on("dialog", () => {
    throw new Error("A note must never execute as HTML");
  });
  await page.goto(`/games/${knight}`);
  await expect(page.getByText("In deiner Bibliothek")).toBeVisible();
  const rating = page.getByLabel("Bewertung (1–10)");
  await rating.selectOption("8");
  await expect(page.getByText("Bewertung gespeichert.")).toBeVisible();
  const text = '<img src=x onerror="alert(1)"> Zweiter Durchlauf mit allen Fähigkeiten';
  const note = page.getByLabel("Persönliche Notiz");
  await note.fill(text);
  await expect(page.locator("#note-length")).toHaveText(`${[...text].length} / 10.000 Zeichen`);
  await page.getByRole("button", { name: "Notiz speichern" }).click();
  await expect(page.getByText("Notiz gespeichert.")).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("Bewertung (1–10)")).toHaveValue("8");
  await expect(page.getByLabel("Persönliche Notiz")).toHaveValue(text);
  await expect(page.locator("img[src=x]")).toHaveCount(0);
  const state = await (await request.get(`${fixture}/state`)).json();
  expect(state.libraries.alice).toContainEqual(
    expect.objectContaining({ gameId: knight, status: "backlog", rating: 8, note: text }),
  );

  await page.getByRole("button", { name: "Notiz löschen" }).click();
  await expect(page.getByText("Notiz gelöscht.")).toBeVisible();
  await expect(page.getByLabel("Persönliche Notiz")).toHaveValue("");
  await page.getByLabel("Bewertung (1–10)").selectOption("");
  await expect(page.getByText("Bewertung entfernt.")).toBeVisible();
  const cleared = await (await request.get(`${fixture}/state`)).json();
  expect(cleared.libraries.alice).toContainEqual(
    expect.objectContaining({ gameId: knight, rating: null, note: null }),
  );
});

test("the first rating adds a missing game to the library as wishlist backlog", async ({
  page,
  request,
}) => {
  await page.goto(`/games/${celeste}`);
  await expect(page.getByText("Bewertung und Notiz fügen das Spiel")).toBeVisible();
  await page.getByLabel("Bewertung (1–10)").selectOption("6");
  await expect(page.getByText("Zur Bibliothek hinzugefügt")).toBeVisible();
  await expect(page.getByLabel("Bewertung (1–10)")).toHaveValue("6");
  const state = await (await request.get(`${fixture}/state`)).json();
  expect(state.libraries.alice).toContainEqual(
    expect.objectContaining({ ownership: "wishlist", status: "backlog", rating: 6 }),
  );
});

test("an unsaved note survives the first rating that adds the game", async ({ page }) => {
  await page.goto(`/games/${celeste}`);
  const note = page.getByLabel("Persönliche Notiz");
  await note.fill("Ungespeicherter Entwurf");
  await page.getByLabel("Bewertung (1–10)").selectOption("6");
  await expect(page.getByText("Zur Bibliothek hinzugefügt")).toBeVisible();
  await expect(page.getByLabel("Bewertung (1–10)")).toHaveValue("6");
  await expect(note).toHaveValue("Ungespeicherter Entwurf");
  await page.getByRole("button", { name: "Favorit" }).click();
  await expect(page.getByRole("button", { name: "Favorit" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(note).toHaveValue("Ungespeicherter Entwurf");
});

test("drawing the next game never changes state until the game is explicitly started", async ({
  page,
  request,
}) => {
  const knightState = async () =>
    (await (await request.get(`${fixture}/state`)).json()).libraries.alice.find(
      (item: { gameId: string }) => item.gameId === knight,
    );
  await page.goto("/dashboard");
  await page.getByRole("link", { name: "Nächstes Spiel ziehen" }).click();
  await expect(page).toHaveURL(/\/next-game$/);
  await page.getByLabel("Plattform").selectOption({ label: "PC" });
  await expect(page).toHaveURL(/platformId=/);
  await page.getByRole("button", { name: "Spiel ziehen" }).click();
  const card = page.getByRole("article", { name: "Hollow Knight" });
  await expect(card).toBeVisible();
  await expect(card).toBeFocused();
  expect(await knightState()).toMatchObject({ status: "backlog", favorite: false, rating: null });

  await page.getByRole("button", { name: "Neu ziehen" }).click();
  await expect(
    page.getByText("Das ist das einzige passende Spiel in deinem Backlog."),
  ).toBeVisible();
  expect((await knightState()).status).toBe("backlog");

  await page.getByRole("button", { name: "Jetzt spielen" }).click();
  await expect(
    page.getByText("Viel Spaß! Das Spiel steht jetzt auf „Spiele ich gerade“."),
  ).toBeVisible();
  expect((await knightState()).status).toBe("playing");

  await page.getByRole("button", { name: "Neu ziehen" }).click();
  await expect(
    page.getByRole("heading", { name: "Kein Backlog-Spiel für diese Plattform" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Alle Plattformen" }).click();
  await expect(page).toHaveURL(/\/next-game$/);
  await page.getByRole("button", { name: "Spiel ziehen" }).click();
  await expect(page.getByRole("heading", { name: "Dein Backlog ist leer" })).toBeVisible();
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "Hollow Knight", exact: true })).toBeVisible();
});

test("a catalog outage still draws from stored data without motion when reduced", async ({
  page,
  request,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await request.post(`${fixture}/config`, { data: { failCatalog: true } });
  await page.goto("/next-game");
  await page.getByRole("button", { name: "Spiel ziehen" }).click();
  const card = page.getByRole("article", { name: "Hollow Knight" });
  await expect(card).toBeVisible();
  await expect(card).toHaveCSS("animation-name", "none");
  await expect(
    page.getByText("Katalog nicht erreichbar. Gespeicherte Spieldaten werden angezeigt."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Jetzt spielen" })).toBeEnabled();
});
