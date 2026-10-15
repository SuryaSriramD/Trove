import { test, expect } from "@playwright/test";
const url = () =>
  `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4283"}/tests/desktop-continuity.fixture.html`;
test("freshness refreshes when a linked source changes without the owning revision", async ({
  page,
}) => {
  await page.goto(url());
  await expect(page.locator(".page-review > summary")).toHaveText(
    "Human reviewed",
  );
  await page
    .getByRole("button", { name: "Change linked source", exact: true })
    .click();
  await expect(page.locator(".page-review > summary")).toHaveText(
    "Changed since review",
    { timeout: 3000 },
  );
});
test("old review-queue pagination cannot append after a snapshot refresh", async ({
  page,
}) => {
  await page.goto(url());
  const queue = page.getByRole("region", { name: "Knowledge review queue" });
  await expect(
    queue.getByRole("button", { name: "Pending review", exact: true }),
  ).toBeVisible();
  await queue.getByRole("button", { name: "Load more" }).click();
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  await expect(
    queue.getByRole("button", { name: "Current review", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Release older queue response", exact: true })
    .click();
  await page.waitForTimeout(50);
  await expect(
    queue.getByText("Obsolete queue result", { exact: true }),
  ).toHaveCount(0);
});
test("form and chart drafts survive snapshots and collapsing the panel", async ({
  page,
}) => {
  await page.goto(url());
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page.getByLabel("Form title", { exact: true }).fill("Keep this draft");
  await page.getByLabel("Form Notes", { exact: true }).fill("Unsaved words");
  await page.getByLabel("Form Amount", { exact: true }).fill("12.75");
  await page.getByLabel("Chart display", { exact: true }).selectOption("donut");
  await page
    .getByRole("button", { name: "Refresh snapshot", exact: true })
    .click();
  await expect(page.getByLabel("Form title", { exact: true })).toHaveValue(
    "Keep this draft",
  );
  await expect(page.getByLabel("Form Notes", { exact: true })).toHaveValue(
    "Unsaved words",
  );
  await expect(page.getByLabel("Form Amount", { exact: true })).toHaveValue(
    "12.75",
  );
  await expect(page.getByLabel("Chart display", { exact: true })).toHaveValue(
    "donut",
  );
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await expect(page.getByLabel("Form title", { exact: true })).toHaveValue(
    "Keep this draft",
  );
});

test("saved settings preserve form drafts; submission resets checkbox defaults and row actions remain reviewed", async ({
  page,
}) => {
  await page.goto(url());
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page.getByLabel("Form title", { exact: true }).fill("First submission");
  await page
    .getByLabel("Form Notes", { exact: true })
    .fill("Retained while saving settings");
  await page.getByLabel("Chart display", { exact: true }).selectOption("donut");
  await expect(
    page.getByRole("button", { name: "Submit local form", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Save chart and settings", exact: true })
    .click();
  await expect(page.getByLabel("Form Notes", { exact: true })).toHaveValue(
    "Retained while saving settings",
  );
  await page
    .getByRole("button", { name: "Submit local form", exact: true })
    .click();
  await expect(page.getByLabel("Form title", { exact: true })).toHaveValue("");
  await page
    .getByLabel("Form title", { exact: true })
    .fill("Second submission");
  await page
    .getByRole("button", { name: "Submit local form", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => (window as any).__submission?.title))
    .toBe("Second submission");
  expect(
    await page.evaluate(
      () =>
        (window as any).__submission.values.find(
          (v: any) => v.value.type === "checkbox",
        ).value.value,
    ),
  ).toBe(false);
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  const dialog = page.getByRole("alertdialog", { name: "Confirm row action" });
  await expect(dialog).toContainText("Set Approved to");
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(
    await page.evaluate(() => (window as any).__rowAction),
  ).toBeUndefined();
  await page.getByRole("button", { name: "Approve", exact: true }).click();
  await dialog
    .getByRole("button", { name: "Confirm Approve", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => (window as any).__rowAction?.confirmed))
    .toBe(true);
});
test("remote settings and form schema changes require fresh review without losing drafts", async ({
  page,
}) => {
  await page.goto(url());
  await page
    .getByText("Charts, local forms and row buttons", { exact: true })
    .click();
  await page
    .getByLabel("Form title", { exact: true })
    .fill("Draft survives conflict");
  await page
    .getByLabel("Form Notes", { exact: true })
    .fill("Original field value");
  await page.getByLabel("Chart display", { exact: true }).selectOption("donut");
  await page
    .getByRole("button", { name: "Change saved settings", exact: true })
    .click();
  await expect(
    page.getByText(
      /Saved settings changed while you were editing. Your draft is retained/,
    ),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Discard settings changes", exact: true })
    .click();
  await expect(
    page.getByLabel("Chart aggregation", { exact: true }),
  ).toHaveValue("sum");
  await expect(page.getByLabel("Form title", { exact: true })).toHaveValue(
    "Draft survives conflict",
  );
  await page
    .getByRole("button", { name: "Change form schema", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Submit local form", exact: true })
    .click();
  await expect(
    page.getByText(/Notes changed while this draft was being edited/),
  ).toBeVisible();
  expect(
    await page.evaluate(() => (window as any).__submission),
  ).toBeUndefined();
  await page
    .getByLabel("Form Notes", { exact: true })
    .fill("Reviewed updated field");
  await page
    .getByRole("button", { name: "Submit local form", exact: true })
    .click();
  await expect
    .poll(() => page.evaluate(() => (window as any).__submission?.title))
    .toBe("Draft survives conflict");
});

test("search and completed AI evidence show elapsed review deadlines without another edit", async ({
  page,
}) => {
  await page.clock.install({ time: new Date("2026-10-15T12:00:00Z") });
  await page.goto(url());
  await page.getByLabel("Search text", { exact: true }).fill("knowledge");
  await expect(page.locator(".search-result")).toContainText("Human reviewed");
  await page.getByText("Source context · 1", { exact: true }).click();
  await expect(page.locator(".ai-source small")).toHaveText(
    "Human reviewed · human review status",
  );
  await page.clock.fastForward(61000);
  await expect(page.locator(".search-result")).toContainText("Review overdue", {
    timeout: 1000,
  });
  await expect(page.locator(".ai-source small")).toHaveText(
    "Review overdue · human review status",
  );
});
