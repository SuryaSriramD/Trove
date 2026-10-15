import { useState, useEffect, useRef } from "react";
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
type Operation = {
  packId: string;
  revision?: number;
  bytes?: number;
  state: string;
  error?: string;
};
type State = {
  operations?: Operation[];
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
  const [progress, setProgress] = useState<Record<string, Operation>>({});
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const mounted = useRef(true);
  const refreshID = useRef(0);
  function acceptProgress(p: Operation) {
    if (!mounted.current || !p?.packId) return;
    setProgress((s) =>
      (s[p.packId]?.revision ?? -1) > (p.revision ?? 0)
        ? s
        : { ...s, [p.packId]: p },
    );
  }
  async function refresh() {
    const request = ++refreshID.current;
    setLoading(true);
    try {
      const next = await dispatch<State>("models.list");
      if (!mounted.current) return;
      next.operations?.forEach(acceptProgress);
      if (request === refreshID.current) setValue(next);
    } catch (e) {
      if (mounted.current && request === refreshID.current) onError(e);
    } finally {
      if (mounted.current && request === refreshID.current) setLoading(false);
    }
  }
  useEffect(() => {
    mounted.current = true;
    const unsubscribe = window.runtime?.EventsOn(
      "models:progress",
      (...args: unknown[]) => {
        const p = args[0] as Operation;
        if (p?.packId) {
          acceptProgress(p);
          if (["ready", "failed", "interrupted"].includes(p.state))
            void refresh();
        }
      },
    );
    void refresh();
    return () => {
      mounted.current = false;
      refreshID.current++;
      unsubscribe?.();
    };
  }, []);
  async function action(method: string, packId: string) {
    setPending((s) => ({ ...s, [packId]: true }));
    try {
      if (method === "import") {
        const pick = window.go?.main.App.PickModelPack;
        if (!pick) throw new Error("Native model import is unavailable.");
        await pick(packId);
      } else {
        const result = await dispatch<any>(`models.${method}`, { packId });
        acceptProgress(result);
      }
      if (mounted.current) await refresh();
    } catch (e) {
      if (mounted.current) onError(e);
    } finally {
      if (mounted.current) setPending((s) => ({ ...s, [packId]: false }));
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
          const cancelling = state === "cancellation_requested";
          const running = [
            "acquiring",
            "importing",
            "cancellation_requested",
          ].includes(state);
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
                  <button
                    disabled={cancelling}
                    onClick={() => void action("cancel", pack.id)}
                  >
                    {cancelling ? "Cancelling…" : "Cancel"}
                  </button>
                ) : state === "ready" ? (
                  <button
                    disabled={pending[pack.id]}
                    onClick={() => void action("remove", pack.id)}
                  >
                    Remove pack
                  </button>
                ) : (
                  <>
                    <button
                      disabled={pending[pack.id]}
                      onClick={() => void action("acquire", pack.id)}
                    >
                      {state === "interrupted" ? "Resume download" : "Download"}
                    </button>
                    <button
                      disabled={pending[pack.id]}
                      onClick={() => void action("import", pack.id)}
                    >
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
