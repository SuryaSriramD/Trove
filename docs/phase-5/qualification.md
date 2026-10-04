# P1 qualification assessment

**Status: supported P0/P1 contracts pass automated qualification; full P1 release approval remains withheld.** Trove 0.5.0 is available as a personal-use candidate for macOS 27 Apple Silicon. The remaining product breadth, live-integration and device limits are explicit below. This assessment does not promote partially implemented requirements to complete.

## Candidate and immutable evidence

The [aggregate](../../evidence/phase5/runs/20260930T191730Z-9d0572ce/verification.json) completed at **2026-09-30 19:23:15 UTC** (2026-10-01 00:53:15 IST): **all 42 checks and V01–V10 passed**. Source and candidate files remained unchanged throughout the run. There were no skipped or failing test cases; Go packages with no test files are not counted as test cases.

- App: `build/phase5-candidate-4/Trove.app`.
- [Personal-use candidate ZIP](../../build/releases/Trove-0.5.0-p1-candidate.zip), with [archive manifest](../../build/releases/Trove-0.5.0-p1-candidate.zip.manifest.json).
- [Release guide and package limitations](release.md), including reproduction and rollback.
- [Immutable run artifacts](../../evidence/phase5/runs/20260930T191730Z-9d0572ce/), including test logs, browser reports, model/dependency inventories and file hashes.

The archive was extracted and compared with the qualified bundle; app, relay and native helpers retained executable permissions, and strict deep signature verification passed. The packager revalidated source, app and evidence after copying. It preserved `releaseApproved: false`. The candidate has not replaced the user's installed P0 app or workspace.

| Identity | SHA-256 |
| --- | --- |
| ZIP | `1b4c641ba9e66fa54f4a396aec9f08f0c7ce9c2454bc9f049957ab54b507036c` |
| App executable | `20ddd24cea0c42a9b4b7934eece1a461a8314cd7160a05c75a0983859e7be7d4` |
| Source fingerprint | `263de3fcff0a00ecb35ce2dd87fb12665a066419e741048e65588457f0a1b793` |
| Bundle fingerprint | `f257b107af6d1198179b09946c33335de0bff375c12d7416edafc63be1ab14f3` |

Source and bundle fingerprints hash sorted file/hash maps, not an archive. Individual file hashes and the verification-report hash are retained in the manifest.

## TRD verification families

Every result below means **automated qualification of the supported contract**. Exact required test identities and associated checks appear in the aggregate; missing, skipped or failed required evidence fails the family. The seven 4A–4G packages are mapped to these families with their remaining limits.

| Family | Result | Evidence scope |
| --- | --- | --- |
| V01 Identity | Passed | P0 canonical records; P1 shared/linked views, moves, retries and restart without duplicate identities. |
| V02 Import reconciliation | Passed | ZIP regression, inert HTML, paginated API fixtures, denied roots/authentication, interrupted downloads and explicit partial outcomes. |
| V03 Provenance | Passed | Original metadata, unsupported payloads, source identities and captured formula evidence survive import and recovery. |
| V04 Editor | Passed | Editing/preservation, tables and merge evidence, tabs/layout ownership, locks, replacement, draft conflicts and bounded ordinary-edit allocations. |
| V05 Formulas | Passed | Independent decimal/type/blank/Unicode/clock fixtures, relation scope, rollups, cycles and numeric/work limits. |
| V06 Recovery | Passed | Thirty populated canonical/P1 tables compared field by field; asset-byte hashes, restart, backups and restored advanced objects; renewed external-root consent. |
| V07 Automation | Passed | Atomic effects/receipts, replay/retry/pause, origin exclusions, skipped conditions, missed runs and non-hour DST transitions. |
| V08 Offline operation | Passed | OS-denied network control; native models; P0/P1 packaged restart/restore; model-free local operation and exact P1 offline regressions. |
| V09 AI evidence | Passed | Current/stale/deleted/contradictory sources, exact citations, scoped memory, reviewed drafts and real local P0 inference. |
| V10 Tool boundaries | Passed | Scope/revision/approval fences, external-root exclusions, actual packaged MCP relay, cancellation, revocation and transport failures. |

The aggregate includes **59 Python tests; 197 Go tests / 301 test-and-subtest results under the race detector; 18 frontend unit tests; 32 application and 3 database browser tests**. Browser tests had no skips or retries. The offline native phase passed **8 tests / 10 results**, and the P1 offline phase passed **12 tests / 33 results**. The required ten offline identities were checked explicitly. The candidate's real relay passed a separate subprocess/loopback/SQLite test.

P0 packaged `p0-create`, `p0-verify` and `native`, plus P1 packaged `p1-create` and `p1-verify`, all passed under OS network denial. Data steps used an empty model cache and minimal PATH; inference received explicitly installed verified packs. These are actual Wails processes, independent of the browser service fixtures.

The [one-hour transcription fixture](../../evidence/phase5/runs/20260930T191730Z-9d0572ce/native-one-hour-native.json) completed all 60 minute units in **95.26 seconds**, retaining **694 timestamped segments** while accepting **41 same-page edits**; the maximum observed edit commit was **423.96 ms**. The input repeats public JFK speech, so this does not establish natural-meeting accuracy or private microphone capture.

## Performance on the qualification Mac

