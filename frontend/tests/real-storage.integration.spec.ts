import { observeLineEdgeKeys, lineEdgeObservations } from "./keyboard-observation";
import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
const evidence = process.env.TROVE_REAL_UI_EVIDENCE!;
const stage = process.env.TROVE_REAL_UI_STAGE!;
const fixture = JSON.parse(fs.readFileSync(path.join(evidence, "fixture.json"), "utf8"));
const read = (name: string) => JSON.parse(fs.readFileSync(path.join(evidence, name), "utf8"));
const write = (name: string, value: unknown) => fs.writeFileSync(path.join(evidence, name), JSON.stringify(value, null, 2) + "\n", { flag: "wx" });

// These are observations through the unchanged production bridge, never seed writes.
async function snapshot(page: Page): Promise<any> {
  return page.evaluate(async () => {
    const response = JSON.parse(await window.go!.main.App.Dispatch("workspace.snapshot", JSON.stringify({ contentMode: "full" })));
    if (!response.ok) throw new Error(JSON.stringify(response.error));
    return response.result;
  });
}
function richProjection(s: any) {
  const pages = s.pages.filter((p: any) => p.title === fixture.pageTitle && !p.deletedAt);
  expect(pages).toHaveLength(1);
  const page = pages[0];
  const blocks = s.blocks.filter((b: any) => b.pageId === page.id && !b.deletedAt).sort((a: any, b: any) => a.sortKey.localeCompare(b.sortKey));
  expect(blocks).toHaveLength(1);
  expect(blocks[0].type).toBe("paragraph");
  expect(blocks[0].data.richText.map((run: any) => run.text).join("")).toBe(fixture.body);
  expect(blocks[0].data.richText.every((run: any) => run.marks?.includes("bold"))).toBe(true);
  return { workspaceId: s.workspaceId, page, blocks };
}
function taskProjection(s: any) {
  const pages = s.pages.filter((p: any) => p.title === fixture.taskTitle && !p.deletedAt);
  expect(pages).toHaveLength(1);
  const page = pages[0];
  const records = s.records.filter((r: any) => r.pageId === page.id);
  expect(records).toHaveLength(1);
  const record = records[0];
  const facets = s.taskFacets.filter((f: any) => f.recordId === record.id);
  expect(facets).toHaveLength(1);
  const facet = facets[0];
  const property = s.properties.find((p: any) => p.id === facet.statusPropertyId);
  const value = record.values.find((v: any) => v.propertyId === property.id).value;
  expect(property.config.options.find((o: any) => o.id === value).group).toBe("done");
  return { workspaceId: s.workspaceId, page, record, facet, statusProperty: property };
}
async function login(page: Page) {
  const unexpected: string[] = [];
  await page.route("**/*", route => {
    const url = new URL(route.request().url());
    if (url.origin === process.env.TROVE_REAL_UI_URL) return route.continue();
    unexpected.push(url.origin);
    return route.abort();
  });
  await page.goto("/");
  await page.getByLabel("Owner token").fill(fs.readFileSync(process.env.TROVE_REAL_UI_TOKEN_FILE!, "utf8").trim());
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page.getByRole("navigation", { name: "Workspace", exact: true })).toBeVisible();
  await expect(page.getByText("HOST", { exact: true })).toBeVisible();
  expect(unexpected).toEqual([]);
}
test.beforeEach(async ({ page }) => login(page));
test.afterEach(async ({ page }, info) => {
  const observation = await page.evaluate(() => {
    const element = document.querySelector(".tiptap[contenteditable=true]") as HTMLElement & { editor?: any } | null;
    const selection = window.getSelection();
    return { focusedElement: document.activeElement?.getAttribute("aria-label"),
      editorFocused: Boolean(element && document.activeElement === element),
      modelBold: element?.editor?.isActive("bold"), model: element?.editor?.getJSON(),
      storedMarks: element?.editor?.state.storedMarks?.map((mark: any) => mark.type.name),
      renderedBold: document.querySelector('[aria-label="bold"]')?.getAttribute("aria-pressed"),
      domCaret: { text: selection?.anchorNode?.textContent, offset: selection?.anchorOffset,
        insideContent: Boolean(selection?.anchorNode && document.querySelector(".tiptap .block-text")?.contains(selection.anchorNode)) } };
  }).catch(error => ({ error: String(error) }));
  write(`${stage}-${info.title}-observation.json`, { status: info.status, observation });
});

