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
