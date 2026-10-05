import { test, expect } from "@playwright/test";

test("OCR shows the actual local engine and source provenance, and drops superseded image results", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const state: any = {
      workspaceId: id(1),
      scopeGrantId: id(3),
      snapshotCursor: "1",
      pages: [
        {
          id: id(2),
          workspaceId: id(1),
          title: "OCR evidence",
          parentPageId: null,
          revision: "1",
          createdAt: "2026-10-01T00:00:00Z",
          updatedAt: "2026-10-01T00:00:00Z",
          deletedAt: null,
        },
      ],
      blocks: [],
      collections: [],
      properties: [],
      records: [],
      views: [],
      files: [4, 5].map((n) => ({
        id: id(n),
        workspaceId: id(1),
        originalName: `source-${n}.png`,
        mediaType: "image/png",
        revision: "7",
        sizeBytes: 100,
      })),
      meetings: [],
      taskFacets: [],
      memories: [],
      containers: [],
      containerCollections: [],
      transcriptSegments: [],
      projectScopes: [],
      importMappings: [],
    };
    (window as any).ocrRequests = [];
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const req = JSON.parse(raw),
              ok = (result: any) => JSON.stringify({ ok: true, result });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "ocr.status")
              return ok({
                available: true,
                engine: "Tesseract 5.5.1 (local)",
                languages: ["English"],
                limitations:
                  "English printed text only. Encoded orientation is retained.",
                maxBytes: 20 << 20,
                maxPixels: 20000000,
                timeoutSeconds: 60,
                minimumConfidence: 0.5,
              });
            if (method === "review.list")
              return ok({
                plans: [],
                runs: [],
                engine: "Deterministic local review",
                scheduling: "While open",
              });
            if (method === "ocr.run") {
              (window as any).ocrRequests.push(req);
              if ((window as any).holdOCR)
                await new Promise(
                  (resolve) => ((window as any).releaseOCR = resolve),
                );
              return ok({
                fileId: req.fileId,
                sourceRevision: "7",
                engine: "Tesseract 5.5.1 (local)",
                languages: ["English"],
                extractorVersion: "trove-tesseract-5.5.1-eng-known",
                modelSha256: "known",
                text: "Printed fixture witness",
                warning: "English only",
                acceptedLines: 1,
                rejectedLines: 0,
              });
            }
            if (method === "providers.status") return ok({ providers: [] });
            if (method === "jobs.list") return ok({ jobs: [] });
            if (method === "search.query") return ok({ hits: [] });
            if (method === "app.status") return ok({});
            return ok({});
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  await page.getByRole("button", { name: /Intelligence/ }).click();
  const panel = page.getByRole("region", { name: "P2 intelligence packages" });
  await panel.getByRole("button", { name: "Image text", exact: true }).click();
  await expect(panel.getByText(/Tesseract 5.5.1.*ready/)).toBeVisible();
  await expect(panel.getByText(/English printed text only/)).toBeVisible();
  await expect(panel.getByText(/Apple Vision/)).toHaveCount(0);
  await panel
    .getByLabel("Attached image")
    .selectOption({ label: "source-4.png" });
  expect(await page.evaluate(() => (window as any).ocrRequests.length)).toBe(0);
  await panel
    .getByRole("button", { name: "Extract image text locally" })
    .click();
  await expect(panel.getByText("Printed fixture witness")).toBeVisible();
  await expect(
    panel.getByText(/Source revision: 7. Extractor: trove-tesseract/),
  ).toBeVisible();
  await page.evaluate(() => ((window as any).holdOCR = true));
  await panel
    .getByRole("button", { name: "Extract image text locally" })
    .click();
  await expect
    .poll(() => page.evaluate(() => (window as any).ocrRequests.length))
    .toBe(2);
  await panel
    .getByLabel("Attached image")
    .selectOption({ label: "source-5.png" });
  await page.evaluate(() => (window as any).releaseOCR());
  await expect(
    panel.getByRole("button", { name: "Extract image text locally" }),
  ).toBeEnabled();
  await expect(panel.getByText("Printed fixture witness")).toHaveCount(0);
});
