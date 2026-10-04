import { test, expect } from "@playwright/test";

function pcmWAV(seconds: number): Buffer {
  const samples = 16000 * seconds;
  const wav = Buffer.alloc(44 + samples * 2);
  wav.write("RIFF", 0); wav.writeUInt32LE(wav.length - 8, 4); wav.write("WAVEfmt ", 8);
  wav.writeUInt32LE(16, 16); wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(16000, 24); wav.writeUInt32LE(32000, 28); wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34);
  wav.write("data", 36); wav.writeUInt32LE(samples * 2, 40);
  for (let i = 0; i < samples; i++) wav.writeInt16LE(Math.round(Math.sin(i * 2 * Math.PI * 440 / 16000) * 2048), 44 + i * 2);
  return wav;
}

async function installMeetingAudioFixture(page: any) {
  await page.evaluate(() => {
    const state = (window as any).fixtureState();
    const makeID = (n: number) => `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const first = { ...state.pages[0], title: "First audio recording" };
    const second = { ...first, id: makeID(501), title: "Second audio recording" };
    state.pages = [first, second];
    state.meetings = [first, second].map((page, i) => ({ id: makeID(510 + i), pageId: page.id, recordingFileId: makeID(520 + i), startedAt: "2026-09-30T10:00:00Z", durationMs: i ? 2000 : 4000, state: "captured", revision: "1" }));
    state.snapshotCursor = "2";
    const app = (window as any).go.main.App, previous = app.Dispatch;
    app.MeetingRecordings = async () => [];
    app.Dispatch = async (method: string, raw: string) => method === "meetings.drafts" ? JSON.stringify({ ok: true, result: [] }) : previous(method, raw);
    const metrics = (window as any).audioLifecycle = { created: [] as string[], revoked: [] as string[], aborted: [] as string[] };
    const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL), originalFetch = window.fetch.bind(window);
    URL.createObjectURL = blob => { const url = create(blob); metrics.created.push(url); return url; };
    URL.revokeObjectURL = url => { metrics.revoked.push(url); revoke(url); };
    window.fetch = (input, init) => {
      const path = String(input);
      if (path.startsWith("/files/")) init?.signal?.addEventListener("abort", () => metrics.aborted.push(path), { once: true });
      return originalFetch(input, init);
    };
    (window as any).emitFixtureEvent("workspace:changed");
  });
}
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const fixturePage = {
  id: id(2),
  workspaceId: id(1),
  parentPageId: null,
  title: "A garden of ideas",
  revision: "1",
  createdAt: "2026-09-30T10:00:00Z",
  updatedAt: "2026-09-30T10:00:00Z",
  deletedAt: null,
  icon: "🌱",
};
const b = (
  n: number,
  type: string,
  data: any,
  parentBlockId: string | null = null,
) => ({
  id: id(n),
  pageId: fixturePage.id,
  parentBlockId,
  sortKey: String(n * 1000).padStart(12, "0"),
  type,
  schemaVersion: 1,
  data,
  revision: "1",
});
const fixture = {
  contractVersion: "1.0.0",
  workspaceId: id(1),
  scopeGrantId: id(3),
  snapshotCursor: "1",
  pages: [fixturePage],
  blocks: [
    b(10, "heading_1", { richText: [{ text: "A place to begin" }] }),
    b(11, "paragraph", {
      richText: [
        { text: "Capture the small things. Connect the useful ones." },
      ],
    }),
    b(12, "callout", {
      richText: [{ text: "Your work stays on this device." }],
      icon: "💡",
      backgroundToken: "yellow",
    }),
    b(13, "heading_4", { richText: [{ text: "Imported detail" }] }),
    b(14, "table", {
      rowCount: 1,
      columnCount: 2,
      headerRows: 1,
      headerColumns: 0,
      columnWidths: [120, 120],
      cells: [
        {
          id: id(20),
          row: 0,
          column: 0,
          rowSpan: 1,
          colSpan: 2,
          richText: [{ text: "Merged source cell" }],
          preMergeCells: [{ value: "Original A" }],
        },
      ],
    }),
    b(15, "unsupported_external", {
      sourceType: "source-widget",
      sourceVersion: 3,
      raw: { original: "keep" },
      readableText: "A retained source widget",
      reason: "unknown_type",
    }),
  ],
  collections: [],
  properties: [],
  records: [],
  views: [],
  files: [],
  meetings: [],
  taskFacets: [],
  containers: [],
  containerCollections: [],
  transcriptSegments: [],
  projectScopes: [],
  memories: [],
  importMappings: [],
};
const largeWorkspaceTest = "10,000-page shell keeps the full hierarchy and body search usable across refresh";
function largeWorkspaceFixture() {
  const state: any = structuredClone(fixture);
  state.pages = [];
  state.blocks = [];
  for (let i = 0; i < 10000; i++) {
    const pageId = id(10000 + i);
    state.pages.push({
      ...fixturePage, id: pageId, title: `Page ${String(i).padStart(5, "0")}`, icon: null,
      parentPageId: i % 25 ? id(10000 + i - i % 25) : null,
      archivedAt: i === 9997 ? fixturePage.updatedAt : null,
      deletedAt: i === 9998 ? fixturePage.updatedAt : null,
    });
    const text = i === 9999 ? "only-body-target తెలుగు"
      : i === 9996 ? "record-body-target"
      : i === 9997 || i === 9998 ? "hidden-only-token"
      : `Research notes for page ${i}. ` + "A useful detail. ".repeat(20);
    for (let j = 0; j < 2; j++) state.blocks.push({
      ...b(30000 + i * 2 + j, "paragraph", { richText: [{ text }] }), pageId,
    });
  }
  state.records = [{ id: id(60000), pageId: id(19996), values: [], revision: "1" }];
  return state;
}
test.beforeEach(async ({ page }, testInfo) => {
  await page.addInitScript(
    ({ fixture }) => {
      let seq = Number(localStorage.getItem("fixtureSequence") ?? "100");
      const selections = new Map<string, any>();
      const prepared = new Map<string, any>();
      let activeUploads = 0;
      (window as any).attachmentMetrics = {
        maxConcurrentUploads: 0,
        stagedNames: [],
      };
      let state =
        JSON.parse(localStorage.getItem("fixture") ?? "null") ?? fixture;
      const makeID = () => {
        localStorage.setItem("fixtureSequence", String(++seq));
        return `019a1234-5678-7000-8000-${String(seq).padStart(12, "0")}`;
      };
      (window as any).fixtureState = () => state;
      const eventListeners = new Map<string, Set<(...args: unknown[]) => void>>();
      (window as any).runtime = {
        EventsOn(event: string, handler: (...args: unknown[]) => void) {
          const listeners = eventListeners.get(event) ?? new Set();
          listeners.add(handler);
          eventListeners.set(event, listeners);
          return () => listeners.delete(handler);
        },
      };
      (window as any).emitFixtureEvent = (event: string, value?: unknown) => {
        for (const listener of eventListeners.get(event) ?? []) listener(value);
      };
      (window as any).go = {
        main: {
          App: {
            ShellReady: async () => {
              (window as any).fixtureShellReadyMs = performance.now() - (window as any).fixtureSnapshotReadyAt;
            },
            Dispatch: async (method: string, raw: string) => {
              const req = JSON.parse(raw);
              const ok = (result: any) => JSON.stringify({ ok: true, result });
              if (method === "workspace.snapshot") {
                const response = ok(state);
                (window as any).fixtureSnapshotReadyAt = performance.now();
                return response;
              }
              if (method === "pageDisplay.get") return ok({pageId:req.pageId,fullWidth:false,presentation:false,revision:state.pages.find((p:any)=>p.id===req.pageId)?.revision ?? "1"});
              if (method === "editor.lock") return ok((state as any).pageLock ?? {locked:false,revision:"0"});
              if (method === "editor.setLock") { (state as any).pageLock={locked:req.locked,revision:String(Number(req.expectedRevision)+1)};return ok((state as any).pageLock); }
              if (method === "editor.templates") return ok({templates:[]});
              if (method === "editor.references") return ok({references:[]});
              if (method === "ai.runs") return ok([]);
              if (method === "memory.list") return ok({ entries: [] });
              if (method === "search.semanticSettings") return ok({ enabled: false, modelPackId: "", dimension: 0 });
              if (method === "recovery.settings") return ok({ historyPolicy: "Keep all page history.", trashPolicy: "Retain trash until reviewed.", recordingPolicy: "Retain recording audio.", backupPolicy: "Cleanup requires review.", backupKeepCount: 0, retainedRevisions: 12, backups: 1 });
              if (method === "documents.status") return ok({ files: [] });
              if (method === "imports.list") return ok([]);
              if (method === "jobs.list") return ok({ jobs: [] });
              if (method === "search.query") return ok({
                hits: req.query.toLowerCase().includes("capture") ? [{
                  chunkId: "fixture-chunk", title: fixture.pages[0].title, kind: "page",
                  excerpt: "Capture the small things. Connect the useful ones.", score: 1,
                  location: { pageId: fixture.pages[0].id, blockId: fixture.blocks[1].id, sourceRevision: state.pages[0].revision },
                }] : [], partial: false, mode: "keyword",
              });
              if (method === "resolveCitation") return ok({ state: "current", location: req.location, currentRevision: state.pages[0].revision });
              if (method === "allocateIds")
                return ok({ ids: Array.from({ length: req.count }, makeID) });
              if (method === "prepareFile") {
                const selected = selections.get(req.selectionHandle);
                if (!selected) throw Error("Unknown fixture selection handle");
                const fileId = makeID(),
                  ingestHandle = makeID();
                const file = {
                  id: fileId,
                  workspaceId: state.workspaceId,
                  originalName: selected.name,
                  mediaType: selected.mediaType,
                  contentHash: "a".repeat(64),
                  sizeBytes: String(atob(selected.base64).length),
                  storageKey: `fixture/${fileId}`,
                  revision: "1",
                };
                prepared.set(ingestHandle, file);
                return ok({
                  fileId,
                  ingestHandle,
                  contentHash: file.contentHash,
                  sizeBytes: file.sizeBytes,
                  mediaType: file.mediaType,
                  expiresAt: "2099-01-01T00:00:00Z",
                });
              }
              if (method === "models.list")
                return ok({
                  packs: [],
                  catalog: [],
                  runtime: { available: false, error: "Test runtime" },
                });
              if (method !== "batch")
                throw Error("Unexpected method " + method);
              const revisions: any[] = [];
              const blockBefore = new Map(state.blocks.map((b: any) => [b.id, b.revision]));
              const touchedBlocks = new Set<string>();
              const changed = new Set<string>();
              const blockGuards = new Set(req.mutation.expectedRevisions.filter((g: any) => g.target.entityType === "block").map((g: any) => g.target.entityId));
              // The real kernel requires guards for every reordered sibling,
              // even when its text is unchanged. Keep this fixture equally strict.
              for (const c of req.commands) {
                const required = c.kind === "block.reorder" ? c.orderedBlockIds
                  : c.kind === "block.insert" ? [c.block.blockId, c.block.parentBlockId].filter(Boolean)
                  : c.kind.startsWith("block.") ? [c.blockId, c.parentBlockId].filter(Boolean) : [];
                for (const blockId of required) if (!blockGuards.has(blockId)) {
                  return JSON.stringify({ ok: false, error: { code: "INVALID_REQUEST", message: "Missing revision guard for block:" + blockId, retryable: false, requestId: req.requestId } });
                }
              }
              for (const ex of req.mutation.expectedRevisions) {
                const object =
                  state.pages.find((p: any) => p.id === ex.target.entityId) ??
                  state.blocks.find((p: any) => p.id === ex.target.entityId);
                if (object && object.revision !== ex.revision)
                  return JSON.stringify({
                    ok: false,
                    error: {
                      code: "CONFLICT",
                      message: "Revision changed",
                      retryable: false,
                      requestId: req.requestId,
                    },
                  });
              }
              for (const c of req.commands) {
                if (c.kind === "page.create") {
                  state.pages.push({
                    ...fixture.pages[0],
                    id: c.pageId,
                    title: c.title,
                    parentPageId: c.parentPageId ?? null,
                    icon: null,
                    revision: "0",
                  });
                  changed.add(c.pageId);
                }
                if (c.kind === "page.update") {
                  const p = state.pages.find((p: any) => p.id === c.pageId);
                  Object.assign(p, c);
                  delete p.kind;
                  if (c.archived !== undefined)
                    p.archivedAt = c.archived ? "2026-09-30T10:00:00Z" : null;
                  changed.add(p.id);
                }
                if (c.kind === "page.move") {
                  state.pages.find((p: any) => p.id === c.pageId).parentPageId =
                    c.parentPageId;
                  changed.add(c.pageId);
                }
                if (c.kind === "page.trash") {
                  state.pages.find((p: any) => p.id === c.pageId).deletedAt =
                    "2026-09-30T10:00:00Z";
                  changed.add(c.pageId);
                }
                if (c.kind === "file.attach") {
                  const file = prepared.get(c.ingestHandle);
                  if (!file || file.id !== c.fileId)
                    throw Error(
                      "Fixture attachment does not reference prepared bytes",
                    );
                  if (!state.files.some((f: any) => f.id === file.id))
                    state.files.push(file);
                  changed.add(c.pageId);
                }
                if (c.kind === "block.insert") {
                  state.blocks.push({
                    ...c.block,
                    id: c.block.blockId,
                    parentBlockId: c.block.parentBlockId ?? null,
                    sortKey: "999999999999",
                    revision: "1",
                  });
                  changed.add(c.block.pageId);
                  touchedBlocks.add(c.block.blockId);
                }
                if (c.kind === "block.update") {
                  const b = state.blocks.find((b: any) => b.id === c.blockId);
                  Object.assign(b, {
                    type: c.type,
                    data: c.data,
                  });
                  changed.add(b.pageId);
                  touchedBlocks.add(c.blockId);
                }
                if (c.kind === "block.delete") {
                  const b = state.blocks.find((b: any) => b.id === c.blockId);
                  changed.add(b.pageId);
                  state.blocks = state.blocks.filter(
                    (b: any) => b.id !== c.blockId,
                  );
                }
                if (c.kind === "block.move") {
                  const b = state.blocks.find((b: any) => b.id === c.blockId);
                  b.parentBlockId = c.parentBlockId;
                  changed.add(b.pageId);
                  touchedBlocks.add(c.blockId);
                }
                if (c.kind === "block.reorder") {
                  c.orderedBlockIds.forEach(
                    (id: string, i: number) => {
                      state.blocks.find((b: any) => b.id === id).sortKey =
                        String((i + 1) * 1000).padStart(12, "0");
                      touchedBlocks.add(id);
                    },
                  );
                  changed.add(c.pageId);
                }
              }
              for (const block of state.blocks) {
                const before = blockBefore.get(block.id) ?? "0";
                if (touchedBlocks.has(block.id)) block.revision = String(Number(before) + 1);
                if (before !== block.revision) revisions.push({ target: { entityType: "block", entityId: block.id }, before, after: block.revision, deleted: false });
              }
              for (const id of changed) {
                const p = state.pages.find((p: any) => p.id === id);
                const before = p.revision;
                p.revision = String(Number(before) + 1);
                revisions.push({
                  target: { entityType: "page", entityId: id },
                  before,
                  after: p.revision,
                  deleted: false,
                });
              }
              localStorage.setItem("fixture", JSON.stringify(state));
              return ok({
                requestId: req.requestId,
                transactionId: makeID(),
                eventCursor: "2",
                revisions,
                replayed: false,
              });
            },
            PickFile: async () => "",
            StageUpload: async (
              name: string,
              mediaType: string,
              base64: string,
            ) => {
              const metrics = (window as any).attachmentMetrics;
              metrics.maxConcurrentUploads = Math.max(
                metrics.maxConcurrentUploads,
                ++activeUploads,
              );
              // Model an asynchronous IPC boundary so concurrent decodes/writes are detectable.
              await new Promise((resolve) => setTimeout(resolve, 5));
              const handle = makeID();
              selections.set(handle, { name, mediaType, base64 });
              metrics.stagedNames.push(name);
              activeUploads--;
              return handle;
            },
          },
        },
      };
    },
    { fixture: testInfo.title === largeWorkspaceTest ? largeWorkspaceFixture() : fixture },
  );
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
});
const open = async (page: any) =>
  page
    .getByRole("button", { name: "🌱 A garden of ideas", exact: true })
    .first()
    .click();
test(largeWorkspaceTest, async ({ page }) => {
  await expect.poll(() => page.evaluate(() => (window as any).fixtureShellReadyMs)).toBeGreaterThan(0);
  // This is renderer readiness from the snapshot response, not native process startup.
  const shellMs = await page.evaluate(() => (window as any).fixtureShellReadyMs);
  await test.info().attach("large-workspace-renderer-ready", {
    body: JSON.stringify({ pages: 10000, blocks: 20000, shellMs, endpoint: "snapshot response to rendered shell plus two frames" }),
    contentType: "application/json",
  });
  expect(shellMs).toBeLessThan(2000);
  const tree = page.getByRole("navigation", { name: "Page tree" });
  await expect(tree.locator(".tree-item")).toHaveCount(9997);
  await expect(tree.locator(".tree-item").nth(1)).toContainText("Page 00001");
  await expect(tree.getByRole("button", { name: "▤ Page 09999", exact: true })).toHaveCount(1);
  await expect(tree.getByRole("button", { name: "▤ Page 09996", exact: true })).toHaveCount(0);

  await page.keyboard.press("Meta+k");
  const dialog = page.getByRole("dialog", { name: "Quick switcher" });
  const query = dialog.getByRole("textbox", { name: "Find a page" });
  await query.fill("only-body-target తెలుగు");
  await expect(dialog.getByRole("button")).toHaveCount(1);
  await expect(dialog.getByRole("button")).toContainText("Page 09999");
  await query.press("Enter");
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");
  await expect(page.getByRole("textbox", { name: "Page body" })).toContainText("only-body-target తెలుగు");
  await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true })).toBeVisible();
  await page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true }).click();
  await page.getByRole("button", { name: "Go back", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");

  await page.keyboard.press("Meta+k");
  await query.fill("record-body-target");
  await expect(dialog.getByRole("button")).toHaveCount(1);
  await expect(dialog.getByRole("button")).toContainText("Page 09996");
  await query.fill("hidden-only-token");
  await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();

  await page.evaluate(() => {
    const state = (window as any).fixtureState();
    const target = state.pages[9999];
    target.title = "Refreshed research";
    target.parentPageId = state.pages[0].id;
    target.revision = "2";
    for (const block of state.blocks.filter((b: any) => b.pageId === target.id)) {
      block.data = { richText: [{ text: "refreshed-body-only" }] };
      block.revision = "2";
    }
    (window as any).emitFixtureEvent("workspace:changed");
  });
  await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Refreshed research", exact: true })).toBeVisible();
  await query.fill("refreshed-body-only");
  await expect(dialog.getByRole("button")).toHaveCount(1);
  await expect(dialog.getByRole("button")).toContainText("Refreshed research");
  await query.fill("only-body-target");
  await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();
  await query.press("Escape");
  await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true })).toBeVisible();
  await expect(tree.locator(".tree-item")).toHaveCount(9997);
  await page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true }).click();
  await page.keyboard.press("Meta+k");
  await query.fill("refreshed-body-only");
  await query.press("Enter");
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Refreshed research");
});
test("shell navigation, focus, quick switcher, page metadata", async ({
  page,
}) => {
  await expect(
    page.getByRole("heading", { name: "Make room for your ideas." }),
  ).toBeVisible();
  await page.screenshot({ path: `${process.env.TROVE_BROWSER_SCREENSHOTS ?? "../evidence"}/phase1-home.png` });
  await open(page);
  await page
    .getByRole("textbox", { name: "Page title" })
    .fill("A calmer workspace");
  await page.getByRole("textbox", { name: "Page title" }).press("Tab");
  await expect
    .poll(() =>
      page.evaluate(() => (window as any).fixtureState().pages[0].title),
    )
    .toBe("A calmer workspace");
  await page
    .getByRole("button", { name: "Favorite page", exact: true })
    .click();
  await page.keyboard.press("Meta+k");
  await expect(
    page.getByRole("dialog", { name: "Quick switcher" }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "Find a page" }).fill("calmer");
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("dialog", { name: "Quick switcher" }),
  ).not.toBeVisible();
  await expect(page.getByRole("textbox", { name: "Page body" })).toBeVisible();
});
test("Unicode composition, RTL, emoji, undo/redo, reopen, raw preservation", async ({
  page,
}) => {
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  await editor.locator(".block-paragraph .block-text").click();
  await page.keyboard.press("End");
  await page.keyboard.insertText(" नमस्ते తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  await editor.dispatchEvent("compositionstart", { data: "" });
  await editor.dispatchEvent("compositionupdate", { data: "漢字" });
  await editor.dispatchEvent("compositionend", { data: "漢字" });
  await page.keyboard.insertText(" 漢字");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Saved");
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as any)
          .fixtureState()
          .blocks.find((b: any) => b.type === "paragraph")
          .data.richText.map((r: any) => r.text)
          .join(""),
      ),
    )
    .toContain("తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  await page.reload();
  await open(page);
  await expect(editor).toContainText("తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  await expect(editor.getByRole("heading", { level: 4 })).toHaveText(
    "Imported detail",
  );
  await expect(editor.locator('th[scope="col"][colspan="2"]')).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as any)
            .fixtureState()
            .blocks.find((b: any) => b.type === "unsupported_external").data
            .raw,
      ),
    )
    .toEqual({ original: "keep" });
  await page.screenshot({ path: `${process.env.TROVE_BROWSER_SCREENSHOTS ?? "../evidence"}/phase1-editor.png` });
});
test("mixed clipboard retains block structure and allocates distinct IDs", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  await editor.locator(".block-paragraph .block-text").click();
  await page.keyboard.press("Meta+a");
  await page.keyboard.press("Meta+c");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("End");
  await page.keyboard.press("Meta+v");
  await expect(editor.locator(".block-heading_4")).toHaveCount(2);
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Saved");
  const blocks = await page.evaluate(
    () => (window as any).fixtureState().blocks,
  );
  expect(new Set(blocks.map((b: any) => b.id)).size).toBe(blocks.length);
  expect(blocks.filter((b: any) => b.type === "heading_4").length).toBe(2);
  expect(
    blocks
      .filter((b: any) => b.type === "unsupported_external")
      .every((b: any) => b.data.raw.original === "keep"),
  ).toBe(true);
});

test("repeated splits and formatted paste preserve imported neighbors with complete revision guards", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.evaluate(() => {
    const state = (window as any).fixtureState();
    const make = (number: number, type: string, richText: any[]) => ({
      id: `019a1234-5678-7000-8000-${String(number).padStart(12, "0")}`,
      pageId: state.pages[0].id, parentBlockId: null,
      sortKey: String(number * 1000).padStart(12, "0"), type,
      schemaVersion: 1, revision: "1", data: { richText },
    });
    state.blocks = [
      make(10, "heading_1", [{ text: "Imported Notion notebook" }]),
      make(11, "paragraph", [{ text: "Original introduction" }]),
      make(12, "paragraph", [{ text: "Launch date 2026-09-30" }]),
      make(13, "paragraph", [{ text: "Milestones", link: "trove://page/019a1234-5678-7000-8000-000000000002" }]),
    ];
    localStorage.setItem("fixture", JSON.stringify(state));
    const app = (window as any).go.main.App, previous = app.Dispatch;
    (window as any).structuralBatches = [];
    app.Dispatch = async (method: string, raw: string) => {
      if (method === "batch") (window as any).structuralBatches.push(JSON.parse(raw));
      return previous(method, raw);
    };
    (window as any).emitFixtureEvent("workspace:changed");
  });
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  await expect(editor).toContainText("Imported Notion notebook");
  for (const [blockID, offset, label] of [[id(13), 1, "First"], [id(12), 11, "Second"]] as const) {
    // Place the caret within the imported rich link/date text, then exercise
    // actual browser split and clipboard transactions rather than JSON edits.
    await editor.locator(`[data-block-id="${blockID}"] .block-text`).evaluate((element, offset) => {
      const text = document.createTreeWalker(element, NodeFilter.SHOW_TEXT).nextNode()!;
      const range = document.createRange();
      range.setStart(text, offset);
      range.collapse(true);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
      element.closest<HTMLElement>('[contenteditable="true"]')!.focus();
    }, offset);
    await page.keyboard.press("Enter");
    await page.evaluate(async (label) => {
      await navigator.clipboard.write([new ClipboardItem({
        "text/html": new Blob([`<p><strong>${label} pasted bold</strong></p><p>తెలుగు 中文 العربية 🌱</p>`], { type: "text/html" }),
        "text/plain": new Blob([`${label} pasted bold\nతెలుగు 中文 العربية 🌱`], { type: "text/plain" }),
      })]);
    }, label);
    await page.keyboard.press("Meta+v");
    await expect(editor).toContainText(`${label} pasted bold`);
    await page.keyboard.press("Meta+s");
    await expect(page.locator(".save-state")).toHaveText("Saved");
    await expect(page.getByText("Missing revision guard", { exact: false })).toHaveCount(0);
  }
  const result = await page.evaluate(() => ({ blocks: (window as any).fixtureState().blocks, batches: (window as any).structuralBatches }));
  expect(result.blocks.find((b: any) => b.id === id(10)).data.richText[0].text).toBe("Imported Notion notebook");
  expect(result.blocks.find((b: any) => b.id === id(11)).data.richText[0].text).toBe("Original introduction");
  expect(result.blocks.flatMap((b: any) => b.data.richText).filter((s: any) => s.link).map((s: any) => s.text).join("")).toBe("Milestones");
  expect(result.blocks.flatMap((b: any) => b.data.richText).filter((s: any) => s.marks?.includes("bold")).map((s: any) => s.text)).toEqual(expect.arrayContaining(["First pasted bold", "Second pasted bold"]));
  expect(new Set(result.blocks.map((b: any) => b.id)).size).toBe(result.blocks.length);
  for (const batch of result.batches) {
    const guards = new Map(batch.mutation.expectedRevisions.filter((g: any) => g.target.entityType === "block").map((g: any) => [g.target.entityId, g.revision]));
    for (const c of batch.commands.filter((c: any) => c.kind === "block.reorder"))
      for (const blockID of c.orderedBlockIds) expect(guards.has(blockID)).toBe(true);
  }
  const headingGuards = result.batches.flatMap((b: any) => b.mutation.expectedRevisions.filter((g: any) => g.target.entityId === id(10)).map((g: any) => Number(g.revision)));
  expect(headingGuards[0]).toBe(1);
  expect(headingGuards.at(-1)).toBeGreaterThan(headingGuards[0]);
  await page.reload();
  await open(page);
  await expect(editor).toContainText("First pasted bold");
  await expect(editor).toContainText("Second pasted bold");
  await expect(editor).toContainText("తెలుగు 中文 العربية 🌱");
});
test("insert real code, Mermaid and KaTeX rendering", async ({ page }) => {
  await open(page);
  for (const label of ["Code", "Mermaid diagram", "Equation"]) {
    await page
      .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
      .click();
    await page.getByRole("button", { name: "＋ Insert", exact: true }).click();
    await page
      .getByRole("dialog", { name: "Insert a block" })
      .getByRole("button", { name: new RegExp("^" + label) })
      .click();
  }
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Saved");
  await expect(page.locator(".block-mermaid svg")).toBeVisible();
  await expect(page.locator(".katex")).toBeVisible();
  await expect(page.locator(".code-preview .hljs-comment")).toBeVisible();
});
test("Markdown shortcuts, duplication, keyboard focus and screen-reader structure", async ({
  page,
}) => {
  await open(page);
  await page
    .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
    .click();
  await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  await page.keyboard.type("## A useful heading");
  await expect(
    page
      .getByRole("textbox", { name: "Page body" })
      .getByRole("heading", { name: "A useful heading", level: 2 }),
  ).toBeVisible();
  await page.keyboard.press("Meta+Shift+d");
  await expect(
    page
      .getByRole("textbox", { name: "Page body" })
      .getByRole("heading", { name: "A useful heading", level: 2 }),
  ).toHaveCount(2);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "Save page" })).toBeFocused();
  const ax = await page.locator("body").ariaSnapshot();
  expect(ax).toContain('navigation "Page tree"');
  expect(ax).toContain('toolbar "Text formatting"');
  expect(ax).toContain('textbox "Page body"');
  expect(ax).toContain('heading "Imported detail" [level=4]');
  await page.getByRole("button", { name: "Save page" }).click();
  await expect(page.getByRole("status")).toHaveText("Saved");
});

const navSurface = async (page: any, name: string) =>
  page
    .getByRole("navigation", { name: "Workspace", exact: true })
    .getByRole("button", { name: new RegExp(`\\b${name}\\b`) })
    .click();
const save = async (page: any) => {
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".editor-toolbar").getByRole("status")).toHaveText(
    "Saved",
  );
};
const newPage = async (page: any, title: string) => {
  await navSurface(page, "Home");
  await page.getByRole("button", { name: "＋ New page", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
    "Untitled",
  );
  await page.getByRole("textbox", { name: "Page title" }).fill(title);
  await page.getByRole("textbox", { name: "Page title" }).press("Tab");
  await expect
    .poll(() =>
      page.evaluate(
        (title: string) =>
          (window as any)
            .fixtureState()
            .pages.some((p: any) => p.title === title),
        title,
      ),
    )
    .toBe(true);
};
const addText = async (page: any, text: string) => {
  await page
    .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
    .click();
  await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  await page.keyboard.insertText(text);
};

test("page lifecycle preserves identity through nesting, icon, archive and navigation", async ({
  page,
}) => {
  await newPage(page, "Field notes");
  const pageId = await page.evaluate(
    () =>
      (window as any)
        .fixtureState()
        .pages.find((p: any) => p.title === "Field notes").id,
  );
  page.once("dialog", (d) => d.accept("📚"));
  await page.getByRole("button", { name: "Set page icon" }).click();
  await expect(page.getByRole("button", { name: "Set page icon" })).toHaveText(
    "📚",
  );
  await page.getByRole("button", { name: "Page options" }).click();
  await page
    .getByRole("combobox", { name: "Move page parent" })
    .selectOption(id(2));
  await page.getByRole("button", { name: "Move here", exact: true }).click();
  await expect
    .poll(() =>
      page.evaluate(
        (pageId: string) =>
          (window as any).fixtureState().pages.find((p: any) => p.id === pageId)
            .parentPageId,
        pageId,
      ),
    )
    .toBe(id(2));
  const breadcrumbs = page.locator(".breadcrumbs");
  await expect(
    breadcrumbs.getByRole("button", { name: "A garden of ideas", exact: true }),
  ).toBeVisible();
  await expect(
    breadcrumbs.getByRole("button", { name: "Field notes", exact: true }),
  ).toBeVisible();
  await breadcrumbs
    .getByRole("button", { name: "A garden of ideas", exact: true })
    .click();
  await expect(
    page.getByRole("tab", { name: "▤ A garden of ideas", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(
    page.getByRole("tab", { name: "▤ Field notes", exact: true }),
  ).toHaveAttribute("aria-selected", "false");
  await page.getByRole("button", { name: "Go back", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
    "Field notes",
  );
  await page.getByRole("button", { name: "Go forward", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
    "A garden of ideas",
  );
  await page.getByRole("tab", { name: "▤ Field notes", exact: true }).click();
  await navSurface(page, "Home");
  await expect(page.locator(".card-grid .page-card").first()).toContainText(
    "Field notes",
  );
  const recentIDs = await page.evaluate(() =>
    JSON.parse(localStorage.getItem("trove.recents") ?? "[]"),
  );
  expect(recentIDs[0]).toBe(pageId);
  await page.getByRole("tab", { name: "▤ Field notes", exact: true }).click();
  await page.getByRole("button", { name: "Page options" }).click();
  await page.getByRole("button", { name: "Archive page", exact: true }).click();
  await expect(
    page
      .getByRole("navigation", { name: "Page tree" })
      .getByRole("button", { name: "📚 Field notes", exact: true }),
  ).toHaveCount(0);
  await navSurface(page, "Settings");
  const archived = page
    .locator(".settings-row")
    .filter({ hasText: "Field notes" });
  await expect(archived).toBeVisible();
  await archived
    .getByRole("button", { name: "Unarchive", exact: true })
    .click();
  await expect(
    page
      .getByRole("navigation", { name: "Page tree" })
      .getByRole("button", { name: "📚 Field notes", exact: true }),
  ).toBeVisible();
  await page.reload();
  await page
    .getByRole("navigation", { name: "Page tree" })
    .getByRole("button", { name: "📚 Field notes", exact: true })
    .click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
    "Field notes",
  );
  expect(
    await page.evaluate(
      (pageId: string) =>
        (window as any)
          .fixtureState()
          .pages.filter((p: any) => p.id === pageId)
          .map((p: any) => ({
            parent: p.parentPageId,
            icon: p.icon,
            archived: p.archivedAt,
          })),
      pageId,
    ),
  ).toEqual([{ parent: id(2), icon: "📚", archived: null }]);
  await page
    .getByRole("button", { name: "Close Field notes", exact: true })
    .click();
  await expect(
    page.getByRole("tab", { name: "▤ Field notes", exact: true }),
  ).toHaveCount(0);
});

test("all named surfaces open and created page links generate navigable backlinks", async ({
  page,
}) => {
  for (const [surface, heading] of [
    ["Home", "Make room for your ideas."],
    ["Inbox", "Inbox"],
    ["Pages", "Pages"],
    ["Tasks", "Tasks"],
    ["Meetings", "Meetings"],
    ["Search", "Search your workspace"],
    ["Import", "Import & export"],
    ["Settings", "Settings"],
  ]) {
    await navSurface(page, surface);
    await expect(
      page.getByRole("heading", { name: heading, exact: true }),
    ).toBeVisible();
  }
  await newPage(page, "Connected thought");
  await page.getByRole("button", { name: "＋ Insert", exact: true }).click();
  page.once("dialog", (d) => d.accept("A garden of ideas"));
  await page
    .getByRole("dialog", { name: "Insert a block" })
    .getByRole("button", { name: /^Page link/ })
    .click();
  await save(page);
  await page
    .getByRole("textbox", { name: "Page body" })
    .getByRole("button", { name: "↗ A garden of ideas", exact: true })
    .click();
  await expect(
    page
      .locator(".backlinks")
      .getByRole("button", { name: "↗ Connected thought", exact: true }),
  ).toBeVisible();
  await page
    .locator(".backlinks")
    .getByRole("button", { name: "↗ Connected thought", exact: true })
    .click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
    "Connected thought",
  );
  await navSurface(page, "Inbox");
  await expect(page.locator(".card-grid")).toContainText("Connected thought");
});

test("block transforms, rich marks, find and nested toggle survive save/reopen", async ({
  page,
}) => {
  await open(page);
  await addText(page, "Expandable idea");
  await page
    .getByRole("combobox", { name: "Block type" })
    .selectOption("toggle");
  await addText(page, "Nested searchable detail");
  await page
    .getByRole("combobox", { name: "Block type" })
    .selectOption("bulleted_list_item");
  await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Home");
  await page.keyboard.press("Shift+End");
  await page.getByRole("button", { name: "bold", exact: true }).click();
  await save(page);
  const added = await page.evaluate(() =>
    (window as any)
      .fixtureState()
      .blocks.filter((b: any) =>
        ["toggle", "bulleted_list_item"].includes(b.type),
      ),
  );
  const parent = added.find((b: any) => b.type === "toggle"),
    child = added.find((b: any) => b.type === "bulleted_list_item");
  expect(child.parentBlockId).toBe(parent.id);
  expect(
    child.data.richText.some((span: any) => span.marks?.includes("bold")),
  ).toBe(true);
  await page.reload();
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  const childNode = editor.locator(`[data-block-id="${child.id}"]`);
  await expect(childNode).toBeVisible();
  await editor
    .getByRole("button", { name: "Toggle content", exact: true })
    .click();
  await expect(childNode).toBeHidden();
  await editor
    .getByRole("button", { name: "Toggle content", exact: true })
    .click();
  await expect(childNode).toBeVisible();
  await page.getByRole("button", { name: "Find in page", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Find text", exact: true })
    .fill("searchable");
  await page.getByRole("button", { name: "Next match", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => window.getSelection()?.toString()))
    .toBe("searchable");
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Row 1 column 1", exact: true })
    .fill("Updated merged cell");
  await save(page);
  expect(
    await page.evaluate(
      () =>
        (window as any)
          .fixtureState()
          .blocks.find((b: any) => b.type === "table").data.cells[0],
    ),
  ).toMatchObject({
    rowSpan: 1,
    colSpan: 2,
    richText: [{ text: "Updated merged cell" }],
    preMergeCells: [{ value: "Original A" }],
  });
});

test("multi-file clipboard and file drop serialize uploads and preserve media captions and cover", async ({
  page,
}) => {
  const pixel =
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5L8AAAAASUVORK5CYII=";
  await page.route("**/files/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "image/png",
      body: Buffer.from(pixel, "base64"),
    }),
  );
  await open(page);
  await page
    .getByRole("textbox", { name: "Page body" })
    .evaluate((editor: any) => {
      const data = new DataTransfer();
      for (const [name, type] of [
        ["image.png", "image/png"],
        ["notes.pdf", "application/pdf"],
        ["recording.wav", "audio/wav"],
        ["clip.mp4", "video/mp4"],
      ])
        data.items.add(new File([`Fixture bytes for ${name}`], name, { type }));
      editor.dispatchEvent(
        new ClipboardEvent("paste", {
          clipboardData: data,
          bubbles: true,
          cancelable: true,
        }),
      );
    });
  await expect
    .poll(() =>
      page.evaluate(() => (window as any).fixtureState().files.length),
    )
    .toBe(4);
  await page.locator("main").evaluate((main: any) => {
    const data = new DataTransfer();
    data.items.add(
      new File(["Retained notes"], "notes.txt", { type: "text/plain" }),
    );
    main.dispatchEvent(
      new DragEvent("drop", {
        dataTransfer: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect
    .poll(() =>
      page.evaluate(() => (window as any).fixtureState().files.length),
    )
    .toBe(5);
  const metrics = await page.evaluate(() => (window as any).attachmentMetrics);
  expect(metrics.maxConcurrentUploads).toBe(1);
  expect(metrics.stagedNames).toEqual([
    "image.png",
    "notes.pdf",
    "recording.wav",
    "clip.mp4",
    "notes.txt",
  ]);
  const editor = page.getByRole("textbox", { name: "Page body" });
  for (const type of ["image", "pdf", "audio", "video", "file"])
    await expect(editor.locator(`.block-${type}`)).toBeVisible();
  await editor
    .locator(".block-image")
    .getByRole("textbox", { name: "Attachment caption", exact: true })
    .fill("A visual reference");
  await save(page);
  const imageID = await page.evaluate(
    () =>
      (window as any)
        .fixtureState()
        .files.find((f: any) => f.mediaType === "image/png").id,
  );
  await page.getByRole("button", { name: "Page options" }).click();
  await page
    .getByRole("combobox", { name: "Cover image" })
    .selectOption(imageID);
  await expect(page.locator(".page-cover img")).toHaveAttribute(
    "src",
    `/files/${imageID}`,
  );
  await page.keyboard.press("Escape");
  await page.reload();
  await open(page);
  await expect(
    editor
      .locator(".block-image")
      .getByRole("textbox", { name: "Attachment caption", exact: true }),
  ).toHaveValue("A visual reference");
  await expect(editor.locator("audio")).toHaveAttribute("controls", "");
  await expect(editor.locator("video")).toHaveAttribute("controls", "");
  await expect(editor.locator("object")).toHaveAttribute(
    "type",
    "application/pdf",
  );
  await expect(editor.locator(".file-card")).toContainText("notes.txt");
  await expect(page.locator(".page-cover img")).toHaveAttribute(
    "src",
    `/files/${imageID}`,
  );
});

test("Phase 2 search opens the exact cited block without model packs", async ({ page }) => {
  await navSurface(page, "Search");
  await expect(page.getByText("Keyword search works entirely offline without a model.", { exact: false })).toBeVisible();
  await page.getByRole("searchbox", { name: "Search text" }).fill("capture");
  await page.locator(".search-result").click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("A garden of ideas");
  await expect(page.locator(`[data-block-id="${id(11)}"]`)).toHaveClass(/source-highlight/);
  await expect(page.locator(".app-footer")).toContainText("Opened cited source");
});

test("copied page and block links route through editor navigation", async ({ page }) => {
  await page.evaluate(() => {
    const state = (window as any).fixtureState();
    const target = "019a1234-5678-7000-8000-000000000802";
    const block = "019a1234-5678-7000-8000-000000000803";
    state.pages.push({...state.pages[0], id: target, title: "Linked destination"});
    state.blocks.push({...state.blocks[1], id: block, pageId: target, data: {richText: [{text: "Exact target paragraph"}]}});
    state.blocks[1].data.richText = [{text: "Open target block", link: `trove://page/${target}/block/${block}?workspace=${state.workspaceId}`}];
    localStorage.setItem("fixture", JSON.stringify(state));
  });
  await page.reload();
  await open(page);
  await page.getByRole("link", {name: "Open target block"}).click();
  await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("Linked destination");
  await expect(page.locator(`[data-block-id="${id(803)}"]`)).toHaveClass(/source-highlight/);
  await expect(page.locator(".app-footer")).toContainText("Opened linked block");
});

