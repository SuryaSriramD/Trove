import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type { Location } from "../../../contracts/generated/domain";
import type {
  ResolveCitationResult,
  SearchHit,
} from "../../../contracts/generated/service";
import "./suggestions.css";

type RelatedHit = SearchHit & { title: string; chunkId: string; kind: string };
type LexicalSuggestion = {
  kind: string;
  pageId: string;
  title: string;
  sourceRevision: string;
  reason: string;
};
type SuggestionResult = {
  suggestions: LexicalSuggestion[];
  method: string;
  partial: boolean;
  sourceRevision: string;
  omissions?: string[];
  semanticBacklinks?: {
    hits: RelatedHit[];
    mode: string;
    partial: boolean;
    warning?: string;
  };
};
type Props = {
  workspace: WorkspaceSnapshot;
  pageId: string;
  onNavigate: (location: Location) => void;
};

// Changing the workspace, grant, source revision or canonical snapshot destroys
// pending requests and the displayed evidence together, before a new render.
export function Suggestions(props: Props) {
  const { workspace, pageId } = props;
  const revision =
    workspace.pages.find((page) => page.id === pageId)?.revision ?? "";
  return (
    <RelatedSuggestions
      key={[
        workspace.workspaceId,
        workspace.scopeGrantId,
        workspace.snapshotCursor,
        pageId,
        revision,
      ].join(":")}
      {...props}
    />
  );
}
function RelatedSuggestions({ workspace, pageId, onNavigate }: Props) {
  const [result, setResult] = useState<SuggestionResult | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const active = useRef(true);
  const sequence = useRef(0);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      ++sequence.current;
    };
  }, []);
  async function find() {
    const request = ++sequence.current;
    setBusy(true);
    setError("");
    setResult(null);
    try {
      const next = await dispatch<SuggestionResult>("ai.suggestions", {
        ...envelope(workspace),
        pageId,
        semantic: true,
      });
      if (active.current && sequence.current === request) setResult(next);
    } catch (e) {
      if (active.current && sequence.current === request) setError(String(e));
    } finally {
      if (active.current && sequence.current === request) setBusy(false);
    }
  }
  async function open(location: Location) {
    const request = sequence.current;
    setError("");
    try {
      const resolved = await dispatch<ResolveCitationResult>(
        "resolveCitation",
        { ...envelope(workspace), location },
      );
      if (!active.current || sequence.current !== request) return;
      if (resolved.state !== "current") {
        setError(
          `This source is ${resolved.state}. Find related content again before opening it.`,
        );
        return;
      }
      onNavigate(location);
    } catch (e) {
      if (active.current && sequence.current === request) setError(String(e));
    }
  }
  const retrieval = result?.semanticBacklinks;
  const semantic = retrieval?.mode === "semantic";
  const seen = new Set<string>([pageId]);
  const hits = (retrieval?.hits ?? [])
    .filter((hit) => {
      if (seen.has(hit.location.pageId)) return false;
      seen.add(hit.location.pageId);
      return true;
    })
    .slice(0, 20);
  const lexical = (result?.suggestions ?? []).filter((suggestion) => {
    if (seen.has(suggestion.pageId)) return false;
    seen.add(suggestion.pageId);
    return true;
  });
  const lexicalLabel = (suggestion: LexicalSuggestion) =>
    suggestion.kind === "duplicate"
      ? "Possible duplicate · matching normalized indexed text"
      : "Vocabulary overlap · lexical heuristic";
  return (
    <details className="related-suggestions">
      <summary>Related content and possible duplicates</summary>
      <p>
        Search saved page text using the enabled local embedding model. Keyword
        matches are shown when semantic search is unavailable. Suggestions do
        not create links or change pages.
      </p>
      <button disabled={busy} onClick={find}>
        Find related content
      </button>
      {busy && <p role="status">Finding related pages…</p>}
      {error && <p role="alert">{error}</p>}
      {result && (
        <>
          <p className="related-source">
            Based on this page at revision {result.sourceRevision}.
          </p>
          {retrieval && (
            <section
              aria-label={
                semantic ? "Semantic related pages" : "Keyword related pages"
              }
            >
              <h3>
                {semantic
                  ? "Semantic related pages"
                  : "Keyword matches · semantic unavailable"}
              </h3>
              <p>
                {semantic
                  ? "Similarity candidates from local embeddings. Review the source excerpt before deciding whether pages are related."
                  : "These matches use indexed words, without a semantic interpretation."}
              </p>
              {retrieval.warning && <p role="status">{retrieval.warning}</p>}
              {retrieval.partial && (
                <p>Retrieval is partial; some sources may be omitted.</p>
              )}
              {hits.map((hit) => {
                const overlap = result.suggestions.find(
                  (suggestion) => suggestion.pageId === hit.location.pageId,
                );
                return (
                  <article
                    className="related-card"
                    key={hit.location.pageId}
                    aria-label={`${semantic ? "Semantic" : "Keyword"} page: ${hit.title || "Untitled"}`}
                  >
                    <h4>
                      <button onClick={() => open(hit.location)}>
                        {hit.title || "Untitled"}
                      </button>
                    </h4>
                    <p className="related-source">
                      {semantic ? "Semantic candidate" : "Keyword match"} ·{" "}
                      {hit.kind} · Revision {hit.location.sourceRevision}
                    </p>
                    <blockquote>
                      {hit.excerpt || "No excerpt available."}
                    </blockquote>
                    {overlap && (
                      <p>
                        {lexicalLabel(overlap)}. {overlap.reason}
                      </p>
                    )}
                  </article>
                );
              })}
              {hits.length === 0 && (
                <p>No current related sources were found in this scope.</p>
              )}
            </section>
          )}
          <section aria-label="Vocabulary and duplicate checks">
            <h3>Vocabulary and duplicate checks</h3>
            <p>
              Word overlap and normalized text comparison are separate from
              semantic similarity.
            </p>
            {lexical.map((suggestion) => (
              <article className="related-card" key={suggestion.pageId}>
                <h4>
                  <button
                    onClick={() =>
                      open({
                        pageId: suggestion.pageId,
                        sourceRevision: suggestion.sourceRevision,
                      })
                    }
                  >
                    {suggestion.title || "Untitled"}
                  </button>
                </h4>
                <p className="related-source">
                  {lexicalLabel(suggestion)} · Revision{" "}
                  {suggestion.sourceRevision}
                </p>
                <p>{suggestion.reason}</p>
              </article>
            ))}
            {lexical.length === 0 && (
              <p>No additional vocabulary or duplicate candidates.</p>
            )}
          </section>
          {!!result.omissions?.length && (
            <ul aria-label="Search omissions">
              {result.omissions.map((omission) => (
                <li key={omission}>{omission}</li>
              ))}
            </ul>
          )}
        </>
      )}
    </details>
  );
}
