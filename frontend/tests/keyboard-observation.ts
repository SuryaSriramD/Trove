import type { Page } from "@playwright/test";

// Passive diagnostic observations only: no selection/editor commands or writes.
export async function observeLineEdgeKeys(page: Page) {
  await page.evaluate(() => {
    const events: unknown[] = [];
    (window as any).__troveLineEdgeKeys = events;
    const capture = (phase: string, event: KeyboardEvent) => {
      if (!["Home", "End", "Shift"].includes(event.key)) return;
      const element = document.querySelector(".tiptap[contenteditable=true]") as HTMLElement & { editor?: any } | null;
      const native = window.getSelection(), editor = element?.editor;
      events.push({ phase, key: event.key, code: event.code, shift: event.shiftKey, meta: event.metaKey, alt: event.altKey,
        trusted: event.isTrusted, defaultPrevented: event.defaultPrevented, focused: document.activeElement?.getAttribute("aria-label"),
        native: { anchorText: native?.anchorNode?.textContent, anchorOffset: native?.anchorOffset,
          focusText: native?.focusNode?.textContent, focusOffset: native?.focusOffset, selected: native?.toString() },
        modelAvailable: Boolean(editor?.state?.selection), model: editor?.state.selection.toJSON() ?? null, modelText: editor?.state.doc.textContent });
    };
    document.addEventListener("keydown", event => {
      capture("keydown-before", event);
      queueMicrotask(() => capture("keydown-after", event));
    }, { capture: true, passive: true });
    document.addEventListener("keyup", event => capture("keyup", event), { capture: true, passive: true });
  });
}
export async function lineEdgeObservations(page: Page) {
  return page.evaluate(() => (window as any).__troveLineEdgeKeys ?? []);
}
