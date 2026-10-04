import { test, expect } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const page = (n: number, title: string) => ({
  id: id(n),
  workspaceId: id(1),
  parentPageId: null,
  title,
  revision: "1",
  createdAt: "2026-09-30T10:00:00Z",
  updatedAt: "2026-09-30T10:00:00Z",
  deletedAt: null,
});
const query = {
  filter: null,
  sorts: [],
  visiblePropertyIds: [id(11), id(12), id(13)],
  groupByPropertyId: null,
  subgroupByPropertyId: null,
};
const fixture = {
  contractVersion: "1.0.0",
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    page(3, "Launch plan"),
    page(4, "Prepare prototype"),
    page(5, "Review scope"),
  ],
  blocks: [],
  collections: [
    {
      id: id(10),
      workspaceId: id(1),
      owningPageId: id(3),
      title: "Launch plan",
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
    {
      id: id(12),
      collectionId: id(10),
      name: "Status",
      type: "status",
      schemaVersion: 1,
      revision: "1",
      config: {
        options: [
          { id: id(20), label: "To do", group: "not_started" },
          { id: id(21), label: "Done", group: "done" },
        ],
        defaultOptionId: id(20),
        reopenOptionId: id(20),
        doneOptionId: id(21),
      },
    },
    {
      id: id(13),
      collectionId: id(10),
      name: "When",
      type: "date",
      schemaVersion: 1,
      config: {},
      revision: "1",
    },
  ],
  records: [
    {
      id: id(30),
      collectionId: id(10),
      pageId: id(4),
      revision: "1",
      sortKey: "000000000001",
      values: [
        { propertyId: id(11), schemaVersion: 1, value: { pageId: id(4) } },
        { propertyId: id(12), schemaVersion: 1, value: id(20) },
        {
          propertyId: id(13),
          schemaVersion: 1,
          value: {
            start: "2026-09-30",
            end: "2026-10-02",
            timezone: "Asia/Kolkata",
            hasTime: false,
          },
        },
      ],
    },
    {
      id: id(31),
      collectionId: id(10),
      pageId: id(5),
      revision: "1",
      sortKey: "000000000002",
      values: [
        { propertyId: id(11), schemaVersion: 1, value: { pageId: id(5) } },
      ],
    },
  ],
  views: ["table", "list", "board", "calendar"].map((type, i) => ({
    id: id(40 + i),
    collectionId: id(10),
    containerId: null,
    owningPageId: id(3),
    name: type[0].toUpperCase() + type.slice(1),
    type,
    schemaVersion: 1,
    revision: "1",
    query,
    config: [
      { columnWidths: [] },
      { showPageIcon: true },
      { groupPropertyId: id(12), hideEmptyGroups: false },
      { datePropertyId: id(13), weekStartsOn: 1 },
    ][i],
  })),
  containers: [],
  containerCollections: [],
  files: [],
  meetings: [],
  memories: [],
  projectScopes: [],
  transcriptSegments: [],
  importMappings: [],
  taskFacets: [],
};
test.beforeEach(async ({ page }) => {
  await page.addInitScript(
    ({ fixture }) => {
      let state =
        JSON.parse(localStorage.getItem("databaseFixture") ?? "null") ??
        fixture;
      let count = 200;
      const commands: any[] = [];
      const requests: any[] = [];
      const fresh = () =>
        `019a1234-5678-7000-8000-${String(count++).padStart(12, "0")}`;
      (window as any).dbState = () => state;
      (window as any).dbCommands = commands;
      (window as any).dbRequests = requests;
      (window as any).runtime = { EventsOn: () => () => {} };
      (window as any).go = {
        main: {
          App: {
            Dispatch: async (method: string, raw: string) => {
              const req = JSON.parse(raw);
              requests.push({ method, ...req });
              const ok = (result: any) => JSON.stringify({ ok: true, result });
              if (method === "workspace.snapshot") return ok(state);
              if (method === "pageDisplay.get") return ok({pageId:req.pageId,fullWidth:false,presentation:false,revision:"1"});
              if (method === "editor.lock") return ok({locked:false,revision:"0"});
              if (method === "editor.templates") return ok({templates:[]});
              if (method === "editor.references") return ok({references:[]});
              if (method === "ai.runs") return ok([]);
              if (method === "memory.list") return ok({entries:[]});
              state.p2 ??= {
                chart: {
                  groupPropertyId: null,
                  valuePropertyId: null,
                  aggregation: "count",
                  visualization: "bar",
                },
                form: { enabled: false, requiredPropertyIds: [] },
                buttons: [],
              };
              if (method === "databaseP2") return ok({ config: state.p2 });
              if (method === "databaseChart")
                return ok({
                  collectionId: state.collections[0].id,
                  revision: state.collections[0].revision,
                  config: state.p2.chart,
                  recordCount: state.records.length,
                  groups: [
                    {
                      key: "all",
                      label: "All records",
                      decimal: String(state.records.length),
                      exactFraction: String(state.records.length),
                      recordIds: state.records.map((r: any) => r.id),
                      numericCount: 0,
                    },
                  ],
                });
              if (method === "databaseDerived")
                return ok({ locked: false, values: {}, structure: {} });
              if (method === "allocateIds")
                return ok({ ids: Array.from({ length: req.count }, fresh) });
              if (method === "queryRecords")
                return ok({
                  recordIds: state.records.map((r: any) => r.id),
                  records: state.records,
                  snapshotCursor: state.snapshotCursor,
                });
              if (method !== "batch") throw Error(method);
              const changes = new Set<any>();
              for (const c of req.commands) {
                commands.push(c);
                if (c.kind === "database.p2Configure") {
                  state.p2 = c.config;
                  changes.add(state.collections[0]);
                }
                if (c.kind === "database.rowButtonRun") {
                  const b = state.p2.buttons.find(
                    (b: any) => b.id === c.buttonId,
                  );
                  if (b.confirmationRequired && !c.confirmed)
                    throw Error("Confirmation required");
                  const r = state.records.find((r: any) => r.id === c.recordId);
                  r.values = r.values.filter(
                    (v: any) => v.propertyId !== b.propertyId,
                  );
                  r.values.push({
                    propertyId: b.propertyId,
                    schemaVersion: 1,
                    value: b.value.value,
                  });
                  changes.add(r);
                  changes.add(state.pages.find((p: any) => p.id === r.pageId));
                  changes.add(state.collections[0]);
                }
                if (c.kind === "record.setValue") {
                  const r = state.records.find((r: any) => r.id === c.recordId);
                  const old = r.values.find(
                    (v: any) => v.propertyId === c.propertyId,
                  );
                  if (old) old.value = c.value.value;
                  else
                    r.values.push({
                      propertyId: c.propertyId,
                      schemaVersion: 1,
                      value: c.value.value,
                    });
                  changes.add(r);
                  changes.add(state.pages.find((p: any) => p.id === r.pageId));
                  changes.add(
                    state.collections.find(
                      (co: any) => co.id === r.collectionId,
                    ),
                  );
                }
                if (c.kind === "page.update") {
                  const p = state.pages.find((p: any) => p.id === c.pageId);
                  p.title = c.title;
                  changes.add(p);
                  const r = state.records.find((r: any) => r.pageId === p.id);
                  if (r) {
                    changes.add(r);
                    changes.add(state.collections[0]);
                  }
                }
                if (
                  c.kind === "record.create" ||
                  c.kind === "database.formSubmit"
                ) {
                  state.pages.push({
                    id: c.pageId,
                    workspaceId: state.workspaceId,
                    title: c.title,
                    parentPageId: state.collections[0].owningPageId,
                    revision: "0",
                    createdAt: "2026-09-30T10:00:00Z",
                    updatedAt: "2026-09-30T10:00:00Z",
                    deletedAt: null,
                  });
                  state.records.push({
                    id: c.recordId,
                    collectionId: c.collectionId,
                    pageId: c.pageId,
                    sortKey: "000000000003",
                    revision: "0",
                    values: [
                      {
                        propertyId: state.collections[0].titlePropertyId,
                        schemaVersion: 1,
                        value: { pageId: c.pageId },
                      },
                    ],
                  });
                  if (c.kind === "database.formSubmit") {
                    for (const v of c.values)
                      state.records.at(-1).values.push({
                        propertyId: v.propertyId,
                        schemaVersion: 1,
                        value: v.value.value,
                      });
                  }
                  changes.add(state.pages.at(-1));
                  changes.add(state.records.at(-1));
                  changes.add(state.collections[0]);
                }
                if (c.kind === "view.save") {
                  const v = state.views.find((v: any) => v.id === c.viewId);
                  Object.assign(v, { ...c, id: c.viewId });
                  delete v.kind;
                  changes.add(v);
                  changes.add(
                    state.pages.find((p: any) => p.id === c.owningPageId),
                  );
                }
                if (c.kind === "task.promote") {
                  state.taskFacets.push({
                    recordId: c.recordId,
                    statusPropertyId: c.statusPropertyId,
                    duePropertyId: null,
                  });
                  changes.add(
                    state.records.find((r: any) => r.id === c.recordId),
                  );
                }
              }
              const revisions = [...changes].map((o) => {
                const before = o.revision;
                o.revision = String(Number(o.revision) + 1);
                return {
                  target: {
                    entityId: o.id,
                    entityType: state.pages.includes(o)
                      ? "page"
                      : state.records.includes(o)
                        ? "record"
                        : state.collections.includes(o)
                          ? "collection"
                          : "view",
                  },
                  before,
                  after: o.revision,
                  deleted: false,
                };
              });
              state.snapshotCursor = String(Number(state.snapshotCursor) + 1);
              localStorage.setItem("databaseFixture", JSON.stringify(state));
              return ok({
                requestId: req.requestId,
                transactionId: fresh(),
                eventCursor: state.snapshotCursor,
                revisions,
                replayed: false,
              });
            },
          },
        },
      };
    },
    { fixture },
  );
  await page.goto(
    (globalThis as any).process?.env?.TROVE_TEST_URL ?? "http://127.0.0.1:4173",
  );
  await page
    .getByRole("navigation", { name: "Workspace", exact: true })
    .getByRole("button", { name: /Pages$/ })
    .click();
  await expect(page.locator(".db-table tbody tr")).toHaveCount(2);
});
test("all four views retain canonical record identities; board and calendar call guarded record mutations", async ({
  page,
}) => {
  const database = page.getByRole("region", { name: "Database", exact: true });
  for (const label of ["Table", "List", "Board"]) {
    await database.getByRole("tab", { name: new RegExp(label + "$") }).click();
    await expect(database.locator("[data-record-id]").first()).toBeVisible();
    expect(
      await database
        .locator("[data-record-id]")
        .evaluateAll((nodes) =>
          [
            ...new Set(nodes.map((n) => n.getAttribute("data-record-id"))),
          ].sort(),
        ),
    ).toEqual([id(30), id(31)]);
  }
  await page
    .getByLabel("Move Prepare prototype to column")
    .selectOption(id(21));
  await expect(
    page
      .locator(".db-board-column")
      .filter({
        has: page.getByRole("heading", { name: "Done", exact: false }),
      })
      .getByLabel("Title for Prepare prototype"),
  ).toBeVisible();
  const call = await page.evaluate(() =>
    (window as any).dbRequests.filter((r: any) => r.method === "batch").at(-1),
  );
  expect(call.commands[0].kind).toBe("record.setValue");
  expect(call.commands[0].recordId).toBe(id(30));
  expect(
    call.mutation.expectedRevisions.map((g: any) => g.target.entityType).sort(),
  ).toEqual(["collection", "page", "property", "record"]);
  await database.getByRole("tab", { name: /Calendar$/ }).click();
  await page.getByLabel("Calendar month").fill("2026-09");
  await page
    .getByLabel("Reschedule Prepare prototype")
    .first()
    .fill("2026-10-05");
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as any)
            .dbState()
            .records[0].values.find((v: any) =>
              v.propertyId.endsWith("000000000013"),
            ).value.start,
      ),
    )
    .toBe("2026-10-05");
  expect(
    await page.evaluate(
      () =>
        (window as any)
          .dbState()
          .records[0].values.find((v: any) =>
            v.propertyId.endsWith("000000000013"),
          ).value.end,
    ),
  ).toBe("2026-10-07");
  await page.getByLabel("Calendar month").fill("2026-10");
  await page.screenshot({
    path: test.info().outputPath("phase1-database-calendar.png"),
    fullPage: true,
  });
  await page.reload();
  await page
    .getByRole("navigation", { name: "Workspace", exact: true })
    .getByRole("button", { name: /Pages$/ })
    .click();
  await expect(page.locator(".db-table tbody tr")).toHaveCount(2);
});

