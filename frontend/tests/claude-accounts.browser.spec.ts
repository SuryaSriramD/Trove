import { test, expect } from "@playwright/test";
const base = process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173";
test("Claude profiles retain explicit billing and never start sign-in or inference automatically", async ({
  page,
}) => {
  await page.goto(`${base}/tests/claude-accounts-fixture.html`);
  await page.getByRole("button", { name: "Use Work", exact: true }).click();
  await expect(page.getByLabel("Selected Claude profile")).toHaveText(
    "019a1234-5678-7000-8000-000000000003|claude_subscription",
  );
  await expect(
    page.getByRole("button", { name: "Use API", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Refresh API status", exact: true })
    .click();
  await page.getByRole("button", { name: "Use API", exact: true }).click();
  await expect(page.getByLabel("Selected Claude profile")).toHaveText(
    "019a1234-5678-7000-8000-000000000004|anthropic_api",
  );
  await page.getByLabel("Claude profile label").fill("Project");
  await page.getByLabel("Claude billing route").selectOption("anthropic_api");
  await page
    .getByRole("button", { name: "Create Claude profile", exact: true })
    .click();
  await expect(page.getByRole("article")).toHaveCount(3);
  const calls = await page.evaluate(() => (window as any).claudeCalls);
  expect(
    calls.find((c: any) => c.method === "providers.claude.create").request
      .billingMode,
  ).toBe("anthropic_api");
  expect(
    calls.some(
      (c: any) =>
        c.method === "providers.claude.login" || c.method === "providers.start",
    ),
  ).toBe(false);
  expect(await page.getByRole("textbox").count()).toBe(1);
});
test("Claude terminal login cancellation and failed logout disable selection", async ({
  page,
}) => {
  await page.goto(`${base}/tests/claude-accounts-fixture.html`);
  await page.getByRole("button", { name: "Use Work", exact: true }).click();
  await page
    .getByRole("button", { name: "Sign in to Work in Terminal", exact: true })
    .click();
  await expect(page.getByLabel("Selected Claude profile")).toHaveText("None");
  await expect(
    page.getByRole("button", { name: "Use Work", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", {
      name: "Sign in to API in Terminal",
      exact: true,
    }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Cancel Work sign-in", exact: true })
    .click();
  await expect(
    page.getByText("Vendor login was cancelled", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Refresh Work status", exact: true })
    .click();
  await page.getByRole("button", { name: "Use Work", exact: true }).click();
  await page.evaluate(() => {
    (window as any).failLogout = true;
  });
  await page
    .getByRole("button", { name: "Sign out of Work", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "could not complete logout",
  );
  await expect(
    page.getByRole("button", {
      name: "Sign in to Work in Terminal",
      exact: true,
    }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Use Work", exact: true }),
  ).toBeDisabled();
  await expect(page.getByLabel("Selected Claude profile")).toHaveText("None");
  await page.evaluate(() => {
    (window as any).failLogout = false;
  });
  await page
    .getByRole("button", { name: "Sign out of Work", exact: true })
    .click();
  await expect(page.getByRole("article").first()).toContainText("Signed out");
  const calls = await page.evaluate(() => (window as any).claudeCalls);
  expect(
    calls.every((c: any) => c.method.startsWith("providers.claude.")),
  ).toBe(true);
});
test("Unqualified native startup leaves saved Claude profiles read-only", async ({
  page,
}) => {
  await page.goto(`${base}/tests/claude-accounts-fixture.html?unavailable`);
  await expect(page.getByRole("article")).toHaveCount(2);
  await expect(page.getByText(/Existing profiles are read-only/)).toBeVisible();
  for (const button of await page.getByRole("button").all())
    await expect(button).toBeDisabled();
  await expect(page.getByLabel("Claude profile label")).toBeDisabled();
  expect(
    (await page.evaluate(() => (window as any).claudeCalls)).map(
      (c: any) => c.method,
    ),
  ).toEqual(["providers.claude.list"]);
});