test(stage === "create" ? "real-rich-create" : "real-rich-reopen", async ({ page }) => {
  if (stage === "create") {
    await page.getByRole("button", { name: "Create page", exact: true }).click();
    await page.getByLabel("Page title", { exact: true }).fill(fixture.pageTitle);
    await page.getByLabel("Page title", { exact: true }).press("Enter");
    await expect(page.getByLabel("Page title", { exact: true })).toBeEnabled();
    const editor = page.locator(".tiptap[contenteditable=true]");
    await editor.locator(".block-text").click({ position: { x: 8, y: 8 } });
    await expect(editor).toBeFocused();
    await expect.poll(() => page.evaluate(() => {
      const selection = window.getSelection();
      return Boolean(selection?.anchorNode && document.querySelector(".tiptap .block-text")?.contains(selection.anchorNode));
    })).toBe(true);
    await page.getByRole("button", { name: "bold", exact: true }).click();
    await expect(editor).toBeFocused();
    await expect(page.getByRole("button", { name: "bold", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect.poll(() => page.evaluate(() => {
      const selection = window.getSelection();
      return Boolean(selection?.anchorNode && document.querySelector(".tiptap .block-text")?.contains(selection.anchorNode));
    })).toBe(true);
    await page.keyboard.insertText(fixture.body);
    await expect(editor.locator(".block-text")).toHaveText(fixture.body);
    await expect(editor.locator("strong")).toHaveText(fixture.body);
    await page.getByRole("button", { name: "Save page", exact: true }).click();
    await expect.poll(async () => {
      try { richProjection(await snapshot(page)); return true; } catch { return false; }
    }).toBe(true);
    write("create-rich.json", richProjection(await snapshot(page)));
    await page.getByRole("navigation", { name: "Workspace", exact: true }).getByRole("button", { name: /Home/ }).click();
  }
  await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: `▤ ${fixture.pageTitle}`, exact: true }).click();
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(fixture.pageTitle);
  await expect(page.locator(".tiptap[contenteditable=true] strong")).toHaveText(fixture.body);
  const current = richProjection(await snapshot(page));
  expect(current).toEqual(read("create-rich.json"));
  write(`${stage}-rich-result.json`, { passed: true, canonical: current });
});

test(stage === "create" ? "real-task-create" : "real-task-reopen", async ({ page }) => {
  await page.getByRole("navigation", { name: "Workspace", exact: true }).getByRole("button", { name: /Tasks/ }).click();
  if (stage === "create") {
    await page.getByLabel("New task title").fill(fixture.initialTaskTitle);
    await page.getByRole("button", { name: "Add task", exact: true }).click();
    await page.getByLabel(`Task title for ${fixture.initialTaskTitle}`, { exact: true }).fill(fixture.taskTitle);
    await page.getByLabel(`Task title for ${fixture.initialTaskTitle}`, { exact: true }).press("Enter");
    await page.getByRole("checkbox", { name: `Complete ${fixture.taskTitle}`, exact: true }).click();
    await expect.poll(async () => {
      try { taskProjection(await snapshot(page)); return true; } catch { return false; }
    }).toBe(true);
    write("create-task.json", taskProjection(await snapshot(page)));
  }
  await expect(page.getByLabel(`Task title for ${fixture.taskTitle}`, { exact: true })).toHaveValue(fixture.taskTitle);
  await expect(page.getByRole("checkbox", { name: `Complete ${fixture.taskTitle}`, exact: true })).toBeChecked();
  await page.getByRole("navigation", { name: "Workspace", exact: true }).getByRole("button", { name: /Home/ }).click();
  await expect(page.getByRole("region", { name: "Tasks on Home" }).getByRole("checkbox", { name: `Complete ${fixture.taskTitle}`, exact: true })).toBeChecked();
  const current = taskProjection(await snapshot(page));
  expect(current).toEqual(read("create-task.json"));
  write(`${stage}-task-result.json`, { passed: true, canonical: current });
});

// Additional workflows remain independent of the original rich-page/task cases.
function recordProjection(s: any, title: string) {
  const pages = s.pages.filter((p: any) => p.title === title && !p.deletedAt);
  expect(pages).toHaveLength(1);
  const records = s.records.filter((r: any) => r.pageId === pages[0].id);
  expect(records).toHaveLength(1);
  const record = records[0];
  const facets = s.taskFacets.filter((f: any) => f.recordId === record.id);
  expect(facets.length).toBeLessThanOrEqual(1);
  return { workspaceId: s.workspaceId, page: pages[0], record, facet: facets[0] ?? null,
    properties: s.properties.filter((p: any) => p.collectionId === record.collectionId).sort((a: any, b: any) => a.id.localeCompare(b.id)) };
}
function identity(r: ReturnType<typeof recordProjection>) {
  return { workspaceId: r.workspaceId, pageId: r.page.id, recordId: r.record.id, collectionId: r.record.collectionId,
    propertyIds: r.properties.map((p: any) => p.id) };
}
function property(r: ReturnType<typeof recordProjection>, name: string) {
  const matches = r.properties.filter((p: any) => p.name === name);
  expect(matches).toHaveLength(1);
  return matches[0];
}
function dateValue(r: ReturnType<typeof recordProjection>, name: string) {
  return r.record.values.find((v: any) => v.propertyId === property(r, name).id)?.value ?? null;
}
const date = (start: string, timezone = fixture.timezone) => ({ start, end: null, timezone, hasTime: false });
async function observe(page: Page, method: "page.freshness" | "recordLayouts.get" | "operations.list" | "databaseP2" | "databaseChart" | "imports.list" | "imports.plan" | "pageDisplay.get" | "databaseDerived" | "skills.list" | "ai.runs" | "review.list", args: Record<string, unknown>) {
  const s = await snapshot(page);
  return page.evaluate(async ({ method, args, workspaceId }) => {
    const a = crypto.getRandomValues(new Uint8Array(16)), t = BigInt(Date.now());
    for (let i = 0; i < 6; i++) a[5 - i] = Number((t >> BigInt(i * 8)) & 255n);
    a[6] = (a[6] & 15) | 112; a[8] = (a[8] & 63) | 128;
    const h = Array.from(a, b => b.toString(16).padStart(2, "0")).join("");
    const requestId = `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
    const response = JSON.parse(await window.go!.main.App.Dispatch(method, JSON.stringify({ contractVersion: 1, requestId, workspaceId, ...args })));
    if (!response.ok) throw Error(JSON.stringify(response.error));
    return response.result;
  }, { method, args, workspaceId: s.workspaceId });
}
async function surface(page: Page, name: "Tasks" | "Home" | "Pages" | "Import" | "Skills" | "Intelligence") {
  await page.getByRole("navigation", { name: "Workspace", exact: true }).getByRole("button", { name: new RegExp(name) }).click();
}
function dateControl(page: Page, label: string) {
  return page.locator("details.db-date-editor").filter({ has: page.getByLabel(`${label} start`, { exact: true }) });
}
async function setDate(page: Page, label: string, start: string, timezone = fixture.timezone) {
  const control = dateControl(page, label);
  if ((await control.getAttribute("open")) === null) await control.locator("summary").click();
  await control.getByLabel(`${label} start`, { exact: true }).fill(start);
  await control.getByLabel(`${label} timezone`, { exact: true }).fill(timezone);
  await control.getByRole("button", { name: "Save date", exact: true }).click();
}
async function expectDate(page: Page, title: string, name: string, start: string) {
  await expect.poll(async () => dateValue(recordProjection(await snapshot(page), title), name)).toEqual(date(start));
}
async function database(page: Page, title: string) {
  await surface(page, "Pages");
  await page.getByLabel("Choose database", { exact: true }).selectOption({ label: title });
}
async function createRecord(page: Page, databaseTitle: string, title: string) {
  await surface(page, "Pages");
  page.once("dialog", dialog => dialog.accept(databaseTitle));
  await page.getByRole("button", { name: "＋ New database", exact: true }).click();
  await expect(page.getByLabel("Choose database", { exact: true }).locator("option:checked")).toHaveText(databaseTitle);
  await page.getByLabel("New record title", { exact: true }).fill(title);
  await page.getByRole("button", { name: "Add record", exact: true }).click();
  await expect(page.getByLabel(`Title for ${title}`, { exact: true })).toBeVisible();
}
async function openRecord(page: Page, title: string) {
  await page.getByRole("button", { name: `Open ${title} page`, exact: true }).first().click();
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(title);
  await expect(page.getByRole("region", { name: "Record properties", exact: true })).toBeVisible();
}

test(`real-due-${stage}`, async ({ page }) => {
  await surface(page, "Tasks");
  if (stage === "create") {
    await page.getByLabel("New task title", { exact: true }).fill(fixture.dueTaskTitle);
    await page.getByRole("button", { name: "Add task", exact: true }).click();
    await expect(page.getByLabel(`Task title for ${fixture.dueTaskTitle}`, { exact: true })).toBeVisible();
    const before = recordProjection(await snapshot(page), fixture.dueTaskTitle);
    expect(before.facet.duePropertyId).toBe(property(before, "Due").id);
    await setDate(page, `Due for ${fixture.dueTaskTitle}`, fixture.dueDates[0]);
    await expectDate(page, fixture.dueTaskTitle, "Due", fixture.dueDates[0]);
    await surface(page, "Home");
    await expect(dateControl(page, `Due for ${fixture.dueTaskTitle}`).locator("summary")).toContainText(fixture.dueDates[0]);
    await setDate(page, `Due for ${fixture.dueTaskTitle}`, fixture.dueDates[1]);
    await expectDate(page, fixture.dueTaskTitle, "Due", fixture.dueDates[1]);
    await page.getByRole("button", { name: `Open ${fixture.dueTaskTitle} task`, exact: true }).click();
    await expect(dateControl(page, "Due on record page").locator("summary")).toContainText(fixture.dueDates[1]);
    await setDate(page, "Due on record page", fixture.dueDates[2]);
    await expectDate(page, fixture.dueTaskTitle, "Due", fixture.dueDates[2]);
    const valid = recordProjection(await snapshot(page), fixture.dueTaskTitle);
    expect(identity(valid)).toEqual(identity(before));
    await setDate(page, "Due on record page", fixture.dueDates[2], "Invalid/Timezone");
    await expect(page.getByRole("alert").filter({ hasText: "Unknown date timezone" })).toBeVisible();
    expect(recordProjection(await snapshot(page), fixture.dueTaskTitle)).toEqual(valid);
    write("create-due.json", { before, canonical: valid, invalidTimezoneRejected: true });
    await surface(page, "Tasks");
  }
  await expect(dateControl(page, `Due for ${fixture.dueTaskTitle}`).locator("summary")).toContainText(fixture.dueDates[2]);
  const current = recordProjection(await snapshot(page), fixture.dueTaskTitle);
  expect(current).toEqual(read("create-due.json").canonical);
  await surface(page, "Home");
  await expect(dateControl(page, `Due for ${fixture.dueTaskTitle}`).locator("summary")).toContainText(fixture.dueDates[2]);
  await page.getByRole("button", { name: `Open ${fixture.dueTaskTitle} task`, exact: true }).click();
  await expect(dateControl(page, "Due on record page").locator("summary")).toContainText(fixture.dueDates[2]);
  write(`${stage}-due-result.json`, { passed: true, canonical: current });
});

test(`real-record-lifecycle-${stage}`, async ({ page }) => {
  if (stage === "create") {
    await createRecord(page, fixture.lifecycleDatabase, fixture.lifecycleTitle);
    await page.getByLabel(`Status for ${fixture.lifecycleTitle}`, { exact: true }).selectOption({ label: "In progress" });
    await expect.poll(async () => {
      const record = recordProjection(await snapshot(page), fixture.lifecycleTitle);
      const status = property(record, "Status");
      return record.record.values.find((v: any) => v.propertyId === status.id)?.value;
    }).toBe(property(recordProjection(await snapshot(page), fixture.lifecycleTitle), "Status").config.options.find((o: any) => o.label === "In progress").id);
    await setDate(page, `Date for ${fixture.lifecycleTitle}`, fixture.lifecycleDate);
    await expectDate(page, fixture.lifecycleTitle, "Date", fixture.lifecycleDate);
    const original = recordProjection(await snapshot(page), fixture.lifecycleTitle);
    expect(original.facet).toBeNull();
    await page.getByRole("button", { name: "Make task", exact: true }).click();
    const promotion = page.getByRole("dialog", { name: "Promote record to task", exact: true });
    await promotion.getByLabel("Task promotion due property", { exact: true }).selectOption({ label: "Date" });
    await promotion.getByRole("button", { name: "Confirm task", exact: true }).click();
    await expect.poll(async () => recordProjection(await snapshot(page), fixture.lifecycleTitle).facet?.duePropertyId).toBe(property(original, "Date").id);
    const promoted = recordProjection(await snapshot(page), fixture.lifecycleTitle);
    expect(identity(promoted)).toEqual(identity(original));
    expect(promoted.record.values).toEqual(original.record.values);
    await surface(page, "Tasks");
    await expect(dateControl(page, `Due for ${fixture.lifecycleTitle}`).locator("summary")).toContainText(fixture.lifecycleDate);
    await page.getByRole("button", { name: `Detach task facet for ${fixture.lifecycleTitle}`, exact: true }).click();
    const detach = page.getByRole("dialog", { name: `Detach task facet: ${fixture.lifecycleTitle}`, exact: true });
    await expect(detach.getByText("No retained todo occurrences reference this task.", { exact: true })).toBeVisible();
    await detach.getByRole("button", { name: "Cancel detachment", exact: true }).click();
    expect(recordProjection(await snapshot(page), fixture.lifecycleTitle)).toEqual(promoted);
    await page.getByRole("button", { name: `Detach task facet for ${fixture.lifecycleTitle}`, exact: true }).click();
    await detach.getByRole("checkbox", { name: /I reviewed all occurrence changes/ }).check();
    await detach.getByRole("button", { name: "Apply reviewed detachment", exact: true }).click();
    await expect.poll(async () => recordProjection(await snapshot(page), fixture.lifecycleTitle).facet).toBeNull();
    const detached = recordProjection(await snapshot(page), fixture.lifecycleTitle);
    expect(identity(detached)).toEqual(identity(original));
    expect(detached.record.values).toEqual(original.record.values);
    await expect(page.getByLabel(`Task title for ${fixture.lifecycleTitle}`, { exact: true })).toHaveCount(0);
    await database(page, fixture.lifecycleDatabase);
    await page.getByLabel(`Title for ${fixture.lifecycleTitle}`, { exact: true }).fill(fixture.lifecycleRenamed);
    await page.getByLabel(`Title for ${fixture.lifecycleTitle}`, { exact: true }).press("Enter");
    await expect(page.getByLabel(`Title for ${fixture.lifecycleRenamed}`, { exact: true })).toBeVisible();
    const renamed = recordProjection(await snapshot(page), fixture.lifecycleRenamed);
    await openRecord(page, fixture.lifecycleRenamed);
    await page.getByText("Grouped changes and undo", { exact: true }).click();
    await page.locator("li").filter({ hasText: `Detach task facet: ${fixture.lifecycleTitle}` }).getByRole("button", { name: "Preview undo", exact: true }).click();
    await page.getByRole("region", { name: "Review grouped undo", exact: true }).getByRole("button", { name: "Apply reviewed undo", exact: true }).click();
    await expect.poll(async () => recordProjection(await snapshot(page), fixture.lifecycleRenamed).facet).toEqual(promoted.facet);
    const restored = recordProjection(await snapshot(page), fixture.lifecycleRenamed);
    expect(identity(restored)).toEqual(identity(original));
    expect(restored.record.values).toEqual(renamed.record.values);
    const operations = await observe(page, "operations.list", { pageId: restored.page.id });
    expect(operations.operations.filter((o: any) => o.label === `Detach task facet: ${fixture.lifecycleTitle}` && o.state === "undone")).toHaveLength(1);
    write("create-record-lifecycle.json", { original, promoted, detached, renamed, canonical: restored, operations, cancellationPreservedCanonicalState: true });
  }
  await surface(page, "Tasks");
  await expect(page.getByLabel(`Task title for ${fixture.lifecycleRenamed}`, { exact: true })).toHaveValue(fixture.lifecycleRenamed);
  await expect(dateControl(page, `Due for ${fixture.lifecycleRenamed}`).locator("summary")).toContainText(fixture.lifecycleDate);
  const current = recordProjection(await snapshot(page), fixture.lifecycleRenamed);
  expect(current).toEqual(read("create-record-lifecycle.json").canonical);
  const operations = await observe(page, "operations.list", { pageId: current.page.id });
  expect(operations).toEqual(read("create-record-lifecycle.json").operations);
  write(`${stage}-record-lifecycle-result.json`, { passed: true, canonical: current, operations });
});

async function reviewAndLayout(page: Page, record: ReturnType<typeof recordProjection>) {
  return { freshness: await observe(page, "page.freshness", { pageId: record.page.id }),
    layout: await observe(page, "recordLayouts.get", { collectionId: record.record.collectionId, recordId: record.record.id }) };
}
test(`real-layout-review-${stage}`, async ({ page }) => {
  if (stage === "create") {
    await createRecord(page, fixture.layoutDatabase, fixture.layoutTitle);
    await setDate(page, `Date for ${fixture.layoutTitle}`, fixture.layoutDates[0]);
    await expectDate(page, fixture.layoutTitle, "Date", fixture.layoutDates[0]);
    await openRecord(page, fixture.layoutTitle);
    await page.locator("details.page-review > summary").click();
    await page.getByRole("button", { name: "I reviewed the current content", exact: true }).click();
    await expect(page.locator("details.page-review > summary")).toHaveText("Human reviewed");
    const initial = recordProjection(await snapshot(page), fixture.layoutTitle);
    const reviewed = await reviewAndLayout(page, initial);
    expect(reviewed.freshness.freshness.state).toBe("reviewed");
    expect(reviewed.freshness.history).toHaveLength(1);
    await database(page, fixture.layoutDatabase);
    const collectionLayout = page.getByRole("region", { name: "Collection record layout", exact: true });
    await collectionLayout.getByText("Edit collection record layout", { exact: true }).click();
    await collectionLayout.getByRole("button", { name: "Add layout section", exact: true }).click();
    await collectionLayout.getByLabel("Section 1 title", { exact: true }).fill(fixture.layoutSection);
    await collectionLayout.getByLabel("Section 1 placement", { exact: true }).selectOption("sidebar");
    await collectionLayout.getByLabel("Layout placement for Date", { exact: true }).selectOption({ label: `${fixture.layoutSection} · sidebar` });
    await collectionLayout.getByLabel("Layout placement for Status", { exact: true }).selectOption("hidden");
    await collectionLayout.getByRole("button", { name: "Save collection layout", exact: true }).click();
    await expect.poll(async () => (await reviewAndLayout(page, initial)).layout.defaultLayout?.sections[0]?.title).toBe(fixture.layoutSection);
    await openRecord(page, fixture.layoutTitle);
    await expect(page.locator(".record-layout-sidebar").getByRole("heading", { name: fixture.layoutSection, exact: true })).toBeVisible();
    await expect(page.getByLabel("Status on record page", { exact: true })).toHaveCount(0);
    const inherited = await reviewAndLayout(page, initial);
    expect(inherited.freshness).toEqual(reviewed.freshness);
    expect(inherited.layout.overrideLayout).toBeNull();
    const recordLayout = page.getByRole("region", { name: "Record properties", exact: true });
    await recordLayout.getByText("Customize this record layout", { exact: true }).click();
    await recordLayout.getByLabel("Layout placement for Date", { exact: true }).selectOption("hidden");
    await recordLayout.getByLabel("Layout placement for Status", { exact: true }).selectOption("unplaced");
    await recordLayout.getByRole("button", { name: "Save record override", exact: true }).click();
    await expect(page.getByLabel("Date on record page start", { exact: true })).toHaveCount(0);
    const override = await reviewAndLayout(page, initial);
    expect(override.layout.overrideLayout.hiddenPropertyIds).toContain(property(initial, "Date").id);
    expect(override.freshness).toEqual(reviewed.freshness);
    await recordLayout.getByText("Customize this record layout", { exact: true }).click();
    await recordLayout.getByRole("button", { name: "Reset to collection default", exact: true }).click();
    await expect(dateControl(page, "Date on record page").locator("summary")).toContainText(fixture.layoutDates[0]);
    const reset = await reviewAndLayout(page, initial);
    expect(reset.layout).toEqual(inherited.layout);
    expect(reset.freshness).toEqual(reviewed.freshness);
    const preserved = recordProjection(await snapshot(page), fixture.layoutTitle);
    expect(identity(preserved)).toEqual(identity(initial));
    expect(preserved.record.values).toEqual(initial.record.values);
    await setDate(page, "Date on record page", fixture.layoutDates[1]);
    await expectDate(page, fixture.layoutTitle, "Date", fixture.layoutDates[1]);
    await expect(page.locator("details.page-review > summary")).toHaveText("Changed since review");
    const stale = await reviewAndLayout(page, initial);
    expect(stale.freshness.freshness.state).toBe("stale");
    expect(stale.freshness.freshness.contentIdentity).not.toEqual(reviewed.freshness.freshness.contentIdentity);
    expect(stale.freshness.history).toEqual(reviewed.freshness.history);
    expect(stale.freshness.freshness.review).toEqual(reviewed.freshness.freshness.review);
    const canonical = recordProjection(await snapshot(page), fixture.layoutTitle);
    write("create-layout-review.json", { initial, reviewed, inherited, override, reset, preserved, canonical, final: stale,
      limitation: "The automated fixture exercises review-event persistence; it is not human acceptance of content." });
  } else {
    await database(page, fixture.layoutDatabase);
    await openRecord(page, fixture.layoutTitle);
  }
  await expect(page.locator("details.page-review > summary")).toHaveText("Changed since review");
  await expect(page.locator(".record-layout-sidebar").getByRole("heading", { name: fixture.layoutSection, exact: true })).toBeVisible();
  const current = recordProjection(await snapshot(page), fixture.layoutTitle);
  expect(current).toEqual(read("create-layout-review.json").canonical);
  const currentReviewLayout = await reviewAndLayout(page, current);
  expect(currentReviewLayout).toEqual(read("create-layout-review.json").final);
  write(`${stage}-layout-review-result.json`, { passed: true, canonical: current, ...currentReviewLayout });
});

function collectionProjection(s: any, title: string) {
  const collections = s.collections.filter((c: any) => c.title === title);
  expect(collections).toHaveLength(1);
  const collection = collections[0];
  const records = s.records.filter((r: any) => r.collectionId === collection.id).sort((a: any, b: any) => a.id.localeCompare(b.id));
  const ids = new Set([collection.owningPageId, ...records.map((r: any) => r.pageId)]);
  return { workspaceId: s.workspaceId, collection, records,
    pages: s.pages.filter((p: any) => ids.has(p.id)).sort((a: any, b: any) => a.id.localeCompare(b.id)),
    properties: s.properties.filter((p: any) => p.collectionId === collection.id).sort((a: any, b: any) => a.id.localeCompare(b.id)) };
}
async function openP2(page: Page) {
  const panel = page.getByRole("region", { name: "Local database enhancements", exact: true });
  if ((await panel.locator(":scope > details").getAttribute("open")) === null)
    await panel.getByText("Charts, local forms and row buttons", { exact: true }).click();
  return panel;
}
async function setStatus(page: Page, title: string, label: string) {
  await page.getByLabel(`Status for ${title}`, { exact: true }).selectOption({ label });
  await expect.poll(async () => {
    const r = recordProjection(await snapshot(page), title), p = property(r, "Status");
    const value = r.record.values.find((v: any) => v.propertyId === p.id)?.value;
    return p.config.options.find((o: any) => o.id === value)?.label;
  }).toBe(label);
}
async function p2State(page: Page, collectionId: string) {
  return { settings: await observe(page, "databaseP2", { collectionId }), chart: await observe(page, "databaseChart", { collectionId }) };
}
async function exactChart(page: Page, expected: Record<string, string>) {
  const chart = page.locator("figure.p2-chart");
  for (const [label, decimal] of Object.entries(expected)) {
    const row = chart.getByRole("row").filter({ has: page.getByRole("cell", { name: label, exact: true }) });
    await expect(row.getByRole("cell").nth(1)).toHaveText(decimal);
  }
  await expect(chart.locator("tbody tr")).toHaveCount(Object.keys(expected).length);
}

test(`real-p2-data-${stage}`, async ({ page }) => {
  if (stage === "create") {
    await createRecord(page, fixture.p2Database, fixture.p2SeedTitle);
    await page.getByRole("button", { name: "＋ Property", exact: true }).click();
    await page.getByLabel("Property name", { exact: true }).fill("Amount");
    await page.getByLabel("Property type", { exact: true }).selectOption("number");
    await page.getByRole("button", { name: "Create property", exact: true }).click();
    await openRecord(page, fixture.p2SeedTitle);
    await page.getByLabel("Amount on record page", { exact: true }).fill(fixture.p2Amounts[0]);
    await page.getByLabel("Amount on record page", { exact: true }).press("Enter");
    await expect.poll(async () => {
      const record = recordProjection(await snapshot(page), fixture.p2SeedTitle);
      return record.record.values.find((v: any) => v.propertyId === property(record, "Amount").id)?.value;
    }).toEqual({ decimal: fixture.p2Amounts[0], format: "decimal" });
    await database(page, fixture.p2Database);
    await setStatus(page, fixture.p2SeedTitle, "To do");
    const panel = await openP2(page), form = panel.getByRole("region", { name: "Local form configuration", exact: true });
    await form.getByRole("checkbox", { name: "Enable a local submission form", exact: true }).check();
    await form.getByRole("checkbox", { name: "Amount", exact: true }).check();
    await form.getByRole("button", { name: "Save form settings", exact: true }).click();
    await expect(form.getByRole("button", { name: "Submit local form", exact: true })).toBeDisabled();
    const beforeInvalid = collectionProjection(await snapshot(page), fixture.p2Database);
    await form.getByLabel("Form title", { exact: true }).fill(fixture.p2FormTitles[0]);
    await form.getByRole("button", { name: "Submit local form", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Amount is required." })).toBeVisible();
    expect(collectionProjection(await snapshot(page), fixture.p2Database)).toEqual(beforeInvalid);
    await form.getByLabel("Form Amount", { exact: true }).fill("not-a-decimal");
    await form.getByRole("button", { name: "Submit local form", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Invalid number value" })).toBeVisible();
    expect(collectionProjection(await snapshot(page), fixture.p2Database)).toEqual(beforeInvalid);
    for (let index = 0; index < 2; index++) {
      await form.getByLabel("Form title", { exact: true }).fill(fixture.p2FormTitles[index]);
      await form.getByLabel("Form Amount", { exact: true }).fill(fixture.p2Amounts[index + 1]);
      await form.getByLabel("Form Status", { exact: true }).selectOption({ label: index === 0 ? "To do" : "Done" });
      await form.getByRole("button", { name: "Submit local form", exact: true }).click();
      await expect(form.getByLabel("Form title", { exact: true })).toHaveValue("");
      const record = recordProjection(await snapshot(page), fixture.p2FormTitles[index]);
      expect(record.record.values.find((v: any) => v.propertyId === property(record, "Amount").id)?.value).toEqual({ decimal: fixture.p2Amounts[index + 1], format: "decimal" });
    }
    await panel.getByLabel("Chart group", { exact: true }).selectOption({ label: "Status" });
    await panel.getByLabel("Chart aggregation", { exact: true }).selectOption("sum");
    await panel.getByLabel("Chart numeric property", { exact: true }).selectOption({ label: "Amount" });
    await panel.getByRole("button", { name: "Save chart and settings", exact: true }).click();
    await exactChart(page, fixture.p2ExpectedSums);
    const canonical = collectionProjection(await snapshot(page), fixture.p2Database);
    expect(canonical.records).toHaveLength(3);
    const state = await p2State(page, canonical.collection.id);
    expect(state.chart.recordCount).toBe(3);
    for (const [label, decimal] of Object.entries(fixture.p2ExpectedSums)) {
      const group = state.chart.groups.find((g: any) => g.label === label);
      expect(group.decimal).toBe(decimal);
      const titles = label === "To do" ? [fixture.p2SeedTitle, fixture.p2FormTitles[0]] : [fixture.p2FormTitles[1]];
      expect([...group.recordIds].sort()).toEqual(titles.map((title: string) => canonical.records.find((r: any) => r.pageId === canonical.pages.find((p: any) => p.title === title)?.id)?.id).sort());
    }
    write("create-p2-data.json", { canonical, state, requiredAndTypedValidationPreservedCanonicalState: true });
  } else await database(page, fixture.p2Database);
  await openP2(page);
  await exactChart(page, fixture.p2ExpectedSums);
  await expect(page.getByRole("img", { name: "Bar chart; exact values in table below", exact: true })).toBeVisible();
  const canonical = collectionProjection(await snapshot(page), fixture.p2Database), state = await p2State(page, canonical.collection.id);
  expect(canonical).toEqual(read("create-p2-data.json").canonical);
  expect(state).toEqual(read("create-p2-data.json").state);
  write(`${stage}-p2-data-result.json`, { passed: true, canonical, state });
});

test(`real-p2-action-${stage}`, async ({ page }) => {
  if (stage === "create") {
    await createRecord(page, fixture.actionDatabase, fixture.actionTitle);
    await setStatus(page, fixture.actionTitle, "In progress");
    await setDate(page, `Date for ${fixture.actionTitle}`, fixture.actionDates[0]);
    await expectDate(page, fixture.actionTitle, "Date", fixture.actionDates[0]);
    const panel = await openP2(page), config = panel.getByRole("region", { name: "Row button configuration", exact: true });
    await config.getByLabel("Row button label", { exact: true }).fill(fixture.actionLabel);
    await config.getByLabel("Row button property", { exact: true }).selectOption({ label: "Status" });
    await config.getByLabel("Row button value", { exact: true }).selectOption({ label: "Done" });
    await expect(config.getByRole("checkbox", { name: "Ask for confirmation each time", exact: true })).toBeChecked();
    await config.getByRole("button", { name: "Add row button", exact: true }).click();
    const before = recordProjection(await snapshot(page), fixture.actionTitle);
    await page.getByRole("button", { name: fixture.actionLabel, exact: true }).click();
    const review = page.getByRole("alertdialog", { name: "Confirm row action", exact: true });
    await expect(review).toContainText(`Set Status to Done on “${fixture.actionTitle}”?`);
    await review.getByRole("button", { name: "Cancel", exact: true }).click();
    expect(recordProjection(await snapshot(page), fixture.actionTitle)).toEqual(before);
    await page.getByRole("button", { name: fixture.actionLabel, exact: true }).click();
    await setDate(page, `Date for ${fixture.actionTitle}`, fixture.actionDates[1]);
    await expectDate(page, fixture.actionTitle, "Date", fixture.actionDates[1]);
    await expect(review).toHaveCount(0);
    const staleInvalidated = recordProjection(await snapshot(page), fixture.actionTitle);
    expect(staleInvalidated.record.values.find((v: any) => v.propertyId === property(before, "Status").id)).toEqual(before.record.values.find((v: any) => v.propertyId === property(before, "Status").id));
    await page.getByRole("button", { name: fixture.actionLabel, exact: true }).click();
    await review.getByRole("button", { name: `Confirm ${fixture.actionLabel}`, exact: true }).click();
    await expect.poll(async () => {
      const r = recordProjection(await snapshot(page), fixture.actionTitle), p = property(r, "Status");
      return r.record.values.find((v: any) => v.propertyId === p.id)?.value === p.config.doneOptionId;
    }).toBe(true);
    const applied = recordProjection(await snapshot(page), fixture.actionTitle);
    expect(identity(applied)).toEqual(identity(before));
    await page.getByLabel(`Title for ${fixture.actionTitle}`, { exact: true }).fill(fixture.actionRenamed);
    await page.getByLabel(`Title for ${fixture.actionTitle}`, { exact: true }).press("Enter");
    await expect(page.getByLabel(`Title for ${fixture.actionRenamed}`, { exact: true })).toBeVisible();
    await openRecord(page, fixture.actionRenamed);
    await page.getByText("Grouped changes and undo", { exact: true }).click();
    await page.locator("li").filter({ hasText: `Row action: ${fixture.actionLabel}` }).getByRole("button", { name: "Preview undo", exact: true }).click();
    await page.getByRole("region", { name: "Review grouped undo", exact: true }).getByRole("button", { name: "Apply reviewed undo", exact: true }).click();
    await expect.poll(async () => recordProjection(await snapshot(page), fixture.actionRenamed).record.values).toEqual(staleInvalidated.record.values);
    const canonical = recordProjection(await snapshot(page), fixture.actionRenamed);
    expect(identity(canonical)).toEqual(identity(before));
    const operations = await observe(page, "operations.list", { pageId: canonical.page.id });
    const rowOperations = operations.operations.filter((o: any) => o.label === `Row action: ${fixture.actionLabel}`);
    expect(rowOperations).toHaveLength(1); expect(rowOperations[0].state).toBe("undone");
    const state = await p2State(page, canonical.record.collectionId);
    write("create-p2-action.json", { before, staleInvalidated, applied, canonical, operations, state, cancelledAndStaleReviewsWroteNoStatus: true });
  }
  await database(page, fixture.actionDatabase);
  await expect(page.getByLabel(`Title for ${fixture.actionRenamed}`, { exact: true })).toHaveValue(fixture.actionRenamed);
  await expect(dateControl(page, `Date for ${fixture.actionRenamed}`).locator("summary")).toContainText(fixture.actionDates[1]);
  const canonical = recordProjection(await snapshot(page), fixture.actionRenamed), operations = await observe(page, "operations.list", { pageId: canonical.page.id });
  const state = await p2State(page, canonical.record.collectionId), expected = read("create-p2-action.json");
  expect(canonical).toEqual(expected.canonical); expect(operations).toEqual(expected.operations); expect(state).toEqual(expected.state);
  write(`${stage}-p2-action-result.json`, { passed: true, canonical, operations, state });
});

function conversationProjection(s: any, item: any) {
  const pages = s.pages.filter((p: any) => p.title === fixture.conversationTitle && !p.deletedAt);
  expect(pages).toHaveLength(1); expect(pages[0].id).toBe(item.pageId);
  const blocks = s.blocks.filter((b: any) => b.pageId === item.pageId && !b.deletedAt).sort((a: any, b: any) => a.sortKey.localeCompare(b.sortKey));
  const texts = blocks.map((b: any) => (b.data.richText ?? []).map((r: any) => r.text).join(""));
  for (const text of fixture.conversationTexts) expect(texts.filter((t: string) => t === text)).toHaveLength(1);
  expect(texts.join("\n")).not.toContain("SOURCE_PAYLOAD_ONLY_V3");
  for (const [node, role] of [["question", "user"], ["reply-cedar", "assistant"], ["reply-ridge", "assistant"]]) {
    const heading = blocks.find((b: any) => item.blockSources[b.id] === `conversation-node:${node}:heading`);
    expect(heading?.type).toBe("heading_3");
    expect(heading.data.richText[0].text).toContain(`${role} · ${node}`);
    const links = blocks.flatMap((b: any) => b.data.richText ?? []).filter((r: any) => r.link === `trove://page/${item.pageId}/block/${heading.id}`);
    expect(links.length).toBeGreaterThan(0);
  }
  const file = s.files.find((f: any) => f.id === item.retainedFileId);
  expect(file.contentHash).toBe(createHash("sha256").update(JSON.stringify(fixture.conversationExport)).digest("hex"));
  return { workspaceId: s.workspaceId, page: pages[0], blocks, file };
}
async function openConversationPlan(page: Page, planId?: string) {
  await surface(page, "Import");
  if (planId) {
    await page.getByText("Recent imports and resumable plans", { exact: true }).click();
    await page.getByRole("button", { name: new RegExp(`^${fixture.conversationFile} · complete`) }).click();
  }
}
test(`real-conversation-display-${stage}`, async ({ page }) => {
  let plan: any, item: any;
  if (stage === "create") {
    await openConversationPlan(page);
    await page.getByLabel("Source namespace", { exact: true }).fill(fixture.conversationNamespace);
    const chosen = page.waitForEvent("filechooser");
    await page.getByRole("button", { name: "Choose import file", exact: true }).click();
    await (await chosen).setFiles({ name: fixture.conversationFile, mimeType: "application/json", buffer: Buffer.from(JSON.stringify(fixture.conversationExport)) });
    await expect(page.getByRole("button", { name: "Commit reviewed import", exact: true })).toBeEnabled();
    const recent = await observe(page, "imports.list", {});
    expect(recent).toHaveLength(1);
    const preview = await observe(page, "imports.plan", { planId: recent[0].planId });
    expect(preview.items.filter((i: any) => i.kind === "conversation")).toHaveLength(1);
    expect((await snapshot(page)).pages.some((p: any) => p.title === fixture.conversationTitle)).toBe(false);
    await page.getByRole("button", { name: "Commit reviewed import", exact: true }).click();
    await expect(page.getByText("Import processing finished. Review all outcomes below.", { exact: true })).toBeVisible();
    plan = await observe(page, "imports.plan", { planId: recent[0].planId });
    expect(plan.state).toBe("complete"); expect(plan.cursor).toBe(plan.items.length);
    item = plan.items.find((i: any) => i.kind === "conversation");
    const sourceLink = await page.getByRole("link", { name: "Original source", exact: true }).getAttribute("href");
    expect(sourceLink).toBeTruthy();
    const bytes = await page.evaluate(async href => {
      const response = await fetch(href!); if (!response.ok) throw Error(`source read ${response.status}`);
      return await response.text();
    }, sourceLink);
    expect(bytes).toBe(JSON.stringify(fixture.conversationExport));
    await page.getByRole("button", { name: item.sourceLocator, exact: true }).click();
    await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(fixture.conversationTitle);
    for (const text of fixture.conversationTexts) await expect(page.locator(".tiptap .block-text").filter({ hasText: text })).toHaveText(text);
    const before = conversationProjection(await snapshot(page), item);
    await page.locator("details.page-review > summary").click();
    await page.getByRole("button", { name: "I reviewed the current content", exact: true }).click();
    await expect(page.locator("details.page-review > summary")).toHaveText("Human reviewed");
    const review = await observe(page, "page.freshness", { pageId: item.pageId });
    await page.getByRole("button", { name: "Page options", exact: true }).click();
    const options = page.getByRole("dialog", { name: "Page options", exact: true });
    await options.getByRole("checkbox", { name: "Full width", exact: true }).click();
    await expect(options.getByRole("checkbox", { name: "Full width", exact: true })).toBeChecked();
    await options.getByRole("checkbox", { name: "Presentation layout", exact: true }).click();
    await expect(page.getByRole("button", { name: "Exit presentation layout", exact: true })).toBeVisible();
    const canonical = conversationProjection(await snapshot(page), item);
    expect(canonical.blocks).toEqual(before.blocks); expect(canonical.file).toEqual(before.file); expect(canonical.page.id).toBe(before.page.id);
    expect(await observe(page, "page.freshness", { pageId: item.pageId })).toEqual(review);
    const display = await observe(page, "pageDisplay.get", { pageId: item.pageId });
    expect(display.fullWidth).toBe(true); expect(display.presentation).toBe(true);
    write("create-conversation-display.json", { preview, plan, before, canonical, review, display, exactOriginalSourceRetained: true });
  } else {
    const saved = read("create-conversation-display.json");
    await openConversationPlan(page, saved.plan.planId);
    plan = await observe(page, "imports.plan", { planId: saved.plan.planId });
    expect(plan).toEqual(saved.plan);
    item = plan.items.find((i: any) => i.kind === "conversation");
    await page.getByRole("button", { name: item.sourceLocator, exact: true }).click();
  }
  await expect(page.locator("article.page-surface")).toHaveClass(/full-width/);
  await expect(page.getByRole("button", { name: "Exit presentation layout", exact: true })).toBeVisible();
  const canonical = conversationProjection(await snapshot(page), item), review = await observe(page, "page.freshness", { pageId: item.pageId });
  const display = await observe(page, "pageDisplay.get", { pageId: item.pageId }), expected = read("create-conversation-display.json");
  expect(canonical).toEqual(expected.canonical); expect(review).toEqual(expected.review); expect(display).toEqual(expected.display);
  write(`${stage}-conversation-display-result.json`, { passed: true, canonical, plan, review, display });
});


// Scheduling inputs and expected dates are fixed independently in fixture.json.
async function setDateRange(page: Page, title: string, value: any) {
  const label = `Date for ${title}`, control = dateControl(page, label);
  if ((await control.getAttribute("open")) === null) await control.locator("summary").click();
  await control.getByRole("checkbox", { name: "Include time", exact: true }).setChecked(value.hasTime);
  await control.getByLabel(`${label} start`, { exact: true }).fill(value.start);
  await control.getByLabel(`${label} end`, { exact: true }).fill(value.end ?? "");
  await control.getByLabel(`${label} timezone`, { exact: true }).fill(value.timezone);
  await control.getByRole("button", { name: "Save date", exact: true }).click();
  await expect.poll(async () => dateValue(recordProjection(await snapshot(page), title), "Date")).toEqual(value);
}
async function uiResponse(page: Page, method: string, gesture: () => Promise<unknown>) {
  const response = page.waitForResponse(response => response.url().endsWith("/api") && response.request().postDataJSON()?.method === method);
  await gesture();
  const result = await response;
  expect(result.status()).toBe(200);
  return result.json();
}
async function configureDependencies(page: Page, title: string, dependsOn: string[]) {
  const panel = page.locator("details.db-advanced");
  if ((await panel.getAttribute("open")) === null) await panel.locator(":scope > summary").click();
  await panel.getByRole("combobox", { name: "Record", exact: true }).selectOption({ label: title });
  await panel.getByRole("listbox", { name: "Depends on", exact: true }).selectOption(dependsOn.map(label => ({ label })));
  const response = await uiResponse(page, "batch", () => panel.getByRole("button", { name: "Save structure", exact: true }).click());
  expect(response.ok).toBe(true);
  await expect(panel.getByRole("button", { name: "Save structure", exact: true })).toBeEnabled();
}
async function openSchedule(page: Page, title: string, start: string, timezone: string, calendar: string) {
  const panel = page.locator("details.scheduling-panel");
  if ((await panel.getAttribute("open")) === null) await panel.locator(":scope > summary").click();
  await panel.getByLabel("Reschedule starting record", { exact: true }).selectOption({ label: title });
  await panel.getByLabel("Reschedule new start", { exact: true }).fill(start);
  await panel.getByLabel("Reschedule timezone", { exact: true }).fill(timezone);
  await panel.getByLabel("Reschedule calendar", { exact: true }).selectOption(calendar);
  return panel;
}
async function previewSchedule(page: Page) {
  const response = await uiResponse(page, "scheduling.preview", () => page.getByRole("button", { name: "Preview complete schedule", exact: true }).click());
  expect(response.ok).toBe(true);
  await expect(page.getByRole("region", { name: "Complete scheduling preview", exact: true })).toBeVisible();
  return response.result;
}
function schedulingRows(s: any, titles: string[]) {
  return titles.map(title => recordProjection(s, title));
}
function exactSchedule(plan: any, original: any[], after: any[], reasons: string[]) {
  expect(plan.records).toHaveLength(original.length);
  for (let i = 0; i < original.length; i++) {
    const change = plan.records.find((r: any) => r.recordId === original[i].record.id);
    expect(change).toMatchObject({ recordId: original[i].record.id, title: original[i].page.title,
      before: dateValue(original[i], "Date"), after: after[i], reason: reasons[i], changed: reasons[i] !== "slack_preserved" });
    expect(change.duration).toBe("2 inclusive eligible dates");
  }
}
async function visibleSchedule(page: Page, plan: any) {
  const preview = page.getByRole("region", { name: "Complete scheduling preview", exact: true });
  await expect(preview.locator("tbody tr")).toHaveCount(plan.records.length);
  for (const record of plan.records) {
    const row = preview.getByRole("row").filter({ has: page.getByRole("rowheader", { name: record.title, exact: true }) });
    await expect(row).toContainText(record.before.start);
    await expect(row).toContainText(record.after.start);
    if (record.after.end) await expect(row).toContainText(record.after.end);
    await expect(row).toContainText(record.duration);
    await expect(row).toContainText(record.reason.replaceAll("_", " "));
  }
}

test(`real-schedule-week-${stage}`, async ({ page }) => {
  const f = fixture.scheduleWeek;
  if (stage === "create") {
    await createRecord(page, f.database, f.titles[0]);
    for (const title of f.titles.slice(1)) {
      await page.getByLabel("New record title", { exact: true }).fill(title);
      await page.getByRole("button", { name: "Add record", exact: true }).click();
      await expect(page.getByLabel(`Title for ${title}`, { exact: true })).toBeVisible();
    }
    for (let i = 0; i < f.titles.length; i++) await setDateRange(page, f.titles[i], f.before[i]);
    for (const title of f.titles.slice(1)) await configureDependencies(page, title, [f.titles[0]]);
    const original = schedulingRows(await snapshot(page), f.titles);
    const structure = (await observe(page, "databaseDerived", { collectionId: original[0].record.collectionId })).structure;
    for (const r of original.slice(1)) expect(structure[r.record.id].dependencyRecordIds).toEqual([original[0].record.id]);
    const panel = await openSchedule(page, f.titles[0], f.after[0].start, f.timezone, "working_week");
    for (const day of ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]) await expect(panel.getByRole("checkbox", { name: day, exact: true })).toBeChecked();
    for (const day of ["Saturday", "Sunday"]) await expect(panel.getByRole("checkbox", { name: day, exact: true })).not.toBeChecked();
    const baseline = collectionProjection(await snapshot(page), f.database);
    const cancelledPlan = await previewSchedule(page);
    exactSchedule(cancelledPlan, original, f.after, ["requested", "dependency", "slack_preserved"]);
    await visibleSchedule(page, cancelledPlan);
    await page.getByRole("button", { name: "Cancel schedule", exact: true }).click();
    await expect(page.getByRole("region", { name: "Complete scheduling preview", exact: true })).toHaveCount(0);
    expect(collectionProjection(await snapshot(page), f.database)).toEqual(baseline);
    const stalePlan = await previewSchedule(page);
    await setStatus(page, f.titles[1], "In progress");
    const beforeRejected = collectionProjection(await snapshot(page), f.database);
    const rejected = await uiResponse(page, "batch", () => page.getByRole("button", { name: "Apply reviewed schedule", exact: true }).click());
    expect(rejected.ok).toBe(false); expect(rejected.error.code).toBe("CONFLICT");
    await expect(page.getByRole("alert")).toBeVisible();
    expect(collectionProjection(await snapshot(page), f.database)).toEqual(beforeRejected);
    const plan = await previewSchedule(page);
    exactSchedule(plan, original, f.after, ["requested", "dependency", "slack_preserved"]);
    await visibleSchedule(page, plan);
    const appliedResponse = await uiResponse(page, "batch", () => page.getByRole("button", { name: "Apply reviewed schedule", exact: true }).click());
    expect(appliedResponse.ok).toBe(true);
    await expect(page.getByRole("region", { name: "Complete scheduling preview", exact: true })).toHaveCount(0);
    const applied = schedulingRows(await snapshot(page), f.titles);
    expect(applied.map(r => dateValue(r, "Date"))).toEqual(f.after);
    expect(applied.map(identity)).toEqual(original.map(identity));
    await page.getByLabel(`Title for ${f.titles[1]}`, { exact: true }).fill(f.renamed);
    await page.getByLabel(`Title for ${f.titles[1]}`, { exact: true }).press("Enter");
    await expect(page.getByLabel(`Title for ${f.renamed}`, { exact: true })).toBeVisible();
    await openRecord(page, f.renamed);
    await page.getByText("Grouped changes and undo", { exact: true }).click();
    await page.locator("li").filter({ hasText: "Reschedule dependencies" }).getByRole("button", { name: "Preview undo", exact: true }).click();
    const undo = page.getByRole("region", { name: "Review grouped undo", exact: true });
    await expect(undo).toContainText(f.before[0].start); await expect(undo).toContainText(f.before[1].start);
    await undo.getByRole("button", { name: "Apply reviewed undo", exact: true }).click();
    const finalTitles = [f.titles[0], f.renamed, f.titles[2]];
    await expect.poll(async () => schedulingRows(await snapshot(page), finalTitles).map(r => dateValue(r, "Date"))).toEqual(f.before);
    const canonical = schedulingRows(await snapshot(page), finalTitles);
    expect(canonical.map(identity)).toEqual(original.map(identity));
    const status = property(canonical[1], "Status"), statusValue = canonical[1].record.values.find((v: any) => v.propertyId === status.id)?.value;
    expect(status.config.options.find((o: any) => o.id === statusValue)?.label).toBe("In progress");
    const operations = await observe(page, "operations.list", { pageId: canonical[1].page.id });
    const schedules = operations.operations.filter((o: any) => o.label === "Reschedule dependencies");
    expect(schedules).toHaveLength(1); expect(schedules[0]).toMatchObject({ id: plan.id, state: "undone" });
    write("create-schedule-week.json", { original, structure, cancelledPlan, stalePlan, rejected, plan, applied, canonical, operations });
  }
  await database(page, f.database);
  const canonical = schedulingRows(await snapshot(page), [f.titles[0], f.renamed, f.titles[2]]), expected = read("create-schedule-week.json");
  expect(canonical).toEqual(expected.canonical);
  expect((await observe(page, "databaseDerived", { collectionId: canonical[0].record.collectionId })).structure).toEqual(expected.structure);
  const operations = await observe(page, "operations.list", { pageId: canonical[1].page.id });
  expect(operations).toEqual(expected.operations);
  for (let i = 0; i < canonical.length; i++) await expect(dateControl(page, `Date for ${canonical[i].page.title}`).locator("summary")).toContainText(f.before[i].start);
  write(`${stage}-schedule-week-result.json`, { passed: true, canonical, operations });
});

test(`real-schedule-dst-${stage}`, async ({ page }) => {
  const f = fixture.scheduleDST;
  if (stage === "create") {
    await createRecord(page, f.database, f.title);
    await setDateRange(page, f.title, f.before);
    const original = recordProjection(await snapshot(page), f.title);
    const panel = await openSchedule(page, f.title, f.after.start, f.timezone, "calendar_days");
    const calendarPlan = await previewSchedule(page);
    expect(calendarPlan.records).toHaveLength(1);
    expect(calendarPlan.records[0]).toMatchObject({ before: f.before, after: f.calendarAfter, duration: "25h0m0s elapsed eligible time" });
    await visibleSchedule(page, calendarPlan);
    await page.getByRole("button", { name: "Cancel schedule", exact: true }).click();
    expect(recordProjection(await snapshot(page), f.title)).toEqual(original);
    await panel.getByLabel("Reschedule calendar", { exact: true }).selectOption("working_week");
    for (const day of ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]) await panel.getByRole("checkbox", { name: day, exact: true }).uncheck();
    await panel.getByRole("checkbox", { name: "Sunday", exact: true }).check();
    const plan = await previewSchedule(page);
    expect(plan.input.policy).toEqual({ version: 1, calendar: "working_week", timezone: f.timezone, weekdays: [0] });
    expect(plan.records).toHaveLength(1);
    expect(plan.records[0]).toMatchObject({ recordId: original.record.id, before: f.before, after: f.after, duration: "25h0m0s elapsed eligible time", changed: true });
    expect(plan.records[0].dstEffects).toEqual(["UTC offsets: before +02:00 → +01:00; after +01:00 → +01:00. Duration uses elapsed eligible time, including DST effects."]);
    await visibleSchedule(page, plan);
    await expect(page.getByRole("region", { name: "Complete scheduling preview", exact: true })).toContainText(plan.records[0].dstEffects[0]);
    const response = await uiResponse(page, "batch", () => page.getByRole("button", { name: "Apply reviewed schedule", exact: true }).click());
    expect(response.ok).toBe(true);
    await expect.poll(async () => dateValue(recordProjection(await snapshot(page), f.title), "Date")).toEqual(f.after);
    const canonical = recordProjection(await snapshot(page), f.title);
    expect(identity(canonical)).toEqual(identity(original));
    const operations = await observe(page, "operations.list", { pageId: canonical.page.id });
    expect(operations.operations.filter((o: any) => o.label === "Reschedule dependencies")).toHaveLength(1);
    write("create-schedule-dst.json", { original, calendarPlan, plan, canonical, operations });
  }
  await database(page, f.database);
  const canonical = recordProjection(await snapshot(page), f.title), expected = read("create-schedule-dst.json");
  expect(canonical).toEqual(expected.canonical);
  expect(dateValue(canonical, "Date")).toEqual(f.after);
  const operations = await observe(page, "operations.list", { pageId: canonical.page.id });
  expect(operations).toEqual(expected.operations);
  await expect(dateControl(page, `Date for ${f.title}`).locator("summary")).toContainText(f.after.start);
  write(`${stage}-schedule-dst-result.json`, { passed: true, canonical, operations });
});


