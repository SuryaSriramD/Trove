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
  RecordQualificationPerformanceDiagnostics?(raw: string): Promise<boolean>;
};
const counterNames = [
  "probes_requested", "probes_scheduled", "probes_completed", "probes_cancelled",
  "probes_hidden", "probes_limited", "probes_missing_start", "records_attempted",
  "records_accepted", "records_rejected", "records_failed", "records_capped",
  "input_trusted", "input_ignored", "scroll_trusted", "scroll_ignored",
] as const;
type Counter = typeof counterNames[number];
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
  focused: () => boolean = () => false,
) {
  let enabled = false;
  let stopped = false;
  const counts = new Map<string, number>();
  const pending = new Set<() => void>();
  let pendingTransports = 0;
  let diagnosticPending = false;
  let activated = false;
  const counters = Object.fromEntries(counterNames.map(name => [name, 0])) as Record<Counter, number>;
  const increment = (name: Counter) => { counters[name] = Math.min(65535, counters[name] + 1); };
  const ready = transport.QualificationPerformanceEnabled?.().then(value => {
    enabled = !stopped && value === true && !!transport.RecordQualificationPerformance;
    activated = enabled;
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
    if (count >= 513) { increment("records_capped"); return; }
    counts.set(label, count + 1);
    increment("records_attempted");
    pendingTransports++;
    void transport.RecordQualificationPerformance?.(label, milliseconds).then(accepted => {
      increment(accepted ? "records_accepted" : "records_rejected");
    }).catch(() => increment("records_failed")).finally(() => { pendingTransports--; });
  }
  function afterFrames(observations: [string, number | undefined][]) {
    if (!enabled) return () => {};
    increment("probes_requested");
    if (!visible()) { increment("probes_hidden"); return () => {}; }
    if (pending.size >= 16) { increment("probes_limited"); return () => {}; }
    if (!observations.some(([, start]) => start !== undefined)) { increment("probes_missing_start"); return () => {}; }
    let id: number;
    let cancelled = false;
    let finished = false;
    const cancel = () => {
      if (finished || cancelled) return;
      cancelled = true; increment("probes_cancelled"); cancelFrame(id); pending.delete(cancel);
    };
    pending.add(cancel);
    increment("probes_scheduled");
    id = frame(() => {
      if (cancelled || !visible()) { if (!cancelled) increment("probes_hidden"); finished = true; pending.delete(cancel); return; }
      id = frame(() => {
        finished = true;
        pending.delete(cancel);
        if (!cancelled && visible()) {
          increment("probes_completed");
          for (const [label, started] of observations) record(label, started);
        } else if (!cancelled) increment("probes_hidden");
      });
    });
    return cancel;
  }
  function stop() {
    stopped = true;
    enabled = false;
    for (const cancel of pending) cancel();
  }
  // Separate bounded content-free observations. Awaiting this acknowledgement
  // confirms only delivery to the local collector, never paint or persistence.
  async function diagnostics() {
    if (!activated || !transport.RecordQualificationPerformanceDiagnostics || diagnosticPending) return false;
    diagnosticPending = true;
    try {
      return await transport.RecordQualificationPerformanceDiagnostics(JSON.stringify({
        ready: enabled, visible: visible(), focused: focused(), stopped,
        pendingFrames: pending.size, pendingTransports, counters,
      }));
    } catch { return false; }
    finally { diagnosticPending = false; }
  }
  return { ready, start, record, afterFrames, stop, diagnostics,
    event: (name: Counter) => { if (enabled && counterNames.includes(name)) increment(name); } };
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
    () => document.visibilityState === "visible", () => document.hasFocus());
  // These are trusted input-event to two animation-frame opportunities. They do
  // not claim actual screen paint, input-device latency or canonical durability.
  const input = (event: Event) => {
    if (!event.isTrusted || !(event.target instanceof HTMLElement) ||
        !event.target.closest('input, textarea, [contenteditable="true"]')) {
      recorder?.event("input_ignored"); return;
    }
    recorder?.event("input_trusted");
    qualificationFrames([["input_to_frames", qualificationStart()]]);
  };
  const scroll = (event: Event) => {
    recorder?.event(event.isTrusted ? "scroll_trusted" : "scroll_ignored");
    if (event.isTrusted) qualificationFrames([["scroll_to_frames", qualificationStart()]]);
  };
  void recorder.ready.then(() => {
    if (qualificationStart() === undefined) return;
    document.addEventListener("input", input, true);
    document.addEventListener("scroll", scroll, { capture: true, passive: true });
    void recorder?.diagnostics();
    const diagnosticTimer = window.setInterval(() => { void recorder?.diagnostics(); }, 500);
    window.addEventListener("pagehide", () => {
      document.removeEventListener("input", input, true);
      document.removeEventListener("scroll", scroll, true);
      window.clearInterval(diagnosticTimer);
      recorder?.stop();
      void recorder?.diagnostics();
    }, { once: true });
  });
}
