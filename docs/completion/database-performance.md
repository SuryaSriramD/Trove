# 10,000-record database service measurement

The October 5, 2026 local measurement completed all nine correctness profiles. It is diagnostic evidence for REL-007, not release qualification. Ordinary first pages took 35.7–186.6 ms at the median on this host. First-page formula and rollup ordering remained substantially more expensive: 1,185.9 ms and 763.5 ms respectively. Cached second pages were much cheaper.

## Source, workload, and boundary

[Environment and source hashes](../../evidence/completion/database-10k/environment.json), [raw samples and assertions](../../evidence/completion/database-10k/results.json), and [test output](../../evidence/completion/database-10k/go-test.jsonl) bind this run to production revision `ecbba8f33ddf1d9ecf36da8eb4111ba8f0d08a86` plus the separately hashed benchmark files. No hashed input changed during the run. The benchmark files were untracked at measurement time; the recorded tracked-only Git status is not a claim that they were part of that revision.

The host was Mac16,12, Apple M4, 10 CPU cores, 24 GiB memory, macOS 27.0.1 (26A434), on AC power. The pinned toolchain was Go 1.27.1, arm64, using `fts5 sqlite_omit_load_extension`. Disposable synthetic workspaces lived on the internal Data filesystem under `/private/tmp`. OS and SQLite caches were warm; other desktop activity was possible.

The deterministic corpus contains 10,000 records, 9,892 live records, 108 trashed pages, 12 properties, 88,320 stored typed values, and 10,000 paragraph bodies totaling 4,433,340 UTF-8 bytes. Values include decimals above JavaScript's safe integer range, missing numbers/dates, differently offset timestamps representing the same UTC clock, rich Unicode text, status, select, multi-select, checkbox, 50 relation targets, an arithmetic formula, and a sum rollup. Pages use deliberately permuted stable order keys. Schema and saved views are created through guarded services; bulk record setup uses prepared SQL, typed contract validation, relation-edge projection, and SQLite integrity/foreign-key checks. This excludes mutation, import, indexing, and event-generation costs.

Each profile first walks every result page against an independent oracle computed from source-row integers. The oracle does not call production filters, comparison functions, SQL, or the formula evaluator. It checks exact membership and order, canonical page/record identity, preserved decimals and live derived values, and absence of gaps or duplicates. SQL-looking text must match only its one literal row. The provider profile grants the collection page plus 99 explicit record pages, and verifies that inaccessible relation targets do not yield rollup values. A canonical mutation must invalidate a saved cursor, and disconnect must revoke the provider bridge. All checks passed; no clock-bound cursor restart was needed.

Timing encloses `Kernel.Dispatch`, including contract checks, query execution, hydration, and response JSON serialization. The provider profile also encloses in-process bridge authentication and scope checks. Caller JSON decoding and independent assertions are excluded. There is one additional unmeasured observation after the correctness walk, followed by three bounded samples for each measured page. Percentiles are nearest-rank; with three samples, p95 is the maximum, not a reliable population percentile.

## Results

All rows request at most 200 records, with all 12 properties visible.

| Profile | Matching rows | First page median / max | Second page median / max |
| --- | ---: | ---: | ---: |
| All live, default order | 9,892 | 35.72 / 35.90 ms | 36.87 / 37.65 ms |
| Exact decimal and status, decimal descending | 1,791 | 186.59 / 187.16 ms | 197.76 / 198.83 ms |
| Timed date and checkbox, date ascending | 1,535 | 171.20 / 172.54 ms | 174.37 / 181.44 ms |
| Case-insensitive Unicode contains | 1,321 | 57.14 / 58.34 ms | 54.28 / 55.90 ms |
| SQL-looking literal text | 1 | 96.05 / 103.12 ms | — |
| Multi-select and missing number | 181 | 87.08 / 87.25 ms | — |
| Formula filter and descending order | 2,572 | 1,185.90 / 1,200.96 ms | 22.24 / 23.06 ms |
| Relation rollup filter and ascending order | 1,979 | 763.55 / 827.56 ms | 28.90 / 30.43 ms |
| Provider bridge, 99 selected record pages | 99 | 68.73 / 70.71 ms | — |

Most full first-page responses were about 535–550 KB of JSON. Mean cumulative Go allocation per formula first-page request was 457,382,091 bytes, and per rollup first-page request was 277,700,981 bytes. These are allocation deltas, not retained memory or process RSS; they exclude SQLite C allocations. The ending Go heap was 99,652,288 bytes, not a measured peak. The expensive initial ordering snapshot is a concrete optimization target; cached pagination does not remove its cost when a view opens or refreshes.

No database-specific time threshold was invented for this measurement. The roadmap's search and hybrid retrieval targets apply to different operations. This run does not qualify Wails IPC, native webview rendering, animation frames, scrolling, keyboard-to-paint latency, memory pressure, concurrent recording/indexing/AI, shell startup, search, or import performance. Those gates remain open and must be measured on an identified packaged candidate. Windows and Linux remain separately unqualified.

## Reproduction and history

Run `python3 scripts/benchmark-database.py --cache <pinned-development-cache> --iterations 3 --output <new-evidence-directory>`. Iterations are bounded to 3–30 (default 7), the Go test has a six-minute timeout, and the fixture is disposable. The runner records hardware, storage, power, source hashes, raw samples, and source-change detection. `localMeasurementComplete` only establishes that this local scenario completed with unchanged inputs; `releaseQualified` is deliberately false.

The [initial attempt](../../evidence/completion/database-10k-initial/environment.json) failed before measurement because the new fixture's filter values omitted required number/date fields. Its [failure log](../../evidence/completion/database-10k-initial/go-test.jsonl) is retained. The fixture was corrected and all stored typed values now undergo contract validation; the successful run above is the measured baseline.
