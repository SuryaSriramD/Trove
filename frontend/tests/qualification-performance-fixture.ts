import { startQualificationPerformance, qualificationFrames, qualificationStart } from "../src/qualificationPerformance";

const state = { metrics: [] as [string, number][], diagnostics: [] as unknown[] };
(window as any).qualificationFixture = state;
window.go = { main: { App: {
  Dispatch: async () => "{}", PickFile: async () => "",
  QualificationPerformanceEnabled: async () => !location.search.includes("disabled"),
  RecordQualificationPerformance: async (label, ms) => { state.metrics.push([label, ms]); return true; },
  RecordQualificationPerformanceDiagnostics: async raw => { state.diagnostics.push(JSON.parse(raw)); return true; },
} } };
startQualificationPerformance();
document.querySelector("#frames")!.addEventListener("click", () => {
  qualificationFrames([["database_load_to_frames", qualificationStart()], ["database_response_to_frames", qualificationStart()]]);
});
