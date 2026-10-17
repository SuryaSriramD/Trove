import test from "node:test";
import assert from "node:assert/strict";
import {
  toDocument,
  fromDocument,
  assignIDs,
  mutations,
  blockMutationGuards,
  richNodes,
  richText,
} from "../src/editor/codec.ts";
const block = (
  id: string,
  type: string,
  data: any,
  parentBlockId: string | null = null,
  sortKey = "000000001000",
): any => ({
  id,
  type,
  data,
  parentBlockId,
  sortKey,
  schemaVersion: 1,
  pageId: "page",
  revision: "1",
});
test("lossless mixed canonical document including unsupported imported structures", () => {
  const fixture = [
    block("heading", "heading_4", {
      richText: [
        {
          text: "తెలుగు 日本語 العربية 👩🏽‍💻",
          marks: ["bold"],
          link: "https://example.com",
        },
      ],
    }),
    block(
      "toggle",
      "toggle",
      { richText: [{ text: "Details" }] },
      null,
      "000000002000",
    ),
    block(
      "nested",
      "paragraph",
      { richText: [{ text: "Nested\nsecond line" }] },
      "toggle",
    ),
    block(
      "table",
      "table",
      {
        rowCount: 2,
        columnCount: 2,
        headerRows: 1,
        headerColumns: 0,
        columnWidths: [100, 100],
        cells: [
          {
            id: "cell",
            row: 0,
            column: 0,
            rowSpan: 2,
            colSpan: 2,
            richText: [{ text: "Merged", marks: ["italic"] }],
            preMergeCells: [{ value: "Original" }],
          },
        ],
      },
      null,
      "000000003000",
    ),
    block(
      "tabs",
      "tab_container",
      { tabs: [{ id: "tab", title: "First", childBlockIds: ["child"] }] },
      null,
      "000000004000",
    ),
    block("child", "paragraph", { richText: [{ text: "Child" }] }, "tabs"),
    block(
      "raw",
      "unsupported_external",
      {
        sourceType: "future-widget",
        sourceVersion: 7,
        raw: { colors: ["red"], executable: "do not run" },
        readableText: "Preserved note",
        reason: "unknown_type",
      },
      null,
      "000000005000",
    ),
  ];
  const output = fromDocument(toDocument(fixture));
  for (const original of fixture) {
    const result = output.find((x) => x.id === original.id)!;
    assert.equal(result.type, original.type);
    if (original.id === "nested") {
      assert.equal(
        result.data.richText.map((s: any) => s.text).join(""),
        "Nested\nsecond line",
      );
    } else assert.deepEqual(result.data, original.data);
    assert.equal(result.parentBlockId, original.parentBlockId);
  }
  assert.equal(
    mutations("page", fixture, output).filter((c) => c.kind !== "block.update")
      .length,
    0,
  );
});
test("split and clipboard duplicates get fresh allocated identities; task references stay canonical", () => {
  const todo = block("a", "todo", { taskRecordId: "task" });
  const drafts = fromDocument({
    type: "doc",
    content: [...toDocument([todo]).content!, ...toDocument([todo]).content!],
  });
  const output = assignIDs(drafts, ["allocated"]);
  assert.deepEqual(
    output.map((b) => b.id),
    ["a", "allocated"],
  );
  assert.deepEqual(
    output.map((b) => b.data.taskRecordId),
    ["task", "task"],
  );
  assert.throws(() => assignIDs(drafts, []));
});
test("indent keeps block identity and emits canonical block.move", () => {
  const fixture = [
    block("a", "paragraph", { richText: [] }),
    block("b", "paragraph", { richText: [] }, null, "000000002000"),
  ];
  const draft = fromDocument(toDocument(fixture));
  draft[1].depth = 1;
  assert.ok(
    mutations("page", fixture, draft).some(
      (c) =>
        c.kind === "block.move" && c.blockId === "b" && c.parentBlockId === "a",
    ),
  );
});
test("marks, links, Unicode and explicit line breaks survive rich text projection", () => {
  const value: any = [
    { text: "नमस्ते", marks: ["bold", "underline"] },
    { text: "\nمرحبا 👨‍👩‍👧‍👦", link: "https://example.com" },
  ];
  const output = richText(richNodes(value));
  assert.equal(
    output.map((s) => s.text).join(""),
    value.map((s: any) => s.text).join(""),
  );
  assert.deepEqual(output[0].marks, ["bold", "underline"]);
  assert.equal(output.at(-1)?.link, "https://example.com");
});
test("deleting a nested tree emits child-first deletes", () => {
  const fixture = [
    block("a", "toggle", { richText: [] }),
    block("b", "paragraph", { richText: [] }, "a"),
  ];
  assert.deepEqual(
    mutations("page", fixture, []).map((c) =>
      "blockId" in c ? c.blockId : null,
    ),
    ["b", "a"],
  );
});

