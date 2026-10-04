# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> insert real code, Mermaid and KaTeX rendering
- Location: tests/browser.spec.ts:662:1

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: locator('.code-preview .hljs-comment')
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" locator('.code-preview .hljs-comment') with timeout 5000ms
  - waiting for locator('.code-preview .hljs-comment')

```

```yaml
- link "Skip to content":
  - /url: "#main"
- complementary:
  - text: ✳
  - strong: Trove
  - text: LOCAL
  - button "S My workspace Room for what matters ⌄"
  - button "⌕ Quick search ⌘ K"
  - navigation "Workspace":
    - button "⌂ Home"
    - button "▣ Inbox"
    - button "▤ Pages"
    - button "✓ Tasks 0"
    - button "◉ Meetings"
    - button "⌕ Search"
    - button "↓ Import"
    - button "↻ Jobs"
    - button "✧ AI"
    - button "⇄ Providers"
    - button "⊕ Capture"
    - button "◷ Automations"
    - button "◇ Intelligence"
    - button "⚙ Settings"
  - text: FAVORITES
  - paragraph: Keep your essentials close.
  - text: YOUR PAGES
  - button "Create page": ＋
  - navigation "Page tree":
    - button "🌱 A garden of ideas"
  - text: On your device ↙
- banner:
  - button "Go back": ‹
  - button "Go forward" [disabled]: ›
  - text: My workspace /
  - button "A garden of ideas"
  - button "Favorite page": ☆
  - button "Page options": •••
  - text: S
- tablist "Open pages":
  - tab "▤ A garden of ideas" [selected]
  - button "Close A garden of ideas": ×
- main:
  - article:
    - button "Set page icon": 🌱
    - textbox "Page title":
      - /placeholder: Untitled
      - text: A garden of ideas
    - text: Your private workspace · Edited Sep 30 ·
    - button "＋ Subpage"
    - button "↗ Attach file"
    - button "Lock page"
    - text: Alt + ↑ / ↓ navigates blocks
    - group:
      - toolbar "Text formatting":
        - combobox "Block type":
          - option "Text" [selected]
          - option "Heading 1"
          - option "Heading 2"
          - option "Heading 3"
          - option "Heading 4"
          - option "Bullet list"
          - option "Numbered list"
          - option "Toggle"
          - option "Quote"
          - option "Callout"
        - button "bold": B
        - button "italic": I
        - button "underline": U
        - button "strike": S
        - text: Color
        - combobox "Text color":
          - option "Default" [selected]
          - option "gray"
          - option "brown"
          - option "orange"
          - option "yellow"
          - option "green"
          - option "blue"
          - option "purple"
          - option "pink"
          - option "red"
        - text: Highlight
        - combobox "Text highlight":
          - option "Default" [selected]
          - option "gray"
          - option "brown"
          - option "orange"
          - option "yellow"
          - option "green"
          - option "blue"
          - option "purple"
          - option "pink"
          - option "red"
        - button "Add link": ↗
        - button "Undo": ↶
        - button "Redo": ↷
        - button "Find in page": ⌕
        - button "＋ Insert"
        - status: Saved
        - button "Save page": Save
      - textbox "Page body":
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - heading "A place to begin" [level=1]
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - button "💡"
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - heading "Imported detail" [level=4]
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - group: Table structure
        - group:
          - text: First cell
          - combobox "First cell":
            - option "Choose" [selected]
            - option "1:1"
          - text: Last cell
          - combobox "Last cell":
            - option "Choose" [selected]
            - option "1:1"
          - button "Merge range"
        - table:
          - rowgroup:
            - row "Row 1 column 1 formatting Merged source cell Restore cells":
              - columnheader "Row 1 column 1 formatting Merged source cell Restore cells":
                - toolbar "Row 1 column 1 formatting":
                  - text: Color
                  - combobox "Row 1 column 1 color":
                    - option "Default" [selected]
                    - option "gray"
                    - option "brown"
                    - option "orange"
                    - option "yellow"
                    - option "green"
                    - option "blue"
                    - option "purple"
                    - option "pink"
                    - option "red"
                  - text: Highlight
                  - combobox "Row 1 column 1 highlight":
                    - option "Default" [selected]
                    - option "gray"
                    - option "brown"
                    - option "orange"
                    - option "yellow"
                    - option "green"
                    - option "blue"
                    - option "purple"
                    - option "pink"
                    - option "red"
                  - button "Row 1 column 1 bold": bold
                  - button "Row 1 column 1 italic": italic
                  - button "Row 1 column 1 underline": underline
                  - button "Row 1 column 1 strike": strike
                  - button "Row 1 column 1 code": code
                  - button "Row 1 column 1 link": Link
                - textbox "Row 1 column 1":
                  - paragraph: Merged source cell
                - button "Restore cells"
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - paragraph: A retained source widget
        - group: View original structure
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - textbox "Code language": javascript
        - textbox "Code filename":
          - /placeholder: Optional filename
        - button "Copy code"
        - checkbox "Wrap code" [checked]
        - checkbox "Code line numbers"
        - code: // Your code here
        - group: Edit source
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - document:
          - paragraph: Idea
          - paragraph: Create
          - paragraph: Learn
        - group: Edit Mermaid
        - button "Drag block": ⠿
        - button "Copy block link": ↗
        - button "Duplicate block": ⧉
        - math: E = m c 2
        - group: Edit equation
      - button "＋ Add a thought · type / for blocks"
    - group: Templates and related pages
    - group: ✧ Ask AI or work with this page
    - text: LINKED TO THIS PAGE
    - paragraph: Backlinks will appear here when another page links to this one.
