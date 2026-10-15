import { test, expect } from "@playwright/test";

test("record layouts persist inheritance and reset; complete schedules cancel, apply and undo", async ({
  page,
}) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4283"}/tests/layout-scheduling.fixture.html`,
  );
  await page
    .getByText("Edit collection record layout", { exact: true })
    .click();
  await page.getByRole("button", { name: "Add layout section" }).click();
  await page.getByLabel("Section 1 title").fill("Schedule");
  await page.getByLabel("Section 1 placement").selectOption("sidebar");
  const section = await page
    .getByLabel("Layout placement for Date")
    .locator("option")
    .last()
    .getAttribute("value");
  await page.getByLabel("Layout placement for Date").selectOption(section!);
  await page.getByLabel("Layout placement for Notes").selectOption("hidden");
  await page.getByRole("button", { name: "Save collection layout" }).click();
  await page.getByRole("button", { name: "Open Plan record" }).click();
  const record = page.getByRole("region", {
    name: "Record properties",
    exact: true,
  });
  await expect(record.getByRole("heading", { name: "Schedule" })).toBeVisible();
  await expect(record.locator(".record-layout-sidebar")).toContainText("Date");
  await expect(record.getByLabel("Notes on record page")).toHaveCount(0);
  await record
    .getByText("Customize this record layout", { exact: true })
    .click();
  await record.getByLabel("Layout placement for Date").selectOption("hidden");
  await record
    .getByLabel("Layout placement for Notes")
    .selectOption("unplaced");
  await record.getByRole("button", { name: "Save record override" }).click();
  await expect(record.getByLabel("Notes on record page")).toHaveValue(
    "Retain these words",
  );
  await page.reload();
  await page.getByRole("button", { name: "Open Plan record" }).click();
  await expect(record.getByLabel("Notes on record page")).toHaveValue(
    "Retain these words",
  );
  await record
    .getByText("Customize this record layout", { exact: true })
    .click();
  await record
    .getByRole("button", { name: "Reset to collection default" })
    .click();
  await expect(record.getByLabel("Notes on record page")).toHaveCount(0);
  expect(
    await page.evaluate(
      () =>
        JSON.parse(localStorage.getItem("g10-fixture")!).records[0].values[1]
          .value[0].text,
    ),
  ).toBe("Retain these words");
  await page.getByRole("button", { name: "Open collection" }).click();
  await page.getByText("Reschedule dependencies", { exact: true }).click();
  await page
    .getByLabel("Reschedule starting record")
    .selectOption({ label: "Plan" });
  await page.getByLabel("Reschedule new start").fill("2026-10-15");
  await page.getByLabel("Reschedule timezone").fill("UTC");
  await page.getByRole("button", { name: "Preview complete schedule" }).click();
  const preview = page.getByRole("region", {
    name: "Complete scheduling preview",
  });
  await expect(preview).toContainText("2026-10-17");
  await expect(preview).toContainText("2 inclusive eligible dates");
  const mutations = await page.evaluate(
    () => JSON.parse(localStorage.getItem("g10-fixture")!).mutations,
  );
  await page.getByRole("button", { name: "Cancel schedule" }).click();
  await expect(preview).toHaveCount(0);
  expect(
    await page.evaluate(
      () => JSON.parse(localStorage.getItem("g10-fixture")!).mutations,
    ),
  ).toBe(mutations);
  await page.getByRole("button", { name: "Preview complete schedule" }).click();
  await page.getByRole("button", { name: "Apply reviewed schedule" }).click();
  await expect(preview).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0);
  expect(
    await page.evaluate(
      () =>
        JSON.parse(localStorage.getItem("g10-fixture")!).records[1].values[0]
          .value.start,
    ),
  ).toBe("2026-10-17");
  await page.getByRole("button", { name: "Open Plan record" }).click();
  await page.getByText("Grouped changes and undo", { exact: true }).click();
  await page
    .locator("details > ul > li")
    .filter({ hasText: "Reschedule dependencies" })
    .getByRole("button", { name: "Preview undo" })
    .click();
  await expect(
    page.getByRole("region", { name: "Review grouped undo" }),
  ).toContainText("2026-10-12");
  await page.getByRole("button", { name: "Apply reviewed undo" }).click();
  expect(
    await page.evaluate(
      () =>
        JSON.parse(localStorage.getItem("g10-fixture")!).records[0].values[0]
          .value.start,
    ),
  ).toBe("2026-10-12");
});
