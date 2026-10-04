import { useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type { Location } from "../../../contracts/generated/domain";
export function Suggestions({
  workspace,
  pageId,
  onNavigate,
}: {
  workspace: WorkspaceSnapshot;
  pageId: string;
  onNavigate: (location: Location) => void;
}) {
  const [result, setResult] = useState<{
      suggestions: {
        kind: string;
        pageId: string;
        title: string;
        sourceRevision: string;
        reason: string;
      }[];
      method: string;
      partial: boolean;
      semanticBacklinks?: unknown;
      semanticDiagnostic?: string;
    } | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  return (
    <details>
      <summary>Related pages and duplicate suggestions</summary>
      <p>
        Review suggestions before linking, merging, moving or tagging pages.
      </p>
      <button
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          setError("");
          try {
            setResult(
              await dispatch("ai.suggestions", {
                ...envelope(workspace),
                pageId,
                semantic: true,
              }),
            );
          } catch (e) {
            setError(String(e));
          } finally {
            setBusy(false);
          }
        }}
      >
        Find suggestions
      </button>
      {error && <p role="alert">{error}</p>}
      {result && (
        <>
          <p>
            {result.method}
            {result.partial ? " · first 1,000 pages" : ""}
          </p>
          {result.suggestions.length === 0 && (
            <p>No strong matches in the current scope.</p>
          )}
          {result.suggestions.map((s) => (
            <article key={s.pageId}>
              <button
                onClick={() =>
                  onNavigate({
                    pageId: s.pageId,
                    sourceRevision: s.sourceRevision,
                  })
                }
              >
                {s.title} · {s.kind}
              </button>
              <p>{s.reason}</p>
            </article>
          ))}
          {result.semanticDiagnostic && <p>{result.semanticDiagnostic}</p>}
          {result.semanticBacklinks != null && (
            <details>
              <summary>Semantic candidates</summary>
              <pre>{JSON.stringify(result.semanticBacklinks, null, 2)}</pre>
            </details>
          )}
        </>
      )}
    </details>
  );
}
