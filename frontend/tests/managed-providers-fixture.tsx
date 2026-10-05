import React from "react";
import { createRoot } from "react-dom/client";
import { ManagedProviderPanel } from "../src/providers/ManagedProviderPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    { id: id(3), title: "Launch", revision: "2" },
    { id: id(4), title: "Private journal", revision: "3" },
  ],
} as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).managedCalls = calls;
const provider =
  new URLSearchParams(location.search).get("provider") === "claude_code"
    ? "claude_code"
    : "codex";
const status: any = { runs: [], activeRunId: "", runtimes: [] };
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "providers.claude.list")
          return ok({
            available: true,
            reason: "",
            profiles: [5, 6].map((n) => ({
              id: id(n),
              label: n === 5 ? "Personal Claude" : "API Claude",
              billingMode: n === 5 ? "claude_subscription" : "anthropic_api",
              createdAt: "2026-10-05T00:00:00Z",
              auth: {
                loggedIn: true,
                account: `vendor-${n}`,
                accountLabel: `account-${n}@example.test`,
                method: "oauth",
                billingMode: n === 5 ? "claude_subscription" : "anthropic_api",
                reason: "",
              },
            })),
          });
        if (method === "providers.auth.list")
          return ok({
            available: true,
            reason: "",
            registrations: [5, 6].map((n) => ({
              id: id(n),
              identity: {
                registrationId: id(n),
                subject: `subject-${n}`,
                clientId: `client-${n}`,
                email: `account-${n}@example.test`,
                name: "Fixture",
              },
              state: "signed_in",
              planAuthorized: true,
              expiresAt: "2026-12-01T00:00:00Z",
            })),
          });
        if (method === "providers.runs") return ok(status);
        if (method === "providers.discover") {
          status.runtimes = [
            {
              provider: request.provider,
              version: request.provider === "codex" ? "0.153.4" : "2.1.283",
              sha256: "a".repeat(64),
              available: !(window as any).runtimeUnavailable,
              reason: (window as any).runtimeUnavailable
                ? "Contained Claude startup failed"
                : "",
            },
          ];
          return ok(status.runtimes[0]);
        }
        if (method === "providers.models")
          return ok({
            registrationId: request.registrationId,
            models: [
              {
                id: request.registrationId === id(5) ? "model-a" : "model-b",
                name: "Account-specific model",
              },
            ],
          });
        if (method === "providers.preview")
          return ok({
            hash: "b".repeat(64),
            pages: workspace.pages
              .filter((p) => request.pageIds.includes(p.id))
              .map((p) => ({
                pageId: p.id,
                title: p.title,
                revision: p.revision,
              })),
            notice: "Only selected page context is shared.",
          });
        if (method === "providers.start") {
          if ((window as any).stalePreview)
            return JSON.stringify({
              ok: false,
              error: {
                code: "REVISION_CONFLICT",
                message:
                  "Selected content changed; preview the shared context again",
              },
            });
          const run = {
            id: id(7),
            provider: request.provider,
            account: request.registrationId,
            accountLabel: "account-5@example.test",
            billingMode: request.billingMode,
            model: request.model,
            state: "running",
            prompt: request.prompt,
            text: "Partial response",
            message: "",
            createdAt: "2026-10-05T00:00:00Z",
            pageIds: request.pageIds,
            tools: request.tools,
          };
          status.runs = [run];
          status.activeRunId = run.id;
          return ok(run);
        }
        if (method === "providers.cancel") {
          status.activeRunId = "";
          status.runs[0].state = "cancelled";
          status.runs[0].message =
            "Provider execution was cancelled; partial output is retained";
          status.runs[0].checkpoint = { threadId: "vendor-thread" };
          return ok({ state: "cancellation_requested" });
        }
        throw new Error(`Unexpected method ${method}`);
      },
    },
  },
};
createRoot(document.getElementById("root")!).render(
  <ManagedProviderPanel workspace={workspace} provider={provider} />,
);
