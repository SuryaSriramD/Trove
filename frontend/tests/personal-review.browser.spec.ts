import { expect, test, type Page } from "@playwright/test";
async function fixture(page: Page) {
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
        title: n === 2 ? "Review inbox" : "Launch evidence",
        parentPageId: null,
        revision: "1",
        createdAt: "2026-10-04T00:00:00Z",
        updatedAt: "2026-10-04T00:00:00Z",
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
    const configuration = {
      modelPackId: id(105),
      categories: ["risks"],
      allowedActions: ["append_review", "rename_page"],
      actionPageIds: [id(5)],
    };
    const runs = [
      {
        id: id(8),
        pageId: id(2),
        planBlockId: id(7),
        status: "ready",
        text: "risks: The launch awaits security review. — “Security review is pending.” [1]",
        sources: [{ pageId: id(5), sourceRevision: "1" }],
        citations: [
          {
            number: 1,
            title: "Launch evidence",
            location: { pageId: id(5), sourceRevision: "1" },
          },
        ],
        createdAt: "2026-10-04T09:00:00Z",
        generative: configuration,
        actions: [
          {
            kind: "update_page",
            target: "Launch evidence",
            title: "Reviewed launch",
          },
        ],
        warning: "Analysis uses bounded excerpts; verify the interpretation.",
      },
      {
        id: id(9),
        pageId: id(2),
        planBlockId: id(7),
        status: "queued",
        text: "Captured digest",
        sources: [],
        createdAt: "2026-10-04T10:00:00Z",
        generative: configuration,
        attempt: 0,
      },
    ];
    (window as any).reviewCalls = [];
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const req = JSON.parse(raw || "{}"),
              ok = (result: any) => JSON.stringify({ ok: true, result });
            (window as any).reviewCalls.push({ method, req });
            if (method === "workspace.snapshot") return ok(state);
            if (method === "readPage")
              return ok({
                page: state.pages.find((p: any) => p.id === req.pageId),
                blocks: state.blocks.filter(
                  (b: any) => b.pageId === req.pageId,
                ),
              });
            if (method === "ocr.status") return ok({ available: false });
            if (method === "review.list")
              return ok({
                plans: [],
                runs,
                engine: "Optional local model review",
                scheduling: "While the owner is running",
              });
            if (method === "models.list")
              return ok({
                packs: [
                  {
                    pack: {
                      id: id(105),
                      name: "Installed instruction model",
                      kind: "chat",
                    },
                    state: "ready",
                  },
                ],
              });
            if (method === "allocateIds") return ok({ ids: [id(20)] });
            if (method === "batch") {
              const block = req.commands.find(
                (c: any) => c.kind === "block.insert",
              )?.block;
              if (block) {
                state.blocks.push({
                  ...block,
                  id: block.blockId,
                  revision: "1",
                });
                state.pages[0].revision = "2";
                state.snapshotCursor = "2";
              }
              return ok({ revisions: [], eventCursor: "2", replayed: false });
            }
            if (method === "review.propose")
              return ok({
                id: id(10),
                changes: [
                  {
                    label: "Rename page",
                    destination: "Launch evidence",
                    before: "Launch evidence",
                    after: "Reviewed launch",
                  },
                  {
                    label: "Append review",
                    destination: "Review inbox",
                    after: runs[0].text,
                  },
                ],
              });
            if (method === "review.apply") {
              runs[0].status = "accepted";
              return ok(runs[0]);
            }
            if (method === "review.cancel") {
              runs[1].status = "cancelled";
              runs[1].error = "Cancelled by the owner; no changes applied.";
              return ok(runs[1]);
            }
            if (method === "review.retry") {
              runs[1].status = "queued";
              return ok(runs[1]);
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
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197");
  await page.getByRole("button", { name: /Intelligence/ }).click();
  const panel = page.getByRole("region", { name: "P2 intelligence packages" });
  await panel
    .getByRole("button", { name: "Personal review", exact: true })
    .click();
  return panel;
}
test("model review plan requires explicit consent and persists categories, fields, destinations and weekly schedule", async ({
  page,
}) => {
  const panel = await fixture(page);
  await panel.getByLabel("Enable local model analysis for this plan").check();
  const save = panel.getByRole("button", {
    name: "Save review plan on this page",
  });
  await expect(save).toBeDisabled();
  await panel
    .getByRole("combobox", { name: "Review model", exact: true })
    .selectOption({ label: "Installed instruction model" });
  await panel.getByLabel("Rename allowed pages").check();
  await panel.getByLabel("Update allowed task status").check();
  await panel.getByLabel("Update allowed task due date").check();
  await panel
    .getByRole("listbox", { name: "Allowed action destinations", exact: true })
    .selectOption({ label: "Launch evidence" });
  await panel
    .getByRole("combobox", { name: "Source page", exact: true })
    .selectOption({ label: "Launch evidence" });
  await panel
    .getByRole("combobox", { name: "Recurrence", exact: true })
    .selectOption("weekly");
  await panel
    .getByRole("combobox", { name: "Review weekday", exact: true })
    .selectOption("4");
  await panel.getByLabel("Include approved relevant memories").check();
  await expect(save).toBeDisabled();
  await panel.getByLabel(/I allow this selected local model/).check();
  await expect(save).toBeEnabled();
  await save.click();
  const commands = await page.evaluate(
    () =>
      (window as any).reviewCalls.find((c: any) => c.method === "batch").req
        .commands,
  );
  const plan = JSON.parse(commands[0].block.data.source);
  expect(plan.generative.categories).toEqual(["summary", "risks"]);
  expect(plan.generative.allowedActions).toEqual([
    "append_review",
    "rename_page",
    "set_task_status",
    "set_task_due",
  ]);
  expect(plan.generative.actionPageIds).toEqual([
    "019a1234-5678-7000-8000-000000000005",
  ]);
  expect(plan.includeMemory).toBe(true);
  expect(plan.schedule.frequency).toBe("weekly");
  expect(plan.schedule.weekday).toBe(4);
  expect(commands[1].kind).toBe("review.activate");
  expect(commands[1].enabled).toBe(true);
});
test("generated review previews changes and keeps the draft until explicit approval", async ({
  page,
}) => {
  const panel = await fixture(page),
    ready = panel
      .getByRole("article", { name: "Generated personal review" })
      .filter({ hasText: "ready ·" });
  await expect(
    ready.getByText("This is a draft. No changes have been applied."),
  ).toBeVisible();
  await ready.getByRole("button", { name: "Review proposed changes" }).click();
  const changes = ready.getByRole("region", {
    name: "Personal review proposed changes",
  });
  await expect(
    changes.getByText("Reviewed launch", { exact: true }),
  ).toBeVisible();
  await changes
    .getByRole("button", { name: "Keep draft without applying" })
    .click();
  expect(
    await page.evaluate(
      () =>
        (window as any).reviewCalls.filter(
          (c: any) => c.method === "review.apply",
        ).length,
    ),
  ).toBe(0);
  await ready.getByRole("button", { name: "Review proposed changes" }).click();
  await ready.getByRole("button", { name: "Approve these changes" }).click();
  await expect(
    panel
      .getByRole("article", { name: "Generated personal review" })
      .filter({ hasText: "accepted ·" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        (window as any).reviewCalls.filter(
          (c: any) => c.method === "review.apply",
        ).length,
    ),
  ).toBe(1);
});
test("queued analysis can be cancelled and explicitly retried without applying changes", async ({
  page,
}) => {
  const panel = await fixture(page);
  await panel.getByRole("button", { name: "Cancel analysis" }).click();
  await expect(
    panel.getByText("Cancelled by the owner; no changes applied."),
  ).toBeVisible();
  await panel
    .getByRole("button", { name: "Retry with current captured evidence" })
    .click();
  await expect(
    panel.getByRole("button", { name: "Cancel analysis" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        (window as any).reviewCalls.filter(
          (c: any) => c.method === "review.apply" || c.method === "batch",
        ).length,
    ),
  ).toBe(0);
});
