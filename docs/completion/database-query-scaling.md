# Database query scaling

`queryRecords` now executes canonical-property filters and ordering inside SQLite,
with bound arguments and keyset paging. The query connection uses Go-backed typed
comparison functions: arbitrary-precision decimals, timed-date instants, Unicode
text matching, array boundaries, nulls, and stable sort-key/ID ties retain their
existing semantics. A page hydrates at most 200 records in two reads; displayed
derived cells are calculated only for that page.

Views that filter or sort on formula/rollup values build one ordered-ID and
derived-value memo snapshot and reuse it across subsequent pages. This is a
bounded fallback, not an indexed formula engine: one query may scan at most
100,000 authorized records, perform 100,000 derived evaluations, and retain
16 MiB of serialized working data. The kernel cache holds at most eight entries
and 64 MiB of serialized data, expires entries after one minute, and evicts least
recently used entries. An expired/evicted snapshot returns `CONFLICT`; exhausted
work/data bounds return `RESOURCE_EXHAUSTED`. Neither produces partial successful
results. These byte counts exclude Go object overhead, SQLite memory, concurrent
in-flight pages, and the rest of the application; they are not process-RSS limits.
SQLite uses file-backed temporary storage for query sorts.

Cursors are HMAC-sealed and bind the collection, view, query, canonical event
cursor, minute clock, actor, policy revision, scope and grant. Tampering or reuse
under another grant is rejected. Canonical changes and minute changes require a
fresh query. Grants are checked again before data is returned. Query functions
are registered on a dedicated two-connection pool per kernel; writer and backup
connections and other kernels do not inherit them. Closing a kernel clears its
memo and cursor secret. There is no persistent derived cache or schema migration.

Automated coverage checks actual SQL comparison parity, precision/date filters,
scoped keyset paging, cursor tampering and grant revocation, memo reuse/expiry,
global/per-query budget failures, cancellation, and connection isolation under
the race detector. This is local developer validation, not a final signed-build
performance qualification.

The renderer now requests `workspace.snapshot` with `contentMode: "shell"`.
The default full snapshot remains compatible. Shell mode returns scoped page
metadata, collection/view/property definitions, record identities/revisions,
title projections and task status/due values; it omits every page's block body
and ordinary record-cell payloads. The active page is fetched with `readPage`;
reusable blocks and review edits use scoped `readBlock`; backlinks use their
existing scoped query. The editor waits for matching page content/revision before
mounting, and failed reads expose a retry rather than an editable empty page.

Ungrouped tables and galleries retain only one 200-record page, with explicit
previous/next navigation. Variable-height virtual rendering keeps the visible
rows plus a focused editor in the DOM. The cursor history stores cursor strings,
not old records. Record changes, view changes and minute-clock changes restart
the query. Complete list/board/calendar/timeline/grouped views load their complete
result up to 10,000 records; larger results display an explicit error, never a
partial count or aggregate. Chart and advanced-derived queries run only when
their panels are opened; chart aggregation still uses the complete authorized
backend result and its existing explicit bounds.

Shell metadata is still O(pages + records), needed by current navigation, relation
pickers and canonical revision guards. Auxiliary collections (including meeting
segments, file metadata, memories and import mappings) retain their existing
snapshot behavior. This removes the general page-body/cell preload but does not
establish a constant-memory shell or a qualified maximum workspace size. Those
claims require immutable-build memory/startup measurements on target hardware.

Additional checks cover shell payload omission, task guard/status retention,
legacy full snapshots, scope denial for block reads, and a 10,000-record fixture.
On this developer machine, race-enabled first and second 200-record reads took
about 106 ms combined; this is a diagnostic observation, not an acceptance SLA.
Browser tests exercise lazy page bodies, independent backlinks, virtual
table/gallery pages and complete-list loading alongside the existing database
editing/width/calendar/chart regressions.

## Scoped quick search correction

Quick-switcher body matches in shell mode use scoped keyword search; title matches remain available from scoped metadata. Responses are tied to workspace, grant, snapshot cursor and query, so stale searches cannot replace current results. Search failures remain visible. The 10,000-page browser fixture now honors shell snapshots and scoped page reads, and tests body-only matches and refresh without a globally loaded block array. Its renderer readiness retains the two-second assertion; the multi-step 10,000-node accessibility workflow has a separate 60-second test timeout.
