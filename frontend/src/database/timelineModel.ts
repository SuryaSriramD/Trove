export type TimelineScale = "day" | "week" | "month" | "quarter" | "year";
export const dayMs = 86400000;
export function dayNumber(day: string) {
  const value = Date.parse(day + "T00:00:00Z");
  if (
    !Number.isFinite(value) ||
    new Date(value).toISOString().slice(0, 10) !== day
  )
    throw Error("Choose a valid date.");
  return value / dayMs;
}
export const dayString = (value: number) =>
  new Date(value * dayMs).toISOString().slice(0, 10);
const utcMonth = (year: number, month: number) => {
  const date = new Date(0);
  date.setUTCFullYear(year, month, 1);
  return date.getTime();
};
export function timelineAxis(anchor: string, scale: TimelineScale) {
  const stamp = dayNumber(anchor) * dayMs,
    date = new Date(stamp);
  let start: number,
    count: number,
    months = 0,
    days = 0;
  switch (scale) {
    case "day":
      start = stamp;
      count = 7;
      days = 1;
      break;
    case "week":
      start = stamp - ((date.getUTCDay() + 6) % 7) * dayMs;
      count = 8;
      days = 7;
      break;
    case "month":
      start = utcMonth(date.getUTCFullYear(), date.getUTCMonth());
      count = 12;
      months = 1;
      break;
    case "quarter":
      start = utcMonth(
        date.getUTCFullYear(),
        Math.floor(date.getUTCMonth() / 3) * 3,
      );
      count = 8;
      months = 3;
      break;
    case "year":
      start = utcMonth(date.getUTCFullYear(), 0);
      count = 5;
      months = 12;
      break;
  }
  const base = new Date(start),
    ticks = [];
  for (let i = 0; i <= count; i++) {
    const at = months
      ? utcMonth(base.getUTCFullYear(), base.getUTCMonth() + months * i)
      : start + days * i * dayMs;
    const date = new Date(at);
    const label =
      scale === "year"
        ? String(date.getUTCFullYear())
        : scale === "quarter"
          ? `Q${Math.floor(date.getUTCMonth() / 3) + 1} ${date.getUTCFullYear()}`
          : scale === "month"
            ? date.toLocaleDateString("en", {
                timeZone: "UTC",
                month: "short",
                year: "numeric",
              })
            : date.toISOString().slice(0, 10);
    ticks.push({ day: at / dayMs, label });
  }
  return {
    ticks,
    start: ticks[0].day,
    end: ticks.at(-1)!.day,
    width: count * 120,
    step: (direction: number) =>
      dayString(
        months
          ? utcMonth(
              base.getUTCFullYear(),
              base.getUTCMonth() + months * count * direction,
            ) / dayMs
          : start / dayMs + days * count * direction,
      ),
  };
}
export function timelineRange(
  start: string,
  end: string | null | undefined,
  axis: { start: number; end: number; width: number },
) {
  const from = dayNumber(start),
    to = Math.max(from, dayNumber(end || start)) + 1;
  if (to <= axis.start || from >= axis.end) return null;
  const clippedStart = Math.max(from, axis.start),
    clippedEnd = Math.min(to, axis.end);
  return {
    left: ((clippedStart - axis.start) / (axis.end - axis.start)) * axis.width,
    width: Math.max(
      4,
      ((clippedEnd - clippedStart) / (axis.end - axis.start)) * axis.width,
    ),
    before: from < axis.start,
    after: to > axis.end,
  };
}