async function chooseImport(page: Page, namespace: string, name: string, mimeType: string, bytes: Buffer) {
  await surface(page, "Import");
  await page.getByLabel("Source namespace", { exact: true }).fill(namespace);
  const selected = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Choose import file", exact: true }).click();
  const response = page.waitForResponse(response => response.url().endsWith("/api") && response.request().postDataJSON()?.method === "imports.preview");
  await (await selected).setFiles({ name, mimeType, buffer: bytes });
  const result = await (await response).json(); expect(result.ok).toBe(true);
  await expect(page.locator(".import-heading h3")).toHaveText(name);
  return result.result;
}
async function finishImport(page: Page, planId: string) {
  await page.getByRole("button", { name: "Commit reviewed import", exact: true }).click();
  await expect(page.getByText("Import processing finished. Review all outcomes below.", { exact: true })).toBeVisible();
  const plan = await observe(page, "imports.plan", { planId });
  expect(plan.state).toBe("complete"); expect(plan.cursor).toBe(plan.items.length);
  return plan;
}
function importedProjection(s: any, pageId: string) {
  const pages = s.pages.filter((p: any) => p.id === pageId && !p.deletedAt);
  expect(pages).toHaveLength(1);
  return { workspaceId: s.workspaceId, page: pages[0],
    blocks: s.blocks.filter((b: any) => b.pageId === pageId && !b.deletedAt).sort((a: any, b: any) => a.sortKey.localeCompare(b.sortKey)) };
}
const blockText = (b: any) => (b.data.richText ?? []).map((r: any) => r.text).join("");
async function openRecentImport(page: Page, plan: any) {
  await surface(page, "Import");
  const summary = page.getByText("Recent imports and resumable plans", { exact: true });
  if ((await summary.locator("..").getAttribute("open")) === null) await summary.click();
  const response = await uiResponse(page, "imports.plan", () => page.getByRole("button", { name: `${plan.sourceName} · ${plan.state} · ${plan.cursor}/${plan.items.length}`, exact: true }).first().click());
  expect(response.ok).toBe(true); expect(response.result.planId).toBe(plan.planId);
}
async function reviewedMovedIdentity(page: Page, plan: any, localId: string) {
  const f = fixture.importMerge, item = plan.items.find((i: any) => i.sourceLocator === f.movedFile);
  expect(item.identityReview?.choice).toBeUndefined();
  await expect(page.getByRole("button", { name: "Commit reviewed import", exact: true })).toBeDisabled();
  await page.getByLabel(`Existing page for ${f.movedFile}`, { exact: true }).selectOption(localId);
  const identity = await uiResponse(page, "imports.configure", () => page.getByRole("button", { name: `Link existing page for ${f.movedFile}`, exact: true }).click());
  expect(identity.ok).toBe(true);
  const linked = identity.result.items.find((i: any) => i.sourceLocator === f.movedFile);
  expect(linked.pageId).toBe(localId); expect(linked.merge.baselineStatus).toBe("retained");
  const title = linked.merge.fields.find((field: any) => field.path === "/title");
  expect(title).toMatchObject({ outcome: "conflict", base: { present: true, value: f.originalTitle }, incoming: { present: true, value: f.movedTitle } });
  await expect(page.getByText("Field review · retained source baseline", { exact: true })).toBeVisible();
  const resolution = await uiResponse(page, "imports.configure", () => page.getByLabel(`Resolution for ${f.movedFile} /title`, { exact: true }).selectOption("keep_local"));
  expect(resolution.ok).toBe(true);
  await expect(page.getByRole("button", { name: "Commit reviewed import", exact: true })).toBeEnabled();
  return resolution.result;
}

