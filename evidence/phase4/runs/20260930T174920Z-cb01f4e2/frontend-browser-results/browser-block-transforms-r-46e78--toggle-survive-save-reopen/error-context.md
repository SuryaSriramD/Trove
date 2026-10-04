# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> block transforms, rich marks, find and nested toggle survive save/reopen
- Location: tests/browser.spec.ts:908:1

# Error details

```
Error: expect(received).toBe(expected) // Object.is equality

Expected: true
Received: false
```

# Page snapshot

```yaml
- generic [ref=e3]:
  - link "Skip to content" [ref=e4] [cursor=pointer]:
    - /url: "#main"
  - complementary [ref=e5]:
    - generic [ref=e6]:
      - generic [ref=e7]: ✳
      - strong [ref=e8]: Trove
      - generic [ref=e9]: LOCAL
    - button "S My workspace Room for what matters ⌄" [ref=e10] [cursor=pointer]:
      - generic [ref=e11]: S
      - generic [ref=e12]:
        - text: My workspace
        - generic [ref=e13]: Room for what matters
      - generic [ref=e14]: ⌄
    - button "⌕ Quick search ⌘ K" [ref=e15] [cursor=pointer]:
      - generic [ref=e16]: ⌕ Quick search
      - generic [ref=e17]: ⌘ K
    - navigation "Workspace" [ref=e18]:
      - button "⌂ Home" [ref=e19] [cursor=pointer]:
        - generic [ref=e20]: ⌂
        - text: Home
      - button "▣ Inbox" [ref=e21] [cursor=pointer]:
        - generic [ref=e22]: ▣
        - text: Inbox
      - button "▤ Pages" [ref=e23] [cursor=pointer]:
        - generic [ref=e24]: ▤
        - text: Pages
      - button "✓ Tasks 0" [ref=e25] [cursor=pointer]:
        - generic [ref=e26]: ✓
        - text: Tasks
        - generic [ref=e27]: "0"
      - button "◉ Meetings" [ref=e28] [cursor=pointer]:
        - generic [ref=e29]: ◉
        - text: Meetings
      - button "⌕ Search" [ref=e30] [cursor=pointer]:
        - generic [ref=e31]: ⌕
        - text: Search
      - button "↓ Import" [ref=e32] [cursor=pointer]:
        - generic [ref=e33]: ↓
        - text: Import
      - button "↻ Jobs" [ref=e34] [cursor=pointer]:
        - generic [ref=e35]: ↻
        - text: Jobs
      - button "✧ AI" [ref=e36] [cursor=pointer]:
        - generic [ref=e37]: ✧
        - text: AI
      - button "⇄ Providers" [ref=e38] [cursor=pointer]:
        - generic [ref=e39]: ⇄
        - text: Providers
      - button "⊕ Capture" [ref=e40] [cursor=pointer]:
        - generic [ref=e41]: ⊕
        - text: Capture
      - button "◷ Automations" [ref=e42] [cursor=pointer]:
        - generic [ref=e43]: ◷
        - text: Automations
      - button "⚙ Settings" [ref=e44] [cursor=pointer]:
        - generic [ref=e45]: ⚙
        - text: Settings
    - generic [ref=e46]: FAVORITES
    - paragraph [ref=e48]: Keep your essentials close.
    - generic [ref=e49]:
      - generic [ref=e50]: YOUR PAGES
      - button "Create page" [ref=e51] [cursor=pointer]: ＋
    - navigation "Page tree" [ref=e52]:
      - button "🌱 A garden of ideas" [ref=e54] [cursor=pointer]:
        - generic [ref=e55]: 🌱
        - generic [ref=e56]: A garden of ideas
    - generic [ref=e57]:
      - text: On your device
      - generic [ref=e59]: ↙
  - generic [ref=e60]:
    - banner [ref=e61]:
      - generic [ref=e62]:
        - button "Go back" [ref=e63] [cursor=pointer]: ‹
        - button "Go forward" [disabled] [ref=e64]: ›
        - generic [ref=e65]: My workspace
        - generic [ref=e66]: /
        - button "A garden of ideas" [ref=e68] [cursor=pointer]
      - generic [ref=e69]:
        - button "Favorite page" [ref=e70] [cursor=pointer]: ☆
        - button "Page options" [ref=e71] [cursor=pointer]: •••
        - generic [ref=e72]: S
    - tablist "Open pages" [ref=e73]:
      - generic [ref=e74]:
        - tab "▤ A garden of ideas" [selected] [ref=e75] [cursor=pointer]
        - button "Close A garden of ideas" [ref=e76] [cursor=pointer]: ×
    - main [ref=e77]:
      - article [ref=e78]:
        - generic [ref=e79]:
          - button "Set page icon" [ref=e80] [cursor=pointer]: 🌱
          - textbox "Page title" [ref=e81]:
            - /placeholder: Untitled
            - text: A garden of ideas
          - generic [ref=e82]:
            - text: Your private workspace
            - generic [ref=e83]: ·
            - text: Edited Sep 30
            - generic [ref=e84]: ·
            - button "＋ Subpage" [ref=e85] [cursor=pointer]
            - button "↗ Attach file" [ref=e86] [cursor=pointer]
        - generic [ref=e87]:
          - button "Lock page" [ref=e88] [cursor=pointer]
          - generic [ref=e89]: Alt + ↑ / ↓ navigates blocks
        - group [ref=e90]:
          - toolbar "Text formatting" [ref=e91]:
            - combobox "Block type" [ref=e92]:
              - option "Text"
              - option "Heading 1"
              - option "Heading 2"
              - option "Heading 3"
              - option "Heading 4"
              - option "Bullet list" [selected]
              - option "Numbered list"
              - option "Toggle"
              - option "Quote"
              - option "Callout"
            - button "bold" [ref=e94] [cursor=pointer]: B
            - button "italic" [ref=e95] [cursor=pointer]: I
            - button "underline" [ref=e96] [cursor=pointer]: U
            - button "strike" [ref=e97] [cursor=pointer]: S
            - button "Add link" [ref=e98] [cursor=pointer]: ↗
            - button "Undo" [ref=e100] [cursor=pointer]: ↶
            - button "Redo" [ref=e101] [cursor=pointer]: ↷
            - button "Find in page" [ref=e102] [cursor=pointer]: ⌕
            - button "＋ Insert" [ref=e103] [cursor=pointer]
            - status [ref=e104]: Saved
            - button "Save page" [active] [ref=e105] [cursor=pointer]: Save
          - textbox "Page body" [ref=e107]:
            - generic [ref=e109]:
              - generic [ref=e110]:
                - button "Drag block" [ref=e111]: ⠿
                - button "Copy block link" [ref=e112] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e113] [cursor=pointer]: ⧉
              - heading "A place to begin" [level=1] [ref=e114]
            - generic [ref=e117]:
              - generic [ref=e118]:
                - button "Drag block" [ref=e119]: ⠿
                - button "Copy block link" [ref=e120] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e121] [cursor=pointer]: ⧉
              - generic [ref=e122]: Capture the small things. Connect the useful ones.
            - generic [ref=e125]:
              - generic [ref=e126]:
                - button "Drag block" [ref=e127]: ⠿
                - button "Copy block link" [ref=e128] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e129] [cursor=pointer]: ⧉
              - button "💡" [ref=e130] [cursor=pointer]
              - generic [ref=e131]: Your work stays on this device.
            - generic [ref=e134]:
              - generic [ref=e135]:
                - button "Drag block" [ref=e136]: ⠿
                - button "Copy block link" [ref=e137] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e138] [cursor=pointer]: ⧉
              - heading "Imported detail" [level=4] [ref=e139]
            - generic [ref=e142]:
              - generic [ref=e143]:
                - button "Drag block" [ref=e144]: ⠿
                - button "Copy block link" [ref=e145] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e146] [cursor=pointer]: ⧉
              - generic [ref=e147]:
                - generic [ref=e148]:
                  - generic [ref=e149]:
                    - text: First cell
                    - combobox "First cell" [ref=e150]:
                      - option "Choose" [selected]
                      - option "1:1"
                  - generic [ref=e151]:
                    - text: Last cell
                    - combobox "Last cell" [ref=e152]:
                      - option "Choose" [selected]
                      - option "1:1"
                  - button "Merge range" [ref=e153] [cursor=pointer]
                - table [ref=e154]:
                  - rowgroup [ref=e155]:
                    - row [ref=e156]:
                      - cell [ref=e157]:
                        - textbox "Row 1 column 1" [ref=e158]: Merged source cell
                        - button "Restore cells" [ref=e159] [cursor=pointer]
            - generic [ref=e161]:
              - generic [ref=e162]:
                - button "Drag block" [ref=e163]: ⠿
                - button "Copy block link" [ref=e164] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e165] [cursor=pointer]: ⧉
              - generic [ref=e166]:
                - generic [ref=e167]: Preserved · source-widget
                - paragraph [ref=e168]: A retained source widget
                - group [ref=e169]:
                  - generic "View original structure" [ref=e170] [cursor=pointer]
            - generic [ref=e172]:
              - generic [ref=e173]:
                - button "Drag block" [ref=e174]: ⠿
                - button "Copy block link" [ref=e175] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e176] [cursor=pointer]: ⧉
              - button "Toggle content" [expanded] [ref=e177] [cursor=pointer]: ▸
              - generic [ref=e178]: Expandable idea
            - generic [ref=e181]:
              - text: •
              - generic [ref=e182]:
                - button "Drag block" [ref=e183]: ⠿
                - button "Copy block link" [ref=e184] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=e185] [cursor=pointer]: ⧉
              - generic [ref=e186]: Nested searchable detail
          - button "＋ Add a thought · type / for blocks" [ref=e188] [cursor=pointer]
        - group [ref=e189]:
          - generic "Templates and related pages" [ref=e190]
          - option "New page" [selected]
        - group [ref=e191]:
          - generic "✧ Ask AI or work with this page" [ref=e192]
          - option "Workspace"
          - option "Current page · A garden of ideas" [selected]
          - option "Project"
          - option "answer" [selected]
          - option "rewrite"
          - option "summarize"
          - option "explain"
          - option "shorten"
          - option "expand"
          - option "outline"
          - option "generate"
          - option "database"
          - option "schema"
          - option "mermaid"
          - option "code_explain"
          - option "code_refactor"
          - option "code_comment"
          - option "organize"
          - option "workspace_agent"
          - option "Choose installed model" [selected]
          - option "Whole page" [selected]
          - option "A place to begin"
          - option "Capture the small things. Connect the useful ones."
          - option "Your work stays on this device."
          - option "Imported detail"
          - option "table"
          - option "unsupported_external"
          - option "Expandable idea"
          - option "Nested searchable detail"
          - option "Preference" [selected]
          - option "Project context"
          - option "Decision with evidence"
          - option "Workspace" [selected]
          - option "Current page"
          - option "Project"
        - generic [ref=e193]:
          - generic [ref=e194]: LINKED TO THIS PAGE
          - paragraph [ref=e195]: Backlinks will appear here when another page links to this one.
    - contentinfo [ref=e196]:
      - generic [ref=e197]: Made for your own way of thinking.
      - generic [ref=e198]: Private by default · Saved on this device
```

