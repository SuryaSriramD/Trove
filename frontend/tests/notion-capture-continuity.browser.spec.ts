import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191"}/tests/notion-capture-continuity-fixture.html`,
  );
  await page
    .getByText("Optional authenticated Notion capture", { exact: true })
    .click();
});
async function enter(page: any) {
  await page
    .getByLabel("Authorized root IDs")
    .fill("01990000-0000-7000-8000-000000000030");
  await page.getByLabel("Integration token").fill("fixture-secret-value");
}
for (const action of ["Switch workspace", "Change scope"])
  test(`${action} clears transient Notion token and previous capture`, async ({
    page,
  }) => {
    await enter(page);
    await page.getByRole("button", { name: action, exact: true }).click();
    await page
      .locator("details")
      .evaluate((el) => ((el as HTMLDetailsElement).open = true));
    await expect(page.getByLabel("Integration token")).toHaveValue("");
    await expect(page.getByLabel("Authorized root IDs")).toHaveValue("");
  });
test("a no-progress Notion capture stops after one fetch", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).noProgress = true;
  });
  await enter(page);
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("no progress");
  expect(await page.evaluate(() => (window as any).notionSteps)).toBe(1);
});
test("pause retains completed batch and resume continues it", async ({
  page,
}) => {
  await enter(page);
  await page.evaluate(() => {
    (window as any).delayStep = true;
  });
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveStep))
    .toBe("function");
  await page.getByRole("button", { name: "Pause after current fetch" }).click();
  await page.evaluate(() => {
    (window as any).delayStep = false;
    (window as any).resolveStep();
  });
  await expect(
    page.getByRole("button", { name: "Resume capture", exact: true }),
  ).toBeEnabled();
  expect(await page.evaluate(() => (window as any).notionSteps)).toBe(1);
  await page
    .getByRole("button", { name: "Resume capture", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Review complete capture inventory" })
    .click();
  expect(await page.evaluate(() => (window as any).notionPlans.length)).toBe(1);
});

test("late batch and import-plan responses cannot cross workspace boundaries", async ({
  page,
}) => {
  await enter(page);
  await page.evaluate(() => {
    (window as any).delayStep = true;
  });
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveStep))
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).delayStep = false;
    (window as any).resolveStep();
  });
  await page.locator("details").evaluate((el) => {
    (el as HTMLDetailsElement).open = true;
  });
  await enter(page);
  await page
    .getByRole("button", { name: "Capture selected roots", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).delayFinish = true;
  });
  await page
    .getByRole("button", { name: "Review complete capture inventory" })
    .click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveFinish))
    .toBe("function");
  await page.getByRole("button", { name: "Change scope", exact: true }).click();
  await page.evaluate(() => {
    (window as any).resolveFinish();
  });
  await expect
    .poll(() => page.evaluate(() => (window as any).notionPlans.length))
    .toBe(0);
  const calls = await page.evaluate(() => (window as any).notionCalls);
  expect(
    calls.filter((c: any) => c.method === "imports.notion.step"),
  ).toHaveLength(2);
  expect(
    calls.filter((c: any) => c.method === "imports.notion.status"),
  ).toHaveLength(0);
});
