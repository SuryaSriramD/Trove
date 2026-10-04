import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/managed-providers-fixture.html`,
  );
});
async function selectRun(page: any) {
  await page.getByRole("button", { name: "Check installed Codex" }).click();
  await page.getByRole("button", { name: "Use this account" }).first().click();
  await page
    .getByRole("button", { name: "Load models for selected account" })
    .click();
  await page.getByLabel("Account model").selectOption("model-a");
  await page.getByLabel("Launch", { exact: true }).check();
  await page
    .getByRole("button", { name: "Review context for this run" })
    .click();
  await page
    .getByLabel("Request to Codex", { exact: true })
    .fill("Summarize Launch and suggest a follow-up task.");
}
test("account/model/context require explicit choices; cancellation retains partial output", async ({
  page,
}) => {
  const start = page.getByRole("button", {
    name: "Start with selected account and context",
  });
  await expect(start).toBeDisabled();
  await selectRun(page);
  await expect(
    page.getByLabel("Private journal", { exact: true }),
  ).not.toBeChecked();
  await expect(
    page.getByLabel("Propose page and task creation for approval"),
  ).not.toBeChecked();
  expect(
    await page.evaluate(() =>
      (window as any).managedCalls.filter(
        (c: any) => c.method === "providers.start",
      ),
    ),
  ).toHaveLength(0);
  await start.click();
  await expect(
    page.getByText("Partial response", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Use this account" }).last(),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Cancel provider run" }).click();
  await expect(
    page.getByText(
      "Provider execution was cancelled; partial output is retained",
    ),
  ).toBeVisible();
  await expect(
    page.getByText("Partial response", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", {
      name: "Continue this session with reviewed context",
    }),
  ).toBeDisabled();
  const calls = await page.evaluate(() => (window as any).managedCalls);
  const request = calls.find(
    (c: any) => c.method === "providers.start",
  ).request;
  expect(request.billingMode).toBe("chatgpt_plan");
  expect(request.model).toBe("model-a");
  expect(request.pageIds).toHaveLength(1);
  expect(request.tools).toEqual(["readPage", "search.query"]);
  expect(request.previewHash).toBe("b".repeat(64));
  expect(calls.some((c: any) => c.method === "providers.approve")).toBe(false);
});
test("account switching clears models and context; stale preview failure is visible", async ({
  page,
}) => {
  await selectRun(page);
  await page.evaluate(() => ((window as any).stalePreview = true));
  await page
    .getByRole("button", { name: "Start with selected account and context" })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Selected content changed",
  );
  await page.getByRole("button", { name: "Use this account" }).last().click();
  await expect(page.getByLabel("Account model")).toHaveValue("");
  await expect(
    page.getByRole("heading", { name: "Context to share" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", {
      name: "Start with selected account and context",
    }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Load models for selected account" })
    .click();
  await expect(page.getByRole("option", { name: /model-a/ })).toHaveCount(0);
  await expect(page.getByRole("option", { name: /model-b/ })).toHaveCount(1);
});