test("linked placements open their own view and retain multiple local tabs after restart", async ({page}) => {
  await page.evaluate(() => {
    const state=(window as any).dbState();
    const id=(n:number)=>`019a1234-5678-7000-8000-${String(n).padStart(12,'0')}`;
    state.pages.push({...state.pages[0],id:id(60),title:'Linked project'}, {...state.pages[0],id:id(61),title:'Linked archive'});
    for (const [n,parent,name,visible] of [[62,60,'Project titles',[11]],[63,60,'Project status',[11,12]],[64,61,'Archive dates',[11,13]]] as const) {
      const view=structuredClone(state.views[0]);
      Object.assign(view,{id:id(n),owningPageId:id(parent),name,query:{...view.query,visiblePropertyIds:visible.map(id)}});
      state.views.push(view);
    }
    localStorage.setItem('databaseFixture',JSON.stringify(state));
  });
  await page.reload();
  await page.locator('.tree-item').filter({hasText:'Linked project'}).click();
  const database=page.getByRole('region',{name:'Database',exact:true});
  await expect(database.getByRole('tab',{name:/Project titles$/})).toHaveAttribute('aria-selected','true');
  await expect(database.getByRole('tab')).toHaveCount(2);
  await expect(database.locator('.db-table thead th')).toHaveCount(2);
  await expect.poll(()=>page.evaluate(()=>(window as any).dbRequests.filter((r:any)=>r.method==='queryRecords').at(-1)?.viewId)).toBe(id(62));
  await database.getByRole('tab',{name:/Project status$/}).click();
  await expect(database.locator('.db-table thead th')).toHaveCount(3);
  await expect.poll(()=>page.evaluate(()=>(window as any).dbRequests.filter((r:any)=>r.method==='queryRecords').at(-1)?.viewId)).toBe(id(63));
  await page.locator('.tree-item').filter({hasText:'Linked archive'}).click();
  await expect(database.getByRole('tab',{name:/Archive dates$/})).toHaveAttribute('aria-selected','true');
  await expect(database.getByRole('tab')).toHaveCount(1);
  await expect(database.locator('.db-table thead')).toContainText('When');
  await page.reload();
  await page.locator('.tree-item').filter({hasText:'Linked archive'}).click();
  await expect(database.getByRole('tab',{name:/Archive dates$/})).toHaveAttribute('aria-selected','true');
  await expect.poll(()=>page.evaluate(()=>(window as any).dbRequests.filter((r:any)=>r.method==='queryRecords').at(-1)?.viewId)).toBe(id(64));
});
test("saved view filter groups, sorting, visibility, rename and move serialize without record duplication", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "View settings", exact: true })
    .click();
  await page.getByLabel("View name", { exact: true }).fill("Priority work");
  await page.getByLabel("View location", { exact: true }).selectOption(id(5));
  await page
    .getByRole("button", { name: "＋ Add filter", exact: true })
    .click();
  await page
    .getByRole("button", { name: "＋ Group", exact: true })
    .first()
    .click();
  await page.getByLabel("Combine filters").last().selectOption("or");
  await page.getByRole("button", { name: "＋ Sort", exact: true }).click();
  await page.getByLabel("Sort property 1").selectOption(id(13));
  await page.getByLabel("Group records", { exact: true }).selectOption(id(12));
  await page
    .getByLabel("Subgroup records", { exact: true })
    .selectOption(id(13));
  await page.getByRole("button", { name: "Save view", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Priority work" })).toBeVisible();
  const state = await page.evaluate(() => (window as any).dbState());
  expect(state.records.map((r: any) => r.id)).toEqual([id(30), id(31)]);
  expect(state.views[0].owningPageId).toBe(id(5));
  expect(state.views[0].query.filter.terms[1].op).toBe("or");
  expect(state.views[0].query.sorts[0].propertyId).toBe(id(13));
});
test("table creates record and edits its body title through the canonical page service", async ({
  page,
}) => {
  await page
    .getByLabel("New record title", { exact: true })
    .fill("A new record");
  await page.getByRole("button", { name: "Add record", exact: true }).click();
  await expect(page.locator(".db-table tbody tr")).toHaveCount(3);
  const field = page.getByLabel("Title for A new record");
  await field.fill("Renamed record");
  await field.press("Enter");
  await expect(page.getByLabel("Title for Renamed record")).toBeVisible();
  expect(
    (await page.evaluate(() => (window as any).dbCommands)).at(-1).kind,
  ).toBe("page.update");
});

