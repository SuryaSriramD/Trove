import { test, expect } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
test("native clipper review preserves pairing details and requires a fresh destination review before save", async ({
  page,
}) => {
  await page.addInitScript(
    ({ id }) => {
      const plan: any = {
        planId: id[4],
        planHash: "first-hash",
        sourceName: "Browser article",
        state: "preview",
        initialized: false,
        cursor: 0,
        rootPageId: id[5],
        captureDestinationTitle: "Approved page",
        sourceOmissions: ["Remote resources not acquired"],
        unknownCompleteness: ["Dynamic content may be missing"],
        items: [
          {
            sourceLocator: "clip.html",
            outcome: "supported",
            reason: "Sanitized source",
            committed: false,
            pageId: id[6],
            kind: "html",
            blocks: [
              {
                type: "paragraph",
                data: {
                  richText: [{ text: "<script>Source is inert</script>" }],
                },
              },
            ],
          },
        ],
      };
      const state: any = {
        contractVersion: "1.0.0",
        workspaceId: id[0],
        scopeGrantId: id[2],
        snapshotCursor: "1",
        pages: [
          {
            id: id[1],
            workspaceId: id[0],
            parentPageId: null,
            title: "Approved page",
            revision: "1",
            createdAt: "2026-10-04T10:00:00Z",
            updatedAt: "2026-10-04T10:00:00Z",
            deletedAt: null,
            icon: null,
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
        containers: [],
        containerCollections: [],
        transcriptSegments: [],
        projectScopes: [],
        memories: [],
        importMappings: [],
      };
      let listening = false,
        paired = false;
      const calls: any[] = [];
      (window as any).clipperCalls = calls;
      (window as any).runtime = { EventsOn: () => () => {} };
      (window as any).go = {
        main: {
          App: {
            ShellReady: async () => {},
            SetEditing: async () => {},
            ClipperStatus: async () => JSON.stringify({ listening, paired }),
            ClipperReviewPlans: async () => JSON.stringify([plan]),
            ShowClipperExtension: async () => {},
            BeginClipperPairing: async (
              extension: string,
              destinations: string[],
            ) => {
              calls.push({ operation: "pair", extension, destinations });
              listening = true;
              return JSON.stringify({
                endpoint: "http://127.0.0.1:12345",
                code: "one-time-secret",
                extensionId: extension,
                expiresAt: "2099-01-01T10:00:00Z",
                sessionOnly: true,
              });
            },
            RevokeClipperPairing: async () => {
              calls.push({ operation: "revoke" });
              listening = false;
              paired = false;
            },
            RefreshClipperDestination: async (planId: string, hash: string) => {
              calls.push({ operation: "refresh", planId, hash });
              plan.planHash = "fresh-review-hash";
              return JSON.stringify(plan);
            },
            Dispatch: async (method: string, raw: string) => {
              const request = JSON.parse(raw),
                ok = (result: any) => JSON.stringify({ ok: true, result });
              if (method === "workspace.snapshot") return ok(state);
              if (
                method === "index.roots" ||
                method === "ai.runs" ||
                method === "imports.list"
              )
                return ok([]);
              if (method === "memory.list") return ok({ entries: [] });
              if (method === "jobs.list") return ok({ jobs: [] });
              if (method === "models.list")
                return ok({
                  packs: [],
                  catalog: [],
                  runtime: { available: false },
                });
              if (method === "search.semanticSettings")
                return ok({ enabled: false, modelPackId: "", dimension: 0 });
              if (method === "imports.commit") {
                calls.push({ operation: "commit", hash: request.planHash });
                if (request.planHash !== "fresh-review-hash")
                  return JSON.stringify({
                    ok: false,
                    error: {
                      code: "CONFLICT",
                      message:
                        "Capture destination changed; refresh for review",
                      retryable: false,
                      requestId: request.requestId,
                    },
                  });
                plan.initialized = true;
                plan.cursor = plan.items.length;
                plan.state = "complete";
                plan.items[0].committed = true;
                return ok(plan);
              }
              throw Error("Unexpected clipper fixture service: " + method);
            },
          },
        },
      };
    },
    { id: Array.from({ length: 8 }, (_, i) => id(i + 1)) },
  );
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191");
  await page
    .getByRole("button", { name: /^⊕ Capture$|^Capture$/ })
    .first()
    .click();
  const clipper = page.getByRole("region", {
    name: "Browser clipper",
    exact: true,
  });
  await clipper
    .getByLabel("Clipper extension ID")
    .fill("mmdbnaapeedpfjhafldjcacmanhjmjbb");
  await clipper.getByLabel("Clipper approved page").selectOption(id(2));
  await clipper
    .getByRole("button", { name: "Create one-time pairing code" })
    .click();
  await expect(clipper.getByLabel("Clipper pairing code")).toHaveValue(
    "one-time-secret",
  );
  await expect(clipper.getByLabel("Clipper local endpoint")).toHaveValue(
    "http://127.0.0.1:12345",
  );
  expect(
    await page.evaluate(() => (window as any).clipperCalls[0]),
  ).toMatchObject({ operation: "pair", destinations: ["inbox", id(2)] });
  await clipper
    .getByRole("button", { name: "Browser article", exact: true })
    .click();
  const review = page.getByRole("region", { name: "Browser capture review" });
  await expect(review).toContainText("<script>Source is inert</script>");
  await expect(review.locator("script")).toHaveCount(0);
  await expect(review).toContainText("Dynamic content may be missing");
  await review
    .getByRole("button", { name: "Save reviewed browser capture" })
    .click();
  await expect(clipper.getByRole("alert")).toContainText("destination changed");
  await expect(
    review.getByRole("button", { name: "Save reviewed browser capture" }),
  ).toBeVisible();
  await review
    .getByRole("button", { name: "Refresh destination for review" })
    .click();
  await expect(clipper).toContainText("Review again before saving");
  expect(
    await page.evaluate(
      () =>
        (window as any).clipperCalls.filter(
          (call: any) => call.operation === "commit",
        ).length,
    ),
  ).toBe(1);
  await review
    .getByRole("button", { name: "Save reviewed browser capture" })
    .click();
  await expect(
    review.getByRole("button", { name: "Open saved browser capture" }),
  ).toBeVisible();
  await expect(clipper).toContainText("saved as offline workspace content");
  await clipper.getByRole("button", { name: "Revoke browser pairing" }).click();
  await expect(clipper).toContainText("listener closed");
  await expect(clipper.getByLabel("Clipper pairing code")).toHaveCount(0);
  await expect(
    clipper.getByRole("button", { name: "Browser article", exact: true }),
  ).toBeVisible();
});
