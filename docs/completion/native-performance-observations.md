# Packaged native performance observations

This opt-in path records fixed-label timings from the actual desktop adapter,
Wails bridge and rendered database view, plus sampled app/owned-descendant RSS.
It is local diagnostic evidence, not a release decision or a newly invented UI
latency SLA. The normal application does not record these samples.

## Preparation and commands

Finish all builds and other heavy work first. Use the same frozen app, helper,
model and source identities as the checkpoint. Every evidence destination must
be new. Use private internal macOS storage for runtime fixtures; the native sink
requires an already-existing owner-private directory and rejects linked
ancestors, existing output files and output replacement/hard links at flush.
These checks reuse the platform owner/mode/link audit; they do not claim a
separate macOS extended-ACL audit.

An optional database benchmark argument retains the same synthetic 10,000-record
fixture after all timed samples. It closes the source before copying, compares
the closed bytes, reopens the copy, checks SQLite integrity, record count and
independently expected first pages, and closes it before the native session.
The original default remains disposable. Retention is outside individual query
timings but inside the existing overall benchmark timeout. A failed/partial
retention cannot be reported as ready.

```sh
python scripts/benchmark-database.py --cache <pinned-cache> --iterations 7 \
  --output <new-service-evidence> --retain-fixture /private/tmp/<new-db-fixture>

python scripts/record-native-performance.py --app <frozen-Trove.app> \
  --workspace /private/tmp/<new-db-fixture> --scenario table \
  --minimum-samples 5 --duration-seconds 600 \
  --output /private/tmp/<new-table-evidence>
```

The driver requires a closed, explicitly selected disposable or authorized
fixture. It refuses a linked fixture root and holds the existing owner lock
while hashing/copying it. Every session operates on its own retained private
clone; input fixture bytes remain unchanged. The source path itself is a trusted
operator selection, not input received from the renderer. The empty-workspace
`idle` scenario may omit `--workspace`. No provider credentials are inherited.
The driver independently checks OS network denial before launching the app.

Operate the actual visible app and quit normally before the deadline. A deadline
or interruption terminates the owned process and is retained as incomplete.
An empty timing file means the app did not flush a valid report. No report,
fixture or prior failure is overwritten. `started.json` retains exact input
inventory digests and the private scratch location for canonical inspection.
Neither process arguments nor workspace filenames/content enter timing or
memory samples; process names/PIDs used for sampling are not saved in those samples. Application
stdout/stderr is discarded by this narrow timing driver.

The driver now also reserves a separate private `renderer-diagnostics.jsonl`
sidecar through `TROVE_PERFORMANCE_DIAGNOSTICS`; it is enabled only together with
the existing `TROVE_PERFORMANCE_REPORT` timing sink. Before qualification
gestures, wait for `readiness.json`: it records
that a newly received renderer snapshot was ready, visible and focused while
the unreaped owned child matched the exact selected executable. Independently
observe that candidate's actual foreground window as well; renderer focus and
an accessibility tree do not establish that observation. `process.json` retains
the owned launch PID and exact executable in private orchestration metadata,
separate from content-free timing and memory samples. Do not select or reacquire
the app after this process exits, because that may reopen a default workspace.
Every reopen uses a new driver invocation with an explicit fixture. If native
automation reports `noWindowsAvailable`, use the predeclared operator-assisted
gesture on the existing bound window and verify its result; do not infer an
application defect or manufacture input samples.

## Repeatable visible scenarios

Record an independent observation alongside the timing files identifying the
scenario, intended operations, expected canonical outcomes, actual outcomes,
visibility and any interruptions. Do not infer correctness from duration alone.

| Scenario | Procedure | Required observations |
| --- | --- | --- |
| `idle` | Open and quit the empty or selected fixture. Repeat separate sessions for startup statistics; use the established shell harness for the shell threshold. | Native snapshot dispatch; this scenario defaults to five samples, so explicitly predeclare `--minimum-samples 1` for a single snapshot-only session. |
| `table` | Open the retained database page named `Changed after benchmark`. Select each ordinary/filter/date view, page forward/back, scroll, then select formula and rollup views. Repeat the same view/page operations at least five times. Keep the app visible. | Database dispatch and bridge samples; successful record loads to two frames; response to two frames; trusted scroll-event to two frames. Inspect exact rows, first/next-page identity and values separately. |
| `edit` | In a cloned fixture, type five distinct short edits, allow each to commit, undo/redo, navigate away/back and quit/reopen for canonical verification. | Mutation dispatch/bridge and trusted input-event to two frames. Two frames do not prove the edit was committed. Retain independent before/after content checks. |
| `concurrent` | Supply already-installed selected packs with `--models`. In the actual UI, start the declared authorized recording/transcription, indexing and AI workloads, perform the same edit sequence, then cancel/retry as declared. | Edit metrics and an observed worker process; independently verify that the intended jobs actually overlapped, progressed, cancelled and preserved content. Worker presence alone proves none of those outcomes. |

