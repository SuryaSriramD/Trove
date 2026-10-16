import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
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
async function observe(page: Page, method: "page.freshness" | "recordLayouts.get" | "operations.list", args: Record<string, unknown>) {
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
async function surface(page: Page, name: "Tasks" | "Home" | "Pages") {
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
