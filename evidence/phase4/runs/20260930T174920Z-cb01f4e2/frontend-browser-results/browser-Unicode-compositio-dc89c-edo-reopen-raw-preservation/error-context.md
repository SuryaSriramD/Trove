# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> Unicode composition, RTL, emoji, undo/redo, reopen, raw preservation
- Location: tests/browser.spec.ts:512:1

# Error details

```
Test timeout of 30000ms exceeded.
```

```
Error: locator.click: Test timeout of 30000ms exceeded.
Call log:
  - waiting for getByRole('button', { name: 'Undo', exact: true })

```

# Page snapshot

```yaml
- generic [ref=f3e3]:
  - link "Skip to content" [ref=f3e4] [cursor=pointer]:
    - /url: "#main"
  - complementary [ref=f3e5]:
    - generic [ref=f3e6]:
      - generic [ref=f3e7]: ✳
      - strong [ref=f3e8]: Trove
      - generic [ref=f3e9]: LOCAL
    - button "S My workspace Room for what matters ⌄" [ref=f3e10] [cursor=pointer]:
      - generic [ref=f3e11]: S
      - generic [ref=f3e12]:
        - text: My workspace
        - generic [ref=f3e13]: Room for what matters
      - generic [ref=f3e14]: ⌄
    - button "⌕ Quick search ⌘ K" [ref=f3e15] [cursor=pointer]:
      - generic [ref=f3e16]: ⌕ Quick search
      - generic [ref=f3e17]: ⌘ K
    - navigation "Workspace" [ref=f3e18]:
      - button "⌂ Home" [ref=f3e19] [cursor=pointer]:
        - generic [ref=f3e20]: ⌂
        - text: Home
      - button "▣ Inbox" [ref=f3e21] [cursor=pointer]:
        - generic [ref=f3e22]: ▣
        - text: Inbox
      - button "▤ Pages" [ref=f3e23] [cursor=pointer]:
        - generic [ref=f3e24]: ▤
        - text: Pages
      - button "✓ Tasks 0" [ref=f3e25] [cursor=pointer]:
        - generic [ref=f3e26]: ✓
        - text: Tasks
        - generic [ref=f3e27]: "0"
      - button "◉ Meetings" [ref=f3e28] [cursor=pointer]:
        - generic [ref=f3e29]: ◉
        - text: Meetings
      - button "⌕ Search" [ref=f3e30] [cursor=pointer]:
        - generic [ref=f3e31]: ⌕
        - text: Search
      - button "↓ Import" [ref=f3e32] [cursor=pointer]:
        - generic [ref=f3e33]: ↓
        - text: Import
      - button "↻ Jobs" [ref=f3e34] [cursor=pointer]:
        - generic [ref=f3e35]: ↻
        - text: Jobs
      - button "✧ AI" [ref=f3e36] [cursor=pointer]:
        - generic [ref=f3e37]: ✧
        - text: AI
      - button "⇄ Providers" [ref=f3e38] [cursor=pointer]:
        - generic [ref=f3e39]: ⇄
        - text: Providers
      - button "⊕ Capture" [ref=f3e40] [cursor=pointer]:
        - generic [ref=f3e41]: ⊕
        - text: Capture
      - button "◷ Automations" [ref=f3e42] [cursor=pointer]:
        - generic [ref=f3e43]: ◷
        - text: Automations
      - button "⚙ Settings" [ref=f3e44] [cursor=pointer]:
        - generic [ref=f3e45]: ⚙
        - text: Settings
    - generic [ref=f3e46]: FAVORITES
    - paragraph [ref=f3e48]: Keep your essentials close.
    - generic [ref=f3e49]:
      - generic [ref=f3e50]: YOUR PAGES
      - button "Create page" [ref=f3e51] [cursor=pointer]: ＋
    - navigation "Page tree" [ref=f3e52]:
      - button "🌱 A garden of ideas" [ref=f3e54] [cursor=pointer]:
        - generic [ref=f3e55]: 🌱
        - generic [ref=f3e56]: A garden of ideas
    - generic [ref=f3e57]:
      - text: On your device
      - generic [ref=f3e59]: ↙
  - generic [ref=f3e60]:
    - banner [ref=f3e61]:
      - generic [ref=f3e62]:
        - button "Go back" [disabled] [ref=f3e63]: ‹
        - button "Go forward" [disabled] [ref=f3e64]: ›
        - generic [ref=f3e65]: My workspace
        - generic [ref=f3e66]: /
        - generic [ref=f3e67]: Home
      - generic [ref=f3e68]: S
    - tablist "Open pages" [ref=f3e70]:
      - generic [ref=f3e71]:
        - tab "▤ A garden of ideas" [ref=f3e72] [cursor=pointer]
        - button "Close A garden of ideas" [ref=f3e73] [cursor=pointer]: ×
    - main [ref=f3e74]:
      - generic [ref=f3e75]:
        - generic [ref=f3e76]:
          - generic [ref=f3e77]: A LITTLE CLARITY, EVERY DAY
          - heading "Make room for your ideas." [level=1] [ref=f3e78]
          - paragraph [ref=f3e79]: Your thoughts, plans, and everything in between.All here. All yours.
          - generic [ref=f3e80]:
            - button "＋ New page" [ref=f3e81] [cursor=pointer]
            - button "↓ Bring something in" [ref=f3e82] [cursor=pointer]
          - generic [aria-hidden] [ref=f3e83]: ✳
        - generic [ref=f3e84]:
          - heading "Pick up where you left off" [level=2] [ref=f3e85]
          - button "All pages ↗" [ref=f3e86] [cursor=pointer]
        - button "🌱 A garden of ideas A place to begin Capture the small things. Connect the useful ones. नमस्ते తెలుగు 日本語 中文 العربية 👩🏽‍💻 漢字 Edited Sep 30" [ref=f3e88] [cursor=pointer]:
          - generic [ref=f3e89]: 🌱
          - strong [ref=f3e90]: A garden of ideas
          - generic [ref=f3e91]: A place to begin Capture the small things. Connect the useful ones. नमस्ते తెలుగు 日本語 中文 العربية 👩🏽‍💻 漢字
          - generic [ref=f3e92]: Edited Sep 30
        - generic [ref=f3e93]:
          - generic [ref=f3e94]:
            - generic [ref=f3e95]: ☀
            - heading "A calmer place to work" [level=3] [ref=f3e96]
            - paragraph [ref=f3e97]: Capture a loose thought. Shape it into a plan. Give it a home.
            - button "Open your inbox ↗" [ref=f3e98] [cursor=pointer]
          - generic [ref=f3e99]:
            - generic [ref=f3e100]: ✓
            - heading "Small steps, real progress" [level=3] [ref=f3e101]
            - paragraph [ref=f3e102]: Keep tasks connected to the work that matters.
            - button "Go to tasks ↗" [ref=f3e103] [cursor=pointer]
    - contentinfo [ref=f3e104]:
      - generic [ref=f3e105]: Made for your own way of thinking.
      - generic [ref=f3e106]: Private by default · Saved on this device
```

