import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { P2IntelligencePanel } from "../src/ai/P2IntelligencePanel";
const w = window as any;
const id = (n: number) =>
  `019a1234-5678-7000-8000-${String(n).padStart(12, "0")}`;
w.p2Calls = [];
w.scope = "first";
w.hold = "";
w.sequence = 0;
w.go = {
  main: {
    App: {
      Dispatch: async (method: string, raw: string) => {
        const request = JSON.parse(raw);
        w.p2Calls.push({ method, request });
        const ok = (result: any) => JSON.stringify({ ok: true, result });
        let result: any;
        if (method === "ocr.status")
          result = { available: true, engine: "Fixture OCR" };
        else if (method === "review.list")
          result = {
            plans: [],
            runs: [
              {
                id: id(80),
                pageId: id(2),
                planBlockId: id(90),
                status: "pending",
                text: `Review ${w.scope} ${w.sequence}`,
                sources: [],
                createdAt: "2026-10-15T00:00:00Z",
              },
            ],
            engine: "Local",
            scheduling: "While running",
          };
        else if (method === "graph.query")
          result = {
            nodes: [
              {
                id: request.pageId,
                title: `Graph ${w.scope} ${request.pageId === id(2) ? "Alpha" : "Beta"}`,
                revision: "1",
              },
            ],
            edges: [],
            truncated: false,
          };
        else if (method === "models.list")
          result = {
            runtime: { visionAvailable: true },
            packs: [
              {
                state: "ready",
                pack: { kind: "vision", id: id(107), name: "Fixture vision" },
              },
            ],
          };
        else if (method === "image.list")
          result = {
            images: [
              {
                id: id(70),
                originalName:
                  request.pageId === id(2) ? "Alpha.png" : "Beta.png",
              },
            ],
            runs: [
              {
                id: id(80),
                pageId: request.pageId,
                fileId: id(70),
                state: "ready",
                question: `Draft ${w.scope} ${request.pageId === id(2) ? "Alpha" : "Beta"}`,
                text: "Fixture answer",
                warning: "Fixture",
                stale: false,
              },
            ],
            truncated: false,
            imagesTruncated: false,
          };
        else if (method === "ocr.run")
          return new Promise((resolve) => {
            w.ocrStarted = request.requestId;
            w.finishSavedOCR = () =>
              resolve(
                ok({
                  fileId: request.fileId,
                  text: "Saved OCR replacement",
                  engine: "Fixture",
                  acceptedLines: 1,
                  rejectedLines: 0,
                  persisted: true,
                  indexingState: "pending",
                  warning:
                    "Extracted text was saved. Search indexing is pending.",
                }),
              );
            w.finishOCR = () =>
              resolve(
                ok({
                  fileId: request.fileId,
                  text: "Prior OCR result",
                  engine: "Fixture",
                  acceptedLines: 1,
                  rejectedLines: 0,
                }),
              );
            w.cancelOCR = () =>
              resolve(
                JSON.stringify({
                  ok: false,
                  error: { code: "CANCELLED", message: "OCR cancelled" },
                }),
              );
          });
        else if (method === "ocr.cancel") {
          if (request.runRequestId !== w.ocrStarted)
            throw Error("Wrong cancellation identity");
          if (w.holdOCRCancellation)
            return ok({ state: "cancellation_requested" });
          if (w.ocrSaved) w.finishSavedOCR();
          else w.cancelOCR();
          return ok({ state: "cancellation_requested" });
        } else throw Error(`Unexpected ${method}`);
        const response = ok(result);
        if (w.hold === method) {
          w.hold = "";
          return new Promise((resolve) => {
            w.release = () => resolve(response);
          });
        }
        return response;
      },
    },
  },
};
function Fixture() {
  const [workspace, setWorkspace] = useState(1),
    [grant, setGrant] = useState(3),
    [cursor, setCursor] = useState(1),
    [error, setError] = useState("");
  const state: any = {
    workspaceId: id(workspace),
    scopeGrantId: id(grant),
    snapshotCursor: String(cursor),
    pages: [2, 5].map((n) => ({
      id: id(n),
      title: n === 2 ? "Alpha" : "Beta",
      revision: String(cursor),
      workspaceId: id(workspace),
      deletedAt: null,
    })),
    files: [{ id: id(70), originalName: "Input.png", mediaType: "image/png" }],
    blocks: [],
    collections: [],
    properties: [],
    records: [],
    views: [],
    meetings: [],
    taskFacets: [],
    memories: [],
  };
  return (
    <>
      <button
        onClick={() => {
          w.scope = "second";
          setWorkspace(4);
        }}
      >
        Switch workspace
      </button>
      <button
        onClick={() => {
          w.scope = "second";
          setGrant(6);
        }}
      >
        Change grant
      </button>
      <button
        onClick={() => {
          w.sequence++;
          setCursor((n) => n + 1);
        }}
      >
        Advance source
      </button>
      {error && <p role="alert">{error}</p>}
      <P2IntelligencePanel
        workspace={state}
        refresh={async () => setCursor((n) => n + 1)}
        onNavigate={() => {}}
        onError={(e) => setError(String(e))}
      />
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
