import { test, expect } from "@playwright/test";

const url = `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197"}/tests/qualification-performance-fixture.html`;

test("qualification observes trusted browser input and scroll while retaining only fixed diagnostics", async ({ page }) => {
  await page.goto(url);
  await expect.poll(() => page.evaluate(() => (window as any).qualificationFixture.diagnostics.length)).toBeGreaterThan(0);
  await page.getByRole("textbox", { name: "Qualification input" }).pressSequentially("private-canary");
  await page.getByRole("button", { name: "Observe frames" }).click();
  await page.locator("#scroll").hover();
  await page.mouse.wheel(0, 300);
  await expect.poll(() => page.evaluate(() => [...new Set((window as any).qualificationFixture.metrics.map((x: any) => x[0]))])).toEqual(expect.arrayContaining([
    "input_to_frames", "scroll_to_frames", "database_load_to_frames", "database_response_to_frames",
  ]));
  await page.getByRole("textbox").dispatchEvent("input");
  await page.locator("#scroll").dispatchEvent("scroll");
  await expect.poll(() => page.evaluate(() => (window as any).qualificationFixture.diagnostics.at(-1).counters.input_ignored)).toBe(1);
  const state = await page.evaluate(() => (window as any).qualificationFixture);
  expect(state.diagnostics.at(-1).counters.input_trusted).toBeGreaterThan(0);
  expect(state.diagnostics.at(-1).counters.scroll_ignored).toBe(1);
  expect(state.diagnostics.at(-1).counters.records_accepted).toBeGreaterThan(0);
  expect(JSON.stringify(state)).not.toContain("private-canary");
});

test("ordinary browser mode emits no qualification samples or diagnostic traffic", async ({ page }) => {
  await page.goto(`${url}?disabled`);
  await page.getByRole("textbox").pressSequentially("private-canary");
  await page.getByRole("button", { name: "Observe frames" }).click();
  await page.locator("#scroll").hover();
  await page.mouse.wheel(0, 300);
  // Give the instrumented mode's two frame opportunities and diagnostic period
  // time to elapse; the disabled mode must never install either observer.
  await page.waitForTimeout(650);
  const state = await page.evaluate(() => (window as any).qualificationFixture);
  expect(state).toEqual({ metrics: [], diagnostics: [] });
});
