import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_NOTION_TEST_URL ?? process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4186"}/tests/notion-capture-fixture.html`,
  );
  await page
    .getByText("Optional authenticated Notion capture", { exact: true })
    .click();
});
test("explicit database roots resume the saved property cursor and require inventory review", async ({
  page,
}) => {
  await page
    .getByRole("combobox", { name: "Root type", exact: true })
    .selectOption("database");
  await page
    .getByLabel("Authorized root IDs", { exact: true })
    .fill("11111111111111111111111111111111,22222222222222222222222222222222");
  await page
    .getByLabel("Integration token", { exact: true })
    .fill("fixture-token");
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "persisted property cursor",
  );
  await expect(
    page.getByText(
      "Next: property · property r%3Ai · continuing saved cursor",
      { exact: true },
    ),
  ).toBeVisible();
  let calls = await page.evaluate(() => (window as any).notionCalls);
  expect(
    calls.find((c: any) => c.method === "imports.notion.start").request.roots,
  ).toEqual([
    { kind: "database", id: "11111111111111111111111111111111" },
    { kind: "database", id: "22222222222222222222222222222222" },
  ]);
  expect(calls.some((c: any) => c.method === "imports.notion.finish")).toBe(
    false,
  );
  await page
    .getByRole("button", { name: "Resume capture", exact: true })
    .click();
  await expect(
    page.getByRole("button", {
      name: "Review complete capture inventory",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Integration token", { exact: true }),
  ).toHaveValue("");
  await page
    .getByRole("button", {
      name: "Review complete capture inventory",
      exact: true,
    })
    .click();
  await expect(page.getByRole("status")).toHaveText(
    "Review inventory: Projects, People · partial",
  );
  calls = await page.evaluate(() => (window as any).notionCalls);
  expect(
    calls
      .filter((c: any) => c.method === "imports.notion.step")
      .map((c: any) => c.request.revision),
  ).toEqual([1, 2]);
  expect(
    calls.find((c: any) => c.method === "imports.notion.finish").request
      .allowPartial,
  ).toBe(false);
});
test("blocked source can finish only as explicitly partial inventory", async ({
  page,
}) => {
  await page
    .getByRole("combobox", { name: "Root type", exact: true })
    .selectOption("data_source");
  await page
    .getByLabel("Authorized root IDs", { exact: true })
    .fill("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  await page
    .getByLabel("Integration token", { exact: true })
    .fill("fixture-token");
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page
    .getByRole("button", {
      name: "Finish with explicit partial outcomes",
      exact: true,
    })
    .click();
  await expect(page.getByRole("status")).toContainText("partial");
  const calls = await page.evaluate(() => (window as any).notionCalls);
  expect(
    calls.find((c: any) => c.method === "imports.notion.start").request.roots[0]
      .kind,
  ).toBe("data_source");
  expect(
    calls.find((c: any) => c.method === "imports.notion.finish").request
      .allowPartial,
  ).toBe(true);
  await expect(
    page.getByLabel("Integration token", { exact: true }),
  ).toHaveValue("");
});
