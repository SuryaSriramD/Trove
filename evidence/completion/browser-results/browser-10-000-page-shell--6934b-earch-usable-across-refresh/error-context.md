# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> 10,000-page shell keeps the full hierarchy and body search usable across refresh
- Location: tests/browser.spec.ts:419:1

# Error details

```
Error: expect(received).toBeLessThan(expected)

Expected: < 2000
Received:   2176.8999999910593
```

# Test source

```ts
  327 |                   touchedBlocks.add(c.block.blockId);
  328 |                 }
  329 |                 if (c.kind === "block.update") {
  330 |                   const b = state.blocks.find((b: any) => b.id === c.blockId);
  331 |                   Object.assign(b, {
  332 |                     type: c.type,
  333 |                     data: c.data,
  334 |                   });
  335 |                   changed.add(b.pageId);
  336 |                   touchedBlocks.add(c.blockId);
  337 |                 }
  338 |                 if (c.kind === "block.delete") {
  339 |                   const b = state.blocks.find((b: any) => b.id === c.blockId);
  340 |                   changed.add(b.pageId);
  341 |                   state.blocks = state.blocks.filter(
  342 |                     (b: any) => b.id !== c.blockId,
  343 |                   );
  344 |                 }
  345 |                 if (c.kind === "block.move") {
  346 |                   const b = state.blocks.find((b: any) => b.id === c.blockId);
  347 |                   b.parentBlockId = c.parentBlockId;
  348 |                   changed.add(b.pageId);
  349 |                   touchedBlocks.add(c.blockId);
  350 |                 }
  351 |                 if (c.kind === "block.reorder") {
  352 |                   c.orderedBlockIds.forEach(
  353 |                     (id: string, i: number) => {
  354 |                       state.blocks.find((b: any) => b.id === id).sortKey =
  355 |                         String((i + 1) * 1000).padStart(12, "0");
  356 |                       touchedBlocks.add(id);
  357 |                     },
  358 |                   );
  359 |                   changed.add(c.pageId);
  360 |                 }
  361 |               }
  362 |               for (const block of state.blocks) {
  363 |                 const before = blockBefore.get(block.id) ?? "0";
  364 |                 if (touchedBlocks.has(block.id)) block.revision = String(Number(before) + 1);
  365 |                 if (before !== block.revision) revisions.push({ target: { entityType: "block", entityId: block.id }, before, after: block.revision, deleted: false });
  366 |               }
  367 |               for (const id of changed) {
  368 |                 const p = state.pages.find((p: any) => p.id === id);
  369 |                 const before = p.revision;
  370 |                 p.revision = String(Number(before) + 1);
  371 |                 revisions.push({
  372 |                   target: { entityType: "page", entityId: id },
  373 |                   before,
  374 |                   after: p.revision,
  375 |                   deleted: false,
  376 |                 });
  377 |               }
  378 |               localStorage.setItem("fixture", JSON.stringify(state));
  379 |               return ok({
  380 |                 requestId: req.requestId,
  381 |                 transactionId: makeID(),
  382 |                 eventCursor: "2",
  383 |                 revisions,
  384 |                 replayed: false,
  385 |               });
  386 |             },
  387 |             PickFile: async () => "",
  388 |             StageUpload: async (
  389 |               name: string,
  390 |               mediaType: string,
  391 |               base64: string,
  392 |             ) => {
  393 |               const metrics = (window as any).attachmentMetrics;
  394 |               metrics.maxConcurrentUploads = Math.max(
  395 |                 metrics.maxConcurrentUploads,
  396 |                 ++activeUploads,
  397 |               );
  398 |               // Model an asynchronous IPC boundary so concurrent decodes/writes are detectable.
  399 |               await new Promise((resolve) => setTimeout(resolve, 5));
  400 |               const handle = makeID();
  401 |               selections.set(handle, { name, mediaType, base64 });
  402 |               metrics.stagedNames.push(name);
  403 |               activeUploads--;
  404 |               return handle;
  405 |             },
  406 |           },
  407 |         },
  408 |       };
  409 |     },
  410 |     { fixture: testInfo.title === largeWorkspaceTest ? largeWorkspaceFixture() : fixture },
  411 |   );
  412 |   await page.goto(process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173");
  413 | });
  414 | const open = async (page: any) =>
  415 |   page
  416 |     .getByRole("button", { name: "🌱 A garden of ideas", exact: true })
  417 |     .first()
  418 |     .click();
  419 | test(largeWorkspaceTest, async ({ page }) => {
  420 |   await expect.poll(() => page.evaluate(() => (window as any).fixtureShellReadyMs)).toBeGreaterThan(0);
  421 |   // This is renderer readiness from the snapshot response, not native process startup.
  422 |   const shellMs = await page.evaluate(() => (window as any).fixtureShellReadyMs);
  423 |   await test.info().attach("large-workspace-renderer-ready", {
  424 |     body: JSON.stringify({ pages: 10000, blocks: 20000, shellMs, endpoint: "snapshot response to rendered shell plus two frames" }),
  425 |     contentType: "application/json",
  426 |   });
> 427 |   expect(shellMs).toBeLessThan(2000);
      |                   ^ Error: expect(received).toBeLessThan(expected)
  428 |   const tree = page.getByRole("navigation", { name: "Page tree" });
  429 |   await expect(tree.locator(".tree-item")).toHaveCount(9997);
  430 |   await expect(tree.locator(".tree-item").nth(1)).toContainText("Page 00001");
  431 |   await expect(tree.getByRole("button", { name: "▤ Page 09999", exact: true })).toHaveCount(1);
  432 |   await expect(tree.getByRole("button", { name: "▤ Page 09996", exact: true })).toHaveCount(0);
  433 | 
  434 |   await page.keyboard.press("Meta+k");
  435 |   const dialog = page.getByRole("dialog", { name: "Quick switcher" });
  436 |   const query = dialog.getByRole("textbox", { name: "Find a page" });
  437 |   await query.fill("only-body-target తెలుగు");
  438 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  439 |   await expect(dialog.getByRole("button")).toContainText("Page 09999");
  440 |   await query.press("Enter");
  441 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");
  442 |   await expect(page.getByRole("textbox", { name: "Page body" })).toContainText("only-body-target తెలుగు");
  443 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true })).toBeVisible();
  444 |   await page.locator(".breadcrumbs").getByRole("button", { name: "Page 09975", exact: true }).click();
  445 |   await page.getByRole("button", { name: "Go back", exact: true }).click();
  446 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Page 09999");
  447 | 
  448 |   await page.keyboard.press("Meta+k");
  449 |   await query.fill("record-body-target");
  450 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  451 |   await expect(dialog.getByRole("button")).toContainText("Page 09996");
  452 |   await query.fill("hidden-only-token");
  453 |   await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();
  454 | 
  455 |   await page.evaluate(() => {
  456 |     const state = (window as any).fixtureState();
  457 |     const target = state.pages[9999];
  458 |     target.title = "Refreshed research";
  459 |     target.parentPageId = state.pages[0].id;
  460 |     target.revision = "2";
  461 |     for (const block of state.blocks.filter((b: any) => b.pageId === target.id)) {
  462 |       block.data = { richText: [{ text: "refreshed-body-only" }] };
  463 |       block.revision = "2";
  464 |     }
  465 |     (window as any).emitFixtureEvent("workspace:changed");
  466 |   });
  467 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Refreshed research", exact: true })).toBeVisible();
  468 |   await query.fill("refreshed-body-only");
  469 |   await expect(dialog.getByRole("button")).toHaveCount(1);
  470 |   await expect(dialog.getByRole("button")).toContainText("Refreshed research");
  471 |   await query.fill("only-body-target");
  472 |   await expect(dialog.getByText("No matching pages. Try another phrase.")).toBeVisible();
  473 |   await query.press("Escape");
  474 |   await expect(page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true })).toBeVisible();
  475 |   await expect(tree.locator(".tree-item")).toHaveCount(9997);
  476 |   await page.locator(".breadcrumbs").getByRole("button", { name: "Page 00000", exact: true }).click();
  477 |   await page.keyboard.press("Meta+k");
  478 |   await query.fill("refreshed-body-only");
  479 |   await query.press("Enter");
  480 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("Refreshed research");
  481 | });
  482 | test("shell navigation, focus, quick switcher, page metadata", async ({
  483 |   page,
  484 | }) => {
  485 |   await expect(
  486 |     page.getByRole("heading", { name: "Make room for your ideas." }),
  487 |   ).toBeVisible();
  488 |   await page.screenshot({ path: `${process.env.TROVE_BROWSER_SCREENSHOTS ?? "../evidence"}/phase1-home.png` });
  489 |   await open(page);
  490 |   await page
  491 |     .getByRole("textbox", { name: "Page title" })
  492 |     .fill("A calmer workspace");
  493 |   await page.getByRole("textbox", { name: "Page title" }).press("Tab");
  494 |   await expect
  495 |     .poll(() =>
  496 |       page.evaluate(() => (window as any).fixtureState().pages[0].title),
  497 |     )
  498 |     .toBe("A calmer workspace");
  499 |   await page
  500 |     .getByRole("button", { name: "Favorite page", exact: true })
  501 |     .click();
  502 |   await page.keyboard.press("Meta+k");
  503 |   await expect(
  504 |     page.getByRole("dialog", { name: "Quick switcher" }),
  505 |   ).toBeVisible();
  506 |   await page.getByRole("textbox", { name: "Find a page" }).fill("calmer");
  507 |   await page.keyboard.press("Enter");
  508 |   await expect(
  509 |     page.getByRole("dialog", { name: "Quick switcher" }),
  510 |   ).not.toBeVisible();
  511 |   await expect(page.getByRole("textbox", { name: "Page body" })).toBeVisible();
  512 | });
  513 | test("Unicode composition, RTL, emoji, undo/redo, reopen, raw preservation", async ({
  514 |   page,
  515 | }) => {
  516 |   await open(page);
  517 |   const editor = page.getByRole("textbox", { name: "Page body" });
  518 |   await editor.locator(".block-paragraph .block-text").click();
  519 |   await page.keyboard.press("End");
  520 |   await page.keyboard.insertText(" नमस्ते తెలుగు 日本語 中文 العربية 👩🏽‍💻");
  521 |   await editor.dispatchEvent("compositionstart", { data: "" });
  522 |   await editor.dispatchEvent("compositionupdate", { data: "漢字" });
  523 |   await editor.dispatchEvent("compositionend", { data: "漢字" });
  524 |   await page.keyboard.insertText(" 漢字");
  525 |   await page.getByRole("button", { name: "Undo", exact: true }).click();
  526 |   await page.getByRole("button", { name: "Redo", exact: true }).click();
  527 |   await page.getByRole("button", { name: "Save page", exact: true }).click();
```