import { useState } from "react";
import { createRoot } from "react-dom/client";
import { SkillsPanel } from "../src/ai/SkillsPanel";
import type { WorkspaceSnapshot } from "../src/api";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
let counter = 10;
const state = JSON.parse(localStorage.getItem("skills-fixture") ?? "null") ?? {
  pages: [
    { id: id(2), title: "Selected project", revision: "1", deletedAt: null },
  ],
  skills: [],
  runs: [],
  previewed: false,
  starts: 0,
};
counter = state.nextID ?? 10;
const snapshot = () =>
  ({
    workspaceId: id(1),
    scopeGrantId: id(3),
    snapshotCursor: String(counter),
    pages: state.pages,
    blocks: [],
    collections: [],
    properties: [],
    records: [],
    taskFacets: [],
    containers: [],
    memories: [],
  }) as unknown as WorkspaceSnapshot;
const persist = () => {
  state.nextID = counter;
  localStorage.setItem("skills-fixture", JSON.stringify(state));
};
(window as any).skillFixture = state;
window.runtime = { EventsOn: () => () => {} };
window.go = {
  main: {
    App: {
      PickFile: async () => "",
      Dispatch: async (method, raw) => {
        const r = JSON.parse(raw);
        let result: any = [];
        if (method === "skills.list")
          result = r.skillId
            ? state.skills.filter((v: any) => v.id === r.skillId).reverse()
            : Object.values(
                Object.fromEntries(state.skills.map((v: any) => [v.id, v])),
              );
        if (method === "allocateIds")
          result = {
            ids: Array.from({ length: r.count }, () => id(++counter)),
          };
        if (method === "batch") {
          for (const c of r.commands) {
            if (c.kind === "page.create")
              state.pages.push({
                id: c.pageId,
                title: c.title,
                revision: "1",
                deletedAt: null,
              });
            if (c.kind === "skill.save") {
              const old = state.skills
                .filter((v: any) => v.id === c.skillId)
                .at(-1);
              if ((old?.version ?? 0) !== c.expectedVersion)
                throw new Error("Stale fixture version");
              state.skills.push({
                id: c.skillId,
                pageId: c.pageId,
                version: c.expectedVersion + 1,
                definition: c.definition,
                archived: c.archived,
                imported: c.imported,
                actorId: id(3),
                createdAt: "2026-10-15T12:00:00Z",
              });
            }
          }
          counter++;
          persist();
          result = {
            revisions: [],
            eventCursor: String(counter),
            replayed: false,
          };
        }
        if (method === "skills.preview") {
          const version = state.skills.find(
            (v: any) =>
              v.id === r.skill.skillId && v.version === r.skill.version,
          );
          state.previewed = true;
          result = {
            hash: "a".repeat(64),
            skill: version,
            provider: r.provider,
            prompt: version.definition.instructions + " " + r.skill.input,
            sources: r.skill.sourcePageIds.map((id: string) => ({
              pageId: id,
              title: state.pages.find((p: any) => p.id === id)?.title,
              revision: "1",
            })),
            tools: r.skill.tools,
          };
        }
        if (method === "models.list")
          result = {
            packs: [
              {
                pack: { id: id(7), name: "Fixture model", kind: "chat" },
                state: "ready",
              },
            ],
          };
        if (method === "memory.list") result = { entries: [] };
        if (method === "ai.runs") result = state.runs;
        if (method === "ai.start") {
          if (!state.previewed || !r.skill || r.useMemory)
            throw new Error("Skill scope preview missing");
          state.starts++;
          result = {
            id: id(++counter),
            conversationId: id(++counter),
            state: "running",
            request: r,
            text: "",
            citations: [],
            sources: [],
            createdAt: "2026-10-15T12:00:00Z",
          };
          state.runs.unshift(result);
          persist();
        }
        if (method === "ai.run")
          result = state.runs.find((v: any) => v.id === r.runId);
        if (method === "ai.cancel") {
          state.runs.find((v: any) => v.id === r.runId).state = "cancelled";
          persist();
          result = { state: "cancellation_requested" };
        }
        return JSON.stringify({ ok: true, result });
      },
    },
  },
};
function Fixture() {
  const [w, setW] = useState(snapshot);
  return (
    <SkillsPanel
      workspace={w}
      onCommitted={() => setW(snapshot())}
      onNavigate={() => {}}
    />
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
