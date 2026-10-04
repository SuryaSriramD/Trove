# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: recovery.browser.spec.ts >> keeps unknown historical payloads readable without assuming a current shape
- Location: tests/recovery.browser.spec.ts:49:1

# Error details

```
Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:4186/tests/recovery-fixture.html
Call log:
  - navigating to "http://127.0.0.1:4186/tests/recovery-fixture.html", waiting until "load"

```

# Page snapshot

```yaml
- generic [ref=e3]:
  - generic [ref=e6]:
    - heading "This site can’t be reached" [level=1] [ref=e7]
    - paragraph [ref=e8]:
      - strong [ref=e9]: 127.0.0.1
      - text: refused to connect.
    - generic [ref=e10]:
      - paragraph [ref=e11]: "Try:"
      - list [ref=e12]:
        - listitem [ref=e13]: Checking the connection
        - listitem [ref=e14]:
          - link "Checking the proxy and the firewall" [ref=e15] [cursor=pointer]:
            - /url: "#buttons"
    - generic [ref=e16]: ERR_CONNECTION_REFUSED
  - generic [ref=e17]:
    - button "Reload" [ref=e19] [cursor=pointer]
    - button "Details" [ref=e20] [cursor=pointer]
```

# Test source

```ts
  1  | import { test, expect } from "@playwright/test";
  2  | 
  3  | test.beforeEach(async ({ page }) => {
> 4  |   await page.goto(`${process.env.TROVE_RECOVERY_TEST_URL ?? "http://127.0.0.1:4186"}/tests/recovery-fixture.html`);
     |              ^ Error: page.goto: net::ERR_CONNECTION_REFUSED at http://127.0.0.1:4186/tests/recovery-fixture.html
  5  |   await page.getByLabel("Page history").selectOption({ label: "Historical record" });
  6  |   await page.getByRole("button", { name: "Load revisions", exact: true }).click();
  7  |   await page.getByRole("button", { name: /^Preview revision 2/ }).click();
  8  | });
  9  | 
  10 | test("maps a typed value only after reviewing the latest decision", async ({ page }) => {
  11 |   const restore = page.getByRole("button", { name: "Restore this revision", exact: true });
  12 |   await expect(restore).toBeDisabled();
  13 |   await expect(page.getByText("Saved value: 12", { exact: true })).toBeVisible();
  14 |   await page.getByLabel("Recovery choice for Amount").selectOption("map");
  15 |   await page.getByLabel("Mapped value for Amount").fill("34");
  16 |   await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  17 |   await expect(restore).toBeEnabled();
  18 |   await page.getByLabel("Mapped value for Amount").fill("35");
  19 |   await expect(restore).toBeDisabled();
  20 |   await page.evaluate(() => { (window as any).failNextRecoveryReview = true; });
  21 |   await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  22 |   await expect(page.getByRole("alert")).toContainText("Property changed");
  23 |   await expect(restore).toBeDisabled();
  24 |   await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  25 |   await expect(restore).toBeEnabled();
  26 |   await restore.click();
  27 |   await expect(page.getByText("Page restored as a new revision.", { exact: true })).toBeVisible();
  28 |   const calls = await page.evaluate(() => (window as any).recoveryCalls);
  29 |   const previews = calls.filter((call: any) => call.method === "history.previewRestore");
  30 |   expect(previews.at(-1).request.propertyDecisions[0]).toMatchObject({ action: "map", value: { decimal: "35", format: "percent" } });
  31 |   const command = calls.find((call: any) => call.method === "batch").request.commands[0];
  32 |   expect(command).toMatchObject({ kind: "history.restore", restoreRevision: "2", previewId: "019a1234-5678-7000-8000-000000000023" });
  33 |   expect(command).not.toHaveProperty("propertyDecisions");
  34 | });
  35 | 
  36 | test("requires an explicit retention choice and displays its reviewed effect", async ({ page }) => {
  37 |   const review = page.getByRole("button", { name: "Review property decisions", exact: true });
  38 |   await expect(review).toBeDisabled();
  39 |   await page.getByLabel("Recovery choice for Amount").selectOption("retain");
  40 |   await expect(page.getByLabel("Mapped value for Amount")).toHaveCount(0);
  41 |   await review.click();
  42 |   await expect(page.getByText("Retain old value as inert evidence; keep current value.", { exact: true })).toBeVisible();
  43 |   await page.getByRole("button", { name: "Restore this revision", exact: true }).click();
  44 |   const calls = await page.evaluate(() => (window as any).recoveryCalls);
  45 |   const preview = calls.filter((call: any) => call.method === "history.previewRestore").at(-1);
  46 |   expect(preview.request.propertyDecisions).toEqual([{ sourcePropertyId: "019a1234-5678-7000-8000-000000000004", action: "retain" }]);
  47 | });
  48 | 
  49 | test("keeps unknown historical payloads readable without assuming a current shape", async ({ page }) => {
  50 |   await page.evaluate(() => { (window as any).opaqueHistoricalEvidence = true; });
  51 |   await page.getByRole("button", { name: /^Preview revision 2/ }).click();
  52 |   await expect(page.getByText('Saved value: {"sourcePayload":"legacy opaque value"}', { exact: true })).toBeVisible();
  53 |   await page.getByLabel("Recovery choice for Amount").selectOption("map");
  54 |   await expect(page.getByLabel("Mapped value for Amount")).toHaveValue("");
  55 |   await page.getByLabel("Mapped value for Amount").fill("21");
  56 |   await page.getByRole("button", { name: "Review property decisions", exact: true }).click();
  57 |   await expect(page.getByRole("button", { name: "Restore this revision", exact: true })).toBeEnabled();
  58 | });
  59 | 
```