- contentinfo: Made for your own way of thinking. Private by default · Saved on this device
```

# Test source

```ts
  578 |   );
  579 |   expect(new Set(blocks.map((b: any) => b.id)).size).toBe(blocks.length);
  580 |   expect(blocks.filter((b: any) => b.type === "heading_4").length).toBe(2);
  581 |   expect(
  582 |     blocks
  583 |       .filter((b: any) => b.type === "unsupported_external")
  584 |       .every((b: any) => b.data.raw.original === "keep"),
  585 |   ).toBe(true);
  586 | });
  587 | 
  588 | test("repeated splits and formatted paste preserve imported neighbors with complete revision guards", async ({ page, context }) => {
  589 |   await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  590 |   await page.evaluate(() => {
  591 |     const state = (window as any).fixtureState();
  592 |     const make = (number: number, type: string, richText: any[]) => ({
  593 |       id: `019a1234-5678-7000-8000-${String(number).padStart(12, "0")}`,
  594 |       pageId: state.pages[0].id, parentBlockId: null,
  595 |       sortKey: String(number * 1000).padStart(12, "0"), type,
  596 |       schemaVersion: 1, revision: "1", data: { richText },
  597 |     });
  598 |     state.blocks = [
  599 |       make(10, "heading_1", [{ text: "Imported Notion notebook" }]),
  600 |       make(11, "paragraph", [{ text: "Original introduction" }]),
  601 |       make(12, "paragraph", [{ text: "Launch date 2026-09-30" }]),
  602 |       make(13, "paragraph", [{ text: "Milestones", link: "trove://page/019a1234-5678-7000-8000-000000000002" }]),
  603 |     ];
  604 |     localStorage.setItem("fixture", JSON.stringify(state));
  605 |     const app = (window as any).go.main.App, previous = app.Dispatch;
  606 |     (window as any).structuralBatches = [];
  607 |     app.Dispatch = async (method: string, raw: string) => {
  608 |       if (method === "batch") (window as any).structuralBatches.push(JSON.parse(raw));
  609 |       return previous(method, raw);
  610 |     };
  611 |     (window as any).emitFixtureEvent("workspace:changed");
  612 |   });
  613 |   await open(page);
  614 |   const editor = page.getByRole("textbox", { name: "Page body" });
  615 |   await expect(editor).toContainText("Imported Notion notebook");
  616 |   for (const [blockID, offset, label] of [[id(13), 1, "First"], [id(12), 11, "Second"]] as const) {
  617 |     // Place the caret within the imported rich link/date text, then exercise
  618 |     // actual browser split and clipboard transactions rather than JSON edits.
  619 |     await editor.locator(`[data-block-id="${blockID}"] .block-text`).evaluate((element, offset) => {
  620 |       const text = document.createTreeWalker(element, NodeFilter.SHOW_TEXT).nextNode()!;
  621 |       const range = document.createRange();
  622 |       range.setStart(text, offset);
  623 |       range.collapse(true);
  624 |       const selection = window.getSelection()!;
  625 |       selection.removeAllRanges();
  626 |       selection.addRange(range);
  627 |       element.closest<HTMLElement>('[contenteditable="true"]')!.focus();
  628 |     }, offset);
  629 |     await page.keyboard.press("Enter");
  630 |     await page.evaluate(async (label) => {
  631 |       await navigator.clipboard.write([new ClipboardItem({
  632 |         "text/html": new Blob([`<p><strong>${label} pasted bold</strong></p><p>తెలుగు 中文 العربية 🌱</p>`], { type: "text/html" }),
  633 |         "text/plain": new Blob([`${label} pasted bold\nతెలుగు 中文 العربية 🌱`], { type: "text/plain" }),
  634 |       })]);
  635 |     }, label);
  636 |     await page.keyboard.press("Meta+v");
  637 |     await expect(editor).toContainText(`${label} pasted bold`);
  638 |     await page.keyboard.press("Meta+s");
  639 |     await expect(page.locator(".save-state")).toHaveText("Saved");
  640 |     await expect(page.getByText("Missing revision guard", { exact: false })).toHaveCount(0);
  641 |   }
  642 |   const result = await page.evaluate(() => ({ blocks: (window as any).fixtureState().blocks, batches: (window as any).structuralBatches }));
  643 |   expect(result.blocks.find((b: any) => b.id === id(10)).data.richText[0].text).toBe("Imported Notion notebook");
  644 |   expect(result.blocks.find((b: any) => b.id === id(11)).data.richText[0].text).toBe("Original introduction");
  645 |   expect(result.blocks.flatMap((b: any) => b.data.richText).filter((s: any) => s.link).map((s: any) => s.text).join("")).toBe("Milestones");
  646 |   expect(result.blocks.flatMap((b: any) => b.data.richText).filter((s: any) => s.marks?.includes("bold")).map((s: any) => s.text)).toEqual(expect.arrayContaining(["First pasted bold", "Second pasted bold"]));
  647 |   expect(new Set(result.blocks.map((b: any) => b.id)).size).toBe(result.blocks.length);
  648 |   for (const batch of result.batches) {
  649 |     const guards = new Map(batch.mutation.expectedRevisions.filter((g: any) => g.target.entityType === "block").map((g: any) => [g.target.entityId, g.revision]));
  650 |     for (const c of batch.commands.filter((c: any) => c.kind === "block.reorder"))
  651 |       for (const blockID of c.orderedBlockIds) expect(guards.has(blockID)).toBe(true);
  652 |   }
  653 |   const headingGuards = result.batches.flatMap((b: any) => b.mutation.expectedRevisions.filter((g: any) => g.target.entityId === id(10)).map((g: any) => Number(g.revision)));
  654 |   expect(headingGuards[0]).toBe(1);
  655 |   expect(headingGuards.at(-1)).toBeGreaterThan(headingGuards[0]);
  656 |   await page.reload();
  657 |   await open(page);
  658 |   await expect(editor).toContainText("First pasted bold");
  659 |   await expect(editor).toContainText("Second pasted bold");
  660 |   await expect(editor).toContainText("తెలుగు 中文 العربية 🌱");
  661 | });
  662 | test("insert real code, Mermaid and KaTeX rendering", async ({ page }) => {
  663 |   await open(page);
  664 |   for (const label of ["Code", "Mermaid diagram", "Equation"]) {
  665 |     await page
  666 |       .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
  667 |       .click();
  668 |     await page.getByRole("button", { name: "＋ Insert", exact: true }).click();
  669 |     await page
  670 |       .getByRole("dialog", { name: "Insert a block" })
  671 |       .getByRole("button", { name: new RegExp("^" + label) })
  672 |       .click();
  673 |   }
  674 |   await page.getByRole("button", { name: "Save page", exact: true }).click();
  675 |   await expect(page.getByRole("status")).toHaveText("Saved");
  676 |   await expect(page.locator(".block-mermaid svg")).toBeVisible();
  677 |   await expect(page.locator(".katex")).toBeVisible();
> 678 |   await expect(page.locator(".code-preview .hljs-comment")).toBeVisible();
      |                                                             ^ Error: expect(locator).toBeVisible() failed
  679 | });
  680 | test("Markdown shortcuts, duplication, keyboard focus and screen-reader structure", async ({
  681 |   page,
  682 | }) => {
  683 |   await open(page);
  684 |   await page
  685 |     .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
  686 |     .click();
  687 |   await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  688 |   await page.keyboard.type("## A useful heading");
  689 |   await expect(
  690 |     page
  691 |       .getByRole("textbox", { name: "Page body" })
  692 |       .getByRole("heading", { name: "A useful heading", level: 2 }),
  693 |   ).toBeVisible();
  694 |   await page.keyboard.press("Meta+Shift+d");
  695 |   await expect(
  696 |     page
  697 |       .getByRole("textbox", { name: "Page body" })
  698 |       .getByRole("heading", { name: "A useful heading", level: 2 }),
  699 |   ).toHaveCount(2);
  700 |   await page.keyboard.press("Escape");
  701 |   await expect(page.getByRole("button", { name: "Save page" })).toBeFocused();
  702 |   const ax = await page.locator("body").ariaSnapshot();
  703 |   expect(ax).toContain('navigation "Page tree"');
  704 |   expect(ax).toContain('toolbar "Text formatting"');
  705 |   expect(ax).toContain('textbox "Page body"');
  706 |   expect(ax).toContain('heading "Imported detail" [level=4]');
  707 |   await page.getByRole("button", { name: "Save page" }).click();
  708 |   await expect(page.getByRole("status")).toHaveText("Saved");
  709 | });
  710 | 
  711 | const navSurface = async (page: any, name: string) =>
  712 |   page
  713 |     .getByRole("navigation", { name: "Workspace", exact: true })
  714 |     .getByRole("button", { name: new RegExp(`\\b${name}\\b`) })
  715 |     .click();
  716 | const save = async (page: any) => {
  717 |   await page.getByRole("button", { name: "Save page", exact: true }).click();
  718 |   await expect(page.locator(".editor-toolbar").getByRole("status")).toHaveText(
  719 |     "Saved",
  720 |   );
  721 | };
  722 | const newPage = async (page: any, title: string) => {
  723 |   await navSurface(page, "Home");
  724 |   await page.getByRole("button", { name: "＋ New page", exact: true }).click();
  725 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue(
  726 |     "Untitled",
  727 |   );
  728 |   await page.getByRole("textbox", { name: "Page title" }).fill(title);
  729 |   await page.getByRole("textbox", { name: "Page title" }).press("Tab");
  730 |   await expect
  731 |     .poll(() =>
  732 |       page.evaluate(
  733 |         (title: string) =>
  734 |           (window as any)
  735 |             .fixtureState()
  736 |             .pages.some((p: any) => p.title === title),
  737 |         title,
  738 |       ),
  739 |     )
  740 |     .toBe(true);
  741 | };
  742 | const addText = async (page: any, text: string) => {
  743 |   await page
  744 |     .getByRole("button", { name: "＋ Add a thought · type / for blocks" })
  745 |     .click();
  746 |   await expect(page.getByRole("textbox", { name: "Page body" })).toBeFocused();
  747 |   await page.keyboard.insertText(text);
  748 | };
  749 | 
  750 | test("page lifecycle preserves identity through nesting, icon, archive and navigation", async ({
  751 |   page,
  752 | }) => {
  753 |   await newPage(page, "Field notes");
  754 |   const pageId = await page.evaluate(
  755 |     () =>
  756 |       (window as any)
  757 |         .fixtureState()
  758 |         .pages.find((p: any) => p.title === "Field notes").id,
  759 |   );
  760 |   page.once("dialog", (d) => d.accept("📚"));
  761 |   await page.getByRole("button", { name: "Set page icon" }).click();
  762 |   await expect(page.getByRole("button", { name: "Set page icon" })).toHaveText(
  763 |     "📚",
  764 |   );
  765 |   await page.getByRole("button", { name: "Page options" }).click();
  766 |   await page
  767 |     .getByRole("combobox", { name: "Move page parent" })
  768 |     .selectOption(id(2));
  769 |   await page.getByRole("button", { name: "Move here", exact: true }).click();
  770 |   await expect
  771 |     .poll(() =>
  772 |       page.evaluate(
  773 |         (pageId: string) =>
  774 |           (window as any).fixtureState().pages.find((p: any) => p.id === pageId)
  775 |             .parentPageId,
  776 |         pageId,
  777 |       ),
  778 |     )
```