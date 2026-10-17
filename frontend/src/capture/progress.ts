export type CaptureImportProgress = {
  planId: string;
  planHash: string;
  initialized: boolean;
  cursor: number;
  state: string;
  items: readonly unknown[];
};

// The reviewed hash stays fixed while initialization and the durable cursor advance.
export function assertCaptureImportProgress(previous: CaptureImportProgress, next: CaptureImportProgress): void {
  if (next.planId !== previous.planId || next.planHash !== previous.planHash || next.items.length !== previous.items.length)
    throw Error("Capture review changed while saving; review the draft again.");
  const invalid = typeof next.initialized !== "boolean" || (next.state !== "running" && next.state !== "complete") ||
    !Number.isSafeInteger(next.cursor) || next.cursor < 0 || next.cursor < previous.cursor || next.cursor > next.items.length ||
    (previous.initialized && !next.initialized) || (!next.initialized && next.cursor !== 0);
  const complete = next.state === "complete";
  const advanced = (!previous.initialized && next.initialized) || next.cursor > previous.cursor;
  if (invalid || (complete ? !next.initialized || next.cursor !== next.items.length : !advanced))
    throw Error("Capture save made no progress; refresh the draft before retrying.");
}
