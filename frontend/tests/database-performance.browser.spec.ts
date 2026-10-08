import { test, expect, type Page } from "@playwright/test";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
async function open(page: Page) {
  await page.clock.install({ time: new Date("2026-10-08T12:00:10Z") });
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4197"}/tests/database-performance-fixture.html`,
  );
  await expect(
    page.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
}
async function queryCalls(page: Page) {
  return page.evaluate(() =>
    (window as any).databasePerformanceFixture.calls.filter(
      (c: any) => c.method === "queryRecords",
    ),
  );
}

test("10k relation targets stay out of closed cells and support bounded searchable keyboard multi-selection", async ({
  page,
}) => {
  await open(page);
  expect(await page.locator("tr[data-record-id]").count()).toBeLessThan(35);
  await expect(page.locator(".db-relation-options")).toHaveCount(0);
  expect(await page.locator(".db-virtual-viewport option").count()).toBe(0);
  const trigger = page.getByRole("button", {
    name: "Related for Row 001",
    exact: true,
  });
  await expect(trigger).toHaveText("Target 09500, Target 00042");
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Edit Related for Row 001" });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("searchbox", { name: "Find a record" }),
  ).toBeFocused();
  await expect(dialog.locator(".db-relation-options input")).toHaveCount(40);
  await expect(dialog.getByRole("status")).toHaveText(
    "2 selected · 1–40 of 10000",
  );
  await dialog.getByRole("button", { name: "Next choices" }).click();
  await expect(
    dialog.getByRole("checkbox", { name: "Target 00042", exact: true }),
  ).toBeChecked();
  await dialog.getByRole("searchbox").fill("తెలుగు 中文");
  await expect(dialog.locator(".db-relation-options input")).toHaveCount(1);
  const last = dialog.getByRole("checkbox", { name: /Target 10000/ });
  await last.focus();
  await page.keyboard.press("Space");
  await expect(last).toBeChecked();
  await dialog.getByRole("searchbox").fill("Target 00001");
  await dialog
    .getByRole("checkbox", { name: "Target 00001", exact: true })
    .check();
  await dialog.getByRole("searchbox").fill("");
  await dialog.getByRole("checkbox", { name: "Show selected only" }).check();
  await expect(dialog.locator(".db-relation-options input")).toHaveCount(4);
  await dialog
    .getByRole("checkbox", { name: "Target 00042", exact: true })
    .focus();
  await page.keyboard.press("Space");
  await expect(
    dialog.getByRole("checkbox", { name: "Target 00042", exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByRole("searchbox")).toBeFocused();
  await dialog.getByRole("button", { name: "Apply selection" }).click();
  await expect(dialog).toHaveCount(0);
  const saved = await page.evaluate(
    () => (window as any).databasePerformanceFixture.saved,
  );
  expect(saved).toHaveLength(1);
  expect(saved[0].commands[0].value.value).toEqual([
    id(39500),
    id(40000),
    id(30001),
  ]);
  expect(
    saved[0].mutation.expectedRevisions
      .filter((g: any) => g.target.entityType === "record")
      .map((g: any) => g.target.entityId),
  ).toEqual(
    expect.arrayContaining([
      id(2001),
      id(39500),
      id(30042),
      id(40000),
      id(30001),
    ]),
  );
  await trigger.click();
  await dialog
    .getByRole("checkbox", { name: "Target 00001", exact: true })
    .uncheck();
  await page.clock.fastForward(60000);
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("checkbox", { name: "Target 00001", exact: true }),
  ).not.toBeChecked();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  expect(
    await page.evaluate(
      () => (window as any).databasePerformanceFixture.saved.length,
    ),
  ).toBe(1);
});

