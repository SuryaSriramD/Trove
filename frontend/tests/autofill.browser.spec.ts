import { test, expect } from "@playwright/test";
test("autofill reviews blanks and explicit overwrites, resumes saved cells and undoes", async ({
  page,
}) => {
  await page.goto(
    (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
      "/tests/autofill-fixture.html",
  );
  await page.getByText("AI autofill", { exact: true }).click();
  await page
    .getByRole("group", { name: "Rows in this window" })
    .getByLabel("Blank row")
    .check();
  await page
    .getByRole("group", { name: "Rows in this window" })
    .getByLabel("Manual row")
    .check();
  await page
    .getByRole("group", { name: "Source fields" })
    .getByLabel("Title")
    .check();
  await page
    .getByRole("group", { name: "Target fields" })
    .getByLabel("Summary")
    .check();
  await page
    .getByLabel("Also generate suggestions for existing values", {
      exact: false,
    })
    .check();
  await page
    .getByRole("button", { name: "Prepare autofill", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Generate suggestions", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Cancel generation", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Resume remaining cells" }),
  ).toBeVisible();
  await page.reload();
  await page.getByText("AI autofill", { exact: true }).click();
  await page.getByLabel("Autofill history").selectOption({ index: 1 });
  await page.getByRole("button", { name: "Resume remaining cells" }).click();
  await expect(
    page.getByRole("region", { name: "Review overwrites", exact: true }),
  ).toContainText("Manual fact");
  await expect(
    page.getByRole("button", { name: "Apply 1 reviewed values atomically" }),
  ).toBeVisible();
  await page
    .getByLabel("Approve overwrite of Manual row · Summary", { exact: true })
    .check();
  await page
    .getByRole("button", { name: "Apply 2 reviewed values atomically" })
    .click();
  await expect(
    page.getByText("2 values applied.", { exact: false }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Undo autofill values" }).click();
  await expect
    .poll(() => page.evaluate(() => (window as any).autofillFixture.undone))
    .toBe(1);
  await expect(page.getByRole("alert")).toBeEmpty();
});
