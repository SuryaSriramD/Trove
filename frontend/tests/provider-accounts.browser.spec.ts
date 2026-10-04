import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_AUTH_TEST_URL ?? process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/provider-accounts-fixture.html`,
  );
});
test("account selection and sign-out stay isolated even with matching email", async ({
  page,
}) => {
  const cards = page.getByRole("article");
  await expect(cards).toHaveCount(2);
  await expect(
    cards.nth(1).getByRole("button", { name: "Use this account", exact: true }),
  ).toBeDisabled();
  await cards
    .nth(0)
    .getByRole("button", { name: "Use this account", exact: true })
    .click();
  await expect(page.getByLabel("Selected registration")).toHaveText(
    "019a1234-5678-7000-8000-000000000003",
  );
  await cards
    .nth(0)
    .getByRole("button", { name: "Sign out this account", exact: true })
    .click();
  await expect(page.getByLabel("Selected registration")).toHaveText("None");
  await expect(
    page.getByText(/Remote revocation was not confirmed/),
  ).toBeVisible();
  await expect(cards.nth(1)).toContainText(
    "Signed in · ChatGPT plan usage disabled",
  );
  const calls = await page.evaluate(() => (window as any).accountCalls);
  expect(
    calls.find((c: any) => c.method === "providers.auth.signOut").request
      .registrationId,
  ).toBe("019a1234-5678-7000-8000-000000000003");
});
test("browser sign-in is cancellable and never silently selects an account", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Continue with ChatGPT", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Continue with ChatGPT", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Cancel sign-in", exact: true })
    .click();
  await expect(
    page.getByText("Sign-in cancelled", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Continue with ChatGPT", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).finishAccount = true;
  });
  await expect(page.getByRole("article")).toHaveCount(3);
  await expect(page.getByLabel("Selected registration")).toHaveText("None");
  const calls = await page.evaluate(() => (window as any).accountCalls);
  expect(calls.some((c: any) => c.method === "providers.auth.cancel")).toBe(
    true,
  );
  expect(calls.some((c: any) => !c.method.startsWith("providers.auth."))).toBe(
    false,
  );
});

test("failed credential cleanup refreshes disabled state and remains retryable", async ({
  page,
}) => {
  const first = page.getByRole("article").nth(0);
  await page.evaluate(() => {
    (window as any).failCleanup = true;
  });
  await first
    .getByRole("button", { name: "Sign out this account", exact: true })
    .click();
  await expect(first).toContainText("Local credential cleanup required");
  await expect(
    first.getByRole("button", { name: "Use this account", exact: true }),
  ).toBeDisabled();
  await expect(
    first.getByRole("button", { name: "Sign in again", exact: true }),
  ).toBeDisabled();
  await page.evaluate(() => {
    (window as any).failCleanup = false;
  });
  await first
    .getByRole("button", { name: "Sign out this account", exact: true })
    .click();
  await expect(first).toContainText("Signed out");
});