test(`real-import-merge-${stage}`, async ({ page }) => {
  const f = fixture.importMerge;
  if (stage === "create") {
    const preview = await chooseImport(page, f.namespace, f.originalFile, "text/html", Buffer.from(f.originalHTML));
    expect((await snapshot(page)).pages.some((p: any) => p.title === f.originalTitle)).toBe(false);
    const initialPlan = await finishImport(page, preview.planId), originalItem = initialPlan.items.find((i: any) => i.kind === "html");
    await page.getByRole("button", { name: originalItem.sourceLocator, exact: true }).click();
    await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(f.originalTitle);
    const original = importedProjection(await snapshot(page), originalItem.pageId);
    expect(original.blocks.map(blockText)).toEqual(f.originalTexts);
    await page.getByLabel("Page title", { exact: true }).fill(f.localTitle);
    const local = page.locator(".tiptap .block-text").filter({ hasText: f.originalTexts[0] });
    await local.click(); await page.keyboard.press("Meta+ArrowLeft"); await page.keyboard.press("Meta+Shift+ArrowRight");
    await page.keyboard.insertText(f.localText);
    await expect(local).toHaveCount(0);
    await page.getByRole("button", { name: "Save page", exact: true }).click();
    await expect.poll(async () => importedProjection(await snapshot(page), originalItem.pageId).blocks.map(blockText)).toEqual([f.localText, f.originalTexts[1]]);
    const localBefore = importedProjection(await snapshot(page), originalItem.pageId);
    expect(localBefore.page.title).toBe(f.localTitle); expect(localBefore.blocks.map((b: any) => b.id)).toEqual(original.blocks.map((b: any) => b.id));
    const movedPreview = await chooseImport(page, f.namespace, f.movedFile, "text/html", Buffer.from(f.incomingHTML));
    const reviewed = await reviewedMovedIdentity(page, movedPreview, originalItem.pageId);
    const reviewedItem = reviewed.items.find((i: any) => i.kind === "html");
    expect(reviewedItem.mappingId).toBe(originalItem.mappingId);
    const byPath = (field: string) => reviewedItem.merge.fields.find((x: any) => x.path === field);
    expect(byPath(`/blocks/${original.blocks[0].id}/content`).outcome).toBe("keep_local");
    expect(byPath(`/blocks/${original.blocks[1].id}/content`).outcome).toBe("apply_incoming");
    // Leaving a reviewed preview without committing must not apply its changes.
    await surface(page, "Home");
    expect(importedProjection(await snapshot(page), originalItem.pageId)).toEqual(localBefore);
    await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: `▤ ${f.localTitle}`, exact: true }).click();
    await page.getByLabel("Page title", { exact: true }).fill(f.finalTitle);
    await page.getByRole("button", { name: "Save page", exact: true }).click();
    await expect.poll(async () => importedProjection(await snapshot(page), originalItem.pageId).page.title).toBe(f.finalTitle);
    const beforeStale = importedProjection(await snapshot(page), originalItem.pageId);
    await openRecentImport(page, reviewed);
    const rejection = await uiResponse(page, "imports.commit", () => page.getByRole("button", { name: "Commit reviewed import", exact: true }).click());
    expect(rejection.ok).toBe(false); expect(rejection.error.code).toBe("CONFLICT");
    await expect(page.getByRole("alert").filter({ hasText: /changed after|changed since|revision/i })).toBeVisible();
    expect(importedProjection(await snapshot(page), originalItem.pageId)).toEqual(beforeStale);
    const refreshed = await chooseImport(page, f.namespace, f.movedFile, "text/html", Buffer.from(f.incomingHTML));
    await reviewedMovedIdentity(page, refreshed, originalItem.pageId);
    const committed = await finishImport(page, refreshed.planId);
    const canonical = importedProjection(await snapshot(page), originalItem.pageId);
    expect(canonical.page.title).toBe(f.finalTitle);
    expect(canonical.blocks.map(blockText)).toEqual([f.incomingText, f.localText]);
    expect(canonical.blocks.map((b: any) => b.id)).toEqual([original.blocks[1].id, original.blocks[0].id]);
    const beforeRetry = await snapshot(page);
    const canonicalIDs = (s: any) => ({ pages: s.pages.map((p: any) => p.id).sort(), blocks: s.blocks.map((b: any) => b.id).sort() });
    const beforeRetryIDs = canonicalIDs(beforeRetry);
    const retriedPreview = await chooseImport(page, f.namespace, f.movedFile, "text/html", Buffer.from(f.incomingHTML));
    expect(retriedPreview.sourceNamespace).toBe(f.namespace);
    const retryItem = retriedPreview.items.find((i: any) => i.kind === "html");
    expect(retryItem).toMatchObject({ sourceLocator: f.movedFile, externalId: f.movedFile, identityKind: "derived_locator", pageId: originalItem.pageId, mappingId: originalItem.mappingId });
    expect(retryItem.identityReview).toMatchObject({ version: 1, choice: "link_existing", localId: originalItem.pageId, mappingId: originalItem.mappingId, reason: "Previously reviewed source identity" });
    expect(retryItem.choice).toBe("keep_local");
    await expect(page.getByRole("status").filter({ hasText: `Identity decision: link existing · ${originalItem.pageId}` })).toBeVisible();
    await expect(page.getByRole("button", { name: "Commit reviewed import", exact: true })).toBeEnabled();
    const retry = await finishImport(page, retriedPreview.planId);
    expect(retry.rootSkipped).toBe(true);
    await expect(page.getByRole("button", { name: "Open imported pages", exact: true })).toHaveCount(0);
    const afterRetry = await snapshot(page);
    expect(importedProjection(afterRetry, originalItem.pageId)).toEqual(canonical);
    expect(canonicalIDs(afterRetry)).toEqual(beforeRetryIDs);
    const retained = await page.getByRole("link", { name: "Original source", exact: true }).getAttribute("href");
    expect(await page.evaluate(async href => (await fetch(href!)).text(), retained)).toBe(f.incomingHTML);
    write("create-import-merge.json", { initialPlan, originalItem, original, localBefore, movedPreview, reviewed, rejection, refreshed, committed, canonical, beforeRetryIDs, retry });
  }
  const saved = read("create-import-merge.json");
  await openRecentImport(page, saved.retry);
  expect(await observe(page, "imports.plan", { planId: saved.retry.planId })).toEqual(saved.retry);
  expect(saved.retry.rootSkipped).toBe(true);
  await expect(page.getByRole("button", { name: "Open imported pages", exact: true })).toHaveCount(0);
  const current = importedProjection(await snapshot(page), saved.originalItem.pageId);
  expect(current).toEqual(saved.canonical);
  await page.getByRole("button", { name: f.movedFile, exact: true }).click();
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(f.finalTitle);
  await expect(page.locator(".tiptap .block-text")).toHaveText([f.incomingText, f.localText]);
  write(`${stage}-import-merge-result.json`, { passed: true, canonical: current, plan: saved.retry });
});

