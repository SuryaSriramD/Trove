import { useState } from "react";
import { batch, pageGuards, type WorkspaceSnapshot } from "../api";
import { clearProperty, guard, setProperty, setTaskDone, uniqueGuards } from "../database/service";
import { DateEditor } from "../database/DatabaseView";
import "./tasks.css";

export function TaskEditor({ workspace: w, recordId, openPage, onCommitted, onError, readOnly = false }: {
  workspace: WorkspaceSnapshot; recordId: string; openPage: (id: string) => void;
  onCommitted: () => Promise<unknown> | void; onError: (error: unknown) => void; readOnly?: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const record = w.records.find(r => r.id === recordId);
  const facet = w.taskFacets.find(f => f.recordId === recordId);
  const page = w.pages.find(p => p.id === record?.pageId);
  const status = w.properties.find(p => p.id === facet?.statusPropertyId);
  const due = w.properties.find(p => p.id === facet?.duePropertyId);
  if (!record || !facet || !page || page.deletedAt || !status) return <span>Unavailable task</span>;
  const options = (status.config as { options: { id: string; label: string; group: string }[] }).options;
  const value = record.values.find(v => v.propertyId === status.id)?.value as string;
  const done = options.find(o => o.id === value)?.group === "done";
  const dueValue = record.values.find(v => v.propertyId === due?.id)?.value ?? null;
  async function run(action: () => Promise<unknown>) {
    if (readOnly || busy) return;
    setBusy(true); try { await action(); await onCommitted(); } catch (e) { onError(e); } finally { setBusy(false); }
  }
  return <div className="canonical-task" contentEditable={false}>
    <input type="checkbox" aria-label={`Complete ${page.title}`} checked={done} disabled={busy || readOnly} onChange={() => void run(() => setTaskDone(w, recordId, !done))}/>
    <input className={`canonical-task-title ${done ? "done" : ""}`} aria-label={`Task title for ${page.title}`} defaultValue={page.title} key={`${page.id}:${page.revision}`} maxLength={512} disabled={busy || readOnly}
      onBlur={e => { const title = e.currentTarget.value.trim(); if (title && title !== page.title) void run(() => batch(w, [{ kind: "page.update", pageId: page.id, title }], pageGuards(w, page))); else e.currentTarget.value = page.title; }}
      onKeyDown={e => { if (e.key === "Enter") e.currentTarget.blur(); if (e.key === "Escape") { e.currentTarget.value = page.title; e.currentTarget.blur(); } }}/>
    <button type="button" onClick={() => openPage(page.id)} aria-label={`Open ${page.title} task`}>↗</button>
    <select aria-label={`Task status for ${page.title}`} value={value} disabled={busy || readOnly} onChange={e => void run(() => setProperty(w, record, status, e.target.value))}>
      {options.map(option => <option key={option.id} value={option.id}>{option.label}</option>)}
    </select>
    {due ? <div className="canonical-task-due"><DateEditor key={`${record.id}:${record.revision}:${due.id}`} label={`Due for ${page.title}`} value={dueValue} disabled={busy || readOnly} onChange={value => void run(() => setProperty(w, record, due, value))}/>
      {dueValue !== null && <button type="button" aria-label={`Clear due for ${page.title}`} disabled={busy || readOnly} onClick={() => void run(() => clearProperty(w, record, due))}>Clear due</button>}</div> :
      <select aria-label={`Bind due property for ${page.title}`} value="" disabled={busy || readOnly} onChange={e => { const due = w.properties.find(p => p.id === e.target.value); if (due) void run(() => batch(w, [{ kind: "task.promote", recordId, statusPropertyId: status.id, duePropertyId: due.id }], uniqueGuards([...pageGuards(w, page), guard("property", status.id, status.revision), guard("property", due.id, due.revision)]))); }}>
        <option value="">No due property</option>{w.properties.filter(p => p.collectionId === record.collectionId && p.type === "date").map(p => <option key={p.id} value={p.id}>Use {p.name} for due</option>)}
      </select>}
  </div>;
}
