// Opt-in local qualification only. Fixed labels and durations are the entire
// wire payload: no request, DOM text, keys, selectors or workspace identities.
const bridgeLabels = new Map([
  ["queryRecords", "database_bridge"], ["workspace.snapshot", "snapshot_bridge"],
  ["batch", "mutation_bridge"], ["search.query", "search_bridge"],
]);
export const bridgeMetric = (method: string): string | undefined => bridgeLabels.get(method);

type Transport = {
  QualificationPerformanceEnabled?(): Promise<boolean>;
  RecordQualificationPerformance?(metric: string, milliseconds: number): Promise<boolean>;
};
const labels = new Set([
  "database_bridge", "snapshot_bridge", "mutation_bridge", "search_bridge",
  "database_load_to_frames", "database_response_to_frames", "input_to_frames", "scroll_to_frames",
]);

export function createQualificationRecorder(
  transport: Transport,
  now: () => number,
  frame: (callback: FrameRequestCallback) => number,
  cancelFrame: (id: number) => void,
  visible: () => boolean,
) {
  let enabled = false;
  let stopped = false;
  const counts = new Map<string, number>();
  const pending = new Set<() => void>();
  const ready = transport.QualificationPerformanceEnabled?.().then(value => {
    enabled = !stopped && value === true && !!transport.RecordQualificationPerformance;
  }).catch(() => {}) ?? Promise.resolve();
  const start = () => enabled ? now() : undefined;
  function record(label: string, started: number | undefined) {
    if (!enabled || started === undefined || !labels.has(label)) return;
    const milliseconds = now() - started;
    if (!Number.isFinite(milliseconds) || milliseconds < 0 || milliseconds > 3600000) return;
    const count = counts.get(label) ?? 0;
    // Bound bridge traffic as well as the backend's independent sample bound.
    // One extra sample records the backend's truncation marker; subsequent
    // events add no traffic. A capped run must not claim full sample coverage.
    if (count >= 513) return;
    counts.set(label, count + 1);
    void transport.RecordQualificationPerformance?.(label, milliseconds).catch(() => {});
  }
  function afterFrames(observations: [string, number | undefined][]) {
    if (!enabled || !visible() || pending.size >= 16 || !observations.some(([, start]) => start !== undefined)) return () => {};
    let id: number;
    let cancelled = false;
    const cancel = () => { cancelled = true; cancelFrame(id); pending.delete(cancel); };
    pending.add(cancel);
    id = frame(() => {
      if (cancelled || !visible()) { pending.delete(cancel); return; }
      id = frame(() => {
        pending.delete(cancel);
        if (!cancelled && visible()) for (const [label, started] of observations) record(label, started);
      });
    });
    return cancel;
  }
  function stop() {
    stopped = true;
    enabled = false;
    for (const cancel of pending) cancel();
  }
  return { ready, start, record, afterFrames, stop };
}

let recorder: ReturnType<typeof createQualificationRecorder> | undefined;
export const qualificationStart = () => recorder?.start();
export const qualificationRecord = (label: string | undefined, started: number | undefined) => {
  if (label) recorder?.record(label, started);
};
export const qualificationFrames = (observations: [string, number | undefined][]) =>
  recorder?.afterFrames(observations) ?? (() => {});

export function startQualificationPerformance() {
  const bridge = window.go?.main.App;
  if (!bridge?.QualificationPerformanceEnabled || recorder) return;
  recorder = createQualificationRecorder(bridge, () => performance.now(),
    callback => requestAnimationFrame(callback), id => cancelAnimationFrame(id),
    () => document.visibilityState === "visible");
  // These are trusted input-event to two animation-frame opportunities. They do
  // not claim actual screen paint, input-device latency or canonical durability.
  const input = (event: Event) => {
    if (!event.isTrusted || !(event.target instanceof HTMLElement) ||
        !event.target.closest('input, textarea, [contenteditable="true"]')) return;
    qualificationFrames([["input_to_frames", qualificationStart()]]);
  };
  const scroll = (event: Event) => {
    if (event.isTrusted) qualificationFrames([["scroll_to_frames", qualificationStart()]]);
  };
  void recorder.ready.then(() => {
    if (qualificationStart() === undefined) return;
    document.addEventListener("input", input, true);
    document.addEventListener("scroll", scroll, { capture: true, passive: true });
    window.addEventListener("pagehide", () => {
      document.removeEventListener("input", input, true);
      document.removeEventListener("scroll", scroll, true);
      recorder?.stop();
    }, { once: true });
  });
}