test("table property order and widths persist through view save and restart", async ({page}) => {
  await page.getByRole("button", {name:"View settings",exact:true}).click();
  await page.getByRole("button", {name:"Move When earlier",exact:true}).click();
  await page.getByRole("button", {name:"Move When earlier",exact:true}).click();
  await page.getByLabel("When column width",{exact:true}).fill("");
  await page.getByLabel("When column width",{exact:true}).pressSequentially("310");
  await page.getByRole("button", {name:"Save view",exact:true}).click();
  await expect(page.locator(".db-table thead th").first()).toHaveText("When");
  await expect.poll(async()=>Math.round((await page.locator(".db-table thead th").first().boundingBox())!.width)).toBe(310);
  const state=await page.evaluate(()=>(window as any).dbState());
  expect(state.views[0].query.visiblePropertyIds).toEqual([id(13),id(11),id(12)]);
  expect(state.views[0].config.columnWidths).toContainEqual({propertyId:id(13),pixels:310});
  await page.reload();
  await page.getByRole("navigation",{name:"Workspace",exact:true}).getByRole("button",{name:/Pages$/}).click();
  await expect(page.locator(".db-table thead th").first()).toHaveText("When");
  await expect.poll(async()=>Math.round((await page.locator(".db-table thead th").first().boundingBox())!.width)).toBe(310);
});

