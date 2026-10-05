# 10,000-record database service measurement

The latest optimization passes all nine independent profiles and cuts formula first-page median time from 1,186 ms to 716 ms, with 56% lower cumulative Go allocation. Rollup allocation falls 52%. See the follow-up section for implementation and test evidence.

The October 5, 2026 baseline measurement completed all nine correctness profiles. It is diagnostic evidence for REL-007, not release qualification. Ordinary first pages took 35.7–186.6 ms at the median on this host. First-page formula and rollup ordering remained substantially more expensive: 1,185.9 ms and 763.5 ms respectively. Cached second pages were much cheaper.

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

## Follow-up optimization

The [optimized environment/source binding](../../evidence/completion/database-10k-optimized/environment.json) and [unchanged-oracle results](../../evidence/completion/database-10k-optimized/results.json) identify revision `7a2e75a` plus the three production files whose complete hashes are recorded in the report. The benchmark source is identical to the baseline. Every expected row-count and ordered record-ID digest matches the baseline; the full paging, exact values, scoped rollup denial, cursor invalidation, and disconnect assertions still pass. No measured source changed during the run.

The implementation avoids hydrating unrelated cells while scanning a derived sort/filter, caches immutable property definitions/configuration only within the current read transaction, and bounds that metadata cache to 256 entries / 512 KiB of encoded definitions (also charged to the existing working-data budget). Response pages still hydrate all canonical properties.

Repeated execution-context schema validation is now performed once on an evaluator-owned deep copy. Every value lookup, including a cached result, still compares the current kernel grant's canonical bytes to that copy and rechecks the record's page, deletion, and the existing page/project scope policy. Scope membership is not cached. Revocation or a changed policy/context therefore stops cached reads. The ordinary public authorization method retains its full validation; its existing scope switch is shared without changing its semantics. Formula interpretation, exact comparisons, snapshot/cursor identities, revisions, and clock binding are unchanged.

| Profile | Optimized first page median / max | Optimized second page median / max |
| --- | ---: | ---: |
| All live, default order | 25.48 / 26.44 ms | 25.54 / 25.55 ms |
| Exact decimal and status | 113.43 / 115.35 ms | 121.56 / 122.43 ms |
| Timed date and checkbox | 117.07 / 117.17 ms | 126.39 / 126.70 ms |
| Unicode contains | 38.63 / 38.73 ms | 42.50 / 42.84 ms |
| SQL-looking literal | 103.86 / 104.96 ms | — |
| Multi-select and missing number | 80.25 / 80.51 ms | — |
| Formula filter/sort | 715.63 / 820.87 ms | 28.58 / 30.93 ms |
| Relation rollup filter/sort | 512.54 / 518.89 ms | 25.14 / 26.09 ms |
| Provider bridge, 99 selected records | 27.93 / 29.81 ms | — |

Mean first-page Go allocations fall from 457,382,091 to 199,475,016 bytes for formulas (56.4%) and from 277,700,981 to 132,722,317 bytes for rollups (52.2%). Formula second-page time rises from 22.24 to 28.58 ms in this small sample because cached values now also check current grant/page scope. These observations do not establish improvements for every machine or workload; the literal-text sample also varied upward. The remaining first-page cost and allocations are still meaningful, and visible interaction has not been measured.

The [focused test log](../../evidence/completion/database-query-optimization-tests.jsonl) records 106 passing tests/subtests covering derived queries, exact semantics, cache limits, formulas, advanced databases, and related cases. The [race run](../../evidence/completion/database-query-optimization-race.jsonl) records 15 passing tests/subtests, including forged contexts, revocation, changed policy, mutated caller scope, moved record page, deletion, removed project membership, scoped relations, and stale proposals. The new mixed-filter regression combines title/status/number filtering with derived values, checks both pages, then edits the formula and requires old-cursor refusal plus fresh results.

A separate profiler run of baseline `7a2e75a` completed successfully. Its [allocation report](../../evidence/completion/database-query-profile-alloc.txt) and [CPU report](../../evidence/completion/database-query-profile-cpu.txt) identify repeated authorization/schema validation, property reads, and hydration as major costs. Profiler timing is excluded from the comparison because instrumentation changes scheduling and overhead. The [intermediate projection-only run](../../evidence/completion/database-10k-projection/results.json) is retained; it reduced allocations but left context validation as the dominant avoidable cost. An [initial regression-fixture failure](../../evidence/completion/database-query-optimization-fixture-failure.jsonl) is also retained: the fixture needed the existing project-scope command and formula evaluator-version field. The corrected fixtures pass.

The final source/fixture/log hashes and comparison checks are in [optimization verification](../../evidence/completion/database-query-optimization-verification.json). All native-frame, keyboard/scrolling, concurrent recording/indexing/AI, representative import, clean-device, and platform gates listed above remain open. This package supplies implementation and local evidence for REL-007; it does not close that release item.