test("minute refresh retains page and scroll, replaces clock values, and cancels a previous clock chain", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  const viewport = page.locator(".db-virtual-viewport");
  await viewport.evaluate((node) => {
    node.scrollTop = 700;
  });
  const scroll = await viewport.evaluate((node) => node.scrollTop);
  const before = (await queryCalls(page)).length;
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.holdQueries = true;
  });
  await page.clock.fastForward(60000);
  await expect
    .poll(async () => (await queryCalls(page)).length)
    .toBe(before + 1);
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  expect(await viewport.evaluate((node) => node.scrollTop)).toBe(scroll);
  expect(await page.locator("tr[data-record-id]").count()).toBeGreaterThan(0);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseOne(),
  );
  await expect
    .poll(async () => (await queryCalls(page)).length)
    .toBe(before + 2);
  await page.clock.fastForward(60000);
  await expect
    .poll(async () => (await queryCalls(page)).length)
    .toBe(before + 3);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseAll(),
  );
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  expect(await viewport.evaluate((node) => node.scrollTop)).toBe(scroll);
  const minute = await page.evaluate(() => Math.floor(Date.now() / 60000));
  await expect(viewport.locator(".db-derived summary").first()).toHaveText(
    `Minute ${minute}`,
  );
  const calls = (await queryCalls(page)).slice(before);
  expect(calls.map((c: any) => c.cursor?.split(":")[1] ?? "0")).toEqual([
    "0",
    "200",
    "0",
    "200",
  ]);
  expect(
    await page.evaluate(
      () => (window as any).databasePerformanceFixture.errors,
    ),
  ).toEqual([]);
});

test("pending Next and Previous keep their target pages across clock ticks", async ({
  page,
}) => {
  await open(page);
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.holdQueries = true;
  });
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect.poll(async () => (await queryCalls(page)).length).toBe(2);
  await expect(
    page.getByRole("textbox", { name: "Title for Row 001", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  await page.clock.fastForward(60000);
  await expect.poll(async () => (await queryCalls(page)).length).toBe(3);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseAll(),
  );
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.holdQueries = true;
  });
  await page
    .getByRole("button", { name: "Previous records", exact: true })
    .click();
  await expect.poll(async () => (await queryCalls(page)).length).toBe(5);
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await page.clock.fastForward(60000);
  await expect.poll(async () => (await queryCalls(page)).length).toBe(6);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseAll(),
  );
  await expect(
    page.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => (window as any).databasePerformanceFixture.errors,
    ),
  ).toEqual([]);
});

test("canonical snapshot changes invalidate cursor history while a clock refresh is pending", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.holdQueries = true;
  });
  await page.clock.fastForward(60000);
  await expect.poll(async () => (await queryCalls(page)).length).toBe(3);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.changeSnapshot(),
  );
  await expect.poll(async () => (await queryCalls(page)).length).toBe(4);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseAll(),
  );
  await expect(
    page.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Title for Row 001", exact: true }),
  ).toBeVisible();
  expect((await queryCalls(page)).length).toBe(4);
});

test("query changes cancel a pending clock prefix and restart at the new view's first page", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.holdQueries = true;
  });
  await page.clock.fastForward(60000);
  await expect.poll(async () => (await queryCalls(page)).length).toBe(3);
  await page.getByRole("tab", { name: /Filtered$/ }).click();
  await expect.poll(async () => (await queryCalls(page)).length).toBe(4);
  await page.evaluate(() =>
    (window as any).databasePerformanceFixture.releaseAll(),
  );
  await expect(page.getByText("50 records", { exact: true })).toBeVisible();
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Title for Row 401", exact: true }),
  ).toBeVisible();
  expect((await queryCalls(page)).length).toBe(4);
});

test("fresh clock membership can shrink the result and clamps to its last available page", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(
    page.getByText("401–450 records", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.limit = 225;
  });
  await page.clock.fastForward(60000);
  await expect(
    page.getByText("201–225 records", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Next records", exact: true }),
  ).toBeDisabled();
  expect((await queryCalls(page)).length).toBe(5);
});

test("failed navigation keeps the settled page label and exposes the cursor conflict without retrying", async ({
  page,
}) => {
  await open(page);
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(
    page.getByText("201–400 records · more available", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.failQueries = true;
  });
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Records, view or clock changed; restart the query",
  );
  await expect(page.getByText("Page 2", { exact: true })).toBeVisible();
  await expect(
    page.getByText("201–400 records", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Title for Row 201", exact: true }),
  ).toBeVisible();
  expect((await queryCalls(page)).length).toBe(3);
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.failQueries = false;
  });
  await page
    .getByRole("button", { name: "Previous records", exact: true })
    .click();
  await expect(
    page.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  expect((await queryCalls(page)).length).toBe(4);
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.failQueries = true;
  });
  await page.getByRole("button", { name: "Next records", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Records, view or clock changed; restart the query",
  );
  await expect(page.getByText("Page 1", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Previous records", exact: true }),
  ).toBeDisabled();
  await page.evaluate(() => {
    (window as any).databasePerformanceFixture.failQueries = false;
  });
  await page.getByRole("button", { name: "Restart view", exact: true }).click();
  await expect(
    page.getByText("1–200 records · more available", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toHaveCount(0);
});
