import type { D } from "../api";

type DateRange = {start: string; end?: string | null; timezone: string; hasTime: boolean};
function wallParts(instant: number, zone: string) {
  const parts = new Intl.DateTimeFormat("en-US", {timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23"}).formatToParts(instant);
  const p = Object.fromEntries(parts.map(p => [p.type, p.value]));
  return {day: `${p.year}-${p.month}-${p.day}`, hour: Number(p.hour), minute: Number(p.minute), second: Number(p.second)};
}
export function calendarDay(value: D.RawValue, boundary: "start" | "end" = "start"): string {
  const range = value as DateRange | null;
  const stamp = range?.[boundary];
  if (!stamp) return "";
  return range?.hasTime ? wallParts(Date.parse(stamp), range.timezone || "UTC").day : stamp.slice(0, 10);
}
function moveBoundary(stamp: string, zone: string, day: string): string {
  const original = Date.parse(stamp), wall = wallParts(original, zone);
  const midnight = Date.parse(`${day}T00:00:00Z`);
  if (!Number.isFinite(midnight) || new Date(midnight).toISOString().slice(0,10) !== day) throw new Error("Choose a valid calendar date.");
  const target = midnight + wall.hour*3600000 + wall.minute*60000 + wall.second*1000 + new Date(original).getUTCMilliseconds();
  const offset = (instant: number) => {
    const p = wallParts(instant, zone);
    return Date.parse(`${p.day}T00:00:00Z`) + p.hour*3600000 + p.minute*60000 + p.second*1000 - Math.floor(instant/1000)*1000;
  };
  // Examine both sides of a transition; preserve the original fold offset when
  // available, and reject nonexistent wall times instead of silently moving them.
  const offsets = new Set([offset(original)]);
  for (let hours=-36; hours<=36; hours+=6) offsets.add(offset(target+hours*3600000));
  for (const delta of offsets) {
    const candidate = target-delta, p = wallParts(candidate, zone);
    if (p.day===day && p.hour===wall.hour && p.minute===wall.minute && p.second===wall.second) return new Date(candidate).toISOString();
  }
  throw new Error("That time does not exist on this date in the record’s time zone. Change its time first.");
}
export function resizeDateRange(value: D.RawValue, boundary: "start" | "end", day: string): D.RawValue {
  const old = value as DateRange | null;
  if (!old?.start) throw new Error("Set a start date before resizing the range.");
  const next = {...old, [boundary]: old.hasTime ? moveBoundary(old[boundary] || old.start, old.timezone || "UTC", day) : day};
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day) || !Number.isFinite(Date.parse(day)) || new Date(Date.parse(day)).toISOString().slice(0,10)!==day) throw new Error("Choose a valid calendar date.");
  if (next.end && Date.parse(next.end)<Date.parse(next.start)) throw new Error("The range end must be on or after its start.");
  return next;
}