function pausedImportProjection(s: any, plan: any) {
  const ids = new Set(plan.items.map((i: any) => i.pageId));
  return { pages: s.pages.filter((p: any) => ids.has(p.id)).sort((a: any, b: any) => a.id.localeCompare(b.id)),
    blocks: s.blocks.filter((b: any) => ids.has(b.pageId)).sort((a: any, b: any) => a.id.localeCompare(b.id)) };
}
test(`real-import-resume-${stage}`, async ({ page }) => {
  const f = fixture.importResume;
  if (stage === "create") {
    const preview = await chooseImport(page, f.namespace, f.file, "application/zip", Buffer.from(f.zipBase64, "base64"));
    expect(preview.items).toHaveLength(f.items.length);
    expect(pausedImportProjection(await snapshot(page), preview)).toEqual({ pages: [], blocks: [] });
    await page.getByRole("button", { name: "Commit reviewed import", exact: true }).click();
    await page.getByRole("button", { name: "Pause after current item", exact: true }).click();
    await expect(page.getByText("Paused after a durable commit. Resume here, including after restarting Trove.", { exact: true })).toBeVisible();
    const paused = await observe(page, "imports.plan", { planId: preview.planId });
    expect(paused.initialized).toBe(true); expect(paused.state).toBe("running"); expect(paused.cursor).toBeLessThan(f.items.length);
    expect(paused.items.filter((i: any) => i.committed)).toHaveLength(paused.cursor);
    const canonical = pausedImportProjection(await snapshot(page), paused);
    expect(canonical.pages.map((p: any) => p.id).sort()).toEqual(paused.items.map((i: any) => i.pageId).sort());
    expect(canonical.blocks).toHaveLength(paused.cursor);
    for (const item of paused.items.filter((i: any) => i.committed)) {
      const original = f.items.find((v: any) => v.file === item.sourceLocator);
      expect(canonical.blocks.find((b: any) => b.pageId === item.pageId)?.data.richText).toEqual([{ text: original.text }]);
    }
    write("create-import-resume.json", { preview, paused, canonical });
    write("create-import-resume-result.json", { passed: true, state: "paused_pending_reopen", canonical, plan: paused });
  } else {
    const saved = read("create-import-resume.json");
    await openRecentImport(page, saved.paused);
    expect(await observe(page, "imports.plan", { planId: saved.paused.planId })).toEqual(saved.paused);
    expect(pausedImportProjection(await snapshot(page), saved.paused)).toEqual(saved.canonical);
    await page.getByRole("button", { name: "Resume import", exact: true }).click();
    await expect(page.getByText("Import processing finished. Review all outcomes below.", { exact: true })).toBeVisible();
    const complete = await observe(page, "imports.plan", { planId: saved.paused.planId });
    expect(complete.state).toBe("complete"); expect(complete.cursor).toBe(f.items.length);
    const canonical = pausedImportProjection(await snapshot(page), complete);
    expect(canonical.pages.map((p: any) => p.id)).toEqual(saved.canonical.pages.map((p: any) => p.id));
    expect(canonical.blocks).toHaveLength(f.items.length);
    for (const item of complete.items) {
      const original = f.items.find((v: any) => v.file === item.sourceLocator), blocks = canonical.blocks.filter((b: any) => b.pageId === item.pageId);
      expect(blocks).toHaveLength(1); expect(blocks[0].id).toBe(item.blocks[0].id); expect(blockText(blocks[0])).toBe(original.text);
    }
    for (const b of saved.canonical.blocks) expect(canonical.blocks.find((v: any) => v.id === b.id)).toEqual(b);
    const last = complete.items[complete.items.length - 1];
    await page.getByRole("button", { name: last.sourceLocator, exact: true }).click();
    await expect(page.locator(".tiptap .block-text")).toHaveText(f.items.find((i: any) => i.file === last.sourceLocator).text);
    write("reopen-import-resume-result.json", { passed: true, canonical, plan: complete });
  }
});