test("native URL events consume a pending block link once", async ({ page }) => {
  await navSurface(page, "Search");
  await page.evaluate(() => {
    let pending = "trove://page/019a1234-5678-7000-8000-000000000002/block/019a1234-5678-7000-8000-000000000011";
    (window as any).go.main.App.PendingDeepLink = async () => {const value = pending; pending = ""; return value;};
    (window as any).emitFixtureEvent("native:openURL");
  });
  await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("A garden of ideas");
  await expect(page.locator(`[data-block-id="${id(11)}"]`)).toHaveClass(/source-highlight/);
  await navSurface(page, "Search");
  await page.evaluate(() => (window as any).emitFixtureEvent("native:openURL"));
  await expect(page.getByRole("searchbox", {name: "Search text"})).toBeVisible();
});

test("links report unavailable workspaces and missing blocks without leaving the current page", async ({ page }) => {
  await open(page);
  for (const [href, message] of [
    [`trove://page/${id(2)}?workspace=${id(999)}`, "another workspace"],
    [`trove://page/${id(2)}/block/${id(998)}`, "block is unavailable"],
    [`trove://page/${id(997)}`, "page is unavailable"],
  ]) {
    await page.evaluate((href) => {
      const app = (window as any).go.main.App;
      let pending = href;
      app.PendingDeepLink = async () => {const value = pending; pending = ""; return value;};
      (window as any).emitFixtureEvent("native:openURL");
    }, href);
    await expect(page.getByRole("alert")).toContainText(message);
    await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("A garden of ideas");
  }
});

