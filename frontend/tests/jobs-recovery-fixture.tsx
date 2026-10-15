import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { JobsPanel } from "../src/JobsPanel";
const w = window as any;
let progress: (() => void) | undefined;
w.jobCalls = [];
w.jobState = "failed";
w.currentJobsScope = "first-grant";
w.emitJobs = () => progress?.();
w.runtime = {
  EventsOn: (_: string, callback: () => void) => {
    progress = callback;
    return () => {
      progress = undefined;
    };
  },
};
w.go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw);
        w.jobCalls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "jobs.list") {
          const result = ok({
            jobs: [
              {
                jobId: "fixture-job",
                kind: "TRANSCRIBE_AUDIO",
                state: w.jobState,
                attempt: 1,
                revision: "9",
                createdAt: "2026-10-01T00:00:00Z",
                progress: {
                  completed: 2,
                  total: 10,
                  unit: "seconds",
                  message: `Jobs for ${w.currentJobsScope}`,
                },
              },
            ],
          });
          if (w.holdNextJobs) {
            w.holdNextJobs = false;
            return new Promise((resolve, reject) => {
              w.releaseJobs = () => resolve(result);
              w.rejectJobs = () => reject(Error("Old scope unavailable"));
            });
          }
          return result;
        }
        if (method === "jobs.retry" || method === "cancelJob") {
          w.jobState = method === "jobs.retry" ? "queued" : "cancelled";
          if (w.holdMutation)
            return new Promise((resolve) => {
              w.releaseMutation = () => resolve(ok({}));
            });
          return ok({});
        }
        throw Error(`unexpected ${method}`);
      },
    },
  },
};
function Fixture() {
  const [grant, setGrant] = useState("first-grant");
  return (
    <>
      <button
        onClick={() => {
          w.currentJobsScope = "second-grant";
          setGrant("second-grant");
        }}
      >
        Change scope
      </button>
      <JobsPanel
        workspace={
          { workspaceId: "fixture-workspace", scopeGrantId: grant } as any
        }
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
