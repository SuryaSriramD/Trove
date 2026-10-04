import mermaid from "mermaid";

// Parse the exact reviewed source without executing or rendering it. The service
// separately checks the safe source envelope and canonical mutation permissions.
export async function validateMermaidDraft(source: string): Promise<void> {
  if (!source.trim() || new TextEncoder().encode(source).length > 32000)
    throw new Error("Mermaid source must contain 1–32000 bytes.");
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    suppressErrorRendering: true,
  });
  try {
    if (!(await mermaid.parse(source))) throw new Error("Invalid diagram");
  } catch {
    throw new Error(
      "Mermaid syntax needs a correction before approval. Edit the draft and preview it again.",
    );
  }
}

export async function validateMermaidCommands(
  commands: unknown[],
): Promise<void> {
  for (const command of commands) {
    if (!command || typeof command !== "object") continue;
    const c = command as Record<string, unknown>;
    const b =
      c.kind === "block.insert"
        ? c.block
        : c.kind === "block.update"
          ? c
          : null;
    if (!b || typeof b !== "object") continue;
    const block = b as Record<string, unknown>;
    if (block.type !== "mermaid") continue;
    const data = block.data as { source?: unknown } | undefined;
    await validateMermaidDraft(
      typeof data?.source === "string" ? data.source : "",
    );
  }
}
