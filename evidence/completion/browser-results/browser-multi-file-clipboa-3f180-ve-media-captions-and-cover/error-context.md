# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: browser.spec.ts >> multi-file clipboard and file drop serialize uploads and preserve media captions and cover
- Location: tests/browser.spec.ts:982:1

# Error details

```
Error: expect(locator).toHaveValue(expected) failed

Locator: getByRole('textbox', { name: 'Page body' }).locator('.block-image').getByRole('textbox', { name: 'Attachment caption', exact: true })
Expected: "A visual reference"
Error: Not an input element

Call log:
  - Expect "toHaveValue" getByRole('textbox', { name: 'Page body' }).locator('.block-image').getByRole('textbox', { name: 'Attachment caption', exact: true }) with timeout 5000ms
  - waiting for getByRole('textbox', { name: 'Page body' }).locator('.block-image').getByRole('textbox', { name: 'Attachment caption', exact: true })

```

# Page snapshot

```yaml
- generic [ref=f1e3]:
  - link "Skip to content" [ref=f1e4] [cursor=pointer]:
    - /url: "#main"
  - complementary [ref=f1e5]:
    - generic [ref=f1e6]:
      - generic [ref=f1e7]: ✳
      - strong [ref=f1e8]: Trove
      - generic [ref=f1e9]: LOCAL
    - button "S My workspace Room for what matters ⌄" [ref=f1e10] [cursor=pointer]:
      - generic [ref=f1e11]: S
      - generic [ref=f1e12]:
        - text: My workspace
        - generic [ref=f1e13]: Room for what matters
      - generic [ref=f1e14]: ⌄
    - button "⌕ Quick search ⌘ K" [ref=f1e15] [cursor=pointer]:
      - generic [ref=f1e16]: ⌕ Quick search
      - generic [ref=f1e17]: ⌘ K
    - navigation "Workspace" [ref=f1e18]:
      - button "⌂ Home" [ref=f1e19] [cursor=pointer]:
        - generic [ref=f1e20]: ⌂
        - text: Home
      - button "▣ Inbox" [ref=f1e21] [cursor=pointer]:
        - generic [ref=f1e22]: ▣
        - text: Inbox
      - button "▤ Pages" [ref=f1e23] [cursor=pointer]:
        - generic [ref=f1e24]: ▤
        - text: Pages
      - button "✓ Tasks 0" [ref=f1e25] [cursor=pointer]:
        - generic [ref=f1e26]: ✓
        - text: Tasks
        - generic [ref=f1e27]: "0"
      - button "◉ Meetings" [ref=f1e28] [cursor=pointer]:
        - generic [ref=f1e29]: ◉
        - text: Meetings
      - button "⌕ Search" [ref=f1e30] [cursor=pointer]:
        - generic [ref=f1e31]: ⌕
        - text: Search
      - button "↓ Import" [ref=f1e32] [cursor=pointer]:
        - generic [ref=f1e33]: ↓
        - text: Import
      - button "↻ Jobs" [ref=f1e34] [cursor=pointer]:
        - generic [ref=f1e35]: ↻
        - text: Jobs
      - button "✧ AI" [ref=f1e36] [cursor=pointer]:
        - generic [ref=f1e37]: ✧
        - text: AI
      - button "⇄ Providers" [ref=f1e38] [cursor=pointer]:
        - generic [ref=f1e39]: ⇄
        - text: Providers
      - button "⊕ Capture" [ref=f1e40] [cursor=pointer]:
        - generic [ref=f1e41]: ⊕
        - text: Capture
      - button "◷ Automations" [ref=f1e42] [cursor=pointer]:
        - generic [ref=f1e43]: ◷
        - text: Automations
      - button "◇ Intelligence" [ref=f1e44] [cursor=pointer]:
        - generic [ref=f1e45]: ◇
        - text: Intelligence
      - button "⚙ Settings" [ref=f1e46] [cursor=pointer]:
        - generic [ref=f1e47]: ⚙
        - text: Settings
    - generic [ref=f1e48]: FAVORITES
    - paragraph [ref=f1e50]: Keep your essentials close.
    - generic [ref=f1e51]:
      - generic [ref=f1e52]: YOUR PAGES
      - button "Create page" [ref=f1e53] [cursor=pointer]: ＋
    - navigation "Page tree" [ref=f1e54]:
      - button "🌱 A garden of ideas" [active] [ref=f1e56] [cursor=pointer]:
        - generic [ref=f1e57]: 🌱
        - generic [ref=f1e58]: A garden of ideas
    - generic [ref=f1e59]:
      - text: On your device
      - generic [ref=f1e61]: ↙
  - generic [ref=f1e62]:
    - banner [ref=f1e63]:
      - generic [ref=f1e64]:
        - button "Go back" [ref=f1e65] [cursor=pointer]: ‹
        - button "Go forward" [disabled] [ref=f1e66]: ›
        - generic [ref=f1e67]: My workspace
        - generic [ref=f1e68]: /
        - button "A garden of ideas" [ref=f1e70] [cursor=pointer]
      - generic [ref=f1e71]:
        - button "Favorite page" [ref=f1e72] [cursor=pointer]: ☆
        - button "Page options" [ref=f1e73] [cursor=pointer]: •••
        - generic [ref=f1e74]: S
    - tablist "Open pages" [ref=f1e75]:
      - generic [ref=f1e76]:
        - tab "▤ A garden of ideas" [selected] [ref=f1e77] [cursor=pointer]
        - button "Close A garden of ideas" [ref=f1e78] [cursor=pointer]: ×
    - main [ref=f1e79]:
      - article [ref=f1e80]:
        - img "Page cover" [ref=f1e82]
        - generic [ref=f1e83]:
          - button "Set page icon" [ref=f1e84] [cursor=pointer]: 🌱
          - textbox "Page title" [ref=f1e85]:
            - /placeholder: Untitled
            - text: A garden of ideas
          - generic [ref=f1e86]:
            - text: Your private workspace
            - generic [ref=f1e87]: ·
            - text: Edited Sep 30
            - generic [ref=f1e88]: ·
            - button "＋ Subpage" [ref=f1e89] [cursor=pointer]
            - button "↗ Attach file" [ref=f1e90] [cursor=pointer]
        - generic [ref=f1e91]:
          - button "Lock page" [ref=f1e92] [cursor=pointer]
          - generic [ref=f1e93]: Alt + ↑ / ↓ navigates blocks
        - group [ref=f1e94]:
          - toolbar "Text formatting" [ref=f1e95]:
            - combobox "Block type" [ref=f1e96]:
              - option "Text"
              - option "Heading 1" [selected]
              - option "Heading 2"
              - option "Heading 3"
              - option "Heading 4"
              - option "Bullet list"
              - option "Numbered list"
              - option "Toggle"
              - option "Quote"
              - option "Callout"
            - button "bold" [ref=f1e98] [cursor=pointer]: B
            - button "italic" [ref=f1e99] [cursor=pointer]: I
            - button "underline" [ref=f1e100] [cursor=pointer]: U
            - button "strike" [ref=f1e101] [cursor=pointer]: S
            - generic [ref=f1e102]:
              - generic [ref=f1e103]: Color
              - combobox "Text color" [ref=f1e104]:
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
            - generic [ref=f1e105]:
              - generic [ref=f1e106]: Highlight
              - combobox "Text highlight" [ref=f1e107]:
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
            - button "Add link" [ref=f1e108] [cursor=pointer]: ↗
            - button "Undo" [ref=f1e110] [cursor=pointer]: ↶
            - button "Redo" [ref=f1e111] [cursor=pointer]: ↷
            - button "Find in page" [ref=f1e112] [cursor=pointer]: ⌕
            - button "＋ Insert" [ref=f1e113] [cursor=pointer]
            - status [ref=f1e114]: Saved
            - button "Save page" [ref=f1e115] [cursor=pointer]: Save
          - textbox "Page body" [ref=f1e117]:
            - generic [ref=f1e119]:
              - generic [ref=f1e120]:
                - button "Drag block" [ref=f1e121]: ⠿
                - button "Copy block link" [ref=f1e122] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e123] [cursor=pointer]: ⧉
              - heading "A place to begin" [level=1] [ref=f1e124]
            - generic [ref=f1e127]:
              - generic [ref=f1e128]:
                - button "Drag block" [ref=f1e129]: ⠿
                - button "Copy block link" [ref=f1e130] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e131] [cursor=pointer]: ⧉
              - generic [ref=f1e132]: Capture the small things. Connect the useful ones.
            - generic [ref=f1e135]:
              - generic [ref=f1e136]:
                - button "Drag block" [ref=f1e137]: ⠿
                - button "Copy block link" [ref=f1e138] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e139] [cursor=pointer]: ⧉
              - button "💡" [ref=f1e140] [cursor=pointer]
              - generic [ref=f1e141]: Your work stays on this device.
            - generic [ref=f1e144]:
              - generic [ref=f1e145]:
                - button "Drag block" [ref=f1e146]: ⠿
                - button "Copy block link" [ref=f1e147] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e148] [cursor=pointer]: ⧉
              - heading "Imported detail" [level=4] [ref=f1e149]
            - generic [ref=f1e152]:
              - generic [ref=f1e153]:
                - button "Drag block" [ref=f1e154]: ⠿
                - button "Copy block link" [ref=f1e155] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e156] [cursor=pointer]: ⧉
              - generic [ref=f1e157]:
                - group [ref=f1e158]:
                  - generic "Table structure" [ref=f1e159] [cursor=pointer]
                  - option "1" [selected]
                  - option "1" [selected]
                  - option "2"
                - group [ref=f1e160]:
                  - generic [ref=f1e161]:
                    - text: First cell
                    - combobox "First cell" [ref=f1e162]:
                      - option "Choose" [selected]
                      - option "1:1"
                  - generic [ref=f1e163]:
                    - text: Last cell
                    - combobox "Last cell" [ref=f1e164]:
                      - option "Choose" [selected]
                      - option "1:1"
                  - button "Merge range" [ref=f1e165] [cursor=pointer]
                - table [ref=f1e166]:
                  - rowgroup [ref=f1e170]:
                    - row [ref=f1e171]:
                      - columnheader [ref=f1e172]:
                        - generic [ref=f1e173]:
                          - toolbar "Row 1 column 1 formatting" [ref=f1e174]:
                            - generic [ref=f1e175]:
                              - generic [ref=f1e176]: Color
                              - combobox "Row 1 column 1 color" [ref=f1e177]:
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
                            - generic [ref=f1e178]:
                              - generic [ref=f1e179]: Highlight
                              - combobox "Row 1 column 1 highlight" [ref=f1e180]:
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
                            - button "Row 1 column 1 bold" [ref=f1e181] [cursor=pointer]: bold
                            - button "Row 1 column 1 italic" [ref=f1e182] [cursor=pointer]: italic
                            - button "Row 1 column 1 underline" [ref=f1e183] [cursor=pointer]: underline
                            - button "Row 1 column 1 strike" [ref=f1e184] [cursor=pointer]: strike
                            - button "Row 1 column 1 code" [ref=f1e185] [cursor=pointer]: code
                            - button "Row 1 column 1 link" [ref=f1e186] [cursor=pointer]: Link
                          - textbox "Row 1 column 1" [ref=f1e188]:
                            - paragraph [ref=f1e189]: Merged source cell
                        - button "Restore cells" [ref=f1e190] [cursor=pointer]
            - generic [ref=f1e192]:
              - generic [ref=f1e193]:
                - button "Drag block" [ref=f1e194]: ⠿
                - button "Copy block link" [ref=f1e195] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e196] [cursor=pointer]: ⧉
              - generic [ref=f1e197]:
                - generic [ref=f1e198]: Preserved · source-widget
                - paragraph [ref=f1e199]: A retained source widget
                - group [ref=f1e200]:
                  - generic "View original structure" [ref=f1e201] [cursor=pointer]
            - generic [ref=f1e203]:
              - generic [ref=f1e204]:
                - button "Drag block" [ref=f1e205]: ⠿
                - button "Copy block link" [ref=f1e206] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e207] [cursor=pointer]: ⧉
              - figure [ref=f1e208]:
                - generic [ref=f1e209]:
                  - button "Zoom image.png" [ref=f1e211]:
                    - img "image.png" [ref=f1e212]
                  - generic [ref=f1e213]:
                    - button "Zoom image" [ref=f1e214] [cursor=pointer]
                    - link "Open original image" [ref=f1e215] [cursor=pointer]:
                      - /url: /files/019a1234-5678-7000-8000-000000000102
                    - generic [ref=f1e216]: Original 1 × 1
                  - group [ref=f1e217]:
                    - generic "Image display" [ref=f1e218] [cursor=pointer]
                    - option "Left" [selected]
                    - option "Center"
                    - option "Right"
                - generic [ref=f1e220]:
                  - toolbar "Attachment caption formatting" [ref=f1e221]:
                    - generic [ref=f1e222]:
                      - generic [ref=f1e223]: Color
                      - combobox "Attachment caption color" [ref=f1e224]:
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
                    - generic [ref=f1e225]:
                      - generic [ref=f1e226]: Highlight
                      - combobox "Attachment caption highlight" [ref=f1e227]:
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
                    - button "Attachment caption bold" [ref=f1e228] [cursor=pointer]: bold
                    - button "Attachment caption italic" [ref=f1e229] [cursor=pointer]: italic
                    - button "Attachment caption underline" [ref=f1e230] [cursor=pointer]: underline
                    - button "Attachment caption strike" [ref=f1e231] [cursor=pointer]: strike
                    - button "Attachment caption code" [ref=f1e232] [cursor=pointer]: code
                    - button "Attachment caption link" [ref=f1e233] [cursor=pointer]: Link
                  - textbox "Attachment caption" [ref=f1e235]:
                    - paragraph [ref=f1e236]: A visual reference
                - group [ref=f1e237]:
                  - generic "Image description" [ref=f1e238] [cursor=pointer]
            - generic [ref=f1e240]:
              - generic [ref=f1e241]:
                - button "Drag block" [ref=f1e242]: ⠿
                - button "Copy block link" [ref=f1e243] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e244] [cursor=pointer]: ⧉
              - figure [ref=f1e245]:
                - region "PDF preview notes.pdf" [ref=f1e246]:
                  - generic [ref=f1e247]:
                    - button "Previous PDF page" [disabled] [ref=f1e248]
                    - generic [ref=f1e249]:
                      - text: Page
                      - spinbutton "PDF page" [ref=f1e250]: "1"
                    - generic [ref=f1e251]: of …
                    - button "Next PDF page" [disabled] [ref=f1e252]
                    - generic [ref=f1e253]:
                      - text: Zoom
                      - combobox "PDF zoom" [ref=f1e254]:
                        - option "50%"
                        - option "75%"
                        - option "100%" [selected]
                        - option "125%"
                        - option "150%"
                        - option "200%"
                    - link "Open original PDF" [ref=f1e255] [cursor=pointer]:
                      - /url: /files/019a1234-5678-7000-8000-000000000107
                  - status [ref=f1e256]: Rendering PDF page…
                  - img "notes.pdf, page 1" [ref=f1e258]
                - generic [ref=f1e260]:
                  - toolbar "Attachment caption formatting" [ref=f1e261]:
                    - generic [ref=f1e262]:
                      - generic [ref=f1e263]: Color
                      - combobox "Attachment caption color" [ref=f1e264]:
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
                    - generic [ref=f1e265]:
                      - generic [ref=f1e266]: Highlight
                      - combobox "Attachment caption highlight" [ref=f1e267]:
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
                    - button "Attachment caption bold" [ref=f1e268] [cursor=pointer]: bold
                    - button "Attachment caption italic" [ref=f1e269] [cursor=pointer]: italic
                    - button "Attachment caption underline" [ref=f1e270] [cursor=pointer]: underline
                    - button "Attachment caption strike" [ref=f1e271] [cursor=pointer]: strike
                    - button "Attachment caption code" [ref=f1e272] [cursor=pointer]: code
                    - button "Attachment caption link" [ref=f1e273] [cursor=pointer]: Link
                  - textbox "Attachment caption" [ref=f1e275]:
                    - paragraph [ref=f1e276]
            - generic [ref=f1e278]:
              - generic [ref=f1e279]:
                - button "Drag block" [ref=f1e280]: ⠿
                - button "Copy block link" [ref=f1e281] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e282] [cursor=pointer]: ⧉
              - figure [ref=f1e283]:
                - generic [ref=f1e286]:
                  - toolbar "Attachment caption formatting" [ref=f1e287]:
                    - generic [ref=f1e288]:
                      - generic [ref=f1e289]: Color
                      - combobox "Attachment caption color" [ref=f1e290]:
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
                    - generic [ref=f1e291]:
                      - generic [ref=f1e292]: Highlight
                      - combobox "Attachment caption highlight" [ref=f1e293]:
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
                    - button "Attachment caption bold" [ref=f1e294] [cursor=pointer]: bold
                    - button "Attachment caption italic" [ref=f1e295] [cursor=pointer]: italic
                    - button "Attachment caption underline" [ref=f1e296] [cursor=pointer]: underline
                    - button "Attachment caption strike" [ref=f1e297] [cursor=pointer]: strike
                    - button "Attachment caption code" [ref=f1e298] [cursor=pointer]: code
                    - button "Attachment caption link" [ref=f1e299] [cursor=pointer]: Link
                  - textbox "Attachment caption" [ref=f1e301]:
                    - paragraph [ref=f1e302]
            - generic [ref=f1e304]:
              - generic [ref=f1e305]:
                - button "Drag block" [ref=f1e306]: ⠿
                - button "Copy block link" [ref=f1e307] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e308] [cursor=pointer]: ⧉
              - figure [ref=f1e309]:
                - generic [ref=f1e312]:
                  - toolbar "Attachment caption formatting" [ref=f1e313]:
                    - generic [ref=f1e314]:
                      - generic [ref=f1e315]: Color
                      - combobox "Attachment caption color" [ref=f1e316]:
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
                    - generic [ref=f1e317]:
                      - generic [ref=f1e318]: Highlight
                      - combobox "Attachment caption highlight" [ref=f1e319]:
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
                    - button "Attachment caption bold" [ref=f1e320] [cursor=pointer]: bold
                    - button "Attachment caption italic" [ref=f1e321] [cursor=pointer]: italic
                    - button "Attachment caption underline" [ref=f1e322] [cursor=pointer]: underline
                    - button "Attachment caption strike" [ref=f1e323] [cursor=pointer]: strike
                    - button "Attachment caption code" [ref=f1e324] [cursor=pointer]: code
                    - button "Attachment caption link" [ref=f1e325] [cursor=pointer]: Link
                  - textbox "Attachment caption" [ref=f1e327]:
                    - paragraph [ref=f1e328]
            - generic [ref=f1e330]:
              - generic [ref=f1e331]:
                - button "Drag block" [ref=f1e332]: ⠿
                - button "Copy block link" [ref=f1e333] [cursor=pointer]: ↗
                - button "Duplicate block" [ref=f1e334] [cursor=pointer]: ⧉
              - figure [ref=f1e335]:
                - link "↗ notes.txt 14 bytes · immutable file" [ref=f1e336] [cursor=pointer]:
                  - /url: /files/019a1234-5678-7000-8000-000000000122
                  - text: ↗ notes.txt
                  - generic [ref=f1e337]: 14 bytes · immutable file
                - generic [ref=f1e339]:
                  - toolbar "Attachment caption formatting" [ref=f1e340]:
                    - generic [ref=f1e341]:
                      - generic [ref=f1e342]: Color
                      - combobox "Attachment caption color" [ref=f1e343]:
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
                    - generic [ref=f1e344]:
                      - generic [ref=f1e345]: Highlight
                      - combobox "Attachment caption highlight" [ref=f1e346]:
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
                    - button "Attachment caption bold" [ref=f1e347] [cursor=pointer]: bold
                    - button "Attachment caption italic" [ref=f1e348] [cursor=pointer]: italic
                    - button "Attachment caption underline" [ref=f1e349] [cursor=pointer]: underline
                    - button "Attachment caption strike" [ref=f1e350] [cursor=pointer]: strike
                    - button "Attachment caption code" [ref=f1e351] [cursor=pointer]: code
                    - button "Attachment caption link" [ref=f1e352] [cursor=pointer]: Link
                  - textbox "Attachment caption" [ref=f1e354]:
                    - paragraph [ref=f1e355]
          - button "＋ Add a thought · type / for blocks" [ref=f1e356] [cursor=pointer]
        - group [ref=f1e357]:
          - generic "Templates and related pages" [ref=f1e358]
          - option "New page" [selected]
        - group [ref=f1e359]:
          - generic "✧ Ask AI or work with this page" [ref=f1e360]
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
          - option "image"
          - option "pdf"
          - option "audio"
          - option "video"
          - option "file"
          - option "Preference" [selected]
          - option "Project context"
          - option "Decision with evidence"
          - option "Workspace" [selected]
          - option "Current page"
          - option "Project"
        - generic [ref=f1e361]:
          - generic [ref=f1e362]: LINKED TO THIS PAGE
          - paragraph [ref=f1e363]: Backlinks will appear here when another page links to this one.
    - contentinfo [ref=f1e364]:
      - generic [ref=f1e365]: Made for your own way of thinking.
      - generic [ref=f1e366]: Private by default · Saved on this device
```

