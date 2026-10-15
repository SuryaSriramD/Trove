import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/models-fixture.html`,
  );
});
test("keeps cancellation pending until native acquisition stops", async ({
  page,
}) => {
  await page.getByRole("button", { name: "Download", exact: true }).click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("status")).toContainText(
    "cancellation_requested",
  );
  await expect(
    page.getByRole("button", { name: "Download", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Cancelling…", exact: true }),
  ).toBeDisabled();
  await page.evaluate(() =>
    (window as any).emitModelProgress("interrupted", 3),
  );
  await expect(
    page.getByRole("button", { name: "Resume download", exact: true }),
  ).toBeEnabled();
});
test("reopened settings recover an active acquisition from native status", async ({
  page,
}) => {
  await page.getByRole("button", { name: "Download", exact: true }).click();
  await page
    .getByRole("button", { name: "Toggle settings", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Toggle settings", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Cancel", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Download", exact: true }),
  ).toHaveCount(0);
});
test("a delayed start receipt cannot replace a newer completion", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).finishBeforeAcquireResponse = true;
  });
  await page.getByRole("button", { name: "Download", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Remove pack", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Cancel", exact: true }),
  ).toHaveCount(0);
});

test("ignores late progress after completion", async ({ page }) => {
  await page.getByRole("button", { name: "Download", exact: true }).click();
  await page.evaluate(() => (window as any).emitModelProgress("ready", 2));
  await expect(
    page.getByRole("button", { name: "Remove pack", exact: true }),
  ).toBeVisible();
  await page.evaluate(() => (window as any).deliverStaleProgress());
  await expect(
    page.getByRole("button", { name: "Remove pack", exact: true }),
  ).toBeVisible();
});
