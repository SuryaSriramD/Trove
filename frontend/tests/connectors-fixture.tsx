import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ConnectorsPanel } from "../src/connectors/ConnectorsPanel";
import type { WorkspaceSnapshot, S } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const w = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  pages: [],
  records: [],
  collections: [],
  files: [],
} as unknown as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).connectorCalls = calls;
let account: S.ConnectorAccount = {
  version: 1,
  id: id(3),
  kind: "github",
  identity: { subject: "42", provider: "github", label: "Example" },
  revision: "a1",
  registrationId: id(4),
  state: "active",
  selections: [],
  updatedAt: new Date().toISOString(),
};
const selection = {
  id: "org/repo",
  kind: "repository",
  label: "Selected repository",
  parentId: "1",
};
let proposals: S.ConnectorProposal[] = [];
const source: S.ConnectorSnapshot = {
  version: 1,
  id: "source1",
  accountId: id(3),
  selectionId: selection.id,
  remoteId: "issue:1",
  kind: "issue",
  title: "Remote issue",
  text: "Private evidence",
  url: "https://github.com/org/repo/issues/1",
  remoteRevision: "r1",
  state: "present",
  content: {},
  retrievedAt: new Date().toISOString(),
  externalAiAllowed: false,
};
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method: string, raw: string) => {
        const r = JSON.parse(raw);
        calls.push({ method, request: r });
        let result: any;
        if (method === "connectors.list")
          result = r.workspaceId === id(9) ? [] : [account];
        else if (method === "connectors.draft")
          result = { revision: "", fields: {} };
        else if (method === "connectors.saveDraft")
          result = { revision: "draft1" };
        else if (method === "connectors.history")
          result = { refreshes: [], proposals };
        else if (method === "connectors.native.discover")
          result = { selections: [selection], nextCursor: "" };
        else if (method === "connectors.selections") {
          account = { ...account, selections: r.selections, revision: "a2" };
          result = account;
        } else if (method === "connectors.native.prepare") {
          const p: S.ConnectorProposal = {
            version: 1,
            id: id(5),
            accountId: account.id,
            accountRevision: account.revision,
            identity: account.identity,
            action: {
              ...r.action,
              effects: ["Notify participants in the selected repository"],
              arguments: {
                ...r.action.arguments,
                body: r.action.arguments.body + "\n<!-- reviewed marker -->",
              },
            },
            hash: "a".repeat(64),
            state: "review",
            createdAt: new Date().toISOString(),
          };
          proposals = [p];
          result = p;
        } else if (method === "connectors.native.execute") {
          proposals = proposals.map((p) => ({
            ...p,
            state: "unknown",
            receipt: {
              version: 1,
              proposalId: p.id,
              outcome: "unknown",
              detail: "Request timed out after send",
              recordedAt: new Date().toISOString(),
            },
          }));
          result = { state: "started" };
        } else if (method === "connectors.native.reconcile") {
          const receipt: S.ConnectorReceipt = {
            version: 1,
            proposalId: id(5),
            outcome: "succeeded",
            detail: "Existing remote result found",
            recordedAt: new Date().toISOString(),
          };
          proposals = proposals.map((p) => ({
            ...p,
            state: "succeeded",
            receipt,
          }));
          result = receipt;
        } else if (method === "connectors.search") result = [source];
        else if (method === "connectors.native.preview") {
          result = source;
          if ((window as any).holdConnectorPreview) {
            return new Promise((resolve) => {
              (window as any).resolveConnectorPreview = () =>
                resolve(JSON.stringify({ ok: true, result }));
            });
          }
        } else if (method === "connectors.native.import") {
          return JSON.stringify({
            ok: false,
            error: {
              code: "CONFLICT",
              message: "External content changed; inspect its latest preview",
            },
          });
        } else if (method === "connectors.native.authBegin")
          result = {
            id: id(6),
            kind: r.registration.kind,
            state: "waiting",
            message: "Authorize in the browser",
            expiresAt: new Date(Date.now() + 600000).toISOString(),
          };
        else if (method === "connectors.native.authPoll")
          result = {
            id: id(6),
            kind: "google_drive",
            state: "waiting",
            message: "Authorize in the browser",
            expiresAt: new Date(Date.now() + 600000).toISOString(),
          };
        else if (method === "connectors.native.authCancel") result = {};
        else throw new Error("Unexpected connector fixture method " + method);
        return JSON.stringify({ ok: true, result });
      },
    },
  },
};
function Fixture() {
  const [other, setOther] = useState(false);
  return (
    <>
      <button onClick={() => setOther(true)}>Switch fixture workspace</button>
      <ConnectorsPanel
        workspace={other ? { ...w, workspaceId: id(9) } : w}
        onChanged={() => {}}
        onOpenPage={() => {}}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