# Test source

```ts
  975  |     rowSpan: 1,
  976  |     colSpan: 2,
  977  |     richText: [{ text: "Updated merged cell" }],
  978  |     preMergeCells: [{ value: "Original A" }],
  979  |   });
  980  | });
  981  | 
  982  | test("multi-file clipboard and file drop serialize uploads and preserve media captions and cover", async ({
  983  |   page,
  984  | }) => {
  985  |   const pixel =
  986  |     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5L8AAAAASUVORK5CYII=";
  987  |   await page.route("**/files/**", (route) =>
  988  |     route.fulfill({
  989  |       status: 200,
  990  |       contentType: "image/png",
  991  |       body: Buffer.from(pixel, "base64"),
  992  |     }),
  993  |   );
  994  |   await open(page);
  995  |   await page
  996  |     .getByRole("textbox", { name: "Page body" })
  997  |     .evaluate((editor: any) => {
  998  |       const data = new DataTransfer();
  999  |       for (const [name, type] of [
  1000 |         ["image.png", "image/png"],
  1001 |         ["notes.pdf", "application/pdf"],
  1002 |         ["recording.wav", "audio/wav"],
  1003 |         ["clip.mp4", "video/mp4"],
  1004 |       ])
  1005 |         data.items.add(new File([`Fixture bytes for ${name}`], name, { type }));
  1006 |       editor.dispatchEvent(
  1007 |         new ClipboardEvent("paste", {
  1008 |           clipboardData: data,
  1009 |           bubbles: true,
  1010 |           cancelable: true,
  1011 |         }),
  1012 |       );
  1013 |     });
  1014 |   await expect
  1015 |     .poll(() =>
  1016 |       page.evaluate(() => (window as any).fixtureState().files.length),
  1017 |     )
  1018 |     .toBe(4);
  1019 |   await page.locator("main").evaluate((main: any) => {
  1020 |     const data = new DataTransfer();
  1021 |     data.items.add(
  1022 |       new File(["Retained notes"], "notes.txt", { type: "text/plain" }),
  1023 |     );
  1024 |     main.dispatchEvent(
  1025 |       new DragEvent("drop", {
  1026 |         dataTransfer: data,
  1027 |         bubbles: true,
  1028 |         cancelable: true,
  1029 |       }),
  1030 |     );
  1031 |   });
  1032 |   await expect
  1033 |     .poll(() =>
  1034 |       page.evaluate(() => (window as any).fixtureState().files.length),
  1035 |     )
  1036 |     .toBe(5);
  1037 |   const metrics = await page.evaluate(() => (window as any).attachmentMetrics);
  1038 |   expect(metrics.maxConcurrentUploads).toBe(1);
  1039 |   expect(metrics.stagedNames).toEqual([
  1040 |     "image.png",
  1041 |     "notes.pdf",
  1042 |     "recording.wav",
  1043 |     "clip.mp4",
  1044 |     "notes.txt",
  1045 |   ]);
  1046 |   const editor = page.getByRole("textbox", { name: "Page body" });
  1047 |   for (const type of ["image", "pdf", "audio", "video", "file"])
  1048 |     await expect(editor.locator(`.block-${type}`)).toBeVisible();
  1049 |   await editor
  1050 |     .locator(".block-image")
  1051 |     .getByRole("textbox", { name: "Attachment caption", exact: true })
  1052 |     .fill("A visual reference");
  1053 |   await save(page);
  1054 |   const imageID = await page.evaluate(
  1055 |     () =>
  1056 |       (window as any)
  1057 |         .fixtureState()
  1058 |         .files.find((f: any) => f.mediaType === "image/png").id,
  1059 |   );
  1060 |   await page.getByRole("button", { name: "Page options" }).click();
  1061 |   await page
  1062 |     .getByRole("combobox", { name: "Cover image" })
  1063 |     .selectOption(imageID);
  1064 |   await expect(page.locator(".page-cover img")).toHaveAttribute(
  1065 |     "src",
  1066 |     `/files/${imageID}`,
  1067 |   );
  1068 |   await page.keyboard.press("Escape");
  1069 |   await page.reload();
  1070 |   await open(page);
  1071 |   await expect(
  1072 |     editor
  1073 |       .locator(".block-image")
  1074 |       .getByRole("textbox", { name: "Attachment caption", exact: true }),
> 1075 |   ).toHaveValue("A visual reference");
       |     ^ Error: expect(locator).toHaveValue(expected) failed
  1076 |   await expect(editor.locator("audio")).toHaveAttribute("controls", "");
  1077 |   await expect(editor.locator("video")).toHaveAttribute("controls", "");
  1078 |   await expect(editor.locator("object")).toHaveAttribute(
  1079 |     "type",
  1080 |     "application/pdf",
  1081 |   );
  1082 |   await expect(editor.locator(".file-card")).toContainText("notes.txt");
  1083 |   await expect(page.locator(".page-cover img")).toHaveAttribute(
  1084 |     "src",
  1085 |     `/files/${imageID}`,
  1086 |   );
  1087 | });
  1088 | 
  1089 | test("Phase 2 search opens the exact cited block without model packs", async ({ page }) => {
  1090 |   await navSurface(page, "Search");
  1091 |   await expect(page.getByText("Keyword search works entirely offline without a model.", { exact: false })).toBeVisible();
  1092 |   await page.getByRole("searchbox", { name: "Search text" }).fill("capture");
  1093 |   await page.locator(".search-result").click();
  1094 |   await expect(page.getByRole("textbox", { name: "Page title" })).toHaveValue("A garden of ideas");
  1095 |   await expect(page.locator(`[data-block-id="${id(11)}"]`)).toHaveClass(/source-highlight/);
  1096 |   await expect(page.locator(".app-footer")).toContainText("Opened cited source");
  1097 | });
  1098 | 
  1099 | test("copied page and block links route through editor navigation", async ({ page }) => {
  1100 |   await page.evaluate(() => {
  1101 |     const state = (window as any).fixtureState();
  1102 |     const target = "019a1234-5678-7000-8000-000000000802";
  1103 |     const block = "019a1234-5678-7000-8000-000000000803";
  1104 |     state.pages.push({...state.pages[0], id: target, title: "Linked destination"});
  1105 |     state.blocks.push({...state.blocks[1], id: block, pageId: target, data: {richText: [{text: "Exact target paragraph"}]}});
  1106 |     state.blocks[1].data.richText = [{text: "Open target block", link: `trove://page/${target}/block/${block}?workspace=${state.workspaceId}`}];
  1107 |     localStorage.setItem("fixture", JSON.stringify(state));
  1108 |   });
  1109 |   await page.reload();
  1110 |   await open(page);
  1111 |   await page.getByRole("link", {name: "Open target block"}).click();
  1112 |   await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("Linked destination");
  1113 |   await expect(page.locator(`[data-block-id="${id(803)}"]`)).toHaveClass(/source-highlight/);
  1114 |   await expect(page.locator(".app-footer")).toContainText("Opened linked block");
  1115 | });
  1116 | 
  1117 | test("native URL events consume a pending block link once", async ({ page }) => {
  1118 |   await navSurface(page, "Search");
  1119 |   await page.evaluate(() => {
  1120 |     let pending = "trove://page/019a1234-5678-7000-8000-000000000002/block/019a1234-5678-7000-8000-000000000011";
  1121 |     (window as any).go.main.App.PendingDeepLink = async () => {const value = pending; pending = ""; return value;};
  1122 |     (window as any).emitFixtureEvent("native:openURL");
  1123 |   });
  1124 |   await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("A garden of ideas");
  1125 |   await expect(page.locator(`[data-block-id="${id(11)}"]`)).toHaveClass(/source-highlight/);
  1126 |   await navSurface(page, "Search");
  1127 |   await page.evaluate(() => (window as any).emitFixtureEvent("native:openURL"));
  1128 |   await expect(page.getByRole("searchbox", {name: "Search text"})).toBeVisible();
  1129 | });
  1130 | 
  1131 | test("links report unavailable workspaces and missing blocks without leaving the current page", async ({ page }) => {
  1132 |   await open(page);
  1133 |   for (const [href, message] of [
  1134 |     [`trove://page/${id(2)}?workspace=${id(999)}`, "another workspace"],
  1135 |     [`trove://page/${id(2)}/block/${id(998)}`, "block is unavailable"],
  1136 |     [`trove://page/${id(997)}`, "page is unavailable"],
  1137 |   ]) {
  1138 |     await page.evaluate((href) => {
  1139 |       const app = (window as any).go.main.App;
  1140 |       let pending = href;
  1141 |       app.PendingDeepLink = async () => {const value = pending; pending = ""; return value;};
  1142 |       (window as any).emitFixtureEvent("native:openURL");
  1143 |     }, href);
  1144 |     await expect(page.getByRole("alert")).toContainText(message);
  1145 |     await expect(page.getByRole("textbox", {name: "Page title"})).toHaveValue("A garden of ideas");
  1146 |   }
  1147 | });
  1148 | 
  1149 | test("Phase 2 import displays complete accounting and editable property preview", async ({ page }) => {
  1150 |   await page.evaluate(() => {
  1151 |     const app = (window as any).go.main.App;
  1152 |     const previous = app.Dispatch;
  1153 |     app.PickFile = async () => "fixture-selection";
  1154 |     const plan = {
  1155 |       planId: "fixture-plan", planHash: "h", sourceNamespace: "fixture", sourceName: "Notion.zip",
  1156 |       rootPageId: "root", sourceFileId: "source", state: "preview", cursor: 0, initialized: false,
  1157 |       sourceCompleteness: "unknown", sourceOmissions: ["Original formula definitions omitted by source CSV"],
  1158 |       unknownCompleteness: ["The original workspace item count is unknown"],
  1159 |       items: [
  1160 |         { sourceLocator: "Notes.md", kind: "markdown", outcome: "preserved", reason: "Text retained", committed: false },
  1161 |         { sourceLocator: "Rows.csv", kind: "csv", outcome: "adapted", reason: "Database values parsed", committed: false,
  1162 |           columns: [{ name: "Name", type: "title", propertyId: "a", samples: ["Plan"] }, { name: "Estimate", type: "number", propertyId: "b", samples: ["14"] }] },
  1163 |         { sourceLocator: "Drawing.md", kind: "markdown", outcome: "partial", reason: "Readable representation retained", committed: false },
  1164 |         { sourceLocator: "Custom.bin", kind: "file", outcome: "unsupported_but_retained", reason: "Original bytes retained", retainedFileId: "raw", committed: false },
  1165 |         { sourceLocator: "../unsafe.md", kind: "file", outcome: "failed", reason: "Unsafe archive path", committed: false },
  1166 |       ],
  1167 |     };
  1168 |     (window as any).phase2Mutations = 0;
  1169 |     app.Dispatch = async (method: string, raw: string) => {
  1170 |       if (method === "imports.preview") return JSON.stringify({ ok: true, result: plan });
  1171 |       if (method === "imports.configure") {
  1172 |         plan.items[1].columns![1].type = JSON.parse(raw).columns[0].type;
  1173 |         return JSON.stringify({ ok: true, result: plan });
  1174 |       }
  1175 |       if (method === "imports.commit") (window as any).phase2Mutations++;
```