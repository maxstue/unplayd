import { describe, expect, it, vi } from "vite-plus/test";
import { page, userEvent } from "vite-plus/test/browser";
import { render } from "vitest-browser-vue";
import GameSearchField from "../../app/components/GameSearchField.vue";
import GameCover from "../../app/components/GameCover.vue";
import DialogHarness from "./dialog-harness.vue";
import type { LibraryEntryView } from "../../shared/library";

const game = {
  id: "10000000-0000-4000-8000-000000000001",
  title: "Hollow Knight",
  coverUrl: null,
  platforms: [],
};
const entry: LibraryEntryView = {
  gameId: game.id,
  platformId: null,
  ownership: "owned",
  status: "backlog",
  game,
  display: game,
  catalogStatus: "available",
};

describe("native removal dialog", () => {
  it.each(["{Escape}", "Abbrechen", "Schließen"])(
    "cancels with %s and restores focus to its trigger",
    async (action) => {
      await render(DialogHarness, { props: { entry }, global: { components: { GameCover } } });
      const trigger = page.getByRole("button", { name: "Entfernen öffnen" });
      await trigger.click();
      await expect.element(page.getByRole("dialog")).toBeVisible();
      await expect.element(page.getByRole("button", { name: "Abbrechen" })).toHaveFocus();
      if (action === "{Escape}") {
        await userEvent.keyboard(action);
      } else {
        await page.getByRole("button", { name: action, exact: true }).click();
      }
      await expect.poll(() => document.querySelector("dialog")?.open).toBe(false);
      await expect.element(page.getByRole("status")).toHaveTextContent("Abgebrochen");
      await expect.element(trigger).toHaveFocus();
    },
  );

  it("confirms the selected entry exactly once", async () => {
    const confirm = vi.fn();
    await render(DialogHarness, {
      props: { entry, onConfirm: confirm },
      global: { components: { GameCover } },
    });
    await page.getByRole("button", { name: "Entfernen öffnen" }).click();
    await page.getByRole("button", { name: "Spiel entfernen", exact: true }).click();
    await expect.poll(() => document.querySelector("dialog")?.open).toBe(false);
    await expect.element(page.getByRole("status")).toHaveTextContent(game.id);
    expect(confirm).toHaveBeenCalledExactlyOnceWith(game.id);
  });
});

describe("game search keyboard interactions", () => {
  it("selects a suggestion with the current trimmed draft", async () => {
    vi.stubGlobal(
      "$fetch",
      vi.fn().mockResolvedValue({
        items: [{ game, matchedChildren: [], childCounts: [] }],
        nextCursor: null,
      }),
    );
    const select = vi.fn();
    await render(GameSearchField, {
      props: { query: "old query", enabled: true, onSelect: select },
    });
    await page.getByRole("searchbox").fill(" Hollow Knight ");
    const suggestion = page.getByRole("button", { name: "Hollow Knight" });
    await expect.element(suggestion).toBeVisible();
    await userEvent.keyboard("{ArrowDown}");
    await expect.element(suggestion).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(select).toHaveBeenCalledExactlyOnceWith(game, "Hollow Knight");
    await expect
      .element(page.getByRole("list", { name: "Spielvorschläge" }))
      .not.toBeInTheDocument();
  });

  it("dismisses suggestions with Escape and submits the draft with Enter", async () => {
    vi.stubGlobal(
      "$fetch",
      vi.fn().mockResolvedValue({
        items: [{ game, matchedChildren: [], childCounts: [] }],
        nextCursor: null,
      }),
    );
    const search = vi.fn();
    const select = vi.fn();
    await render(GameSearchField, {
      props: { query: "", enabled: true, onSearch: search, onSelect: select },
    });
    const input = page.getByRole("searchbox");
    await input.fill(" Hollow Knight ");
    await expect.element(page.getByRole("button", { name: "Hollow Knight" })).toBeVisible();
    await userEvent.keyboard("{ArrowUp}{Escape}");
    await expect.element(input).toHaveFocus();
    await expect
      .element(page.getByRole("list", { name: "Spielvorschläge" }))
      .not.toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(search).toHaveBeenCalledExactlyOnceWith("Hollow Knight");
    expect(select).not.toHaveBeenCalled();
  });
});
