import { test, expect } from "@playwright/test";
const base = process.env.TROVE_UPDATE_TEST_URL ?? "http://127.0.0.1:4177";
test("requires review, then reveals only the prepared native kit", async ({ page }) => {
  await page.goto(`${base}/tests/update-fixture.html`);
  await page.getByRole("button", { name: "Choose update to verify…", exact: true }).click();
  await expect(page.getByLabel("Application update review")).toContainText("1.0.0 (build 10) → 1.1.0 (build 11)");
  expect(await page.evaluate(() => (window as any).updateCalls)).toEqual([{ method: "PreviewApplicationUpdate", args: [] }]);
  await page.getByRole("button", { name: "Save verified apps and workspace backup…", exact: true }).click();
  await expect(page.getByLabel("Recovery result")).toContainText("Quit Trove");
  await page.getByRole("button", { name: "Verify again and show update in Finder", exact: true }).click();
  expect(await page.evaluate(() => (window as any).updateCalls)).toEqual([{ method: "PreviewApplicationUpdate", args: [] }, { method: "PrepareApplicationUpdate", args: ["native-update-review"] }, { method: "RevealApplicationUpdate", args: ["native-kit-review"] }]);
  await expect(page.getByRole("button", { name: "Verify again and show update in Finder", exact: true })).toHaveCount(0);
});
test("workspace changes invalidate preparation and source failures do not report success", async ({ page }) => {
  await page.goto(`${base}/tests/update-fixture.html`);
  await page.getByRole("button", { name: "Choose update to verify…", exact: true }).click();
  await page.getByRole("button", { name: "Fixture switch workspace", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save verified apps and workspace backup…", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Discard review", exact: true }).click();
  await page.evaluate(() => { (window as any).updateFailure = true; });
  await page.getByRole("button", { name: "Choose update to verify…", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Application changed");
  await expect(page.getByLabel("Recovery result")).toHaveCount(0);
});
test("cancellation is reachable during preparation", async ({ page }) => {
  await page.goto(`${base}/tests/update-fixture.html`);
  await page.getByRole("button", { name: "Choose update to verify…", exact: true }).click();
  await page.evaluate(() => { (window as any).delayUpdate = true; });
  await page.getByRole("button", { name: "Save verified apps and workspace backup…", exact: true }).click();
  await expect(page.getByRole("button", { name: "Review compatible rollback…", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Cancel recovery operation", exact: true }).click();
  await expect(page.getByRole("button", { name: "Review compatible rollback…", exact: true })).toBeEnabled();
  await expect(page.getByLabel("Recovery result")).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).updateCalls.at(-1).method)).toBe("CancelUpdateOperation");
});
test("rollback uses a reviewed backup and explains preserved newer data", async ({ page }) => {
  await page.goto(`${base}/tests/update-fixture.html`);
  await page.getByRole("button", { name: "Review compatible rollback…", exact: true }).click();
  await expect(page.getByLabel("Compatible rollback review")).toContainText("Keep your newer workspace");
  await page.getByRole("button", { name: "Restore compatible workspace into a new folder…", exact: true }).click();
  await expect(page.getByLabel("Recovery result")).toContainText("/selected/kit/previous/Trove.app");
  expect(await page.evaluate(() => (window as any).updateCalls.at(-1))).toEqual({ method: "RestoreApplicationRollback", args: ["native-rollback-review"] });
  await page.getByRole("button", { name: "Open restored workspace with previous app and quit", exact: true }).click();
  expect(await page.evaluate(() => (window as any).updateCalls.at(-1))).toEqual({ method: "LaunchApplicationRollback", args: ["native-launch-review"] });
  await expect(page.getByRole("button", { name: "Open restored workspace with previous app and quit", exact: true })).toHaveCount(0);
});
test("migration recovery remains available when startup has no workspace", async ({ page }) => {
  await page.goto(`${base}/tests/update-fixture.html?startupFailure`);
  await expect(page.getByRole("button", { name: "Choose update to verify…", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Recover an interrupted upgrade…", exact: true }).click();
  await expect(page.getByLabel("Migration recovery review")).toContainText("2 managed file and recording objects");
  await page.getByRole("button", { name: "Restore pre-upgrade workspace into a new folder…", exact: true }).click();
  await expect(page.getByLabel("Recovery result")).toContainText("Use a compatible application");
  expect(await page.evaluate(() => (window as any).updateCalls.at(-1))).toEqual({ method: "RestoreMigrationRecovery", args: ["native-migration-review"] });
});
