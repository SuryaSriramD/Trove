import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  type S,
  type WorkspaceSnapshot,
} from "./api";

export function OperationHistory({
  workspace,
  pageId,
  onChange,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  pageId: string;
  onChange: () => Promise<void>;
  onError: (error: unknown) => void;
}) {
  const [operations, setOperations] = useState<
    S.OperationsListResult["operations"]
  >([]);
  const [preview, setPreview] = useState<S.OperationPreviewUndoResult>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    setPreview(undefined);
    dispatch<S.OperationsListResult>("operations.list", {
      ...envelope(workspace),
      pageId,
    })
      .then((r) => {
        if (active) {
          setOperations(r.operations ?? []);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(String(e));
      });
    return () => {
      active = false;
    };
  }, [workspace.workspaceId, workspace.snapshotCursor, pageId]);
  async function inspect(operationId: string) {
    setBusy(true);
    try {
      setPreview(
        await dispatch<S.OperationPreviewUndoResult>("operations.previewUndo", {
          ...envelope(workspace),
          operationId,
        }),
      );
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function undo() {
    if (!preview) return;
    setBusy(true);
    try {
      await batch(workspace, [preview.command], preview.expectedRevisions);
      setPreview(undefined);
      await onChange();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  }
  if (!operations.length && !error) return null;
  function describe(action: S.OperationPreviewUndoResult["actions"][number]) {
    if (action.kind === "collection.trash.set") return `${action.trashed ? "Hide collection in Trash" : "Restore collection visibility"} (schema, values and identities retained)`;
    if (action.kind === "page.trash.set") return `${action.trashed ? "Move page to Trash" : "Restore page"} (content, values and identities preserved)`;
    if (action.kind === "record.trash.set") {
      const record = workspace.records.find((r) => r.id === action.recordId);
      const title = workspace.pages.find((p) => p.id === record?.pageId)?.title ?? action.recordId;
      return `${action.trashed ? "Move to trash" : "Restore from trash"}: ${title}. Record values, content and attachments retain their identities.`;
    }
    if (action.kind === "collection.property.restore") {
      const collection = workspace.collections.find((c) => c.id === action.collectionId)?.title ?? "Collection";
      if (!action.definition) {
        const name = workspace.properties.find((p) => p.id === action.propertyId)?.name ?? action.propertyId;
        return `${collection}: remove property “${name}”.`;
      }
      return `${collection}: restore property “${action.definition.name}” as ${action.definition.type}, with its saved configuration ${JSON.stringify(action.definition.config)} and ${action.values?.length ?? 0} populated row values: ${JSON.stringify(action.values ?? [])}.`;
    }
    if (
      action.kind === "collection.layout.set" ||
      action.kind === "record.layout.set"
    ) {
      const title =
        action.kind === "collection.layout.set"
          ? (workspace.collections.find((c) => c.id === action.collectionId)
              ?.title ?? "Collection")
          : (workspace.pages.find(
              (p) =>
                p.id ===
                workspace.records.find((r) => r.id === action.recordId)?.pageId,
            )?.title ?? "Record");
      if (!action.layout)
        return `${title}: ${action.kind === "collection.layout.set" ? "restore standard record layout" : "use collection layout"}`;
      const name = (id: string) =>
        workspace.properties.find((p) => p.id === id)?.name ??
        `unavailable property ${id}`;
      const sections = action.layout.sections
        .map(
          (s) =>
            `${s.title || "Untitled section"} (${s.placement}): ${s.propertyIds.map(name).join(", ") || "empty"}`,
        )
        .join("; ");
      return `${title}: ${sections || "no custom sections"}; hidden: ${action.layout.hiddenPropertyIds.map(name).join(", ") || "none"}. Unplaced properties remain visible.`;
    }
    const pageId =
      "pageId" in action
        ? action.pageId
        : workspace.records.find((r) => r.id === action.recordId)?.pageId;
    const title = workspace.pages.find((p) => p.id === pageId)?.title || "Page";
    if (action.kind === "page.update")
      return `${title}: restore title to “${action.title}”`;
    if (action.kind === "page.display.set")
      return `${title}: ${action.fullWidth ? "full" : "standard"} width, presentation ${action.presentation ? "on" : "off"}`;
    const property =
      workspace.properties.find((p) => p.id === action.propertyId)?.name ||
      "Property";
    if (action.kind === "record.clearValue")
      return `${title}: clear ${property}`;
    const value = action.value.value;
    let text = String(value ?? "empty");
    if (value && typeof value === "object") {
      const object = value as Record<string, unknown>;
      if ("start" in object)
        text = String(object.start) + (object.end ? ` to ${object.end}` : "");
      else if ("decimal" in object) text = String(object.decimal);
      else if (Array.isArray(value))
        text = value
          .map((v) =>
            typeof v === "object" && v && "text" in v
              ? String(v.text)
              : String(v),
          )
          .join("");
      else text = "the previous saved value";
    }
    return `${title}: restore ${property} to ${text}`;
  }
  return (
    <details>
      <summary>Grouped changes and undo</summary>
      {error && <p role="alert">{error}</p>}
      <ul>
        {operations.map((o) => (
          <li key={o.id}>
            {o.label} · {new Date(o.createdAt).toLocaleString()}{" "}
            {o.state === "undone" ? (
              "· Undone"
            ) : (
              <button disabled={busy} onClick={() => void inspect(o.id)}>
                Preview undo
              </button>
            )}
          </li>
        ))}
      </ul>
      {preview && (
        <section aria-label="Review grouped undo">
          <h3>Undo {preview.label}</h3>
          <p>
            Only these values will be restored. An affected value or property
            that changed afterwards prevents undo.
          </p>
          <ol>
            {preview.actions.map((a, i) => (
              <li key={i}>{describe(a)}</li>
            ))}
          </ol>
          <button disabled={busy} onClick={() => void undo()}>
            Apply reviewed undo
          </button>
          <button disabled={busy} onClick={() => setPreview(undefined)}>
            Cancel
          </button>
        </section>
      )}
    </details>
  );
}
