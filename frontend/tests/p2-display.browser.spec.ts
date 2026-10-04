import { test, expect } from "@playwright/test";

test("page display settings persist per page and presentation layout exits after restart", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const state = JSON.parse(
      localStorage.getItem("p2-display-state") ?? "null",
    ) ?? {
      workspaceId: id(1),
      scopeGrantId: id(3),
      snapshotCursor: "1",
      pages: [
        {
          id: id(2),
          workspaceId: id(1),
          title: "Presentation fixture",
          parentPageId: null,
          revision: "1",
          createdAt: "2026-09-30T00:00:00Z",
          updatedAt: "2026-09-30T00:00:00Z",
          deletedAt: null,
        },
      ],
      blocks: [
        {
          id: id(4),
          pageId: id(2),
          type: "paragraph",
          schemaVersion: 1,
          parentBlockId: null,
          sortKey: "000001",
          revision: "1",
          data: { richText: [{ text: "Readable presentation content" }] },
        },
      ],
      collections: [],
      properties: [],
      records: [],
      views: [],
      files: [],
      meetings: [],
      taskFacets: [],
      memories: [],
      containers: [],
      containerCollections: [],
      transcriptSegments: [],
      display: { fullWidth: false, presentation: false },
    };
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const req = JSON.parse(raw),
              ok = (result: any) => JSON.stringify({ ok: true, result });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "pageDisplay.get")
              return ok({
                pageId: req.pageId,
                ...state.display,
                revision: state.pages[0].revision,
              });
            if (method === "editor.lock")
              return ok({ locked: false, revision: "0" });
            if (method === "editor.templates") return ok({ templates: [] });
            if (method === "editor.references") return ok({ references: [] });
            if (method === "ai.runs") return ok([]);
            if (method === "memory.list") return ok({ entries: [] });
            if (method === "imports.list") return ok([]);
            if (method === "batch") {
              const c = req.commands[0],
                guard = req.mutation.expectedRevisions.find(
                  (g: any) => g.target.entityId === state.pages[0].id,
                );
              if (
                c.kind !== "page.display.set" ||
                guard.revision !== state.pages[0].revision
              )
                throw Error("Unfenced display mutation");
              state.display = {
                fullWidth: c.fullWidth,
                presentation: c.presentation,
              };
              state.pages[0].revision = String(
                Number(state.pages[0].revision) + 1,
              );
              localStorage.setItem("p2-display-state", JSON.stringify(state));
              return ok({ revisions: [], eventCursor: "2", replayed: false });
            }
            throw Error("Unexpected method " + method);
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  const open = () =>
    page
      .getByRole("navigation", { name: "Page tree" })
      .getByRole("button", { name: "▤ Presentation fixture", exact: true })
      .click();
  await open();
  await page.getByRole("button", { name: "Page options", exact: true }).click();
  await page.getByRole("checkbox", { name: "Full width", exact: true }).check();
  await expect(page.locator(".page-surface")).toHaveClass(/full-width/);
  await page
    .getByRole("checkbox", { name: "Presentation layout", exact: true })
    .check();
  await expect(page.locator(".sidebar")).toBeHidden();
  await expect(
    page.getByRole("button", { name: "Exit presentation layout" }),
  ).toBeVisible();
  await page.reload();
  await open();
  await expect(page.locator(".page-surface")).toHaveClass(/full-width/);
  await expect(page.locator(".sidebar")).toBeHidden();
  await expect(
    page
      .getByRole("textbox", { name: "Page body" })
      .getByText("Readable presentation content", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Exit presentation layout" }).click();
  await expect(page.locator(".sidebar")).toBeVisible();
  await expect(page.locator(".page-surface")).toHaveClass(/full-width/);
  await page.getByRole("button", { name: "Page options", exact: true }).click();
  await expect(
    page.getByRole("checkbox", { name: "Presentation layout", exact: true }),
  ).not.toBeChecked();
  await page
    .getByRole("checkbox", { name: "Full width", exact: true })
    .uncheck();
  await expect(page.locator(".page-surface")).not.toHaveClass(/full-width/);
});
