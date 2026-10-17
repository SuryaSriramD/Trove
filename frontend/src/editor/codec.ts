import type { Block, RichText } from "../../../contracts/generated/domain";
import type {
  Command,
  ExpectedRevision,
} from "../../../contracts/generated/service";
import { colorToken } from "./palette.ts";
export type NodeJSON = {
  type: string;
  attrs?: { [key: string]: any };
  content?: NodeJSON[];
  text?: string;
  marks?: { type: string; attrs?: { [key: string]: any } }[];
};
export const textKinds = [
  "paragraph",
  "heading_1",
  "heading_2",
  "heading_3",
  "heading_4",
  "bulleted_list_item",
  "numbered_list_item",
  "quote",
  "toggle",
  "toggle_heading",
  "callout",
];
export const plain = (rt: RichText = []) => rt.map((x) => x.text).join("");
export function richNodes(rt: RichText): NodeJSON[] {
  return rt
    .filter((x) => x.text)
    .flatMap((x) =>
      x.text.split("\n").flatMap((text, index) => [
        ...(index ? [{ type: "hardBreak", marks: [
          ...(x.marks ?? []).map(type=>({type})),...(x.link?[{type:"link",attrs:{href:x.link}}]:[]),
          ...(x.color?[{type:"textColor",attrs:{token:x.color}}]:[]),...(x.highlight?[{type:"textHighlight",attrs:{token:x.highlight}}]:[]),
        ] }] : []),
        ...(text
          ? [
              {
                type: "text",
                text,
                marks: [
                  ...(x.marks ?? []).map((type) => ({ type })),
                  ...(x.color ? [{type:"textColor",attrs:{token:x.color}}] : []),
                  ...(x.highlight ? [{type:"textHighlight",attrs:{token:x.highlight}}] : []),
                  ...(x.link
                    ? [{ type: "link", attrs: { href: x.link } }]
                    : []),
                ],
              },
            ]
          : []),
      ]),
    );
}
export function richText(nodes: NodeJSON[] = []): RichText {
  const out: RichText = [];
  for (const n of nodes) {
    const text = n.type === "hardBreak" ? "\n" : n.text;
    if (!text) continue;
    const marks = (n.marks ?? [])
      .filter((m) =>
        ["bold", "italic", "underline", "strike", "code"].includes(m.type),
      )
      .map((m) => m.type) as NonNullable<RichText[number]["marks"]>;
    const link = n.marks?.find((m) => m.type === "link")?.attrs?.href;
    const color=colorToken(n.marks?.find(m=>m.type==="textColor")?.attrs?.token),highlight=colorToken(n.marks?.find(m=>m.type==="textHighlight")?.attrs?.token);
    out.push({
      text,
      ...(color?{color}:{}),...(highlight?{highlight}:{}),
      ...(marks.length ? { marks } : {}),
      ...(link ? { link } : {}),
    });
  }
  return out;
}
export function orderedBlocks(blocks: Block[]): Block[] {
  const out: Block[] = [];
  const seen = new Set<string>();
  const walk = (parent: string | null) => {
    for (const b of blocks
      .filter((b) => b.parentBlockId === parent)
      .sort((a, b) => a.sortKey.localeCompare(b.sortKey))) {
      if (seen.has(b.id)) continue;
      seen.add(b.id);
      out.push(b);
      walk(b.id);
    }
  };
  walk(null);
  for (const b of blocks) if (!seen.has(b.id)) out.push(b);
  return out;
}
export function toDocument(blocks: Block[]): NodeJSON {
  const map = new Map(blocks.map((b) => [b.id, b]));
  return {
    type: "doc",
    content: orderedBlocks(blocks).map((b) => {
      let depth = 0,
        p = b.parentBlockId;
      const seen = new Set<string>();
      while (p && map.has(p) && !seen.has(p)) {
        seen.add(p);
        depth++;
        p = map.get(p)!.parentBlockId;
      }
      const text = textKinds.includes(b.type) && b.schemaVersion === 1;
      return {
        type: text ? "troveText" : "troveAtom",
        attrs: {
          blockId: b.id,
          kind: b.type,
          parentBlockId: b.parentBlockId,
          depth,
          version: b.schemaVersion,
          data: b.data,
        },
        ...(text
          ? {
              content: richNodes(
                (b.data as { richText: RichText }).richText ?? [],
              ),
            }
          : {}),
      };
    }),
  };
}
export type DraftBlock = {
  id: string | null;
  parentBlockId: string | null;
  type: Block["type"];
  schemaVersion: 1;
  data: any;
  depth: number;
};
export function fromDocument(doc: NodeJSON): DraftBlock[] {
  const out: DraftBlock[] = [];
  for (const n of doc.content ?? []) {
    const a = n.attrs ?? {};
    let data = structuredClone(a.data ?? {});
    if (n.type === "troveText") data.richText = richText(n.content);
    out.push({
      id: a.blockId ?? null,
      parentBlockId: a.parentBlockId ?? null,
      type: a.kind ?? "paragraph",
      schemaVersion: a.version ?? 1,
      data,
      depth: a.depth ?? 0,
    });
  }
  return out;
}
// Duplicate identities created by split/paste are replaced using a kernel-allocated pool.
export function assignIDs(
  drafts: DraftBlock[],
  allocated: string[],
): DraftBlock[] {
  const seen = new Set<string>();
  let next = 0;
  return drafts.map((b) => {
    const id = b.id && !seen.has(b.id) ? b.id : allocated[next++];
    if (!id) throw new Error("Not enough allocated block IDs");
    seen.add(id);
    return { ...b, id };
  });
}
// JSON maps carry no member-order semantics; array order, types and missing
// members remain significant. Compare without rewriting the authored data.
function sameBlockData(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (left === null || right === null || typeof left !== "object" || typeof right !== "object") return false;
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length
      && left.every((value, index) => sameBlockData(value, right[index]));
  }
  const a = left as Record<string, unknown>, b = right as Record<string, unknown>;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every(key =>
    Object.prototype.hasOwnProperty.call(b, key) && sameBlockData(a[key], b[key]));
}
export function mutations(
  pageId: string,
  old: Block[],
  drafts: DraftBlock[],
): Command[] {
  const before = new Map(old.map((b) => [b.id, b]));
  const commands: Command[] = [];
  const ids = new Set(drafts.map((b) => b.id));
  const oldDepths = new Map(
    fromDocument(toDocument(old)).map((b) => [b.id, b.depth]),
  );
  const depthStack: DraftBlock[] = [];
  const groups = new Map<string | null, string[]>();
  for (const b of drafts) {
    while (depthStack.length > b.depth) depthStack.pop();
    const prev = before.get(b.id!);
    const parent =
      prev && oldDepths.get(b.id) === b.depth && b.parentBlockId === prev.parentBlockId
        ? prev.parentBlockId
        : b.depth > 0
          ? (depthStack[b.depth - 1]?.id ?? null)
          : null;
    depthStack[b.depth] = b;
    b.parentBlockId = parent;
    if (!prev)
      commands.push({
        kind: "block.insert",
        block: {
          blockId: b.id!,
          pageId,
          ...(parent ? { parentBlockId: parent } : {}),
          type: b.type,
          schemaVersion: b.schemaVersion,
          data: b.data,
        },
      });
    else {
      if (prev.parentBlockId !== parent)
        commands.push({
          kind: "block.move",
          blockId: b.id!,
          pageId,
          parentBlockId: parent,
        });
      if (
        prev.type !== b.type ||
        !sameBlockData(prev.data, b.data)
      )
        commands.push({
          kind: "block.update",
          blockId: b.id!,
          type: b.type,
          schemaVersion: b.schemaVersion,
          data: b.data,
        });
    }
    const list = groups.get(parent) ?? [];
    list.push(b.id!);
    groups.set(parent, list);
  }
  for (const b of orderedBlocks(old)
    .filter((b) => !ids.has(b.id))
    .reverse())
    commands.push({
      kind: "block.delete",
      blockId: b.id,
      includeDescendants: false,
    });
  for (const [parent, orderedBlockIds] of groups) {
    const oldIDs = old
      .filter((b) => b.parentBlockId === parent)
      .sort((a, b) => a.sortKey.localeCompare(b.sortKey))
      .map((b) => b.id);
    if (JSON.stringify(oldIDs) !== JSON.stringify(orderedBlockIds))
      commands.push({
        kind: "block.reorder",
        pageId,
        parentBlockId: parent,
        orderedBlockIds,
      });
  }
  return commands;
}
// Structural commands can touch unchanged neighbors, parents, and descendants.
// Collect guards from the command sequence, using the accepted baseline's
// revisions even when the same block is touched more than once in the batch.
export function blockMutationGuards(
  old: Block[],
  commands: Command[],
): ExpectedRevision[] {
  const before = new Map(old.map((block) => [block.id, block]));
  const parents = new Map(old.map((block) => [block.id, block.parentBlockId]));
  const inserted = new Set<string>();
  const required = new Set<string>();
  const require = (id: string | null | undefined) => {
    if (id) required.add(id);
  };
  const subtree = (root: string): string[] => {
    const found = new Set([root]);
    for (const parent of found)
      for (const [id, owner] of parents) if (owner === parent) found.add(id);
    return [...found];
  };
  const siblings = (parent: string | null) => {
    for (const [id, owner] of parents) if (owner === parent) require(id);
  };
  for (const command of commands) {
    switch (command.kind) {
      case "block.insert": {
        const block = command.block;
        inserted.add(block.blockId);
        require(block.blockId);
        require(block.parentBlockId);
        parents.set(block.blockId, block.parentBlockId ?? null);
        if (block.afterBlockId) siblings(block.parentBlockId ?? null);
        break;
      }
      case "block.update":
        require(command.blockId);
        break;
      case "block.delete": {
        const deleted = command.includeDescendants
          ? subtree(command.blockId)
          : [command.blockId];
        for (const id of deleted) {
          require(id);
          parents.delete(id);
        }
        break;
      }
      case "block.move":
        for (const id of subtree(command.blockId)) require(id);
        require(command.parentBlockId);
        parents.set(command.blockId, command.parentBlockId);
        // Moving repositions the destination's complete current sibling set.
        siblings(command.parentBlockId);
        break;
      case "block.reorder":
        for (const id of command.orderedBlockIds) require(id);
        break;
    }
  }
  return [...required].map((id) => {
    const revision =
      before.get(id)?.revision ?? (inserted.has(id) ? "0" : null);
    if (revision === null)
      throw new Error(`Block ${id} is outside the accepted editor baseline`);
    return { target: { entityType: "block", entityId: id }, revision };
  });
}
export function readable(block: Block) {
  const d = block.data as any;
  return d.richText
    ? plain(d.richText)
    : (d.source ?? d.latex ?? d.readableText ?? d.displayName ?? block.type);
}
