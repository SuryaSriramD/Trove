import { useEffect, useRef, useState } from "react";
import type { D } from "./api";
// Sync clean fields with committed updates while retaining an in-progress draft
// and the revision it started from, so a concurrent rename cannot be overwritten.
export function PageTitleInput({
  page,
  onCommit,
  registerCommit,
  onPendingChange,
}: {
  page: D.Page;
  onCommit: (base: D.Page, title: string) => Promise<boolean>;
  registerCommit?: (commit: (() => Promise<boolean>) | null) => void;
  onPendingChange?: (pending: boolean) => void;
}) {
  const [draft, setDraft] = useState(page.title),
    [dirty, setDirty] = useState(false),
    [saving, setSaving] = useState(false);
  const base = useRef(page);
  const flight = useRef<Promise<boolean> | null>(null);
  const live = useRef(true);
  async function commit() {
    if (flight.current) return flight.current;
    if (!dirty) return true;
    const title = draft || "Untitled";
    if (title === base.current.title) {
      setDirty(false);
      return true;
    }
    setSaving(true);
    const attempt = (async () => {
      try {
        const saved = await onCommit(base.current, title);
        if (saved && live.current) setDirty(false);
        return saved;
      } finally {
        flight.current = null;
        if (live.current) setSaving(false);
      }
    })();
    flight.current = attempt;
    return attempt;
  }
  const commitRef = useRef(commit);
  commitRef.current = commit;
  useEffect(() => {
    live.current = true;
    registerCommit?.(() => commitRef.current());
    return () => { live.current = false; registerCommit?.(null); onPendingChange?.(false); };
  }, []);
  useEffect(() => { onPendingChange?.(dirty); }, [dirty]);
  useEffect(() => {
    if (!dirty) {
      base.current = page;
      setDraft(page.title);
    }
  }, [page, dirty]);
  return (
    <input
      className="page-title"
      aria-label="Page title"
      value={draft}
      placeholder="Untitled"
      disabled={saving}
      onChange={(e) => {
        if (!dirty) base.current = page;
        setDirty(true);
        setDraft(e.target.value);
      }}
      onBlur={() => { void commit(); }}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
        if (e.key === "Escape") {
          setDirty(false);
          setDraft(page.title);
        }
      }}
    />
  );
}
