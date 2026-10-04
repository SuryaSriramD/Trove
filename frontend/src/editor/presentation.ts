import type { NodeJSON } from "./codec";

export type PresentationBlock = { key: string; id?: string; parent: string | null; kind: string; collapsed: boolean };
// Canonical pages use a flat ProseMirror sequence. Current indentation is used
// here so an unsaved indent/move gets the same visibility as its next commit.
export function presentationBlocks(nodes: NodeJSON[]): PresentationBlock[] {
  const stack: string[] = [];
  return nodes.map((node, index) => {
    const attrs = node.attrs ?? {};
    const depth = Math.max(0, Number(attrs.depth) || 0);
    const key = attrs.blockId ?? attrs.clientKey ?? `draft-${index}`;
    stack.length = Math.min(stack.length, depth);
    const parent = depth > 0 ? stack[depth - 1] ?? attrs.parentBlockId ?? null : null;
    stack[depth] = key;
    return { key, id: attrs.blockId, parent, kind: attrs.kind, collapsed: attrs.data?.collapsed === true };
  });
}
export function blockVisibility(blocks: PresentationBlock[], revealedIds: ReadonlySet<string>) {
  const byKey = new Map(blocks.map(block => [block.key, block]));
  const revealed = new Set<string>(), expanded = new Set<string>();
  for (const target of blocks) {
    if (!target.id || !revealedIds.has(target.id)) continue;
    let current: PresentationBlock | undefined = target;
    while (current && !revealed.has(current.key)) {
      revealed.add(current.key);
      if (current.key !== target.key) expanded.add(current.key);
      current = current.parent ? byKey.get(current.parent) : undefined;
    }
  }
  const hidden = new Set<string>();
  for (const block of blocks) {
    let current = block.parent ? byKey.get(block.parent) : undefined;
    const visited = new Set<string>();
    while (current && !visited.has(current.key)) {
      visited.add(current.key);
      const collapsed = ["toggle", "toggle_heading"].includes(current.kind) && current.collapsed && !expanded.has(current.key);
      const proxy = ["columns", "tab_container"].includes(current.kind) && !revealed.has(block.key);
      if (collapsed || proxy) { hidden.add(block.key); break; }
      current = current.parent ? byKey.get(current.parent) : undefined;
    }
  }
  return { hidden, expanded };
}
