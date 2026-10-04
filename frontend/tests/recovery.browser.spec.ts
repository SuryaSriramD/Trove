import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto(`${process.env.TROVE_RECOVERY_TEST_URL ?? "http://127.0.0.1:4186"}/tests/recovery-fixture.html`);
  await page.getByLabel("Page history").selectOption({ label: "Historical record" });
  await page.getByRole("button", { name: "Load revisions", exact: true }).click();
  await page.getByRole("button", { name: /^Preview revision 2/ }).click();
});

test("maps a typed value only after reviewing the latest decision", async ({ page }) => {
  const restore = page.getByRole("button", { name: "Restore this revision", exact: true });
  await expect(restore).toBeDisabled();
  await expect(page.getByText("Saved value: 12", { exact: true })).toBeVisible();
  await page.getByLabel("Recovery choice for Amount").selectOption("map");
  await page.getByLabel("Mapped value for Amount").fill("34");
  await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  await expect(restore).toBeEnabled();
  await page.getByLabel("Mapped value for Amount").fill("35");
  await expect(restore).toBeDisabled();
  await page.evaluate(() => { (window as any).failNextRecoveryReview = true; });
  await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Property changed");
  await expect(restore).toBeDisabled();
  await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  await expect(restore).toBeEnabled();
  await restore.click();
  await expect(page.getByText("Page restored as a new revision.", { exact: true })).toBeVisible();
  const calls = await page.evaluate(() => (window as any).recoveryCalls);
  const previews = calls.filter((call: any) => call.method === "history.previewRestore");
  expect(previews.at(-1).request.propertyDecisions[0]).toMatchObject({ action: "map", value: { decimal: "35", format: "percent" } });
  const command = calls.find((call: any) => call.method === "batch").request.commands[0];
  expect(command).toMatchObject({ kind: "history.restore", restoreRevision: "2", previewId: "019a1234-5678-7000-8000-000000000023" });
  expect(command).not.toHaveProperty("propertyDecisions");
});

test("requires an explicit retention choice and displays its reviewed effect", async ({ page }) => {
  const review = page.getByRole("button", { name: "Review property decisions", exact: true });
  await expect(review).toBeDisabled();
  await page.getByLabel("Recovery choice for Amount").selectOption("retain");
  await expect(page.getByLabel("Mapped value for Amount")).toHaveCount(0);
  await review.click();
  await expect(page.getByText("Retain old value as inert evidence; keep current value.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Restore this revision", exact: true }).click();
  const calls = await page.evaluate(() => (window as any).recoveryCalls);
  const preview = calls.filter((call: any) => call.method === "history.previewRestore").at(-1);
  expect(preview.request.propertyDecisions).toEqual([{ sourcePropertyId: "019a1234-5678-7000-8000-000000000004", action: "retain" }]);
});

test("keeps unknown historical payloads readable without assuming a current shape", async ({ page }) => {
  await page.evaluate(() => { (window as any).opaqueHistoricalEvidence = true; });
  await page.getByRole("button", { name: /^Preview revision 2/ }).click();
  await expect(page.getByText('Saved value: {"sourcePayload":"legacy opaque value"}', { exact: true })).toBeVisible();
  await page.getByLabel("Recovery choice for Amount").selectOption("map");
  await expect(page.getByLabel("Mapped value for Amount")).toHaveValue("");
  await page.getByLabel("Mapped value for Amount").fill("21");
  await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  await expect(page.getByRole("button", { name: "Restore this revision", exact: true })).toBeEnabled();
});
