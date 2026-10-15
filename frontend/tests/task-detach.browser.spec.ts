import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/task-detach-fixture.html";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
test("canonical task controls review every occurrence and apply the exact chosen plan", async ({
  page,
}) => {
  await page.goto(url);
  await page
    .getByRole("button", { name: "Detach task facet for Publish Friday" })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Detach task facet: Publish Friday",
  });
  await expect(dialog).toContainText("due 2026-10-16");
  await expect(page.getByLabel("Reminder effects")).toContainText(
    "will become eligible again",
  );
  await expect(
    page.getByRole("button", { name: "Apply reviewed detachment" }),
  ).toBeDisabled();
  await expect(
    page
      .getByLabel(`Occurrence ${id(13)} action`)
      .locator('option[value="remove"]'),
  ).toHaveJSProperty("disabled", true);
  await page.getByLabel(`Occurrence ${id(11)} action`).selectOption("remove");
  await expect(page.getByLabel(`Occurrence ${id(11)} action`)).toHaveValue(
    "remove",
  );
  await page
    .getByRole("checkbox", {
      name: "I reviewed all occurrence changes and want to detach this task facet.",
    })
    .check();
  await page.getByRole("button", { name: "Apply reviewed detachment" }).click();
  await expect(page.getByText("Record retained; no task facet")).toBeVisible();
  const requests = await page.evaluate(() => (window as any).fixture.commands);
  expect(requests).toHaveLength(1);
  expect(requests[0].commands[0].kind).toBe("task.detach.apply");
  expect(
    requests[0].commands[0].plan.occurrences.map((o: any) => o.action),
  ).toEqual(["remove", "convert"]);
  expect(requests[0].mutation.expectedRevisions).toEqual(
    requests[0].commands[0].plan.requiredRevisions,
  );
});
test("no-occurrence detach still requires review and cancellation writes nothing", async ({
  page,
}) => {
  await page.goto(url + "?empty=1");
  await page
    .getByRole("button", { name: "Detach task facet for Publish Friday" })
    .click();
  await expect(
    page.getByText("No retained todo occurrences reference this task."),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Cancel detachment", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(
    await page.evaluate(() => (window as any).fixture.commands),
  ).toHaveLength(0);
});
test("cancelled or foreign-grant late previews cannot reappear", async ({
  page,
}) => {
  for (const action of ["Cancel detachment review", "Switch grant"]) {
    await page.goto(url + "?delay=1");
    await page
      .getByRole("button", { name: "Detach task facet for Publish Friday" })
      .click();
    await expect(
      page.getByText("Preparing complete occurrence review…"),
    ).toBeVisible();
    await page.getByRole("button", { name: action, exact: true }).click();
    await page.evaluate(() => (window as any).fixture.release());
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(
      await page.evaluate(() => (window as any).fixture.commands),
    ).toHaveLength(0);
  }
});
test("stale detachment requires a new preview and cannot retain approval", async ({
  page,
}) => {
  await page.goto(url + "?stale=1");
  await page
    .getByRole("button", { name: "Detach task facet for Publish Friday" })
    .click();
  await page
    .getByRole("checkbox", {
      name: "I reviewed all occurrence changes and want to detach this task facet.",
    })
    .check();
  await page.getByRole("button", { name: "Apply reviewed detachment" }).click();
  await expect(page.getByRole("alert")).toContainText("Task changed");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Detach task facet for Publish Friday" })
    .click();
  await expect(
    page.getByRole("button", { name: "Apply reviewed detachment" }),
  ).toBeDisabled();
});
