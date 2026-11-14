import { test, expect } from "@playwright/test";

const knight = "10000000-0000-4000-8000-000000000001";
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
