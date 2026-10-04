import { test, expect } from "@playwright/test";
const base = process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191";
const rich = (text: string) => [{ text }];
const block = (
  id: string,
  type: string,
  data: unknown,
  parentBlockId: string | null = null,
) => ({
  id,
  pageId: "p",
  type,
  data,
  parentBlockId,
  sortKey: id,
  schemaVersion: 1,
  revision: "1",
});
test("isolated rich print document resolves supported content without external requests", async ({
  page,
  context,
}) => {
  const requests: string[] = [];
  await context.route("**/*", async (route) => {
    const url = route.request().url();
    if (!url.startsWith(base)) {
      requests.push(url);
      await route.abort();
      return;
    }
    await route.continue();
  });
  const blocks = [
    block("00", "paragraph", {
      richText: [
        { text: "Block citation", link: "trove://page/p/block/05?workspace=w" },
      ],
    }),
    block("01", "paragraph", {
      richText: [
        {
          text: "<script>inert source</script>",
          marks: ["bold"],
          color: "purple",
          highlight: "yellow",
          link: "javascript:window.attacked=true",
        },
      ],
    }),
    block("02", "mermaid", {
      source: "flowchart LR\nA[Capture]-->B[Find]",
      renderMode: "inert_svg",
    }),
    block("03", "equation", { latex: "E=mc^2", display: true }),
    block("04", "columns", {
      columns: [
        { id: "a", widthRatio: 1, childBlockIds: ["05"] },
        { id: "b", widthRatio: 1, childBlockIds: ["06"] },
      ],
    }),
    block("05", "paragraph", { richText: rich("Left reusable source") }, "04"),
    block("06", "paragraph", { richText: rich("Right column") }, "04"),
    block("07", "synced_block", { contentSourceId: "05" }),
    block("08", "embed", {
      url: "https://tracking.invalid/frame",
      offlineText: "Offline retained media",
    }),
    block("09", "image", {
      fileId: "unavailable",
      altText: "Retained caption",
      caption: rich("Readable image fallback"),
    }),
    block("10", "equation", { latex: "\\invalidcommand{", display: true }),
    block("11", "table", {
      rowCount: 40,
      columnCount: 2,
      headerRows: 1,
      headerColumns: 0,
      cells: Array.from({ length: 80 }, (_, i) => ({
        row: Math.floor(i / 2),
        column: i % 2,
        rowSpan: 1,
        colSpan: 1,
        richText: rich(i < 2 ? "Repeated header" : `Cell ${i}`),
      })),
    }),
  ];
  await context.route("**/snapshot.json", (r) =>
    r.fulfill({
      json: {
        pages: [
          {
            id: "p",
            canonicalPage: { title: "Rich print fixture" },
            canonicalBlocks: blocks,
          },
        ],
        attachments: [],
      },
    }),
  );
  await page.goto(base + "/print.html");
  await page.waitForFunction(() => window.trovePrintStatus?.ready === true);
  await expect(page.locator(".diagram svg")).toBeVisible();
  await expect(page.locator(".katex")).toHaveCount(1);
  await expect(page.locator(".columns .layout-group")).toHaveCount(2);
  await expect(page.locator(".reusable")).toContainText("Left reusable source");
  await expect(page.locator("strong")).toHaveText(
    "<script>inert source</script>",
  );
  await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);
  await expect(page.locator(".export-notes")).toContainText("invalid equation");
  expect(await page.locator(".table-chunk").count()).toBeGreaterThan(1);
  expect(await page.locator(".table-chunk thead").count()).toBe(
    await page.locator(".table-chunk").count(),
  );
  expect(
    await page.evaluate(() => ({
      go: (window as any).go,
      attacked: (window as any).attacked,
    })),
  ).toEqual({ go: undefined, attacked: undefined });
  expect(requests).toEqual([]);
  await expect(
    page.getByRole("link", { name: "Block citation" }),
  ).toHaveAttribute("href", "#block-05");
  const capture = await page.locator("#document").screenshot();
  await test
    .info()
    .attach("rich-print-browser", { body: capture, contentType: "image/png" });
});
