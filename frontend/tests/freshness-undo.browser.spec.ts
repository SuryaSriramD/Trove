import { test, expect } from "@playwright/test";

test("human review queue, attribution and guarded grouped undo are usable", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const state = JSON.parse(
      localStorage.getItem("review-fixture") ?? "null",
    ) ?? {
      workspaceId: id(1),
      scopeGrantId: id(3),
      snapshotCursor: "1",
      pages: [
        {
          id: id(2),
          workspaceId: id(1),
          title: "Review fixture",
          parentPageId: null,
          revision: "1",
          createdAt: "2026-10-15T00:00:00Z",
          updatedAt: "2026-10-15T00:00:00Z",
          deletedAt: null,
        },
      ],
      blocks: [],
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
      review: null,
      undone: false,
      mutations: 0,
    };
    const identity = { version: 1, digest: "a".repeat(64) };
    const freshness = () => ({
      state: state.review ? "reviewed" : "never_reviewed",
      contentIdentity: identity,
      ...(state.review ? { review: state.review } : {}),
    });
    const persist = () =>
      localStorage.setItem("review-fixture", JSON.stringify(state));
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const r = JSON.parse(raw),
              ok = (result: any) => JSON.stringify({ ok: true, result });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "readPage")
              return ok({
                page: state.pages[0],
                blocks: [],
                snapshotCursor: state.snapshotCursor,
              });
            if (method === "pageDisplay.get")
              return ok({
                pageId: id(2),
                fullWidth: false,
                presentation: false,
                revision: state.pages[0].revision,
              });
            if (method === "page.freshness")
              return ok({
                pageId: id(2),
                freshness: freshness(),
                history: state.review ? [state.review] : [],
              });
            if (method === "page.reviewQueue")
              return ok({
                items: state.review
                  ? []
                  : [
                      {
                        pageId: id(2),
                        title: state.pages[0].title,
                        freshness: freshness(),
                      },
                    ],
              });
            if (method === "operations.list")
              return ok({
                operations: [
                  {
                    id: id(9),
                    label: "Rename page",
                    createdAt: "2026-10-15T00:00:00Z",
                    state: state.undone ? "undone" : "applied",
                  },
                ],
              });
            if (method === "operations.previewUndo")
              return ok({
                operationId: id(9),
                label: "Rename page",
                actions: [
                  {
                    kind: "page.update",
                    pageId: id(2),
                    title: "Earlier title",
                  },
                ],
                expectedRevisions: [
                  {
                    target: { entityType: "page", entityId: id(2) },
                    revision: state.pages[0].revision,
                  },
                ],
                command: { kind: "operation.undo", operationId: id(9) },
              });
            if (method === "batch") {
              const guard = r.mutation.expectedRevisions.find(
                (g: any) => g.target.entityId === id(2),
              );
              if (guard?.revision !== state.pages[0].revision)
                throw Error("Missing current page guard");
              const c = r.commands[0];
              if (c.kind === "page.review") {
                if (c.contentIdentity.digest !== identity.digest)
                  throw Error("Missing reviewed content identity");
                state.review = {
                  id: id(8),
                  pageId: id(2),
                  actorId: id(3),
                  reviewedAt: "2026-10-15T12:00:00Z",
                  contentIdentity: identity,
                  ...(c.expiresAt ? { expiresAt: c.expiresAt } : {}),
                };
              } else if (c.kind === "operation.undo") {
                if (c.operationId !== id(9)) throw Error("Wrong operation");
                state.undone = true;
                state.pages[0].title = "Earlier title";
              } else throw Error("Unexpected mutation");
              state.mutations++;
              state.pages[0].revision = String(
                Number(state.pages[0].revision) + 1,
              );
              state.snapshotCursor = state.pages[0].revision;
              persist();
              return ok({
                revisions: [],
                eventCursor: state.snapshotCursor,
                replayed: false,
              });
            }
            if (method === "editor.lock")
              return ok({ locked: false, revision: "0" });
            if (method === "editor.templates") return ok({ templates: [] });
            if (method === "editor.references") return ok({ references: [] });
            if (method === "memory.list") return ok({ entries: [] });
            if (method === "links.backlinks") return ok({ links: [] });
            return ok([]);
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191");
  await page
    .getByRole("region", { name: "Knowledge review queue" })
    .getByRole("button", { name: "Review fixture" })
    .click();
  await page.locator(".page-review summary").first().click();
  await page
    .getByLabel("Next review (optional, local time)")
    .fill("2027-01-01T09:00");
  await page
    .getByRole("button", { name: "I reviewed the current content" })
    .click();
  await expect(page.locator(".page-review summary").first()).toHaveText(
    "Human reviewed",
  );
  await page.reload();
  await page
    .getByRole("navigation", { name: "Page tree" })
    .getByRole("button", { name: "▤ Review fixture", exact: true })
    .click();
  await expect(page.locator(".page-review summary").first()).toHaveText(
    "Human reviewed",
  );
  await page.getByText("Grouped changes and undo", { exact: true }).click();
  await page.getByRole("button", { name: "Preview undo", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Review grouped undo" }),
  ).toContainText("restore title to “Earlier title”");
  await page
    .getByRole("region", { name: "Review grouped undo" })
    .getByRole("button", { name: "Cancel", exact: true })
    .click();
  expect(
    await page.evaluate(
      () => JSON.parse(localStorage.getItem("review-fixture")!).mutations,
    ),
  ).toBe(1);
  await page.getByRole("button", { name: "Preview undo", exact: true }).click();
  await page
    .getByRole("button", { name: "Apply reviewed undo", exact: true })
    .click();
  await expect(
    page.getByRole("textbox", { name: "Page title", exact: true }),
  ).toHaveValue("Earlier title");
});
