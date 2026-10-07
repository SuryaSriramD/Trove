import { test, expect } from "@playwright/test";
const base = process.env.TROVE_SUPPORT_TEST_URL ?? "http://127.0.0.1:4193";
test("metadata is default and exact preview precedes opaque native save", async ({
  page,
}) => {
  await page.goto(`${base}/tests/support-fixture.html`);
  await expect(
    page.getByRole("checkbox", { name: "Include selected page titles" }),
  ).not.toBeChecked();
  await expect(
    page.getByRole("checkbox", { name: "Include selected page text" }),
  ).not.toBeChecked();
  await expect(
    page.getByRole("button", { name: "Save reviewed bundle…" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await expect(page.getByLabel("Support bundle preview")).toContainText(
    '{"application":"Trove"}',
  );
  expect(
    await page.evaluate(() => (window as any).supportCalls.at(-1)),
  ).toEqual({
    method: "preview",
    request: {
      workspaceId: "workspace-current",
      selection: { titles: false, text: false, pageIds: [] },
    },
  });
  await page.getByRole("button", { name: "Save reviewed bundle…" }).click();
  await expect(page.getByRole("status")).toHaveText(
    "Support bundle saved locally. Nothing was uploaded.",
  );
  expect(
    await page.evaluate(() => (window as any).supportCalls.at(-1)),
  ).toEqual({ method: "export", id: "opaque-review" });
});
test("content requires category and page selection; changing it discards consent", async ({
  page,
}) => {
  await page.goto(`${base}/tests/support-fixture.html`);
  await page
    .getByRole("checkbox", { name: "Include selected page text" })
    .check();
  await expect(
    page.getByRole("button", { name: "Preview exact bundle contents" }),
  ).toBeDisabled();
  await expect(page.getByLabel("Local support bundle")).toContainText(
    "does not automatically redact",
  );
  await page
    .getByLabel("Pages to include", { exact: true })
    .selectOption("page-two");
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await expect(page.getByLabel("Support bundle preview")).toContainText(
    "explicitly consented secret",
  );
  expect(
    await page.evaluate(
      () => (window as any).supportCalls.at(-1).request.selection,
    ),
  ).toEqual({ titles: false, text: true, pageIds: ["page-two"] });
  await page
    .getByRole("checkbox", { name: "Include selected page titles" })
    .check();
  await expect(page.getByLabel("Support bundle preview")).toHaveCount(0);
});
test("workspace change, stale content and cancellation cannot report success", async ({
  page,
}) => {
  await page.goto(`${base}/tests/support-fixture.html`);
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await page.getByRole("button", { name: "Fixture change workspace" }).click();
  await expect(page.getByLabel("Support bundle preview")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await page.evaluate(() => {
    (window as any).supportStale = true;
  });
  await page.getByRole("button", { name: "Save reviewed bundle…" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Selected content changed",
  );
  await expect(page.getByRole("status")).toHaveCount(0);
  await page.evaluate(() => {
    (window as any).supportStale = false;
    (window as any).supportDelay = true;
  });
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await page.getByRole("button", { name: "Save reviewed bundle…" }).click();
  await page.getByRole("button", { name: "Cancel support operation" }).click();
  await expect(
    page.getByRole("button", { name: "Preview exact bundle contents" }),
  ).toBeEnabled();
  await expect(page.getByRole("status")).toHaveCount(0);
});

test("cancelled or replaced preview ignores a late successful native reply", async ({
  page,
}) => {
  await page.goto(`${base}/tests/support-fixture.html`);
  await page
    .getByRole("checkbox", { name: "Include selected page text" })
    .check();
  await page
    .getByLabel("Pages to include", { exact: true })
    .selectOption("page-one");
  await page.evaluate(() => {
    (window as any).delaySupportPreview = true;
  });
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await page.getByRole("button", { name: "Cancel support operation" }).click();
  await page.evaluate(() => {
    (window as any).completeSupportPreview();
  });
  await expect(page.getByLabel("Support bundle preview")).toHaveCount(0);
  await expect(
    page.getByText('"explicitly consented secret"', { exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole("status")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await page.getByRole("button", { name: "Fixture change workspace" }).click();
  await page.evaluate(() => {
    (window as any).completeSupportPreview();
  });
  await expect(page.getByLabel("Support bundle preview")).toHaveCount(0);
});

test("operational diagnostics require explicit consent and fresh preview", async ({
  page,
}) => {
  await page.goto(`${base}/tests/support-fixture.html`);
  const option = page.getByRole("checkbox", {
    name: "Include operational diagnostics",
  });
  await expect(option).not.toBeChecked();
  await option.check();
  await page
    .getByRole("button", { name: "Preview exact bundle contents" })
    .click();
  await expect(page.getByLabel("Support bundle preview")).toContainText(
    "diagnostics.json",
  );
  expect(
    await page.evaluate(
      () => (window as any).supportCalls.at(-1).request.selection,
    ),
  ).toEqual({ diagnostics: true, titles: false, text: false, pageIds: [] });
  await option.uncheck();
  await expect(page.getByLabel("Support bundle preview")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Save reviewed bundle…" }),
  ).toHaveCount(0);
});
