import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import ModelSettings from "../src/ModelSettings";
const w = window as any;
const pack = {
  id: "01990000-0000-7000-8000-000000000105",
  name: "Fixture chat pack",
  kind: "chat",
  format: "gguf",
  license: "MIT",
  licenseUrl: "https://example.com/license",
  sizeBytes: 1048576,
  estimatedMemoryBytes: 2097152,
};
w.modelCalls = [];
w.modelOperations = {};
w.modelInstalled = [];
let listener: ((x: unknown) => void) | undefined;
w.emitModelProgress = (state: string, revision: number) => {
  const event = { version: 1, packId: pack.id, state, revision, bytes: 100 };
  w.modelOperations[pack.id] = event;
  if (state === "ready")
    w.modelInstalled = [
      { pack, state: "ready", downloadedBytes: pack.sizeBytes },
    ];
  listener?.(event);
};
w.deliverStaleProgress = () =>
  listener?.({ version: 1, packId: pack.id, state: "acquiring", revision: 1 });
w.runtime = {
  EventsOn: (_: string, callback: (x: unknown) => void) => {
    listener = callback;
    return () => {
      listener = undefined;
    };
  },
};
w.go = {
  main: {
    App: {
      Dispatch: async (method: string) => {
        w.modelCalls.push(method);
        let result;
        if (method === "models.list")
          result = {
            packs: w.modelInstalled,
            operations: Object.values(w.modelOperations),
            catalog: [pack],
            runtime: { available: true, error: "" },
          };
        else if (method === "models.acquire") {
          const started = {
            version: 1,
            packId: pack.id,
            state: "acquiring",
            revision: 1,
            bytes: 0,
          };
          w.modelOperations[pack.id] = started;
          if (w.finishBeforeAcquireResponse) w.emitModelProgress("ready", 2);
          result = started;
        } else if (method === "models.cancel") {
          w.emitModelProgress("cancellation_requested", 2);
          result = w.modelOperations[pack.id];
        } else throw new Error(`unexpected ${method}`);
        return JSON.stringify({ ok: true, result });
      },
      PickModelPack: async () => {},
    },
  },
};
function Fixture() {
  const [shown, show] = useState(true);
  const [error, setError] = useState("");
  return (
    <>
      <button onClick={() => show(!shown)}>Toggle settings</button>
      {shown && <ModelSettings onError={(e) => setError(String(e))} />}
      {error && <p role="alert">{error}</p>}
    </>
  );
}
createRoot(document.getElementById("root")!).render(<Fixture />);
