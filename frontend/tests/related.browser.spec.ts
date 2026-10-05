import { test, expect } from "@playwright/test";
async function showRelated(page: any) {
  await page.getByText("Templates and related pages", { exact: true }).click();
  await page
    .getByText("Related content and possible duplicates", { exact: true })
    .click();
}
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/related-fixture.html`,
  );
  await showRelated(page);
});
test("editor exposes readable semantic cards only after Find, deduplicates, and resolves before opening", async ({
  page,
}) => {
  expect(
    await page.evaluate(() =>
      (window as any).relatedFixture.calls.filter(
        (c: any) => c.method === "ai.suggestions",
      ),
    ),
  ).toHaveLength(0);
  await page.getByRole("button", { name: "Find related content" }).click();
  await expect(
    page.getByRole("heading", { name: "Semantic related pages" }),
  ).toBeVisible();
  await expect(
    page.getByRole("article", { name: "Semantic page: Canine exercise" }),
  ).toContainText("Daily exercise supports canine wellbeing.");
  await expect(
    page.getByRole("article", { name: "Semantic page: Canine exercise" }),
  ).toContainText("Revision 7");
  await expect(
    page.getByRole("button", { name: "Canine exercise", exact: true }),
  ).toHaveCount(1);
  await expect(
    page.getByRole("button", { name: "Current notebook", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("Possible duplicate · matching normalized indexed text", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("Search omissions")).toContainText(
    "first 1,000 accessible pages",
  );
  await expect(
    page.getByText("Unlinked title mention", { exact: false }),
  ).toBeVisible();
  await expect(page.locator("pre")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Canine exercise", exact: true })
    .click();
  const fixture = await page.evaluate(() => (window as any).relatedFixture);
  expect(fixture.navigations).toHaveLength(1);
  expect(
    fixture.calls.filter((c: any) => c.method === "resolveCitation")[0].request
      .location.sourceRevision,
  ).toBe("7");
  expect(
    fixture.calls.some((c: any) => /batch|apply|link/.test(c.method)),
  ).toBe(false);
});
test("keyword fallback is labeled and stale sources cannot open", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).relatedFixture.mode = "keyword";
    (window as any).relatedFixture.resolution = "stale";
  });
  await page.getByRole("button", { name: "Find related content" }).click();
  await expect(
    page.getByRole("heading", {
      name: "Keyword matches · semantic unavailable",
    }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Semantic related content is disabled. Showing keyword matches.",
    ),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Canine exercise", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("This source is stale");
  expect(
    await page.evaluate(() => (window as any).relatedFixture.navigations),
  ).toHaveLength(0);
});
for (const action of [
  "Switch page",
  "Switch workspace",
  "Change grant",
  "Edit source",
]) {
  test(`pending suggestions are discarded after ${action}`, async ({
    page,
  }) => {
    await page.evaluate(() => {
      (window as any).relatedFixture.deferSuggestions = true;
    });
    await page.getByRole("button", { name: "Find related content" }).click();
    await expect(page.getByRole("status", { name: "" })).toContainText(
      "Finding related pages",
    );
    await page.getByRole("button", { name: action, exact: true }).click();
    await page.evaluate(() => {
      (window as any).relatedFixture.releaseSuggestions();
    });
    await showRelated(page);
    await expect(
      page.getByRole("button", { name: "Canine exercise", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Find related content" }),
    ).toBeEnabled();
  });
}
test("pending citation resolution cannot navigate a replacement page", async ({
  page,
}) => {
  await page.getByRole("button", { name: "Find related content" }).click();
  await page.evaluate(() => {
    (window as any).relatedFixture.deferResolution = true;
  });
  await page
    .getByRole("button", { name: "Canine exercise", exact: true })
    .click();
  await page.getByRole("button", { name: "Switch page", exact: true }).click();
  await page.evaluate(() => {
    (window as any).relatedFixture.releaseResolution();
  });
  expect(
    await page.evaluate(() => (window as any).relatedFixture.navigations),
  ).toHaveLength(0);
});
