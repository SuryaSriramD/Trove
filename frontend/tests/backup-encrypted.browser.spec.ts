import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => { await page.goto(`${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/backup-encrypted-fixture.html`); });

test("creates through the native bridge and clears both passphrase inputs", async ({ page }) => {
  await page.getByLabel("New backup passphrase").fill("test-passphrase");
  await page.getByLabel("Confirm backup passphrase").fill("mismatch");
  await page.getByRole("button", { name: "Create encrypted backup…", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("matching confirmation");
  expect(await page.evaluate(() => (window as any).backupCalls.includes("create"))).toBe(false);
  await expect(page.getByLabel("New backup passphrase")).toHaveValue("");
  await expect(page.getByLabel("Confirm backup passphrase")).toHaveValue("");
  await page.getByLabel("New backup passphrase").fill("test-passphrase");
  await page.getByLabel("Confirm backup passphrase").fill("test-passphrase");
  await page.getByRole("button", { name: "Create encrypted backup…", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Encrypted backup saved");
  await expect(page.getByLabel("New backup passphrase")).toHaveValue("");
  expect(await page.evaluate(() => (window as any).backupCalls.filter((value: string) => value === "create").length)).toBe(1);
});

test("reviews authenticated contents and disposes cancelled previews", async ({ page }) => {
  await page.getByLabel("Encrypted backup passphrase", { exact: true }).fill("test-passphrase");
  await page.getByRole("button", { name: "Choose encrypted backup to review…", exact: true }).click();
  await expect(page.getByLabel("Encrypted backup passphrase", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("Native restore preview")).toContainText("authenticated");
  await page.getByRole("button", { name: "Cancel review", exact: true }).click();
  await expect(page.getByLabel("Native restore preview")).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).backupCalls)).toContain("close:review-1");
  await page.getByLabel("Encrypted backup passphrase", { exact: true }).fill("test-passphrase");
  await page.getByRole("button", { name: "Choose encrypted backup to review…", exact: true }).click();
  await page.getByRole("button", { name: "Restore into a new folder…", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Restored and verified");
  expect(await page.evaluate(() => (window as any).backupCalls)).toContain("restore:review-1");
});

test("shows native progress and cancels without retaining an entered passphrase", async ({ page }) => {
  await page.evaluate(() => { (window as any).holdBackup = true; });
  await page.getByLabel("New backup passphrase").fill("test-passphrase");
  await page.getByLabel("Confirm backup passphrase").fill("test-passphrase");
  await page.getByRole("button", { name: "Create encrypted backup…", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Authenticating");
  await expect(page.getByLabel("Backup operation progress")).toHaveAttribute("value", "50");
  await expect(page.getByLabel("New backup passphrase")).toHaveValue("");
  await page.getByRole("button", { name: "Cancel backup operation", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Backup cancelled");
  await expect(page.getByRole("button", { name: "Create encrypted backup…", exact: true })).toBeEnabled();
});
