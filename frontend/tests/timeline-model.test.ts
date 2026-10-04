import assert from "node:assert/strict";
import test from "node:test";
import {
  timelineAxis,
  timelineRange,
  dayString,
} from "../src/database/timelineModel.ts";
test("saved scales define different finite calendar windows with leap-aware months", () => {
  const day = timelineAxis("2028-02-10", "day"),
    week = timelineAxis("2028-02-10", "week"),
    month = timelineAxis("2028-02-10", "month"),
    quarter = timelineAxis("2028-02-10", "quarter"),
    year = timelineAxis("2028-02-10", "year");
  assert.equal(day.end - day.start, 7);
  assert.equal(dayString(week.start), "2028-02-07");
  assert.equal(week.end - week.start, 56);
  assert.equal(month.ticks[1].day - month.ticks[0].day, 29);
  assert.equal(quarter.ticks[0].label, "Q1 2028");
  assert.equal(year.ticks[0].label, "2028");
  assert.equal(year.step(1), "2033-01-01");
  assert.equal(month.step(-1), "2027-02-01");
  const widths = [day, week, month, quarter, year].map(
    (axis) => timelineRange("2028-02-10", "2028-02-12", axis)!.width,
  );
  assert.ok(
    widths[0] > widths[1] &&
      widths[1] > widths[2] &&
      widths[2] > widths[3] &&
      widths[3] >= widths[4],
  );
});
test("range projection clips edges, retains inclusive all-day ends and rejects invalid dates", () => {
  const axis = timelineAxis("2026-10-01", "day");
  assert.deepEqual(timelineRange("2026-09-28", "2026-10-03", axis), {
    left: 0,
    width: 360,
    before: true,
    after: false,
  });
  assert.equal(timelineRange("2027-01-01", null, axis), null);
  assert.equal(timelineRange("2026-10-01", null, axis)?.width, 120);
  assert.throws(() => timelineAxis("2026-02-30", "day"));
});
