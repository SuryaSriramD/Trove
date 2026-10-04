import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ProviderAccounts } from "../src/providers/ProviderAccounts";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const workspace = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
} as WorkspaceSnapshot;
const calls: any[] = [];
(window as any).accountCalls = calls;
let attempt: any;
let registrations: any[] = [3, 4].map((n) => ({
  id: id(n),
  identity: {
    registrationId: id(n),
    subject: "subject",
    clientId: `oaiapp_${n}`,
    email: "same@example.test",
    name: "Fixture",
  },
  state: "signed_in",
  planAuthorized: n === 3,
  expiresAt: "2026-10-05T10:00:00Z",
}));
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const request = JSON.parse(raw);
        calls.push({ method, request });
        const ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "providers.auth.list")
          return ok({ available: true, reason: "", registrations });
        if (method === "providers.auth.begin") {
          attempt = {
            id: id(8),
            registrationId: request.registrationId || "",
            state: "waiting",
            message: "Complete sign-in in your browser",
            expiresAt: "2026-10-05T10:00:00Z",
          };
          return ok(attempt);
        }
        if (method === "providers.auth.attempt") {
          if ((window as any).finishAccount) {
            const account = registrations.find(
              (r) => r.id === attempt.registrationId,
            );
            if (account) account.planAuthorized = true;
            else {
              attempt.registrationId = id(5);
              registrations.push({
                ...registrations[0],
                id: id(5),
                identity: {
                  ...registrations[0].identity,
                  registrationId: id(5),
                  clientId: "oaiapp_5",
                },
              });
            }
            attempt = {
              ...attempt,
              state: "completed",
              message: "Signed in. Select this account in Trove",
            };
          }
          return ok(attempt);
        }
        if (method === "providers.auth.cancel") {
          attempt = { ...attempt, state: "cancelled" };
          return ok({ state: "cancelled" });
        }
        if (method === "providers.auth.signOut") {
          registrations = registrations.map((r) =>
            r.id === request.registrationId
              ? {
                  ...r,
                  state: (window as any).failCleanup
                    ? "sign_out_incomplete"
                    : "signed_out",
                  planAuthorized: false,
                }
              : r,
          );
          if ((window as any).failCleanup)
            return JSON.stringify({
              ok: false,
              error: {
                code: "WORKER_FAILED",
                message: "Protected credentials could not be cleared",
              },
            });
          return ok({ localCleared: true, remoteRevoked: false });
        }
        throw new Error(`Unexpected operation ${method}`);
      },
    },
  },
};
function Fixture() {
  const [selected, setSelected] = useState("");
  return (
    <>
      <ProviderAccounts
        workspace={workspace}
        selectedRegistrationId={selected}
        onSelect={setSelected}
      />
      <output aria-label="Selected registration">{selected || "None"}</output>
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