test("Phase 2 import displays complete accounting and editable property preview", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App;
    const previous = app.Dispatch;
    app.PickFile = async () => "fixture-selection";
    const plan = {
      planId: "fixture-plan", planHash: "h", sourceNamespace: "fixture", sourceName: "Notion.zip",
      rootPageId: "root", sourceFileId: "source", state: "preview", cursor: 0, initialized: false,
      sourceCompleteness: "unknown", sourceOmissions: ["Original formula definitions omitted by source CSV"],
      unknownCompleteness: ["The original workspace item count is unknown"],
      items: [
        { sourceLocator: "Notes.md", kind: "markdown", outcome: "preserved", reason: "Text retained", committed: false },
        { sourceLocator: "Rows.csv", kind: "csv", outcome: "adapted", reason: "Database values parsed", committed: false,
          columns: [{ name: "Name", type: "title", propertyId: "a", samples: ["Plan"] }, { name: "Estimate", type: "number", propertyId: "b", samples: ["14"] }] },
        { sourceLocator: "Drawing.md", kind: "markdown", outcome: "partial", reason: "Readable representation retained", committed: false },
        { sourceLocator: "Custom.bin", kind: "file", outcome: "unsupported_but_retained", reason: "Original bytes retained", retainedFileId: "raw", committed: false },
        { sourceLocator: "../unsafe.md", kind: "file", outcome: "failed", reason: "Unsafe archive path", committed: false },
      ],
    };
    (window as any).phase2Mutations = 0;
    app.Dispatch = async (method: string, raw: string) => {
      if (method === "imports.preview") return JSON.stringify({ ok: true, result: plan });
      if (method === "imports.configure") {
        plan.items[1].columns![1].type = JSON.parse(raw).columns[0].type;
        return JSON.stringify({ ok: true, result: plan });
      }
      if (method === "imports.commit") (window as any).phase2Mutations++;
      return previous(method, raw);
    };
  });
  await navSurface(page, "Import");
  await page.getByRole("button", { name: "Choose import file" }).click();
  await expect(page.getByText("5 discovered items · 5 accounted for · 0 committed")).toBeVisible();
  for (const label of ["Preserved", "Adapted", "Partial", "Unsupported but retained", "Failed"])
    await expect(page.locator(".import-outcomes")).toContainText(label);
  await expect(page.getByRole("heading", { name: "Known source omissions" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Unknown completeness" })).toBeVisible();
  await page.getByLabel("Type for Estimate").selectOption("text");
  await expect(page.getByLabel("Type for Estimate")).toHaveValue("text");
  expect(await page.evaluate(() => (window as any).phase2Mutations)).toBe(0);
});

test("Phase 2 AI works without packs and shows reviewed memory controls", async ({ page }) => {
  await navSurface(page, "AI");
  await expect(page.getByRole("heading", { name: "Local AI", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Generate locally" })).toBeDisabled();
  await expect(page.getByRole("heading", { name: "Approved memory", exact: true })).toBeVisible();
  await open(page);
  await page.getByText("✧ Ask AI or work with this page", { exact: true }).click();
  await expect(page.getByLabel("AI source scope")).toHaveValue("page");
  await page.getByLabel("Writing operation").selectOption("shorten");
  await expect(page.getByLabel("Writing operation")).toHaveValue("shorten");
  await expect(page.getByRole("button", { name: "Remember for review" })).toBeDisabled();
});

test("Phase 2 failed job retry preserves revision and resumed progress", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    const job = { jobId: "fixture-job", kind: "TRANSCRIBE_AUDIO", state: "failed", attempt: 1, revision: "9", createdAt: "2026-09-30T10:00:00Z", progress: { completed: 120000, total: 3600000, unit: "milliseconds", message: "Saved two minutes" }, error: { message: "Worker interrupted" } };
    app.Dispatch = async (method: string, raw: string) => {
      if (method === "jobs.list") return JSON.stringify({ ok: true, result: { jobs: [job] } });
      if (method === "jobs.retry") { (window as any).retryRequest = JSON.parse(raw); job.state = "queued"; job.revision = "10"; return JSON.stringify({ ok: true, result: job }); }
      return previous(method, raw);
    };
  });
  await navSurface(page, "Jobs");
  await expect(page.getByText("Worker interrupted", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Retry saved inputs" }).click();
  await expect(page.locator(".job-state")).toHaveText("queued");
  expect(await page.evaluate(() => (window as any).retryRequest)).toMatchObject({ jobId: "fixture-job", expectedRevision: "9" });
  await expect(page.getByRole("progressbar", { name: "Transcription progress" })).toHaveAttribute("value", "120000");
});

test("Phase 2 native restore is reviewed before mutation and retention keeps all", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App;
    (window as any).restores = 0;
    app.PreviewNativeRestore = async () => JSON.stringify({ previewId: "review", sourceName: "Verified backup", manifest: { workspaceId: "source", createdAt: "2026-09-30T10:00:00Z", eventCursor: "12", assets: [{}], recordings: [{}] }, warnings: ["Review source revision before restoring."] });
    app.RestoreNativeBackup = async () => { (window as any).restores++; return JSON.stringify({ destination: "/new-workspace" }); };
  });
  await navSurface(page, "Settings");
  await expect(page.getByRole("combobox", { name: "Backup retention count" })).toHaveValue("0");
  await page.getByRole("button", { name: "Choose backup to review…" }).click();
  await expect(page.getByRole("article", { name: "Native restore preview" })).toBeVisible();
  expect(await page.evaluate(() => (window as any).restores)).toBe(0);
  await page.getByRole("button", { name: "Restore into a new folder…" }).click();
  await expect(page.getByText("Restored and verified at /new-workspace.", { exact: false })).toBeVisible();
  expect(await page.evaluate(() => (window as any).restores)).toBe(1);
});

