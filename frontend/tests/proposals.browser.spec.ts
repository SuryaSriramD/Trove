import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/proposals-fixture.html`,
  );
});
test("named local plan has readable review without internal command entry and requires approval", async ({
  page,
}) => {
  const panel = page.getByRole("region", {
    name: "Local AI and approved memory",
  });
  await expect(
    panel.getByText(
      "Your workspace plan is ready to review. No changes have been applied.",
    ),
  ).toBeVisible();
  await panel
    .getByText("Review a page or task change", { exact: true })
    .click();
  await expect(panel.getByLabel("Reviewed draft text")).toHaveCount(0);
  await expect(panel.getByLabel("Reviewed action")).toHaveCount(0);
  await panel.getByRole("button", { name: "Preview exact changes" }).click();
  await expect(panel.locator(".ai-proposal")).toContainText(
    "Create page: Checklist → Launch",
  );
  await expect(panel.locator(".ai-proposal")).toContainText(
    "Verify restored files.",
  );
  let calls = await page.evaluate(() => (window as any).proposalCalls);
  expect(calls.filter((c: any) => c.method === "ai.applyAction")).toHaveLength(
    0,
  );
  expect(
    calls.find((c: any) => c.method === "ai.prepareAction").request.kind,
  ).toBe("workspace_patch");
  await panel
    .getByRole("button", { name: "Approve and apply these changes" })
    .click();
  calls = await page.evaluate(() => (window as any).proposalCalls);
  expect(calls.filter((c: any) => c.method === "ai.applyAction")).toHaveLength(
    1,
  );
});
test("paired client changes show destinations and remain unapplied until review", async ({
  page,
}) => {
  const panel = page.locator(".provider-panel");
  await expect(
    panel.getByRole("heading", { name: "Create page: Checklist" }),
  ).toBeVisible();
  await expect(panel.getByText("Destination: Launch")).toBeVisible();
  await expect(
    panel.getByLabel(
      "Propose named page, task and database changes for review",
    ),
  ).not.toBeChecked();
  await panel
    .getByLabel("Propose named page, task and database changes for review")
    .check();
  await expect(
    panel.getByText(
      "New pages and tasks require approval in a selected destination.",
      { exact: false },
    ),
  ).toBeVisible();
  expect(
    await page.evaluate(() =>
      (window as any).proposalCalls.filter(
        (c: any) => c.method === "providers.approve",
      ),
    ),
  ).toHaveLength(0);
  await panel
    .getByRole("button", { name: "Approve these exact changes" })
    .click();
  expect(
    await page.evaluate(() =>
      (window as any).proposalCalls.filter(
        (c: any) => c.method === "providers.approve",
      ),
    ),
  ).toHaveLength(1);
});
