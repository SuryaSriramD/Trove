import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { CapturePanel } from "../src/capture/CapturePanel";
const w = window as any;
const id = (n: number) =>
  `01990000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = (n: number) =>
  ({ workspaceId: id(n), scopeGrantId: id(n + 10), pages: [] }) as any;
const plan = (name = "Old capture") => ({
  planId: id(20),
  planHash: "hash",
  state: "preview",
  rootPageId: id(21),
  initialized: false,
  cursor: 0,
  sourceName: name,
  items: [
    {
      sourceLocator: name,
      outcome: "supported",
      reason: "Retained source",
      committed: false,
    },
  ],
  captureDestinationTitle: "Inbox",
});
w.commitCalls = 0;
w.captureChanged = 0;
w.captureCalls = [];
w.currentCaptureWorkspace = 1;
w.runtime = { EventsOn: () => () => {} };
w.go = {
  main: {
    App: {
      ClipperStatus: async () =>
        JSON.stringify({ listening: false, paired: false }),
      ClipperReviewPlans: async () => {
        const result = JSON.stringify(
          w.currentCaptureWorkspace === 1 ? [plan("Old browser draft")] : [],
        );
        if (w.delayBrowserDrafts)
          return new Promise((resolve) => {
            w.resolveBrowserDrafts = () => resolve(result);
          });
        return result;
      },
      BeginClipperPairing: async () => "{}",
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw);
        w.captureCalls.push({ method, request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "index.roots") return ok([]);
        if (method === "capture.preview") {
          const result = ok(plan());
          if (w.delayCapturePreview)
            return new Promise((resolve) => {
              w.resolveCapturePreview = () => resolve(result);
            });
          return result;
        }
        if (method === "imports.commit") {
          w.commitCalls++;
          if (w.commitCalls > 100)
            throw Error("fixture exhausted commit budget");
          if (w.delayCaptureCommit)
            return new Promise((resolve) => {
              w.resolveCaptureCommit = () =>
                resolve(
                  ok({
                    ...plan(),
                    planHash: "hash-2",
                    state: "committing",
                    cursor: 1,
                  }),
                );
            });
          return ok(plan());
        }
        throw Error(`unexpected ${method}`);
      },
    },
  },
};
function Fixture() {
  const [n, setN] = useState(1);
  return (
    <>
      <button
        onClick={() => {
          w.currentCaptureWorkspace = 2;
          setN(2);
        }}
      >
        Switch workspace
      </button>
      <CapturePanel
        workspace={workspace(n)}
        onChanged={async () => {
          w.captureChanged++;
        }}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
