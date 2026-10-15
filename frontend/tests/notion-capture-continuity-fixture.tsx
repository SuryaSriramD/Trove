import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { NotionCapturePanel } from "../src/imports/NotionCapturePanel";
const w = window as any;
const id = (n: number) =>
  `01990000-0000-7000-8000-${String(n).padStart(12, "0")}`;
w.notionCalls = [];
w.notionPlans = [];
w.notionSteps = 0;
const capture = (revision = 1) => ({
  captureId: id(20),
  revision,
  state: "capturing",
  planId: id(21),
  authorizedRoots: [id(30)],
  pending: [{ kind: "page", id: id(30) }],
  completed: revision - 1,
});
w.go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const req = JSON.parse(raw);
        w.notionCalls.push({ method, req });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "imports.notion.list") return ok([]);
        if (method === "imports.notion.start") return ok(capture());
        if (method === "imports.notion.step") {
          w.notionSteps++;
          if (w.delayStep)
            return new Promise((resolve) => {
              w.resolveStep = () => resolve(ok(capture(2)));
            });
          if (w.notionSteps > 4) throw Error("fixture exhausted step budget");
          return ok(
            w.noProgress
              ? capture()
              : { ...capture(req.revision + 1), state: "ready", pending: [] },
          );
        }
        if (method === "imports.notion.finish") {
          if (w.delayFinish)
            return new Promise((resolve) => {
              w.resolveFinish = () => resolve(ok({ planId: id(21) }));
            });
          return ok({ planId: id(21) });
        }
        if (method === "imports.notion.status")
          return ok({ ...capture(3), state: "finished", pending: [] });
        throw Error("unexpected " + method);
      },
    },
  },
};
function Fixture() {
  const [ws, setWs] = useState(1),
    [scope, setScope] = useState(10);
  return (
    <>
      <button onClick={() => setWs(2)}>Switch workspace</button>
      <button onClick={() => setScope(11)}>Change scope</button>
      <NotionCapturePanel
        workspace={{ workspaceId: id(ws), scopeGrantId: id(scope) } as any}
        namespace="fixed-notion"
        onPlan={(p) => w.notionPlans.push(p)}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