test("Phase 2 AI review shows exact before and after and applies only on approval", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App;
    const previous = app.Dispatch;
    const state = (window as any).fixtureState();
    const source = state.blocks.find((b: any) => b.type === "paragraph");
    const run = {
      id: "019a1234-5678-7000-8000-000000001001", conversationId: "019a1234-5678-7000-8000-000000001002", state: "completed",
      request: { mode: "rewrite", prompt: "Make this concise", pageId: state.pages[0].id, blockId: source.id, useMemory: false },
      text: "Capture ideas. Connect what matters.", citations: [], sources: [{ number: 1, title: state.pages[0].title, text: "Capture the small things. Connect the useful ones.", state: "current", location: { pageId: state.pages[0].id, blockId: source.id, sourceRevision: "1" } }], createdAt: "2026-09-30T10:00:00Z",
    };
    (window as any).aiReviewCalls = { previews: 0, applications: 0 };
    app.Dispatch = async (method: string, raw: string) => {
      const ok = (result: any) => JSON.stringify({ ok: true, result });
      if (method === "models.list") return ok({ packs: [{ pack: { id: "01990000-0000-7000-8000-000000000105", name: "Verified instruction pack", kind: "chat" }, state: "ready" }], catalog: [], runtime: { available: true } });
      if (method === "ai.runs") return ok([run]);
      if (method === "ai.run") return ok(run);
      if (method === "ai.prepareAction") {
        const req = JSON.parse(raw);
        (window as any).aiReviewCalls.previews++;
        (window as any).aiReviewedText = req.body;
        return ok({ id: "019a1234-5678-7000-8000-000000001003", runId: run.id, state: "draft", kind: req.kind, pageId: state.pages[0].id, title: "", before: "Capture the small things. Connect the useful ones.", after: req.body, body: req.body, commands: [{ kind: "block.update", blockId: source.id, type: "paragraph", schemaVersion: 1, data: { richText: [{ text: req.body }] } }] });
      }
      if (method === "ai.applyAction") {
        (window as any).aiReviewCalls.applications++;
        source.data = { richText: [{ text: (window as any).aiReviewedText }] };
        source.revision = "2";
        state.pages[0].revision = "2";
        state.snapshotCursor = "2";
        return ok({ pageId: state.pages[0].id, state: "applied" });
      }
      if (method === "readPage") return ok({ page: state.pages[0], blocks: state.blocks });
      return previous(method, raw);
    };
  });
  await navSurface(page, "AI");
  await expect(page.getByLabel("Local instruction model")).toHaveValue("01990000-0000-7000-8000-000000000105");
  await page.getByText("Saved conversations · 1", { exact: true }).click();
  await page.getByRole("button", { name: "Make this concise · completed" }).click();
  await page.getByText("Review a page or task change", { exact: true }).click();
  await page.getByLabel("Reviewed action").selectOption("replace_block");
  await page.getByLabel("Reviewed draft text").fill("Capture ideas. Connect the useful ones.");
  await page.getByRole("button", { name: "Preview exact changes" }).click();
  await expect(page.locator(".ai-proposal")).toContainText("Before");
  await expect(page.locator(".ai-proposal")).toContainText("Capture the small things. Connect the useful ones.");
  await expect(page.locator(".ai-proposal")).toContainText("After");
  await expect(page.locator(".ai-proposal")).toContainText("Capture ideas. Connect the useful ones.");
  expect(await page.evaluate(() => (window as any).aiReviewCalls)).toEqual({ previews: 1, applications: 0 });
  await page.getByRole("button", { name: "Approve and apply these changes" }).click();
  await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("A garden of ideas");
  await expect(page.getByRole("textbox", { name: "Page body" })).toContainText("Capture ideas. Connect the useful ones.");
  expect(await page.evaluate(() => (window as any).aiReviewCalls)).toEqual({ previews: 1, applications: 1 });
});

