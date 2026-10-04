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

The shell's full workspace snapshot and frontend all-page preload are separate
implementation work. Query paging alone does not remove their memory cost or
justify a workspace-size/RSS claim. Charts and grouped views must retain complete
aggregation semantics when the frontend adopts incremental loading.
