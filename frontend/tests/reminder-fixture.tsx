import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { ReminderPanel } from "../src/reminders/ReminderPanel";
import type { WorkspaceSnapshot, S } from "../src/api";
const key = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const calls: any[] = [];
(window as any).reminderCalls = calls;
const w = {
  workspaceId: key(1),
  scopeGrantId: key(2),
  pages: [{ id: key(3), title: "Source task", revision: "1" }],
  records: [
    { id: key(4), pageId: key(3), collectionId: key(5), revision: "1" },
  ],
  collections: [{ id: key(5), revision: "1" }],
  taskFacets: [{ recordId: key(4), statusPropertyId: key(6) }],
  properties: [{ id: key(6), revision: "1", config: { doneOptionId: key(7) } }],
} as WorkspaceSnapshot;
let rules: S.ReminderRule[] = [];
let item: S.ReminderInboxItem = {
  id: key(8) + ":1",
  reminderId: key(8),
  revision: "1",
  dueAt: "2026-10-14T09:00:00Z",
  outcome: "unknown",
  pageId: key(3),
  title: "Source task",
  actionRevision: 0,
};
let status = {
  available: true,
  enabled: false,
  registration: "notRegistered",
  publication: "inactive",
  permission: "notDetermined",
  epoch: "",
  generation: 0,
};
const native = {
  Dispatch: async (method: string, raw: string) => {
    const request = JSON.parse(raw);
    calls.push({ method, request });
    let result: any;
    if (method === "reminders.list") {
      result =
        request.workspaceId === key(9)
          ? { rules: [], inbox: [], more: false }
          : {
              rules,
              inbox: [
                {
                  ...item,
                  title: request.before ? "Older task" : "Source task",
                },
              ],
              more: false,
              ...(!request.before ? { nextCursor: item.id } : {}),
            };
      if ((window as any).holdReminderList && request.workspaceId === key(1)) {
        (window as any).holdReminderList = false;
        return new Promise<string>((resolve) => {
          (window as any).resolveReminderList = () =>
            resolve(JSON.stringify({ ok: true, result }));
        });
      }
    } else if (method === "allocateIds") result = { ids: [key(8)] };
    else if (method === "reminders.history") result = rules;
    else if (method === "batch") {
      for (const c of request.commands) {
        if (c.kind === "reminder.put")
          rules = [
            {
              id: c.reminderId,
              revision: "1",
              pageId: c.pageId,
              title: "Source task",
              startDate: c.startDate,
              localTime: c.localTime,
              timezone: c.timezone,
              frequency: c.frequency,
              enabled: c.enabled,
              private: c.private,
            },
          ];
        if (c.kind === "reminder.inboxAction")
          item = {
            ...item,
            action: c.action,
            snoozedUntil: c.snoozedUntil,
            actionRevision: (item.actionRevision ?? 0) + 1,
          };
      }
      result = {};
    } else throw Error(method);
    return JSON.stringify({ ok: true, result });
  },
  PickFile: async () => "",
  ReminderStatus: async () => status,
  RefreshReminders: async () => {
    calls.push({ method: "native.refresh" });
    return status;
  },
  SetReminderBackgroundEnabled: async (enabled: boolean) => {
    calls.push({ method: "native.enable", enabled });
    status = {
      ...status,
      enabled,
      registration: enabled ? "enabled" : "notRegistered",
      publication: enabled ? "acknowledged" : "inactive",
      permission: "authorized",
    };
    return status;
  },
};
window.go = { main: { App: native } };
function Fixture() {
  const [current, setCurrent] = useState(w);
  return (
    <>
      <button onClick={() => setCurrent({ ...w, workspaceId: key(9) })}>
        Switch fixture workspace
      </button>
      <ReminderPanel
        workspace={current}
        onRefresh={() => {}}
        onNavigate={(location) => calls.push({ method: "navigate", location })}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