Each scenario uses a new invocation/output. Model-free table/edit checks should
omit `--models`. A representative audio/device scenario still needs authorized
input and actual device consent. This script neither starts recording nor grants
permission. It does not implement a native input robot, UI assertions or a
database SLA. Advanced P1 formula/rollup observations remain distinct from P0
table behavior even when they reuse this fixture.

## Meaning and limits of the measurements

`database_dispatch`, `snapshot_dispatch`, `mutation_dispatch` and
`search_dispatch` enclose the actual desktop `App.Dispatch` call, including its
admission, startup wait, locks, service dispatch and response conversion. Success
and failure durations are both included. Existing Go service benchmarks remain
the narrower kernel timing source.

The corresponding `*_bridge` samples enclose client serialization, the bound
desktop call, response transport and JSON parsing. They are independent aggregate
samples, not paired spans; subtracting their percentiles from dispatch timings
does not yield a valid transport measurement.

`database_load_to_frames` begins before the visible database request and ends
after a matching successful React record state is committed and two animation
frame callbacks occur. `database_response_to_frames` begins when that matching
response arrives. Stale or failed loads are excluded. `input_to_frames` and
`scroll_to_frames` begin at trusted browser events and end after two frame
callbacks. These are **scheduling/render opportunities**, not confirmed screen
paint, hardware input latency, correct rendering or canonical persistence.

Frame probes are opportunistic. Hidden documents, cancelled/stale renders and
more than sixteen pending probes omit observations without complete input-event
accounting. Per-metric samples are capped at 512; a further attempted report
marks truncation and prevents the driver claiming a usable local observation.
Renderer calls are asynchronous and can still be in flight at shutdown. The
native report's `finalized` field means only that its collector flushed. The
driver always reports `sampleAccountingComplete: false`; its predeclared sample
minima establish only that those observations were recorded. No complete
workflow, latency or release acceptance follows from `localObservationRecorded`.

The separate diagnostic stream contains only fixed readiness/visibility/focus
booleans, pending counts and saturated counters for probes and transport
acknowledgements. It distinguishes hidden, cancelled, limited and missing-start
probes from rejected/failed metric delivery. It sends at most one report in
flight, normally every 500 ms; the native collector accepts at most 4,096
snapshots plus a final boundary and rejects malformed, decreasing or unknown
counters. No DOM content, input values, keys, selectors, paths or raw errors
enter this stream. Missing/invalid finalization, capped diagnostics or absent
live readiness keep collection incomplete. Diagnostic counters do not restore
omitted timing observations or establish complete event accounting. The final
boundary means the backend collector closed, not that the renderer drained or
that every event was sampled. Both instrumentation paths remain disabled in
ordinary application runs without their explicit private output configuration.
Each JSONL record has `schemaVersion: 1`, monotonic `sequence`, `finalized` and
`capped` flags; `ready`, `visible`, `focused`, `stopped`; `pendingFrames` and
`pendingTransports`; and the fixed `counters` object. The collector accepts no
additional fields. This separate wire shape does not change `timings.json` or
the qualification observation schema. `readiness.json` is a historical live
witness, so the operator still checks the current window before each workflow;
its `actualForegroundWindowIndependentlyObserved` is deliberately false.

The external sampler targets a 250 ms interval and records actual acquisition
time and spacing. It starts after the app reserves its timing sink, checks the
owned root process identity, and reconstructs current descendants each time,
bounded to 512 processes. The unreaped owned root cannot reuse its PID during a
sample. Failed samples are unavailable, never zero; an unavailable final sample
after confirmed owned-process exit is distinguished from an active-process
sampling failure. App RSS, recognized worker
RSS and other-descendant RSS remain separate. Their shared pages can overlap;
non-descendant WebKit, GPU/driver allocations and peaks between samples are
absent. **This is not complete whole-application memory qualification.**

The tool reports raw samples, counts, median, nearest-rank p95 and maximum. Small
samples are diagnostic, not population percentile guarantees. Instrumentation
and the external sampler add overhead. Run uninstrumented shell/service timing
separately; preserve all slow/failed observations. No numeric database, editing
or scrolling acceptance limit has been agreed by this package.