// Suite 7: independently specified, non-generative library/review/graph workflows.
async function createPlainPage(page: Page, title: string, body: string) {
  await surface(page, "Pages");
  await page.getByRole("button", { name: "Create page", exact: true }).click();
  await page.getByLabel("Page title", { exact: true }).fill(title);
  await page.getByLabel("Page title", { exact: true }).press("Enter");
  await expect(page.getByLabel("Page title", { exact: true })).toBeEnabled();
  await page.locator(".tiptap .block-text").click({ position: { x: 8, y: 8 } });
  await page.keyboard.insertText(body);
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect.poll(async () => {
    const s = await snapshot(page), p = s.pages.find((p: any) => p.title === title && !p.deletedAt);
    return p ? s.blocks.filter((b: any) => b.pageId === p.id).map(blockText) : [];
  }).toEqual([body]);
  const s = await snapshot(page), p = s.pages.find((p: any) => p.title === title && !p.deletedAt);
  return importedProjection(s, p.id);
}
async function openPlainPage(page: Page, title: string) {
  await surface(page, "Pages");
  await page.getByRole("navigation", { name: "Page tree", exact: true }).getByRole("button", { name: `▤ ${title}`, exact: true }).click();
  await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(title);
  await expect(page.locator(".tiptap[contenteditable=true]")).toBeVisible();
}
function canonicalContent(s: any) {
  return { pages: s.pages, blocks: s.blocks, collections: s.collections, properties: s.properties, records: s.records, taskFacets: s.taskFacets };
}
function unrelatedRecords(s: any) {
  return { collections: s.collections, properties: s.properties, records: s.records, taskFacets: s.taskFacets };
}
const skillsPanel = (page: Page) => page.getByRole("region", { name: "Skills library", exact: true });
async function saveSkillVersion(page: Page, name: string, version: number) {
  const panel = skillsPanel(page);
  const result = await uiResponse(page, "batch", () => panel.getByRole("button", { name: "Save new version", exact: true }).click());
  expect(result.ok).toBe(true);
  await expect.poll(async () => (await observe(page, "skills.list", {})).find((v: any) => v.definition.name === name)?.version).toBe(version);
  const saved = (await observe(page, "skills.list", {})).find((v: any) => v.definition.name === name);
  await expect(panel.getByLabel("Saved skill", { exact: true })).toHaveValue(saved.id);
  await expect(panel.getByRole("button", { name: "Save new version", exact: true })).toBeEnabled();
  return saved;
}
async function skillState(page: Page) {
  const library = (await observe(page, "skills.list", {})).sort((a: any, b: any) => a.id.localeCompare(b.id));
  const versions: Record<string, any> = {};
  for (const v of library) versions[v.id] = await observe(page, "skills.list", { skillId: v.id });
  return { library, versions, aiRuns: await observe(page, "ai.runs", {}) };
}

