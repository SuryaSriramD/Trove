import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { TaskEditor } from "../src/tasks/TaskEditor";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const query = new URLSearchParams(location.search);
const state: any = {
  workspaceId: id(1),
  scopeGrantId: id(2),
  snapshotCursor: "1",
  pages: [
    { id: id(3), title: "Publish Friday", revision: "1", deletedAt: null },
  ],
  records: [
    {
      id: id(4),
      pageId: id(3),
      collectionId: id(5),
      revision: "1",
      values: [{ propertyId: id(6), value: id(8) }],
    },
  ],
  collections: [{ id: id(5), revision: "1" }],
  properties: [
    {
      id: id(6),
      collectionId: id(5),
      name: "Status",
      type: "status",
      revision: "1",
      config: {
        options: [{ id: id(8), label: "To do", group: "not_started" }],
        doneOptionId: id(9),
        reopenOptionId: id(8),
      },
    },
  ],
  taskFacets: [{ recordId: id(4), statusPropertyId: id(6) }],
};
const fixture: any = {
  commands: [],
  previews: [],
  delay: query.has("delay"),
  release: () => {},
};
(window as any).fixture = fixture;
(window as any).go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const r = JSON.parse(raw),
          ok = (result: unknown) => JSON.stringify({ ok: true, result });
        if (method === "tasks.detachPreview") {
          fixture.previews.push(r);
          if (fixture.delay)
            await new Promise<void>((resolve) => {
              fixture.release = resolve;
            });
          return ok({
            version: 1,
            id: id(20),
            hash: "a".repeat(64),
            recordId: id(4),
            pageId: id(3),
            title: "Publish Friday",
            binding: { statusPropertyId: id(6), duePropertyId: null },
            reminders: [
              {
                id: id(40),
                revision: "1",
                configuredEnabled: true,
                eligibleBefore: false,
                eligibleAfter: true,
                description: "daily at 09:00 (UTC)",
              },
            ],
            requiredRevisions: [
              {
                target: { entityType: "page", entityId: id(3) },
                revision: "1",
              },
            ],
            occurrences: query.has("empty")
              ? []
              : [
                  {
                    blockId: id(11),
                    pageId: id(12),
                    pageTitle: "Meeting notes",
                    trashed: false,
                    canRemove: true,
                    removalReason: "",
                    action:
                      r.choices.find((c: any) => c.blockId === id(11))
                        ?.action ?? "convert",
                    text: "Publish Friday — To do; due 2026-10-16 (UTC)",
                  },
                  {
                    blockId: id(13),
                    pageId: id(12),
                    pageTitle: "Nested notes",
                    trashed: false,
                    canRemove: false,
                    removalReason:
                      "Convert this occurrence to preserve its nested blocks.",
                    action: "convert",
                    text: "Publish Friday — To do; due 2026-10-16 (UTC)",
                  },
                ],
          });
        }
        if (method === "batch") {
          fixture.commands.push(r);
          if (query.has("stale"))
            return JSON.stringify({
              ok: false,
              error: {
                code: "CONFLICT",
                message: "Task changed; review again",
              },
            });
          state.taskFacets = [];
          return ok({});
        }
        throw Error(method);
      },
    },
  },
};
function Harness() {
  const [w, setW] = useState<WorkspaceSnapshot>({ ...state });
  const [error, setError] = useState("");
  return (
    <>
      <button onClick={() => setW({ ...w, scopeGrantId: id(99) })}>
        Switch grant
      </button>
      <p role="alert">{error}</p>
      {w.taskFacets.length ? (
        <TaskEditor
          workspace={w}
          recordId={id(4)}
          openPage={() => {}}
          onCommitted={() => setW({ ...state })}
          onError={(e) => setError(String(e))}
        />
      ) : (
        <p>Record retained; no task facet</p>
      )}
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Harness />);
