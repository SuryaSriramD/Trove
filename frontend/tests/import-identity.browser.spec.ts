import { test, expect } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL}/tests/import-identity.fixture.html`,
  );
});
test("manual link is explicit and opens baseline-unavailable content review", async ({
  page,
}) => {
  await expect(
    page.getByText("Review source identities", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", {
      name: "Link existing page for Renamed.html",
      exact: true,
    }),
  ).toBeDisabled();
  await page.getByLabel("Existing page for Renamed.html").selectOption(id(3));
  await page
    .getByRole("button", {
      name: "Link existing page for Renamed.html",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("baseline unavailable", { exact: false }).first(),
  ).toBeVisible();
  const calls = await page.evaluate(() => (window as any).identityCalls);
  const r = calls.find((x: any) => x.method === "imports.configure").req
    .identityResolutions[0];
  expect(r.identityChoice).toEqual({
    choice: "link_existing",
    localKind: "page",
    localId: id(3),
  });
  expect(r.expectedLocalRevision).toBe("4");
  expect(r.incomingSourceRevision).toBe("b".repeat(64));
  await expect(page.locator('option[value="duplicate"]')).toHaveCount(0);
  await page
    .getByLabel("Resolution for Renamed.html /title")
    .selectOption("keep_local");
  await page.reload();
  await expect(
    page.getByText(/Identity decision: link existing/),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Commit reviewed import", exact: true })
    .click();
  await expect(
    page.getByText("Import processing finished. Review all outcomes below."),
  ).toBeVisible();
});
test("explicit create-new and skip bind only the proposed source identity", async ({
  page,
}) => {
  await page
    .getByRole("button", {
      name: "Create new page for Renamed.html",
      exact: true,
    })
    .click();
  let calls = await page.evaluate(() => (window as any).identityCalls);
  expect(
    calls.find((x: any) => x.method === "imports.configure").req
      .identityResolutions[0].identityChoice,
  ).toEqual({ choice: "create_new", localKind: "page", localId: id(7) });
  await page.evaluate(() => localStorage.removeItem("identity-plan"));
  await page.reload();
  await page
    .getByRole("button", { name: "Skip Renamed.html", exact: true })
    .click();
  calls = await page.evaluate(() => (window as any).identityCalls);
  expect(
    calls.find((x: any) => x.method === "imports.configure").req
      .identityResolutions[0].identityChoice,
  ).toEqual({ choice: "skip" });
  await expect(page.getByText(/Identity decision: skip/)).toBeVisible();
  await page
    .getByRole("button", { name: "Commit reviewed import", exact: true })
    .click();
  await expect(
    page.getByText("Import processing finished. Review all outcomes below."),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Open imported pages", exact: true }),
  ).toHaveCount(0);
});
test("stale target fails visibly without silently applying import", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).identityStale = true;
  });
  await page.getByLabel("Existing page for Renamed.html").selectOption(id(3));
  await page
    .getByRole("button", {
      name: "Link existing page for Renamed.html",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Selected local page changed; review its current revision"),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        (window as any).identityCalls.filter(
          (x: any) => x.method === "imports.commit",
        ).length,
    ),
  ).toBe(0);
});
