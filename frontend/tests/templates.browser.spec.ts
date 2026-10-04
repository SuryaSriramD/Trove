import { test, expect } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEMPLATE_TEST_URL ?? process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4186"}/tests/templates-fixture.html`,
  );
});
test("captures combined content and applies the exact reviewed kernel plan in shell mode", async ({
  page,
}) => {
  page.once("dialog", (dialog) => dialog.accept("Weekly combined template"));
  await page
    .getByRole("button", { name: "Save combined template", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "property defaults and page body",
  );
  await page.getByLabel("Saved template", { exact: true }).selectOption(id(10));
  await page.getByLabel("New title", { exact: true }).fill("Week 42");
  await page
    .getByRole("button", { name: "Preview template", exact: true })
    .click();
  await expect(
    page.getByLabel("Template preview", { exact: true }),
  ).toContainText("2 property defaults · 5 body blocks · 1 independent tasks");
  let calls = await page.evaluate(() => (window as any).templateCalls);
  expect(calls.filter((c: any) => c.method === "batch")).toHaveLength(0);
  await page
    .getByRole("button", { name: "Create from reviewed template", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "Created the combined template copy",
  );
  calls = await page.evaluate(() => (window as any).templateCalls);
  expect(
    calls.find((c: any) => c.method === "editor.saveTemplate").request,
  ).toMatchObject({ pageId: id(2), expectedPageRevision: "7" });
  expect(
    calls.find((c: any) => c.method === "template.preview").request,
  ).toMatchObject({
    templateId: id(10),
    collectionId: id(4),
    title: "Week 42",
    propertyDecisions: [],
  });
  expect(calls.find((c: any) => c.method === "batch").request.commands).toEqual(
    [{ kind: "template.apply", previewId: id(21) }],
  );
  expect(
    calls.some(
      (c: any) => c.method === "allocateIds" || c.method === "readRecord",
    ),
  ).toBe(false);
});
test("requires a fresh reviewed typed mapping and clears stale previews", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).templateNeedsMapping = true;
  });
  await page.getByLabel("Saved template", { exact: true }).selectOption(id(10));
  await page
    .getByRole("button", { name: "Preview template", exact: true })
    .click();
  const create = page.getByRole("button", {
    name: "Create from reviewed template",
    exact: true,
  });
  await expect(create).toBeDisabled();
  await page.getByLabel("Map Effort", { exact: true }).selectOption(id(5));
  await page
    .getByLabel("Value for Effort", { exact: true })
    .fill('{"decimal":"13","format":"number"}');
  await page
    .getByRole("button", { name: "Preview template", exact: true })
    .click();
  await expect(create).toBeEnabled();
  await page.getByLabel("New title", { exact: true }).fill("Changed title");
  await expect(create).toHaveCount(0);
  await page.evaluate(() => {
    (window as any).staleTemplate = true;
  });
  await page
    .getByRole("button", { name: "Preview template", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Destination schema changed",
  );
  await expect(create).toHaveCount(0);
  const calls = await page.evaluate(() => (window as any).templateCalls);
  expect(calls.filter((c: any) => c.method === "batch")).toHaveLength(0);
  expect(
    calls.filter((c: any) => c.method === "template.preview")[1].request
      .propertyDecisions,
  ).toEqual([
    {
      sourcePropertyId: id(5),
      action: "map",
      targetPropertyId: id(5),
      value: { decimal: "13", format: "number" },
    },
  ]);
});
