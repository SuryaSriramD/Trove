# Development candidate d9471a8 — 8 October 2026

Trove has a fresh integrated macOS P0 development candidate. The requested AI safeguards, acceptance mappings and measurement tooling are implemented, but **the completion checkpoint remains incomplete and the application is not release-qualified**. Two AI case executions fail, native table responsiveness is unresolved, and native workflows and external release prerequisites remain open.

The [evidence index](../../evidence/completion/macos-p0-candidate-d9471a8/manifest.json) binds retained reports and records their limits. Raw outputs, immutable application/helpers, selected model packs and retained fixtures live outside the checkout under `/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-checkpoint-20261007`. Existing failures were preserved. Later documentation commits do not change the candidate's source identity.

## Frozen identity and implementation

| Identity | Value |
| --- | --- |
| Source commit | `d9471a880a58c19cf2a0542a2db883e77f935df9` |
| Source tree | `788f68661c76f94b95104440dc79cc11c11883e7` |
| Candidate ID | `610d39013b828724d6c30f02e708e0e748028db51ba6a7b455a941eb4de9ef7e` |
| Executable SHA-256 | `f74379c3d941153690ab58293e72478a10bcf4cfd842deb4c0a48f9513bcba07` |
| Frozen at | 7 October 2026, 19:21:27 UTC |
| Target and host | macOS arm64; Apple M4, 10 logical CPUs, 24 GiB; macOS 27.0.1 (26A434) |
| Signature | Ad-hoc development signature |

Packaging used the pinned toolchain, complete frontend output, freshly generated notices and fresh helper/application destinations. Desktop packaging finished rebuilding helpers before the freeze captured their final hashes. Actual build logs and source archive are retained. This establishes recorded provenance, not an independent bit-for-bit rebuild or production signing.

Approved-memory evidence now carries optional canonical `memoryKind` through preparation and review. Preferences supply presentation guidance while keeping supporting quotations available to direct preference questions. Current source facts retain precedence. Legacy saved evidence remains generic approved memory, with canonical kinds loaded during fresh preparation. No database migration or worker-protocol change is required.

The cited-draft validator checks the complete draft before publication, rejects whitespace-normalized duplicate claim text and requires abstention to be the sole claim with empty title and quote. Distinct claims may share a quote. Source-supported negative facts are explicitly distinguished from missing evidence. Existing citation, numeric, permission, freshness and token-budget checks remain; there is one generation attempt and no silent claim deletion, regeneration or model fallback.

All **395 macOS P0 criteria are mapped**, including the previously unmapped 393. Each newly mapped observation scenario has a pinned procedure, fixture/oracle requirements and evidence scope. Two narrow mappings use independently anchored service assertions. All nine target/tier catalogs retain their prior scope. Template generation now projects only accepted observation fields without a schema-version change. Mappings alone do not supply passing acceptance evidence.

Qualification tracing adds bounded service, bridge, two-animation-frame and editing measurements, with separately sampled application/owned-worker memory. The database harness retains a closed fixture outside timed samples and verifies reopening, integrity and seven independent first-page oracles. No database or native UI latency threshold was invented.

## Source checks and packaged correctness

| Check | Final source result |
| --- | --- |
| Service race suite | 624 top-level passes, 50 skips; 1,262 passing nodes including subtests |
| Desktop race suite | 55 top-level passes, one skip; 121 passing nodes including subtests |
| Frontend | TypeScript passes; 49 unit tests pass |
| Tooling / expanded evaluation runner | 222 / 16 tests pass |
| Main browser suite | 110 pass, two optional live Vimeo/YouTube checks skip |
| Database browser suite | 11 pass |

These scopes overlap and are not counts of product requirements. Earlier build/environment/browser failures remain in the source-validation evidence. The editor-selection fixture and unavailable-provider fixture were repaired and independently reviewed without weakening their behavior assertions. Required skipped checks remain pending.