async function installEditorAppendFixture(page: any) {
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    const state = (window as any).fixtureState();
    (window as any).editorBatches = [];
    app.Dispatch = async (method: string, raw: string) => {
      if (method === "batch") (window as any).editorBatches.push(JSON.parse(raw));
      return previous(method, raw);
    };
    (window as any).appendCanonicalTranscript = () => {
      const remote = { id: "019a1234-5678-7000-8000-000000000901", pageId: state.pages[0].id, parentBlockId: null, sortKey: "000000901000", type: "paragraph", schemaVersion: 1, data: { richText: [{ text: "Transcript 00:30 — Audio was preserved before recognition." }] }, revision: "1" };
      state.blocks.push(remote);
      state.pages[0].revision = String(Number(state.pages[0].revision) + 1);
      state.snapshotCursor = String(Number(state.snapshotCursor) + 1);
      localStorage.setItem("fixture", JSON.stringify(state));
      (window as any).emitFixtureEvent("workspace:changed");
    };
  });
}

test("background append synchronizes a clean editor without writes and survives the next edit", async ({ page }) => {
  await installEditorAppendFixture(page);
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  const original = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await original.click();
  await page.keyboard.press("End");
  await page.evaluate(() => (window as any).appendCanonicalTranscript());
  await expect(editor).toContainText("Transcript 00:30 — Audio was preserved before recognition.");
  expect(await page.evaluate(() => (window as any).editorBatches.length)).toBe(0);
  // No second click: synchronization must keep the caret in the original block.
  await page.keyboard.insertText(" Still writing here.");
  await expect(original).toContainText("Still writing here.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".save-state")).toHaveText("Saved");
  const result = await page.evaluate(() => ({ blocks: (window as any).fixtureState().blocks, batches: (window as any).editorBatches }));
  expect(result.blocks.filter((b: any) => b.id.endsWith("000000000901"))).toHaveLength(1);
  expect(result.batches[0].mutation.expectedRevisions.find((g: any) => g.target.entityType === "page").revision).toBe("2");
  expect(result.batches.flatMap((b: any) => b.commands).some((c: any) => c.kind === "block.delete" && c.blockId.endsWith("000000000901"))).toBe(false);
});

test("background append merges into a dirty editor and remains through save and local undo", async ({ page }) => {
  await installEditorAppendFixture(page);
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  const original = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await original.click();
  await page.keyboard.press("End");
  await page.keyboard.insertText(" Local draft before audio.");
  await page.evaluate(() => (window as any).appendCanonicalTranscript());
  await expect(editor).toContainText("Transcript 00:30 — Audio was preserved before recognition.");
  await expect(original).toContainText("Local draft before audio.");
  await page.keyboard.insertText(" And after audio.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".save-state")).toHaveText("Saved");
  expect(await page.evaluate(() => (window as any).fixtureState().blocks.find((b: any) => b.id.endsWith("000000000011")).data.richText.map((s: any) => s.text).join(""))).toContain("Local draft before audio. And after audio.");
  await expect(page.locator(".conflict")).toHaveCount(0);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(editor).toContainText("Transcript 00:30 — Audio was preserved before recognition.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".save-state")).toHaveText("Saved");
  expect(await page.evaluate(() => (window as any).fixtureState().blocks.filter((b: any) => b.id.endsWith("000000000901")).length)).toBe(1);
});

test("background append received during an in-flight save reconciles after completion without dropping ongoing typing", async ({ page }) => {
  await installEditorAppendFixture(page);
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    let held = false;
    app.Dispatch = async (method: string, raw: string) => {
      const result = await previous(method, raw);
      if (method === "batch" && !held && JSON.parse(raw).commands.some((c: any) => c.kind === "block.update")) {
        held = true;
        (window as any).appendCanonicalTranscript();
        (window as any).fixtureSaveWaiting = true;
        await new Promise<void>(resolve => { (window as any).finishFixtureSave = resolve; });
      }
      return result;
    };
  });
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  const original = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await original.click();
  await page.keyboard.press("End");
  await page.keyboard.insertText(" First saved draft.");
  await page.keyboard.press("Meta+s");
  await expect.poll(() => page.evaluate(() => (window as any).fixtureSaveWaiting)).toBe(true);
  await page.keyboard.insertText(" Typed during save.");
  await page.evaluate(() => (window as any).finishFixtureSave());
  await expect(editor).toContainText("Transcript 00:30 — Audio was preserved before recognition.");
  await expect(original).toContainText("First saved draft. Typed during save.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".save-state")).toHaveText("Saved");
  await expect(page.locator(".conflict")).toHaveCount(0);
  const result = await page.evaluate(() => ({ blocks: (window as any).fixtureState().blocks, batches: (window as any).editorBatches }));
  expect(result.blocks.filter((b: any) => b.id.endsWith("000000000901"))).toHaveLength(1);
  expect(result.blocks.find((b: any) => b.id.endsWith("000000000011")).data.richText.map((s: any) => s.text).join("")).toContain("First saved draft. Typed during save.");
  expect(result.batches.at(-1).mutation.expectedRevisions.find((g: any) => g.target.entityType === "page").revision).toBe("3");
});

test("overlapping background block edits preserve the local draft and require conflict review", async ({ page }) => {
  await installEditorAppendFixture(page);
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  const original = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await original.click();
  await page.keyboard.press("End");
  await page.keyboard.insertText(" My unsaved overlapping draft.");
  await page.evaluate(() => {
    const state = (window as any).fixtureState();
    const block = state.blocks.find((b: any) => b.id.endsWith("000000000011"));
    block.data.richText = [{ text: "A separately committed version of this same paragraph." }];
    block.revision = "2";
    state.pages[0].revision = "2";
    state.snapshotCursor = "2";
    (window as any).emitFixtureEvent("workspace:changed");
  });
  await expect(page.locator(".conflict")).toContainText("Your draft is still here");
  await expect(original).toContainText("My unsaved overlapping draft.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  expect(await page.evaluate(() => (window as any).editorBatches.length)).toBe(0);
  expect(await page.evaluate(() => (window as any).fixtureState().blocks.find((b: any) => b.id.endsWith("000000000011")).data.richText[0].text)).toBe("A separately committed version of this same paragraph.");
});

test("an append racing the initial save safely rebases and retries after a revision conflict", async ({ page }) => {
  await installEditorAppendFixture(page);
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    let first = true;
    app.Dispatch = async (method: string, raw: string) => {
      if (method === "batch" && first) {
        first = false;
        (window as any).appendCanonicalTranscript();
      }
      return previous(method, raw);
    };
  });
  await open(page);
  const editor = page.getByRole("textbox", { name: "Page body" });
  await editor.locator(`[data-block-id="${id(11)}"] .block-text`).click();
  await page.keyboard.press("End");
  await page.keyboard.insertText(" Retain the draft after the race.");
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(editor).toContainText("Transcript 00:30 — Audio was preserved before recognition.");
  await expect(page.locator(".save-state")).toHaveText("Saved");
  await expect(page.locator(".conflict")).toHaveCount(0);
  const result = await page.evaluate(() => ({ blocks: (window as any).fixtureState().blocks, batches: (window as any).editorBatches }));
  expect(result.batches).toHaveLength(2);
  expect(result.batches.map((b: any) => b.mutation.expectedRevisions.find((g: any) => g.target.entityType === "page").revision)).toEqual(["1", "2"]);
  expect(result.blocks.filter((b: any) => b.id.endsWith("000000000901"))).toHaveLength(1);
  expect(result.blocks.find((b: any) => b.id.endsWith("000000000011")).data.richText.map((s: any) => s.text).join("")).toContain("Retain the draft after the race.");
});

test("P0 saved audio loads Blob metadata, plays, seeks, and releases old recordings", async ({ page }) => {
  await page.route("**/files/**", route => route.fulfill({ status: 200, contentType: "audio/wav", body: pcmWAV(route.request().url().endsWith(id(520)) ? 4 : 2) }));
  await installMeetingAudioFixture(page);
  await navSurface(page, "Meetings");
  const player = page.locator(".meeting-detail audio");
  await expect(player).toHaveAttribute("src", /^blob:/);
  await expect.poll(() => player.evaluate((element: HTMLAudioElement) => element.duration)).toBe(4);
  const firstURL = await player.getAttribute("src");
  await player.evaluate(async (element: HTMLAudioElement) => { element.muted = true; await element.play(); });
  await expect.poll(() => player.evaluate((element: HTMLAudioElement) => element.currentTime)).toBeGreaterThan(.15);
  await player.evaluate((element: HTMLAudioElement) => { element.pause(); element.currentTime = 2; });
  await expect.poll(() => player.evaluate((element: HTMLAudioElement) => !element.seeking && Math.abs(element.currentTime - 2) < .05)).toBe(true);
  await player.evaluate((element: HTMLAudioElement) => element.play());
  await expect.poll(() => player.evaluate((element: HTMLAudioElement) => element.currentTime)).toBeGreaterThan(2.15);
  await player.evaluate((element: HTMLAudioElement) => { (window as any).previousRecordingPlayer = element; });
  await page.getByRole("button", { name: /^Second audio recording/ }).click();
  await expect.poll(() => player.evaluate((element: HTMLAudioElement) => element.duration)).toBe(2);
  const secondURL = await player.getAttribute("src");
  expect(secondURL).not.toBe(firstURL);
  await expect.poll(() => page.evaluate(url => (window as any).audioLifecycle.revoked.includes(url), firstURL)).toBe(true);
  expect(await page.evaluate(() => (window as any).previousRecordingPlayer.paused)).toBe(true);
  await navSurface(page, "Home");
  await expect.poll(() => page.evaluate(url => (window as any).audioLifecycle.revoked.includes(url), secondURL)).toBe(true);
  expect(await page.evaluate(() => { const m = (window as any).audioLifecycle; return m.created.every((url: string) => m.revoked.includes(url)); })).toBe(true);
});

test("P0 audio loading aborts on selection change and reports failed authorized fetches", async ({ page }) => {
  let releaseFirst!: () => void;
  let firstFinished!: () => void;
  const release = new Promise<void>(resolve => { releaseFirst = resolve; });
  const finished = new Promise<void>(resolve => { firstFinished = resolve; });
  await page.route("**/files/**", async route => {
    if (route.request().url().endsWith(id(520))) {
      await release;
      try { await route.fulfill({ status: 200, contentType: "audio/wav", body: pcmWAV(4) }); } catch { /* Cancellation may already have closed this request. */ }
      finally { firstFinished(); }
    } else {
      await route.fulfill({ status: 403, contentType: "text/plain", body: "Unavailable recording" });
    }
  });
  await installMeetingAudioFixture(page);
  await navSurface(page, "Meetings");
  await expect(page.getByRole("status").filter({ hasText: "Loading saved audio" })).toBeVisible();
  await page.getByRole("button", { name: /^Second audio recording/ }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Saved audio could not be loaded" })).toBeVisible();
  await expect.poll(() => page.evaluate(file => (window as any).audioLifecycle.aborted.includes(`/files/${file}`), id(520))).toBe(true);
  releaseFirst();
  await finished;
  await expect(page.getByRole("status").filter({ hasText: "Loading saved audio" })).toHaveCount(0);
  await expect(page.locator(".meeting-detail audio")).not.toHaveAttribute("src", /^blob:/);
  expect(await page.evaluate(() => (window as any).audioLifecycle.created)).toEqual([]);
});


test("advanced editor H4, replacement, page lock and native column content", async ({page}) => {
  await open(page);
  await page.getByRole("button",{name:"Find in page",exact:true}).click();
  await page.getByRole("textbox",{name:"Find text",exact:true}).fill("small things");
  await page.getByRole("textbox",{name:"Replace with",exact:true}).fill("useful details");
  await page.getByRole("button",{name:"Replace all (exact)",exact:true}).click();
  await page.getByRole("button",{name:"Save page",exact:true}).click();
  await expect(page.locator(".trove-editor")).toContainText("Capture the useful details");
  await page.getByRole("button",{name:"Lock page",exact:true}).click();
  await expect(page.getByRole("button",{name:"Unlock page",exact:true})).toBeVisible();
  await expect(page.getByRole("textbox",{name:"Page body",exact:true})).toHaveAttribute("contenteditable","false");
  await page.getByRole("button",{name:"Unlock page",exact:true}).click();
  await expect(page.getByRole("textbox",{name:"Page body",exact:true})).toHaveAttribute("contenteditable","true");
  await page.getByRole("button",{name:"＋ Insert",exact:true}).click();
  await page.getByRole("button",{name:"Columns Arrange reusable content side by side",exact:true}).click();
  await expect(page.locator(".column-panels")).toBeVisible();
  await page.getByRole("textbox",{name:"Column content",exact:true}).first().fill("Canonical column text");
  await page.getByRole("button",{name:"Save page",exact:true}).click();
  await expect.poll(()=>page.evaluate(()=>(window as any).fixtureState().blocks.some((b:any)=>JSON.stringify(b.data).includes("Canonical column text")))).toBe(true);
  await expect(page.locator('[data-kind="heading_4"] [aria-level="4"]')).toBeVisible();
});

async function installEditorLockFixture(page: any) {
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    const state = (window as any).fixtureState();
    const locks = new Map<string, {locked:boolean;revision:string}>();
    const lockFor = (pageId:string) => locks.get(pageId) ?? {locked:false,revision:'0'};
    (window as any).editorLockCalls = [];
    app.Dispatch = async (method:string, raw:string) => {
      const req = JSON.parse(raw);
      const ok = (result:any) => JSON.stringify({ok:true,result});
      const conflict = (message:string) => JSON.stringify({ok:false,error:{code:'CONFLICT',message,retryable:false,requestId:req.requestId}});
      if (method === 'editor.lock') return ok(lockFor(req.pageId));
      if (method === 'editor.setLock') {
        (window as any).editorLockCalls.push({method,request:req});
        const page = state.pages.find((p:any)=>p.id===req.pageId), before = lockFor(req.pageId);
        if (page.revision!==req.expectedPageRevision || before.revision!==req.expectedRevision)
          return conflict('Page or lock changed before lock operation');
        const next = {locked:req.locked,revision:String(Number(before.revision)+1)};
        locks.set(req.pageId,next);
        return ok(next);
      }
      if (method === 'batch') {
        (window as any).editorLockCalls.push({method,request:req});
        if (req.mutation.expectedRevisions.some((g:any)=>g.target.entityType==='page' && lockFor(g.target.entityId).locked))
          return conflict('Page is locked; unlock it before making changes');
      }
      return previous(method,raw);
    };
    (window as any).remoteEditorLock = (changedText:string|null=null, notify=true) => {
      const page = state.pages[0], before = lockFor(page.id);
      locks.set(page.id,{locked:true,revision:String(Number(before.revision)+1)});
      if (changedText!==null) {
        const block = state.blocks.find((b:any)=>b.id.endsWith('000000000011'));
        block.data.richText = [{text:changedText}];
        block.revision = String(Number(block.revision)+1);
        page.revision = String(Number(page.revision)+1);
      }
      if (notify) {
        state.snapshotCursor = String(Number(state.snapshotCursor)+1);
        (window as any).emitFixtureEvent('workspace:changed');
      }
    };
  });
}