# Test source

```ts
  424 |     contentType: "application/json",
  425 |   });
  426 |   expect(shellMs).toBeLessThan(2000);
  427 |   const tree = page.getByRole("navigation", { name: "Page tree" });
  428 |   await expect(tree.locator(".tree-item")).toHaveCount(9997);
  429 |   await expect(tree.locator(".tree-item").nth(1)).toContainText("Page 00001");
  430 |   await expect(tree.getByRole("button", { name: "▤ Page 09999", exact: true })).toHaveCount(1);
  431 |   await expect(tree.getByRole("button", { name: "▤ Page 09996", exact: true })).toHaveCount(0);
  432 | 
  433 |   await page.keyboard.press("Meta+k");
  434 |   const dialog = page.getByRole("dialog", { name: "Quick switcher" });
  435 |   const query = dialog.getByRole("textbox", { name: "Find a page" });
  436 |   await query.fill("only-body-target తెలుగు");
  437 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  438 |   await expect(dialog.getByRole("button")).toContainText("Page 09999");
  439 |   await query.press("Enter");
  440 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");
  441 |   await expect(page.getByRole("textbox", { name: "Page body" })).toContainText("only-body-target తెలుగు");
  442 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true })).toBeVisible();
  443 |   await page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true }).click();
  444 |   await page.getByRole("button", { name: "Go back", exact: true }).click();
  445 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");
  446 | 
  447 |   await page.keyboard.press("Meta+k");
  448 |   await query.fill("record-body-target");
  449 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  450 |   await expect(dialog.getByRole("button")).toContainText("Page 09996");
  451 |   await query.fill("hidden-only-token");
  452 |   await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();
  453 | 
  454 |   await page.evaluate(() => {
  455 |     const state = (window as any).fixtureState();
  456 |     const target = state.pages[9999];
  457 |     target.title = "Refreshed research";
  458 |     target.parentPageId = state.pages[0].id;
  459 |     target.revision = "2";
  460 |     for (const block of state.blocks.filter((b: any) => b.pageId === target.id)) {
  461 |       block.data = { richText: [{ text: "refreshed-body-only" }] };
  462 |       block.revision = "2";
  463 |     }
  464 |     (window as any).emitFixtureEvent("workspace:changed");
  465 |   });
  466 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Refreshed research", exact: true })).toBeVisible();
  467 |   await query.fill("refreshed-body-only");
  468 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  469 |   await expect(dialog.getByRole("button")).toContainText("Refreshed research");
  470 |   await query.fill("only-body-target");
  471 |   await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();
  472 |   await query.press("Escape");
  473 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true })).toBeVisible();
  474 |   await expect(tree.locator(".tree-item")).toHaveCount(9997);
  475 |   await page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true }).click();
  476 |   await page.keyboard.press("Meta+k");
  477 |   await query.fill("refreshed-body-only");
  478 |   await query.press("Enter");
  479 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Refreshed research");
  480 | });
  481 | test("shell navigation, focus, quick switcher, page metadata", async ({
  482 |   page,
  483 | }) => {
  484 |   await expect(
  485 |     page.getByRole("heading", { name: "Make room for your ideas." }),
  486 |   ).toBeVisible();
  487 |   await page.screenshot({ path: "../evidence/phase1-home.png" });
  488 |   await open(page);
  489 |   await page
  490 |     .getByRole("textbox", { name: "Page title" })
  491 |     .fill("A calmer workspace");
  492 |   await page.getByRole("textbox", { name: "Page title" }).press("Tab");
  493 |   await expect
  494 |     .poll(() =>
  495 |       page.evaluate(() => (window as any).fixtureState().pages[0].title),
  496 |     )
  497 |     .toBe("A calmer workspace");
  498 |   await page
  499 |     .getByRole("button", { name: "Favorite page", exact: true })
  500 |     .click();
  501 |   await page.keyboard.press("Meta+k");
  502 |   await expect(
  503 |     page.getByRole("dialog", { name: "Quick switcher" }),
  504 |   ).toBeVisible();
  505 |   await page.getByRole("textbox", { name: "Find a page" }).fill("calmer");
  506 |   await page.keyboard.press("Enter");
  507 |   await expect(
  508 |     page.getByRole("dialog", { name: "Quick switcher" }),
  509 |   ).not.toBeVisible();
  510 |   await expect(page.getByRole("textbox", { name: "Page body" })).toBeVisible();
  511 | });
  512 | test("Unicode composition, RTL, emoji, undo/redo, reopen, raw preservation", async ({
  513 |   page,
  514 | }) => {
  515 |   await open(page);
  516 |   const editor = page.getByRole("textbox", { name: "Page body" });
  517 |   await editor.locator(".block-paragraph .block-text").click();
  518 |   await page.keyboard.press("End");
  519 |   await page.keyboard.insertText(" नमस्ते తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  520 |   await editor.dispatchEvent("compositionstart", { data: "" });
  521 |   await editor.dispatchEvent("compositionupdate", { data: "漢字" });
  522 |   await editor.dispatchEvent("compositionend", { data: "漢字" });
  523 |   await page.keyboard.insertText(" 漢字");
> 524 |   await page.getByRole("button", { name: "Undo", exact: true }).click();
      |                                                                 ^ Error: locator.click: Test timeout of 30000ms exceeded.
  525 |   await page.getByRole("button", { name: "Redo", exact: true }).click();
  526 |   await page.getByRole("button", { name: "Save page", exact: true }).click();
  527 |   await expect(page.getByRole("status")).toHaveText("Saved");
  528 |   await expect
  529 |     .poll(() =>
  530 |       page.evaluate(() =>
  531 |         (window as any)
  532 |           .fixtureState()
  533 |           .blocks.find((b: any) => b.type === "paragraph")
  534 |           .data.richText.map((r: any) => r.text)
  535 |           .join(""),
  536 |       ),
  537 |     )
  538 |     .toContain("తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  539 |   await page.reload();
  540 |   await open(page);
  541 |   await expect(editor).toContainText("తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  542 |   await expect(editor.getByRole("heading", { level: 4 })).toHaveText(
  543 |     "Imported detail",
  544 |   );
  545 |   await expect(editor.locator('td[colspan="2"]')).toBeVisible();
  546 |   await expect
  547 |     .poll(() =>
  548 |       page.evaluate(
  549 |         () =>
  550 |           (window as any)
  551 |             .fixtureState()
  552 |             .blocks.find((b: any) => b.type === "unsupported_external").data
  553 |             .raw,
  554 |       ),
  555 |     )
  556 |     .toEqual({ original: "keep" });
  557 |   await page.screenshot({ path: "../evidence/phase1-editor.png" });
  558 | });
  559 | test("mixed clipboard retains block structure and allocates distinct IDs", async ({
  560 |   page,
  561 |   context,
  562 | }) => {
  563 |   await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  564 |   await open(page);
  565 |   const editor = page.getByRole("textbox", { name: "Page body" });
  566 |   await editor.locator(".block-paragraph .block-text").click();
  567 |   await page.keyboard.press("Meta+a");
  568 |   await page.keyboard.press("Meta+c");
  569 |   await page.keyboard.press("ArrowRight");
  570 |   await page.keyboard.press("End");
  571 |   await page.keyboard.press("Meta+v");
  572 |   await page.getByRole("button", { name: "Save page", exact: true }).click();
  573 |   await expect(page.getByRole("status")).toHaveText("Saved");
  574 |   const blocks = await page.evaluate(
  575 |     () => (window as any).fixtureState().blocks,
  576 |   );
  577 |   expect(new Set(blocks.map((b: any) => b.id)).size).toBe(blocks.length);
  578 |   expect(blocks.filter((b: any) => b.type === "heading_4").length).toBe(2);
  579 |   expect(
  580 |     blocks
  581 |       .filter((b: any) => b.type === "unsupported_external")
  582 |       .every((b: any) => b.data.raw.original === "keep"),
  583 |   ).toBe(true);
  584 | });
  585 | 
  586 | test("repeated splits and formatted paste preserve imported neighbors with complete revision guards", async ({ page, context }) => {
  587 |   await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  588 |   await page.evaluate(() => {
  589 |     const state = (window as any).fixtureState();
  590 |     const make = (number: number, type: string, richText: any[]) => ({
  591 |       id: `019a1234-5678-7000-8000-${String(number).padStart(12, "0")}`,
  592 |       pageId: state.pages[0].id, parentBlockId: null,
  593 |       sortKey: String(number * 1000).padStart(12, "0"), type,
  594 |       schemaVersion: 1, revision: "1", data: { richText },
  595 |     });
  596 |     state.blocks = [
  597 |       make(10, "heading_1", [{ text: "Imported Notion notebook" }]),
  598 |       make(11, "paragraph", [{ text: "Original introduction" }]),
  599 |       make(12, "paragraph", [{ text: "Launch date 2026-09-30" }]),
  600 |       make(13, "paragraph", [{ text: "Milestones", link: "trove://page/019a1234-5678-7000-8000-000000000002" }]),
  601 |     ];
  602 |     localStorage.setItem("fixture", JSON.stringify(state));
  603 |     const app = (window as any).go.main.App, previous = app.Dispatch;
  604 |     (window as any).structuralBatches = [];
  605 |     app.Dispatch = async (method: string, raw: string) => {
  606 |       if (method === "batch") (window as any).structuralBatches.push(JSON.parse(raw));
  607 |       return previous(method, raw);
  608 |     };
  609 |     (window as any).emitFixtureEvent("workspace:changed");
  610 |   });
  611 |   await open(page);
  612 |   const editor = page.getByRole("textbox", { name: "Page body" });
  613 |   await expect(editor).toContainText("Imported Notion notebook");
  614 |   for (const [blockID, offset, label] of [[id(13), 1, "First"], [id(12), 11, "Second"]] as const) {
  615 |     // Place the caret within the imported rich link/date text, then exercise
  616 |     // actual browser split and clipboard transactions rather than JSON edits.
  617 |     await editor.locator(`[data-block-id="${blockID}"] .block-text`).evaluate((element, offset) => {
  618 |       const text = document.createTreeWalker(element, NodeFilter.SHOW_TEXT).nextNode()!;
  619 |       const range = document.createRange();
  620 |       range.setStart(text, offset);
  621 |       range.collapse(true);
  622 |       const selection = window.getSelection()!;
  623 |       selection.removeAllRanges();
  624 |       selection.addRange(range);
```