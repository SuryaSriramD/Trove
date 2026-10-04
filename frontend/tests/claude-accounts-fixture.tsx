import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ClaudeAccounts } from "../src/providers/ClaudeAccounts";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
} as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).claudeCalls = calls;
let profiles: any[] = [
  {
    id: id(3),
    label: "Work",
    billingMode: "claude_subscription",
    createdAt: "2026-10-05T10:00:00Z",
    auth: {
      loggedIn: true,
      method: "claude.ai",
      account: "vendor-fingerprint",
      accountLabel: "same@example.test",
      billingMode: "claude_subscription",
    },
  },
  {
    id: id(4),
    label: "API",
    billingMode: "anthropic_api",
    createdAt: "2026-10-05T10:00:00Z",
    auth: {
      loggedIn: false,
      method: "none",
      reason: "Sign in through the official runtime",
    },
  },
];
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "providers.claude.list")
          return ok({ available: true, reason: "", profiles });
        if (method === "providers.claude.create") {
          const p = {
            id: id(5),
            label: request.label,
            billingMode: request.billingMode,
            createdAt: "2026-10-05T10:00:00Z",
            auth: {
              loggedIn: false,
              method: "",
              reason: "Refresh status through the official runtime",
            },
          };
          profiles.push(p);
          return ok(p);
        }
        const p = profiles.find((p) => p.id === request.profileId);
        if (!p) throw Error("Unknown profile");
        if (method === "providers.claude.login") {
          p.auth = {
            loggedIn: false,
            method: "",
            reason: "Refresh status through the official runtime",
          };
          p.attempt = {
            id: id(8),
            profileId: p.id,
            state: "waiting",
            message: "Complete sign-in in the vendor terminal",
            expiresAt: "2026-10-05T10:10:00Z",
          };
          return ok(p.attempt);
        }
        if (method === "providers.claude.cancel") {
          p.attempt = {
            ...p.attempt,
            state: "cancelled",
            message: "Vendor login was cancelled",
          };
          return ok(p);
        }
        if (method === "providers.claude.status") {
          p.auth = {
            loggedIn: true,
            method:
              p.billingMode === "claude_subscription" ? "claude.ai" : "api_key",
            account: p.id,
            accountLabel: "same@example.test",
            billingMode: p.billingMode,
          };
          return ok(p);
        }
        if (method === "providers.claude.logout") {
          if ((window as any).failLogout) {
            p.signOutIncomplete = true;
            p.auth = {
              ...p.auth,
              reason:
                "Vendor logout has not completed; managed execution is disabled",
            };
            return JSON.stringify({
              ok: false,
              error: {
                code: "WORKER_FAILED",
                message: "The official runtime could not complete logout",
              },
            });
          }
          p.signOutIncomplete = false;
          p.auth = {
            loggedIn: false,
            method: "none",
            reason: "Sign in through the official runtime",
          };
          return ok(p);
        }
        throw Error(`Unexpected operation ${method}`);
      },
    },
  },
};
function Fixture() {
  const [selected, setSelected] = useState("");
  return (
    <>
      <ClaudeAccounts
        workspace={workspace}
        runtimeAvailable={!location.search.includes("unavailable")}
        selectedProfileId={selected.split("|")[0]}
        onSelect={(id, billing) => setSelected(id ? `${id}|${billing}` : "")}
      />
      <output aria-label="Selected Claude profile">{selected || "None"}</output>
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