test("calendar resizes a range using its end field and drag handle with guarded writes", async ({page}) => {
  await page.getByRole("tab",{name:/Calendar$/}).click();
  await page.getByLabel("Calendar month").fill("2026-10");
  await page.getByLabel("Range end for Prepare prototype",{exact:true}).first().fill("2026-10-06");
  const read=()=>page.evaluate(()=>(window as any).dbState().records[0].values.find((v:any)=>v.propertyId.endsWith("000000000013")).value);
  await expect.poll(read).toEqual({start:"2026-09-30",end:"2026-10-06",timezone:"Asia/Kolkata",hasTime:false});
  await page.getByRole("button",{name:"Resize end of Prepare prototype",exact:true}).dragTo(page.getByRole("gridcell",{name:"2026-10-09",exact:true}));
  await expect.poll(read).toEqual({start:"2026-09-30",end:"2026-10-09",timezone:"Asia/Kolkata",hasTime:false});
  const call=await page.evaluate(()=>(window as any).dbRequests.filter((r:any)=>r.method==="batch").at(-1));
  expect(call.commands[0].kind).toBe("record.setValue");
  expect(call.mutation.expectedRevisions.map((g:any)=>g.target.entityType).sort()).toEqual(["collection","page","property","record"]);
});

test("P2 chart traces rows and persists display configuration", async ({
  page,
}) => {
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await expect(
    page.getByRole("img", { name: "Bar chart; exact values in table below" }),
  ).toBeVisible();
  await page
    .locator(".p2-chart")
    .getByText("2 records", { exact: true })
    .click();
  await expect(
    page
      .locator(".p2-chart")
      .getByRole("button", { name: "Prepare prototype", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Chart display").selectOption("donut");
  await page.getByRole("button", { name: "Save chart and settings" }).click();
  await expect(
    page.getByRole("img", { name: "Donut chart; exact values in table below" }),
  ).toBeVisible();
  await page.reload();
  await page
    .getByRole("navigation", { name: "Workspace", exact: true })
    .getByRole("button", { name: /Pages$/ })
    .click();
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await expect(page.getByLabel("Chart display")).toHaveValue("donut");
});
test("P2 local form requires configured field then sends one guarded submission", async ({
  page,
}) => {
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page.getByLabel("Enable a local submission form").check();
  await page
    .getByRole("group", { name: "Required fields (title is always required)" })
    .getByLabel("Status", { exact: true })
    .check();
  await page.getByRole("button", { name: "Save form settings" }).click();
  await page.getByLabel("Form title", { exact: true }).fill("Form submission");
  await page.getByRole("button", { name: "Submit local form" }).click();
  expect(
    await page.evaluate(
      () =>
        (window as any).dbCommands.filter(
          (c: any) => c.kind === "database.formSubmit",
        ).length,
    ),
  ).toBe(0);
  await page.getByLabel("Form Status", { exact: true }).selectOption(id(21));
  await page.getByRole("button", { name: "Submit local form" }).click();
  await expect(page.getByLabel("Title for Form submission")).toBeVisible();
  const call = await page.evaluate(() =>
    (window as any).dbRequests.filter((r: any) => r.method === "batch").at(-1),
  );
  expect(call.commands[0].kind).toBe("database.formSubmit");
  expect(
    call.mutation.expectedRevisions.map((g: any) => g.target.entityType).sort(),
  ).toEqual(["collection", "page", "property", "record"]);
});
test("P2 row buttons cancel without mutation then confirm the selected row only", async ({
  page,
}) => {
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page.getByLabel("Row button label").fill("Mark done");
  await page.getByLabel("Row button property").selectOption(id(12));
  await page.getByLabel("Row button value").selectOption(id(21));
  await page
    .getByRole("button", { name: "Add row button", exact: true })
    .click();
  const row = page.locator(`[data-record-id="${id(30)}"]`);
  await row.getByRole("button", { name: "Mark done", exact: true }).click();
  await expect(
    row.getByRole("alertdialog", { name: "Confirm row action" }),
  ).toContainText("Prepare prototype");
  await page.getByLabel("Chart display").selectOption("donut");
  await page.getByRole("button", { name: "Save chart and settings" }).click();
  await expect(
    row.getByRole("alertdialog", { name: "Confirm row action" }),
  ).toHaveCount(0);
  await row.getByRole("button", { name: "Mark done", exact: true }).click();
  await row.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(
    await page.evaluate(
      () =>
        (window as any).dbCommands.filter(
          (c: any) => c.kind === "database.rowButtonRun",
        ).length,
    ),
  ).toBe(0);
  await row.getByRole("button", { name: "Mark done", exact: true }).click();
  await row
    .getByRole("button", { name: "Confirm Mark done", exact: true })
    .click();
  await expect(
    page.getByLabel("Status for Prepare prototype", { exact: true }),
  ).toHaveValue(id(21));
  const call = await page.evaluate(() =>
    (window as any).dbRequests.filter((r: any) => r.method === "batch").at(-1),
  );
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "/Volumes/HD-B1/HD-Projects/Trove/evidence/phase6/database-ui.png",
    fullPage: true,
  });
  expect(call.commands[0]).toMatchObject({
    kind: "database.rowButtonRun",
    recordId: id(30),
    confirmed: true,
  });
  expect(
    call.mutation.expectedRevisions.map((g: any) => g.target.entityType).sort(),
  ).toEqual(["collection", "page", "property", "record"]);
});