Fresh candidate-bound service regressions pass. The frozen packaged application completed create, separate-process reopen and backup/restore journeys under OS-enforced network denial. A separate packaged installed-runtime run generated chat output, returned 384-dimensional embeddings and transcribed the declared JFK clip. These are actual packaged Wails service/helper journeys; they do not establish the complete visible editor, physical microphone or representative-model acceptance scope.

Closed fixture copies and restore supplements preserve the compared history and asset bytes. The source and backup contained no baseline meeting or transcript rows. Restore finalization added one meeting row; transcript tables remained empty, so populated transcript-timing preservation is not qualified. Explicit recovery of saved audio was not exercised through the native interface.

## AI evaluation

| Unchanged evaluation | Result |
| --- | --- |
| Original P0 corpus | **24/24 pass** |
| New independently specified holdouts | **11/12 pass** |
| Prior fixed modes, strict memory and held-out facts | **20/21 pass**: 14/14, 3/3 and 3/4 respectively |

The original corpus hash remains `3fdb5e0fc556620cda76a9e3c255dbdce35551bbe134cbfea17153eaabbd6fee`; the independently locked 12-case holdout hash is `73dbf59d71582e777c215d2a1664aac629b640bc88fa4e194bb7b0b1d6b6f812`. The same verified Qwen2.5 7B Q3 pack and 8,192/512-token budgets were used. Every case has a terminal outcome; no missing/skipped output or rejected duplicate draft is counted as a repaired answer. The three evaluations contain 55 passing case executions out of 57, not a single passing model gate.

`distinct-facts-shared-quote` returns both correct distinct facts supported by the same quote, but omits the sentence-ending punctuation required by the locked two-complete-sentences oracle. Its expected output and raw draft remain unchanged. `unassigned-owner-supported-negative` incorrectly abstains against evidence stating that the owner has not been assigned. Its restrictive prompt asks for a named person only if one is identified, creating an instruction/expected-negative tension that remains documented; it is still a failed regression.

All 36 original/new corpus cases report unchanged **canonical mutation-history counts**. That check does not compare every canonical row and asset byte. The evidence index includes a wording correction to the already-recorded failed AI observation; the immutable observation and failure receipt were retained. Full inventory preservation requires stronger independent before/after comparisons on a later evaluation, separately from the existing source safeguards and native fixture audits.

## Performance evidence and limits

| Workload | Observed result and qualification limit |
| --- | --- |
| Startup | 80 complete launches, 20 each at 0/100/1,000/10,000 pages; maximum **1,056.692 ms**, all at most 2,500 ms. Diagnostic sampling was disabled. Raw traces survive, but the generated temporary workspaces disappeared after the interruption, so complete fixture traceability remains pending. The historical 8,399.656 ms outlier is not erased. |
| Original search run | 300 keyword samples, maximum **25.56325 ms**; 90 installed-model hybrid samples, maximum **170.282625 ms**, across all three scales. All recorded values meet the existing limits. Reranking, bridge and paint are excluded. Its temporary workspaces later disappeared; raw samples and reports survive. |
| Retained search rerun 2 | Failed during the 10k profile with `CONFLICT: Worker lease expired`; the service report is empty, so no numerical subset is claimed. Power logs confirm sleep during the run. A retained job lease expired long before the dark wake matching failure completion. Suspension is strongly consistent with the failure; exact heartbeat/handler causality was not instrumented. All three failed-run workspaces are retained. |
| Retained search rerun 3 | 300 keyword samples (maximum 38.344292 ms) and 90 installed-model hybrid samples (maximum 237.761625 ms), across 100/1,000/10,000 pages, all meet the unchanged strict limits. The three exact closed workspaces are durably retained and independently verified; this passes the declared synthetic service workload only |
| Database | All nine correctness profiles pass. Seven iterations per measured operation were recorded with exact samples. Formula and rollup first-page p95 values are **1,150.90 ms** and **982.39 ms**. These are service observations, not a pass against an agreed database latency limit. The 10,000-record fixture is durably retained and independently reopened. |
| Native table | Second attempt completed normal quit and canonical comparison, but load-to-two-frame observations reached **16,202 ms**, response-to-two-frame reached **16,084 ms**, and scroll-to-two-frame reached **5,664 ms**. Full paging/workflow accounting is incomplete. Responsiveness remains unresolved. |
| Native editing | Five separately saved paragraph edits were observed before the Mac locked. Input-to-two-frame observations reached **91 ms**; this interrupted partial workflow does not qualify editing, clipboard, IME or restart acceptance. |

