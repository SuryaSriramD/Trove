import { test, expect, type Page } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;

async function setup(page: Page) {
  page.on("pageerror", (error) => console.log("BROWSER ERROR", error.stack));
  await page.addInitScript(() => {
    const id = (n: number) =>
      `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
    const makePage = (n: number, title: string) => ({
      id: id(n),
      workspaceId: id(1),
      parentPageId: null,
      title,
      revision: "1",
      createdAt: "2026-10-04T00:00:00Z",
      updatedAt: "2026-10-04T00:00:00Z",
      deletedAt: null,
    });
    const query = {
      filter: null,
      sorts: [],
      visiblePropertyIds: [id(11)],
      groupByPropertyId: null,
      subgroupByPropertyId: null,
    };
    const state: any = {
      contractVersion: "1.0.0",
      workspaceId: id(1),
      scopeGrantId: id(2),
      snapshotCursor: "1",
      pages: [makePage(3, "Paged database"), makePage(4, "Unopened source")],
      blocks: [
        {
          id: id(9),
          pageId: id(4),
          parentBlockId: null,
          sortKey: "000000000001",
          type: "paragraph",
          schemaVersion: 1,
          data: { richText: [{ text: "LAZY PAGE BODY" }] },
          revision: "1",
        },
      ],
      collections: [
        {
          id: id(10),
          workspaceId: id(1),
          owningPageId: id(3),
          title: "Paged database",
          titlePropertyId: id(11),
          systemRole: "none",
          revision: "1",
        },
      ],
      properties: [
        {
          id: id(11),
          collectionId: id(10),
          name: "Name",
          type: "title",
          schemaVersion: 1,
          config: {},
          revision: "1",
        },
      ],
      records: [],
      views: [
        {
          id: id(12),
          collectionId: id(10),
          owningPageId: id(3),
          containerId: null,
          name: "Table",
          type: "table",
          schemaVersion: 1,
          query,
          config: { columnWidths: [] },
          revision: "1",
        },
        {
          id: id(13),
          collectionId: id(10),
          owningPageId: id(3),
          containerId: null,
          name: "Gallery",
          type: "gallery",
          schemaVersion: 1,
          query,
          config: { cardSize: "small", coverPropertyId: null },
          revision: "1",
        },
        {
          id: id(14),
          collectionId: id(10),
          owningPageId: id(3),
          containerId: null,
          name: "Complete list",
          type: "list",
          schemaVersion: 1,
          query,
          config: {},
          revision: "1",
        },
      ],
      files: [],
      meetings: [],
      taskFacets: [],
      containers: [],
      containerCollections: [],
      memories: [],
      projectScopes: [],
      transcriptSegments: [],
      importMappings: [],
    };
    for (let n = 1; n <= 450; n++) {
      state.pages.push(makePage(1000 + n, `Row ${String(n).padStart(3, "0")}`));
      state.records.push({
        id: id(2000 + n),
        collectionId: id(10),
        pageId: id(1000 + n),
        sortKey: String(n).padStart(12, "0"),
        revision: "1",
        values: [
          {
            propertyId: id(11),
            schemaVersion: 1,
            value: { pageId: id(1000 + n) },
          },
        ],
      });
    }
    const calls: any[] = [];
    (window as any).scalingCalls = calls;
    (window as any).runtime = { EventsOn: () => () => {} };
    (window as any).go = {
      main: {
        App: {
          Dispatch: async (method: string, raw: string) => {
            const request = JSON.parse(raw);
            calls.push({ method, ...request });
            const ok = (result: any) => JSON.stringify({ ok: true, result });
            if (method === "workspace.snapshot")
              return ok({ ...state, contentMode: "shell", blocks: [] });
            if (method === "ai.runs") return ok([]);
            if (method === "models.list") return ok({ packs: [] });
            if (method === "memory.list") return ok({ entries: [] });
            if (method === "readPage")
              return ok({
                page: state.pages.find((p: any) => p.id === request.pageId),
                blocks: state.blocks.filter(
                  (b: any) => b.pageId === request.pageId,
                ),
                snapshotCursor: state.snapshotCursor,
              });
            if (method === "readBlock")
              return ok({
                block: state.blocks.find((b: any) => b.id === request.blockId),
              });
            if (method === "links.backlinks")
              return ok({
                links:
                  request.pageId === id(3)
                    ? [{ pageId: id(4), blockId: id(9) }]
                    : [],
              });
            if (method === "pageDisplay.get")
              return ok({
                pageId: request.pageId,
                revision: "0",
                fullWidth: false,
                presentation: false,
              });
            if (method === "editor.templates") return ok({ templates: [] });
            if (method === "editor.references") return ok({ references: [] });
            if (method === "editor.lock")
              return ok({ locked: false, revision: "0" });
            if (method === "databaseP2")
              return ok({
                config: {
                  chart: {
                    groupPropertyId: null,
                    valuePropertyId: null,
                    aggregation: "count",
                    visualization: "bar",
                  },
                  form: { enabled: false, requiredPropertyIds: [] },
                  buttons: [],
                },
              });
            if (method === "databaseDerived")
              return ok({ locked: false, values: {}, structure: {} });
            if (method === "queryRecords") {
              const offset = Number(request.cursor ?? 0),
                records = state.records.slice(offset, offset + request.limit);
              return ok({
                recordIds: records.map((r: any) => r.id),
                records,
                snapshotCursor: state.snapshotCursor,
                ...(offset + request.limit < state.records.length
                  ? { nextCursor: String(offset + request.limit) }
                  : {}),
              });
            }
            if (method === "batch")
              return ok({
                revisions: [],
                eventCursor: "1",
                transactionId: id(99),
              });
            throw new Error(`Missing fixture service: ${method}`);
          },
        },
      },
    };
  });
  await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197");
}

test("shell loads one requested page body and retrieves backlinks independently", async ({
  page,
}) => {
  await setup(page);
  expect(
    await page.evaluate(
      () =>
        (window as any).scalingCalls.filter((c: any) => c.method === "readPage")
          .length,
    ),
  ).toBe(0);
  await page
    .getByRole("button", { name: /Unopened source/ })
    .first()
    .click();
  await expect(page.locator(".tiptap")).toContainText("LAZY PAGE BODY");
  expect(
    await page.evaluate(() =>
      (window as any).scalingCalls
        .filter((c: any) => c.method === "readPage")
        .map((c: any) => c.pageId),
    ),
  ).toEqual([id(4)]);
  await page
    .getByRole("button", { name: /Paged database/ })
    .first()
    .click();
  await expect(page.locator(".backlinks")).toContainText("Unopened source");
  await expect(page.locator(".tiptap")).not.toContainText("LAZY PAGE BODY");
});

test("table and gallery retain a single page with a small rendered window", async ({
  page,
}) => {
  await setup(page);
  await page
    .getByRole("button", { name: /Paged database/ })
    .first()
    .click();
  const database = page.getByRole("region", { name: "Database", exact: true });
  await expect(
    database.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
  expect(await database.locator("tr[data-record-id]").count()).toBeLessThan(35);
  expect(
    await page.evaluate(
      () =>
        (window as any).scalingCalls.filter((c: any) =>
          ["databaseDerived", "databaseChart"].includes(c.method),
        ).length,
    ),
  ).toBe(0);
  await database.locator(".db-virtual-viewport").evaluate((node) => {
    node.scrollTop = node.scrollHeight;
  });
  await expect(
    database.locator(`[data-record-id="${id(2200)}"]`),
  ).toBeVisible();
  expect(await database.locator("tr[data-record-id]").count()).toBeLessThan(35);
  await database
    .getByRole("button", { name: "Next records", exact: true })
    .click();
  await expect(
    database.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(
    database.locator(`[data-record-id="${id(2201)}"]`),
  ).toBeVisible();
  await expect(database.locator(`[data-record-id="${id(2001)}"]`)).toHaveCount(
    0,
  );
  await database
    .getByRole("button", { name: "Previous records", exact: true })
    .click();
  await expect(
    database.locator(`[data-record-id="${id(2001)}"]`),
  ).toBeVisible();
  await database.getByRole("tab", { name: "Gallery", exact: true }).click();
  await expect(database.locator(".db-gallery-card").first()).toBeVisible();
  expect(await database.locator(".db-gallery-card").count()).toBeLessThan(40);
  await database.getByRole("tab", { name: /Complete list/ }).click();
  await expect(
    database.getByText("450 records", { exact: true }),
  ).toBeVisible();
  expect(await database.locator(".db-list article").count()).toBe(450);
});