# Test source

```ts
  838  |     "Field notes",
  839  |   );
  840  |   expect(
  841  |     await page.evaluate(
  842  |       (pageId: string) =>
  843  |         (window as any)
  844  |           .fixtureState()
  845  |           .pages.filter((p: any) => p.id === pageId)
  846  |           .map((p: any) => ({
  847  |             parent: p.parentPageId,
  848  |             icon: p.icon,
  849  |             archived: p.archivedAt,
  850  |           })),
  851  |       pageId,
  852  |     ),
  853  |   ).toEqual([{ parent: id(2), icon: "📚", archived: null }]);
  854  |   await page
  855  |     .getByRole("button", { name: "Close Field notes", exact: true })
  856  |     .click();
  857  |   await expect(
  858  |     page.getByRole("tab", { name: "▤ Field notes", exact: true }),
  859  |   ).toHaveCount(0);
  860  | });
  861  | 
  862  | test("all named surfaces open and created page links generate navigable backlinks", async ({
  863  |   page,
  864  | }) => {
  865  |   for (const [surface, heading] of [
  866  |     ["Home", "Make room for your ideas."],
  867  |     ["Inbox", "Inbox"],
  868  |     ["Pages", "Pages"],
  869  |     ["Tasks", "Tasks"],
  870  |     ["Meetings", "Meetings"],
  871  |     ["Search", "Search your workspace"],
  872  |     ["Import", "Import & export"],
  873  |     ["Settings", "Settings"],
  874  |   ]) {
  875  |     await navSurface(page, surface);
  876  |     await expect(
  877  |       page.getByRole("heading", { name: heading, exact: true }),
  878  |     ).toBeVisible();
  879  |   }
  880  |   await newPage(page, "Connected thought");
  881  |   await page.getByRole("button", { name: "＋ Insert", exact: true }).click();
  882  |   page.once("dialog", (d) => d.accept("A garden of ideas"));
  883  |   await page
  884  |     .getByRole("dialog", { name: "Insert a block" })
  885  |     .getByRole("button", { name: /^Page link/ })
  886  |     .click();
  887  |   await save(page);
  888  |   await page
  889  |     .getByRole("textbox", { name: "Page body" })
  890  |     .getByRole("button", { name: "↗ A garden of ideas", exact: true })
  891  |     .click();
  892  |   await expect(
  893  |     page
  894  |       .locator(".backlinks")
  895  |       .getByRole("button", { name: "↗ Connected thought", exact: true }),
  896  |   ).toBeVisible();
  897  |   await page
  898  |     .locator(".backlinks")
  899  |     .getByRole("button", { name: "↗ Connected thought", exact: true })
  900  |     .click();
  901  |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
  902  |     "Connected thought",
  903  |   );
  904  |   await navSurface(page, "Inbox");
  905  |   await expect(page.locator(".card-grid")).toContainText("Connected thought");
  906  | });
  907  | 
  908  | test("block transforms, rich marks, find and nested toggle survive save/reopen", async ({
  909  |   page,
  910  | }) => {
  911  |   await open(page);
  912  |   await addText(page, "Expandable idea");
  913  |   await page
  914  |     .getByRole("combobox", { name: "Block type" })
  915  |     .selectOption("toggle");
  916  |   await addText(page, "Nested searchable detail");
  917  |   await page
  918  |     .getByRole("combobox", { name: "Block type" })
  919  |     .selectOption("bulleted_list_item");
  920  |   await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  921  |   await page.keyboard.press("Tab");
  922  |   await page.keyboard.press("Home");
  923  |   await page.keyboard.press("Shift+End");
  924  |   await page.getByRole("button", { name: "bold", exact: true }).click();
  925  |   await save(page);
  926  |   const added = await page.evaluate(() =>
  927  |     (window as any)
  928  |       .fixtureState()
  929  |       .blocks.filter((b: any) =>
  930  |         ["toggle", "bulleted_list_item"].includes(b.type),
  931  |       ),
  932  |   );
  933  |   const parent = added.find((b: any) => b.type === "toggle"),
  934  |     child = added.find((b: any) => b.type === "bulleted_list_item");
  935  |   expect(child.parentBlockId).toBe(parent.id);
  936  |   expect(
  937  |     child.data.richText.some((span: any) => span.marks?.includes("bold")),
> 938  |   ).toBe(true);
       |     ^ Error: expect(received).toBe(expected) // Object.is equality
  939  |   await page.reload();
  940  |   await open(page);
  941  |   const editor = page.getByRole("textbox", { name: "Page body" });
  942  |   const childNode = editor.locator(`[data-block-id="${child.id}"]`);
  943  |   await expect(childNode).toBeVisible();
  944  |   await editor
  945  |     .getByRole("button", { name: "Toggle content", exact: true })
  946  |     .click();
  947  |   await expect(childNode).toBeHidden();
  948  |   await editor
  949  |     .getByRole("button", { name: "Toggle content", exact: true })
  950  |     .click();
  951  |   await expect(childNode).toBeVisible();
  952  |   await page.getByRole("button", { name: "Find in page", exact: true }).click();
  953  |   await page
  954  |     .getByRole("textbox", { name: "Find text", exact: true })
  955  |     .fill("searchable");
  956  |   await page.getByRole("button", { name: "Next match", exact: true }).click();
  957  |   await expect
  958  |     .poll(() => page.evaluate(() => window.getSelection()?.toString()))
  959  |     .toBe("searchable");
  960  |   await page.getByRole("button", { name: "Close", exact: true }).click();
  961  |   await page
  962  |     .getByRole("textbox", { name: "Row 1 column 1", exact: true })
  963  |     .fill("Updated merged cell");
  964  |   await save(page);
  965  |   expect(
  966  |     await page.evaluate(
  967  |       () =>
  968  |         (window as any)
  969  |           .fixtureState()
  970  |           .blocks.find((b: any) => b.type === "table").data.cells[0],
  971  |     ),
  972  |   ).toMatchObject({
  973  |     rowSpan: 1,
  974  |     colSpan: 2,
  975  |     richText: [{ text: "Updated merged cell" }],
  976  |     preMergeCells: [{ value: "Original A" }],
  977  |   });
  978  | });
  979  | 
  980  | test("multi-file clipboard and file drop serialize uploads and preserve media captions and cover", async ({
  981  |   page,
  982  | }) => {
  983  |   const pixel =
  984  |     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5L8AAAAASUVORK5CYII=";
  985  |   await page.route("**/files/**", (route) =>
  986  |     route.fulfill({
  987  |       status: 200,
  988  |       contentType: "image/png",
  989  |       body: Buffer.from(pixel, "base64"),
  990  |     }),
  991  |   );
  992  |   await open(page);
  993  |   await page
  994  |     .getByRole("textbox", { name: "Page body" })
  995  |     .evaluate((editor: any) => {
  996  |       const data = new DataTransfer();
  997  |       for (const [name, type] of [
  998  |         ["image.png", "image/png"],
  999  |         ["notes.pdf", "application/pdf"],
  1000 |         ["recording.wav", "audio/wav"],
  1001 |         ["clip.mp4", "video/mp4"],
  1002 |       ])
  1003 |         data.items.add(new File([`Fixture bytes for ${name}`], name, { type }));
  1004 |       editor.dispatchEvent(
  1005 |         new ClipboardEvent("paste", {
  1006 |           clipboardData: data,
  1007 |           bubbles: true,
  1008 |           cancelable: true,
  1009 |         }),
  1010 |       );
  1011 |     });
  1012 |   await expect
  1013 |     .poll(() =>
  1014 |       page.evaluate(() => (window as any).fixtureState().files.length),
  1015 |     )
  1016 |     .toBe(4);
  1017 |   await page.locator("main").evaluate((main: any) => {
  1018 |     const data = new DataTransfer();
  1019 |     data.items.add(
  1020 |       new File(["Retained notes"], "notes.txt", { type: "text/plain" }),
  1021 |     );
  1022 |     main.dispatchEvent(
  1023 |       new DragEvent("drop", {
  1024 |         dataTransfer: data,
  1025 |         bubbles: true,
  1026 |         cancelable: true,
  1027 |       }),
  1028 |     );
  1029 |   });
  1030 |   await expect
  1031 |     .poll(() =>
  1032 |       page.evaluate(() => (window as any).fixtureState().files.length),
  1033 |     )
  1034 |     .toBe(5);
  1035 |   const metrics = await page.evaluate(() => (window as any).attachmentMetrics);
  1036 |   expect(metrics.maxConcurrentUploads).toBe(1);
  1037 |   expect(metrics.stagedNames).toEqual([
  1038 |     "image.png",
```