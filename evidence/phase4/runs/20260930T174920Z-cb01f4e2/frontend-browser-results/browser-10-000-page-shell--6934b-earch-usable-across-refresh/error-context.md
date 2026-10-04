# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> 10,000-page shell keeps the full hierarchy and body search usable across refresh
- Location: tests/browser.spec.ts:418:1

# Error details

```
Error: expect(locator).toHaveValue(expected) failed

Locator:  getByRole('textbox', { name: 'Page title' })
Expected: "Refreshed research"
Received: "Page 09999"
Timeout:  5000ms

Call log:
  - Expect "toHaveValue" getByRole('textbox', { name: 'Page title' }) with timeout 5000ms
  - waiting for getByRole('textbox', { name: 'Page title' })
    13 × locator resolved to <input class="page-title" placeholder="Untitled" aria-label="Page title" value="Refreshed research"/>
       - unexpected value "Page 09999"

```

```yaml
- textbox "Page title":
  - /placeholder: Untitled
  - text: Page 09999
```

# Test source

```ts
  379 |                 requestId: req.requestId,
  380 |                 transactionId: makeID(),
  381 |                 eventCursor: "2",
  382 |                 revisions,
  383 |                 replayed: false,
  384 |               });
  385 |             },
  386 |             PickFile: async () => "",
  387 |             StageUpload: async (
  388 |               name: string,
  389 |               mediaType: string,
  390 |               base64: string,
  391 |             ) => {
  392 |               const metrics = (window as any).attachmentMetrics;
  393 |               metrics.maxConcurrentUploads = Math.max(
  394 |                 metrics.maxConcurrentUploads,
  395 |                 ++activeUploads,
  396 |               );
  397 |               // Model an asynchronous IPC boundary so concurrent decodes/writes are detectable.
  398 |               await new Promise((resolve) => setTimeout(resolve, 5));
  399 |               const handle = makeID();
  400 |               selections.set(handle, { name, mediaType, base64 });
  401 |               metrics.stagedNames.push(name);
  402 |               activeUploads--;
  403 |               return handle;
  404 |             },
  405 |           },
  406 |         },
  407 |       };
  408 |     },
  409 |     { fixture: testInfo.title === largeWorkspaceTest ? largeWorkspaceFixture() : fixture },
  410 |   );
  411 |   await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  412 | });
  413 | const open = async (page: any) =>
  414 |   page
  415 |     .getByRole("button", { name: "🌱 A garden of ideas", exact: true })
  416 |     .first()
  417 |     .click();
  418 | test(largeWorkspaceTest, async ({ page }) => {
  419 |   await expect.poll(() => page.evaluate(() => (window as any).fixtureShellReadyMs)).toBeGreaterThan(0);
  420 |   // This is renderer readiness from the snapshot response, not native process startup.
  421 |   const shellMs = await page.evaluate(() => (window as any).fixtureShellReadyMs);
  422 |   await test.info().attach("large-workspace-renderer-ready", {
  423 |     body: JSON.stringify({ pages: 10000, blocks: 20000, shellMs, endpoint: "snapshot response to rendered shell plus two frames" }),
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
> 479 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Refreshed research");
      |                                                                   ^ Error: expect(locator).toHaveValue(expected) failed
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
  524 |   await page.getByRole("button", { name: "Undo", exact: true }).click();
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
```