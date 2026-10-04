# CORE-008 — Database view configuration and calendar ranges

Implementation commit: `02cdb68`. This records source and fixture evidence; packaged macOS and other platform qualification remain open.

Saved visible-property IDs now determine column order. View settings expose order and 40–2000 pixel widths, and the table applies those saved widths. Invalid intermediate width input can be edited normally but cannot be submitted. View identity remains attached to its owning page.

Calendar records expose a range-end field and start/end drag handles. Resizing changes only the selected boundary using guarded record mutations. Timed values retain local wall time in their named IANA timezone across DST; nonexistent local times and inverted ranges are rejected. Ambiguous local times preserve the original offset when available.

Independent expectations are in `frontend/tests/date-range.test.ts` and the property-order/calendar-range cases in `frontend/src/database/tests/database.browser.spec.ts`. They verify saved state after reload, actual rendered 310-pixel width, digit-by-digit entry, linked-view identity, a September–October range extension, and required page/collection/property/record revisions. Date fixtures cover New York DST and an India/UTC date boundary.

The four initial affected browser cases passed. A later two-case check timed out navigating while Vite optimized a newly integrated editor dependency; the unchanged controls then passed in `evidence/completion/checkpoint-1/view-config-warm-rerun.json`. These timings are development-server checks, not a shell-startup benchmark. Database virtualization is tracked separately under CORE-007.
