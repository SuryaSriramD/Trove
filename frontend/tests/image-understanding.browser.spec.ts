import { expect, test, type Page } from "@playwright/test";
async function fixture(page: Page) {
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const owner = {
      id: id(2),
      workspaceId: id(1),
      title: "Image evidence",
      parentPageId: null,
      revision: "1",
      createdAt: "2026-10-05T00:00:00Z",
      updatedAt: "2026-10-05T00:00:00Z",
      deletedAt: null,
    };
    const state = {
      workspaceId: id(1),
      scopeGrantId: id(3),
      snapshotCursor: "1",
      pages: [owner],
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
    const runs: any[] = [];
    (window as any).imageCalls = [];
    (window as any).imageRuns = runs;
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          WorkspaceInfo: () => ({ workspaceId: id(1) }),
          Dispatch: async (method: string, raw: string) => {
            const req = JSON.parse(raw || "{}");
            (window as any).imageCalls.push({ method, req });
            const ok = (result: any) => JSON.stringify({ ok: true, result });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "readPage")
              return ok({ page: owner, blocks: [], snapshotCursor: "1" });
            if (method === "ocr.status") return ok({ available: false });
            if (method === "review.list")
              return ok({
                plans: [],
                runs: [],
                engine: "Local",
                scheduling: "App open",
              });
            if (method === "models.list")
              return ok({
                runtime: { available: true, visionAvailable: true },
                packs: [
                  {
                    pack: {
                      id: id(107),
                      name: "Installed vision pair",
                      kind: "vision",
                    },
                    state: "ready",
                  },
                ],
                catalog: [],
              });
            if (method === "image.list")
              return ok({
                runs,
                truncated: false,
                images: [{ id: id(70), originalName: "Shapes.png" }],
                imagesTruncated: false,
              });
            if (method === "image.start") {
              const run = {
                id: id(80),
                pageId: req.pageId,
                fileId: req.fileId,
                modelPackId: req.modelPackId,
                state: "running",
                question: req.question,
                text: "",
                warning: "Experimental local draft",
                pageRevision: "1",
                fileRevision: "1",
                contentHash: "a".repeat(64),
                stale: false,
              };
              runs.unshift(run);
              return ok(run);
            }
            if (method === "image.cancel") {
              runs[0].state = "cancelled";
              runs[0].error = "Cancelled by you.";
              return ok(runs[0]);
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
            if (method === "links.backlinks") return ok({ links: [] });
            if (method === "ai.runs" || method === "imports.list")
              return ok([]);
            if (method === "memory.list") return ok({ entries: [] });
            throw Error("Unexpected method " + method);
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197");
  await page.getByRole("button", { name: /Intelligence/ }).click();
  const panel = page.getByRole("region", { name: "P2 intelligence packages" });
  await panel
    .getByRole("button", { name: "Image questions", exact: true })
    .click();
  return panel.getByRole("region", { name: "Local image understanding" });
}
test("image questions are explicit local drafts with source preview and cancellation", async ({
  page,
}) => {
  const vision = await fixture(page),
    ask = vision.getByRole("button", { name: "Ask about image locally" });
  await expect(ask).toBeDisabled();
  await vision
    .getByLabel("Image attached to this page")
    .selectOption({ label: "Shapes.png" });
  await vision
    .getByLabel("Local image model")
    .selectOption({ label: "Installed vision pair" });
  await expect(
    vision.getByAltText("Original image for the question"),
  ).toHaveAttribute("src", /files\//);
  await vision.getByLabel("Image question").fill("What color is the square?");
  await ask.click();
  await expect(
    vision.getByRole("heading", { name: "running", exact: true }),
  ).toBeVisible();
  await expect(ask).toBeDisabled();
  await expect(
    vision.getByRole("link", { name: "Open original image" }),
  ).toHaveAttribute("href", /files\//);
  await vision.getByRole("button", { name: "Cancel image analysis" }).click();
  await expect(
    vision.getByRole("heading", { name: "cancelled", exact: true }),
  ).toBeVisible();
  await expect(ask).toBeEnabled();
  const calls = await page.evaluate(() => (window as any).imageCalls);
  expect(
    calls.some(
      (c: any) =>
        c.method === "image.start" &&
        c.req.question === "What color is the square?",
    ),
  ).toBe(true);
  expect(calls.some((c: any) => c.method === "batch")).toBe(false);
});

test("source pixel abstention is visibly distinct from model interpretation and keeps original provenance", async ({ page }) => {
  const vision = await fixture(page);
  await vision.getByLabel("Image attached to this page").selectOption({ label: "Shapes.png" });
  await vision.getByRole("button", { name: "Ask about image locally" }).click();
  await expect(vision.getByRole("heading", { name: "running", exact: true })).toBeVisible();
  await page.evaluate(() => {
    Object.assign((window as any).imageRuns[0], {
      state: "ready", analysisMethod: "pixel_measurement", finishReason: "measurement",
      text: "Source pixel measurement (256 × 256 decoded pixels, before resizing and alpha composition; encoded orientation): the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted.",
      warning: "Measured from all decoded source pixels before resizing or alpha composition, using encoded orientation. Original bytes are retained; no model interpretation or OCR was performed.",
    });
  });
  await vision.getByRole("button", { name: "Refresh image drafts" }).click();
  await expect(vision.getByText("Local pixel measurement; model not run.", { exact: true })).toBeVisible();
  await expect(vision.getByText(/Measured from all decoded source pixels before resizing/)).toBeVisible();
  await expect(vision.getByText(/the image is exactly uniform white/)).toBeVisible();
  await vision.getByText("Source and model evidence", { exact: true }).click();
  await expect(vision.getByText(`Original SHA-256: ${"a".repeat(64)}`, { exact: true })).toBeVisible();
  await expect(vision.getByText("Page revision 1 · file revision 1", { exact: true })).toBeVisible();
  await expect(vision.getByText(/Model SHA-256:/)).toHaveCount(0);
  await expect(vision.getByRole("link", { name: "Open original image" })).toHaveAttribute("href", /files\//);
  expect(await page.evaluate(() => (window as any).imageCalls.some((c: any) => c.method === "batch"))).toBe(false);
});