test("split and formatted paste guard unchanged reordered siblings and new blocks", () => {
  const fixture = [
    block("heading", "heading_1", { richText: [{ text: "Notion import" }] }),
    block(
      "date",
      "paragraph",
      { richText: [{ text: "Launch 2026-09-30" }] },
      null,
      "000000002000",
    ),
    block(
      "link",
      "paragraph",
      { richText: [{ text: "Milestones", link: "trove://page/target" }] },
      null,
      "000000003000",
    ),
  ];
  fixture[0].revision = "7";
  fixture[1].revision = "3";
  const doc = toDocument(fixture);
  doc.content![2].content![0].text = "M";
  doc.content!.push(
    {
      type: "troveText",
      attrs: { kind: "paragraph" },
      content: [
        { type: "text", text: "Pasted bold", marks: [{ type: "bold" }] },
      ],
    },
    {
      type: "troveText",
      attrs: { kind: "paragraph" },
      content: [{ type: "text", text: "తెలుగు 中文 العربية 🌱" }],
    },
    {
      ...structuredClone(doc.content![2]),
      content: [
        {
          type: "text",
          text: "ilestones",
          marks: [{ type: "link", attrs: { href: "trove://page/target" } }],
        },
      ],
    },
  );
  const draft = assignIDs(fromDocument(doc), [
    "paste-bold",
    "paste-unicode",
    "link-tail",
  ]);
  const changes = mutations("page", fixture, draft);
  const guards = blockMutationGuards(fixture, changes);
  assert.deepEqual(
    Object.fromEntries(guards.map((g) => [g.target.entityId, g.revision])),
    {
      link: "1",
      "paste-bold": "0",
      "paste-unicode": "0",
      "link-tail": "0",
      heading: "7",
      date: "3",
    },
  );
  assert.equal(
    guards.length,
    new Set(guards.map((g) => g.target.entityId)).size,
  );
  assert.ok(
    changes.some(
      (c) =>
        c.kind === "block.reorder" && c.orderedBlockIds.includes("heading"),
    ),
  );
  assert.deepEqual(draft.find((b) => b.id === "paste-bold")!.data.richText, [
    { text: "Pasted bold", marks: ["bold"] },
  ]);
});

test("structural guards include an unchanged parent, moved descendants and destination siblings", () => {
  const fixture = [
    block("parent", "toggle", { richText: [] }),
    block("existing-child", "paragraph", { richText: [] }, "parent"),
    block("moving", "toggle", { richText: [] }, null, "000000002000"),
    block("descendant", "paragraph", { richText: [] }, "moving"),
  ];
  const moved = blockMutationGuards(fixture, [
    {
      kind: "block.move",
      blockId: "moving",
      pageId: "page",
      parentBlockId: "parent",
    },
  ]);
  assert.deepEqual(
    new Set(moved.map((g) => g.target.entityId)),
    new Set(["moving", "descendant", "parent", "existing-child"]),
  );
  const inserted = blockMutationGuards(fixture, [
    {
      kind: "block.insert",
      block: {
        blockId: "new-child",
        pageId: "page",
        parentBlockId: "parent",
        type: "paragraph",
        schemaVersion: 1,
        data: { richText: [] },
      },
    },
  ]);
  assert.deepEqual(
    Object.fromEntries(inserted.map((g) => [g.target.entityId, g.revision])),
    { "new-child": "0", parent: "1" },
  );
  assert.deepEqual(
    blockMutationGuards(fixture, [
      {
        kind: "block.update",
        blockId: "existing-child",
        type: "paragraph",
        schemaVersion: 1,
        data: { richText: [{ text: "Text-only change" }] },
      },
    ]).map((g) => g.target.entityId),
    ["existing-child"],
  );
});

