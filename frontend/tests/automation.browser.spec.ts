import { test, expect } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197"}/tests/automation-fixture.html`,
  );
});
test("configures weekly schedules, exact typed conditions and record/task actions without JSON", async ({
  page,
}) => {
  await page.getByLabel("Name", { exact: true }).fill("Weekly review");
  await page
    .getByRole("combobox", { name: "Frequency", exact: true })
    .selectOption("weekly");
  await page
    .getByRole("combobox", { name: "Weekday", exact: true })
    .selectOption("1");
  await page.getByLabel("Local time", { exact: true }).fill("09:30");
  await page.getByLabel("Timezone", { exact: true }).fill("Asia/Kolkata");
  await page
    .getByRole("combobox", { name: "Missed runs", exact: true })
    .selectOption("catch_up");
  await page.getByLabel("Maximum catch-up", { exact: true }).fill("2");
  await page.getByLabel("Only run when a record matches").check();
  await page
    .getByRole("combobox", { name: "Condition record", exact: true })
    .selectOption(id(12));
  await page
    .getByRole("combobox", { name: "Condition property", exact: true })
    .selectOption(id(7));
  await page
    .getByLabel("Condition value", { exact: true })
    .fill("9007199254740993.125");
  const action1 = page.getByRole("group", { name: "Action 1", exact: true });
  await action1
    .getByRole("combobox", { name: "Action type", exact: true })
    .selectOption("record.setValue");
  await action1
    .getByRole("combobox", { name: "Target record", exact: true })
    .selectOption(id(12));
  await action1
    .getByRole("combobox", { name: "Property", exact: true })
    .selectOption(id(7));
  await action1.getByLabel("Property value").fill("123.4500");
  await page.getByRole("button", { name: "Add action", exact: true }).click();
  const action2 = page.getByRole("group", { name: "Action 2", exact: true });
  await action2
    .getByRole("combobox", { name: "Action type", exact: true })
    .selectOption("task.setStatus");
  await action2
    .getByRole("combobox", { name: "Target record", exact: true })
    .selectOption(id(12));
  await action2
    .getByRole("combobox", { name: "Task status", exact: true })
    .selectOption(id(10));
  await page
    .getByRole("button", { name: "Save disabled rule", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Weekly review", exact: true }),
  ).toBeVisible();
  const calls = await page.evaluate(() => (window as any).automationCalls);
  const saved = calls.find((c: any) => c.method === "batch").request.commands[0]
    .rule;
  expect(saved).toMatchObject({
    enabled: false,
    paused: false,
    trigger: {
      frequency: "weekly",
      weekday: 1,
      hour: 9,
      minute: 30,
      timezone: "Asia/Kolkata",
      missedPolicy: "catch_up",
      catchUpLimit: 2,
    },
    condition: {
      recordId: id(12),
      propertyId: id(7),
      op: "eq",
      value: { decimal: "9007199254740993.125", format: "number" },
    },
    actions: [
      {
        kind: "record.setValue",
        recordId: id(12),
        propertyId: id(7),
        value: {
          type: "number",
          schemaVersion: 1,
          value: { decimal: "123.4500", format: "number" },
        },
      },
      { kind: "task.setStatus", recordId: id(12), statusOptionId: id(10) },
    ],
  });
  await page.getByRole("button", { name: "Edit rule", exact: true }).click();
  await expect(page.getByLabel("Condition value", { exact: true })).toHaveValue(
    "9007199254740993.125",
  );
  await expect(
    page.getByRole("combobox", { name: "Frequency", exact: true }),
  ).toHaveValue("weekly");
});
test("reviews immutable recurring templates, enables explicitly and retains run history", async ({
  page,
}) => {
  await page.getByLabel("Name", { exact: true }).fill("Weekly template");
  await page
    .getByRole("combobox", { name: "Action type", exact: true })
    .selectOption("template.instantiate");
  await page
    .getByRole("combobox", { name: "Saved template", exact: true })
    .selectOption(id(20));
  await expect(
    page.getByRole("combobox", { name: "Destination database", exact: true }),
  ).toHaveValue(id(5));
  await page
    .getByRole("button", { name: "Save disabled rule", exact: true })
    .click();
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Run this occurrence", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByLabel("Automation preview", { exact: true }),
  ).toContainText("Copy paragraph block");
  await page.getByRole("button", { name: "Enable", exact: true }).click();
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await page
    .getByRole("button", { name: "Run this occurrence", exact: true })
    .click();
  await expect(
    page.getByRole("cell", { name: "succeeded", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Resume", exact: true }),
  ).toBeVisible();
  const calls = await page.evaluate(() => (window as any).automationCalls);
  const saved = calls.find((c: any) => c.method === "batch").request.commands[0]
    .rule;
  expect(saved.actions).toEqual([
    {
      kind: "template.instantiate",
      templateId: id(20),
      templateHash: "a".repeat(64),
      title: "Weekly template · {{occurrence}}",
      collectionId: id(5),
    },
  ]);
  const execute = calls.find(
    (c: any) => c.method === "automation.execute",
  ).request;
  expect(execute.expectedRevisions).toEqual([
    { target: { entityType: "page", entityId: id(3) }, revision: "5" },
  ]);
  expect(execute.occurrenceKey).toBe("schedule:2026-10-05T04:00:00Z");
});
test("uses recent events and reports schema conflicts without executing", async ({
  page,
}) => {
  await page
    .getByRole("combobox", { name: "Trigger", exact: true })
    .selectOption("event");
  await page.getByLabel("Explicitly include reviewed AI changes").check();
  await page
    .getByRole("button", { name: "Save disabled rule", exact: true })
    .click();
  await page
    .getByRole("combobox", { name: "Event for preview", exact: true })
    .selectOption(id(30));
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  const calls = await page.evaluate(() => (window as any).automationCalls);
  expect(
    calls.find((c: any) => c.method === "automationPreview").request.eventId,
  ).toBe(id(30));
  expect(
    calls.find((c: any) => c.method === "batch").request.commands[0].rule
      .allowedOrigins,
  ).toEqual(["user", "ai"]);
  await page
    .getByRole("button", { name: "Close preview", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).automationConflict = true;
  });
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "capture a compatible template",
  );
  expect(
    (await page.evaluate(() => (window as any).automationCalls)).some(
      (c: any) => c.method === "automation.execute",
    ),
  ).toBe(false);
});