test('Phase 5 page lock keeps saved advanced content clean and unlock permits editing', async ({page}) => {
  await installEditorLockFixture(page);
  await open(page);
  const editor = page.getByRole('textbox',{name:'Page body',exact:true});
  await page.getByRole('button',{name:'＋ Insert',exact:true}).click();
  await page.getByRole('button',{name:'Content tabs Group canonical content into tabs',exact:true}).click();
  await page.getByRole('textbox',{name:'Tab title',exact:true}).fill('Evidence');
  await page.getByRole('button',{name:'Save page',exact:true}).click();
  await expect(page.locator('.save-state')).toHaveText('Saved');
  const before = await page.evaluate(() => structuredClone((window as any).fixtureState().blocks));
  expect(before.map((b:any)=>b.type)).toEqual(expect.arrayContaining(['heading_4','table','tab_container']));
  await page.evaluate(() => { (window as any).editorLockCalls=[]; });
  await page.getByRole('button',{name:'Lock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','false');
  // Cross the autosave deadline: editability must create neither a dirty draft
  // nor a forbidden content write, even with structured editor nodes mounted.
  await page.waitForTimeout(1000);
  await expect(page.locator('.save-state')).toHaveText('Saved');
  expect(await page.evaluate(()=>(window as any).editorLockCalls.map((c:any)=>c.method))).toEqual(['editor.setLock']);
  expect(await page.evaluate(()=>(window as any).fixtureState().blocks)).toEqual(before);
  await page.getByRole('button',{name:'Unlock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','true');
  await expect(page.locator('.save-state')).toHaveText('Saved');
  const paragraph = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.insertText(' Saved after unlocking.');
  await page.getByRole('button',{name:'Save page',exact:true}).click();
  await expect(page.locator('.save-state')).toHaveText('Saved');
  await expect(page.locator('.conflict')).toHaveCount(0);
  const after = await page.evaluate(()=>(window as any).fixtureState().blocks);
  expect(after.find((b:any)=>b.id===id(11)).data.richText.map((r:any)=>r.text).join('')).toContain('Saved after unlocking.');
  expect(after.filter((b:any)=>['heading_4','table','tab_container'].includes(b.type))).toEqual(before.filter((b:any)=>['heading_4','table','tab_container'].includes(b.type)));
});

test('Phase 5 remote page lock preserves a pending draft and unlock retries it safely', async ({page}) => {
  await installEditorLockFixture(page);
  await open(page);
  const editor = page.getByRole('textbox',{name:'Page body',exact:true});
  const paragraph = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.insertText(' My pending draft survives.');
  // Simulate the real lock service: a lock need not emit a content revision or
  // notification. The save rejection must reveal Unlock without losing draft.
  await page.evaluate(()=>(window as any).remoteEditorLock(null,false));
  await page.getByRole('button',{name:'Save page',exact:true}).click();
  await expect(page.getByRole('button',{name:'Unlock page',exact:true})).toBeVisible();
  await expect(editor).toHaveAttribute('contenteditable','false');
  await expect(paragraph).toContainText('My pending draft survives.');
  await expect(page.locator('.save-state')).toHaveText('Unsaved changes (page locked)');
  await expect(page.locator('.conflict')).toHaveCount(0);
  await page.getByRole('button',{name:'Unlock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','true');
  await expect(paragraph).toContainText('My pending draft survives.');
  await expect(page.locator('.save-state')).toHaveText('Unsaved changes');
  expect(await page.evaluate(()=>(window as any).editorLockCalls.map((c:any)=>c.method))).toEqual(['batch','editor.setLock']);
  await page.getByRole('button',{name:'Save page',exact:true}).click();
  await expect(page.locator('.save-state')).toHaveText('Saved');
  expect(await page.evaluate(()=>(window as any).fixtureState().blocks.find((b:any)=>b.id.endsWith('000000000011')).data.richText.map((r:any)=>r.text).join(''))).toContain('My pending draft survives.');
});

test('Phase 5 locking a dirty page commits its draft before changing the lock', async ({page}) => {
  await installEditorLockFixture(page);
  await open(page);
  const editor = page.getByRole('textbox',{name:'Page body',exact:true});
  const paragraph = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.insertText(' Save this before locking.');
  await page.getByRole('button',{name:'Lock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','false');
  await expect(page.locator('.save-state')).toHaveText('Saved');
  const result = await page.evaluate(()=>({calls:(window as any).editorLockCalls,blocks:(window as any).fixtureState().blocks}));
  expect(result.calls.map((c:any)=>c.method)).toEqual(['batch','editor.setLock']);
  expect(result.calls[1].request.expectedPageRevision).toBe('2');
  expect(result.blocks.find((b:any)=>b.id===id(11)).data.richText.map((r:any)=>r.text).join('')).toContain('Save this before locking.');
  await page.getByRole('button',{name:'Unlock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','true');
  await expect(page.locator('.save-state')).toHaveText('Saved');
});

test('Phase 5 unlock preserves an overlapping draft and its original content revision fence', async ({page}) => {
  await installEditorLockFixture(page);
  await open(page);
  const editor = page.getByRole('textbox',{name:'Page body',exact:true});
  const paragraph = editor.locator(`[data-block-id="${id(11)}"] .block-text`);
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.insertText(' My conflicting draft stays here.');
  await page.evaluate(()=>(window as any).remoteEditorLock('New canonical text from another editor.'));
  await expect(editor).toHaveAttribute('contenteditable','false');
  await expect(page.locator('.conflict')).toContainText('Your draft is still here');
  await page.getByRole('button',{name:'Unlock page',exact:true}).click();
  await expect(editor).toHaveAttribute('contenteditable','true');
  await expect(paragraph).toContainText('My conflicting draft stays here.');
  await expect(page.locator('.conflict')).toContainText('Your draft is still here');
  await page.getByRole('button',{name:'Save page',exact:true}).click();
  const result = await page.evaluate(()=>({calls:(window as any).editorLockCalls,blocks:(window as any).fixtureState().blocks}));
  expect(result.calls.map((c:any)=>c.method)).toEqual(['editor.setLock']);
  expect(result.calls[0].request.expectedPageRevision).toBe('2');
  expect(result.blocks.find((b:any)=>b.id===id(11)).data.richText).toEqual([{text:'New canonical text from another editor.'}]);
});

test("Phase 4 surfaces expose scoped pairing, gated vendors and reviewed capture", async ({page}) => {
 await page.evaluate(()=>{
  const app=(window as any).go.main.App,previous=app.Dispatch;
  (window as any).phase4Calls=[];
  app.Dispatch=async(method:string,raw:string)=>{
   const r=JSON.parse(raw),ok=(result:any)=>JSON.stringify({ok:true,result});
   (window as any).phase4Calls.push(method);
   if(method==='providers.status')return ok({connections:[],capabilities:[{provider:'codex',authOwner:'Trove',route:'official SIWC',enabled:false,reason:'Runtime and containment qualification required'},{provider:'claude_code',authOwner:'Claude Code',route:'unchanged CLI',enabled:false,reason:'Runtime and containment qualification required'}]});
   if(method==='providers.proposals')return ok([]);
   if(method==='providers.preview')return ok({pages:r.pageIds.map((pageId:string)=>({pageId,title:'A garden of ideas',revision:'1'})),notice:'Only these pages are shared.'});
   if(method==='providers.connect')return ok({connection:{id:'paired',provider:'mcp',state:'connected',token:'fixture-only-token'},url:'http://127.0.0.1:4000/mcp',relayPath:'/fixture/trove-mcp'});
   if(method==='index.roots')return ok([]);
   if(method==='automationHistory')return ok({rules:[],runs:[]});
   if(method==='capture.preview')return ok({planId:'clip',planHash:'fixture',state:'previewed',items:[{sourceLocator:'selection',outcome:'adapted',reason:'Reviewed text retained'}]});
   return previous(method,raw);
  };
 });
 await navSurface(page,'Providers');
 await expect(page.getByRole('button',{name:'Preview shared context'})).toBeDisabled();
 await page.getByLabel('A garden of ideas').check();
 await page.getByRole('button',{name:'Preview shared context'}).click();
 await expect(page.getByText('Only these pages are shared.',{exact:false})).toBeVisible();
 expect(await page.evaluate(()=>(window as any).phase4Calls.includes('providers.connect'))).toBe(false);
 await page.screenshot({path:`${process.env.TROVE_BROWSER_SCREENSHOTS ?? '../evidence/phase4'}/providers-preview.png`,fullPage:true});
 await page.getByRole('button',{name:'Approve context and pair client'}).click();
 await expect(page.getByText('fixture-only-token',{exact:false})).toBeVisible();
 await page.getByLabel('Provider',{exact:true}).selectOption('codex');
 await expect(page.getByText('Unavailable in this build.',{exact:false})).toBeVisible();
 await navSurface(page,'Capture');
 await page.getByLabel('Shared selection').fill('A clipped offline note');
 await page.getByRole('button',{name:'Preview clip',exact:true}).click();
 await expect(page.getByRole('button',{name:'Save reviewed capture'})).toBeVisible();
 expect(await page.evaluate(()=>(window as any).phase4Calls.includes('imports.commit'))).toBe(false);
 await navSurface(page,'Automations');
 await expect(page.getByText(/while Trove is open/)).toBeVisible();
});

test("AI review rejects malformed Mermaid before approval", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    const run = { id: "mermaid-run", conversationId: "diagram", state: "completed", request: { mode: "mermaid", prompt: "Draw a diagram", useMemory: false }, text: "flowchart LR\n A -->", citations: [], sources: [], createdAt: "2026-09-30T10:00:00Z" };
    (window as any).diagramPreviews = 0;
    app.Dispatch = async (method: string, raw: string) => {
      const ok = (result: any) => JSON.stringify({ ok: true, result });
      if (method === "ai.runs") return ok([run]);
      if (method === "ai.run") return ok(run);
      if (method === "ai.prepareAction") {
        const r = JSON.parse(raw);
        (window as any).diagramPreviews++;
        return ok({ id: "diagram-proposal", kind: "mermaid", after: r.body, body: r.body, commands: [{ kind: "block.insert", block: { type: "mermaid", data: { source: r.body } } }] });
      }
      return previous(method, raw);
    };
  });
  await navSurface(page, "AI");
  await page.getByText("Saved conversations · 1", { exact: true }).click();
  await page.getByRole("button", { name: "Draw a diagram · completed" }).click();
  await page.getByText("Review a page or task change", { exact: true }).click();
  await page.getByLabel("Reviewed action").selectOption("mermaid");
  await page.getByRole("button", { name: "Preview exact changes" }).click();
  await expect(page.getByRole("alert")).toContainText("Mermaid syntax needs a correction");
  await expect(page.getByRole("button", { name: "Approve and apply these changes" })).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).diagramPreviews)).toBe(0);
  await page.getByLabel("Reviewed draft text").fill("flowchart LR\n A --> B");
  await page.getByRole("button", { name: "Preview exact changes" }).click();
  await expect(page.getByRole("button", { name: "Approve and apply these changes" })).toBeVisible();
  expect(await page.evaluate(() => (window as any).diagramPreviews)).toBe(1);
});

