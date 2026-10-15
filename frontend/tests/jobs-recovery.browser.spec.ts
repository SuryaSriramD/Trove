import { test, expect, type Page } from "@playwright/test";
const painted = (page: Page) =>
  page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/jobs-recovery-fixture.html`,
  );
  await expect(page.locator(".job-state")).toHaveText("failed");
});
test("older polling response cannot replace a newer retry result", async ({
  page,
}) => {
  await page.evaluate(() => {
    const w = window as any;
    w.holdNextJobs = true;
    w.emitJobs();
  });
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).releaseJobs))
    .toBe("function");
  await page.getByRole("button", { name: "Retry saved inputs" }).click();
  await expect(page.locator(".job-state")).toHaveText("queued");
  await page.evaluate(() => (window as any).releaseJobs());
  await painted(page);
  expect(await page.locator(".job-state").textContent()).toBe("queued");
});
test("scope change clears prior results and discards old read", async ({
  page,
}) => {
  await page.evaluate(() => {
    const w = window as any;
    w.holdNextJobs = true;
    w.emitJobs();
  });
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).releaseJobs))
    .toBe("function");
  await page.getByRole("button", { name: "Change scope" }).click();
  await page.evaluate(() => (window as any).emitJobs());
  await expect(
    page.getByText("Jobs for second-grant", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => (window as any).releaseJobs());
  await painted(page);
  expect(
    await page.getByText("Jobs for first-grant", { exact: true }).count(),
  ).toBe(0);
});
test("completed old-scope mutation cannot trigger another old-scope read", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).holdMutation = true;
  });
  await page.getByRole("button", { name: "Retry saved inputs" }).click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).releaseMutation))
    .toBe("function");
  await page.getByRole("button", { name: "Change scope" }).click();
  await expect(
    page.getByText("Jobs for second-grant", { exact: true }),
  ).toBeVisible();
  const before = await page.evaluate(() => (window as any).jobCalls.length);
  await page.evaluate(() => (window as any).releaseMutation());
  await painted(page);
  expect(await page.evaluate(() => (window as any).jobCalls.length)).toBe(
    before,
  );
});

test("older polling failure cannot replace a newer successful read", async ({
  page,
}) => {
  await page.evaluate(() => {
    const w = window as any;
    w.holdNextJobs = true;
    w.emitJobs();
  });
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).rejectJobs))
    .toBe("function");
  await page.evaluate(() => {
    const w = window as any;
    w.jobState = "queued";
    w.emitJobs();
  });
  await expect(page.locator(".job-state")).toHaveText("queued");
  await page.evaluate(() => (window as any).rejectJobs());
  await painted(page);
  expect(await page.getByRole("alert").count()).toBe(0);
});
