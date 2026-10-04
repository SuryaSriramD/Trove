import { test, expect } from "@playwright/test";

test("knowledge graph exposes evidence, OCR availability, and explicitly reviewed digests", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const state: any = {
      workspaceId: id(1),
      scopeGrantId: id(3),
      snapshotCursor: "1",
      pages: [2, 5].map((n) => ({
        id: id(n),
        workspaceId: id(1),
        title: n === 2 ? "Review inbox" : "Orion evidence",
        parentPageId: null,
        revision: "1",
        createdAt: "2026-10-01T00:00:00Z",
        updatedAt: "2026-10-01T00:00:00Z",
        deletedAt: null,
      })),
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
      projectScopes: [],
      importMappings: [],
    };
    let accepted = false;
    (window as any).p2Calls = [];
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const req = JSON.parse(raw),
              ok = (result: any) => JSON.stringify({ ok: true, result });
            (window as any).p2Calls.push({ method, req });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "ocr.status")
              return ok({
                available: false,
                engine: "Apple Vision (local)",
                maxBytes: 20 << 20,
                maxPixels: 20000000,
                timeoutSeconds: 60,
                minimumConfidence: 0.5,
              });
            if (method === "review.list")
              return ok({
                plans: [],
                engine: "Deterministic local review",
                scheduling: "While the owner is running",
                runs: [
                  {
                    id: id(8),
                    pageId: id(2),
                    planBlockId: id(7),
                    status: accepted ? "accepted" : "pending",
                    text: "Review Orion evidence before completing the task. No action has been executed.",
                    sources: [{ pageId: id(5), sourceRevision: "1" }],
                    createdAt: "2026-10-01T09:00:00Z",
                  },
                ],
              });
            if (method === "graph.query")
              return ok({
                nodes: state.pages.map((p: any) => ({
                  id: p.id,
                  title: p.title,
                  revision: p.revision,
                })),
                edges: [
                  {
                    source: id(2),
                    target: id(5),
                    kind: "explicit",
                    location: {
                      pageId: id(2),
                      blockId: id(9),
                      sourceRevision: "1",
                    },
                    evidence: "Canonical link in source block",
                  },
                ],
                truncated: false,
              });
            if (method === "allocateIds") return ok({ ids: [id(20)] });
            if (method === "batch") {
              const c = req.commands[0];
              if (
                c.kind !== "review.accept" ||
                c.approved !== true ||
                c.runId !== id(8)
              )
                throw Error("Unexpected or unapproved review effect");
              const guards = req.mutation.expectedRevisions;
              for (const [entityType, entityId, revision] of [
                ["page", id(2), "1"],
                ["page", id(5), "1"],
                ["block", id(20), "0"],
              ]) {
                if (
                  !guards.some(
                    (g: any) =>
                      g.target.entityType === entityType &&
                      g.target.entityId === entityId &&
                      g.revision === revision,
                  )
                )
                  throw Error("Missing source or destination revision guard");
              }
              accepted = true;
              state.pages[0].revision = "2";
              state.snapshotCursor = "2";
              return ok({ revisions: [], eventCursor: "2", replayed: false });
            }
            if (method === "editor.lock")
              return ok({ locked: false, revision: "0" });
            if (method === "editor.templates") return ok({ templates: [] });
            if (method === "editor.references") return ok({ references: [] });
            if (method === "pageDisplay.get")
              return ok({
                pageId: req.pageId,
                fullWidth: false,
                presentation: false,
                revision: "1",
              });
            if (method === "ai.runs" || method === "imports.list")
              return ok([]);
            if (method === "memory.list") return ok({ entries: [] });
            throw Error("Unexpected method " + method);
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  await page
    .getByRole("button", { name: /Intelligence/, exact: false })
    .click();
  const panel = page.getByRole("region", { name: "P2 intelligence packages" });
  await panel.getByRole("button", { name: "Explore connections" }).click();
  await expect(
    panel.getByRole("img", { name: "Page relationships" }),
  ).toBeVisible();
  await panel.getByText("explicit · Review inbox → Orion evidence").click();
  await expect(panel.getByText("Canonical link in source block")).toBeVisible();
  await expect(panel.getByText(/Source revision 1/)).toBeVisible();
  await panel.getByRole("button", { name: "Image text", exact: true }).click();
  await expect(
    panel.getByText(/Local OCR helper is unavailable in this bundle/),
  ).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "Extract image text locally" }),
  ).toBeDisabled();
  await panel
    .getByRole("button", { name: "Personal review", exact: true })
    .click();
  await expect(
    panel.getByText(
      "Review Orion evidence before completing the task. No action has been executed.",
    ),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        (window as any).p2Calls.filter((c: any) => c.method === "batch").length,
    ),
  ).toBe(0);
  await panel
    .getByRole("button", { name: "Approve and append this digest" })
    .click();
  await expect(panel.getByRole("heading", { name: /^accepted/ })).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "Approve and append this digest" }),
  ).toHaveCount(0);
  expect(
    await page.evaluate(
      () =>
        (window as any).p2Calls.filter((c: any) => c.method === "batch").length,
    ),
  ).toBe(1);
});
