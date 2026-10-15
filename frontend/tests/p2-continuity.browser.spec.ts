import { test, expect, type Page } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const paint = (page: Page) =>
  page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
const hold = (page: Page, method: string) =>
  page.evaluate((method) => {
    (window as any).hold = method;
    delete (window as any).release;
  }, method);
const waiting = (page: Page) =>
  expect
    .poll(() => page.evaluate(() => typeof (window as any).release))
    .toBe("function");
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:5188"}${process.env.TROVE_P2_FIXTURE ?? "/tests/p2-continuity-fixture.html"}`,
  );
  await expect(
    page.getByRole("heading", { name: "Local intelligence" }),
  ).toBeVisible();
});
test("late graph cannot appear under another selected page", async ({
  page,
}) => {
  await hold(page, "graph.query");
  await page.getByRole("button", { name: "Explore connections" }).click();
  await waiting(page);
  await page
    .getByRole("combobox", { name: "Page", exact: true })
    .selectOption(id(5));
  await page.evaluate(() => (window as any).release());
  await paint(page);
  expect(
    await page.getByRole("img", { name: "Page relationships" }).count(),
  ).toBe(0);
  await page.getByRole("button", { name: "Explore connections" }).click();
  await expect(
    page.getByRole("button", { name: "Graph first Beta", exact: true }),
  ).toBeVisible();
});
for (const change of ["Switch workspace", "Change grant", "Advance source"])
  test(`graph clears on ${change}`, async ({ page }) => {
    await page.getByRole("button", { name: "Explore connections" }).click();
    await expect(
      page.getByRole("img", { name: "Page relationships" }),
    ).toBeVisible();
    await page.getByRole("button", { name: change, exact: true }).click();
    await paint(page);
    expect(
      await page.getByRole("img", { name: "Page relationships" }).count(),
    ).toBe(0);
  });
test("late manual review read cannot replace newer source state", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Personal review", exact: true })
    .click();
  await expect(page.getByText("Review first 0", { exact: true })).toBeVisible();
  await hold(page, "review.list");
  await page.getByRole("button", { name: "Refresh review drafts" }).click();
  await waiting(page);
  await page.getByRole("button", { name: "Advance source" }).click();
  await expect(page.getByText("Review first 1", { exact: true })).toBeVisible();
  await page.evaluate(() => (window as any).release());
  await paint(page);
  expect(await page.getByText("Review first 0", { exact: true }).count()).toBe(
    0,
  );
});
test("late manual image read cannot replace another page", async ({ page }) => {
  await page
    .getByRole("button", { name: "Image questions", exact: true })
    .click();
  await expect(
    page.getByText("Draft first Alpha", { exact: true }),
  ).toBeVisible();
  await hold(page, "image.list");
  await page.getByRole("button", { name: "Refresh image drafts" }).click();
  await waiting(page);
  await page
    .getByRole("combobox", { name: "Page", exact: true })
    .selectOption(id(5));
  await expect(
    page.getByText("Draft first Beta", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => (window as any).release());
  await paint(page);
  expect(
    await page.getByText("Draft first Alpha", { exact: true }).count(),
  ).toBe(0);
});
test("OCR has request-bound cancellation and no late result", async ({
  page,
}) => {
  await page.getByRole("button", { name: "Image text", exact: true }).click();
  await page.getByLabel("Attached image").selectOption(id(70));
  await page
    .getByRole("button", { name: "Extract image text locally" })
    .click();
  await expect(
    page.getByRole("button", { name: "Cancel image text extraction" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Cancel image text extraction" })
    .click();
  await expect(page.getByText(/Extraction cancelled/)).toBeVisible();
  expect(
    await page.getByText("Prior OCR result", { exact: true }).count(),
  ).toBe(0);
  await expect(
    page.getByRole("button", { name: "Extract image text locally" }),
  ).toBeEnabled();
});

for (const change of ["Switch workspace", "Change grant"])
  test(`late image result is discarded on ${change}`, async ({ page }) => {
    await page
      .getByRole("button", { name: "Image questions", exact: true })
      .click();
    await expect(
      page.getByText("Draft first Alpha", { exact: true }),
    ).toBeVisible();
    await hold(page, "image.list");
    await page.getByRole("button", { name: "Refresh image drafts" }).click();
    await waiting(page);
    await page.getByRole("button", { name: change, exact: true }).click();
    await page
      .getByRole("button", { name: "Image questions", exact: true })
      .click();
    await expect(
      page.getByText("Draft second Alpha", { exact: true }),
    ).toBeVisible();
    await page.evaluate(() => (window as any).release());
    await paint(page);
    expect(
      await page.getByText("Draft first Alpha", { exact: true }).count(),
    ).toBe(0);
  });
test("leaving OCR requests cancellation for its exact original workspace request", async ({
  page,
}) => {
  await page.getByRole("button", { name: "Image text", exact: true }).click();
  await page.getByLabel("Attached image").selectOption(id(70));
  await page
    .getByRole("button", { name: "Extract image text locally" })
    .click();
  await expect(
    page.getByRole("button", { name: "Cancel image text extraction" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as any).p2Calls.some(
          (c: any) =>
            c.method === "ocr.cancel" &&
            c.request.workspaceId === "019a1234-5678-7000-8000-000000000001" &&
            c.request.runRequestId === (window as any).ocrStarted,
        ),
      ),
    )
    .toBe(true);
  expect(
    await page.getByText("Prior OCR result", { exact: true }).count(),
  ).toBe(0);
});