test("creates a record and fills it through a later typed action", async ({
  page,
}) => {
  await page
    .getByRole("combobox", { name: "Action type", exact: true })
    .selectOption("record.create");
  await page
    .getByRole("combobox", { name: "Destination database", exact: true })
    .selectOption(id(5));
  await page.getByLabel("Title", { exact: true }).fill("Weekly new record");
  await page.getByRole("button", { name: "Add action", exact: true }).click();
  const second = page.getByRole("group", { name: "Action 2", exact: true });
  await second
    .getByRole("combobox", { name: "Action type", exact: true })
    .selectOption("record.setValue");
  await second
    .getByRole("combobox", { name: "Target record", exact: true })
    .selectOption({ label: "New record: Weekly new record" });
  await second
    .getByRole("combobox", { name: "Property", exact: true })
    .selectOption(id(11));
  await second
    .getByLabel("Property value", { exact: true })
    .fill("Written from a typed control");
  await page
    .getByRole("button", { name: "Save disabled rule", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Edit rule", exact: true }),
  ).toBeVisible();
  const saved = await page.evaluate(
    () =>
      (window as any).automationCalls.find((c: any) => c.method === "batch")
        .request.commands[0].rule,
  );
  expect(saved.actions[0].recordId).toMatch(/^\$id:record-/);
  expect(saved.actions[1]).toMatchObject({
    recordId: saved.actions[0].recordId,
    value: {
      type: "text",
      schemaVersion: 1,
      value: [{ text: "Written from a typed control", marks: [] }],
    },
  });
});
