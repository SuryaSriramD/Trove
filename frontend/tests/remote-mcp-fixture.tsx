import React from "react";
import { createRoot } from "react-dom/client";
import { RemoteMCPPanel } from "../src/providers/RemoteMCPPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    { id: id(3), title: "Shared research", revision: "4", deletedAt: null },
    { id: id(4), title: "Private journal", revision: "6", deletedAt: null },
  ],
} as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).remoteCalls = calls;
let status: any = {
  enabled: false,
  origin: "",
  bindAddress: "",
  certificateExpiresAt: "2026-12-01T00:00:00Z",
  connections: [],
  authorizations: [],
  message: "",
};
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "providers.remote.status") return ok(status);
        if (method === "providers.remote.selectCertificate")
          return ok({
            handle: id(5),
            dnsNames: ["trove.example.test"],
            expiresAt: status.certificateExpiresAt,
          });
        if (method === "providers.remote.enable") {
          status = {
            ...status,
            enabled: true,
            origin: request.publicOrigin,
            bindAddress: request.bindAddress,
          };
          return ok(status);
        }
        if (method === "providers.preview")
          return ok({
            pages: workspace.pages
              .filter((p) => request.pageIds.includes(p.id))
              .map((p) => ({
                pageId: p.id,
                title: p.title,
                revision: p.revision,
              })),
            notice: "Only these selected pages are shared.",
          });
        if (method === "providers.remote.createConnection") {
          const c = {
            id: id(6),
            url: "https://trove.example.test/mcp/opaque-fixture",
            pageIds: request.pageIds,
            tools: request.tools,
            clientId: "",
            state: "awaiting_client",
          };
          status.connections.push(c);
          return ok(c);
        }
        if (
          method === "providers.remote.approveAuthorization" ||
          method === "providers.remote.denyAuthorization"
        ) {
          const a = status.authorizations.find(
            (a: any) => a.id === request.authorizationId,
          );
          a.state = method.includes("approve") ? "approved" : "denied";
          if (a.state === "approved") {
            status.connections[0].state = "authorized";
            status.connections[0].clientId = a.clientId;
          }
          return ok(status);
        }
        if (method === "providers.remote.disconnect") {
          status.connections.find(
            (c: any) => c.id === request.connectionId,
          ).state = "disconnected";
          return ok(status);
        }
        if (method === "providers.remote.disable") {
          status = {
            ...status,
            enabled: false,
            connections: [],
            authorizations: [],
          };
          return ok(status);
        }
        throw new Error(`Unexpected remote method ${method}`);
      },
    },
  },
};
(window as any).requestRemoteAuthorization = () => {
  status.authorizations = [
    {
      id: "a".repeat(43),
      connectionId: id(6),
      clientId: "https://client.example.test/oauth.json",
      clientName: "Supplied client label",
      metadataUrl: "https://client.example.test/oauth.json",
      redirectUri: "https://client.example.test/callback",
      comparisonCode: "REVIEW42",
      reviewHash: "c".repeat(64),
      state: "pending",
      expiresAt: "2099-12-01T00:00:00Z",
    },
  ];
};
createRoot(document.getElementById("root")!).render(
  <RemoteMCPPanel workspace={workspace} />,
);
