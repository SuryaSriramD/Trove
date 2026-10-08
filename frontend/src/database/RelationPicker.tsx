import { useEffect, useMemo, useRef, useState } from "react";
import type { D } from "../api";

type Target = { id: string; title: string; search: string };
export type RelationTargets = { options: Target[]; byId: Map<string, Target> };
const emptyTargets: RelationTargets = { options: [], byId: new Map() };
const pageSize = 40;

// One linear index per shell snapshot, shared by all rendered relation cells.
// In particular, never search all pages for each of the 10,000 target records.
export function indexRelationTargets(records: D.Record[], pages: D.Page[]) {
  const titles = new Map(pages.map((page) => [page.id, page.title]));
  const collections = new Map<string, RelationTargets>();
  for (const record of records) {
    let targets = collections.get(record.collectionId);
    if (!targets) {
      targets = { options: [], byId: new Map() };
      collections.set(record.collectionId, targets);
    }
    const title = titles.get(record.pageId) ?? record.id;
    const target = { id: record.id, title, search: title.toLocaleLowerCase() };
    targets.options.push(target);
    targets.byId.set(target.id, target);
  }
  return collections;
}

export function RelationPicker({
  targets = emptyTargets,
  value,
  label,
  disabled,
  onChange,
}: {
  targets?: RelationTargets;
  value: D.RawValue;
  label: string;
  disabled: boolean;
  onChange: (value: string[]) => void;
}) {
  const selected = Array.isArray(value) ? (value as string[]) : [];
  const [editing, setEditing] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const summary = selected
    .slice(0, 2)
    .map((id) => targets.byId.get(id)?.title ?? "Unavailable record")
    .join(", ");
  return (
    <>
      <button
        ref={button}
        type="button"
        className="db-relation-value"
        aria-label={label}
        aria-haspopup="dialog"
        disabled={disabled}
        onClick={() => setEditing(true)}
      >
        {summary || "Choose records"}
        {selected.length > 2 ? ` +${selected.length - 2} more` : ""}
      </button>
      {editing && (
        <RelationDialog
          targets={targets}
          selected={selected}
          label={label}
          disabled={disabled}
          onClose={() => {
            setEditing(false);
            button.current?.focus();
          }}
          onChange={onChange}
        />
      )}
    </>
  );
}

function RelationDialog({
  targets,
  selected,
  label,
  disabled,
  onClose,
  onChange,
}: {
  targets: RelationTargets;
  selected: string[];
  label: string;
  disabled: boolean;
  onClose: () => void;
  onChange: (value: string[]) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState(() => new Set(selected));
  const [search, setSearch] = useState("");
  const [selectedOnly, setSelectedOnly] = useState(false);
  const [page, setPage] = useState(0);
  const close = () => {
    // The trigger is inert while a modal is open; close it before restoring
    // focus, including when Escape is handled explicitly.
    dialog.current?.close();
    onClose();
  };
  useEffect(() => {
    const element = dialog.current!;
    element.showModal();
    searchInput.current?.focus();
    return () => element.close();
  }, []);
  const matches = useMemo(() => {
    const term = search.trim().toLocaleLowerCase();
    // A selected ID absent from the current target list is retained until the
    // user explicitly removes it. Filtering and paging must not drop values.
    const candidates = selectedOnly
      ? [...draft].map(
          (id) =>
            targets.byId.get(id) ?? {
              id,
              title: "Unavailable record",
              search: "unavailable record",
            },
        )
      : targets.options;
    return term
      ? candidates.filter((target) => target.search.includes(term))
      : candidates;
  }, [targets, draft, selectedOnly, search]);
  const currentPage = Math.min(
    page,
    Math.max(0, Math.ceil(matches.length / pageSize) - 1),
  );
  const offset = currentPage * pageSize;
  const shown = matches.slice(offset, offset + pageSize);
  return (
    <dialog
      ref={dialog}
      className="db-relation-dialog"
      aria-label={`Edit ${label}`}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
    >
      <h3>{label}</h3>
      <label>
        Find a record
        <input
          ref={searchInput}
          type="search"
          value={search}
          disabled={disabled}
          onChange={(event) => {
            setSearch(event.target.value);
            setPage(0);
          }}
        />
      </label>
      <label className="db-relation-check">
        <input
          type="checkbox"
          checked={selectedOnly}
          disabled={disabled}
          onChange={(event) => {
            setSelectedOnly(event.target.checked);
            setPage(0);
          }}
        />
        Show selected only
      </label>
      <p role="status">
        {draft.size} selected ·{" "}
        {matches.length
          ? `${offset + 1}–${offset + shown.length} of ${matches.length}`
          : "No matching records"}
      </p>
      <fieldset disabled={disabled} className="db-relation-options">
        <legend className="sr-only">Related records</legend>
        {shown.map((target) => (
          <label key={target.id} className="db-relation-check">
            <input
              type="checkbox"
              checked={draft.has(target.id)}
              onChange={(event) => {
                const checked = event.target.checked;
                setDraft((old) => {
                  const next = new Set(old);
                  if (checked) next.add(target.id);
                  else next.delete(target.id);
                  return next;
                });
                // Removing the focused selected-only choice unmounts it. Restore a
                // stable keyboard focus target inside the modal before that happens.
                if (selectedOnly && !checked) searchInput.current?.focus();
              }}
            />
            {target.title}
          </label>
        ))}
      </fieldset>
      <nav aria-label="Related record choices" className="db-pager">
        <button
          type="button"
          disabled={disabled || currentPage === 0}
          onClick={() => setPage(currentPage - 1)}
        >
          Previous choices
        </button>
        <button
          type="button"
          disabled={disabled || offset + pageSize >= matches.length}
          onClick={() => setPage(currentPage + 1)}
        >
          Next choices
        </button>
      </nav>
      <div className="db-row-actions">
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            // Preserve existing order and IDs; append newly selected records in
            // selection order, rather than reordering when a search changes.
            const next = [...draft];
            if (
              next.length !== selected.length ||
              next.some((id, index) => id !== selected[index])
            )
              onChange(next);
            close();
          }}
        >
          Apply selection
        </button>
        <button type="button" onClick={close}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}