// The service serializes object keys in canonical order; editor projections may
// construct equal maps in a different insertion order. That is not an edit.
test("service-ordered rich text roundtrip emits no canonical update", () => {
  const data = JSON.parse('{"richText":[{"color":"purple","highlight":"yellow","link":"https://example.test/retained","marks":["bold"],"text":"Willow! field notes"}]}');
  const before = [block("rich", "paragraph", data)];
  const draft = fromDocument(toDocument(before));
  assert.deepEqual(draft[0].data, data);
  assert.notEqual(JSON.stringify(draft[0].data), JSON.stringify(data));
  assert.deepEqual(mutations("page", before, draft), []);
});

test("removing an empty layout group updates only the container, preserving rich children", () => {
  const rich = JSON.parse('{"richText":[{"color":"purple","highlight":"yellow","link":"https://example.test/layout-source","marks":["bold"],"text":"Willow! field notes"}]}');
  const kept = { childBlockIds: ["plain", "rich"], id: "kept", widthRatio: 1 };
  const before = [
    block("layout", "columns", { columns: [
      { childBlockIds: [], id: "empty", widthRatio: 2 }, kept,
    ] }),
    block("plain", "paragraph", { richText: [{ text: "Keep 37 samples" }] }, "layout", "000000000001"),
    block("rich", "paragraph", rich, "layout", "000000000002"),
  ];
  before[2].revision = "6";
  const draft = fromDocument(toDocument(before));
  draft.find(b => b.id === "layout")!.data = { columns: [structuredClone(kept)] };
  const commands = mutations("page", before, draft);
  assert.deepEqual(commands, [{ kind: "block.update", blockId: "layout", type: "columns", schemaVersion: 1, data: { columns: [kept] } }]);
  assert.deepEqual(blockMutationGuards(before, commands).map(g => ({ id: g.target.entityId, revision: g.revision })), [{ id: "layout", revision: "1" }]);
  assert.equal(before[2].revision, "6");
  assert.deepEqual(draft.find(b => b.id === "rich")!.data, rich);
});

test("nested map member order is ignored without sorting arrays or changing data", () => {
  const before = [block("raw", "unsupported_external", {
    sourceType: "future-widget", sourceVersion: 1, readableText: "Retained evidence", reason: "unknown_type",
    raw: { first: { count: 3, nullable: null }, second: [{ code: "A", enabled: true }, { code: "B", enabled: false }] },
  })];
  const draft = fromDocument(toDocument(before));
  draft[0].data = {
    raw: { second: [{ enabled: true, code: "A" }, { enabled: false, code: "B" }], first: { nullable: null, count: 3 } },
    reason: "unknown_type", readableText: "Retained evidence", sourceVersion: 1, sourceType: "future-widget",
  };
  const retained = structuredClone(draft[0].data);
  assert.deepEqual(mutations("page", before, draft), []);
  assert.deepEqual(draft[0].data, retained);
});

test("nested values, array order, types and null versus missing remain edits", async t => {
  const original = { nullable: null, number: 1, enabled: true, nested: { color: "purple" }, order: ["a", "b"], empty: {} };
  const variants = [
    ["nested value", { ...original, nested: { color: "blue" } }],
    ["array order", { ...original, order: ["b", "a"] }],
    ["array length", { ...original, order: ["a"] }],
    ["primitive type", { ...original, number: "1" }],
    ["boolean", { ...original, enabled: false }],
    ["missing member", { number: 1, enabled: true, nested: { color: "purple" }, order: ["a", "b"], empty: {} }],
    ["null versus map", { ...original, empty: null }],
    ["map versus array", { ...original, empty: [] }],
  ] as const;
  for (const [name, raw] of variants) await t.test(name, () => {
    const before = [block("raw", "unsupported_external", { sourceType: "future-widget", sourceVersion: 1, raw: original, readableText: "Retained", reason: "unknown_type" })];
    const draft = fromDocument(toDocument(before));
    draft[0].data.raw = structuredClone(raw);
    assert.deepEqual(mutations("page", before, draft), [{ kind: "block.update", blockId: "raw", type: "unsupported_external", schemaVersion: 1, data: draft[0].data }]);
  });
});