test(stage === "create" ? "real-skills-create" : "real-skills-reopen", async ({ page }) => {
  const f = fixture.skills;
  if (stage === "create") {
    const selected = await createPlainPage(page, f.sourceTitle, f.sourceBody);
    const excluded = await createPlainPage(page, f.excludedTitle, f.excludedBody);
    await surface(page, "Skills");
    const panel = skillsPanel(page), beforeInvalid = canonicalContent(await snapshot(page));
    await panel.getByLabel("Skill instructions", { exact: true }).fill(f.instructions[0]);
    const rejected = await uiResponse(page, "batch", () => panel.getByRole("button", { name: "Save new version", exact: true }).click());
    expect(rejected.ok).toBe(false); expect(rejected.error.code).toBe("INVALID_REQUEST");
    expect(canonicalContent(await snapshot(page))).toEqual(beforeInvalid);
    expect(await observe(page, "skills.list", {})).toEqual([]);
    await panel.getByLabel("Skill name", { exact: true }).fill(f.name);
    await panel.getByLabel("Skill purpose", { exact: true }).fill(f.purpose);
    await panel.getByLabel("Skill inputDescription", { exact: true }).fill(f.inputDescription);
    await panel.getByLabel("Skill expectedOutput", { exact: true }).fill(f.expectedOutput);
    await panel.getByLabel("Skill mode", { exact: true }).selectOption("summarize");
    await panel.getByRole("group", { name: "Available sources", exact: true }).getByRole("checkbox", { name: f.sourceTitle, exact: true }).check();
    const permitted = panel.getByRole("group", { name: "Permitted tools", exact: true });
    await permitted.getByRole("checkbox", { name: "proposeActions", exact: true }).uncheck();
    await permitted.getByRole("checkbox", { name: "search.query", exact: true }).check();
    const first = await saveSkillVersion(page, f.name, 1);
    const expectedDefinition = { name: f.name, purpose: f.purpose, instructions: f.instructions[0], inputDescription: f.inputDescription,
      expectedOutput: f.expectedOutput, mode: "summarize", sourcePageIds: [selected.page.id], tools: ["readPage", "search.query"] };
    expect(first.definition).toEqual(expectedDefinition); expect(first.imported).toBe(false); expect(first.archived).toBe(false);
    await panel.getByLabel("Skill instructions", { exact: true }).fill(f.instructions[1]);
    const second = await saveSkillVersion(page, f.name, 2);
    expect(second.id).toBe(first.id); expect(second.pageId).toBe(first.pageId);
    expect(second.definition).toEqual({ ...expectedDefinition, instructions: f.instructions[1] });
    await panel.getByRole("button", { name: "Version history", exact: true }).click();
    await panel.getByRole("button", { name: "Use version 1 as next draft", exact: true }).click();
    const third = await saveSkillVersion(page, f.name, 3);
    expect(third.id).toBe(first.id); expect(third.definition).toEqual(expectedDefinition);
    const history = await observe(page, "skills.list", { skillId: first.id });
    expect(history).toEqual([third, second, first]);
    await panel.getByRole("button", { name: "Duplicate", exact: true }).click();
    const copy = await saveSkillVersion(page, f.name + " copy", 1);
    expect(copy.id).not.toBe(first.id); expect(copy.pageId).not.toBe(first.pageId);
    expect(copy.definition).toEqual({ ...expectedDefinition, name: f.name + " copy" });
    const runsBefore = await observe(page, "ai.runs", {}), canonicalBeforePreview = canonicalContent(await snapshot(page));
    await panel.getByLabel("Skill input", { exact: true }).fill(f.input);
    await panel.getByRole("group", { name: "Tools for this run", exact: true }).getByRole("checkbox", { name: "search.query", exact: true }).uncheck();
    const preview = await uiResponse(page, "skills.preview", () => panel.getByRole("button", { name: "Review skill run", exact: true }).click());
    expect(preview.ok).toBe(true);
    expect(preview.result.skill).toEqual(copy); expect(preview.result.provider).toBe("local");
    expect(preview.result.tools).toEqual(["readPage"]);
    expect(preview.result.sources).toEqual([{ pageId: selected.page.id, title: f.sourceTitle, revision: selected.page.revision }]);
    expect(preview.result.prompt).toBe(`Purpose: ${f.purpose}\nInstructions: ${f.instructions[0]}\nExpected output: ${f.expectedOutput}\nInput: ${f.input}`);
    expect(preview.result.hash).toMatch(/^[a-f0-9]{64}$/);
    await expect(panel.getByRole("heading", { name: "Review · local · v1", exact: true })).toBeVisible();
    expect(canonicalContent(await snapshot(page))).toEqual(canonicalBeforePreview);
    expect(await observe(page, "ai.runs", {})).toEqual(runsBefore);
    await panel.getByLabel("Saved skill", { exact: true }).selectOption(first.id);
    await panel.getByRole("checkbox", { name: "Archived (cannot run)", exact: true }).check();
    const archived = await saveSkillVersion(page, f.name, 4);
    expect(archived.archived).toBe(true); expect(archived.definition).toEqual(expectedDefinition);
    await expect(panel.getByRole("region", { name: "Run skill", exact: true })).toHaveCount(0);
    await panel.getByText("Import or export a skill definition", { exact: true }).click();
    const beforeImport = canonicalContent(await snapshot(page)), beforeLibrary = await observe(page, "skills.list", {});
    await panel.getByLabel("Skill JSON", { exact: true }).fill('{"name":"Incomplete imported definition"}');
    await panel.getByRole("button", { name: "Review imported definition", exact: true }).click();
    await expect(panel.getByRole("alert")).toContainText("Import a valid skill definition");
    expect(canonicalContent(await snapshot(page))).toEqual(beforeImport);
    const importedDefinition = { ...expectedDefinition, name: f.importName, instructions: f.importInstructions,
      sourcePageIds: [excluded.page.id], tools: ["readPage", "proposeChanges"] };
    await panel.getByLabel("Skill JSON", { exact: true }).fill(JSON.stringify(importedDefinition));
    await panel.getByRole("button", { name: "Review imported definition", exact: true }).click();
    await expect(panel.getByLabel("Skill name", { exact: true })).toHaveValue(f.importName);
    await expect(panel.getByRole("checkbox", { name: "Archived (cannot run)", exact: true })).toBeChecked();
    await expect(panel.getByRole("group", { name: "Available sources", exact: true }).getByRole("checkbox", { checked: true })).toHaveCount(0);
    expect(canonicalContent(await snapshot(page))).toEqual(beforeImport);
    expect(await observe(page, "skills.list", {})).toEqual(beforeLibrary);
    const imported = await saveSkillVersion(page, f.importName, 1);
    expect(imported.definition).toEqual({ ...importedDefinition, sourcePageIds: [] });
    expect(imported.imported).toBe(true); expect(imported.archived).toBe(true);
    await expect(panel.getByRole("region", { name: "Run skill", exact: true })).toHaveCount(0);
    expect(await observe(page, "ai.runs", {})).toEqual(runsBefore);
    const final = await snapshot(page);
    expect(importedProjection(final, selected.page.id)).toEqual(selected);
    expect(importedProjection(final, excluded.page.id)).toEqual(excluded);
    expect(unrelatedRecords(final)).toEqual(unrelatedRecords(beforeInvalid));
    write("create-skills.json", { selected, excluded, first, second, third, copy, archived, imported, preview: preview.result, state: await skillState(page) });
  }
  const saved = read("create-skills.json");
  await surface(page, "Skills");
  const panel = skillsPanel(page);
  expect(await skillState(page)).toEqual(saved.state);
  for (const version of [saved.archived, saved.copy, saved.imported]) {
    await panel.getByLabel("Saved skill", { exact: true }).selectOption(version.id);
    await expect(panel.getByLabel("Skill instructions", { exact: true })).toHaveValue(version.definition.instructions);
    await expect(panel.getByRole("checkbox", { name: "Archived (cannot run)", exact: true })).toBeChecked({ checked: version.archived });
    const selectedSources = await panel.getByRole("group", { name: "Available sources", exact: true }).getByRole("checkbox", { checked: true }).evaluateAll(nodes => nodes.map(n => n.parentElement?.textContent?.trim()));
    expect(selectedSources).toEqual(version.definition.sourcePageIds.length ? [f.sourceTitle] : []);
    const selectedTools = await panel.getByRole("group", { name: "Permitted tools", exact: true }).getByRole("checkbox", { checked: true }).evaluateAll(nodes => nodes.map(n => n.parentElement?.textContent?.trim()));
    expect(selectedTools).toEqual(version.definition.tools);
    await expect(panel.getByRole("region", { name: "Run skill", exact: true })).toHaveCount(version.archived ? 0 : 1);
  }
  const current = await snapshot(page);
  expect(importedProjection(current, saved.selected.page.id)).toEqual(saved.selected);
  expect(importedProjection(current, saved.excluded.page.id)).toEqual(saved.excluded);
  write(`${stage}-skills-result.json`, { passed: true, state: saved.state, previewOnly: true, generationExecuted: false, importedActivation: false });
});

const intelligencePanel = (page: Page) => page.getByRole("region", { name: "P2 intelligence packages", exact: true });
async function openIntelligence(page: Page, pageId: string, tab: "Personal review" | "Knowledge graph") {
  await surface(page, "Intelligence");
  const panel = intelligencePanel(page);
  await panel.getByRole("combobox", { name: "Page", exact: true }).selectOption(pageId);
  await panel.getByRole("navigation", { name: "Intelligence packages", exact: true }).getByRole("button", { name: tab, exact: true }).click();
  return panel;
}
function reviewPlanCard(page: Page, name: string) {
  return intelligencePanel(page).getByRole("article").filter({ has: page.getByRole("heading", { name, exact: true }) });
}
function pendingReviewCard(page: Page) {
  return intelligencePanel(page).getByRole("article").filter({ has: page.getByRole("button", { name: "Approve and append this digest", exact: true }) });
}
async function prepareReview(page: Page, plan: any, sources: any[]) {
  const before = canonicalContent(await snapshot(page));
  const response = await uiResponse(page, "review.run", () => reviewPlanCard(page, plan.plan.name).getByRole("button", { name: "Prepare review now", exact: true }).click());
  expect(response.ok).toBe(true);
  const run = response.result;
  expect(run.planBlockId).toBe(plan.blockId); expect(run.pageId).toBe(plan.pageId); expect(run.planHash).toBe(plan.hash);
  expect(run.status).toBe("pending"); expect(run.generative).toBeUndefined(); expect(run.modelRunId).toBeUndefined();
  expect(run.sources).toEqual(sources.map(s => ({ pageId: s.page.id, sourceRevision: s.page.revision })));
  for (const source of sources) { expect(run.text).toContain(source.page.title); expect(run.text).toContain(blockText(source.blocks[0])); }
  expect(canonicalContent(await snapshot(page))).toEqual(before);
  await expect(pendingReviewCard(page)).toHaveCount(1);
  await expect(pendingReviewCard(page).locator("pre")).toHaveText(run.text);
  return run;
}
async function decideReview(page: Page, approve: boolean) {
  return uiResponse(page, "batch", () => pendingReviewCard(page).getByRole("button", { name: approve ? "Approve and append this digest" : "Dismiss", exact: true }).click());
}