Database dispatch, bridge and frame measurements have different boundaries. Animation-frame callbacks are scheduling opportunities, not physical paint or hardware input latency. Native frame/input sample accounting is explicitly incomplete; accessibility automation and visibility effects were not isolated. No new numeric UI/database acceptance limit is asserted.

Sampled application RSS maxima were 235,765,760 bytes for the completed table attempt and 161,939,456 bytes for editing. No owned model worker was observed during those UI attempts. WebKit/GPU/shared-process memory and unsampled peaks are outside the measurement, so these figures do not qualify whole-application or concurrent-AI memory bounds.

Source inspection identified two investigation targets: relation cells populate native select controls with all target records, multiplying large option lists across visible rows; the 60-second refresh key resets paging. These are plausible contributors to long rendering and observed page resets, not isolated profiler proof. No repair to those paths is included in this candidate.

## Native observations, diagnostics and retained failures

The completed table attempt displayed independently expected precise decimals, dates, Unicode, literal filtering and multi-select/missing-number results. Its declared 48-table canonical inventory and original assets compare equal; operational/cache tables are explicitly excluded. Three fully settled paging cycles were not established in that attempt. The earlier table attempt timed out and its missing scratch directory prevents canonical comparison.

The editor reached the seed paragraph plus five separately saved edits, with corresponding history states verified after closure. The full predeclared nine-paragraph/rich-clipboard oracle remains failed/incomplete; the six-paragraph partial audit does not replace it. The original temporary audit helper disappeared, so its reconstructed post-observation implementation and that verification limitation are explicit. Actual table/editor workspace copies and failed reports remain retained.

The Mac locked before undo/redo, formatted/Unicode clipboard, search/citation navigation, separate editor restart and remaining workflows could be observed. Manual unlock was requested; it has not been confirmed. Model/memory controls, visible backup/restore, concurrent indexing/AI/editing, cancellation, recovery, IME/VoiceOver and physical microphone workflows remain pending. Five successful saves do not imply those journeys passed.

Packaged startup traces exercised the sanitized startup path. The current candidate has not completed the native operational-diagnostics preview/export workflow or full startup/resource/worker diagnostics coverage. Prior candidates' support-export receipts are not imported.

## Qualification and next required work

Two of 55 required gates pass (P0 service regressions and build provenance); one fails (local AI), two are unqualified (native editor and performance), and 50 remain pending. Two of 395 criteria have passing evidence, with zero unmapped macOS P0 criteria. The final identity/evidence check reports no errors. `releaseQualified` and `releaseApproved` both remain false

The immediate follow-up is to repair the two surviving AI behaviors and investigate the table rendering/paging findings with isolated diagnostics. Any source, application, signing or policy change requires a new candidate and fresh affected evidence. Then repeat startup with retained fixtures on an unlocked Mac and complete the declared native/concurrency journeys. Keep the unchanged answer oracles, model and budgets; add stronger canonical inventory comparisons without replacing existing checks.

Independent clean-Mac installation and second-installation restore remain blocked by the unavailable separate Mac. Production Developer ID signing/notarization, representative authorized datasets, physical microphone workflows and complete distribution/update/rollback evidence remain pending. P1 live providers/Notion/HTTPS MCP, expanded P2 evaluation, Windows/Linux native qualification and the missing research report remain separate open workstreams. The backlog still records **34 implemented at documented scope, 11 in progress, five open and one awaiting research**; those counts are not release acceptance.
