# Timeline contract and implementation

PARITY-003 adds five persisted axis scales: day shows seven daily ticks, week eight Monday-based weeks, month twelve calendar months, quarter eight calendar quarters, and year five years. Each window can move earlier/later or anchor to a chosen date. Month lengths and leap years affect bar geometry. Saved group and subgroup settings retain the canonical query order inside each group.

Date ranges use the civil dates in their stored timezone; an all-day end is inclusive. Timed records retain wall-clock time and timezone when moved/resized, and nonexistent DST wall times or inverted ranges are rejected. Drag the bar or its edge, or use the accessible start/end date controls. Changes use the existing guarded record.setValue service (page, record, collection and property revisions). The timeline does not reschedule dependent records.

The optional showDependencies setting defaults to true for old views. Arrows point from a prerequisite's end to the dependent's start when both ranges are in the visible window. Dependency lists include scoped targets outside the filtered view. Structure is read without hydrating cell payloads or running formula evaluation; both endpoints must belong to the granted scope. The query returns an explicit resource error beyond 10,000 scoped records or 100,000 dependency edges. More than 5,000 visible arrows suppresses the overlay with an explicit notice; full dependency lists remain available. A dependency-query failure is shown as unavailable rather than as an empty set.

Timeline/grouped views retain the existing complete-result 10,000-record UI limit; this is not a claim of constant memory or arbitrarily large timeline rendering. Table/gallery paging remains unchanged.

Validation: independent scale/geometry and timezone-resize unit cases; Go race test for dependency-scope filtering and saved scale/group/order configuration after reopen; browser test for axis geometry, group headings, dependency arrows, guarded date edits and saved configuration after refresh; TypeScript compilation. This is development evidence, not signed build or cross-platform qualification.
