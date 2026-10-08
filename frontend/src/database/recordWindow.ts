// Rebuild only the cursor prefix needed for the requested window. Old-minute
// cursors are never reused; the service still enforces snapshot/clock identity.
export async function loadRecordWindow<T>(
  load: (cursor?: string) => Promise<{ records: T[]; nextCursor?: string }>,
  index: number,
  history: string[],
  cancelled: () => boolean,
) {
  const cursors = history.slice(0, index + 1);
  if (!cursors.length) cursors.push("");
  let current = Math.min(index, cursors.length - 1);
  while (true) {
    if (cancelled()) throw new Error("Record loading cancelled");
    const result = await load(cursors[current]);
    if (cancelled()) throw new Error("Record loading cancelled");
    if (current === index || !result.nextCursor) {
      return {
        ...result,
        index: current,
        history: cursors.slice(0, current + 1),
      };
    }
    cursors.push(result.nextCursor);
    current++;
  }
}
