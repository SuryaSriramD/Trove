import { test, expect } from "@playwright/test";
const base = process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4214";
// Independent tiny PDF producer: two physical pages, literal text, valid xrefs,
// hostile document actions and link annotations. No PDF.js-created fixture.
function pdf(lines: string[]) {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /JavaScript /JS (window.documentAttacked=true) >> >>",
    `<< /Type /Pages /Count ${lines.length} /Kids [${lines.map((_, i) => `${4 + i * 2} 0 R`).join(" ")}] >>`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  for (let i = 0; i < lines.length; i++) {
    const stream = `BT /F1 16 Tf 50 700 Td (${lines[i]}) Tj ET`;
    objects.push(
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents ${5 + i * 2} 0 R /Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /URI /URI (https://outside.invalid/leak) >> >>] >>`,
    );
    objects.push(
      `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    );
  }
  let raw = "%PDF-1.7\n";
  const offsets = [0];
  objects.forEach((object, i) => {
    offsets.push(raw.length);
    raw += `${i + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = raw.length;
  raw += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  raw += offsets
    .slice(1)
    .map((x) => `${String(x).padStart(10, "0")} 00000 n \n`)
    .join("");
  return Buffer.from(
    raw +
      `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`,
  );
}
test("isolated PDF.js extraction preserves physical pages and never executes actions or external requests", async ({
  page,
  context,
}) => {
  const outside: string[] = [];
  await context.route("**/*", (r) => {
    if (
      !r
        .request()
        .url()
        .startsWith(base + "/")
    ) {
      outside.push(r.request().url());
      return r.abort();
    }
    return r.continue();
  });
  await context.route("**/input.pdf", (r) =>
    r.fulfill({
      body: pdf(["Amber invoice 47", "Cobalt shipment 83"]),
      contentType: "application/pdf",
    }),
  );
  await page.goto(base + "/extract.html");
  await page.waitForFunction(
    () =>
      window.troveDocumentStatus?.ready || window.troveDocumentStatus?.error,
  );
  const result = await page.evaluate(() => window.troveDocumentStatus);
  expect(result.error).toBeUndefined();
  expect(result.pages).toBe(2);
  expect(result.text?.split("\f").map((x) => x.trim())).toEqual([
    "Amber invoice 47",
    "Cobalt shipment 83",
  ]);
  expect(
    await page.evaluate(() => (window as any).documentAttacked),
  ).toBeUndefined();
  expect(outside).toEqual([]);
});
test("PDF validation counts physical pages without publishing extracted content", async ({
  page,
}) => {
  await page.route("**/result.pdf", (r) =>
    r.fulfill({
      body: pdf(["private text", "second", "third"]),
      contentType: "application/pdf",
    }),
  );
  await page.goto(base + "/validate.html");
  await page.waitForFunction(
    () =>
      window.troveDocumentStatus?.ready || window.troveDocumentStatus?.error,
  );
  expect(await page.evaluate(() => window.troveDocumentStatus)).toEqual({
    ready: true,
    pages: 3,
  });
});
test("malformed and over-limit page catalogues fail without a successful result", async ({
  page,
}) => {
  for (const bytes of [
    Buffer.from("%PDF-broken"),
    pdf(Array.from({ length: 2001 }, () => "page")),
  ]) {
    await page.route("**/result.pdf", (r) =>
      r.fulfill({ body: bytes, contentType: "application/pdf" }),
    );
    await page.goto(base + "/validate.html");
    await page.waitForFunction(() => window.troveDocumentStatus?.error);
    expect(await page.evaluate(() => window.troveDocumentStatus.ready)).toBe(
      false,
    );
    await page.unroute("**/result.pdf");
  }
});
