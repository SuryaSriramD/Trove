import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ProviderPanel } from "../src/providers/ProviderPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  pages: [{ id: id(3), title: "Shared launch", revision: "1" }],
  records: [],
  collections: [],
} as unknown as WorkspaceSnapshot;
const connection = {
  id: id(4),
  provider: "Original client",
  state: "connected",
  authMode: "paired",
  billingMode: "none",
  pageIds: [id(3)],
  tools: ["readPage"],
  token: "synthetic-pair-token",
};
let paired = false;
(window as any).holdProviderStatus = new URLSearchParams(location.search).has(
  "late",
);
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const r = JSON.parse(raw);
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "providers.remote.status")
          return ok({ enabled: false, connections: [], authorizations: [] });
        if (method === "providers.status") {
          if (r.workspaceId === id(1) && (window as any).holdProviderStatus) {
            (window as any).holdProviderStatus = false;
            return new Promise((resolve) => {
              (window as any).resolveProviderStatus = () =>
                resolve(ok({ connections: [connection] }));
            });
          }
          return ok({
            connections: r.workspaceId === id(1) && paired ? [connection] : [],
          });
        }
        if (method === "providers.proposals") return ok([]);
        if (method === "providers.preview")
          return ok({ pages: workspace.pages, notice: "Explicit preview" });
        if (method === "providers.connect") {
          paired = true;
          return ok({
            connection,
            url: "http://127.0.0.1:1/mcp",
            relayPath: "fixture-relay",
          });
        }
        throw new Error("Unexpected fixture call " + method);
      },
    },
  },
};
function Fixture() {
  const [other, setOther] = useState(false);
  return (
    <>
      <button onClick={() => setOther(true)}>Switch workspace</button>
      <ProviderPanel
        workspace={
          other ? { ...workspace, workspaceId: id(9), pages: [] } : workspace
        }
        onCommitted={() => {}}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
