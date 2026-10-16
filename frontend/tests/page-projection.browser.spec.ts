import { test, expect, type Page } from "@playwright/test";
const pageId = "019a1234-5678-7000-8000-000000000002";
async function setup(page: Page) {
  await page.addInitScript(() => {
    const w = window as any, id = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const makePage = (n: number) => ({ id: id(n), workspaceId: id(1), parentPageId: null, title: `Projection page ${n}`, revision: "1", createdAt: "2026-10-16T00:00:00Z", updatedAt: "2026-10-16T00:00:00Z", deletedAt: null });
    const state = w.projectionState = { workspaceId: id(1), scopeGrantId: id(9), snapshotCursor: "1", contentMode: "shell", pages: [makePage(2), makePage(3)],
      blocks: [2, 3].map(n => ({ id: id(n + 10), pageId: id(n), parentBlockId: null, sortKey: "000000001000", type: "paragraph", schemaVersion: 1, revision: "1", data: { richText: [{ text: `Saved paragraph ${n}.` }] } })),
      collections: [], properties: [], records: [], views: [], files: [], meetings: [], taskFacets: [], memories: [], containers: [], containerCollections: [], transcriptSegments: [], projectScopes: [] };
    const controls = w.projectionControls = { holdReads: false, holdTitle: false, readFailure: "", releaseTitle: null as null | (() => void), held: [] as (() => void)[], batches: [] as any[] };
    const clone = (x: any) => JSON.parse(JSON.stringify(x)), listeners: (() => void)[] = [];
    w.projectionChanged = () => listeners.forEach(fn => fn()); w.troveHost = true;
    w.runtime = { EventsOn: (name: string, fn: () => void) => { if (name === "workspace:changed") listeners.push(fn); return () => {}; } };
    w.go = { main: { App: { ShellReady: async () => {}, SetEditing: async () => {}, Dispatch: async (method: string, raw: string) => {
      const req = JSON.parse(raw), ok = (result: unknown) => JSON.stringify({ ok: true, result });
      if (method === "workspace.snapshot") return ok({ ...clone(state), blocks: [] });
      if (method === "readPage") {
        const failure = controls.readFailure;
        const result = clone({ page: state.pages.find((p: any) => p.id === req.pageId), blocks: state.blocks.filter((b: any) => b.pageId === req.pageId) });
        if (controls.holdReads) await new Promise<void>(resolve => controls.held.push(resolve));
        if (failure) return JSON.stringify({ ok: false, error: { code: failure, message: "Page read unavailable", retryable: false } });
        return ok(result);
      }
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
      if (method !== "batch") throw Error(`Unexpected fixture method ${method}`);
      controls.batches.push(clone(req));
      for (const guard of req.mutation.expectedRevisions) {
        const entity = [...state.pages, ...state.blocks].find((x: any) => x.id === guard.target.entityId);
        if (entity && entity.revision !== guard.revision) return JSON.stringify({ ok: false, error: { code: "CONFLICT", message: "Entity revision changed", retryable: false } });
      }
      const touched = new Set<any>(), target = state.pages[0];
      for (const command of req.commands) {
        if (command.kind === "page.update") target.title = command.title;
        else if (command.kind === "block.update") { const b = state.blocks.find((b: any) => b.id === command.blockId); b.data = command.data; b.type = command.type; touched.add(b); }
        else throw Error(`Unexpected fixture command ${command.kind}`);
      }
      touched.add(target);
      const revisions = [...touched].map((entity: any) => { const before = entity.revision; entity.revision = String(+before + 1); return { target: { entityType: entity.pageId ? "block" : "page", entityId: entity.id }, before, after: entity.revision, deleted: false }; });
      state.snapshotCursor = String(+state.snapshotCursor + 1);
      if (controls.holdTitle && req.commands.some((c: any) => c.kind === "page.update")) await new Promise<void>(resolve => controls.releaseTitle = resolve);
      return ok({ requestId: req.requestId, transactionId: id(99), eventCursor: state.snapshotCursor, revisions, replayed: false });
    } } } };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4319");
  await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: "▤ Projection page 2", exact: true }).click();
  await expect(page.locator(".tiptap .block-text")).toHaveText("Saved paragraph 2.");
  await page.evaluate(() => (window as any).originalEditor = document.querySelector(".tiptap"));
}
async function typeDraft(page: Page) {
  await page.locator(".tiptap .block-text").click();
  await page.keyboard.press("Meta+ArrowLeft"); await page.keyboard.press("Meta+Shift+ArrowRight");
  await page.keyboard.insertText("Local draft must survive.");
  await expect(page.locator(".tiptap .block-text")).toHaveText("Local draft must survive.");
}
async function releaseReads(page: Page) {
  await page.evaluate(() => { const c = (window as any).projectionControls; c.holdReads = false; c.held.splice(0).forEach((fn: () => void) => fn()); });
}
test("title save preserves the mounted draft until its coherent page projection arrives", async ({ page }) => {
  await setup(page);
  await page.evaluate(() => Object.assign((window as any).projectionControls, { holdReads: true, holdTitle: true }));
  await page.getByLabel("Page title", { exact: true }).fill("Renamed projection page");
  await typeDraft(page);
  await expect.poll(() => page.evaluate(() => Boolean((window as any).projectionControls.releaseTitle))).toBe(true);
  await page.evaluate(() => (window as any).projectionControls.releaseTitle());
  await expect.poll(() => page.evaluate(() => (window as any).projectionControls.held.length)).toBe(1);
  await page.waitForTimeout(1100); // Cross the800ms autosave while readPage remains held.
  expect(await page.evaluate(() => (window as any).originalEditor.isConnected)).toBe(true);
  await expect(page.locator(".tiptap .block-text")).toHaveText("Local draft must survive.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  expect(await page.evaluate(() => (window as any).projectionControls.batches.length)).toBe(1);
  await releaseReads(page);
  await expect.poll(() => page.evaluate(() => (window as any).projectionState.blocks[0].data.richText[0].text)).toBe("Local draft must survive.");
  await expect(page.locator(".save-state")).toContainText("Saved");
  await expect(page.getByRole("alert")).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).projectionState.pages[0].title)).toBe("Renamed projection page");
});
test("remote block changes keep a dirty draft and explicit conflict review", async ({ page }) => {
  await setup(page); await typeDraft(page);
  await page.evaluate(() => { const w = window as any, s = w.projectionState; w.projectionControls.holdReads = true; s.pages[0].revision = "2"; s.blocks[0].revision = "2"; s.blocks[0].data.richText = [{ text: "Remote committed content." }]; s.snapshotCursor = "2"; w.projectionChanged(); });
  await expect.poll(() => page.evaluate(() => (window as any).projectionControls.held.length)).toBe(1);
  expect(await page.evaluate(() => (window as any).originalEditor.isConnected)).toBe(true);
  await releaseReads(page);
  await expect(page.locator(".conflict")).toContainText("Your draft is still here");
  await expect(page.locator(".tiptap .block-text")).toHaveText("Local draft must survive.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  expect(await page.evaluate(() => (window as any).projectionControls.batches)).toHaveLength(0);
  expect(await page.evaluate(() => (window as any).projectionState.blocks[0].data.richText[0].text)).toBe("Remote committed content.");
});
for (const change of ["grant", "workspace", "removed"] as const) test(`cached content is discarded on ${change} change while an old read is delayed`, async ({ page }) => {
  await setup(page);
  await page.evaluate(() => { const w = window as any; w.projectionControls.holdReads = true; w.projectionState.pages[0].revision = "2"; w.projectionState.snapshotCursor = "2"; w.projectionChanged(); });
  await expect.poll(() => page.evaluate(() => (window as any).projectionControls.held.length)).toBe(1);
  await page.evaluate(change => {
    const w = window as any, s = w.projectionState;
    if (change === "grant") s.scopeGrantId = "019a1234-5678-7000-8000-000000000080";
    if (change === "workspace") { s.workspaceId = "019a1234-5678-7000-8000-000000000081"; s.pages.forEach((p: any) => p.workspaceId = s.workspaceId); }
    if (change === "removed") s.pages = s.pages.slice(1);
    s.blocks[0].data.richText = [{ text: "New authorized content." }]; s.snapshotCursor = "3"; w.projectionChanged();
  }, change);
  await expect.poll(() => page.evaluate(() => (window as any).originalEditor.isConnected)).toBe(false);
  await expect(page.getByText("Saved paragraph 2.", { exact: true })).toHaveCount(0);
  await releaseReads(page);
  if (change === "grant") await expect(page.locator(".tiptap .block-text")).toHaveText("New authorized content.");
  if (change === "removed") await expect(page.getByLabel("Page title", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Saved paragraph 2.", { exact: true })).toHaveCount(0);
});

test("switching pages ignores a delayed old projection", async ({ page }) => {
  await setup(page);
  await page.evaluate(() => { const w = window as any; w.projectionControls.holdReads = true; w.projectionState.pages[0].revision = "2"; w.projectionState.snapshotCursor = "2"; w.projectionChanged(); });
  await expect.poll(() => page.evaluate(() => (window as any).projectionControls.held.length)).toBe(1);
  await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: "▤ Projection page 3", exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).originalEditor.isConnected)).toBe(false);
  await releaseReads(page);
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue("Projection page 3");
  await expect(page.locator(".tiptap .block-text")).toHaveText("Saved paragraph 3.");
  await expect(page.getByText("Saved paragraph 2.", { exact: true })).toHaveCount(0);
});
test("denied page read discards cached content even before shell membership changes", async ({ page }) => {
  await setup(page);
  await page.evaluate(() => { const w = window as any; w.projectionControls.readFailure = "SCOPE_DENIED"; w.projectionState.pages[0].revision = "2"; w.projectionState.snapshotCursor = "2"; w.projectionChanged(); });
  await expect(page.getByRole("alert").filter({ hasText: "Page read unavailable" })).toBeVisible();
  await expect(page.locator(".tiptap")).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => (window as any).originalEditor.isConnected)).toBe(false);
});
test("transient page read failure keeps draft and requires explicit read retry", async ({ page }) => {
  await setup(page); await typeDraft(page);
  await page.evaluate(() => { const w = window as any; w.projectionControls.readFailure = "RESOURCE_EXHAUSTED"; w.projectionState.pages[0].revision = "2"; w.projectionState.snapshotCursor = "2"; w.projectionChanged(); });
  await expect(page.getByRole("alert").filter({ hasText: "Page read unavailable" })).toBeVisible();
  await expect(page.locator(".tiptap .block-text")).toHaveText("Local draft must survive.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  expect(await page.evaluate(() => (window as any).projectionControls.batches)).toHaveLength(0);
  await page.evaluate(() => (window as any).projectionControls.readFailure = "");
  await page.getByRole("button", { name: "Retry loading page", exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).projectionState.blocks[0].data.richText[0].text)).toBe("Local draft must survive.");
  await expect(page.locator(".save-state")).toContainText("Saved");
});
