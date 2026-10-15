import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/provider-workspace-fixture.html";
test("pairing configuration and selected context do not cross workspaces", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByLabel("Shared launch", { exact: true }).check();
  await page.getByRole("button", { name: "Preview shared context" }).click();
  await page
    .getByRole("button", { name: "Approve context and pair client" })
    .click();
  await expect(page.getByText(/synthetic-pair-token/)).toBeVisible();
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await expect(page.getByText(/synthetic-pair-token/)).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Preview shared context" }),
  ).toBeDisabled();
});
test("late old-workspace provider status cannot replace current connections", async ({
  page,
}) => {
  await page.goto(url + "?late=1");
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveProviderStatus),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).resolveProviderStatus();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await expect(
    page.getByText("Original client · connected", { exact: true }),
  ).toHaveCount(0);
});
