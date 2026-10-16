import { test, expect, type Page } from "@playwright/test";

async function setup(page: Page) {
  await page.addInitScript(() => {
    const w = window as any;
    const id = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const pages = [2, 3].map(n => ({ id: id(n), workspaceId: id(1), parentPageId: null, title: `Admission page ${n}`, revision: "1", createdAt: "2026-10-16T00:00:00Z", updatedAt: "2026-10-16T00:00:00Z", deletedAt: null }));
    const blocks = pages.map((p, i) => ({ id: id(10 + i), pageId: p.id, parentBlockId: null, sortKey: "0001", type: "paragraph", schemaVersion: 1, data: { richText: [{ text: `Saved content ${i + 2}` }] }, revision: "1" }));
    const snapshot = { workspaceId: id(1), scopeGrantId: id(9), snapshotCursor: "1", contentMode: "shell", pages, blocks: [], collections: [], properties: [], records: [], views: [], files: [], meetings: [], taskFacets: [], memories: [], containers: [], containerCollections: [], transcriptSegments: [], projectScopes: [] };
    w.troveHost = true;
    // One occupied transport slot represents the bridge's independent poll.
    const state = w.admissionEvidence = { active: 1, peak: 1, rejected: 0, calls: [] as any[], held: [] as (() => void)[] };
    w.runtime = { EventsOn: () => () => {} };
    w.go = { main: { App: { ShellReady: async () => {}, SetEditing: async () => {}, Dispatch: async (method: string, raw: string) => {
      const req = JSON.parse(raw), ok = (result: unknown) => JSON.stringify({ ok: true, result });
      state.calls.push({ method, pageId: req.pageId });
      if (state.active >= 4) { state.rejected++; throw Error("Host busy"); }
      state.active++; state.peak = Math.max(state.peak, state.active);
      try {
        if (req.pageId?.startsWith("hold-")) await new Promise<void>(resolve => state.held.push(resolve));
        else await new Promise(resolve => setTimeout(resolve, 50));
        if (method === "workspace.snapshot") return ok(snapshot);
        if (method === "readPage") return ok({ page: pages.find(p => p.id === req.pageId), blocks: blocks.filter(b => b.pageId === req.pageId) });
        if (method === "pageDisplay.get") return ok({ pageId: req.pageId, fullWidth: false, presentation: false, revision: "1" });
        if (method === "page.freshness") return ok({ pageId: req.pageId, freshness: { state: "never_reviewed", contentIdentity: { version: 2, digest: "fixture" }, review: null }, history: [] });
        if (method === "page.reviewQueue") return ok({ items: [] });
        if (method === "operations.list") return ok({ operations: [] });
        if (method === "links.backlinks") return ok({ links: [] });
        if (method === "editor.lock") return ok({ locked: false, revision: "0" });
        if (method === "editor.templates") return ok({ templates: [] });
        if (method === "editor.references") return ok({ references: [] });
        if (method === "models.list") return ok({ packs: [] });
        if (method === "ai.runs") return ok([]);
        if (method === "memory.list") return ok({ entries: [] });
        throw Error(`Unexpected fixture method ${method}`);
      } finally { state.active--; }
    } } } };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  await expect(page.getByRole("navigation", { name: "Workspace", exact: true })).toBeVisible();
  const tree = page.getByRole("navigation", { name: "Page tree", exact: true });
  await expect(tree.getByRole("button", { name: "▤ Admission page 2", exact: true })).toBeVisible();
  await expect(tree.getByRole("button", { name: "▤ Admission page 3", exact: true })).toBeVisible();
}

test("hosted initial page fan-out renders saved content beneath the unchanged four-call gate", async ({ page }) => {
  await setup(page);
  await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: "▤ Admission page 2", exact: true }).click();
  await expect(page.locator(".tiptap .block-text")).toHaveText("Saved content 2");
  await expect(page.getByText("Host busy", { exact: true })).toHaveCount(0);
  const pageReads = ["readPage", "page.freshness", "pageDisplay.get", "operations.list", "links.backlinks"];
  await expect.poll(() => page.evaluate(methods => methods.every(method =>
    (window as any).admissionEvidence.calls.some((call: any) => call.method === method)), pageReads)).toBe(true);
  const evidence = await page.evaluate(() => (window as any).admissionEvidence);
  expect(evidence.rejected).toBe(0); expect(evidence.peak).toBeLessThanOrEqual(3);
});

test("navigation cancels an obsolete queued page read before sending it", async ({ page }) => {
  await setup(page);
  await page.evaluate(async () => {
    const { dispatch } = await import("/src/api.ts");
    (window as any).heldAdmissions = [dispatch("readPage", { pageId: "hold-a" }), dispatch("readPage", { pageId: "hold-b" })];
  });
  await expect.poll(() => page.evaluate(() => (window as any).admissionEvidence.held.length)).toBe(2);
  const tree = page.getByRole("navigation", { name: "Page tree", exact: true });
  await tree.getByRole("button", { name: "▤ Admission page 2", exact: true }).click();
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue("Admission page 2");
  await tree.getByRole("button", { name: "▤ Admission page 3", exact: true }).click();
  await page.evaluate(() => (window as any).admissionEvidence.held.splice(0).forEach((release: () => void) => release()));
  await expect(page.locator(".tiptap .block-text")).toHaveText("Saved content 3");
  const evidence = await page.evaluate(() => (window as any).admissionEvidence);
  expect(evidence.rejected).toBe(0);
  expect(evidence.calls.filter((call: any) => call.method === "readPage" && call.pageId === "019a1234-5678-7000-8000-000000000002")).toHaveLength(0);
  expect(evidence.calls.filter((call: any) => call.method === "readPage" && call.pageId === "019a1234-5678-7000-8000-000000000003")).toHaveLength(1);
});