The [service benchmark](../../evidence/phase5/performance-final/benchmark-service.json) and [host/model evidence](../../evidence/phase5/performance-final/benchmark-environment.json) use an Apple M4, 24 GiB, ten CPUs and synthetic pages with two paragraph blocks each. Source and helper files match candidate 4 in the [binding record](../../evidence/phase5/performance-final/candidate-binding.json).

| Pages | Setup seconds | Keyword search p95 ms | Idle edit commit p95 ms | Maximum usable-shell ms |
| ---: | ---: | ---: | ---: | ---: |
| Empty | — | — | — | 921.15 |
| 100 | 0.11 | 0.98 | 0.70 | 782.92 |
| 1,000 | 0.80 | 3.33 | 0.80 | 810.05 |
| 10,000 | 13.58 | 31.51 | 1.02 | 2,308.32 |

Keyword timings use 100 measured queries per profile, edit timings use 20 commits, and the native hybrid search p95 was **150.07 ms on the 100-page profile only**. Larger hybrid profiles were not measured. Service timings exclude bridge/paint time.

All **twenty fresh app launches** in the [shell benchmark](../../evidence/phase5/performance-final/shell-benchmark.json) passed with networking blocked and no installed models. The maximum was 2.31 seconds, within the recorded interpretation of “approximately two seconds” as at most 2.5 seconds. Each sample uses a new process; OS disk caches were not flushed. These measurements do not establish clean-machine/Gatekeeper time or every workload's responsiveness.

## Native acceptance

The [candidate-4 observations](../../evidence/phase5/native-ui/candidate4-session.json) bind the native session to the final executable hash and synthetic restored workspace. A [read-only canonical checkpoint](../../evidence/phase5/native-ui/candidate4-before-backup-canonical.json) retains the saved state of thirty tables before the remaining backup test.

| Final-candidate native case | Observed result |
| --- | --- |
| Startup and advanced content | Reopened the restored workspace with the Unicode paragraph, H4, original table cells, renamed Review tab and public audio intact. |
| Lock, edit, save and reopen | Lock/unlock stayed Saved; a new marker saved and survived navigation/reopen. |
| Actual clipboard | Verified full-line native Cmd+C/Cmd+V retained `Persistent notes — తెలుగు 中文 العربية 🌱` on a new saved page. |
| Local AI and sources | Qwen2.5 1.5B answered with the exact Original history marker and a citation; expanded source context resolved to the canonical page/block. Memory was off; no generated change was applied. |
| Shared views and formulas | One shared record retained its edited title; live decimal result 0.3 remained distinct from original expression and captured 999 evidence. |
| Automation retry | Re-running the same occurrence retained a single child and succeeded receipt. The synthetic schedule was disabled afterward. |
| Public audio | Playback controls changed Play→Pause and elapsed time advanced from 0 to 4 seconds. |
| Screenshot error recovery | Terminating only the verified screenshot child restored controls and showed an explicit cancellation/denial error. App-bound CUA Escape did not reach the OS picker; ordinary picker cancellation and retained capture remain unverified. |
| Final backup/review/restore, provider controls and subsequent restart | **Pending: macOS locked the screen during acceptance.** Candidate 4 remains open with saved work; an unlock was requested. |

No private microphone audio was recorded and no capture permission was granted. The earlier [candidate-3 observations](../../evidence/phase5/native-ui/candidate3-session.json) passed native backup/review/restore, page-history restoration and provider availability as well as the editor/data flows. Those observations remain bound to candidate 3 and are not substituted for the pending final-candidate checks. Actual final-candidate service backup/restore and separate packaged-process restart already passed in the automated aggregate.

## Optional integrations and remaining scope

Paired local MCP is opt-in and qualified for the tested loopback protocol and reviewed tool boundaries. Notion page/block capture is an explicit opt-in preview with fixture evidence; live account behavior and complete database/schema/view API reconstruction remain unqualified. Managed Claude Code, managed Codex and remote MCP remain disabled. No qualification step introduced credentials, cloud inference or network fallback.

Full P1 approval remains open because the [Phase 4 package limits](../phase-4/implementation.md) still include limited rich nested editor controls, incomplete migration/API breadth, and unestablished general output quality for every new AI mode. The referenced 307-row research audit R00 was not supplied, as recorded in the [requirements ledger](../phase-0/requirements-ledger.json); complete audit parity cannot be inferred from these tests. Physical microphone permissions/capture and retained screenshot capture are separate from synthetic audio and cancellation observations.

The ad-hoc signature, macOS 27/arm64 target, and absence of a separate clean-Mac installation/notarization check are distribution limits. They are not additional requests for user approval. The generated aggregate and archive manifest retain conservative pending gates; this assessment supplies additional measured/native evidence without rewriting the immutable run.

## Failures retained and fixed

Earlier [run attempts](../../evidence/phase5/runs/) remain available. A TypeScript build error and a report-writer argument collision were fixed with regression coverage. A subsequent run was interrupted after native acceptance found the lock/unlock defect. Candidate 3 failed its packaged P0 startup check because Wails could report DOM readiness before workspace initialization; candidate 4 repeats that journey successfully after the synchronization fix.

The original large-workspace benchmark was stopped after proving quadratic whole-workspace block decoding; its [diagnosis and process sample](../../evidence/phase5/performance-diagnosis/diagnosis.json) and [incomplete result](../../evidence/phase5/performance/benchmark-environment.json) are retained. The corrected benchmark and allocation-scaling/cycle/lock regressions pass. No failed or interrupted attempt was promoted as qualifying evidence. See the release guide for all product fixes found in this phase.