test(stage === "create" ? "real-personal-review-create" : "real-personal-review-reopen", async ({ page }) => {
  const f = fixture.personalReview;
  if (stage === "create") {
    const source = await createPlainPage(page, f.sourceTitle, f.sourceBody);
    const additional = await createPlainPage(page, f.additionalTitle, f.additionalBody);
    const destination = await createPlainPage(page, f.destinationTitle, f.unrelatedBody);
    const unrelated = unrelatedRecords(await snapshot(page)), aiBefore = await observe(page, "ai.runs", {});
    let panel = await openIntelligence(page, destination.page.id, "Personal review");
    await panel.getByLabel("Name", { exact: true }).fill(f.name);
    await panel.getByRole("combobox", { name: "Source page", exact: true }).selectOption(source.page.id);
    await panel.getByRole("listbox", { name: "Additional source pages", exact: true }).selectOption([additional.page.id]);
    await expect(panel.getByRole("checkbox", { name: "Include approved relevant memories", exact: true })).not.toBeChecked();
    await expect(panel.getByRole("checkbox", { name: "Enable local model analysis for this plan", exact: true })).not.toBeChecked();
    await panel.getByRole("combobox", { name: "Recurrence", exact: true }).selectOption("weekly");
    await panel.getByRole("combobox", { name: "Review weekday", exact: true }).selectOption(String(f.weekday));
    await panel.getByLabel("Local time", { exact: true }).fill(f.time);
    await panel.getByLabel("Time zone", { exact: true }).fill(f.timezone);
    await panel.getByRole("combobox", { name: "After downtime", exact: true }).selectOption("skip");
    const created = await uiResponse(page, "batch", () => panel.getByRole("button", { name: "Save review plan on this page", exact: true }).click());
    expect(created.ok).toBe(true);
    await expect(reviewPlanCard(page, f.name)).toContainText("Enabled locally");
    const plan = (await observe(page, "review.list", {})).plans.find((p: any) => p.plan.name === f.name);
    expect(plan.pageId).toBe(destination.page.id); expect(plan.activated).toBe(true);
    expect(plan.plan).toEqual({ type: "trove.personal-review/v1", name: f.name, enabled: true,
      sourcePageIds: [source.page.id, additional.page.id], query: "", includeMemory: false,
      schedule: { type: "schedule", timezone: f.timezone, frequency: "weekly", hour: 23, minute: 59, weekday: f.weekday, missedPolicy: "skip", catchUpLimit: 1 } });
    const dismissed = await prepareReview(page, plan, [source, additional]);
    expect((await decideReview(page, false)).ok).toBe(true);
    await expect(pendingReviewCard(page)).toHaveCount(0);
    expect((await observe(page, "review.list", {})).runs.find((r: any) => r.id === dismissed.id).status).toBe("dismissed");
    const stale = await prepareReview(page, plan, [source, additional]);
    await openPlainPage(page, f.sourceTitle);
    await observeLineEdgeKeys(page);
    try {
      await page.locator(".tiptap .block-text").click({ position: { x: 8, y: 8 } });
      await page.keyboard.press("Home"); await page.keyboard.press("Shift+End"); await page.keyboard.insertText(f.changedBody);
      await page.getByRole("button", { name: "Save page", exact: true }).click();
      await expect.poll(async () => blockText(importedProjection(await snapshot(page), source.page.id).blocks[0])).toBe(f.changedBody);
    } finally {
      write(`${stage}-personal-review-key-events.json`, await lineEdgeObservations(page));
    }
    const changedSource = importedProjection(await snapshot(page), source.page.id);
    expect(changedSource.blocks[0].id).toBe(source.blocks[0].id);
    panel = await openIntelligence(page, destination.page.id, "Personal review");
    await expect(pendingReviewCard(page).locator("pre")).toHaveText(stale.text);
    const beforeStale = canonicalContent(await snapshot(page)), rejection = await decideReview(page, true);
    expect(rejection.ok).toBe(false); expect(rejection.error.code).toBe("CONFLICT");
    expect(canonicalContent(await snapshot(page))).toEqual(beforeStale);
    expect((await observe(page, "review.list", {})).runs.find((r: any) => r.id === stale.id).status).toBe("pending");
    expect((await decideReview(page, false)).ok).toBe(true);
    await expect(pendingReviewCard(page)).toHaveCount(0);
    const fresh = await prepareReview(page, plan, [changedSource, additional]);
    const beforeApproval = importedProjection(await snapshot(page), destination.page.id);
    expect((await decideReview(page, true)).ok).toBe(true);
    await expect(pendingReviewCard(page)).toHaveCount(0);
    const accepted = importedProjection(await snapshot(page), destination.page.id);
    const added = accepted.blocks.filter((b: any) => !beforeApproval.blocks.some((v: any) => v.id === b.id));
    expect(added).toHaveLength(1); expect(added[0].type).toBe("paragraph"); expect(blockText(added[0])).toBe(fresh.text);
    for (const block of beforeApproval.blocks) expect(accepted.blocks.find((v: any) => v.id === block.id)).toEqual(block);
    const toggle = async (label: string) => {
      const response = await uiResponse(page, "batch", () => reviewPlanCard(page, f.name).getByRole("button", { name: label, exact: true }).click());
      expect(response.ok).toBe(true);
      await expect(reviewPlanCard(page, f.name).getByRole("button", { name: label === "Pause" ? "Activate locally" : "Pause", exact: true })).toBeEnabled();
    };
    await toggle("Pause"); await toggle("Activate locally"); await toggle("Pause");
    const state = await observe(page, "review.list", {}), final = await snapshot(page);
    expect(state.plans).toHaveLength(1); expect(state.plans[0].blockId).toBe(plan.blockId);
    expect(state.plans[0].activated).toBe(false); expect(state.plans[0].plan).toEqual({ ...plan.plan, enabled: false });
    expect(state.runs.map((r: any) => [r.id, r.status]).sort()).toEqual([[dismissed.id, "dismissed"], [stale.id, "dismissed"], [fresh.id, "accepted"]].sort());
    expect(importedProjection(final, additional.page.id)).toEqual(additional);
    expect(importedProjection(final, source.page.id)).toEqual(changedSource);
    const finalDestination = importedProjection(final, destination.page.id);
    expect(finalDestination.blocks.find((b: any) => b.id === destination.blocks[0].id)).toEqual(destination.blocks[0]);
    expect(finalDestination.blocks.find((b: any) => b.id === added[0].id)).toEqual(added[0]);
    expect(unrelatedRecords(final)).toEqual(unrelated);
    expect(await observe(page, "ai.runs", {})).toEqual(aiBefore);
    write("create-personal-review.json", { source: changedSource, additional, destination: finalDestination, plan, dismissed, stale, fresh, rejection, state, unrelated, aiBefore });
  }
  const saved = read("create-personal-review.json");
  const panel = await openIntelligence(page, saved.destination.page.id, "Personal review");
  const state = await observe(page, "review.list", {});
  expect(state).toEqual(saved.state);
  await expect(reviewPlanCard(page, f.name)).toContainText("Paused / activation required");
  await expect(panel.getByRole("button", { name: "Approve and append this digest", exact: true })).toHaveCount(0);
  const s = await snapshot(page);
  for (const projection of [saved.source, saved.additional, saved.destination]) expect(importedProjection(s, projection.page.id)).toEqual(projection);
  expect(unrelatedRecords(s)).toEqual(saved.unrelated);
  expect(await observe(page, "ai.runs", {})).toEqual(saved.aiBefore);
  write(`${stage}-personal-review-result.json`, { passed: true, state, generationExecuted: false, exactAppendBlock: saved.destination.blocks.find((b: any) => blockText(b) === saved.fresh.text) });
});

async function insertPageLink(page: Page, title: string) {
  await page.getByRole("button", { name: "＋ Add a thought · type / for blocks", exact: true }).click();
  await page.getByRole("button", { name: "＋ Insert", exact: true }).click();
  page.once("dialog", dialog => dialog.accept(title));
  await page.getByRole("dialog", { name: "Insert a block", exact: true }).getByRole("button", { name: /^Page link/ }).click();
  await page.getByRole("button", { name: "Save page", exact: true }).click();
  await expect(page.locator(".tiptap .page-link").filter({ hasText: title })).toBeVisible();
}
async function visibleGraph(page: Page, pageId: string) {
  const panel = await openIntelligence(page, pageId, "Knowledge graph");
  await expect(panel.getByRole("checkbox", { name: "Include retrieval associations", exact: true })).not.toBeChecked();
  const response = await uiResponse(page, "graph.query", () => panel.getByRole("button", { name: "Explore connections", exact: true }).click());
  expect(response.ok).toBe(true); expect(response.result.truncated).toBe(false);
  await expect(panel.getByRole("img", { name: "Page relationships", exact: true })).toBeVisible();
  return response.result;
}
function expectedGraphEdge(source: any, target: any, blockId: string) {
  return { source: source.page.id, target: target.page.id, kind: "explicit", evidence: "Canonical link in source block",
    location: { pageId: source.page.id, sourceRevision: source.page.revision, blockId } };
}

test(stage === "create" ? "real-graph-create" : "real-graph-reopen", async ({ page }) => {
  const f = fixture.graph;
  if (stage === "create") {
    const target = await createPlainPage(page, f.targetTitle, f.targetBody);
    const removedTarget = await createPlainPage(page, f.removedTitle, f.removedBody);
    const source = await createPlainPage(page, f.sourceTitle, f.sourceBody), unrelated = unrelatedRecords(await snapshot(page));
    await insertPageLink(page, f.targetTitle); await insertPageLink(page, f.removedTitle);
    await expect.poll(async () => importedProjection(await snapshot(page), source.page.id).blocks.filter((b: any) => b.type === "page_mention").length).toBe(2);
    const linked = importedProjection(await snapshot(page), source.page.id);
    const link = linked.blocks.find((b: any) => b.type === "page_mention" && b.data.targetPageId === target.page.id);
    const removedLink = linked.blocks.find((b: any) => b.type === "page_mention" && b.data.targetPageId === removedTarget.page.id);
    expect(link).toBeTruthy(); expect(removedLink).toBeTruthy(); expect(link.id).not.toBe(removedLink.id);
    const graph = await visibleGraph(page, source.page.id);
    expect(graph.edges).toEqual([expectedGraphEdge(linked, target, link.id), expectedGraphEdge(linked, removedTarget, removedLink.id)].sort((a, b) => a.location.blockId.localeCompare(b.location.blockId)));
    expect(graph.nodes.map((n: any) => n.id).sort()).toEqual([source.page.id, target.page.id, removedTarget.page.id].sort());
    const edge = intelligencePanel(page).locator("details").filter({ has: page.getByText(`explicit · ${f.sourceTitle} → ${f.targetTitle}`, { exact: true }) });
    await edge.locator("summary").click();
    await expect(edge).toContainText(`Source revision ${linked.page.revision} · block ${link.id}`);
    await edge.getByRole("button", { name: "Open evidence", exact: true }).click();
    await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(f.sourceTitle);
    await page.locator(".tiptap").getByRole("button", { name: `↗ ${f.targetTitle}`, exact: true }).click();
    await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(f.targetTitle);
    await page.locator(".backlinks").getByRole("button", { name: `↗ ${f.sourceTitle}`, exact: true }).click();
    await expect(page.getByLabel("Page title", { exact: true })).toHaveValue(f.sourceTitle);
    // Existing keyboard block navigation selects canonical atoms; no editor command injection.
    await page.locator(`.tiptap [data-block-id="${source.blocks[0].id}"] .block-text`).click({ position: { x: 8, y: 8 } });
    await page.keyboard.press("Alt+ArrowDown"); await page.keyboard.press("Alt+ArrowDown");
    await expect(page.locator(".tiptap .ProseMirror-selectednode [data-block-id]")).toHaveAttribute("data-block-id", removedLink.id);
    await page.keyboard.press("Backspace");
    await page.getByRole("button", { name: "Save page", exact: true }).click();
    await expect.poll(async () => importedProjection(await snapshot(page), source.page.id).blocks.some((b: any) => b.id === removedLink.id)).toBe(false);
    const final = await snapshot(page), finalSource = importedProjection(final, source.page.id);
    // Deleting a sibling emits a guarded reorder. Each retained sibling is
    // touched once; its identity, parent, order and values must stay exact.
    expect(finalSource.blocks).toEqual(linked.blocks.filter((b: any) => b.id !== removedLink.id)
      .map((b: any) => ({ ...b, revision: String(BigInt(b.revision) + 1n) })));
    expect(importedProjection(final, target.page.id)).toEqual(target); expect(importedProjection(final, removedTarget.page.id)).toEqual(removedTarget);
    expect(unrelatedRecords(final)).toEqual(unrelated);
    const finalGraph = await visibleGraph(page, source.page.id);
    expect(finalGraph.edges).toEqual([expectedGraphEdge(finalSource, target, link.id)]);
    expect(finalGraph.nodes.map((n: any) => n.id).sort()).toEqual([source.page.id, target.page.id].sort());
    write("create-graph.json", { source: finalSource, target, removedTarget, removedLink, link, graph, finalGraph, unrelated });
  }
  const saved = read("create-graph.json"), current = await snapshot(page);
  for (const projection of [saved.source, saved.target, saved.removedTarget]) expect(importedProjection(current, projection.page.id)).toEqual(projection);
  expect(unrelatedRecords(current)).toEqual(saved.unrelated);
  expect(await visibleGraph(page, saved.source.page.id)).toEqual(saved.finalGraph);
  const removed = await visibleGraph(page, saved.removedTarget.page.id);
  expect(removed.edges).toEqual([]); expect(removed.nodes.map((n: any) => n.id)).toEqual([saved.removedTarget.page.id]);
  await expect(intelligencePanel(page).getByText("No connections in this neighborhood. Add a page link or include retrieval associations.", { exact: true })).toBeVisible();
  write(`${stage}-graph-result.json`, { passed: true, graph: saved.finalGraph, removed, semanticAssociations: false });
});
