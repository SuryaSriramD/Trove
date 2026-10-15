import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/reminder-fixture.html`,
  );
  await expect(
    page.getByRole("button", {
      name: "Enable background reminders",
      exact: true,
    }),
  ).toBeEnabled();
});
test("uses private 09:00 defaults and makes background delivery an explicit action", async ({
  page,
}) => {
  expect(
    await page.evaluate(() =>
      (window as any).reminderCalls.some(
        (c: any) => c.method === "native.enable",
      ),
    ),
  ).toBe(false);
  await page.getByRole("button", { name: "New reminder", exact: true }).click();
  await expect(page.getByLabel("Reminder time", { exact: true })).toHaveValue(
    "09:00",
  );
  await expect(page.getByLabel("Use private notification text")).toBeChecked();
  await page.getByLabel("Reminder date").fill("2026-11-01");
  await page.getByLabel("Reminder timezone").fill("America/New_York");
  await page.getByLabel("Reminder recurrence").selectOption("weekly");
  await page
    .getByRole("button", { name: "Save reminder", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Edit reminder", exact: true }),
  ).toBeVisible();
  const command = await page.evaluate(
    () =>
      (window as any).reminderCalls.find((c: any) => c.method === "batch")
        .request.commands[0],
  );
  expect(command).toMatchObject({
    kind: "reminder.put",
    localTime: "09:00",
    timezone: "America/New_York",
    frequency: "weekly",
    private: true,
    expectedRevision: "0",
  });
  await page
    .getByRole("button", { name: "Enable background reminders", exact: true })
    .click();
  await expect(
    page.getByRole("button", {
      name: "Disable background reminders",
      exact: true,
    }),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () =>
        (window as any).reminderCalls.filter(
          (c: any) => c.method === "native.enable",
        ).length,
    ),
  ).toBe(1);
});
test("snoozes with a revision and completes through canonical task guards", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Snooze 15 minutes", exact: true })
    .click();
  await expect(page.getByText(/snoozed until/)).toBeVisible();
  await page
    .getByRole("button", { name: "Complete task", exact: true })
    .click();
  const batches = await page.evaluate(() =>
    (window as any).reminderCalls
      .filter((c: any) => c.method === "batch")
      .map((c: any) => c.request),
  );
  expect(batches[0].commands[0]).toMatchObject({
    kind: "reminder.inboxAction",
    expectedActionRevision: 0,
    action: "snoozed",
  });
  expect(batches[1].commands[0].kind).toBe("task.setStatus");
  expect(
    batches[1].mutation.expectedRevisions
      .map((r: any) => r.target.entityType)
      .sort(),
  ).toEqual(["collection", "page", "property", "record"]);
  await page
    .getByRole("button", { name: "Dismiss reminder", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Snooze 15 minutes", exact: true }),
  ).toBeDisabled();
});
test("pages retained inbox entries and rejects late responses from another workspace", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Older reminders", exact: true })
    .click();
  await expect(page.getByText("Older task", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Newer reminders", exact: true })
    .click();
  await expect(page.getByText("Source task", { exact: true })).toBeVisible();
  await page.evaluate(() => {
    (window as any).holdReminderList = true;
  });
  await page
    .getByRole("button", { name: "Older reminders", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveReminderList))
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch fixture workspace", exact: true })
    .click();
  await expect(page.getByText("No due reminders yet.")).toBeVisible();
  await page.evaluate(() => (window as any).resolveReminderList());
  await expect(page.getByText("No due reminders yet.")).toBeVisible();
  await expect(page.getByText("Older task", { exact: true })).toHaveCount(0);
});
