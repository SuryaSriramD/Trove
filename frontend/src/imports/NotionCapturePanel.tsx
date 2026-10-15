import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type {
  NotionCaptureResult as Capture,
  NotionRoot,
} from "../../../contracts/generated/service";
type Props = {
  workspace: WorkspaceSnapshot;
  namespace: string;
  initialRoots?: NotionRoot[];
  onPlan: (plan: unknown) => void;
};

export function NotionCapturePanel(props: Props) {
  return (
    <CaptureSession
      key={`${props.workspace.workspaceId}:${props.workspace.scopeGrantId}:${props.namespace}:${JSON.stringify(props.initialRoots ?? [])}`}
      {...props}
    />
  );
}

function CaptureSession({
  workspace,
  namespace,
  initialRoots = [],
  onPlan,
}: Props) {
  const [rootKind, setRootKind] = useState<NotionRoot["kind"]>(
    initialRoots[0]?.kind ?? "page",
  );
  const [rootIds, setRootIds] = useState(
      initialRoots.map((root) => root.id).join(", "),
    ),
    [token, setToken] = useState(""),
    [capture, setCapture] = useState<Capture>(),
    [recent, setRecent] = useState<Capture[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const pause = useRef(false);
  const active = useRef(true);
  const request = async <T,>(method: string, data: object = {}) => {
    if (!active.current) throw new Error("Capture context changed");
    const result = await dispatch<T>(method, {
      ...envelope(workspace),
      ...data,
    });
    if (!active.current) throw new Error("Capture context changed");
    return result;
  };
  const refresh = () =>
    request<Capture[]>("imports.notion.list").then(setRecent);
  useEffect(() => {
    active.current = true;
    void refresh().catch((e) => {
      if (active.current) setError(String(e));
    });
    return () => {
      active.current = false;
      pause.current = true;
    };
  }, []);
  const run = async (start: boolean) => {
    setBusy(true);
    setError("");
    pause.current = false;
    try {
      let current = start
        ? await request<Capture>("imports.notion.start", {
            roots: rootIds
              .split(/[\s,]+/)
              .filter(Boolean)
              .map((id) => ({ kind: rootKind, id })),
            sourceNamespace: namespace,
          })
        : capture;
      if (!current) return;
      setCapture(current);
      let batches = 0;
      while (
        !pause.current &&
        current.state !== "ready" &&
        current.state !== "finished"
      ) {
        const next: Capture = await request<Capture>("imports.notion.step", {
          captureId: current.captureId,
          revision: current.revision,
          token,
        });
        if (
          next.captureId !== current.captureId ||
          next.revision <= current.revision
        ) {
          throw new Error(
            "Capture made no progress. Saved responses are retained; refresh or resume before trying again.",
          );
        }
        current = next;
        setCapture(current);
        if (current.state === "blocked") break;
        if (
          ++batches >= 500 &&
          current.state !== "ready" &&
          current.state !== "finished"
        ) {
          setError(
            "Capture paused after 500 batches. Resume to continue from the saved cursor.",
          );
          break;
        }
      }
      if (current.state === "ready") setToken("");
      await refresh();
    } catch (e) {
      if (active.current) setError(String(e));
    } finally {
      if (active.current) setBusy(false);
    }
  };
  const finish = async (partial: boolean) => {
    if (!capture) return;
    setBusy(true);
    setError("");
    try {
      onPlan(
        await request("imports.notion.finish", {
          captureId: capture.captureId,
          revision: capture.revision,
          allowPartial: partial,
        }),
      );
      setToken("");
      setCapture(
        await request<Capture>("imports.notion.status", {
          captureId: capture.captureId,
        }),
      );
      await refresh();
    } catch (e) {
      if (active.current) setError(String(e));
    } finally {
      if (active.current) setBusy(false);
    }
  };
  return (
    <details>
      <summary>Optional authenticated Notion capture</summary>
      <p>
        Choose explicit page, database container or data source roots shared
        with your integration. Capture fetches their descendants in resumable
        batches. The token stays in memory and is sent only to Notion; enter it
        again after restarting. Live account qualification is unverified.
      </p>
      <p>
        Supported schemas, records, linked views, complete property values and
        downloaded files work offline after import. Unsupported settings and
        calculations remain source evidence. Re-import keeps an existing
        connected database graph or creates a reviewed copy.
      </p>
      <div className="import-controls">
        <label>
          Root type
          <select
            value={rootKind}
            disabled={busy}
            onChange={(e) => setRootKind(e.target.value as NotionRoot["kind"])}
          >
            <option value="page">Page and descendants</option>
            <option value="database">Database container and its sources</option>
            <option value="data_source">One data source</option>
          </select>
        </label>
        <label>
          Authorized root IDs
          <textarea
            value={rootIds}
            disabled={busy}
            onChange={(e) => setRootIds(e.target.value)}
            placeholder="Notion UUIDs, separated by commas"
            rows={2}
          />
        </label>
        <label>
          Integration token
          <input
            type="password"
            autoComplete="off"
            value={token}
            disabled={busy}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
        <button
          disabled={
            busy || !rootIds.trim() || token.length < 8 || !namespace.trim()
          }
          onClick={() => run(true)}
        >
          Capture selected roots
        </button>
      </div>
      {recent.length > 0 && (
        <label>
          Resume saved capture
          <select
            value={capture?.captureId ?? ""}
            disabled={busy}
            onChange={(e) =>
              setCapture(recent.find((c) => c.captureId === e.target.value))
            }
          >
            <option value="">Choose capture…</option>
            {recent.map((c) => (
              <option key={c.captureId} value={c.captureId}>
                {c.captureId.slice(-8)} · {c.state} · {c.completed} responses
              </option>
            ))}
          </select>
        </label>
      )}
      {capture && (
        <div>
          <p>
            {capture.state} · API {capture.apiVersion} · {capture.completed}{" "}
            responses retained · {capture.pending.length} pending batches
          </p>
          {capture.pending[0] && (
            <p>
              Next: {capture.pending[0].kind}
              {capture.pending[0].propertyId
                ? ` · property ${capture.pending[0].propertyId}`
                : ""}
              {capture.pending[0].cursor ? " · continuing saved cursor" : ""}
            </p>
          )}
          {capture.error && <p role="alert">{capture.error}</p>}
          {busy ? (
            <button
              onClick={() => {
                pause.current = true;
              }}
            >
              Pause after current fetch
            </button>
          ) : (
            capture.state !== "ready" &&
            capture.state !== "finished" && (
              <button disabled={token.length < 8} onClick={() => run(false)}>
                Resume capture
              </button>
            )
          )}
          {capture.state === "ready" && (
            <button disabled={busy} onClick={() => finish(false)}>
              Review complete capture inventory
            </button>
          )}
          {capture.state !== "ready" && capture.state !== "finished" && (
            <button disabled={busy} onClick={() => finish(true)}>
              Finish with explicit partial outcomes
            </button>
          )}
        </div>
      )}
      {error && (
        <p role="alert" className="import-error">
          {error}
        </p>
      )}
    </details>
  );
}
