import type { D } from "../api";
export type Option = { id: string; label: string; group?: string };
export const options = (p: D.PropertyDefinition): Option[] =>
  (p.config as { options?: Option[] })?.options ?? [];
export const valueOf = (r: D.Record, p: string): D.RawValue =>
  r.values.find((v) => v.propertyId === p)?.value ?? null;
export function displayValue(
  value: D.RawValue,
  property?: D.PropertyDefinition,
  pages: D.Page[] = [],
): string {
  if (value == null) return "";
  if (property?.type === "title")
    return (
      pages.find((p) => p.id === (value as { pageId: string }).pageId)?.title ??
      "Unavailable"
    );
  if (property?.type === "select" || property?.type === "status")
    return (
      options(property).find((o) => o.id === value)?.label ?? String(value)
    );
  if (property?.type === "multi_select")
    return (value as string[])
      .map((id) => options(property).find((o) => o.id === id)?.label ?? id)
      .join(", ");
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (Array.isArray(value))
    return value
      .map((v) => displayValue(v))
      .join(property?.type === "text" ? "" : ", ");
  if (typeof value === "object") {
    const v = value as Record<string, D.RawValue>;
    if (
      (v.kind === "page" || v.kind === "person") &&
      typeof v.name === "string"
    )
      return v.name;
    if (v.decimal !== undefined) return String(v.decimal);
    if (v.start) return `${v.start}${v.end ? ` → ${v.end}` : ""}`;
    for (const k of ["text", "displayName", "label", "readableText"])
      if (v[k] !== undefined) return String(v[k]);
  }
  return JSON.stringify(value);
}
export type RecordGroup = {
  key: string;
  label: string;
  records: D.Record[];
  children: RecordGroup[];
};
export function groupRecords(
  records: D.Record[],
  property: D.PropertyDefinition | undefined,
  subgroup: D.PropertyDefinition | undefined,
  pages: D.Page[],
): RecordGroup[] {
  if (!property) return [{ key: "all", label: "", records, children: [] }];
  const groups = new Map<string, RecordGroup>();
  for (const record of records) {
    const value = valueOf(record, property.id),
      key = JSON.stringify(value);
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        label: displayValue(value, property, pages) || "No " + property.name,
        records: [],
        children: [],
      };
      groups.set(key, group);
    }
    group.records.push(record);
  }
  for (const group of groups.values())
    if (subgroup)
      group.children = groupRecords(group.records, subgroup, undefined, pages);
  return [...groups.values()];
}
export function reschedule(value: D.RawValue, day: string): D.RawValue {
  const old = (value ?? {}) as {
    start?: string;
    end?: string | null;
    timezone?: string;
    hasTime?: boolean;
  };
  if (!old.hasTime) {
    const distance =
      old.start && old.end ? Date.parse(old.end) - Date.parse(old.start) : 0;
    return {
      start: day,
      end: old.end
        ? new Date(Date.parse(day) + distance).toISOString().slice(0, 10)
        : null,
      timezone:
        old.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone,
      hasTime: false,
    };
  }
  // Keep the original explicit offset and wall time. The service validates the
  // timestamp; moving a timed range preserves its elapsed duration.
  const start = day + (old.start ?? "").slice(10),
    distance =
      old.start && old.end ? Date.parse(old.end) - Date.parse(old.start) : 0;
  return {
    start,
    end: old.end ? new Date(Date.parse(start) + distance).toISOString() : null,
    timezone: old.timezone ?? "UTC",
    hasTime: true,
  };
}
