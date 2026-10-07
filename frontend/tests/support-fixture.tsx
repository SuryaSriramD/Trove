import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import SupportBundlePanel from "../src/SupportBundlePanel";
import "../src/style.css";
const calls: any[] = [];
(window as any).supportCalls = calls;
let release: ((value: string) => void) | null = null;
window.go = {
  main: {
    App: {
      Dispatch: async () => "{}",
      PickFile: async () => "",
      PreviewSupportBundle: async (raw: string) => {
        const request = JSON.parse(raw);
        calls.push({ method: "preview", request });
        const response = JSON.stringify({
          previewId: "opaque-review",
          workspaceId: request.workspaceId,
          expiresAt: "2099-01-01T00:00:00Z",
          files: [
            { name: "metadata.json", content: '{"application":"Trove"}' },
            ...(request.selection.diagnostics
              ? [
                  {
                    name: "diagnostics.json",
                    content: '{"version":1,"events":[{"component":"search"}]}',
                  },
                ]
              : []),
            ...(request.selection.text || request.selection.titles
              ? [
                  {
                    name: "selected-content.json",
                    content: '"explicitly consented secret"',
                  },
                ]
              : []),
          ],
        });
        if ((window as any).delaySupportPreview)
          return new Promise<string>((resolve) => {
            (window as any).completeSupportPreview = () => resolve(response);
          });
        return response;
      },
      ExportSupportBundle: async (id: string) => {
        calls.push({ method: "export", id });
        if ((window as any).supportStale)
          throw new Error(
            "Selected content changed; review a new support bundle",
          );
        if ((window as any).supportDelay)
          return new Promise<string>((resolve) => {
            release = resolve;
          });
        return JSON.stringify({
          message: "Support bundle saved locally. Nothing was uploaded.",
        });
      },
      CancelSupportBundle: async () => {
        calls.push({ method: "cancel" });
        release?.("");
        release = null;
      },
    } as any,
  },
};
function Fixture() {
  const [id, setID] = useState("workspace-current"),
    [error, setError] = useState("");
  return (
    <main>
      <button onClick={() => setID("workspace-other")}>
        Fixture change workspace
      </button>
      <SupportBundlePanel
        workspaceId={id}
        pages={[
          { id: "page-one", title: "First" },
          { id: "page-two", title: "Second" },
        ]}
        onError={(e) => setError(String(e))}
      />
      <p role="alert">{error}</p>
    </main>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
