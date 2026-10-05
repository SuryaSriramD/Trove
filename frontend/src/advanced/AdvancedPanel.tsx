import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type D, type WorkspaceSnapshot } from "../api";
import { references, type Reference } from "./service";
import { TemplateLibrary } from "./TemplateLibrary";
import { Suggestions } from "../ai/Suggestions";

type Props = {
  workspace: WorkspaceSnapshot;
  page: D.Page;
  flush: () => Promise<boolean>;
  onCommitted: () => Promise<void>;
  onError: (e: unknown) => void;
  openPage: (id: string) => void;
};
export function AdvancedPanel(props: Props) {
  const { workspace, page } = props;
  return (
    <Panel
      key={[
        workspace.workspaceId,
        workspace.scopeGrantId,
        workspace.snapshotCursor,
        page.id,
        page.revision,
      ].join(":")}
      {...props}
    />
  );
}
function Panel({
  workspace: w,
  page,
  flush,
  onCommitted,
  onError,
  openPage,
}: Props) {
  const [refs, setRefs] = useState<Reference[]>([]);
  const [error, setError] = useState("");
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    references(w, page.id)
      .then((result) => {
        if (active.current) setRefs(result.references);
      })
      .catch((e) => {
        if (active.current) setError(String(e));
      });
    return () => {
      active.current = false;
    };
  }, []);
  async function open(reference: Reference) {
    try {
      const result = await dispatch<{ state: string }>("resolveCitation", {
        ...envelope(w),
        location: {
          pageId: reference.pageId,
          sourceRevision: reference.sourceRevision,
        },
      });
      if (!active.current) return;
      if (result.state !== "current") {
        setError(
          `This reference is ${result.state}. Refresh the page before opening it.`,
        );
        return;
      }
      openPage(reference.pageId);
    } catch (e) {
      if (active.current) setError(String(e));
    }
  }
  return (
    <details className="advanced-panel">
      <summary>Templates and related pages</summary>
      <TemplateLibrary
        workspace={w}
        sourcePages={[page]}
        flush={flush}
        onCommitted={onCommitted}
        onError={onError}
        openPage={openPage}
      />
      <Suggestions
        workspace={w}
        pageId={page.id}
        onNavigate={(location) => openPage(location.pageId)}
      />
      <section>
        <h3>Unlinked mentions and shared words</h3>
        <p>These lexical references match page titles or shared terms.</p>
        {error && <p role="alert">{error}</p>}
        {refs.map((reference) => (
          <p key={reference.pageId}>
            <button onClick={() => open(reference)}>{reference.title}</button>{" "}
            <small>
              {reference.kind === "unlinked"
                ? "Unlinked title mention"
                : "Shared-word heuristic"}{" "}
              · {reference.reason}
            </small>
          </p>
        ))}
        {!refs.length && (
          <p>No lexical references found in the current scope.</p>
        )}
      </section>
    </details>
  );
}
