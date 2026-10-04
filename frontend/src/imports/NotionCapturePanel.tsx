import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type {
  NotionCaptureResult as Capture,
  NotionRoot,
} from "../../../contracts/generated/service";
export function NotionCapturePanel({
  workspace,
  namespace,
  onPlan,
}: {
  workspace: WorkspaceSnapshot;
  namespace: string;
  onPlan: (plan: unknown) => void;
}) {
  const [rootKind, setRootKind] = useState<NotionRoot["kind"]>("page");
  const [rootIds, setRootIds] = useState(""),
    [token, setToken] = useState(""),
    [capture, setCapture] = useState<Capture>(),
    [recent, setRecent] = useState<Capture[]>([]),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const pause = useRef(false);
  const request = <T,>(method: string, data: object = {}) =>
    dispatch<T>(method, { ...envelope(workspace), ...data });
  const refresh = () =>
    request<Capture[]>("imports.notion.list").then(setRecent);
  useEffect(() => {
    void refresh().catch((e) => setError(String(e)));
    return () => {
      pause.current = true;
    };
  }, [workspace.workspaceId]);
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
      while (
        !pause.current &&
        current.state !== "ready" &&
        current.state !== "finished"
      ) {
        current = await request<Capture>("imports.notion.step", {
          captureId: current.captureId,
          revision: current.revision,
          token,
        });
        setCapture(current);
        if (current.state === "blocked") break;
      }
      if (current.state === "ready") setToken("");
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
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
      setError(String(e));
    } finally {
      setBusy(false);
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
