import type { D, S, WorkspaceSnapshot } from "../api";
import { displayValue, options } from "../database/model";

const editableTypes = new Set(["text", "number", "select", "multi_select", "status", "checkbox", "date", "url", "email", "phone", "files", "relation", "identity", "place"]);

function initialValue(property: S.HistoryPropertyDefinition): unknown {
  switch (property.type) {
    case "text": case "multi_select": case "files": case "relation": case "identity": return [];
    case "number": return { decimal: "", format: (property.config as { format?: string }).format ?? "number" };
    case "checkbox": return false;
    case "select": case "status": return null;
    case "date": return { start: "", hasTime: false };
    case "place": return { label: "", raw: {} };
    default: return "";
  }
}

function savedValue(issue: S.HistoryHistoricalValueIssue, workspace: WorkspaceSnapshot): string {
  // Historical metadata can describe versions and payloads outside today's registry.
  try { return displayValue(issue.historicalValue.value, issue.historicalProperty as D.PropertyDefinition | undefined, workspace.pages) || "Empty"; }
  catch { return JSON.stringify(issue.historicalValue.value); }
}

function MappedValue({ property: p, value, workspace: w, disabled, onChange }: {
  property: S.HistoryPropertyDefinition; value: unknown; workspace: WorkspaceSnapshot; disabled: boolean; onChange: (value: unknown) => void;
}) {
  const label = `Mapped value for ${p.name}`;
  if (p.type === "checkbox") return <label><input type="checkbox" aria-label={label} disabled={disabled} checked={value === true} onChange={e => onChange(e.target.checked)} />Checked</label>;
  if (p.type === "select" || p.type === "status" || p.type === "multi_select") {
    const multiple = p.type === "multi_select";
    return <select aria-label={label} disabled={disabled} multiple={multiple} value={multiple ? (Array.isArray(value) ? value as string[] : []) : typeof value === "string" ? value : ""} onChange={e => onChange(multiple ? Array.from(e.target.selectedOptions, o => o.value) : e.target.value || null)}>
      {!multiple && <option value="" disabled={p.type === "status"}>{p.type === "status" ? "Choose status" : "Empty"}</option>}
      {options(p as D.PropertyDefinition).map(option => <option key={option.id} value={option.id}>{option.label}</option>)}
    </select>;
  }
  if (p.type === "files") return <select multiple aria-label={label} disabled={disabled} value={Array.isArray(value) ? value.map(v => (v as { fileId: string }).fileId) : []} onChange={e => onChange(Array.from(e.target.selectedOptions, option => ({ fileId: option.value, displayName: option.text })))}>{w.files.map(file => <option key={file.id} value={file.id}>{file.originalName}</option>)}</select>;
  if (p.type === "relation") return <select multiple aria-label={label} disabled={disabled} value={Array.isArray(value) ? value as string[] : []} onChange={e => onChange(Array.from(e.target.selectedOptions, option => option.value))}>{w.records.filter(record => record.collectionId === (p.config as { targetCollectionId: string }).targetCollectionId).map(record => <option key={record.id} value={record.id}>{w.pages.find(page => page.id === record.pageId)?.title ?? "Untitled record"}</option>)}</select>;
  if (p.type === "number") return <input aria-label={label} disabled={disabled} inputMode="decimal" value={(value as { decimal?: string } | null)?.decimal ?? ""} onChange={e => onChange({ decimal: e.target.value, format: (p.config as { format?: string }).format ?? "number" })} />;
  if (p.type === "text") return <textarea aria-label={label} disabled={disabled} value={displayValue(value, p as D.PropertyDefinition)} onChange={e => onChange([{ text: e.target.value }])} />;
  if (p.type === "date") return <label>Mapped day<input type="date" aria-label={label} disabled={disabled} value={((value as { start?: string } | null)?.start ?? "").split("T")[0]} onChange={e => onChange({ start: e.target.value, hasTime: false })} /></label>;
  if (p.type === "place") return <input aria-label={label} disabled={disabled} value={(value as { label?: string } | null)?.label ?? ""} onChange={e => onChange({ ...(typeof value === "object" ? value : {}), label: e.target.value, raw: (value as { raw?: unknown } | null)?.raw ?? {} })} />;
  if (p.type === "identity") return <label>Names, separated by commas<input aria-label={label} disabled={disabled} value={Array.isArray(value) ? value.map(v => (v as { displayName: string }).displayName).join(", ") : ""} onChange={e => onChange(e.target.value.split(",").map(name => name.trim()).filter(Boolean).map(displayName => ({ sourceSystem: "trove", externalKind: "person", displayName, raw: {} })))} /></label>;
  return <input aria-label={label} disabled={disabled} type={p.type === "email" ? "email" : p.type === "url" ? "url" : p.type === "phone" ? "tel" : "text"} value={typeof value === "string" ? value : ""} onChange={e => onChange(e.target.value)} />;
}

export default function HistoricalValueReview({ issues, targets, decisions, workspace, disabled, onDecision }: {
  issues: S.HistoryHistoricalValueIssue[]; targets: S.HistoryPropertyDefinition[]; decisions: Record<string, S.HistoryPropertyDecision>; workspace: WorkspaceSnapshot; disabled: boolean; onDecision: (decision: S.HistoryPropertyDecision) => void;
}) {
  const editable = targets.filter(property => editableTypes.has(property.type));
  return <section aria-label="Historical property review">
    <h4>Review changed properties</h4>
    <p>These saved values no longer match the current database. Retain a value as evidence to keep the current value, or choose its destination and review an explicit replacement. Shared property definitions stay current.</p>
    {issues.map(issue => {
      const id = issue.historicalValue.propertyId;
      const name = issue.historicalProperty?.name ?? "Removed property";
      const decision = decisions[id];
      const target = decision?.action === "map" ? editable.find(p => p.id === decision.targetPropertyId) : undefined;
      return <fieldset key={id} disabled={disabled}>
        <legend>{name}</legend>
        <p>{issue.reason}</p>
        <p>Saved value: {savedValue(issue, workspace)}</p>
        <details><summary>View retained source details</summary><pre>{JSON.stringify(issue.historicalValue.value, null, 2)}</pre></details>
        <label>Recovery choice<select aria-label={`Recovery choice for ${name}`} value={decision?.action ?? ""} onChange={event => {
          if (event.target.value === "retain") onDecision({ sourcePropertyId: id, action: "retain" });
          else if (event.target.value === "map") {
            const property = editable.find(p => p.id === issue.currentProperty?.id);
            onDecision({ sourcePropertyId: id, action: "map", targetPropertyId: property?.id ?? "", value: property ? initialValue(property) : null });
          }
        }}><option value="" disabled>Choose how to recover</option><option value="retain">Retain as evidence; keep current value</option><option value="map">Map to a current property</option></select></label>
        {decision?.action === "map" && <>
          <label>Destination property<select aria-label={`Destination property for ${name}`} value={decision.targetPropertyId} onChange={event => {
            const property = editable.find(p => p.id === event.target.value);
            if (property) onDecision({ ...decision, targetPropertyId: property.id, value: initialValue(property) });
          }}><option value="" disabled>Choose a current property</option>{editable.filter(p => p.id === id || !issues.some(other => other.historicalValue.propertyId === p.id)).map(p => <option key={p.id} value={p.id}>{p.name} · {p.type}</option>)}</select></label>
          {target && <MappedValue property={target} value={decision.value} workspace={workspace} disabled={disabled} onChange={value => onDecision({ ...decision, value })} />}
        </>}
      </fieldset>;
    })}
  </section>;
}
