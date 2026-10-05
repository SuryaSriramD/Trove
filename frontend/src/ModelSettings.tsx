import { useState, useEffect } from "react";
import { dispatch } from "./api";
type Pack = {
  id: string;
  name: string;
  kind: string;
  format: string;
  license: string;
  licenseUrl: string;
  sizeBytes: number;
  projector?: { sizeBytes: number };
  estimatedMemoryBytes: number;
  qualificationOnly?: boolean;
};
type State = {
  packs: { pack: Pack; state: string; downloadedBytes: number }[];
  catalog: Pack[];
  runtime: { available: boolean; visionAvailable?: boolean; error: string };
};
const size = (n: number) =>
  `${(n / 1024 / 1024).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
export default function ModelSettings({
  onError,
}: {
  onError: (e: unknown) => void;
}) {
  const [value, setValue] = useState<State | null>(null);
  const [loading, setLoading] = useState(true);
  const [progress, setProgress] = useState<
    Record<string, { bytes?: number; state: string; error?: string }>
  >({});
  async function refresh() {
    setLoading(true);
    try {
      setValue(await dispatch<State>("models.list"));
    } catch (e) {
      onError(e);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void refresh();
    return window.runtime?.EventsOn("models:progress", (...args: unknown[]) => {
      const p = args[0] as any;
      if (p?.packId) {
        setProgress((s) => ({ ...s, [p.packId]: p }));
        if (["ready", "failed", "interrupted"].includes(p.state))
          void refresh();
      }
    });
  }, []);
  async function action(method: string, packId: string) {
    try {
      if (method === "import") {
        await window.go?.main.App.PickModelPack?.(packId);
      } else {
        const result = await dispatch<any>(`models.${method}`, { packId });
        setProgress((s) => ({ ...s, [packId]: result }));
      }
      await refresh();
    } catch (e) {
      onError(e);
    }
  }
  return (
    <div className="settings-section">
      <h2>Local models</h2>
      <p>
        {value
          ? value.runtime.available
            ? "Native runtimes are ready on this Mac."
            : "Native runtime is unavailable."
          : loading
            ? "Checking local runtimes…"
            : "Could not check local runtimes."}{" "}
        {value?.runtime.error}
      </p>
      <p>
        Chat, embeddings, speech, and image understanding have separate packs.
        Each file is verified against its pinned hash and runtime before it can
        load.
      </p>
      {value?.catalog
        .filter((pack) => !pack.qualificationOnly)
        .map((pack) => {
          const installed = value.packs.find((x) => x.pack.id === pack.id);
          const p = progress[pack.id];
          const state = p?.state ?? installed?.state ?? "not installed";
          const running = ["acquiring", "importing"].includes(state);
          return (
            <div className="model-pack" key={pack.id}>
              <div>
                <span className="eyebrow">
                  {pack.kind} · {pack.format}
                </span>
                <h3>{pack.name}</h3>
                <p>
                  {size(pack.sizeBytes + (pack.projector?.sizeBytes ?? 0))}{" "}
                  download · {size(pack.estimatedMemoryBytes)} memory ·{" "}
                  <a href={pack.licenseUrl} target="_blank" rel="noreferrer">
                    {pack.license}
                  </a>
                </p>
                {pack.qualificationOnly && (
                  <p>
                    Runtime qualification pack. Not intended for useful model
                    output.
                  </p>
                )}
                {pack.projector && (
                  <p>
                    Two pinned files are required. English image descriptions
                    are experimental; verify every answer against the original.{" "}
                    {value.runtime.visionAvailable
                      ? "Local image helper ready."
                      : "Compatible image helper unavailable."}
                  </p>
                )}
                <small role="status">
                  {state}
                  {p?.bytes !== undefined ? ` · ${size(p.bytes)}` : ""}
                  {p?.error ? ` · ${p.error}` : ""}
                </small>
                {running && (
                  <progress
                    max={pack.sizeBytes + (pack.projector?.sizeBytes ?? 0)}
                    value={p?.bytes ?? installed?.downloadedBytes ?? 0}
                  />
                )}
              </div>
              <div className="model-actions">
                {running ? (
                  <button onClick={() => void action("cancel", pack.id)}>
                    Cancel
                  </button>
                ) : state === "ready" ? (
                  <button onClick={() => void action("remove", pack.id)}>
                    Remove pack
                  </button>
                ) : (
                  <>
                    <button onClick={() => void action("acquire", pack.id)}>
                      {state === "interrupted" ? "Resume download" : "Download"}
                    </button>
                    <button onClick={() => void action("import", pack.id)}>
                      {pack.projector
                        ? "Import model and projector"
                        : "Import file"}
                    </button>
                  </>
                )}
              </div>
            </div>
          );
        })}
      {value?.catalog.length === 0 && (
        <p>No compatible packs are included in this runtime catalogue.</p>
      )}
    </div>
  );
}
