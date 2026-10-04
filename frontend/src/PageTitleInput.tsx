import { useEffect, useRef, useState } from "react";
import type { D } from "./api";
// Sync clean fields with committed updates while retaining an in-progress draft
// and the revision it started from, so a concurrent rename cannot be overwritten.
export function PageTitleInput({
  page,
  onCommit,
}: {
  page: D.Page;
  onCommit: (base: D.Page, title: string) => Promise<boolean>;
}) {
  const [draft, setDraft] = useState(page.title),
    [dirty, setDirty] = useState(false),
    [saving, setSaving] = useState(false);
  const base = useRef(page);
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
      onBlur={async () => {
        if (!dirty || saving) return;
        const title = draft || "Untitled";
        if (title === base.current.title) {
          setDirty(false);
          return;
        }
        setSaving(true);
        try {
          if (await onCommit(base.current, title)) setDirty(false);
        } finally {
          setSaving(false);
        }
      }}
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