test("provider disconnect stays available while approval is pending", async ({ page }) => {
  await page.evaluate(() => {
    const app = (window as any).go.main.App, previous = app.Dispatch;
    let state = "connected", proposalState = "pending", finish: ((result: string) => void) | undefined;
    (window as any).providerDisconnects = 0;
    app.Dispatch = async (method: string, raw: string) => {
      const ok = (result: any) => JSON.stringify({ ok: true, result });
      if (method === "providers.status") return ok({ connections: [{ id: "paired", provider: "mcp", state, pageIds: ["page"], tools: ["proposeChanges"] }], capabilities: [] });
      if (method === "providers.proposals") return ok([{ id: "proposal", state: proposalState, request: { commands: [{ kind: "page.update", title: "Reviewed change" }] } }]);
      if (method === "providers.approve") return new Promise<string>(resolve => { finish = resolve; });
      if (method === "providers.disconnect") {
        state = "disconnected";
        proposalState = "interrupted";
        (window as any).providerDisconnects++;
        finish?.(JSON.stringify({ ok: false, error: { code: "SCOPE_DENIED", message: "Connection revoked before commit" } }));
        return ok({ state });
      }
      return previous(method, raw);
    };
  });
  await navSurface(page, "Providers");
  await page.getByRole("button", { name: "Approve these exact changes" }).click();
  await expect(page.getByRole("button", { name: "Approve these exact changes" })).toBeDisabled();
  const disconnect = page.getByRole("button", { name: "Disconnect and revoke local access" });
  await expect(disconnect).toBeEnabled();
  await disconnect.click();
  await expect.poll(() => page.evaluate(() => (window as any).providerDisconnects)).toBe(1);
  await expect(page.getByText("mcp · disconnected", { exact: true })).toBeVisible();
});
