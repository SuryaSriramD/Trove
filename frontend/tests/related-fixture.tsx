import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { AdvancedPanel } from "../src/advanced/AdvancedPanel";
import "../src/style.css";
import "../src/editor/advanced.css";
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
const state = {
  calls: [] as any[],
  navigations: [] as string[],
  mode: "semantic",
  resolution: "current",
  deferSuggestions: false,
  deferResolution: false,
  releaseSuggestions: undefined as (() => void) | undefined,
  releaseResolution: undefined as (() => void) | undefined,
};
(window as any).relatedFixture = state;
const hit = (page: number, title: string, chunk: string) => ({
  chunkId: chunk,
  title,
  kind: "page",
  excerpt: "Daily exercise supports canine wellbeing.",
  location: { pageId: id(page), sourceRevision: "7" },
  sourceState: "current",
  contentHash: "fixture",
  score: 0.9,
});
(window as any).go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw);
        state.calls.push({ method, request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        if (method === "editor.templates") return ok({ templates: [] });
        if (method === "editor.references")
          return ok({
            references: [
              {
                pageId: id(7),
                title: "Notebook title mention",
                sourceRevision: "3",
                kind: "unlinked",
                reason: "Page title occurs in visible text",
              },
            ],
          });
        if (method === "ai.suggestions") {
          const result = {
            sourceRevision: "2",
            method: "deterministic vocabulary and exact duplicate comparison",
            partial: true,
            omissions: [
              "Vocabulary comparison covers the first 1,000 accessible pages.",
            ],
            suggestions: [
              {
                pageId: id(4),
                title: "Canine exercise",
                sourceRevision: "7",
                kind: "related",
                reason: "Shared vocabulary",
              },
              {
                pageId: id(6),
                title: "Possible copied note",
                sourceRevision: "8",
                kind: "duplicate",
                reason: "Matching normalized indexed text",
              },
            ],
            semanticBacklinks: {
              mode: state.mode,
              partial: false,
              warning:
                state.mode === "keyword"
                  ? "Semantic related content is disabled. Showing keyword matches."
                  : "",
              hits: [
                hit(3, "Current notebook", "self"),
                hit(4, "Canine exercise", "first"),
                hit(4, "Canine exercise", "duplicate"),
              ],
            },
          };
          if (state.deferSuggestions)
            await new Promise<void>((resolve) => {
              state.releaseSuggestions = resolve;
            });
          return ok(result);
        }
        if (method === "resolveCitation") {
          const resolution = state.resolution;
          if (state.deferResolution)
            await new Promise<void>((resolve) => {
              state.releaseResolution = resolve;
            });
          return ok({
            state: resolution,
            location: request.location,
            currentRevision: "7",
          });
        }
        return ok({});
      },
    },
  },
};
function Fixture() {
  const [context, setContext] = useState({
    workspace: 1,
    grant: 2,
    page: 3,
    revision: "2",
    cursor: "10",
  });
  const page: any = {
    id: id(context.page),
    title: "Notebook",
    revision: context.revision,
  };
  const workspace: any = {
    workspaceId: id(context.workspace),
    scopeGrantId: id(context.grant),
    snapshotCursor: context.cursor,
    pages: [page],
    blocks: [],
    collections: [],
    properties: [],
    records: [],
    views: [],
    containers: [],
    files: [],
    memories: [],
    taskFacets: [],
  };
  return (
    <>
      <button onClick={() => setContext((c) => ({ ...c, page: 9 }))}>
        Switch page
      </button>
      <button onClick={() => setContext((c) => ({ ...c, workspace: 10 }))}>
        Switch workspace
      </button>
      <button onClick={() => setContext((c) => ({ ...c, grant: 11 }))}>
        Change grant
      </button>
      <button
        onClick={() =>
          setContext((c) => ({ ...c, revision: "3", cursor: "11" }))
        }
      >
        Edit source
      </button>
      <AdvancedPanel
        workspace={workspace}
        page={page}
        flush={async () => true}
        onCommitted={async () => {}}
        onError={() => {}}
        openPage={(page) => {
          state.navigations.push(page);
        }}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
