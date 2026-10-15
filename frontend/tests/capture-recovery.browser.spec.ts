import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/capture-recovery-fixture.html`,
  );
});
const preview = async (page: any) => {
  await page
    .getByLabel("Shared selection", { exact: true })
    .fill("Captured source");
  await page.getByRole("button", { name: "Preview clip", exact: true }).click();
};
test("refuses a capture commit that makes no progress", async ({ page }) => {
  await preview(page);
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("no progress");
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
});
test("workspace switch discards a delayed capture preview", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).delayCapturePreview = true;
  });
  await preview(page);
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCapturePreview),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).resolveCapturePreview();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await expect(
    page.getByRole("button", { name: "Save reviewed capture", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText("Old capture", { exact: false })).toHaveCount(0);
});
test("workspace switch discards delayed native browser drafts", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).delayBrowserDrafts = true;
  });
  await page
    .getByRole("button", { name: "Refresh browser drafts", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveBrowserDrafts),
    )
    .toBe("function");
  await page.evaluate(() => {
    (window as any).savedOldBrowserDrafts = (
      window as any
    ).resolveBrowserDrafts;
    (window as any).delayBrowserDrafts = false;
  });
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).savedOldBrowserDrafts();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await expect(
    page.getByRole("button", { name: "Old browser draft", exact: true }),
  ).toHaveCount(0);
});
test("cancel stops after the current commit batch and retains a resumable review", async ({
  page,
}) => {
  await preview(page);
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Cancel capture save", exact: true })
    .click();
  await page.evaluate(() => (window as any).resolveCaptureCommit());
  await expect(
    page.getByRole("button", { name: "Resume reviewed capture", exact: true }),
  ).toBeEnabled();
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
});

test("browser capture cancellation retains the last committed review", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Old browser draft", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed browser capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Cancel browser capture save", exact: true })
    .click();
  await page.evaluate(() => (window as any).resolveCaptureCommit());
  await expect(
    page.getByRole("button", {
      name: "Resume reviewed browser capture",
      exact: true,
    }),
  ).toBeEnabled();
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
});
test("workspace switch prevents another commit after the in-flight batch", async ({
  page,
}) => {
  await preview(page);
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).resolveCaptureCommit();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  await expect(
    page.getByRole("button", { name: "Resume reviewed capture", exact: true }),
  ).toHaveCount(0);
});
