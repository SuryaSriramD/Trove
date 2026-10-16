# Trove — project progress

**The single current development and progress tracker for this project.** Read and update this file for every meaningful implementation change, investigation, test result, decision, blocker and handoff. This is not an automatically running monitor.

Last updated: **2026-10-16 11:59 IST**. User-facing dates use **Asia/Kolkata (IST, UTC+05:30)**; source artifacts may retain UTC timestamps.

This file consolidates the original 51-item backlog, the newly selected desktop scope, current qualification, next actions and development history. Earlier status pages are historical snapshots. Machine-readable contracts, the legacy registry and immutable qualification records remain supporting inputs/evidence; they are not additional human progress dashboards. Their frozen bytes must not be rewritten to make a status look current.

[Current snapshot](#current-snapshot) · [Next actions](#next-actions) · [Decisions](#decisions) · [Milestones](#milestones) · [Original 51 items](#original-backlog) · [New desktop work](#new-desktop-work) · [Detailed workstreams](#detailed-workstreams) · [Blockers](#blockers) · [Update rules](#update-rules) · [Development log](#development-log) · [Commit history](#commit-history) · [Evidence](#evidence)

<a id="current-snapshot"></a>
## Current snapshot

**The full macOS feature integration is built; quality and release acceptance remain incomplete.** No platform/tier is release-qualified or approved. The new development candidate is separate from candidate 7; historical results do not become passes for changed source.

| Measure | Recorded result | Meaning |
| --- | --- | --- |
| Original backlog | 51 items: 34 source-implemented, 11 in progress, five open, one awaiting source | These are the legacy registry's scoped implementation labels, reconciled here on 13 October. They are not 34 release-complete features. |
| Latest development package | Repaired full macOS P2 development app, source `6d017cf`, candidate `0434852918824066e1fa950917a8a0b7fd13fd18e0f0c19ea80dc23d1f1d82db` | Fresh build; offline packaged P0/P1/P2, two native instruction/tokenizer checks and one image/OCR smoke pass. Ad-hoc signatures verified. Formal packaged-journey receipt passed; **1/66 passed, 65 pending**, no current receipt errors (DEV-060). |
| Preserved prior feature package | Source `9a39485`, candidate `42d3dd5a5c3186b05d1859ccc19f6916d3e67eda446a062c670298c322b444d9` | Original app unchanged. Its two passed receipts and 64 pending checks remain scoped to that candidate; they are not transferred to the repaired package. |
| Frozen `9a39485` consolidated model campaign | **113 passes, 25 failures, 12 required-input skips** across 150 declared cases | All eight families finished. Includes evaluator limitations, substantive output/ranking defects and service-boundary cases; this is not a feature-completion percentage. Frozen inputs/canonical evidence remained unchanged. Independent review, human usability and release acceptance remain separate (DEV-051). |
| Historical candidate 7 gates | **3 passed / 52 pending / 0 failed**, out of 55 | Most required work is unexecuted. Zero failed receipts does not mean historical defects were fixed. |
| Candidate 7 macOS P0 criteria | **395 mapped / 2 proved** | Mapping defines the test; it does not supply a pass. This is not a percentage of working product features. |
| Source checks for candidate 7 | 654 service race passes, 54 skips; 59 desktop race passes, one skip; 56 frontend unit passes; TypeScript and frontend build; 238 tooling passes; four affected browser passes | Recorded source checks, not rerun by this tracking update. Required skips remain unresolved. |
| Candidate 7 completed gates | Service regressions, build provenance, packaged P0 journey | Actual create/edit/save/reopen, backup/restore and OS-denied-network journeys; installed chat/embedding/speech helper smoke; six independent content/asset preservation assertions. |
| Candidate 7 still unexecuted | AI: 101 cases and 51 manual assessments; fresh startup/search/database; required foreground workflows | Exact handoffs exist. No receipt can be inferred from a prepared procedure. |
| Historical candidate 6 failures | 78/89 combined AI passes; 11 failures. One startup took 6,252.767 ms against 2,500 ms | Keep as historical failures. Candidate 7 has no adopted production prompt/context repair. |
| Release state | `releaseQualified=false`, `releaseApproved=false` | Signing/notarization, native/device, representative-data, clean-install and other gates remain open. |

Candidate 7's focused 22-case prompt experiment was rejected and production prompts were restored. A later startup diagnostic did not reproduce the earlier outlier and did not qualify performance. The package omits two AppleDouble metadata sidecars from the helper source tree; common helper bytes match and the inventory difference is disclosed in its build evidence.

**Item-count percentages requested on 15 October (DEV-009):** Original backlog source labels are 34/51 implemented (**66.7%**) and 17/51 unfinished (**33.3%**): 11 in progress (21.6%), five open (9.8%) and one awaiting source (2.0%). Candidate-7 gate execution is separately 3/55 passed (**5.5%**) and 52/55 pending (**94.5%**). At that historical checkpoint, none of the 11 active G groups had completed acceptance; four had contract/specification/foundation progress and seven were planned. Later implementation is recorded below; those old percentages are not the current feature-completion estimate. G11 remains deferred. These are proportions of named item sets, not effort, time remaining or overall product readiness; overlapping legacy/G/CONN rows cannot be added into one completion denominator. All original final candidate/platform acceptances remain pending. Percentages are rounded to one decimal.

### Source and checkout identity

| Location | Branch / source | Current interpretation |
| --- | --- | --- |
| Primary project: `/Volumes/HD-B1/HD-Projects/Trove` | `codex/macos-feature-completion`, `6d017cf` plus DEV-060 package/evidence record; repaired app `6d017cf` | Repaired P2 app built and independently audited. Retained mode/review/runtime/query fixes have bounded evidence; answerability, diagram, expansion, relevance and vision quality gaps remain open. Original evidence and rejected experiments are preserved. No release approval. |
| Frozen new-candidate execution checkout: `/Users/suryasriramduvvuri/.codex/worktrees/feature-canonical-tasks/Trove` | Detached `9a3948529a1e772b1e5fdb710203e2c1d234d3cc` | Reuses the clean completed worker checkout; original branch `codex/feature-layout-scheduling` remains at `8b739d5`. Completed frozen campaign executed here against the development app helpers; preserve this checkout and its evidence. |
| Latest development worktree: `/Users/suryasriramduvvuri/.codex/worktrees/macos-p0-title-context/Trove` | `codex/macos-p0-title-context`, `299e43df9b716f771125d939ad320c3cb595f2c7` | Candidate 7 source. One descendant commit containing tests, fixtures, telemetry and execution documentation. |

Keep candidate 7's execution in its declared worktree. Review and integrate the descendant commit separately; do not rebuild a package just to align a checkout. A product/binary/policy change requires a new candidate and affected fresh evidence.

### What this update actually completes

- Consolidated current progress and the agreed plan into this file.
- Preserved all original item IDs, owners, phases and acceptance statements.
- Added individually trackable new desktop features, connectors, evaluations, P2 and platform work.
- Added an append-only development log and the complete 133-commit reachable primary/candidate-7 lineage as of this update.
- Established project instructions to keep this file current; older status pages point here.
- Added the [PRD scope addendum](docs/PRD_SCOPE_ADDENDUM.md), covering G01–G12, the six connector adapters and 36 uniquely identified acceptance scenarios while retaining existing decisions and delivery boundaries.

Consolidation validation recorded at DEV-005 (not rerun for the scope addendum): **passed**. The completion validator reported 51 backlog items, 312 product mappings, 26 explicit contract-boundary exclusions, 41 coverage packages and six historical candidate records, with no errors and `releaseQualified=false`. Those structural checks confirmed all 51 original acceptance statements, all new workstream rows, 112 valid local/navigation links and 133 unique recorded commits. Independent review corrections were incorporated and whitespace checks passed.

Scope-addendum validation at DEV-007: **passed for documentation only**. Checks covered G01–G12, CONN-00–06, 36 unique acceptance IDs, local links and whitespace; independent contract/scope review passed. Original PRD/TRD SHA-256 hashes are unchanged. Machine-readable acceptance mapping was pending at DEV-007; see DEV-013 for the new implementation-workstream mapping. No product tests, model runs, benchmarks, app launches, source integration or release actions were performed.

<a id="next-actions"></a>
## Next actions

| Order | ID / scope | Status | Next concrete action | Exit evidence |
| --- | --- | --- | --- | --- |
| 1 | TRACK-001: progress consolidation | Done | Maintain this file after every meaningful development; continue with source alignment below | All 51 original acceptance statements, G01–G12 and child workstreams accounted for; structural/link checks, completion validator and independent review complete |
| Documentation | TRACK-002: PRD scope addendum | Done | Use the [addendum](docs/PRD_SCOPE_ADDENDUM.md) during G/CONN implementation; add versioned machine mappings with the affected contracts before qualification | G01–G12, CONN-00–06 and 36 acceptance scenarios documented; independent scope review and structural/link checks passed; original PRD/TRD unchanged |
| Active | IMPL-001: full macOS functionality | In progress | Execute D-P11–14: align source; implement shared foundations, G01–G10, six connectors/EXT and all nine P2 packages with focused checks | Integrated UI/services/persistence/recovery and focused evidence per feature; live/native acceptance remains separate |
| 2 | GOV-001 / GOV-003: source alignment | Integrated baseline | Continue implementation from merge `ee14f0c`; retain candidate-7 evidence/worktree | Reviewed diff, source identity and affected validation; candidate-7 artifacts preserved |
| 3 | G01 / G02 / G08–10: shared foundations | Integrated; acceptance pending | Verify retained three-way and identity review, canonical task editing/detachment, grouped undo and semantic freshness in the combined app | Meaningful conflict/retry/preservation tests and versioned contracts |
| 4 | G03 / G05 / G06 | G03/G05/G06 integrated; native acceptance pending | Encrypted portable backup, recording controls/correction/retention and reminder-only helper | Usable native workflow plus failure/recovery evidence |
| 5 | G07–10 / AI-* / EVAL-* | G07–10 and evaluation runners integrated; full available-input campaign finished with failures | Finish independent assessment and controlled relevance/mode repairs; preserve frozen results and required-input skips | Focused current-source checks; full historical candidate-7 baseline is not a prerequisite |
| 6 | G01 / CONN-00–06 / EXT-* / P2-01–09 | Initial adapters, approved connected AI, task detachment, identity review and desktop/P2 continuity integrated and built | Qualify combined workflows, real accounts/devices and representative data separately; retain all quality failures | Production integration; real-account/device prerequisites tracked separately |
| 7 | REL-001–008: macOS release | After functionality integration | Complete final candidate, native/device, signing/install/recovery and bounded performance qualification | Exact signed artifact with all applicable criteria satisfied |
| 8 | WIN-* / LIN-* / G11 | Subsequent platforms; mobile deferred | Implement/qualify Windows and Linux after full macOS functionality; revisit mobile only after desktop | Independent platform evidence and explicit later mobile decision |

Product correctness and usable features lead the work. Run focused regressions during development, and the declared full performance matrix at a release milestone. Additional startup diagnostics require a reproduced problem. On a busy-host refusal, record the reason once and work on independent tasks until conditions change; do not repeatedly poll or build another candidate for an unchanged prerequisite.

<a id="decisions"></a>
## Decisions and scope

These are directly confirmed by the user in this chat on 13 October 2026 unless identified otherwise.

| ID | Decision | Effect |
| --- | --- | --- |
| D-P01 | Deliver macOS first; keep Windows and Linux in the completion programme | Other-platform qualification does not block a macOS-only first release. |
| D-P02 | Include the research proposals, with desktop as the immediate focus | G01–G12 are accounted for below; mobile implementation is deferred. |
| D-P03 | Use OS disk protection plus encrypted portable backups | No application-managed workspace encryption/SQLCipher migration is selected. |
| D-P04 | Build a broad connector framework with Google Calendar, Drive, Gmail, GitHub, Notion and outbound MCP | Six named adapters and shared infrastructure must be tracked separately. Microsoft 365 is not selected. |
| D-P05 | Read/refresh selected external sources; explicitly review external writes | No automatic two-way edit synchronization or unapproved external mutations. |
| D-P06 | Background reminders while the main desktop app/window is closed | Opt-in local reminder delivery and lifecycle work are required. |
| D-P07 | Preview dependency-driven rescheduling | Show the affected dates; apply and undo only the reviewed change. |
| D-P08 | One project file tracks progress and every development | This file is the sole current human tracker; update it with each meaningful work outcome. |
| D-P09 | Productivity-first emphasis; user questioned system benchmarking | Measure Trove's experience and investigate actual failures. No new minimum-RAM target was selected. |
| D-P10 | User requested a Markdown addendum adding the annotated Notion-review features to the existing PRD | [Addendum v1.0](docs/PRD_SCOPE_ADDENDUM.md) formalizes G01–G12 and their acceptance without declaring implementation complete or overriding the more specific D-P01–09 choices; mobile remains deferred under D-P02/G11. |
| D-P11 (15 Oct) | Implement functionality first, with focused correctness/native feasibility checks; broad release campaigns follow integration | The full unfinished candidate-7 baseline does not block development; frozen evidence stays unchanged and before/after repair evidence remains required. |
| D-P12 (15 Oct) | First functionality-complete target is full macOS P0/P1/P2, all six connectors and existing managed-provider/MCP scope | Windows/Linux follow; mobile remains deferred. Implementation, live/native acceptance and release approval stay separate. |
| D-P13 (15 Oct) | Dependency rescheduling offers calendar days and a configurable working week; default calendar days | Preserve duration in the selected calendar, explicit timezone, zero-lag finish-to-start, full preview/atomic apply/grouped undo. |
| D-P14 (15 Oct) | Missed reminders enter the inbox and produce one deduplicated catch-up summary per recovery episode | No burst of individual overdue alerts; reminder helper retains derived state only. |
| D001 (existing) | Tasks are canonical record facets; todo blocks are references | Preserve the accepted contract; G02 is reconciliation/acceptance, not a redesign. |
| D002 (existing) | Re-import is a three-way merge with retained baseline and explicit conflict review | Reconcile all importer paths against this contract; missing/denied source is not deletion. |

**Implementation proposals from the completion plan, not delivered features:** a reminder-only agent with derived state and acknowledged schedules; age-encrypted native backup container; manual skills/autofill; finish-to-start rescheduling with explicit date/calendar semantics; selected-file Drive access; Notion MCP plus direct import token. Verify against current code and vendor contracts before implementing. Changes to these details must be logged here.

The plan proposed 90% representative manual task success and English WER targets of 15% clean / 25% noisy. Those numbers have **not** been individually accepted or inserted into qualification policy. G04 must review and lock new rubrics before running them; never weaken existing oracles or retroactively turn a measured result into its own target. Existing 2,500 ms shell, 300 ms keyword and 1,000 ms hybrid harness limits remain unchanged. No generic hardware minimum is claimed.

The October 12 report covers 39 Notion feature labels and 12 recommendations. It is available. The separately referenced original 307-behavior report is still missing; the new report does not silently replace it.

<a id="milestones"></a>
## Delivery milestones

| Milestone | Scope | Status | Completion boundary |
| --- | --- | --- | --- |
| M0 — Consolidated project | Source alignment, this tracker, requirements and new scope | In progress | One current source baseline and progress record; evidence/owners/acceptance mapped |
| M1 — Core correctness | Existing FIX/CORE, AI defects, preservation, missing evaluation runners | In progress | Actual failures repaired with independent regressions and no dropped acceptance |
| M2 — macOS P0 | Native workflows, real migration/audio, meeting controls, backup/install/recovery/distribution | Unqualified | Final macOS P0 candidate and all applicable criteria pass |
| M3 — Full personal desktop | Existing PARITY/AI actions plus G06–G10 | Integrated source; focused verification and acceptance pending | Packaged native behavior, history, restart and recovery verified |
| M4 — Live integrations | Existing managed providers/inbound MCP and CONN-00–06 | Implementation integrated; live acceptance pending | Authorized live flows, revocation, resume and reviewed effects verified |
| M5 — P2 packages | P2-01–09 and expanded model/image/review work | Implementation integrated; model/native qualification pending | Each package passes its declared persistence, authority and recovery scope |
| M6 — Windows | Windows 11 x64 native implementation and release | Foundation only | Native durability, containment, devices, installation/update and portability pass |
| M7 — Linux | Ubuntu 24.04 x64 native implementation and release | Foundation only | Native containment, PDF/providers, devices, installation/update and portability pass |
| M8 — Final reconciliation | All accepted scope, release records, support claims and research | Pending | No accepted item silently omitted; blockers and deliberate exclusions explicit |

Preparation and independent implementation may run in parallel. Use one integration coordinator and up to three workers in separate worktrees. Serialize shared-Mac inference and controlled measurements. Shared contracts/migrations need integration review. P1/P2/provider work does not become an accidental P0 prerequisite.

<a id="original-backlog"></a>
## Original backlog — all 51 items

This section is the current human-facing item register. Initial source labels were imported from the legacy registry dated 8 October and checked against newer evidence. **Every item's final candidate/platform acceptance is still pending.** Supporting documents below are scoped implementation evidence, not automatic closure. The original acceptance wording is retained so progress cannot quietly reduce the task.

Owners: **A** kernel/contracts/jobs/integration; **B** editor/navigation/interface; **C** databases/properties/views/formulas; **D** migration/files/history/recovery; **E** search/AI/memory; **F** native/audio/models/packaging/platforms. These are responsibility lanes, not claims that an agent is currently running.

### Correctness repairs

<a id="fix-001"></a>
#### FIX-001 — Exclude trashed-only attachments from portable exports

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** D. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A trashed-only or otherwise unreferenced private asset is absent from current-content exports; a shared asset still referenced by a live page remains present; Markdown/CSV and HTML exports behave consistently. Verify archive bytes, not just manifest counts.

**Supporting evidence:** [recovery.md](docs/completion/recovery.md).

<a id="fix-002"></a>
#### FIX-002 — Restore meeting history when transcript segments still exist

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, D. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Ordinary meeting restore and recovery after transcript deletion both pass. Test existing, edited, deleted, stale, and unauthorized segments; preserve timing and audio references; do not rewind unrelated records.

**Supporting evidence:** [recovery.md](docs/completion/recovery.md).

<a id="fix-003"></a>
#### FIX-003 — Preserve rich formatting and links during table and layout edits

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Import a cell and layout paragraph containing bold text and links, change one character through the visible control, save, reopen, undo/redo, export, and restore. Unaffected formatting and targets remain intact. Test the actual change handlers, not only serialization helpers.

**Supporting evidence:** [rich-layouts.md](docs/completion/rich-layouts.md), [browser-tests.json](evidence/completion/checkpoint-1/browser-tests.json).

<a id="fix-004"></a>
#### FIX-004 — Open the correct linked database view on its owning page

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, C. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Two pages can display different filtered views of one collection; each opens with its own configuration. Multiple views on the same page remain distinct while sharing the same records through rename, move, restart, and restore.

**Supporting evidence:** [views.md](docs/completion/views.md), [view-config-tests.json](evidence/completion/checkpoint-1/view-config-tests.json).

<a id="fix-005"></a>
#### FIX-005 — Make copied block links navigable

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, F. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A copied link pasted into another page opens and focuses the correct block. External launch routing is tested in the packaged app. Missing/deleted targets and unavailable workspaces produce useful errors. Existing search-citation navigation remains functional.

**Supporting evidence:** [platform-desktop-packaging.md](docs/completion/platform-desktop-packaging.md), [manifest.json](evidence/completion/platform-desktop-packaging/manifest.json).

<a id="fix-006"></a>
#### FIX-006 — Correct nested toggle visibility and collapse persistence

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B. **Phase:** C1.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A toggle containing nested lists, paragraphs, and technical blocks hides all descendants; reopening restores them intact. Save/reopen, moving children, and undo/redo preserve the agreed collapse behavior.

**Supporting evidence:** [rich-layouts.md](docs/completion/rich-layouts.md), [browser-tests.json](evidence/completion/checkpoint-1/browser-tests.json).

### P0 product foundation

<a id="core-001"></a>
#### CORE-001 — Complete simple table structure controls

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Users can grow and shrink tables without dropping populated cells silently; header rendering, undo, import, export, and restart preserve structure. P1 merge/unmerge continues to preserve original cell content.

**Supporting evidence:** [browser-tests.json](evidence/completion/checkpoint-1/browser-tests.json).

<a id="core-002"></a>
#### CORE-002 — Add rich-text colors and highlights

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, B. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Supported colors/highlights survive editing, mixed paste, undo, restart, and portability. Older content remains compatible; unsupported incoming values are retained safely.

**Supporting evidence:** [selected-stack-alignment.md](docs/completion/selected-stack-alignment.md), [browser-tests.json](evidence/completion/checkpoint-1/browser-tests.json).

<a id="core-003"></a>
#### CORE-003 — Finish code and image block controls

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Controls work in the packaged app, source bytes remain unchanged, settings survive restart/round trips, and file access remains scoped to managed IDs.

**Supporting evidence:** [selected-stack-alignment.md](docs/completion/selected-stack-alignment.md), [browser-tests.json](evidence/completion/checkpoint-1/browser-tests.json).

<a id="core-004"></a>
#### CORE-004 — Connect workspace Q&A to hybrid retrieval

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** E. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Paraphrased/conceptual questions retrieve relevant differently worded notes. Scope restrictions, current-source checks, deleted evidence, memory invalidation, and citation navigation remain correct. Include a real local-model/desktop acceptance case.

**Supporting evidence:** [retrieval.md](docs/completion/retrieval.md), [richtext-search.md](docs/completion/richtext-search.md).

<a id="core-005"></a>
#### CORE-005 — Decode and normalize common uploaded audio

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** F, E. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Supported MP3/M4A/AAC and ordinary stereo/44.1/48 kHz WAV inputs are normalized and transcribed locally. Keep originals; bound resource use; deny arbitrary network inputs; support cancellation/retry and recoverable unsupported/corrupt inputs. Publish the exact supported codec matrix.

**Supporting evidence:** [audio-decoding.md](docs/completion/audio-decoding.md), [c7-c8-audio-evidence.json](evidence/completion/c7-c8-audio-evidence.json).

<a id="core-006"></a>
#### CORE-006 — Provide schema-aware historical value restoration

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, C. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A schema change exposes incompatible old values in a preview; the user can map or deliberately retain them as evidence. Applying restoration creates a new revision, preserves the current recovery point, and does not rewind shared schemas or unrelated records.

**Supporting evidence:** [recovery.md](docs/completion/recovery.md).

<a id="core-007"></a>
#### CORE-007 — Complete database query scaling and virtualization

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, C. **Phase:** C2, C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Large representative collections load incrementally with bounded memory and visible-row rendering. Typed filters, formula/rollup behavior, stable sorting, scopes, and cursor invalidation remain correct. Measure bridge/paint as well as service time; do not reuse page-search benchmarks as database-view evidence.

**Supporting evidence:** [database-query-scaling.md](docs/completion/database-query-scaling.md), [database-performance.md](docs/completion/database-performance.md).

<a id="core-008"></a>
#### CORE-008 — Honor core view configuration and calendar range editing

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** C. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Property order, width, visibility, sort/group configuration, and calendar ranges survive restart and linked-view use. Resizing changes duration through guarded mutations and preserves timezone semantics.

**Supporting evidence:** [views.md](docs/completion/views.md).

<a id="core-009"></a>
#### CORE-009 — Resolve semantic index implementation and scale

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, E. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Exact model/hash/dimension generations are maintained; incompatible changes rebuild derived indexes without affecting notes; scope/revision checks hold; large-corpus latency and memory meet the declared benchmark profile.

**Supporting evidence:** [retrieval.md](docs/completion/retrieval.md).

<a id="core-010"></a>
#### CORE-010 — Resolve the backup snapshot contract

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, D. **Phase:** C2.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Concurrent edits and recording remain within documented responsiveness bounds; database/asset consistency, cancellation, insufficient disk, crash recovery, staged restore, and source preservation pass. Do not label existing backups corrupt solely because their implementation differs.

**Supporting evidence:** [recovery.md](docs/completion/recovery.md), [pre-migration-snapshots.md](docs/completion/pre-migration-snapshots.md).

### P1 workspace features

<a id="parity-001"></a>
#### PARITY-001 — Complete rich layout and reusable-block editing

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Code, media, tables, and nested content can be edited through supported layout controls without flattening. Identity, moves, duplication, source updates, cycle rejection, undo, preservation, and recovery remain correct.

**Supporting evidence:** [rich-layouts.md](docs/completion/rich-layouts.md).

<a id="parity-002"></a>
#### PARITY-002 — Complete bookmarks and optional online embeds

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, D. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** No network request occurs without the intended explicit action. Supported sources display in a restricted context, unsafe content remains inert, and offline/source-unavailable behavior uses the cached fallback. Never execute imported HTML automatically.

**Supporting evidence:** [online-content.md](docs/completion/online-content.md).

<a id="parity-003"></a>
#### PARITY-003 — Complete timeline behavior

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** C. **Phase:** C4.

**Next:** Qualify existing visual timeline contract. G10 adds a separately reviewed rescheduling operation and does not retroactively change original acceptance.

**Done when:** Scale changes alter the visible time axis meaningfully; saved grouping and dependency relationships are visible; date ranges and configured ordering are respected after restart. No scheduling optimizer is required.

**Supporting evidence:** [timeline.md](docs/completion/timeline.md).

<a id="parity-004"></a>
#### PARITY-004 — Unify database templates with values and page bodies

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, C. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A selected template creates a new page-backed record with both parts, fresh/remapped identities, preserved references, normal approval/revision rules, and no accidental shared mutable template content.

**Supporting evidence:** [unified-templates.md](docs/completion/unified-templates.md).

<a id="parity-005"></a>
#### PARITY-005 — Close the formula compatibility contract

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** C. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Independent expected-output fixtures cover every claimed feature; original expressions and captured values remain distinct from live results; unsupported expressions preserve evidence and report useful errors. Full proprietary Notion-function parity is not required.

**Supporting evidence:** [formula-compatibility.md](docs/completion/formula-compatibility.md).

<a id="parity-006"></a>
#### PARITY-006 — Complete Notion API database and source reconstruction

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** C, D. **Phase:** C4.

**Next:** Qualify real authorized Notion roots, multiple sources, pagination, attachments, disconnect and re-import conflicts; CONN-05 adds connected actions separately.

**Done when:** Multiple sources in one container remain distinct; linked views share canonical records; interrupted pagination/downloads resume without duplication; permissions never look like empty content; original formulas/caches/provenance remain separate; captured content works after disconnect.

**Supporting evidence:** [notion-api-import.md](docs/completion/notion-api-import.md).

<a id="parity-007"></a>
#### PARITY-007 — Preserve reconstructable nested import structure

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, D. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Supported source structure is reconstructed; unavailable or unsupported semantics retain raw/readable evidence and accurate partial outcomes. Known omissions and unknown completeness stay separate. Do not invent source metadata or claim unavailable comments/transcripts were captured.

**Supporting evidence:** [import-structure.md](docs/completion/import-structure.md).

<a id="parity-008"></a>
#### PARITY-008 — Render usable rich page PDFs

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, D. **Phase:** C4.

**Next:** Qualify macOS rich PDF output and Windows native helper; Linux extraction/export remains disabled until LIN-01/02 containment passes.

**Done when:** Export representative rich pages offline; inspect pagination, clipping, missing glyphs, and attachment handling; retain Unicode and readable fallbacks. Pixel-perfect Notion PDF reproduction is not required. State platform support explicitly.

**Supporting evidence:** [rich-pdf.md](docs/completion/rich-pdf.md), [portable-documents.md](docs/completion/portable-documents.md).

<a id="parity-009"></a>
#### PARITY-009 — Deliver the browser clipper workflow

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** B, D. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Pairing, source provenance, sanitization, selected destination, explicit resource acquisition, revoked pairing, unavailable app, and offline retained content are tested. No unauthenticated arbitrary-write endpoint is introduced.

**Supporting evidence:** [browser-clipper.md](docs/completion/browser-clipper.md).

<a id="parity-010"></a>
#### PARITY-010 — Expose usable automation configuration

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, B. **Phase:** C4.

**Next:** Qualify existing local rule UI/lifecycle. Background reminder delivery in G06 is new scope, not proof of an always-on automation engine.

**Done when:** Users can configure the documented subset without hand-authoring internal commands; preview/run history/pause/retry work; origin exclusions, DST, deduplication, sleep/restart, and bounded catch-up remain correct. No always-on service or unrestricted vendor automation parity is required.

**Supporting evidence:** [automation-configuration.md](docs/completion/automation-configuration.md).

### Reviewed AI and intelligence

<a id="ai-001"></a>
#### AI-001 — Complete reviewed workspace-agent actions

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, E. **Phase:** C4.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A representative natural-language task becomes an understandable proposal, is applied only through approved scoped services, and handles cancellation, stale revisions, retries, failure, and partial progress without duplicate effects. Code and arbitrary shell commands are never executed by default.

**Supporting evidence:** [reviewed-actions.md](docs/completion/reviewed-actions.md).

<a id="ai-002"></a>
#### AI-002 — Qualify context budgeting and every exposed AI mode

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** E, F. **Phase:** C2, C4, C6.

**Next:** Complete candidate-7 AI101/manual review and EVAL-02/03; repair actual failures; qualify native UI budgets and representative quality. Older 21/24 registry results are historical, not latest.

**Done when:** Long inputs have useful bounded behavior and explicit omissions; code replacement cannot drop omitted source; malformed outputs stay drafts; citations resolve to supplied current evidence; no silent cloud fallback. Record correctness, citation validity, latency, memory, and correction effort rather than relying only on schema-valid output.

**Supporting evidence:** [ai-context.md](docs/completion/ai-context.md), [related-content.md](docs/completion/related-content.md).

<a id="ai-003"></a>
#### AI-003 — Qualify memory and semantic related-content workflows

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** E. **Phase:** C2, C4, C6.

**Next:** Complete EVAL-04/05 and candidate-7 cases; verify source change, deletion, revocation, forgetting and restore with real retrieval. Candidate-6 11 failures remain historical.

**Done when:** Memory improves the declared task set without overriding newer authoritative records; stale/forgotten entries remain excluded during concurrent changes, including entries restored in those states; decisions cite evidence. Explain that restoring an older backup can recover earlier memory states: forgetting does not retroactively erase historical backups. Semantic and lexical suggestions are distinguishable and scope-safe. Existing memory lifecycle services remain the foundation.

**Supporting evidence:** [ai-context.md](docs/completion/ai-context.md), [development-candidate-4900d4b.md](docs/completion/development-candidate-4900d4b.md).

### Existing provider and MCP integrations

<a id="ext-001"></a>
#### EXT-001 — Complete managed Claude Code execution

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** A, F. **Phase:** C5.

**Next:** Qualify actual authorized Claude login, inference, cancellation, compatible resume and revocation; complete Windows/Linux containment before advertising those targets.

**Done when:** A supported real account completes a scoped task; denied tools, interrupted output, process failure, stale changes, revocation, and disconnect are handled correctly. Native filesystem/shell capabilities cannot bypass Trove permissions. Do not copy subscription credentials or enable the mode before containment is enforceable.

**Supporting evidence:** [claude-runtime.md](docs/completion/claude-runtime.md), [claude-profiles.md](docs/completion/claude-profiles.md).

<a id="ext-002"></a>
#### EXT-002 — Complete managed Codex execution

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** A, F. **Phase:** C5.

**Next:** Qualify actual authorized Codex account/model selection, inference, interrupted/completed resume, refresh/revocation and handoff.

**Done when:** A real completed request proves account/model eligibility; refresh/resume, account switching, denied tools, expired/revoked authorization, interrupted streams, and cancellation pass. Credentials remain out of logs/workspace/model prompts. API billing is never an automatic fallback.

**Supporting evidence:** [managed-runtimes.md](docs/completion/managed-runtimes.md), [codex-siwc-auth.md](docs/completion/codex-siwc-auth.md).

<a id="ext-003"></a>
#### EXT-003 — Add reviewed MCP page and task creation

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A. **Phase:** C4, C5.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Authorized page/task creation works after review; invalid destinations, repeated requests, revoked grants, stale parents, and disconnected clients cannot create or duplicate content. Delete and arbitrary-file access retain their separate policies.

**Supporting evidence:** [reviewed-actions.md](docs/completion/reviewed-actions.md).

<a id="ext-004"></a>
#### EXT-004 — Complete the supported remote MCP route

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, F. **Phase:** C5.

**Next:** Qualify the existing inbound remote MCP service with reachable trusted HTTPS and a real client; CONN-06 outbound MCP is separate new work.

**Done when:** Supported real-client connectivity, authentication/revocation, scope, approvals, disconnect, failure recovery, and credential handling pass. The bridge still forwards to the one owning kernel; local workflows work with the connection disabled.

**Supporting evidence:** [remote-mcp.md](docs/completion/remote-mcp.md), [managed-runtimes.md](docs/completion/managed-runtimes.md).

<a id="ext-005"></a>
#### EXT-005 — Qualify provider handoffs and client interoperability

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** A, E. **Phase:** C5.

**Next:** Run real cross-provider/client handoffs, account/scope/version checks, cancellation and repeated/interrupted requests without duplicate effects.

**Done when:** Handoffs exclude secrets, hidden reasoning, unrelated history, and uncommitted changes. Resume remains provider/account/version compatible. One active mutating provider run is enforced; duplicate retries cannot repeat effects. No unannounced provider/billing/context switch occurs.

**Supporting evidence:** [managed-runtimes.md](docs/completion/managed-runtimes.md), [mcp-sdk.md](docs/completion/mcp-sdk.md).

### Release and qualification

<a id="rel-001"></a>
#### REL-001 — Freeze and identify the final release candidate

**Source:** Open. **Acceptance:** Pending for final candidate/platform. **Owner:** A, F. **Phase:** C3, C5, C6, C7, C8.

**Next:** Candidate 7 is a development baseline, not the final release. After repairs, build/sign/freeze the actual final artifact and bind fresh required evidence.

**Done when:** Source/bundle hashes remain stable through qualification; required checks cannot pass through missing/skipped evidence; candidate-specific limitations and approval state are explicit.

**Supporting evidence:** [development-candidate-4900d4b.md](docs/completion/development-candidate-4900d4b.md), [manifest.json](evidence/completion/candidate-4900d4b/manifest.json).

<a id="rel-002"></a>
#### REL-002 — Finish native end-to-end acceptance

**Source:** Open. **Acceptance:** Pending for final candidate/platform. **Owner:** B, D, E. **Phase:** C3, C5, C6, C7, C8.

**Next:** Complete table/support independently; exact editor then reopen/controls/backup/concurrency. Real clipboard/IME/focus and content/history/asset oracles are required.

**Done when:** Record actual user-visible behavior on that exact build, including restored content and selected workspace. Mocked-browser/service fixtures cannot substitute for native observations. Resolve the previously interrupted final-candidate checks.

**Supporting evidence:** [development-candidate-4900d4b.md](docs/completion/development-candidate-4900d4b.md), [operational-diagnostics.md](docs/completion/operational-diagnostics.md).

<a id="rel-003"></a>
#### REL-003 — Qualify physical audio and screenshot capture

**Source:** Open. **Acceptance:** Pending for final candidate/platform. **Owner:** B, F. **Phase:** C3, C5, C6, C7, C8.

**Next:** Acquire explicit device/capture consent and actual microphone/screenshot fixtures. Verify denial, disconnect, interruption and originals; system audio is separately scoped.

**Done when:** Device/user-consent outcomes are visible and recoverable; captured originals survive interruption; no unrelated private audio/screen content is used as a fixture. System-audio capture remains separately platform-qualified and need not block a microphone/upload-first release.

**Supporting evidence:** [macos-miniaudio.md](docs/completion/macos-miniaudio.md), [microphone-portability.md](docs/completion/microphone-portability.md).

<a id="rel-004"></a>
#### REL-004 — Validate real migration and meeting datasets

**Source:** Open. **Acceptance:** Pending for final candidate/platform. **Owner:** D, E. **Phase:** C3, C5, C6, C7, C8.

**Next:** Prepare authorized representative imports/recordings with independent expected content, critical facts and asset inventories; reconcile every discovered item.

**Done when:** Reconcile every discovered item, compare meaningful source content and asset bytes, report source omissions, and measure task/transcript quality. Repeated public speech and fixture HTTP servers remain useful regression inputs but do not establish general fidelity or live-account compatibility.

**Supporting evidence:** [notion-api-import.md](docs/completion/notion-api-import.md), [audio-decoding.md](docs/completion/audio-decoding.md).

<a id="rel-005"></a>
#### REL-005 — Qualify complete installation and model-pack paths

**Source:** Open. **Acceptance:** Pending for final candidate/platform. **Owner:** F. **Phase:** C3, C5, C6, C7, C8.

**Next:** Obtain independent clean Mac and test native install, model acquisition/import/interruption/offline operation and second-installation restoration.

**Done when:** Core app installation needs no Node, Python, Ollama, Docker, database server, or product account. Installed packs support offline AI/transcription. Backup/restore succeeds on a second clean installation. An empty PATH on the development Mac is not equivalent evidence.

**Supporting evidence:** [platform-desktop-packaging.md](docs/completion/platform-desktop-packaging.md), [development-candidate-4900d4b.md](docs/completion/development-candidate-4900d4b.md).

<a id="rel-006"></a>
#### REL-006 — Finish distribution, update, and recovery qualification

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** F. **Phase:** C3, C5, C6, C7, C8.

**Next:** Obtain signing access, sign/notarize final macOS app, then verify update/readiness/rollback/uninstall and recovery faults; Windows/Linux adapters remain gated.

**Done when:** Published artifacts have verified hashes and reproducible manifests; upgrades and rollback preserve canonical data; failed updates retain recoverable state; uninstall does not delete workspaces without an explicit request. Keep the current ad-hoc build labeled as such until these checks pass.

**Supporting evidence:** [pre-migration-snapshots.md](docs/completion/pre-migration-snapshots.md), [verified-updates.md](docs/completion/verified-updates.md).

<a id="rel-007"></a>
#### REL-007 — Benchmark realistic end-to-end performance

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** A, B, C, E. **Phase:** C3, C5, C6, C7, C8.

**Next:** Measure Trove at release: 80 startup, 300 keyword, 90 hybrid, nine database profiles/105 timings, actual editing/concurrency/resource observations. Candidate-6 search/database results do not qualify candidate 7.

**Done when:** Verify the declared approximately-two-second shell, under-300-ms common keyword search, under-one-second hybrid retrieval excluding optional reranking, and responsive editing against the agreed profiles. Record limits honestly; a 100-page hybrid benchmark or 10,000 simple pages is not evidence for all workloads.

**Supporting evidence:** [development-candidate-5d7cdb0.md](docs/completion/development-candidate-5d7cdb0.md), [database-performance.md](docs/completion/database-performance.md).

<a id="rel-008"></a>
#### REL-008 — Add regression coverage for the audit gaps

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** F. **Phase:** C0, C1, C2, C3, C4, C5, C6, C7, C8.

**Next:** Run affected regressions after real changes and the integrated suite at release; keep skipped native/model/account cases pending. Do not repeat unrelated suites without cause.

**Done when:** Tests fail on the audited behavior and pass after the fix. Use independent expected outcomes and archive/content inspection; avoid tests that simply repeat implementation logic. Run affected existing suites and real integration paths before closing an item.

**Supporting evidence:** [development-candidate-4900d4b.md](docs/completion/development-candidate-4900d4b.md), [pre-migration-snapshots.md](docs/completion/pre-migration-snapshots.md).

<a id="rel-009"></a>
#### REL-009 — Qualify additional platforms separately

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** F. **Phase:** C7, C8.

**Next:** Complete WIN-* and LIN-* native implementation and exact target acceptance; cross-compilation is not target operation.

**Done when:** Publish platform-specific supported features and evidence. Do not infer cross-platform readiness from Go portability or the macOS build.

**Supporting evidence:** [platform-native-workers.md](docs/completion/platform-native-workers.md), [platform-credentials.md](docs/completion/platform-credentials.md).

### Governance and integration

<a id="gov-001"></a>
#### GOV-001 — Establish source control and coordinated integration

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A. **Phase:** C0.

**Next:** Preserve tracker changes and review/integrate the single candidate-7 descendant commit. No source integration has occurred in this documentation update.

**Done when:** Changes have reviewable history, shared baseline updates reach dependent workstreams, integrated checks run before promotion, and no user data/secrets enter commits. This documentation task does not itself initialize Git.

**Supporting evidence:** [codegen-reproducibility.md](docs/completion/codegen-reproducibility.md), [platform-desktop-packaging.md](docs/completion/platform-desktop-packaging.md).

<a id="gov-002"></a>
#### GOV-002 — Make development and qualification reproducible

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, F. **Phase:** C0.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** A new contributor can provision the pinned toolchain and run checks without another chat's temporary environment. Record exact native build inputs and distinguish build dependencies from end-user runtime requirements.

**Supporting evidence:** [codegen-reproducibility.md](docs/completion/codegen-reproducibility.md), [platform-desktop-packaging.md](docs/completion/platform-desktop-packaging.md).

<a id="gov-003"></a>
#### GOV-003 — Reconcile the requirements ledger and completion claims

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** A. **Phase:** C0, C2, C3, C5, C8.

**Next:** This file consolidates human progress. Complete remaining candidate/platform/tier mappings and reconcile new G/CONN contracts without rewriting historical evidence.

**Done when:** Native editing, import reconstruction, fallback preservation, and verification are separate claims. A passing verification family does not close unimplemented breadth. Preserve historical reports; publish a new assessment for new evidence.

**Supporting evidence:** [requirements-overlay.json](docs/completion/requirements-overlay.json), [requirements-reconciliation.md](docs/completion/requirements-reconciliation.md).

<a id="gov-004"></a>
#### GOV-004 — Obtain and reconcile the referenced research evidence

**Source:** Awaiting external source. **Acceptance:** Pending for final candidate/platform. **Owner:** A. **Phase:** C0.

**Next:** Reconcile the available October 12 report separately; obtain or explicitly supersede the original missing 307-behavior source before closing that obligation.

**Done when:** Each accepted behavior has traceable evidence or an explicit unresolved state. Missing source wording is not invented. Research proposals do not become accepted features automatically. The CodeSwitchOS documents are design precedent, not a prerequisite for fixing known product defects.

<a id="gov-005"></a>
#### GOV-005 — Resolve selected-stack deviations explicitly

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A. **Phase:** C0, C2, C3, C5, C8.

**Next:** Retain the existing implementation; execute the acceptance below on the applicable packaged candidate and repair any demonstrated gap. Do not rebuild an already implemented feature from its unchecked historical backlog entry.

**Done when:** Implement the selected contract or record an explicitly accepted ADR with behavior, maintenance, security, licensing, offline, and qualification consequences. A different library is not automatically a user-visible bug; silently claiming exact TRD conformance is not acceptable.

**Supporting evidence:** [retrieval.md](docs/completion/retrieval.md), [recovery.md](docs/completion/recovery.md).

### Optional enhancements

<a id="opt-001"></a>
#### OPT-001 — Qualify and optionally expand local image intelligence

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** E, F. **Phase:** C6.

**Next:** Finish EVAL-07 and supported native image paths; supply six authorized photos and six screenshots. Existing narrow fixture passes are not broad image quality.

**Done when:** Use representative images and document quality/resource limits. Additional handwriting, scanned-PDF, rotation, or language support is required only when explicitly selected. Images remain local unless the user separately invokes an external integration.

**Supporting evidence:** [local-image-understanding.md](docs/completion/local-image-understanding.md), [c6-vision-manifest.json](evidence/completion/c6-vision-manifest.json).

<a id="opt-002"></a>
#### OPT-002 — Decide whether personal review needs generative analysis

**Source:** Implemented at stated source scope. **Acceptance:** Pending for final candidate/platform. **Owner:** A, E. **Phase:** C6.

**Next:** Complete EVAL-06 and native plan activation/proposal/retry/recovery acceptance; retain deterministic digest and separately approved mutations.

**Done when:** Preserve occurrence deduplication, activation consent, current-source/memory checks, reviewed writes, and failure isolation. Do not rename a digest as autonomous task management or introduce unreviewed knowledge rewriting.

**Supporting evidence:** [personal-reviews.md](docs/completion/personal-reviews.md), [related-content.md](docs/completion/related-content.md).

<a id="opt-003"></a>
#### OPT-003 — Qualify declared P2 package boundaries

**Source:** In progress. **Acceptance:** Pending for final candidate/platform. **Owner:** E, F. **Phase:** C6.

**Next:** Close P2-01–09 separately with actual model/native/hosting/persistence/recovery evidence.

**Done when:** Conversation fixtures preserve branches/roles/timestamps/provenance; graph associations disclose evidence; calculations and submissions remain canonical; operational settings stay inert on portable import; host authentication/TLS/ownership transfer/recovery remain correct. Arbitrary remote infrastructure, network filesystems, and offline multi-device sync are not implied by the existing host tests.

**Supporting evidence:** [implementation.md](docs/phase-6/implementation.md), [hosting.md](docs/phase-6/hosting.md).

<a id="new-desktop-work"></a>
## Newly selected desktop work — G01–G12

These are additional/reconciled requirements from the October 12 review, scoped by the user's October 13 decisions. The [PRD scope addendum](docs/PRD_SCOPE_ADDENDUM.md) supplies the normative feature requirements, baseline-section amendments and acceptance scenarios. The G IDs are tracking IDs; formal contract/qualification mappings still need updating. Parent and child rows must not be summed into a completion percentage.

| ID | Deliverable / source state | Owner / depends on | Remaining work and completion evidence |
| --- | --- | --- | --- |
| G01 | Re-import consistency — merge/deletion/order and explicit identity review integrated; acceptance pending | A,D / D002, FIX-001/002 | Verify link-existing/create-new/skip, retained exact aliases, raw evidence, stale targets and partial/rendition inventory changes in the integrated app. |
| G02 | One task authority — editing/promotion/extraction and reviewed facet detachment integrated; acceptance pending | A,B,C / D001 | Verify the integrated packaged task workflow across Tasks/Home/todo/databases, restart and restore; retain guarded occurrence conversion/removal, reminder effects and grouped undo. |
| G03 | OS protection + encrypted portable backups — implementation integrated; acceptance pending | D,F / CORE-010, REL-005/006 | Document copied-backup/lost-device boundaries; add passphrase-encrypted native archive, complete authenticated staging, verified preview and fresh-destination restore. Wrong key/tampering/truncation/crash must not activate a destination. Native format compatibility stays. |
| G04 | Concrete quality and support contract — partly specified | A,E,F / EVAL-*, REL-007 | Retain existing thresholds, name actual hardware/OS/model/language/workloads, lock new independent rubrics before measurement, report task/citation/speech quality and correction effort. New accuracy percentages and RAM targets remain proposals. |
| G05 | Meeting controls and retention — implementation integrated; native acceptance pending | B,D,E,F / CORE-005, REL-003/004 | Implement/verify visible input and start/pause/resume/stop, consent/permission/device/low-disk handling, recoverable gaps, correction history/source anchors, stale derived outputs, and distinct audio/transcript/index/backup retention. No hidden pruning. |
| G06 | Personal background reminders — integrated; native acceptance pending | A,B,F / task authority, native notifications | Separate reminder time from due date; opt-in agent, acknowledged schedule publication, snooze/dismiss/inbox and sleep/restart/DST/restore handling. Agent never opens canonical workspace SQLite; Complete task uses desktop owner. Track uncertain OS delivery honestly. |
| G07 | Reusable AI skills — implementation integrated; acceptance pending | A,B,E / AI-001, permission/review contracts | Version instructions/inputs/output/scopes/tools; manual invocation, selected sources, cancellation and provider handoff. Imported skills are inert; no new permission from their text. Proposed writes use existing guarded approval. |
| G08 | AI database autofill — implementation integrated; acceptance pending | A,B,C,E / AI-001, G07 | Selected rows/properties, typed values, blank-first behavior, overwrite preview, provenance, source/schema/model revisions, cancellation/resume and grouped undo. Stale outputs cannot overwrite manual edits. |
| G09 | Page review/freshness lifecycle — semantic coverage v3 integrated; acceptance pending | A,B,E / history, search/AI evidence | Store human-reviewed revision/date/expiry; invalidate after content change/expiry; expose queue and citation/search status. Separate from generative personal review and do not label a badge machine-proven truth. |
| G10 | Record layouts + previewed dependency rescheduling — implementation integrated; acceptance pending | A,B,C / PARITY-003/004, guarded mutations | G10-LAYOUT: stable property-ID layouts, sections/header/sidebar, overrides/reset without data deletion. G10-SCHEDULE: explicit full affected-date preview, duration preservation, cycle/constraint checks, calendar/timezone rules, atomic apply and grouped undo. Existing dependency links remain compatible. |
| G11 | Phone capture and continuity — deferred by user | B,D,F / desktop completion | Record desktop-first positioning and revisit a companion only after desktop work. No mobile client or two-way mobile sync is being implemented in this plan. |
| G12 | Broad connected-app platform — initial adapters/UI and reviewed sharing integrated; live acceptance pending | A,D,E / CONN-00–06 | Six named adapters, selected-source refresh and explicitly reviewed external writes; real authorization, provenance, stale/offline/denied/deleted distinctions and revocation. No automatic two-way sync. |

Implementation defaults to review before coding: age passphrase streaming around a verified native backup, no custom cipher; no remembered passphrase by default; reminder agent owns only derived scheduling/delivery state with registration epochs; manual skills/autofill; finish-to-start, preserved-duration, explicit-calendar rescheduling; no ambient connector context sent to external models. These choices are a plan, not implementation evidence.

<a id="detailed-workstreams"></a>
## Detailed workstreams

### Evaluation implementation, execution and review

| ID | Family | Implementation / input state | Execution / acceptance remaining |
| --- | --- | --- | --- |
| EVAL-01 | Candidate-7 AI baseline: 101 cases / 51 manual reviews | Frozen runner/inputs available | All seven families and manual reviews unexecuted; preserve one-generation rule, actual request controls and immutable outcomes. Under D-P11 this historical campaign does not block development; changed products require their own evaluation. |
| EVAL-02 | 42 held-out mode cases, 14 modes | Full frozen-source run: 25/42 automated passes, 17 failures; all cases executed (DEV-049) | Independent review of every output; 27 required manual semantic assessments remain separate. Resolve EVAL-09/10 without rewriting the original run; no automatic application of proposals. |
| EVAL-03 | 12 context cases | Full frozen-source run: 11/12 automated passes; near-budget quote-boundary failure retained | Independently assess exact quotation requirement; preserve real-token admission, omission, code and stale-source checks. |
| EVAL-04 | 12 memory-lifecycle cases | Full frozen-source run: 12/12 automated passes, unchanged inputs/canonical state | Independent review and final-candidate acceptance remain; live retrieval guards cover edit/delete/forget/revoke/concurrent-change and old-backup restoration. |
| EVAL-05 | 12 related-content cases | Full frozen-source run: 11/12 automated passes; budget relevance order failed | Investigate ranking without fixture-specific rules; preserve inaccessible/current-source exclusions, freshness and semantic/lexical disclosure. |
| EVAL-06 | 12 personal-review cases | Full v2 frozen-source run: 10/12 automated passes; requested second fact and decision body omitted in two cases | Repair EVAL-13 with fresh independent evidence; preserve prior v1/v2 results and separate human acceptance. |
| EVAL-07 | 24 image cases | Full declared run: 9 passes, three failures, 12 required-input skips; representative photos/screenshots unavailable | Repair EVAL-14 and obtain authorized representative sources with locked facts/questions/hashes; skipped cases cannot qualify. |
| EVAL-08 | Requested-attribute relevance and abstention | Independent 12-case prompt experiment rejected and reverted (DEV-053); original failure remains open | Seek a general repair; preserve protocol failures, semantic judgments and original grades. No hidden retries or synthesized abstention. |
| EVAL-09 | Mode-oracle number formatting and code layout | Strict numeric and syntax-tree corrections integrated; before/after regressions and combined 97-test integration pass (DEV-050) | Future evaluations use corrected checks; original corpus, raw outputs and failed grades remain unchanged. No production/model repair is implied. |
| EVAL-10 | Mode output correctness | Go proposal guard retained; database/comment guidance passes original six cases and independent controls; diagram/expansion guidance rejected (DEV-058) | Expansion and Mermaid defects remain open; original failures and 27 declared human assessments remain separate. |
| EVAL-11 | Near-capacity quotation boundary | Independent review confirms correct answer/current citation and exact 487-character source span; frozen oracle requires exactly the 43-character fact | Exact quote extent is an oracle limitation; excess filler remains a quote-quality caveat. Preserve failed grade and any future correction separately. |
| EVAL-12 | Related-content relevance ranking | Model-selected pooling and deterministic title-once query composition integrated; native comparison remains 11/12 (DEV-059) | Budget case still favors a generic title distractor; further ranking work requires independent evidence. Preserve original source text and all failed grades. |
| EVAL-13 | Personal-review completeness and omission disclosure | Omission disclosure and claim guidance integrated; original 12-case workflow improves from 10/12 to 12/12 with assessed claims/citations (DEV-055) | New-domain transfer controls and human acceptance remain pending; preserve original failures and bounded scope of demonstrated improvement. |
| EVAL-14 | Vision descriptions of plain pixels | Three cases return unsupported numeric/yes responses despite successful transport and preserved source pixels | Investigate native model/prompt behavior using independent pixel controls; do not substitute OCR success for image interpretation quality. |
| EVAL-15 | Native embedding pooling compatibility | Model-declared pooling and verified helper-bound vector identity integrated; 86 affected race passes and completed native comparison (DEV-055) | Correct runtime mismatch retained; 11/12 ranking result unchanged. EVAL-12 remains open; a new package is required. |

For every family, retain source/helper/model/fixture identities, required skipped results, raw failures and the independent before/after evidence required by its declared scope. Preserve each frozen candidate-7 fixture exactly, including inherited empty/context-only cases; those cases do not prove populated asset/audio preservation. Add separately declared populated preservation cases for future qualification without retrofitting candidate 7. Candidate-7 budgets and oracles remain frozen; later product/UI/model changes require their own declared evaluation. Fixture counts are not claims of representative usefulness.

### Additional P1 acceptance

These rows make existing REL-002/REL-008 and editor/navigation acceptance explicit; they do not add to the original 51-item count. The [requirements overlay](docs/completion/requirements-overlay.json) records their source coverage and remaining native gates.

| ID | Deliverable / source state | Owner / parent scope | Remaining work and completion evidence |
| --- | --- | --- | --- |
| P1-01 | Selected local folder/repository indexing — source implemented; native acceptance pending | A,D,E,F / REL-002, REL-008 | Verify explicit root selection with the native picker, permission boundaries, exclusions, symlinks and file identity; exercise changes, deletion, revoked access, restart and recovery. Search must respect selected scope and current source access. Retain independent filesystem/content evidence for the [external index implementation](internal/workspace/index_external.go). |
| P1-02 | Native accessibility, navigation and input — source foundations; native acceptance pending | B,F / REL-002, REL-008 and editor/navigation packages | Exercise keyboard navigation/shortcuts, VoiceOver, RTL/mixed text, IME and clipboard in the actual focused packaged app; verify deep-link launch routing, restart, undo/history and unchanged unrelated content. Browser fixture passes cover only their declared scope. |

### Connector delivery

Correction to the initial planned labels: CONN-00–06 now have integrated production adapters, native account/selection controls, Connections UI, retained refresh/write state and focused regressions (DEV-024–036). Reviewed external-model sharing is integrated with final cancellation follow-up active. No adapter is live-qualified; current API deletion/P2 repairs and all account-dependent acceptance remain separately tracked.

| ID | Adapter / owner | Initial read and refresh | Explicitly reviewed writes / constraints |
| --- | --- | --- | --- |
| CONN-00 | Shared framework / A,D,E | Native credentials, account/grant/selection, external snapshot/provenance, cursors, bounded refresh while app runs, freshness/offline status | Durable proposal/outbox/receipt; permission/version recheck; timeout-after-send is unknown outcome requiring reconciliation, not blind retry. Restore/import credentials and activation stay inert. |
| CONN-01 | Google Calendar / A,D | Selected calendars, recurrence/exceptions, timezones/cancellations | Create/update/cancel; preview attendees and notification effects; native browser OAuth/PKCE and incremental token recovery. |
| CONN-02 | Google Drive / A,D | Individually selected files via scoped picker; supported download/export | Upload exports/new files, explicitly replace supported selected files; no promise of unrestricted Drive or structural Docs/Sheets editing. |
| CONN-03 | Gmail / A,D,E | Selected labels/query/thread sets and explicitly acquired attachments | Local compose, reviewed send/reply; public restricted-scope verification; Gmail-derived external-AI context disabled by default until separately qualified. |
| CONN-04 | GitHub / A,D | Selected installed repositories: issues/PRs/comments/documentation | Issue/comment updates and PR creation from existing branches; no code push, merge, workflow dispatch or admin. Native device flow and account/installation limits. |
| CONN-05 | Notion / A,D,C | Selected shared roots and recursive REST capture/import with Connections entry point; hosted MCP actions; local connected-source AI and reviewed external sharing integrated; direct archived-source import proof integrated | Reviewed child pages, blocks and properties. Plan: direct import via user-owned selected-page token and public-client MCP; no confidential OAuth secret in desktop binary. |
| CONN-06 | Outbound MCP / A,E | Explicit trusted HTTPS server, selected resources/tools; separate from inbound Trove MCP | Exact selected tool/arguments/effects reviewed; resource-bound tokens and negotiated protocol; annotations or remote text cannot grant trust. |

Live acceptance for every adapter: account switch, selection limits, denied/revoked access, pagination interruption, token rotation, rate limits, stale writes, ambiguous remote outcome, disconnect/reconnect and inert restore. Cache refresh never deletes user-owned canonical imports merely because access is lost. App registrations, scopes, privacy documentation and applicable Google/Gmail verification are implementation/release prerequisites; no accounts have been connected by this tracking task.

### P2 package acceptance

All nine packages have source foundations; general package/model/native/platform acceptance remains **pending**. Existing scoped fixture passes stay historical.

| ID | Package | Owner | Remaining completion boundary |
| --- | --- | --- | --- |
| P2-01 | Charts | C | Exact aggregates, filters and record provenance; accessible data equivalent; reopen/restore |
| P2-02 | Local forms | B,C | Typed required-field validation, atomic canonical submission, restart and recovery |
| P2-03 | Row actions | A,C | Selected-record scope, explicit confirmation, stale revisions, idempotency and undo/recovery |
| P2-04 | Knowledge graph | B,E | Explicit/inferred evidence distinction, source/permission/model-consent freshness and navigable citations |
| P2-05 | OCR/vision | E,F | EVAL-07 and qualified native engines, actual image inputs, abstention and declared limits |
| P2-06 | Personal reviews | A,E | EVAL-06, consent/activation/schedule/retry and separately approved writes; deterministic digest preserved |
| P2-07 | Conversation imports | D,E | Original bytes, roles/timestamps/branches/provenance across hostile input, re-import/export and recovery |
| P2-08 | Per-page display settings | B | Persistence/history/export/import, without accidental global changes |
| P2-09 | Headless host | A,F | Real authenticated TLS deployment, Origin/upload/attachment boundaries, single owner, restart/recovery and fenced transfer; no implied multi-device offline sync |

### Platform implementation and release

| ID | Deliverable | Current state / next acceptance |
| --- | --- | --- |
| WIN-01 | Windows recovery durability | Foundation only; gated create/migrate/backup/restore/transfer remain unavailable. Prove positive-completion durability on local NTFS and integrate every producer before enabling. |
| WIN-02 | Windows provider containment | Missing production availability; complete AppContainer/filesystem authority plus Job lifecycle; native malicious-child, cancellation and parent-death tests. |
| WIN-03 | Windows PDF/device/desktop adapters | PDF and mic source exists; actual target build/device tests pending. Implement screenshot acquisition and qualify WebView/clipboard/URL routing/credentials. |
| WIN-04 | Windows publisher/update/rollback | Foundations exist; product selection/launch/rollback disabled. Wire trust and adapters after recovery proof; test authenticated readiness and ambiguous failure. |
| WIN-05 | Windows release | Native host/toolchain availability unknown; clean install/model/offline/performance/upgrade/rollback/uninstall and cross-platform backup acceptance pending. |
| LIN-01 | Linux process/filesystem isolation | cgroup foundation covers trusted fixtures only. Complete restricted filesystem/process/network authority, supervisor-crash ownership and native tests. |
| LIN-02 | Linux PDF/providers | Deliberately unavailable until LIN-01; prove whole-child ownership before restoring capability/packaging admission. Viewer/HTML paths remain separate. |
| LIN-03 | Linux capture/desktop/credentials | Implement portal screenshots; qualify existing mic/OCR, Secret Service, display/D-Bus, clipboard and URL routing on target. |
| LIN-04 | Linux publisher/update/rollback | Signature foundation exists; product adapters disabled. Supply independent trust pins and verify launch/readiness/rollback/recovery. |
| LIN-05 | Linux release | Native host/toolchain availability unknown; exact pinned desktop build, install/device/offline/update/uninstall/performance and backup portability pending. |

System-audio capture is separately platform-qualified and not a microphone/upload-first P0 blocker. User-controlled canonical data must remain recoverable on every failure path. Native target evidence is mandatory; macOS tests and cross-compilation are not substitutes.

### Final macOS qualification matrix

| Work | Status | Required evidence |
| --- | --- | --- |
| Frozen candidate-7 service/build/packaged journey | Passed for recorded scope | Existing three receipts; do not repeat solely for a tracking edit |
| Independent table and support-export gestures | Pending | Paging/scroll/chooser, preview/discard/save and exact exported bytes |
| Editor then dependent workflows | Pending | Real focus/clipboard/IME/edit/undo/restart plus content/history/assets; only then reopen, controls, backup/concurrency fixtures |
| Physical audio/screenshots and representative imports | Pending | Authorized data/device workflows and independent source/fact/byte oracles |
| Performance at release | Pending | 80 shell launches at 0/100/1,000/10,000 pages; 300 keyword and 90 hybrid samples; nine database correctness profiles, 63 first-page + 42 second-page timings; actual foreground responsiveness/resources |
| Distribution/install/update/recovery | Pending | Developer ID/notarization, clean installation/model paths, independently restored workspace, actual update/rollback/uninstall and failures |
| Criteria and approval | Pending | All required exact candidate/tier/platform evidence; new features get explicit new acceptance mapping; approval is not inferred from status tooling |

Startup uses the existing 2,500 ms limit; keyword samples must remain below 300 ms and hybrid below 1,000 ms. No numerical database SLA exists in the current plan. Instrumented diagnostic launches never replace normal qualification samples. Before the editor workflow, prepare its original compatibility baseline needed by later memory/concurrency audits; follow the immutable handoff rather than retrofitting a missing baseline after edits.

<a id="blockers"></a>
## Blockers and prerequisites

| ID | State as of this update | Affected work | Resolution / evidence required |
| --- | --- | --- | --- |
| B01 | Separate clean Mac confirmed unavailable in prior work | REL-005/006, final macOS release | Obtain independent installation/restoration target; a fresh user on the development Mac is supplementary, not equivalent |
| B02 | Apple signing/notarization and publisher access not established | REL-006, WIN/LIN updates | Supported credential/signing access and actual verified distribution results; never put secrets here |
| B03 | Windows 11 / Ubuntu 24.04 native environments unknown | REL-009, WIN-*, LIN-* | Provision the declared native OS/toolchain/filesystem/device environments |
| B04 | Representative datasets incomplete | REL-004, EVAL-03–07 | Authorized imports, audio and image inputs with independent expected outcomes |
| B05 | Physical capture consent/device evidence pending | REL-003, G05 | Explicit scope/consent and real native device/permission outcomes |
| B06 | Live accounts/HTTPS/app registrations not established | EXT-*, PARITY-006, CONN-* | Supported account flows, trusted test endpoints and applicable verification |
| B07 | Original 307-behavior research not supplied | GOV-004 | Obtain exact source or explicitly resolve its replacement; 39-feature report alone does not close it |
| B08 | Host/session readiness is transient | AI, performance, native gestures | Recheck only before relevant execution. Earlier battery/CPU refusals are historical; the October 13 audit observed AC power. No current CPU/focus readiness is claimed |
| B09 | Private storage headroom must be rechecked | AI/fixtures/backup work | Existing runner requires at least 4 GiB on its declared evidence/runtime volumes; no new product disk minimum is implied |
| B10 | Current source/checkpoint publication split | GOV-001/003, implementation | Integrate reviewed source; keep this file current. External candidate evidence is machine-local and must be preserved durably before cleanup/move |

The last recorded candidate-7 table refusal on 12 October at 17:38:17 IST launched no app and collected no sample (then battery 97%, Chrome renderer 59.1% CPU). It is not a product performance failure. No unrelated process should be stopped automatically to clear a guard.

<a id="update-rules"></a>
## How this file stays current

1. Read the snapshot, next actions and relevant item before starting project work. Inspect actual source/worktree state; do not assume the file updates itself.
2. Use stable IDs. Before substantial work, record the selected item, responsible lane and worktree in the next-actions table/log. Subagents report outcomes to the coordinator; avoid simultaneous conflicting edits here.
3. After each meaningful implementation, investigation, test campaign, decision, blocker, commit, integration or release event, update the relevant item and append a development-log entry. Do this before handing off or ending the task. Routine shell reads do not need individual log rows.
4. Each entry records IST date/time, IDs, what changed or was attempted, source/candidate identity, actual verification/evidence, limitations/blockers and the next action. Record failed, interrupted and rejected attempts; append corrections rather than hiding them.
5. Keep source implementation separate from acceptance. States: **planned/open**, **in progress**, **implemented—acceptance pending**, **blocked** (name the external prerequisite), **verified for named scope/candidate**, **done** (all applicable acceptance), **deferred** (explicit user scope decision).
6. Missing, skipped, mocked, stale or unrun evidence cannot become a pass. A prepared plan/fixture is not an executed result. Do not use counts of differently sized tasks as a readiness percentage.
7. Update snapshot counts only from the actual item set and immutable receipts. Do not sum parent/child items or mix historical candidates. Update the commit index when new source commits are made; record uncommitted work as uncommitted, never invent a SHA.
8. Keep this as the single current progress document. Add links to supporting code/tests/manifests instead of creating another status/checkpoint narrative. Frozen evidence remains immutable and machine-readable contracts still need their normal synchronized changes.
9. Exclude credentials, passphrases, private customer content and raw sensitive logs. Use scoped evidence references and concise results. Local absolute artifact links may not survive moving machines; preserve evidence before deleting it.
10. This is a manually maintained repository file with agent instructions, not a background automation. Changes made outside participating workflows need reconciliation on the next review. No recurring job, automatic Git hook or release/push action was created.

Suggested entry format:

```text
YYYY-MM-DD HH:MM IST | EVENT-ID | task IDs | actor/worktree
Change/attempt:
Verification and evidence:
Source/candidate/commit:
Remaining/next:
```

<a id="development-log"></a>
## Development log

This section begins with reconstructed milestones from retained evidence and then records this chat's work. The commit index below covers all 133 reachable commits in the selected primary/candidate-7 lineage. It does not pretend to reconstruct every old uncommitted edit, abandoned branch or terminal command.

| Date (IST) | Event | Development / observed outcome | Source / next consequence |
| --- | --- | --- | --- |
| 30 Sep–1 Oct 2026 | HIST-001 | Original phase implementations and audit identified substantial existing P0/P1/P2 functionality with unqualified release boundaries | Historical phase docs and original backlog; no blanket release pass |
| 4–5 Oct 2026 | HIST-002 | Correctness repairs, database/editor/import/AI/provider/native foundations integrated through the recorded commits | Commit index; source implementation separate from native/model/platform acceptance |
| 7 Oct 2026 | HIST-003 | Qualification mapping, expanded-evaluation preparation, signing/diagnostics and recovery/publisher foundations added | `c7f0857` through `0bff6ed`; model failures and platform limits retained |
| 8–9 Oct 2026 | HIST-004 | Integrated AI/context/reporting/native measurement repairs and candidate checkpoints | `d9471a8` through `12c56b8`; prior failures preserved |
| 9–11 Oct 2026 | HIST-005 | Candidate 6: 78/89 combined AI passes; 11 failed cases; 300 keyword/90 hybrid and nine database profiles passed their recorded scopes; one of 80 startup launches took 6.25 s; native editor unfinished | Historical candidate-6 report; no transfer of receipts to candidate 7 |
| 12 Oct 2026 17:12–17:32 | HIST-006 | Candidate 7 frozen and three gates passed; six independent content/asset assertions passed; 52 gates pending | `299e43d`, candidate `397737cb…`; actual AI/performance/native work still pending |
| 12 Oct 2026 17:35–17:38 | HIST-007 | Later handoff preparation and host preflight refusals produced no new candidate pass or app launch | Native editor/reopen handoff and retained table refusal v2/v3 |
| 12 Oct 2026 | HIST-008 | Separate Notion requirements review completed: 39 feature labels, 12 proposals; original PRD/TRD unchanged | Research report available; original 307-behavior source still missing |
| 13 Oct 2026 | DEV-001 | Audited repository, related work, candidate evidence and platform code; identified source/status drift and full remaining inventory | Read-only audit; no product fix/test/launch performed |
| 13 Oct 2026 | DEV-002 | User selected macOS-first desktop scope, encrypted backups, six connector adapters/read+reviewed-write behavior, background reminders and previewed rescheduling | Decisions D-P01–09; completion plan prepared; new features not implemented |
| 13 Oct 2026 | DEV-003 | User questioned repeated benchmarking; plan clarified productivity-first implementation and bounded release-stage measurement | No minimum-RAM selection or policy-threshold change |
| 2026-10-13 18:33 IST | DEV-004 / TRACK-001 | Created this consolidated tracker with all 51 original acceptance statements, G01–G12, child workstreams, evidence, blockers and commit history; added navigation and maintenance instructions | Documentation-only working changes on `12c56b8`; verification pending; no commit/merge/release |
| 2026-10-13 18:41 IST | DEV-005 / TRACK-001 | Completed consolidation review and validation. Added explicit P1 indexing/accessibility acceptance and clarified frozen evaluation scope. Completion validator: passed, no errors; coverage: all 51 original acceptance statements and all new workstreams; links: 112 valid; history: 133 unique commits; whitespace: passed | Coordinator in primary checkout at `12c56b8`; documentation remains uncommitted. TRACK-001 done; source alignment and product/release work remain pending as listed above |
| 2026-10-13 21:41 IST | DEV-006 / TRACK-002 | Began the requested Markdown PRD scope addendum; reconciled the annotated research with existing D-P01–09 and D001/D002 rather than treating historical gaps as current missing contracts | Coordinator in primary checkout, branch `codex/macos-p0-checkpoint-20261008`, HEAD `12c56b88133d48247d94263903c2f0fa992151b9`; documentation work only. Next: read current PRD/TRD, write explicit requirements/acceptance and review traceability; no product tests, integration or release claim |
| 2026-10-13 21:51 IST | DEV-007 / TRACK-002 / D-P10 | Created `docs/PRD_SCOPE_ADDENDUM.md` v1.0 with all 12 scope groups, six adapters plus CONN-00, and 36 unique acceptance scenarios. Reconciled D001–D003, OS protection/encrypted backups, closed-app reminders, reviewed external writes and deferred mobile. Independent read-only scope review passed; local-link/ID/whitespace checks passed; original PRD/TRD hashes unchanged | Same primary branch/HEAD `12c56b8`; new addendum and tracker changes remain uncommitted. No product code, machine contracts, frozen evidence or qualification state changed; no product tests/launch/merge/release. Next: source alignment and affected versioned contracts/mappings before implementation qualification, following existing milestone order |
| 2026-10-15 16:15 IST | DEV-008 / TRACK-002 / GOV-001 / GOV-003 | Resumed the interrupted scope intake and read the addendum against current decisions, milestones and G/CONN workstreams. Independent read-only review found no substantive scope contradiction. Reconfirmed branch/HEAD, working documentation changes and the separate candidate-7 worktree; Git history shows one descendant commit, whose file summary was inspected but whose contents were not reviewed for integration | Coordinator in primary checkout `codex/macos-p0-checkpoint-20261008` at `12c56b88133d48247d94263903c2f0fa992151b9`; candidate 7 remains at `299e43df9b716f771125d939ad320c3cb595f2c7`. Only this progress record changed in this intake; no product tests, benchmarks, launches, merge, commit or release action. Next: review/integrate the descendant while preserving documentation, then affected versioned contracts/mappings and existing milestone order. Implementation and qualification states remain unchanged |
| 2026-10-15 16:22 IST | DEV-009 / GOV-003 | Answered the user's progress-percentage request by parsing all 51 original item source labels and independently checking the G-group boundaries. Confirmed 34 implemented / 17 unfinished (66.7% / 33.3%); candidate-7 gate counts separately yield 5.5% passed / 94.5% pending. No active G group is accepted complete; parent/child and legacy overlap prevent a defensible overall expanded-product percentage | Primary branch/HEAD reconfirmed at `codex/macos-p0-checkpoint-20261008`, `12c56b8`; candidate-7 figures remain recorded evidence for `299e43d`, not newly executed checks. Arithmetic/read-only review only; no product test, source integration, commit or qualification change. Next: existing source-alignment and implementation order; estimate remaining effort separately if needed |
| 2026-10-15 17:12 IST | DEV-010 / IMPL-001 / D-P11–14 | User approved the full implementation plan; created `codex/macos-feature-completion` from `12c56b8`. Recorded functionality-first ordering, full macOS scope, selectable scheduling calendars and a single missed-reminder catch-up summary. Planning source inspection confirmed missing field-level re-import, task title/due/extraction identity, grouped undo and presentation-neutral review identity; prior source labels are not erased | Coordinator in primary checkout; documentation checkpoint and descendant review/integration next, then isolated implementation lanes. No product test, feature completion or release pass claimed |
| 2026-10-15 17:14 IST | DEV-011 / GOV-001 / GOV-003 | Checkpointed accepted scope/documentation as `679bdc4`. Independent review of `12c56b8..299e43d` found no blocking code issues: test-only generation controls/fixtures, locked fixture v2 hash verified, and whitespace check passed. Merge encountered one documentation conflict; resolved it by retaining the current-tracker banner above the descendant historical snapshot | On `codex/macos-feature-completion`; merge commit pending this entry. No product/model tests run by review; no frozen evidence changed. Next: complete integration commit and start isolated G01/G02/G03 plus shared-foundation work |
| 2026-10-15 17:20 IST | DEV-012 / GOV-001 / IMPL-001 | Completed reviewed candidate-7 integration as merge `ee14f0c`; candidate-7 worktree and immutable evidence remain unchanged. Started shared semantic-content/review contracts and assigned isolated G01/G02/G03 implementation workers. Managed worktree creation reached checkout completion in Git but app operations remain creating; attempted attachment could not verify ownership, so workers are inspecting source read-only pending registration | Primary feature branch, uncommitted foundation work. No product/model test or feature acceptance yet; next: complete shared services and focused regressions, then integrate worker changes |
| 2026-10-15 17:39 IST | DEV-013 / G09 / G08–10 / P2-03 / GOV-003 | All three managed worktrees completed and registered at `ee14f0c`; workers implement G01/G02/G03 independently. Primary adds version-1 semantic content identity, attributable review events/expiry/queue/search/citation/AI evidence, page review UI, and a guarded operation journal with field-specific inverse batches exposed for row actions. Added machine mapping `macos-feature-scope-v1.json`: all 51 legacy items and 36 addendum scenarios mapped without changing frozen evidence or legacy labels | Uncommitted source on feature branch. Initial root tests exposed a missing schema type, unsupported canonical encoder input and a missing test collection guard; corrected. Focused freshness/undo tests pass, plus race command covering freshness/undo/P2 row actions/page display/search; frontend TypeScript and generated contract drift checks pass. Native review attribution/history restore and backup persistence pass service fixtures; browser/native UI acceptance and full release remain pending. Next: targeted browser checks, independent review and checkpoint shared foundations |
| 2026-10-15 17:50 IST | DEV-014 / G09 / G08–10 / P2-03 | Independent shared-foundation review found relation-side-effect undo gaps and semantic omissions for reusable blocks, formatted text properties and computed definitions. Corrected with an explicit editable-scalar journal boundary, per-field display inverses, bounded reusable/rollup dependency graphs, normalized text cells and all applicable property definitions. Added counterexample regressions. G01 page merge, G02 tasks and G03 backups have separate worker commits pending integration/review | Primary uncommitted source based on `ee14f0c`: 26 focused service race tests passed, zero failed/skipped (`/private/tmp/trove-shared-foundations-race.jsonl`); one browser workflow passed with mocked service bridge (`/private/tmp/trove-freshness-undo-browser.json`). TypeScript, generated schema/type drift and whitespace checks passed before checkpoint. Browser server first hit sandbox bind denial; authorized local server launch succeeded. These do not establish packaged native, real-model/account or final release acceptance. Next: checkpoint foundations, integrate reviewed worker commits and continue feature slices |
| 2026-10-15 17:53 IST | DEV-015 / G02 / IMPL-001 | Shared foundations checkpointed as `0ed5350`. Independently reviewed G02 worker commit `6f0381c`: canonical title/status/due controls in Tasks/Home/todo, explicit same-collection due binding during promotion, durable extraction proposal/item/target IDs and atomic receipts. Integrated source and resolved only shared generated-type/query-map/import conflicts; regenerated consumers from the combined authoring schema | G02 worker: 11 focused service race tests, contracts, 56 frontend units, TypeScript and one mocked-bridge browser workflow passed. Integrated primary: 16 selected task/extraction/freshness/undo race tests passed, zero failed/skipped (`/private/tmp/trove-g02-integration-race.jsonl`), plus TypeScript. Earlier worker missing-FTS5 invocation and numeric fixture mismatch were corrected before its pass. Native restore and packaged/device acceptance remain pending; integration commit follows this entry. G01 review found four blocking page-merge defects; worker repairs them before integration |

| 2026-10-15 18:00 IST | DEV-016 / G02 / G03 / G09 | G02 integration committed as `2b421a4`. Reviewed and integrated encrypted native backups as `950cea5` plus summary correction `6e713bc`; shared service schemas regenerated as the union. Backup summary now distinguishes complete SQLite payload (including indexes and approval/job evidence) from restore transformations instead of claiming those bytes are excluded. Final freshness correction records synced todo task-facet bindings and formula relation dependencies; added regression for due rebinding | Integrated primary: 45 focused service race test/subtest passes, zero failed/skipped (`/private/tmp/trove-backup-integration-race.jsonl`), including authenticated-before-parse corrupt/truncated/wrong-passphrase cases, cancellation/staging/low-space guards, task/extraction and freshness/undo. Contract drift and TypeScript passed; production frontend build in progress. Worker G03 had four mocked-native browser passes, not physical-device acceptance. No real disk-full/crash/device or release pass claimed. G01 four-defect corrective commit reviewed; integrate next. G05 native/meeting lane and G10 layouts/scheduling lane started from `6e713bc` |

| 2026-10-15 18:14 IST | DEV-017 / G01 / G07 / G10 | Integrated retained page three-way merge `2279bf0` and four-defect repair `a3f9e54`; combined authoritative domain/service contracts regenerated. 46 focused integrated import/Notion/portable/conversation race tests passed, zero failed/skipped (`/private/tmp/trove-import-integration-race.jsonl`). G03-integrated frontend production build passed with existing bundle-size/directive warnings. G07 versioned library, run preview, exact skill references and existing local/managed engine integration are under development; first five focused service regressions passed | Root skills remain uncommitted and under review/test; initial compile exposed an integer-width mismatch and adding a slice to a previously comparable request struct, both corrected by retaining source selection in the skill reference. Typed database import, explicit CSV identities, G05 and G10 remain active worker slices. G10 tests exposed an existing-value grouped-undo input representation bug; worker fixes the journal boundary with a regression. No live model/account/device or release acceptance claimed; next: finish skills UI/authority/retry checks, review and integrate typed import/meeting/layout slices |

| 2026-10-15 18:24 IST | DEV-018 / G07 / EXT-005 | G07 now has versioned create/edit/duplicate/archive/import, selected source/tool preview, local-default and explicit managed-provider execution, cancellation/history/retry controls, exact version/input/scope binding, review-only proposals and durable retry receipts. Independent review found preview-to-generation races and trashed-skill execution; corrected with a final transaction-bound preview check in local and managed run persistence and an explicit live-skill check. Provider reconnect retains stable proposal keys and committed receipts; pending proposals require fresh approval under the new connection | Uncommitted primary at `a3f9e54`: 18 focused skills/provider/freshness race passes, zero failed/skipped (`/private/tmp/trove-skills-provider-race.jsonl`), eight selected native/provider/backup/contract race passes (`/private/tmp/trove-skills-native-race.jsonl`), TypeScript and contract drift pass. One mocked-bridge skills browser workflow passed after sandbox Chrome launch failure and missing memory fixture correction; final import assertion now verifies saved persistence. Initial unit command incorrectly assumed Vitest; repository uses Node tests; corrected run passed 56 tests, zero failed/skipped. Worker review found no remaining confirmed blocker; no independent real-model/account acceptance. G05 capture commit `d3a84df` reviewed for integration; G01 typed follow-up validation pending |

### DEV-019 — 15 October 2026, 18:34 IST: meeting capture and typed re-import integrated

**Source:** `codex/macos-feature-completion`; G07 committed as `3e8fc20`, G05 capture worker `d3a84df` integrated as `97201e7`, and reviewed G01 typed/historical changes `a26a96b`/`a8e2bc8` integrated as `45ef173`/`5cb0951`. Generated-domain conflict was resolved by regeneration from the merged schema. Candidate 7 remains untouched.

**Implemented:** Meeting input selection, disclosure acknowledgement, acknowledged pause/resume/stop, sample-derived duration, retained pause/interruption gaps, low-space checks and inert interrupted-capture recovery. Typed collection imports retain stable property/option/record identities and review independent schema/value fields; complete derived graphs are preflighted before resumable writes. Historical baselines reconstruct only from retained matching source revision/raw identity with a known normalizer; absent evidence still requires explicit review. Unsupported schema conversions/deletions remain explicit conflicts, not silent destructive merges.

**Evidence:** Integrated capture/recovery/contract race run at `97201e7`: **130 test/subtest passes, zero failures, one skip**, `/private/tmp/trove-g05-integrated-race.jsonl`. The skipped startup-deadline test needs a separate compile-time stalled-startup fixture; worker is building it. This corrects the earlier worker broad-run summary, which did not expose that skip. Import worker reports **58 affected race passes, zero failures/skips**, `/private/tmp/trove-import-known-baseline-race.jsonl`; integrated follow-up checks remain next. G07 evidence remains scoped at DEV-018. No physical microphone/device, live model, real-account, signing or release acceptance is claimed.

**Next:** Integrate G10 layout/scheduling with its grouped-undo normalization repair; finish G05 transcript correction/retention then G06 reminder helper. Continue G01 explicit CSV row identity. Coordinator starts G08 persisted per-cell generation, explicit overwrite review, typed validation, resumable outcomes and atomic grouped application. Other full macOS, connector, AI and P2 scope remains active.

### DEV-020 — 15 October 2026, 18:45 IST: autofill implementation checkpoint

**Source:** Primary `5cb0951` plus coordinator G08 changes, being checkpointed before G10/CSV integration. Added persisted bounded batch/cell suggestions, source/property snapshots, manual local generation with shared cancellation, per-cell checkpoints, explicit overwrite selection, reviewed atomic command and journal integration, plus usable database review/history controls. An independent worker review prompted per-cell source/permission rechecks before generation and persistence and selected option-label evidence in the prompt.

**Evidence/limitations:** Service/root/contracts compilation and frontend TypeScript passed at the initial checkpoint. New focused regressions initially exposed a typed-ID-to-string bug in journal preparation (corrected); existing-value grouped undo also depends on G10's pending shared normalization repair. These checks are not a G08 pass or feature acceptance. Tests and browser workflow verification continue after integration. No real model was invoked. Initial implementation provenance records prompt version and selected model-pack identity; exact model artifact provenance and further stale/retry review remain next.

### DEV-021 — 15 October 2026, 18:56 IST: G05/G08/G10 integration and connector preparation

**Source:** G08 checkpoint `33906a3`, G10 integrated `92955e5`, explicit CSV identity `1f7a26c`, G05 correction/retention `cc1b0ca`; current G08 hardening changes remain uncommitted. Shared schema conflicts were combined at the generator source and all consumers regenerated. G10 supplies the scalar journal normalization required by autofill undo. G05 archived-summary projection follow-up `a798931` is ready for integration.

**Implemented:** Stable property-ID layouts with defaults/overrides/reset/history; complete guarded dependency schedules with calendar/working-week and explicit timezone; corrected stable transcript segments and reviewed category-specific retention; persisted manual autofill source/schema snapshots, typed per-cell outcomes, explicit overwrite review, bounded local generation, cancellation/resume, idempotent atomic application, provenance and grouped undo. Autofill now records the verified model artifact hash/source/runtime identity and rejects changing that identity on resume. Grant revocation interrupts the flight durably; source changes during generation cannot publish a suggestion.

**Evidence:** Combined service/native/root/contracts campaign at `cc1b0ca` plus G08 hardening passed **176 tests/subtests, zero failures/skips**, `/private/tmp/trove-g05-g08-g10-integrated-race.jsonl`. The separately built stalled-startup fixture supplies the previously missing microphone case; these remain device-free native tests. Initial G08 typed-ID failure is corrected. Final output-decoder/source-validation follow-up: workspace suite has **12 passes**, `/private/tmp/trove-autofill-final-race.jsonl`; root/contracts compilation still running at this entry, so not yet reported complete. TypeScript and **56 frontend unit tests** passed. Autofill actual-component browser test with mocked dispatcher passed (`/private/tmp/trove-autofill-browser.json`): cancellation, reload/resume, separate overwrite review, apply and undo. Production frontend build is still running. Worker G10 evidence: 13 service races plus one mocked browser; G05 follow-up:154 test/subtest passes and four mocked browser passes; these separate runs are not additive product-completion counts.

**Connector findings and next action:** Begin CONN-00 framework and native account lifecycle while workers complete G06, G09/P2 and import attachment/conversation gaps. Firecrawl developer search succeeded; page scrape hit exhausted credits, so official documentation was read through the available web tool. [Google installed-app OAuth](https://developers.google.com/identity/protocols/oauth2/native-app) requires system-browser/local callback and supports PKCE; installed-app incremental authorization is unsupported. [Desktop Picker](https://developers.google.com/workspace/drive/picker/guides/desktop-mobile-picker) requires `drive.file` alone and returns explicit selected IDs, so Drive Picker grants must remain separate from Calendar/Gmail grants even when account-lifecycle code is shared. [GitHub App device flow](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app) supports native user tokens without a bundled confidential secret. Registrations and authorized live-account validation remain dependent checks, not completed integrations. No real models/accounts, physical devices, release candidate or release approval is claimed.

### DEV-022 — 15 October 2026, 18:58 IST: autofill final follow-up and build correction

Final autofill workspace and protocol suite passed **37 tests/subtests, zero skips** at `cc1b0ca` plus current G08 changes (`/private/tmp/trove-autofill-final-race.jsonl`), including duplicate-key output rejection, source changes during generation, grant revocation with resumable state, model artifact changes, overwrite guards and existing-value undo. The same command's desktop package failed to compile because a concurrently running Vite build replaced files under embedded `frontend/dist`; this is retained as a failed campaign, not counted as a desktop pass. Frontend production build subsequently completed successfully with bundler size/directive/timing warnings (`/private/tmp/trove-autofill-build.log`). Corrective desktop-only race/compile rerun is now serialized after the build. Future frontend rebuild and desktop embedding checks must be sequential. No candidate was packaged and this build duration is not controlled performance evidence.

**19:06 correction:** The serialized desktop race/compile rerun passed **87 tests/subtests, zero failures/skips**, `/private/tmp/trove-autofill-root-final-race.jsonl`. G08 hardening, generated contracts and the actual-component browser fixture are ready to checkpoint. This corrects the earlier build overlap without erasing its failed result. The newly started connector package contains types only and is not included in this implementation claim.

### DEV-023 — 15 October 2026, 19:14 IST: freshness/P2 integration and connector state implementation

**Source:** G08 hardening `d67190d`, G09/P2 continuity `406c34c`, archived meeting summary guard `8e8912d`. G09 semantic digest v2 covers transcript timing/speakers/meeting binding and canonical task scope; v1 review events remain readable with explicit changed-coverage status. Review queues and search/AI labels refresh after content or expiry changes. P2 forms/settings preserve drafts across refresh/collapse, detect schema/config conflicts, and reuse checkbox defaults. Worker source `307a157` evidence: 19 focused service race passes, 56 frontend units, eight final changed-UI browser passes; earlier 27 browser passes and two optional external-video skips. Independent pre-fix cases failed as expected. These are worker-source scoped checks, not integrated release acceptance.

**Current implementation:** CONN-00 storage and state-machine code is being added: versioned account/selection metadata and history, external snapshots, bounded durable refresh cursors, reviewed proposals/outbox/receipts, uncertain-send reconciliation and inert restore transformations. Credentials are excluded from service records. This code is not yet integrated or verified as a usable connector. HTTP uses HTTPS, bounded responses, denied redirects and public-address validation. Actual adapters/native authorization/UI remain next.

**Review corrections:** Attachment worker separated deliberate user attachment intent from automatic imported evidence, including same-byte deduplication and historical membership. Ready checkpoints include attachment merge `1ae3fd0`, history v6 `1ffc46f`, intent repair `37f3c20` and conversation merge `0594f90`; integrated worker campaign passed 92 race tests/subtests without skips. G05 owning-collection retention guard `d5c3db8` is ready. Multi-commit cherry-pick again paused after the first commit with a worktree-overwrite warning; inspecting actual diff before continuing. No user changes will be discarded. Native reminder helper and missing real-model evaluation runners continue separately; no inference has started.

### DEV-024 — 15 October 2026, 20:02 IST: import continuity, focused model repairs and connector implementation

**Integrated source:** `80e4132` adds the record-owned meeting collection guard; `0dd5d91`/`52b9fbc`/`6a9fc8b` integrate three-way attachment membership, exact history-v6 relationships and explicit user attachment intent; `8530d83` binds conversation identity and partial capture; `a17da24` requires explicit incoming block-deletion review. Unsupported populated/dependent schema conversions and record/property deletion remain explicit G01 gaps, pending schema recovery operations. Current-source import/freshness/retention campaign passed **30 tests/subtests, zero failures/skips**, `/private/tmp/trove-freshness-import-retention-integrated.jsonl`.

**AI repair and evidence:** `9c20f82` preserves distinct evidence chunks that shared a location, fixing an independent ORCHID-73 tail-evidence regression. `ed95297` rejects impossible/substituted evaluation token budgets. Executable 12-case context and 12-case related-content families and independent evidence consumers are integrated. Worker checks: 75 focused race passes, 40 Python passes. A first native attempt produced **zero generations and three missing results** because the frozen candidate-7 microphone helper used protocol v1; that failed attempt is preserved. A private current protocol-v2 evaluation bundle then passed **three selected real-model context cases and two selected embedding/related-content cases**. The complete families were not run (`fullFamilyRun=false`, `CorpusPassed=false`); this is focused repair evidence only. Reports are retained under `evidence/completion/workflow-context-20261015-v1`, `workflow-context-20261015-v2` and `workflow-related-20261015-v1`. Candidate 7 and its immutable artifacts are unchanged.

**Connector implementation:** `ad8ba42` integrates selected Calendar/Drive/Gmail adapters; `d56f19c` distinguishes Google media rate limits from revoked access. Worker Google campaign: 35 race tests/subtests, zero failures/skips, `/private/tmp/trove-google-rate-limit-race.jsonl`. Current uncommitted coordinator code adds OS-protected native account authorization, selected installed GitHub repositories, exact reviewed GitHub writes, durable refresh/proposal/receipt state, authenticated previews, deliberate G01 imports and inert restoration. Root connector/auth/restore campaign passed **24 tests/subtests, zero failures/skips**, `/private/tmp/trove-connectors-github-auth-race.jsonl`; the desktop package compiled but had no matched tests. An earlier compile failed on an incorrect IP-range helper and was corrected before these passes. Connector UI, integration checks and real-account acceptance remain pending. Gmail-derived external-client context is denied, including retained import provenance. No live credentials or accounts were used by fixtures.

**G06:** Worker `a840f66` is ready: canonical reminders/inbox/history, private opt-in Swift login item, acknowledged publication, durable uncertain outcomes and one catch-up summary. Worker evidence: 168 initial focused race passes and nine follow-up passes, three browser passes, 56 frontend units, 15 signing-policy fixtures and successful type/build/generator checks. Review found that an old acknowledgement could falsely revive a dead helper; freshness checks and a new explicit-reconciliation generation repair that defect. Production helper compiled; actual notification permission, closed-app/login/sleep delivery and release signing remain unexecuted. Integration follows this connector foundation checkpoint.

**Next:** Integrate G06 and outbound MCP; complete Connections UI and current-source checks; implement selected-root Notion, remaining evaluation runners/EXT/P2/native gaps. Full macOS completion, aggregate completion percentage and release acceptance are not claimed.

### DEV-025 — 15 October 2026, 20:16 IST: reminder/MCP integration and Connections interface

**Source:** `53eb3c8` checkpoints the connector foundation. `573d31b` integrates G06 reminders; shared route conflicts were resolved by preserving both sets and regenerating Go/TypeScript contracts. `200eaff` adds outbound MCP transport, exact selected tools/resources and native OAuth; coordinator wiring now connects its protected OAuth state and permits Notion's separately verified remote identity without weakening generic MCP's opaque-grant identity. `41ff16c` adds atomic stable-membership sibling-order import review (worker 88 race passes); `425e732` repairs authenticated headless routes for new shared functionality (worker 14 race passes). `20c0937` discloses older-backup memory recovery and `f05eb2e` repairs reminder publisher lock handoff (worker 96 race passes). No deployed headless host, OS notification registration or frozen candidate changed.

**Current work:** Connections UI exposes native authorization, explicit source selection, bounded refresh/cancel/resume history, external search, authenticated previews and exact reviewed writes with unknown-outcome reconciliation. Deliberate imports use G01 retained baselines and stale-preview guards; incomplete Notion bodies remain inspectable but cannot replace a complete body. Drive acquisition reuses actor-bound file ingress. Gmail attachment ingress remains disabled while durable origin restrictions across attachment moves/portable recovery are implemented. Notion adapter, UI validation, native bridge verification and remaining credentials-cleanup/recovery details are active work, not completed acceptance.

**Checks and corrections:** Two early connector checkpoint runs are retained: the first had a missing Go import and both exposed a Google403 fixture mismatch after overbroad Retry-After classification. Classification now uses GitHub rate-limit headers or Google's documented bounded reason codes; generic Google403 remains denied access. Initial combined native integration recorded 76 passes, four test/subtest failures from sandbox-denied OAuth loopback listeners, and two skipped Swift cases with no helper path. Correctly configured local native fixture rerun `/private/tmp/trove-connector-native-integrated-race-v2.jsonl` passed **70 tests/subtests, zero failures/skips** using `/private/tmp/trove-g06-helper-v5/TroveReminders.app/Contents/MacOS/TroveReminders`. These fixtures do not authorize live accounts or submit notifications. New connector import test initially used an invalid page.rename command; corrected to the actual page.update contract, rerun pending. Frontend type check passed before adding browser fixtures; current browser/type/service checks continue. Real-account, physical-device, full-model, performance and release campaigns remain separate and pending.

### DEV-026 — 15 October 2026, 20:25 IST: reviewed connection workflows and native model ownership

**Source:** `6a43979` integrates selected-page Notion MCP capture/writes (worker 34 Notion plus 35 Google race passes); `28ae823` verifies memory restoration warnings; `12911e2` prevents a second model manager from deleting an active transfer or leased inference model. Two native ownership regressions failed before repair; worker final evidence has 23 native and four explicit process-death/descriptor/link passes without skips. Private empty ownership-lock files intentionally persist to avoid unlink/recreate ownership races. Source evidence retains initial post-fix fixture failures and their corrections. No frozen models or candidate were changed.

**Connections implementation checkpoint:** The native bridge now connects all six initial adapters, authenticates current previews, binds deliberate imports to remote revisions, retains partial-source status, and supports Drive acquisition into reviewed file ingress. Plain-text/Google document previews now acquire actual content; oversized/unsupported previews declare partial content. Local composition drafts and revisioned history persist without sending. Credential cleanup uses a durable opaque-registration queue outside activation, retries after disconnection/reconnection, and is cleared by restoration. Outbound MCP authorization distinguishes registered clients from explicitly approved dynamic registration; generic grant identity is not called a verified person. Notion verifies workspace/user identity. Exact selected-page Notion writes are available; recursive/typed Notion capture and Gmail attachment provenance remain implementation gaps.

**Verification:** Coordinator import/service bridge campaign `/private/tmp/trove-connectors-ui-bridge-race-v1.jsonl` passed eight tests/subtests, zero failures/skips; root/contracts compiled with no matching tests. Earlier connector import-only rerun passed seven after correcting the test's nonexistent page.rename command. Current TypeScript check passed. Actual-component browser fixture `/private/tmp/trove-connectors-browser-v3.json` passed all three cases: source selection and exact write review, unknown reconciliation without a resend, stale-preview rejection, late response isolation after workspace switch, and native authorization cancellation. The first browser run had two passes and one locator timeout; corrected to the rendered combobox role, with all three passing in both subsequent runs. New Drive-content regression initially used a calendar-kind fixture selection; corrected to file selection and rerunning. These use mocked provider/bridge boundaries and do not establish live account acceptance.

**Next:** Integrate the completed memory-lifecycle runner; persist Gmail asset/derived-context restrictions before exposing its acquisition; finish schema recovery/conversion, remaining evaluation runners, managed-provider and desktop/P2 gaps. Complete feature integration remains in progress. Full native notification/account, representative-data, model-family, performance, signing/install and release acceptance remain pending.

### DEV-027 — 15 October 2026, 20:43 IST: memory evaluation, connection cancellation and workspace isolation

**Source:** `8552b57` checkpoints the Connections interface/native bridge and local composition/recovery work. `acc33d0` adds the memory-lifecycle runner and independent consumers: worker evidence records 18 focused Go passes, 42 Python passes and four selected real-model cases, with unchanged canonical before/after snapshots. Evidence is retained at `evidence/completion/workflow-memory-20261015-v1`; `fullFamilyRun=false` and `automatedCorpusPassed=false`. `29f67f8` integrates ordered model-operation progress, reopen recovery and stale-response rejection. Worker model UI checks failed three cases before repair and passed four afterward; four focused native model checks passed. These are focused results, not full-family or release acceptance.

**Repairs in this checkpoint:** Authenticated connector previews/import preparation/acquisition now share the bounded native flight registry, expose cancellation and retain honest completion-race outcomes. Managed-provider UI is remounted per workspace so pairing tokens, scoped context and proposals cannot remain visible after switching workspaces. A synthetic actual-component regression reproduced the retained pairing token before repair; a separate late-response test passed before repair and is not claimed as a second reproduced failure.

**Current checks:** `/private/tmp/trove-source-cancellation-model-progress-race.jsonl` records **12 tests/subtests passed, zero failures/skips**. TypeScript and **56 frontend unit tests** passed. `/private/tmp/trove-connections-providers-models-browser-v4.json` records **15 browser tests passed, zero skips** across Connections, managed-provider workflows, model acquisition recovery, workspace isolation and scoped remote MCP consent. An earlier combined browser run had six passes and one remote-MCP fixture failure; its cause was not established, and its evidence is preserved. The final run completed with source edits paused. `/private/tmp/trove-connectors-content-race-v2.jsonl` passed 41 Notion/Google/Drive tests/subtests, zero failures/skips after correcting the previously recorded selection-kind fixture. The frontend production build passed at the preceding Connections checkpoint; that artifact predates the latest provider/cancellation/model UI changes and is not current-build evidence.

**Next:** Integrate schema recovery only after the independently found evaluator-v1 formula-dependency guard is repaired. Complete Gmail attachment/derived-content policy and portable recovery before enabling its acquisition. Continue populated schema conversion, deeper Notion capture, capture/P2 workflow repairs, and the remaining image/personal-review evaluation runners. No aggregate feature-completion percentage, live-account/device acceptance, final candidate or release approval is claimed.

### DEV-028 — 15 October 2026, 20:55 IST: schema recovery and capture continuation repairs

**Integrated source:** `04aeb7a` checkpoints connector cancellation/provider workspace isolation. `11edebb` adds explicit proven-complete portable schema-deletion review, unused editable scalar changes and guarded definition/options inverses with monotonic property identity. `4f9493a` repairs the independently identified formula-expression dependency hole: v1/v2 executable references veto deletion/conversion even when no stored value or AST exists. Shared schema/type conflicts were resolved by regenerating the union. Current integrated schema campaign `/private/tmp/trove-schema-integrated-race.jsonl` passed **32 tests/subtests, zero failures/skips**; generator drift checks passed. Worker prior evidence records 127 import/history passes, 11 historical-preimage follow-ups and ten formula-guard passes. Populated migration is still being implemented separately; dependency-bearing conversion is not silently accepted.

**Capture repair:** `0c3ff2d` integrates workspace/grant-bound capture and browser clipping, bounded commit continuation, no-progress detection, cancellation after the current batch, resumable state and canonical parent refresh between batches. Worker negative fixtures reproduced an unbounded no-progress loop, unavailable cancellation, a late draft appearing in another workspace and a second commit after workspace switch. Earlier race assertions were too early; one delayed-preview attempt failed in the browser harness and is not a product failure claim. Final worker evidence records 12 capture/PDF/page-display browser passes and six follow-up capture passes, TypeScript and production frontend/desktop builds. These use mocked service/native boundaries; actual OS PDF/capture dialogs and signed-artifact acceptance remain pending.

**Connector follow-up under implementation:** Independent GitHub regressions recorded ten failing tests/subtests for incomplete text, caller-expanded documentation selection and stale cross-account/selection cursors. Corrections preserve UTF-8, disclose partial evidence, use canonical selections and bind versioned cursors; the focused rerun passed 20 tests/subtests. Two MCP regressions reproduced an unreachable binary-acquisition preview and an overlong resource preview; a focused integrated acquisition campaign now passes 20 tests/subtests. Source acquisition is additionally checked against the authenticated preview's exact remote identity/attachment keys and revision. UI format/attachment/recipient controls passed seven browser tests in `/private/tmp/trove-connections-acquisition-browser-v2.json`; v1 had six passes and one action-combobox locator timeout, corrected to the rendered role. TypeScript passed. These fixture successes do not enable Gmail native acquisition: that boundary remains disabled until the worker's durable-origin policy passes integration.

**Next:** Finish populated schema migration, Gmail provenance/history/portable validation, Notion depth/typed capture, folder-index identity repair and remaining evaluation runners. Worker desktop audit has 11 passing navigation/automation/graph browser workflows; a database fixture lacks the newly integrated layout-read contract and requires correction before its rerun. Full functionality integration and all live/native/release acceptance remain pending.

### DEV-029 — 15 October 2026, 21:05 IST: explicit resolution of uncertain external writes

**Source:** `12ca8ca` checkpoints source-bound GitHub cursors, partial UTF-8 previews, authenticated acquisition membership/revisions and the Drive/Gmail/MCP interface controls. Current coordinator work adds an explicit manual-resolution workflow for providers without an automatic receipt lookup. The user must inspect the exact remote result, record applied/not-applied evidence and confirm the inspected account/change. Fresh identity, selection, account revision and proposal hash are rechecked. The append-only receipt keeps its provider outcome **unknown** and stores a separate attributable user inspection; the proposal becomes resolved and cannot be sent again. New work still requires a separate review. This is an implementation choice within CONN-00/G12 reconciliation, not a claim that manual inspection establishes provider-confirmed success.

**Correctness:** Final send admission now rechecks other sending/unknown proposals for the account; a proposal prepared earlier cannot bypass the uncertain-outcome fence. Repeated identical user resolution is idempotent; a differing later attestation cannot overwrite it. Adapter results cannot populate user attestations. An independent source review found no remaining confirmed blocker in authority, revision, delayed-reconciliation, restoration or replay handling; it did not independently rerun tests.

**Checks:** `/private/tmp/trove-connector-manual-resolution-race-v2.jsonl` records **14 focused tests/subtests passed, zero failures/skips**. The first attempt failed to compile because the new code used Actor.Id instead of the actual Actor.ActorId field; corrected before v2. `/private/tmp/trove-connections-manual-resolution-browser-v1.json` records **eight actual-component browser tests passed, zero skips**, including the inspection acknowledgement, retained uncertainty and exactly one remote-send request. TypeScript and whitespace checks passed. Tests use mocked adapters/bridge; no live external write was performed. The final adapter-attestation clearing line is an additional boundary guard and receives the integration rerun below.

**Next:** Integrate reviewed populated schema checkpoint, Gmail origin policy and folder-index identity fixes; enable Gmail acquisition after current-source policy checks. Continue record-deletion reconciliation, deeper Notion capture, remaining review/image runners and desktop job-state recovery. Full feature integration, live/device acceptance and release qualification remain pending.

### DEV-030 — 15 October 2026, 21:18 IST: Gmail preservation, schema conversion and desktop workflow integration

**Integrated source:** `3d9d6cf` checkpoints uncertain-write resolution and final send admission. `5311e43` persists Gmail origin restrictions across acquired assets, imported pages, shared/moved bytes, derived AI/memory/autofill/tasks/transcripts/summaries and cleanup; history version 7 and portable/native validation retain that policy without credentials. Current native acquisition is enabled only after authenticated attachment-membership/revision checks and this canonical policy integration. Overlong Google previews now retain original evidence and declare partial content. Worker evidence includes 201 reported race passes, zero failures/skips (138 workspace tests executed; the final 63 connector results were cached, with their prior execution retained). Earlier source/asset and meeting-cleanup policy leaks, and intermediate fixture/compile failures, are retained as failures followed by corrections.

`c961595` integrates explicit reviewed conversion of populated text/number/checkbox/date/url/email properties, bounded exact-value inventories and atomic grouped undo. Dependent/unsupported conversions remain rejected. Worker campaigns recorded 147 broad and 54 final focused passes; independent source review found no additional blocker without rerunning tests. `c7917b4` fences folder publication against changed identity and completed scans. Coordinator review found a remaining revoke/reapprove race before the first completed scan; the worker has reproduced it and prepared a dedicated approval-epoch migration, pending integration. `654e766` fixes Jobs UI workspace/grant changes and out-of-order reads. Three component regressions failed before repair; four new cases and the existing workflow passed afterward. `8922380` exposes guarded canonical record/block reads, automation options and search rebuild through authenticated hosting: four real-HTTP workflows initially failed with SCOPE_DENIED, and the worker's corrected campaign passed 19 tests/subtests. Deployed TLS/host lifecycle acceptance remains separate.

**Current integrated checks:** `/private/tmp/trove-schema-gmail-acquisition-folder-integrated-v1.jsonl` records **125 executed race tests/subtests passed, zero failures/skips** (32 connectors, 90 workspace, three desktop bridge; count=1). `/private/tmp/trove-integrated-feature-ui-20261015-v2.json` records **18 actual-component browser passes** across capture, connections and Jobs. Its first attempt used the wrong fixture port and recorded eight passes/ten connection failures; it is retained, not counted as product evidence. The database pattern in that attempt selected no database tests. The separate correctly configured `/private/tmp/trove-integrated-database-ui-20261015-v1.json` now records **12 database browser passes**, covering views, record editing, charts/forms/row actions, timelines, formulas and canonical tasks. TypeScript and the production frontend build passed (`/private/tmp/trove-integrated-feature-build-20261015-v1.log`). Browser providers/native boundaries are fixtures; none establishes live account/device acceptance.

**Remaining implementation:** Owner-scope record/page/collection deletion and deeper recursive/typed Notion capture are active. Direct connected-source AI context is a newly confirmed G12 gap: deliberate canonical imports work, but the AI request currently accepts only canonical evidence. Its implementation must preserve account/selection/revision checks, current access, citations, cancellation, local-default use and explicit external sharing; Gmail-derived external AI remains prohibited. Personal-review runner work reports three of four selected real-model cases passing; the fourth fails its unchanged exact-output oracle despite correct cited suggestion behavior. Full model families, image evaluation, required remote-host/provider gaps and all native/release qualification remain pending. No aggregate completion percentage or full feature completion is claimed.

### DEV-031 — 15 October 2026, 21:28 IST: import deletion integration and remaining quality runners

**Source:** `91a4cbd` checkpoints guarded Gmail acquisition/partial preview evidence. `32e9a31` integrates dedicated version-1 folder approval epochs without changing the frozen external-root schema or LastScan meaning. Every approval/revocation rotates the epoch; delayed scans recheck it inside the writer. Worker evidence retains two reproduced first-scan reapproval failures, then 23 broad and four additional migration/native-restore race passes with zero skips. `8c03e37` integrates explicit proven-complete owner-collection record deletion, source scope binding, retained tombstones/attachments/values and guarded metadata-only undo. Shared generated contracts were regenerated from the combined schema. Worker evidence had 157 broad and 31 focused race passes; independent source review found no blocker without a test rerun.

**Integration failure retained:** Coordinator current-source campaign `/private/tmp/trove-record-index-host-integrated-20261015-v1.jsonl` produced **21 passes and one failure**, zero skips. Repeated source-namespace tests reproduced two failures out of 30: ZIP entry order lets Markdown and CSV renditions compete for the same owning-page source identity. This can bypass the intended collection baseline/scope comparison. The import worker is repairing deterministic identity assignment and adding explicit ordering regressions before the next integration; the original pass evidence does not override this failure. TypeScript and contract drift checks passed.

**Review runner:** `daf4877` integrates executable personal-review scenarios and independently checked receipts/semantic outcomes. Worker focused service/race campaign passed 49 tests/subtests and Python consumers passed 46. The four selected native cases have **three passes and one failure**; risk-suggestion omitted the initial “The” required by the locked exact-output oracle. Correct facts/citation/approved effect do not convert that failure to a pass. Canonical snapshots and native inputs were unchanged. A convenience preflight looked for `.gguf` files while installed packs use `.model`; the wrapper independently selected and hashed the actual verified pack. Retained evidence is `evidence/completion/workflow-reviews-20261015-v1`; full-family and release flags remain false.

**Image runner in progress:** Coordinator added a separate versioned execution contract without modifying the original 24-slot corpus, preserving 12 unavailable photograph/screenshot slots. It binds original-pixel hashes, prompts and independent color/measurement/detail-loss/OCR predicates, native model/projector/helper identities and complete canonical snapshots; only derived image drafts are excluded from this family-specific audit. Initial focused image service/oracle campaign passed 32 tests/subtests, zero failures/skips; updated bound-dimension regressions are running. Independent Python consumers now pass 49 tests. A Python bytecode check initially hit the default cache sandbox restriction; the corrected private-cache invocation passed. A private evaluation helper copy includes a newly compiled production Apple Vision OCR helper; no frozen candidate helper changed. Focused native image execution is next.

**Other active scope:** Deeper Notion capture, whole-page/collection deletion, direct connected-source AI context and EXT are still implementation work. Hosting scope explicitly reserves interactive native model controls for desktop, but the audit found a required background-worker gap: configured headless hosts queue generative reviews without draining them. That repair is active. No representative photo/screenshot, live-account/device, final candidate or release acceptance is claimed.

### DEV-032 — 15 October 2026, 21:35 IST: native image evidence and headless review execution

**Source:** `cbcf356` adds the image runner/independent consumer; `ebb3589` integrates the required headless generative-review worker. Worker evidence records 39 focused race passes, zero skips/failures, a successful CLI build, and an actual compiled CLI/loopback queued→durable-missing-model-failure→clean-restart workflow. Generated HTTP scenarios used a controlled generator and do not establish model quality. Initial test fixtures used a nil required array and UUIDv4 IDs; both were corrected with failures retained. Explicit desktop-only model management remains unchanged by this background-worker implementation.

**Image execution:** Six selected cases ran with the verified local vision pair and newly compiled production OCR helper in a private copy of the native bundle. Evidence at `evidence/completion/workflow-images-20261015-v1` records **four passes/two failures**, unchanged source inputs and six unchanged canonical row/asset snapshot pairs. The red-square model answer and exact white/black/transparent source measurements passed. Both faint-mark cases failed safely with OUTPUT_LIMIT; all six original-image and no-invented-OCR predicates passed. No retry or post-run rewrite changes this result.

**Oracle correction:** The new version-1 execution contract incorrectly assumed that the original 128×96, 8-bit faint variations disappear during normalization. Source-pixel inspection establishes that they remain nonuniform. A separate version-2 contract now requires complete supported model interpretations for those cases; guarded model errors still cannot pass their answer oracle. The original corpus, version-1 contract and failed native evidence remain intact. No quality threshold is weakened, and no version-2 native pass is claimed yet. The completed image service/oracle campaigns each passed 32 tests/subtests; independent Python consumers passed 49 checks. The current combined image/host integration campaign is still running.

**Notion reconciliation:** Source audit found that the existing REST import workflow already recursively captures explicitly selected page/database/data-source roots, typed properties, supplied descendants, paginated values, acquired files, views and original bytes through the G01 normalizer. The hosted MCP adapter supplies connected context and reviewed writes separately. Coordinator is verifying those actual capture/re-import paths before declaring a new recursive MCP implementation necessary. Direct connected-source AI context and whole-object import recovery remain active; real account/device and full quality/release campaigns remain pending.

### DEV-033 — 15 October 2026, 21:55 IST: source-deletion recovery, Notion continuity and connected-context review

**Integrated source:** `3b5eb4e` fixes portable source identity when Markdown and CSV share a canonical page UUID: explicit manifest roles prevent archive entry order from choosing different page/collection identities. This repairs DEV-031's retained integration failure; worker evidence includes 30 repeated identity runs and 162 broad import race passes. `411129b` integrates provider revocation before managed-process shutdown and stale read fences in provider/MCP panels (worker: 8 native, 5 browser and 52 runtime passes). `df03e95` integrates explicit whole-page source deletion from a versioned owner inventory, with graph closure, retained typed rows/assets, exact tombstone inverses and guarded undo. Independent peer review found no additional blocker; worker campaigns passed 15 focused and 171 broad tests. Coordinator's current integrated page/record/identity/Gmail campaign passed **49 tests/subtests, zero failures/skips** (`/private/tmp/trove-page-identity-gmail-integrated-20261015-v1.jsonl`). These counts overlap; they are not additive feature totals. Independent collection visibility remains in progress.

**Notion UI:** Three reproduced failures (workspace/scope retaining transient credentials; a non-progressing capture looping) are repaired. Capture now resets with workspace/scope/namespace/root selection, discards late results, validates capture identity/revision progress and bounds each activation to 500 resumable batches. Connections exposes descendant capture through the existing explicit-root REST workflow, with separate integration-token authorization and inventory review. Original database/property-cursor/partial-inventory regressions are preserved. The final browser campaign passed **16 scenarios, zero skips/failures** (`/private/tmp/trove-notion-connectors-continuity-after-v3.json`). Before evidence retains three actual failures/one pause pass; an earlier malformed fixture caused four setup failures, and the first after run exposed a fixture revision that did not advance on completion (corrected to the real service contract). TypeScript and generated contract checks pass. Coordinator Notion service/MCP campaign passed 53 tests/subtests; loopback image/host/import campaign passed 44. The first image/host run's worker-listener sandbox denial remains recorded, not counted as a production failure or pass.

**Connected context:** `d5156aa`, `c8c64cc` and `5539e35` integrate bounded process-local access receipts, exact owner source references, stable evidence hashes that exclude unsupplied rotating provider metadata, and isolated exact-recipient sharing reviews. Worker focused checks passed 15 tests/subtests, zero skips/failures. No local AI or external-provider hook is activated by these preparations. Automatic approval review rejected the six-file core AI integration because authorization/publication changes were not yet narrowly proven. A concrete formatted, independently reviewed patch is frozen at `/private/tmp/trove-connected-ai-integration-review.patch`, SHA-256 `7d1fe31764efa5b106e938a73cf3d7f8fdb0581c0d1b428b87caa4dccc1e4847`. Review found and corrected a missing final grant-revocation fence; its proposed tests remain unexecuted. Explicit user approval to apply/test this patch is pending. This blocker does not stop independent work.

**Next:** Complete collection-visibility recovery; repair native vision message framing against the installed model's declared template and run affected real-model cases; correct any independently reproduced provider metadata disclosure. After approval, integrate/test connected-source local AI and reviewed provider sharing. Physical-device/live-account, representative-image, consolidated final candidate and release qualification remain pending.

### DEV-034 — 15 October 2026, 22:12 IST: approved connected AI integration and native quality corrections

**Authorization/source:** The user explicitly approved applying/testing the frozen local connected-source AI patch from DEV-033. `d420ce3` integrates it unchanged: exact selected evidence, live access rechecks after context preparation and before publication/reviewed application, separate external citation identity, final grant-revocation fences and sticky Gmail restrictions. Worker broad race campaign passed 258 tests/subtests with one explicit unexecuted prior-telemetry-consumer case; coordinator's integrated connected/provider campaign passed 26 tests/subtests, zero skips/failures. `7c53b05` separately closes a reproduced Gmail-derived page-title leak in provider context manifests and connection selection (worker: two actual before failures, 41 after passes, zero skips/failures).

**Native/interface implementation:** Coordinator adds the native account/session/identity-checked source reader, shared bounded cancellable connector flights, explicit multi-source local AI selection, external-source citations and checked-at/revision/partial labels. Connected-only runs add neither canonical workspace/project scope nor approved memory; Gmail remains local. A focused actual native-model run passed **one case/one generation** with a controlled source reader, exact external citation and repeated source-access validation (`/private/tmp/trove-real-connected-local-ai-20261015-v1.jsonl`); this is not a live-account result. Initial native reader/cancellation campaigns passed 11 then 13 tests; first UI campaign passed 14 scenarios. The next UI run passed 14 and failed one fixture assertion because the fixture did not handle the unrelated memory panel; connected-only UI now omits that panel. No failed run is overwritten.

**Review corrections under current verification:** Independent review found (1) completion events entering a newly selected AI source context, (2) a recursive app read-lock deadlock when source revalidation overlapped an app-state writer, and (3) cancellation arriving before request admission being lost. Repairs bind events/polls/history to the selected context/run, use lifecycle admission with a short app-state snapshot, and retain bounded one-minute cancellation intents. Preparation has visible progress/cancel controls and no long-held flight mutex. Final focused campaign passed **19 native tests/subtests and 16 browser scenarios, zero failures/skips** (`/private/tmp/trove-native-connected-cancellation-v4.jsonl`, `/private/tmp/trove-connected-ai-ui-20261015-v3.json`). TypeScript, regenerated-contract drift and whitespace checks passed. These coordinator changes are checkpointed with this entry.

**Quality evidence:** `5e14d4f` fixes the native vision user-turn terminator required by the verified installed model's declared chat template; three compiled boundary regressions pass. Fresh unchanged-v2 image execution passed one red-square control and failed both faint-mark semantic cases. They now terminate normally but answer incorrectly; all canonical/source bytes remain preserved. The failed three-case evidence is retained under `evidence/completion/workflow-images-framing-20261015-v1`. `357af9b` adds an explicitly versioned personal-review rubric allowing only optional initial article variation in claim prose, while preserving exact facts, quotation, polarity, action/target/body, citations and reviewed effects. Counterexamples reject actor/date/negation/unsupported-content changes. Worker checks: 24 Go race passes and 50 Python checks; a fresh one-generation risk-suggestion case passed with unchanged production prompt/model and exact once-only approved effects. Version-1 failure remains intact; this does not complete the 12-case family.

**Remaining integration:** Independent collection visibility/history and reviewed connected-source sharing into external providers are active. Automatic approval review rejected the proposed provider integration before any edit, stating that its untested authorization, publication, tool-exposure and scope changes were not covered by the earlier exact local-patch approval. Worker is preparing a complete reviewable provider patch and independent regression cases before the coordinator requests the required approval. Local AI work is unaffected. Live accounts/devices, representative image inputs, remaining model quality and consolidated release qualification remain pending.

### DEV-035 — 2026-10-15 22:23 IST: collection recovery integration and provider sharing approval

**Source and verification:** `5e7bec9` checkpoints DEV-034 native/local connected AI integration. `6ab090d` integrates reviewed independent collection visibility, explicit complete-source collection deletion, guarded grouped undo and history version 8. Hiding a collection requires explicit decisions for every live record; owning pages, typed values, schemas, identities and linked placements remain recoverable. Linked views display an unavailable state. Worker evidence: 25 focused and 175 broad service tests/subtests, plus one browser scenario; independent source review found no blocker. Coordinator's integrated import/collection/backup/restore campaign passed **142 tests/subtests, zero failures/skips**, `/private/tmp/trove-collection-import-restore-integrated-20261015-v1.jsonl`; affected linked-view browser scenario passed **1/1**, `/private/tmp/trove-collection-linked-integrated-20261015-v1.json`. TypeScript, generated contract drift and whitespace checks pass. Counts overlap and do not represent feature percentages.

**Confirmed remaining repair:** Semantic identity version 2 omitted owned/linked collection facts: hiding an empty collection could leave its owning page review current. Worker retained two before failures at `/private/tmp/trove-collection-freshness-before.jsonl`; version-3 coverage and scoped review dependencies are under repair, preserving historical events without fabricated review. Changed-membership sibling ordering is a separate active import repair. Explicit unsupported populated/dependent type conversions remain retained for review rather than silently coerced.

**Provider approval:** Automatic review initially blocked external-provider sharing integration (DEV-034). The independently reviewed version-2 patch `/private/tmp/trove-provider-connected-integration-review.patch`, SHA-256 `ce21f4ba55003de5d727414093dd681825bdb404c2524d545024601ee7d74842`, fixes a cancellation race through final source validation and the writer transaction; cancelled/failed cleanup withholds output/checkpoints. The user explicitly approved applying and testing this exact separate patch. Application, derived-contract regeneration and focused tests are now authorized and underway; no test passes or live-account acceptance are inferred. Coordinator will integrate exact-recipient review in the interface. Generic paired inbound MCP connected-source UI is outside this patch and requires scope reconciliation.

**Next:** Integrate the collection freshness repair and provider sharing, repair confirmed ordering gaps, and audit remaining functionality. Physical-device/live-account, representative-image, known image quality failures and consolidated final candidate/release qualification remain separately pending.

### DEV-036 — 2026-10-15 22:31 IST: collection freshness, atomic ordering and approved provider context

**Integrated source:** `5b12739` adds semantic identity version 3 for bound collection visibility/schema/live facts with scoped review dependencies. Presentation/view changes remain excluded. Prior review events remain attributable and require a human review under the expanded coverage; undo never fabricates a review. Coordinator's focused current-source freshness/visibility campaign passed **28 tests/subtests, zero failures/skips**, `/private/tmp/trove-collection-freshness-integrated-20261015-v1.jsonl`. Worker before evidence retains two actual failures; after focused 47 and broad 200 passes overlap, with no failures/skips.

`0c224eb` integrates atomic sibling-order review for evidenced additions/removals/parent moves and rejects incomplete final membership. The worker reproduced the prior scalar-order failure; after checks passed 16 focused and 152 broad tests/subtests, zero failures/skips. HTML end-to-end preserves independent local text and exact reviewed order with idempotent retry. Missing/malformed placement and unsupported dependent schema migrations remain explicit review boundaries.

`e73486a` integrates the exact user-approved managed-provider patch plus generated consumers. Explicit recipient reviews are single-use and expire; source-only grants serialize an empty page list and deny every canonical surface. Gmail context remains prohibited; source/grant changes and cancellation prevent output/checkpoint publication and proposal application. Coordinator's combined provider/connected/order/freshness campaign passed **79 tests/subtests, zero failures/skips**, `/private/tmp/trove-provider-order-freshness-integrated-20261015-v1.jsonl`. Worker broad service checks passed 82 with one explicitly unexecuted 10K benchmark; native checks passed 13. Correct cached-Python affected contract checks passed 29 after preserving an overly broad system-Python failure and a missing-cache retry failure. Generated contract drift, TypeScript and whitespace checks pass on the integrated root.

**Interface and follow-up:** Coordinator's exact-recipient/excerpt consent UI, optional canonical pages, expired-review handling, Gmail restriction and workspace/source fencing passed an initial **20 browser scenarios**, zero failures/skips (`/private/tmp/trove-managed-connected-ui-20261015-v1.json`). Those service bridges are controlled fixtures, not live provider acceptance. A preparation-cancellation gap was confirmed and is being repaired in native service and UI before checkpoint. Independent audit also found delayed P2 graph/image results crossing context and missing OCR cancellation; these are active implementation repairs. Generic paired/remote inbound MCP retains its separately reviewed canonical scope; EXT-003/004/005 do not require connector-context parity, and no such hookup is claimed.

**Additional verification:** The final affected UI campaign passed **23 scenarios**, zero failures/skips (`/private/tmp/trove-managed-connected-ui-20261015-v3.json`), including preparation cancellation and the Connections entry point. Independent UI source review found no blocker. Current frontend production build and all **56 unit tests** pass; existing bundler directive/chunk-size warnings remain.

**Next:** Finish cancellation/P2 continuity, verify complete-source API deletion boundaries against G01-A2, and integrate remaining demonstrated gaps. Known native image semantic failures, representative datasets, real-account/device and final release campaigns remain pending; these tests do not establish full product completion.

### DEV-037 — 2026-10-15 22:46 IST: provider sharing interface, broad service checks and bounded review recovery

**Provider integration:** `b24bd6f` integrates trusted request-ID preparation cancellation, bounded early-cancel intents, native generation aliases, starting-row cancellation cleanup and the provider approval callback lock repair. Integration preserved the earlier `411129b` rule: revoke bridge authority before draining provider execution. Coordinator's selected native campaign passed **15 tests/subtests, zero failures/skips**, `/private/tmp/trove-provider-preparation-integrated-20261015-v1.jsonl`; worker focused campaign passed 21. Root now checkpoints the exact-recipient/excerpt sharing interface, explicit consent, optional canonical pages, source/account/workspace fences, Gmail restriction, preparation cancellation and fresh-review requirement after every start attempt. The final affected interface campaign passed **23 scenarios**, zero failures/skips; independent source review found no blocker. Cancellation before exclusive workspace lifecycle admission is a separately confirmed follow-up under repair.

**Broad current-source evidence:** At integrated service source `e73486a` (before the later cancellation/freshness-limit changes), the full workspace race suite passed **1,246 tests/subtests with 22 explicit skips and zero failures**, `/private/tmp/trove-workspace-integrated-20261015-v1.jsonl`. Skips are native/model workflow suites, prior-telemetry consumer, live bookmark/clipper, 10K performance, WebKit PDF and native diagnostics prerequisites; none are passes. Frontend production build and **56 unit tests** passed at the sharing-interface source; existing bundler directive/chunk warnings remain. This is source verification, not an integrated release candidate or final qualification campaign.

**Packaging correction:** `100398b` aligns fixture sidecars with microphone v2 and the reminder helper. The portable package validator now checks the pinned microphone identity and all required recording/control/input capabilities. Worker reproduced three existing fixture errors and five capability-omission failures before repair; corrected affected Python execution passed **60 tests**, zero failures/errors/skips (inherited/imported repetitions included). Intermediate 59-pass/one reminder-fixture expectation failure is retained. No signing/install/device acceptance is inferred.

**New confirmed freshness regression and repair:** The v3 collection graph bound caused a 1,001-row collection to abort ordinary search, AI evidence, freshness reads and review queues. Before evidence retains four failing subcases plus their failing parent and one passing guarded-review case. Current coordinator repair adds explicit unavailable freshness with no invented computed identity; historical human review remains attributable, ordinary reads continue and new reviews still fail at the original bounded projection. Focused after checks pass **32 tests/subtests**, zero failures/skips (`/private/tmp/trove-freshness-limit-after-20261015-v2.jsonl`), and affected review/form/chart/search/AI UI checks pass **8 scenarios** (`/private/tmp/trove-freshness-unavailable-browser-20261015-v1.json`). This repair is not yet checkpointed with the sharing interface; independent review is pending.

**Remaining work:** Direct successful Notion archive proofs are under implementation, preserving denied/partial/omitted sources and requiring explicit deletion review. P2 delayed-result isolation and cancellable OCR are under final review/test. Known native image semantic failures and all representative/live-account/physical-device/final release acceptance remain recorded.

### DEV-038 — 2026-10-15 22:51 IST: lifecycle and bounded freshness repairs; final functional gaps

**Source:** Sharing UI committed as `d2abf12`. `6c2f40e` integrates provider preparation suspension before exclusive workspace open/shutdown, including admission during overlapping transitions and resuming after a failed workspace selection. Worker before evidence retained failing open/shutdown cases; focused after checks passed 25. Coordinator's integrated provider/native campaign passed **23 tests/subtests, zero failures/skips**, `/private/tmp/trove-provider-lifecycle-integrated-20261015-v1.jsonl`. Packaging plus domain/contract Python checks now passed **89 executions**, zero failures/errors/skips, on integrated source (`/private/tmp/trove-packaging-contracts-integrated-20261015-v1.log`); inherited repetitions remain included.

**Freshness checkpoint:** Independent source review found no blocker in the explicit unavailable state from DEV-037. This checkpoint includes the optional computed identity, required unavailable reason, preserved historical event and disabled new-review control. Generated contract/type checks, TypeScript, 32 focused service race passes and eight affected browser passes are recorded above. The version-3 semantic projection itself is unchanged; unavailable coverage never becomes a new human review or synthetic digest.

**API/P2 integration:** Reviewed Notion worker `4d9b9ad` is integrated: exact successful page/database/data-source archive responses produce version-1 capture/root/object/raw-hash proof and explicit deletion review, with final mapping/finished-capture and local graph-closure guards. Omission/403/404/partial enumeration remains retained, and first-seen archived objects remain inert. Worker passed 172 affected race tests and 22 final focused tests (overlap), including native backup/restore; coordinator integration checks follow. P2 worker `85c5261` passed 19 service/HTTP/contract tests, one native test, eight overlapping strict contract checks and 13 browser scenarios; root reviewed its source without a blocker, integration follows. These are controlled service/browser/native boundaries, not physical OCR/device or model-quality acceptance.

**G02 correction:** The final source audit found no existing task-facet detachment service/UI despite D001 and G02 requirement 3. Earlier editing/promotion/extraction implementation labels remain valid at their scope; detachment is an explicit implementation gap under repair. The plan preserves record/page/property identity and offers reviewed occurrence conversion, or removal for leaf occurrences; nested occurrence children must survive conversion. Grouped undo must preserve unrelated edits. Known image-quality failures and live-account/device/final release campaigns remain separate.

### DEV-039 — 2026-10-15 23:00 IST: P2 continuity integration and follow-up review

**Source and verification:** Notion deletion proof integrated as `a221a5c`; bounded freshness repair as `408ffcf`; P2 request-identity fencing and OCR cancellation as `e75e3bc`. Combined affected workspace/hosting/contracts/native race checks passed **96 tests/subtests, zero failures/skips** (`/private/tmp/trove-p2-notion-lifecycle-integrated-20261015-v1.jsonl`). TypeScript and both generated schema/type drift checks passed. These checks cover controlled service/native boundaries, not physical OCR, live accounts or final release qualification. Reviewed legacy Notion compatibility regression `f84ef0b` integrated as `0125f7c`: old resumed captures retain their API version and require direct archive evidence; current captures reject the removed alias. Worker focused race evidence is 12 passes, zero failures/skips; root rerun is pending.

**Independent review corrections:** Follow-up review of the initial OCR integration confirmed two remaining defects: cancellation after canonical OCR persistence could report that prior text was preserved, and overlapping workspace opens could install an OCR runtime outside the old runtime's cancellation boundary. Both have assigned corrective implementation and deterministic regressions. This appends corrections to the initial review and pass evidence rather than treating those checks as proof of absent defects. Task-facet detachment remains active, including explicit occurrence conversion/removal, guarded grouped undo and persistence/recovery tests. No new candidate, release acceptance or overall completion percentage is claimed.

### DEV-040 — 2026-10-15 23:09 IST: broader UI evidence and remaining identity gaps

**Integrated corrections:** OCR follow-up `6dd3cd3` independently reviewed and integrated as `bcd52fb`. App-owned suspension covers replacement kernels during overlapping workspace transitions; an already persisted extraction returns an honest saved/indexing-pending result after cancellation. Worker evidence: 21 service/HTTP/contract, six native lifecycle and 14 browser passes, zero failures/skips. Root affected checks passed 18 tests/subtests; the hosting case failed to bind its loopback listener in the sandbox and is being rerun with local-listener permission, not counted as a pass. Root legacy Notion rerun passed 11 selected tests/subtests, zero failures/skips (`/private/tmp/trove-notion-legacy-integrated-20261015-v1.jsonl`). Full Python tooling on `186234f` passed **243 tests**, zero failures/errors/skips (`/private/tmp/trove-tooling-integrated-20261015-v1.log`).

**Broader browser investigation:** The initial 202-scenario run on `186234f` was stopped with **93 passes, three failures, one interrupted and 105 unexecuted** (`/private/tmp/trove-frontend-integrated-20261015-v1.json`). One failure was a real regression: the new provider busy guard disabled disconnect while approval was pending. Separate per-connection revocation now remains available, rejects duplicate requests and fences old-context responses; independent review found no blocker. Two failures were stale fixtures: capture used the obsolete `previewed` state and database paging omitted the newly required layout read. Corrected fixtures retain the original assertions. The interrupted editor selection case and its layout counterpart pass unchanged in the focused after run: **five passes, zero failures/skips** (`/private/tmp/trove-provider-revocation-fixtures-after-20261015-v1.json`). No product fix is inferred for the interrupted case. Browser report/screenshot defaults now use scratch test output; a test-list command's accidental historical-report rewrite was restored byte-for-byte to Git before checkpoint (SHA-256 `1085e76d1f2f4b57a531f5c0bfcec92f1842c2f36b788a004a0f2d4a210ae406`).

**New concrete omissions under repair:** Actual-App audit reproduced three failures: delayed shell responses can replace newer state/workspace, and valid block deep links are rejected because shell snapshots omit blocks. The fix is being checked with serialized/coalesced reads and scoped page resolution. G01 audit confirmed repeated stable source IDs and renamed files without stable IDs can create new pages without D002 identity review. Explicit link-existing/create-new/skip decisions, retained aliases and target revision guards are being implemented; ordinary new stable-ID imports remain unchanged. G02 detachment review additionally identified reminder reactivation after removing a completed facet; the immutable preview will disclose and bind exact reminder effects. These findings correct prior scoped implementation labels without erasing their evidence. Native image quality failures, real-account/device checks and release qualification remain pending.

### DEV-041 — 2026-10-15 23:18 IST: task detachment and shell integration; broad source checks

**Source:** `d5c90f8` checkpoints the reviewed provider revocation and fixture corrections. `acc7d27` integrates G02 worker `ca40c5d`: immutable complete occurrence review; explicit conversion/removal; unchanged record/page/property identity and values; grouped undo preserving later unrelated edits; monotonic restored block revisions; exact reminder reactivation effects and stale configuration rejection. The maximum is 127 occurrences per detach; nested/attached occurrences require conversion. Shared route conflicts were resolved as a union with OCR cancellation, then consumers regenerated. `a928201` integrates reviewed worker `51f1866`: serialized/coalesced shell reads, scoped block-link resolution, current workspace/revision fences, manual navigation intent and unmount guards. Four reproduced pre-fix shell/link/navigation failures remain retained in worker evidence.

**Current affected verification:** Integrated task/undo/host-route race checks passed **22 tests/subtests, zero failures/skips** (`/private/tmp/trove-task-detach-integrated-20261015-v1.jsonl`); actual-App shell links plus task component checks passed **15 browser scenarios, zero failures/skips** (`/private/tmp/trove-task-shell-integrated-20261015-v1.json`). TypeScript and generated schema/type drift checks passed. The previously sandbox-denied OCR HTTP case passed with local listener permission (`/private/tmp/trove-ocr-host-integrated-20261015-v2.jsonl`). Worker detachment evidence separately includes native backup verification/restart and retained failed intermediate fixtures; no physical reminder delivery is inferred.

**Broader evidence at `d5c90f8`:** The complete Go race command passed **2,074 tests/subtests across 20 passing packages, zero failures, 65 skips** (`/private/tmp/trove-all-go-integrated-20261015-v1.jsonl`). Skips cover explicit real-model/device/provider/helper, platform, performance and native qualification dependencies; none count as passes. Browser rerun recorded **190 passes, 11 failures and two optional real-video skips** (`/private/tmp/trove-frontend-integrated-20261015-v2.json`). Ten failures were connection-refused setup errors from support/update suites' separate default ports; the unchanged ten scenarios passed against the correct server (`/private/tmp/trove-support-update-integrated-20261015-v1.json`). The remaining OCR scenario assumes the old mid-flight image-selection behavior and is being aligned with acknowledged cancellation while retaining provenance and stale-result assertions. Earlier failure evidence remains unchanged. G01 explicit identity review is the remaining confirmed implementation gap under repair before final integration/build; model-quality, authorized live-account/device and release acceptance remain pending.

### DEV-042 — 2026-10-15 23:24 IST: OCR workflow regression alignment and build staging

**Source and evidence:** `dc54ec9` integrates worker `9244d63`: the portable OCR scenario now verifies the deliberate one-flight UI, exact request/workspace cancellation acknowledgement and subsequent image selection, retaining engine/source provenance assertions. Three additional cases reject late successful OCR results after source cursor, workspace or grant changes. Integrated root execution passed **15 browser scenarios, zero failures/skips** (`/private/tmp/trove-ocr-fixtures-integrated-20261015-v1.json`). `f8c3975` integrates worker `8b739d5`: support/update test URLs now honor their explicit overrides, then the common test URL, then their legacy defaults; ten unchanged scenarios passed using only the common URL. This corrects the DEV-041 harness setup failures without changing product behavior.

**Build preparation:** Created a new ignored development output directory, `build/macos-feature-integration-20261015`, containing a separate 30-file native input copy and its SHA-256 origin inventory. The packaging script will rebuild current helpers before producing the app. No candidate-7 or other original helper bytes changed; no app build or qualification is claimed from preparation. G01 identity review is under independent review; review found and is correcting a legacy content-duplicate path that could contradict a frozen identity choice. Final integration/build follows that repair and its tests.

### DEV-043 — 2026-10-15 23:36 IST: current helper build and packaging review

**Native build:** On primary source `7e77764`, rebuilt the pinned llama, whisper, microphone v2, vision, document extraction, audio decoding and PDF helpers successfully. The new APFS output is `/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/native-staging`; `native-build.log` and `native-build-inventory.json` retain command outcome and all 54 file hashes. This replaces the earlier exFAT staging destination for the new app; the earlier copy, original helpers and candidate 7 remain unchanged. Cached pinned sources supplied the speech fixture and license files. No inference, device measurement, application build or release qualification is inferred from helper compilation.

**Review:** Final packaging review found the Developer ID validator still expected the old microphone capability list, which would reject the implemented capture-controls/input-devices helper. A narrow validator repair and regression are in progress; ad-hoc development packaging is unaffected. G01 review additionally exposed legacy content duplication and partial/rendition identity alias edge cases. Those are being resolved before the worker checkpoint; its intermediate passing results do not yet count as integrated root evidence. Next: integrate reviewed final identity/signing changes, run combined checks and assemble the new app.

### DEV-044 — 2026-10-15 23:49 IST: explicit import identity and packaging repair integrated

**Source:** `71a3f7f` integrates worker checkpoint `82b2064`: original source identity remains unchanged; all duplicate occurrences require explicit link-existing/create-new/skip choices; unmatched derived locators in established namespaces require review. Versioned exact aliases bind locator/group/mapping/target, survive restart/native backup and reject stale target or baseline changes. Linking without retained evidence requires baseline-unavailable field review. Changed partial inventories and raw-ID/portable rendition changes cannot silently select an alias. Legacy content duplication cannot override the frozen identity. Skipped parent/reference dependencies and unsupported plain-to-typed identity changes reject before canonical writes. Two worker turns hit model-capacity errors after the final repair; coordinator inspected the final delta and completed their checkpoint/integration, preserving their work and evidence.

**Evidence:** Retained worker broad v3 has **192 test/subtest passes**, zero failures/skips (193 if the passing package is also counted); final canonical-edge run has **18 test/subtest passes**, zero failures/skips; three actual import-panel browser scenarios pass. The preceding canonical edge case failed before repair, and earlier failed development runs remain retained in `/private/tmp/trove-import-identity-*`. These counts correct worker messages that mixed package and test counts. Integrated root import regressions and the complete frontend browser suite are running; TypeScript, regenerated schema/type drift and whitespace checks pass. No worker-only result is represented as an integrated root pass.

**Packaging:** `7bcf608` integrates reviewed worker `5d4e127`; signing now requires the complete current microphone capabilities. Integrated signing/desktop-package/microphone tests passed **31 tests**, zero failures/skips, in `macos-feature-integration-20261015/packaging-regressions.log` under the external artifact directory recorded in DEV-043. The prior current-helper rejection and old-helper acceptance were both reproduced and retained. No actual Developer ID signing/notarization is claimed. Next: finish combined tests, build the full macOS development app from this source, and record its own candidate identity.

### DEV-045 — 2026-10-16 00:02 IST: integrated macOS app, frozen candidate and consolidated source checks

**Build/candidate:** Source `9a3948529a1e772b1e5fdb710203e2c1d234d3cc` produced the full P2 development app at `/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/Trove.app`. New candidate ID is `76951ebbe0ac3d2baec0bd98cc9ba4973b2848d084fe91952d92c445a0d01504`; `qualification/candidate.json` binds the clean source archive, app, native helpers, installed model inventory, fixtures and current policy. Hardware is Mac16,12 / arm64 / 24 GiB RAM, macOS 27.0.1. Native compilation and production frontend build passed, retaining existing bundle-size/directive/deprecation warnings. Ad-hoc deep/strict signature verification passed. Independent read-only native bindings, executable inventory, reminder identity/version and both reminder Swift source hashes match. This is not Developer ID signing, notarization or release qualification.

**Integrated evidence:** In the same external artifact directory, `all-go-race.jsonl` records **2,103 passing tests/subtests, zero failures and 65 explicit skips**, with 20 passing packages (four packages contain no tests). `import-integration.jsonl` separately records 175 selected import/contract passes, zero failures/skips. Python tooling passed **245 tests**; frontend units passed **56 tests**; TypeScript, generated contracts and whitespace checks passed. The full browser run records **220 passes, one failure and two optional external-video skips**. Worker investigation proved a separate controlled cursor-reconciliation race in the fixture, while the exact original unchanged-text outcome was not independently reproduced. Test-only `73c9f6f` integrates `a393b40`: synchronize the editor selection, assert model/DOM focus and caret agreement, then send actual keyboard input. Original marks/link/undo/reload assertions remain. Three affected worker scenarios passed; root runs cover the failed layout workflow and clipboard plus the two direct table/layout workflows separately. The original broad failure remains retained; no product repair or retroactive all-pass result is claimed. This descendant changes a test only; the app remains exactly source `9a39485`.

**Packaged operation:** OS-denied-network P0, P1 and P2 create/reopen/restore journeys passed in separate isolated workspaces. A separate packaged native smoke passed installed chat, embedding and speech execution with the pinned audio fixture; production microphone capture is explicitly not run. These are service-driven native-app checks, not manual editor/accessibility, physical recording, notifications, clean-install or performance acceptance. Evidence is in `packaged-p0`, `packaged-p1`, `packaged-p2` and `packaged-native`. Original worker before/after import, signing and editor investigation logs/patches are copied to `review-evidence` without rewriting them.

**Next phase:** Started the consolidated eight-family local-model campaign serially from a clean detached checkout of the frozen `9a39485` source, using the new packaged helpers and synthetic locked corpora. The previous worker branch is preserved. No new result is claimed until each terminal report and its independent consumer checks complete. Known prior image semantic failures, missing authorized representative photos/screenshots, required real accounts/devices, manual quality, controlled performance and signing/install/recovery campaigns remain separately open. Formal qualification currently has 66 pending checks and no granted release approval; the successful development checks above have not been converted into qualification receipts.

### DEV-046 — 2026-10-16 00:09 IST: corrected candidate model-input binding

**Correction to DEV-045:** The first candidate record bound the entire mutable model-cache directory. The packaged native smoke subsequently created three empty ownership lock files for the chat/embedding/speech packs. The qualifier correctly rejected the changed directory inventory; the original candidate `76951eb...` and failed template attempt remain preserved and must not be used as a valid qualification record. `candidate-inventory-correction.json` proves exactly three added `.owner.lock` paths, no removed files and no changed existing bytes. No model weights/manifests or app/helper bytes changed.

A fresh `qualification-v2/candidate.json` now binds each of the 16 installed model/manifest files individually, together with the same app/source/helper/fixture inputs. Its identity is **`42d3dd5a5c3186b05d1859ccc19f6916d3e67eda446a062c670298c322b444d9`**, source `9a39485`. This excludes mutable runtime locks from the model-input set without weakening any bound model hash. The serial evaluation runner already binds exact model/manifest files, so its in-progress evidence is unaffected. Formal candidate service-check execution and packaged-observation preparation have started; no receipt is yet claimed. The completion structural validator passes with the unchanged historical registry/catalog counts and `releaseQualified=false`.

Root fixture follow-ups are complete: the failed rich-layout scenario and clipboard scenario passed in `editor-after.json`; direct table/layout editing, marks, undo/redo and reload passed in `editor-caret-after.json` (two more scenarios). These supplement the retained failed broad run rather than replace it. The eight-family native model campaign remains active; nine early P0 reports received independent read-only semantic inspection without a finding, not family acceptance. Next: complete and review each family's terminal accounting, then reconcile candidate receipts and remaining external/manual prerequisites.

### DEV-047 — 2026-10-16 00:38 IST: P0 model results, storage interruption and restored execution prerequisites

**Completed evidence:** Frozen source `9a3948529a1e772b1e5fdb710203e2c1d234d3cc`, candidate `42d3dd5a5c3186b05d1859ccc19f6916d3e67eda446a062c670298c322b444d9`: the full P0 family finished with **24/24 passing cases, zero failures/skips/missing cases**, unchanged input hashes, successful independent canonical-evidence consumer checks and `automatedCorpusPassed=true`. An independent agent reviewed all raw/displayed answers and corpus expectations, checked all 24 canonical before/after pairs, and found no substantive semantic issue. This is bounded synthetic-corpus evidence, not human usability or broad real-world quality acceptance. Retained evidence is `model-p0/{manifest.json,results.jsonl,canonical/}` and `independent-p0-semantic-review.json` under the DEV-045 artifact directory; the review SHA-256 is `10d0e87bdafea782f0a915cb7f475a1be4deff73a5d92ce27a8073f40ba0ece6`. The candidate-bound `p0-service-regressions` receipt passed at 00:09:17 IST: **one of 66 formal checks passed, 65 pending**, with no release qualification/approval granted.

**Interruption:** At approximately 00:13 IST the seven later families refused startup because internal free space fell to about 263 MB, below the existing 4 GiB runner precondition. They executed no model cases. HD-B1 subsequently became unmounted, preventing the primary tracker update and access to shared Git metadata. Original refusal logs and `model-campaign.jsonl` remain unchanged. A normal mount attempt stalled and was interrupted without repair, formatting or forced mounting. The task-owned disposable Go build cache was cleared with `go clean -cache`, recovering about 5 GiB; source, app, model weights, fixtures and evidence were retained. The retained source archive hash was reverified. Machine evidence is `campaign-storage-incident-20261016-v1.json` and `-v2.json` in the same artifact directory.

**Recovery and next action:** The user confirmed the drive was back. Read-only checks find the primary branch clean at `cc6ebbb`, the execution worktree clean/detached at `9a39485`, HD-B1 mounted, and approximately 9 GiB free internally. This delayed tracker entry reconciles the completed result and interruption now that the authoritative file is writable. Resume only the seven unstarted families serially into fresh `model-<family>-resume-v1` directories, retaining one generation per case and all existing oracles. Stop for another unmet resource prerequisite; do not loop over unchanged refusals. Review terminal outcomes independently and record actual failures, missing representative images and manual requirements separately. No product or frozen-candidate inputs change in this recovery step.

### DEV-048 — 2026-10-16 00:46 IST: resumed holdout failure and packaged-observation scope review

**Resume:** Commit `5cfb976` checkpoints DEV-047. The resumed serial campaign uses unchanged frozen source `9a39485` and candidate `42d3dd5a...`; `run-model-campaign-resume-v1.py`, its journal/log and fresh per-family directories retain this execution separately from the startup refusals. The full P0 holdout family finished **11/12 passed, one failed, zero missing cases**, with unchanged input hashes and successful canonical preservation checks. `review-date-absent` asks when a review is scheduled; its evidence states an agenda and an unrelated unassigned owner. The model returned and cited the agenda instead of the required exclusive evidence-absent abstention. Independent review confirms a substantive relevance failure rather than an oracle wording problem or fabricated fact. `automatedCorpusPassed=false`; no retry or oracle change has occurred. Evidence is `model-p0-holdouts-resume-v1` under the DEV-045 artifact directory. The modes family is now executing. **EVAL-08: answer relevance/abstention repair** is a new tracked defect; investigate a general repair with independent regressions, preserving the failed case and all original corpora.

**Packaged evidence:** The exact app's post-freeze model-free P0 create/reopen/restore fixture passed in `packaged-p0-postfreeze-v1`, with OS-denied networking and isolated workspace. Independent review (`independent-packaged-p0-postfreeze-v1-scope-review.json`) finds that the fixture's public snapshot comparisons and recording duration/segment checks alone are narrower than the formal restore scenario's exact content/history/timing/asset/audio/source-preservation requirements. Earlier fixture passes remain valid at their actual assertion scope; they do not establish the broader gate by themselves. Read-only inspection of retained fixture bytes is in progress. The formal offline scenario additionally requires installed-model operation, which will run after the serial campaign. No packaged qualification receipt has been submitted.

**Follow-up at 00:50 IST:** Independent read-only retained-fixture inspection completed **51 checks across all 128 SQLite tables**, with no fixture byte changes during inspection. Relative to the retained pre-restore backup, source differences are limited to backup completion/pin release. Restored prior canonical rows, 14 mutation-history rows, 23 page snapshots/events, six original asset files and recording identity/start/gap timing are preserved; explicit restore, retrieval and recording-finalization changes are separately accounted. Original and backup PCM are exactly equal (128,000 bytes); restored WAV has the expected 32,000 silent 16-bit samples at 16 kHz, including the audio tail. The fixture is synthetic silent audio and does not qualify a real microphone. `independent-packaged-p0-postfreeze-v1-preservation.json` (SHA-256 `4c21da2b5529f6d4fceb8370eeb94ba4eca1c3e615f8116f4e78a699d0f60066`) and its retained inspection script supplement the narrow packaged assertions. No pre-create baseline was fabricated. Offline installed-model evidence remains pending.

**Repair experiment preparation:** EVAL-08's publication path checks quotes/current authority but does not deterministically establish answer relevance. Prepare one answer-mode-only guidance candidate in a separate worker checkout and independently lock 12 balanced transfer cases before model execution. Preserve the failed question, existing positive controls, exact request/token accounting and one-generation rule. Do not infer semantic safety from a model-authored flag, add hidden retries, filter inconvenient claims or synthesize an abstention. This is an unproven candidate experiment, not an adopted production repair. Original frozen campaign inputs stay unchanged.

### DEV-049 — 2026-10-16 00:59 IST: complete modes accounting and unintegrated repair controls

**Frozen campaign:** `model-modes-resume-v1` finished on source `9a39485` / candidate `42d3dd5a...` with **25/42 automated passes, 17 failures, no missing cases**, unchanged inputs and canonical preservation. Original failures include numeric grouping (`26,500` versus a literal `26500` expectation), code layout comparison, missing words in requested database titles, malformed diagram edges, syntactically invalid commented code and unsupported factual elaboration. These causes overlap: correcting an oracle does not establish semantic correctness of an output that also invents a claim. EVAL-09 tracks the separately confirmed strict-number and syntax-tree oracle limitations; EVAL-10 tracks substantive output defects. Independent review of all 42 raw/displayed outputs is underway; 27 declared manual semantic assessments are not marked complete by automation. Original grades and corpus bytes remain unchanged. The context family has started serially.

**EVAL-08 candidate checks:** The first isolated answerability-guidance candidate exceeded an existing 1,024/256 word-budget fixture; the failed run is retained. A compact revision preserves all prior policies, adds only 32 whitespace words for answer-mode guidance, and passes the same focused six top-level tests (**22 tests/subtests, zero failures/skips**), without raising budgets. Native token cost and semantic effectiveness are unverified. Patch v2 SHA-256 is `57aa282780ff257511a46267daf9b4369c15f167f0f4fca62ebc0b93ab43273f`; v1/v2 patches and test logs are copied into the artifact directory's `review-evidence`. Worker branch `codex/ai-answerability-repair` is based on `9a39485`; changes remain uncommitted/unintegrated. Neither root production source nor the frozen execution checkout has changed.

**Independent transfer fixtures:** A separate worker locked 12 new cases across four domains, balanced equally across absent, explicit-negative and supported-positive evidence, without inspecting the proposed prompt. Original `answerability-transfer-v1.json` SHA-256 `a3a0a837c042955037b2e6449ff332e65cfb3bb934c43f4b45476b087a5c6d2d` remains retained. Before any model/test execution, source inspection found overescaped regex punctuation that would reject supported golden examples. A separately versioned syntax-only correction and additive harness are being prepared; no source/question/expected fact or model result is being changed. Golden and poison checks must pass before the controlled native pilot. Existing transfer and closure corpora remain untouched.

**Inspection correction at 01:00 IST:** The worker retracted the reported escaping defect after checking decoded character values. Coordinator inspection confirms one backslash before the literal punctuation, all 16 regexes compile, and the sealed corpus still has SHA-256 `a3a0a837...`. The earlier diagnosis arose from reading multiple layers of escaped display text; it was not an executed golden-test failure. No corpus bytes were changed and no v2 corpus was created. Keep the exact v1 binding and require actual golden/poison execution before any model pilot.

### DEV-050 — 2026-10-16 01:09 IST: evaluator corrections, independent transfer harness and resumed families

**Source integration:** On primary `bd01af9`, applied the separately reviewed test-only mode-oracle patch `832a8e829b703edbf42b9e5f34253135031fa2d2180ec600268474fa9e76faae` and additive answerability harness `472e98da428880768c1be087d436c0e82039fe19f7f00f89060e79f711ee65b7`. No production prompt/runtime code changed. The frozen `9a39485` checkout, package, original corpora and run grades remain unchanged. Strict decimal-string normalization accepts only valid English grouping without changing signs/digits/precision; Go AST comparison ignores layout while retaining syntax-bearing token presence. Independent before/after checks demonstrate both old evaluator defects; corrected checks pass **45 tests/subtests, zero failures/skips**, including all 42 unchanged mode goldens. Evidence and exact command/source hashes are retained in `review-evidence/trove-expanded-mode-oracle-*` under the DEV-045 artifact root.

**Transfer harness:** Sealed corpus `a3a0a837...` is copied byte-identically into an additive test fixture and bound by its exact hash. The original transfer execution loop is shared without changing its behavior. Independent peer review found no blocker. Golden, 68 locked semantic poison, structural/scope and cross-state controls run before any native pilot. Root integration command used `-tags 'fts5 sqlite_omit_load_extension'`, seven focused test selectors and no race/model execution: **97 tests/subtests passed, zero failures/skips** in `evaluation-harness-integration-v1.jsonl`. The earlier manifest's `sqlite_fts5` command was incorrect and is being explicitly corrected; no run used it. The answer-only production candidate remains unintegrated and semantically unproven; native BEFORE/AFTER execution must wait for the serial campaign.

**Independent modes assessment:** All 42 outputs were reviewed. Of the 17 original automated failures, **11 contain substantive defects**, five fail only numeric display comparison and one fails only code-layout comparison. Mermaid 12.0.0 parsing independently rejects cases 1/3; case 2 parses but has two incorrect isolated nodes and zero required edges. No original grade was changed. Parser evidence `independent-modes-resume-v1-mermaid-parser.json` (`eff6dff4...`) and assessment supplement (`c9c7ac7e...`) preserve these findings. Required human usability/semantic acceptance is not supplied by this automated-agent review.

**Frozen campaign continuation:** Context finished **11/12**, related content **11/12**, and memory lifecycle **12/12** automated passes, with no missing cases and unchanged bound inputs/canonical evidence. Context `near-budget` used 7,664 prompt tokens with a 512-token output reservation, retained the fact and produced the correct launch answer; its exact source quote includes extensive filler and fails the predeclared exact-short-quote check. This is EVAL-11 pending independent scope assessment, not established wrong-answer behavior. Related `budget` retrieves the relevant source but ranks unrelated content above it (EVAL-12). All raw failures remain retained under the separate `model-*-resume-v1` directories. Reviews/images continue serially; internal free space remains close to the runner's 4 GiB precondition, and the existing additional-space question remains pending. Formal candidate state remains **1/66 checks passed, 65 pending**, unqualified/unapproved.

### DEV-051 — 2026-10-16 01:19 IST: resumed campaign completed, storage recovered and packaged receipt recorded

**Campaign outcome:** The serial resumed process exited normally after all seven resumed families, retaining quality failures. Including the earlier completed P0 family, frozen product `9a39485` has P0 **24/24**, holdouts **11/12**, modes **25/42**, context **11/12**, related **11/12**, memory lifecycle **12/12**, reviews **10/12**, and images **9 passed / 3 failed / 12 skipped**. Aggregate: **113 passed, 25 failed, 12 required-input skips out of 150 declared cases**; 138 cases executed. These are automated case counts, not overall product readiness or human task-success estimates. All bound input inventories remained unchanged and executed-case canonical checks preserved their before/after evidence; the 12 skipped image cases have no execution/preservation proof. Representative screenshots/photos remain unavailable. Original raw outputs, failed grades and frozen corpora remain unchanged. No campaign is left running in the background.

**Independent findings:** Context's correct answer cites a 487-character verbatim span containing the 43-character launch fact plus filler; exact-extent rejection is an oracle limitation, with quote precision still a quality caveat. Related `budget` is a genuine ranking failure; passing `travel`/`meeting` retrieve the right page but show a generic title-only excerpt. Memory lifecycle has four actual generations and eight native-tokenizer/service-boundary cases; all 12 preserved canonical bytes and no additional substantive issue was found. Reviews has seven actual generations and five service cases: `weekly-summary` omits the explicitly requested documentation fact, and `categories` substitutes page-title text for the requested security decision. Its captured warning also claims an exact omitted count although the bounded query establishes only a lower bound (EVAL-13). Images' three failed plain-pixel descriptions are `1.0.`, `1.0.` and `Yes.`; transport/OCR/provenance passing does not make them useful descriptions (EVAL-14). Independent context/related/memory/reviews assessment JSON files remain under the DEV-045 artifact root; all 50 inspected main/supplemental canonical pairs matched. Image review is continuing.

**Storage recovery:** Internal free space fell to about 3.8 GiB during inference. Read-only inspection identified an unused 11 GiB Go build cache at the completion cache's `go-cache`; its own README identifies regenerable build artifacts, and active processes used `/private/tmp/trove-feature-go-cache`. Standard `go clean -cache` targeted only that unused cache, completed successfully, and restored over 15 GiB free. Active cache, models, source, app, ordinary workspace and evidence were preserved. The additional-space request is resolved for this execution without user cleanup.

**Packaged qualification:** Fresh `packaged-native-postfreeze-v1` ran the unchanged app offline after the model campaign: model-free create/reopen and installed chat/embedding/speech checks passed, with OS network denial and isolated fixtures. Together with post-freeze P0 create/restore and the independent 51-check preservation artifact, this supports the formal packaged service journey. Observation v1 was rejected because observation artifacts must be individual files; it supplied directory bindings. Recomputed identities confirmed no bytes changed. Preserved v1; v2 binds the actual report files, and `p0-packaged-journey` receipt passed at `2026-10-15T19:48:28Z`, observation SHA-256 `7f53202f7d84fad42895ac177e2e466d7148d0059de9c8a71be714a2dde3a0f1`. Candidate now has **2/66 formal checks passed, 64 pending**; still unqualified/unapproved. This is automated packaged-process evidence, not foreground UI, physical microphone, Developer ID, clean-install, performance or human acceptance.

**Next controlled work:** Commit `0226841` records DEV-050's tested evaluation-only integration. A reviewed artifact runner (`run-answerability-transfer-pilot-v1.py`, SHA-256 `3c7204e15fd52767d44c209f3124580296895543c6a1fe15ba3dd41784e76e10`) is executing the sealed 12-case BEFORE pilot against that committed source, with one generation per case and OS-denied networking. Golden/poison/source gates passed before model admission. Source remains unchanged until completion; the candidate prompt remains unapplied. Subsequent AFTER must retain identical corpus/model/helper/harness inputs and compare actual outcomes, including failures. Independent workers prepare separate mode guidance/Go-comment guards and the omission-count correction; no unproven repair is counted as complete.

### DEV-052 — 2026-10-16 01:25 IST: independent answerability baseline and controlled candidate

**BEFORE:** `answerability-before-v1` completed all 12 sealed cases on source `0226841`, exactly one generation each, unchanged bound inputs and successful retained canonical checks. Automated result is **5 passed / 7 failed**, with `executionComplete=true` and no skipped/missing cases. All four explicit-negative cases and one supported-positive case pass. Root inspection identifies a valid supported bicycle torque paraphrase outside the narrow predeclared claim grammar; its original failed grade remains. Other failures include absent-evidence protocol/citation errors and incorrect sesame/growing-medium conclusions despite available evidence. One unsupported numeric answer was rejected by the existing production validator. Keep structural execution, semantic correctness and displayed-error behavior separate; no oracle was changed after seeing these outputs.

**Experimental AFTER source:** Applied only answerability candidate v2 `57aa282780ff257511a46267daf9b4369c15f167f0f4fca62ebc0b93ab43273f` to the primary development checkout for a committed, reproducible comparison. It adds a compact generic answer-mode example inside measured context; evidence grammar, rendering, generation count, authority checks and publication rules remain unchanged. Root context/citation/original-transfer/new-transfer checks pass **94 tests/subtests, zero failures/skips** (`answerability-candidate-integration-v1.jsonl`). This is an experiment, not an adopted successful semantic repair. The unchanged development app/candidate and frozen campaign remain at `9a39485`. Run the exact same 12 controls AFTER, assess every output and preserve any regression; reject or revise the product candidate based on evidence rather than rerunning for a favorable outcome.

**Separate repair preparation:** Worker mode candidate v3 (`602275a4...`) adds generic mode guidance and a Go-only comment-proposal guard. Its independent injected-draft tests demonstrate 20 unsafe proposals accepted BEFORE and **78 focused passes AFTER**, zero failures/skips; it remains unintegrated and has no model-quality evidence. A separate worker sealed 16 new mode controls (`9d23ba55...`) before inspecting that candidate. The omission-warning correction also has a retained failing regression and 39 affected race passes in isolation, pending integration. Image independent review confirms six correct diagrams, three correct direct pixel measurements, three substantive vision failures and 12 required-input skips; all 12 available original PNGs/canonical snapshots were preserved (`independent-images-resume-v1-semantic-review.json`, `7f08dad8...`).

### DEV-053 — 2026-10-16 01:44 IST: reject answerability experiment and integrate focused correctness repairs

**Answerability outcome:** The sealed AFTER pilot on `48473da` completed all 12 cases, one generation each, with unchanged bound inputs and retained exact canonical pairs. Automated grades are **4 passed / 8 failed**, compared with BEFORE **5 / 7**. Independent assessment finds four substantive failures, two honest-absence protocol/citation failures, four correct explicit-negative answers and two valid positive paraphrases outside the narrow grammar. The museum-positive grade change is lexical, not a semantic regression. However, museum-absent now publishes an unsupported language assertion with a floor-map quote, whereas BEFORE the numeric-support validator rejected its unsupported answer. The greenhouse-positive answer remains wrong, now substituting an unrelated grow-light fact. No generalized improvement was demonstrated; the candidate is **rejected**, and its three source/test files were restored byte-for-byte to `0226841`. This single paired experiment does not establish an aggregate hallucination-rate change. Both runs, unchanged grades, temporary commit and independent review remain preserved: AFTER review SHA-256 `842b756389b79f260d52b24d9878764982c5d7682e6405f54c9e79a0285982d4`, comparison `8c20e173641082a4f0c777964cf5a3ec8d71e523000c4d40b17299e50ea81cee` under the DEV-045 artifact root.

**Integrated repairs:** Applied only the independently reviewed Go comment proposal guard from mode candidate v3 (`review-evidence/go-comment-guard-v1.patch`, SHA-256 `b9fba29d4026cf39d429c988cc101ee7e87aa1de5132c6d124f996db4f66af16`). Full reconstructed Go programs/declarations must parse and preserve their syntax tree, including token-presence distinctions; changed directives and cgo are rejected conservatively. Incomplete original fragments and other languages retain their existing review path. This validates proposals; it does not establish model generation quality. Mode prompt guidance remains unapplied. Separately integrated omission disclosure v2 (`review-omission-disclosure-v2.patch`, `d5d7f4a82aec95bb8a136d6591ae213cf7cf92e6c87613f51c3a2b8d122c87bd`): a bounded capture now says “at least” for its known lower bound. Worker failing-before evidence and 39 affected race passes remain retained. Root affected comment/code/advanced-AI/personal-review integration checks pass **91 tests/subtests, zero failures/skips** (`guard-omission-integration-v1.jsonl`). No canonical schema, review authority or automatic application policy changes.

**Embedding investigation:** EVAL-15 records a concrete native mismatch: installed pack 102 GGUF declares BGE v1.5 CLS pooling while the worker forces mean. An isolated candidate honors model-selected sequence pooling, rejects unsupported/unpooled output and versions embedding identity so old vectors cannot mix with the new pipeline. The new identity regression fails BEFORE; two native identity and three existing invalidation/scope tests pass AFTER. A private ad-hoc helper was built against the exact existing pinned runtime libraries with retained source/link provenance; no model run or ranking improvement is yet claimed. The frozen app/helper remains unchanged. Separate personal-review guidance and 16 independently locked mode controls are prepared for controlled evaluation, not adopted as successful repairs.

**State and next action:** Frozen app `9a39485`, candidate `42d3dd5a...`, campaign totals and formal **2/66 passed, 64 pending** remain unchanged. Independent image review is complete with its original failures/skips preserved. Review the pooling candidate, run bounded native related-content comparison, and evaluate remaining prompt candidates once their independent controls and review gates are ready. Keep all source changes separate from release acceptance.

### DEV-054 — 2026-10-16 01:46 IST: bind embedding coordinates to the verified producer

**Source and review:** Commit `b5dfc98` checkpoints DEV-053's rejection and correctness fixes. Its Git timestamp is **01:40:56 IST**; DEV-053's manually entered 01:44 heading was late by approximately three minutes and is retained as a timestamp correction, not an additional event. Independent review found a blocker in pooling candidate v1: the new Go pipeline label alone could identify mean-pooled vectors from an old accepted helper as compatible with a repaired CLS helper. Candidate v2 now includes the actually verified loaded helper SHA-256, runtime, runtime commit and architecture in embedding identity, rejects unavailable/incompatible records, and retains Run's byte verification. The same model under old/new helper bytes has distinct coordinate identities. The new collision regression failed against v1 and passes with v2; all 12 isolated affected checks pass. Review artifact `independent-embedding-pooling-v1-v2-source-review.json` (`9a074cc54d6cb3e86c871f833e54745e0b62c1b860a1644286d780a0c4294af6`) preserves the finding and closure.

**Integration/evidence:** Applied reviewed patch `4247d3b3b8b190f532deb5944487a98c62feff6cac0babe1d955e46f94688782` to the primary checkout. Native embedding honors GGUF pooling, accepts supported sequence pooling only, and never falls back silently to a token vector. Root native identity, semantic/related-content, Go-comment and personal-review race checks pass **86 tests/subtests, zero failures/skips**, two packages (`pooling-guard-review-integration-race-v1.jsonl`). A private native bundle changes only llama-worker and its declared SHA; all other helper bytes and the frozen app were verified unchanged. Helper SHA-256 is `94e42ce0da77acd98332c7bbf3c6eaa1b5349232cb77d2f0d1b53a7c45a34fcb`; bundle provenance is `embedding-pooling-candidate-v2-native-provenance.json` (`c11e8ee45321032b95c16b38b58eea1fb0504c8349b4ad860e2db956c28853e3`). This is a private helper build against retained pinned libraries, not a full clean runtime rebuild or release package.

**Separate work:** Original 12 personal-review cases are running once against experimental guidance v2 in worker commit `0fb3830`, unchanged original corpus/model/frozen helper; no semantic success is yet claimed. Sixteen sealed independent mode controls now have a bounded test/runner draft. Root found and corrected a proposed Go-token oracle rejection of formatting-induced implicit semicolons before model execution; AST structure and literal spelling remain checked. An identified root agent assessed eight code/prose goldens and 24 semantic poisons against every source/request clause (`independent-mode-controls-preflight-root-review-v1.json`); the other 52 structural items still require actual parser/service gates. These are agent assessments, not human release acceptance. Vision investigation identifies a metadata/preprocessing mismatch in the installed projector, but does not establish why its three descriptions failed; no speculative runtime repair has been applied.

**Next:** Run the 12 related-content cases with the exact new helper and unchanged corpus once the shared model slot is free. Complete the sealed mode gates before inference, assess review-generation outcomes, and build a new development package for adopted changes. Existing app/candidate receipts, campaign failures, representative-input skips and release state remain unchanged.

### DEV-055 — 2026-10-16 01:53 IST: review improvement, unchanged ranking failure and sealed mode admission

**Personal reviews:** Worker source `0fb3830` completed all original 12 review cases using guidance v2, unchanged corpus/oracle/runner/frozen helper/model: **12/12 pass**, versus preserved BEFORE **10/12**. Seven cases perform one real native generation each; five test service boundaries. All 14 retained canonical pairs match. The weekly result now states both requested facts; the decisions result states the full security decision instead of its page title. Root read all seven actual drafts/citations and concurs with the worker's claim-by-claim assessment: no new unsupported claim/action, current source citations, approved effects and duplicate-apply controls preserved. Worker assessment `agent-reviews-guidance-v2-semantic-comparison.json` (`422db95f000a3186a1ad3a1ccd835b300cae5c9c822573674df045d25a3c5e4f`) explicitly discloses candidate authorship; it is not a blind human assessment. Integrated guidance-only v2 `6a6273074fdc046e59ac650c31a448170fa4c09c0d322899d2aae796a03bc32e` for this demonstrated improvement. Four new-domain transfer controls with 16 semantic poisons were sealed and independently reviewed but **not executed**; generalized reliability and human acceptance are unproved.

**Embedding/related content:** Source `6dd55ca`, private helper `94e42ce0...`, unchanged 12-case corpus/pack102 completed **11/12 pass**, the same `budget` ranking failure as BEFORE. All bound inputs and canonical evidence remain unchanged (`model-related-pooling-v2`). Correct model-selected pooling is retained as a runtime correctness fix, not claimed as a ranking repair. Read-only comparison shows relevant body text outranks distractor body text in all 12 direct queries; in `budget`, a generic title-only distractor wins instead. Source investigation also confirms the query page title is included twice and indexed chunks lack explicit aggregation order. A small isolated composition cleanup is being prepared; no title filtering, ranking weights or fixture-specific terms have been adopted. Comparison artifact is `/private/tmp/trove-related-pooling-existing-evidence-comparison-v1.json` (`74c8dd6e2fd31431b671342c4a417fc17e5166e8beb6b9303d23f25cfd5e201c`).

**Mode harness:** Integrated six additive evaluation files and the exact sealed 16-case corpus. V1 root checks had **117 passes, two failures** (the partial-code scope subtest and parent): the harness incorrectly searched for raw multiline selected code, although production intentionally supplies a JSON string. No model had run. V2 checks exact serialized selection/marker/request bytes and separately records native callback success, so a failed process cannot establish a comparable baseline. Earlier formatting-induced semicolon oracle issue was corrected before this run. Full v2 patch SHA-256 is `6837ccb5161026d6c0dcaba1b29df7db394445750de1e9e2309f24ceb32d8c48`; sealed corpus/preflight and original 42 controls are unchanged. Actual Mermaid parser gates pass **20/20**; first sandbox-nesting refusal is preserved separately and executed no parser/model. Root combined mode/golden/poison/scope/personal-review checks now pass **119 tests/subtests, zero failures/skips**; consumer validation passes **15 tests**. Independent source review found no remaining blocker; database/diagram proposal payloads will also receive agent inspection because automated payload comparison is strongest for code proposals.

**Next:** Commit this integration and run one BEFORE generation per sealed mode case with unchanged mode prompts, exact frozen helper/model and OS-denied network. Only then apply the separate mode guidance and run comparable AFTER. Preserve every output, original grades and explicit agent-versus-human assessment boundary. Current frozen app/candidate and release state remain unchanged.

### DEV-056 — 2026-10-16 02:09 IST: preserve independent mode baseline and stage controlled guidance

**BEFORE:** Source `462bbc85a4c2869424ffd29f830fe5c9a1a49bc6` completed all 16 sealed controls with exactly one successful native callback per case, no timeout, no missing cases and every canonical inventory unchanged. Runner `mode-controls-before-v1/manifest.json` is terminal with inputs unchanged, exit 1. Go-level structural checks report 13/16; the actual pinned Mermaid parser additionally rejects the case-distinct diagram, which drops `load` and `load --> LOAD`. Four database outputs retain requested titles/properties and four code outputs preserve code/literals; separate semantic review identifies an unsupported example-purpose assertion in the PathLabel comment. All four expansion outputs add unsupported benefits or objects. The two required-suggestion outputs clearly express optional suggestions in ordinary language but fail the oracle's colon-label regex; retain those grades and apply this same semantic interpretation AFTER. This is bounded agent assessment, not the outstanding human review or a performance qualification. Frozen 42-case outputs/grades and the packaged candidate remain untouched.

**Controlled candidate:** Applied only `review-evidence/mode-guidance-v1.patch` (SHA-256 `2a4ba8c9556fe69fc2a03dbb2f77478283f30ebd7f738a4e4a46e68584db69cf`): full database titles, explicit Mermaid relationships, physical comment newlines and fact-preserving expansion guidance. Production validators, model/helper, sealed corpus, parser, runner and oracles are unchanged. Root focused mode/context/golden/poison/scope checks pass **112 tests/subtests, zero failures/skips** (`mode-guidance-integration-v1.jsonl`). Guidance remains experimental until the paired native/semantic comparison; no quality improvement is claimed yet.

**Independent preparation:** Related-query composition cleanup v3 remains unapplied during the paired mode run. Its deterministic BEFORE has five expected test/subtest failures; worker AFTER has **44 passes, zero failures/skips**, including scope, memory, current revision and query boundaries. Evidence manifest `related-query-composition-v3-evidence/manifest.json` SHA-256 is `ba925ed705bbbe39d352959d1ea0be761aaf4b6fb1f715715632b608e0c33fef`; no native ranking gain is inferred. A separate ordinary-byte build staging copy `repaired-build-native-v1` contains 52 verified files, no links/shared inodes; copy provenance SHA-256 `7b1e32a0efc9e020d5e2d6d8582bd2109996b65764a09bcefa7958399c52752c`. The build script mutates its native input, so subsequent builds must use this staging copy and a fresh app output. No new app has been built.

**Next:** Commit this controlled candidate and run all 16 AFTER cases against the exact BEFORE manifest. Keep source and evaluation inputs fixed until terminal, assess every output/proposal and preserve regressions before deciding which guidance to adopt. Then integrate the independent query cleanup and build the final adopted source separately. Release qualification is unchanged.

### DEV-057 — 2026-10-16 11:39 IST: resume after interruption and reject unsuccessful mode guidance

**Execution reconciliation:** The user confirmed recovered disk space; current read-only checks find HD-B1 mounted and approximately 14 GiB internal free, above the existing 4 GiB harness requirement. The conversation was interrupted while AFTER was running; on resumption its process was already terminal (exit 1, no timeout), source remained clean at `e4f9621fafa20d312b861421bb1060c4f73488ad`, and all 16 native callbacks completed with inputs/canonical inventories unchanged. No silent rerun occurred. Retained manifest: `mode-controls-after-v1/manifest.json`; elapsed execution is not performance qualification.

**Observed comparison:** BEFORE agent assessment accepted 10/16, with six rejected; exact independent review SHA-256 `430fdef347f2be0f891a7757b972300f75d0b5ce8982190f7930d9c581db6247`, separate agent receipt `b4e62c9f8d12506d0586357079061b4f77e086f36b48479b8899d95c55b15250`. AFTER Go task grades improve 13/16 to 14/16 only because one expansion meets the paragraph count; actual Mermaid parser worsens 3/4 to 2/4. A previously valid sequence now contains malformed `Gate -->|>> Sensor`; the already-failing case-distinct graph also changes requested LR to TD. All four expansions still add unsupported facts. Root inspected all 16 AFTER drafts: database schemas preserve all requested titles/properties; code/literals remain unchanged, with comment semantics receiving separate assessment. Pending independent AFTER receipt must not be inferred from these structural grades.

**Disposition:** Rejected the Mermaid and expansion prompt changes, restoring exact prior instructions (`ai_context.go` restored from `462bbc8`). Preserve every experimental output/grade. Retain database-title/comment guidance only as a candidate for the original six affected cases; no aggregate quality improvement or release pass is claimed. The already-proven Go syntax/AST proposal guard is retained independently.

**Verification correction:** DEV-056's 112-test command selected mode/golden/poison/scope checks but its anchored regex did not select the intended context tests. The subsequently explicit context-budget/protocol/draft selector completed **83 tests/subtests, zero failures/skips** before model generation (`mode-context-integration-v2.jsonl`). Keep both actual commands/results; do not reinterpret the first selection.

**Next:** Commit the narrowed candidate, run the original three database and three code-comment controls once, and assess exact outputs. Then integrate the separately reviewed related-query cleanup, verify its twelve native cases with corrected embedding helper, and build the adopted source into a fresh P2 development app. Frozen candidate, required human/live/device checks and release state are unchanged.

### DEV-058 — 2026-10-16 11:42 IST: retain bounded mode improvements and integrate deterministic related queries

**Narrowed mode outcome:** Source `58eb383ff365b2bd4cbe7a0ee930949bf371e091`, unchanged pack106/frozen native helper, original corpus and corrected mode oracles completed the six selected original database/comment cases: **6/6 pass**, no errors/skips, all inputs and canonical evidence unchanged (`model-modes-db-comment-followup-v1`). Full-family pass remains false because this was an explicit subset. Root read all six actual drafts: Harbor/Orchid/Maple retain their full requested `work` suffix; all three Go functions preserve exact expressions and add accurate comments using physical newlines. Database-title/comment guidance is retained for this demonstrated improvement. Original frozen grades remain unchanged, including the separate code-layout oracle limitation; no 42-case campaign pass is inferred.

**Independent transfer comparison:** AFTER review `independent-mode-controls-after-v1-semantic-review.json` SHA-256 `b60bd5b251285540aedd0ccf17d986726215fa3a7da64ca3d469509f68e926e9`; exact agent receipt `4654835d0db261b00c24aeae82c108ec17b9f6e649be6b98a9f2bc62cdbe8913`; comparison `a14a397114960d2e497e2b697eddd34574f887aa7843dd5c2637a9261f4c7f99`. Combined structural checks remain 12/16 and agent semantic acceptance remains 10/16: comments improve 3→4 by removing an unsupported purpose assertion, database stays 4/4, diagrams worsen 3→2, expansions stay 0/4. All 32 exact canonical pairs, stable bindings and one native callback per case were checked. Diagram/expansion guidance has been rejected; retain these evidence limitations and human-review boundaries.

**Related-query implementation:** Integrated reviewed v3 patch SHA-256 `879ce8ef42092b4db26a65724c6bcb913e216999e5a59e3ba708cb3b53ade49b`. The current page title enters the semantic query once; indexed bodies/properties are aggregated by stable chunk ID, excluding only the synthetic title chunk. Real body/property text equal to a title remains present. Primary/fallback paths retain current-revision checks, authorization, memory lifecycle and byte limits. Stable indexed order is not claimed to reproduce editor reading order. Root focused **44 race tests/subtests pass, zero failures/skips**, including reversed derived index insertion, real reindex and boundaries (`related-query-integration-race-v3.jsonl`). No native ranking improvement is claimed before its separate run.

**Next:** Commit and execute all twelve related native cases once with corrected helper `94e42ce0...`; then build the accepted production source and run bounded packaged/native checks. Build recipe v2 fixes every Go cache setting to the existing `/private/tmp/trove-feature-go-cache`, preserving prior recipe bytes and avoiding a duplicate cache. Candidate/release acceptance and representative-data/live/device prerequisites remain separate.

### DEV-059 — 2026-10-16 11:45 IST: related comparison unchanged; prepare fresh repaired package

**Actual native result:** Source `cd5e17d06f8555d4e2b24ffc46cf1bc81df0e91e`, corrected helper `94e42ce0da77acd98332c7bbf3c6eaa1b5349232cb77d2f0d1b53a7c45a34fcb`, unchanged pack102 and twelve-case corpus completed **11/12 pass**, exit 1, inputs and canonical evidence unchanged (`model-related-query-composition-v3`). The same `budget` ranking case fails. Keep the title-once/stable-order repair for deterministic input correctness; it has not solved relevance ranking. No extra model attempts or changed oracle are used to manufacture a pass. Independent outcome comparison is being recorded separately.

**Build boundary:** Adopted production source now includes verified pooling/helper-bound embedding identity, Go comment proposal protection, bounded review disclosure/claim guidance, measured database/comment guidance and stable related queries. Rejected answerability, Mermaid and expansion experiments remain reverted. The latest packaged app is still frozen source `9a39485`; source evidence is not transferred to it. Frontend reuse proof binds all 615 substantive files exactly to the frozen app; only a browser test changed, with no runtime frontend/config/contracts changes. Technical recipe v2 SHA-256 `57f9395bccfae368f8fb8e0c1f0b15191d64e585fdf11f7a0cae00894a090fa6` keeps every Go cache setting on the existing active cache. Build/smoke wrapper is `/private/tmp/trove-run-repaired-build-smoke-v1.py`; it is unexecuted at this entry and records exact argv/log hashes, source and each step outcome.

**Next:** Commit this verification record, build a fresh full P2 `Trove-repaired.app` using the independent mutable helper copy, and freeze its own candidate before bounded observations. Run installed-model instruction/tokenizer checks, OS-offline packaged P0/P1/P2 journeys and one rebuilt image/OCR smoke. These checks do not close known answerability, diagram, expansion, relevance or vision failures, replace missing representative inputs, or qualify release/signing/install/device/live-account acceptance. Preserve both old candidate bytes and all previous failures.

### DEV-060 — 2026-10-16 11:59 IST: build and verify the repaired full P2 development candidate

**Package and source:** Built from clean committed source `6d017cf2338a0f9caaca0c403bb40e5e11a9f403`. Fresh app is `Trove-repaired.app`; candidate `0434852918824066e1fa950917a8a0b7fd13fd18e0f0c19ea80dc23d1f1d82db` froze at 06:18:31 UTC on 16 October before its observations. App binary SHA-256 `d185efa80e32d4637060df215217ef49f7972daedd2077b76b902a50760e8d1c`; full app inventory `c816322f2e67fc30f87cf8b543fea589b23f43c31ae90ae77054803fe8950941`. All seven serial build/freeze/verification steps completed successfully in `repaired-build-smoke-execution-v1.json`; actual runner is retained as `repaired-build-smoke-runner-v1.py` (`bc6b13afe77789e21a294b2b024a9b25371a09c3b2d16ef66b953021fd404909`). No model/test process is left running.

**Actual checks:** Packaged P0 create/reopen/restore and installed chat/embedding/speech, P1 create/reopen and P2 create/reopen all pass under OS-denied network. Native instruction answer and tokenizer/incomplete-output tests pass **2/2**. One rebuilt image/OCR diagram case passes with unchanged inputs/canonical evidence; its full-family flag stays false and prior vision failures/required-image skips remain open. No physical microphone check was run. These are bounded automated service/native checks, not interactive human, performance or clean-install qualification.

**Independent package audit:** Original frozen app (683 files) and original helper bundle remain unchanged. All 615 frontend files match; all 52 substantive packaged native files match the rebuilt stage; correct pooling helper SHA-256 remains `94e42ce0...`. Twelve read-only signature verifications pass with ad-hoc signing; all three Go binaries report clean source `6d017cf`. Initial strict inventory audit retains **49/50** observations: only two deliberately excluded AppleDouble metadata files differ between stage and package. Addendum verifies the exact declared packaging exclusion, without rewriting the initial result. Evidence SHA-256: `repaired-package-independent-audit-v1.json` = `8381b489fd622e5bd612d9b5d3c37df45dfe44eaa2860e376aff564b18b2dd24`; v2 = `1a82a537889716fe24da39965b47f1104531d897d740a88ff2fcf971b413156e`. Existing static runtime libraries were reused; no clean runtime rebuild or distribution signing is claimed.

**Independent preservation:** New isolated fixture's 128 tables, earlier canonical/history rows, six original assets and complete recording bytes were inspected. Initial result remains **50/51** (`830f7db11c05225a1386f6a7cc214afe528e32d318eb7557269519926fd5950b`): an old source-bookkeeping assumption omitted two derived `search_dirty` rows added by the additional native-process startup. Supplemental **8/8** checks (`18b5d73877f9c88a77dbe2a5fb02f88fdedfed5cd78249dbf7925fae5e574b59`) bind exactly the forgotten/stale memory pages lacking chunks to the frozen startup SQL. Every other source table matches the backup except expected backup lifecycle rows; fixture bytes stayed unchanged during both inspections. Attribution uses source, exact row sets and process order, not an unavailable statement trace. Synthetic silent audio preservation does not establish physical capture quality.

**Qualification record:** Root reviewed and rehashed all eight evidence files in `packaged-observation-repaired-v1.draft.json`; the actual observation was then recorded as a passed `p0-packaged-journey` receipt. First CLI invocation supplied a directory instead of `candidate.json` and was refused before recording; its failure log remains preserved. Corrected invocation passed. Read-only status exits 2 as expected for an unqualified candidate: **1 passed / 65 pending / 0 failed receipts**, no identity/evidence errors, `releaseQualified=false`, `releaseApproved=false`. Prior candidate passes are not transferred. Automated/agent observations remain distinct from human acceptance.

**Additional assessment and remaining work:** Independent original six-mode review (`ba1004f807d372398631f0ff5b7f2ff4a0492e0619e969db3f7b55d6b96b5d14`) confirms five output repairs and one already-valid code case preserved. Independent related comparison (`a558c0032a27f84af19d82624a5990a6fb9985e17775f6d5ec8a0743cdd271b5`) verifies 48 snapshot files, identical direct-query scores across all twelve cases and the unchanged ranking failure; exact native query strings were not logged, so reconstructed queries are labeled as such. Continue EVAL-08/10–14 quality repairs and independent review-transfer controls, representative images/imports/audio, foreground/device/live-account workflows, then full candidate-bound performance/signing/install/recovery qualification. Full macOS feature integration remains built; Windows/Linux remain subsequent and mobile deferred. Do not infer overall product completion from test or receipt percentages.

<a id="commit-history"></a>
## Recorded source-development history

Complete reachable primary + candidate-7 lineage at initial consolidation: **133 commits**, through `299e43d`. Dates below are converted from Git committer timestamps to IST. Commit titles describe changes, not independent acceptance results. Additional unmerged worktree branches are not silently counted as integrated development.

<details>
<summary>Expand all 133 recorded commits</summary>

| Commit | Committed (IST) | Development |
| --- | --- | --- |
| `632d0cd` | 2026-10-04 20:14 | chore: preserve audited Trove baseline before completion roadmap |
| `231d0ee` | 2026-10-04 20:39 | Fix scoped meeting history restoration with existing segments |
| `d703404` | 2026-10-04 20:39 | Scope portable exports to live content and isolated source evidence |
| `b97405b` | 2026-10-04 20:42 | Preserve linked view placement, route scoped block links, and enable hybrid Q&A |
| `630107e` | 2026-10-04 20:43 | Make developer setup reproducible with persistent verified caches |
| `82ad139` | 2026-10-04 20:46 | Use online SQLite snapshots and nonblocking recording backups |
| `831a34f` | 2026-10-04 20:52 | Preserve rich cell edits and persisted nested toggle presentation |
| `971c5c3` | 2026-10-04 20:53 | Use generation-qualified sqlite-vec KNN for scoped semantic retrieval |
| `c3c7047` | 2026-10-04 21:01 | Decode common local meeting audio through bounded macOS helper |
| `d521e7a` | 2026-10-04 21:03 | Retain inert database configuration in portable import review |
| `c29cb77` | 2026-10-04 21:07 | Complete table structure and code image display controls |
| `02cdb68` | 2026-10-04 21:09 | Persist database columns and resize calendar date ranges |
| `97714c2` | 2026-10-04 21:15 | Review incompatible historical values before restoring pages |
| `826d0dd` | 2026-10-04 21:23 | Page database queries with scoped cursors and bounded derived memo reuse |
| `755a245` | 2026-10-04 21:25 | Preserve finite rich text colors across editing import and export |
| `8121c57` | 2026-10-04 21:39 | Pin offline Shiki and PDF.js renderer dependencies |
| `4f5c96a` | 2026-10-04 21:44 | Render code and managed PDFs with packaged Shiki and PDF.js assets |
| `311a902` | 2026-10-04 21:46 | Reconstruct nested import ownership and preserve explicit source identities |
| `92c8ff0` | 2026-10-04 21:54 | Load shell content on demand and render bounded database pages |
| `244f1ea` | 2026-10-04 21:59 | Edit rich layout subtrees and render canonical reusable sources |
| `6403b78` | 2026-10-04 22:10 | Budget local prompts with exact tokens and validate cited draft evidence |
| `b274352` | 2026-10-04 22:11 | Verify Shiki rich captions and PDF.js in full editor regressions |
| `b70fe60` | 2026-10-04 22:16 | Use shared fixture URLs and preserve historical browser evidence |
| `e96706b` | 2026-10-04 22:16 | Add scoped timeline scales, grouping, dependencies and guarded ranges |
| `877f75f` | 2026-10-04 22:21 | Prevent provider frames from embedding native or hosted app documents |
| `22cf850` | 2026-10-04 22:21 | Resolve typed action plans to scoped revision-guarded commands |
| `7c3fe22` | 2026-10-04 22:22 | Unify template defaults and nested bodies behind guarded kernel previews |
| `fc1c183` | 2026-10-04 22:31 | Cache explicitly fetched bookmarks and isolate consented provider players |
| `17bbbd8` | 2026-10-04 22:38 | Restore scoped body search in the lightweight workspace shell |
| `c776bee` | 2026-10-04 22:45 | Review named local AI and MCP actions through shared guarded proposals |
| `7837bf3` | 2026-10-04 22:47 | Record completion ownership and development evidence without release qualification |
| `68546c4` | 2026-10-04 22:47 | Add explicit formula v2 contract, guarded editing and scoped previews |
| `8c2367f` | 2026-10-04 22:59 | Print rich offline page PDFs with a bundled isolated macOS renderer |
| `5225a05` | 2026-10-04 23:03 | Adopt official MCP SDK with isolated sessions and exact relay identities |
| `7bbd4a9` | 2026-10-04 23:21 | Avoid Metal initialization for vocabulary and CPU worker operations |
| `fef6ab2` | 2026-10-04 23:21 | feat(import): reconstruct resumable Notion database captures |
| `e46fc8d` | 2026-10-04 23:25 | Expose typed local rules and atomic recurring template copies |
| `8e1e89e` | 2026-10-04 23:41 | Add contained native runtime discovery and scoped Codex app-server transport |
| `148eb11` | 2026-10-04 23:42 | Define bounded managed provider desktop requests |
| `de370c2` | 2026-10-04 23:43 | Deliver paired browser clipper with durable owner-reviewed capture |
| `f363aac` | 2026-10-04 23:49 | feat(auth): implement isolated ChatGPT account authorization |
| `01830f7` | 2026-10-05 00:04 | Connect explicit ChatGPT accounts and models to contained managed Codex runs |
| `5d7cdb0` | 2026-10-05 00:05 | Add opt-in local personal reviews with cited analysis and approved actions |
| `10beeb3` | 2026-10-05 00:14 | Add scoped Claude runtime adapter with native startup capability gating |
| `06d2d92` | 2026-10-05 00:25 | Align explicit Claude billing routes and share the scoped loopback bridge |
| `a5ce7f6` | 2026-10-05 00:29 | Add opt-in remote MCP TLS and native OAuth consent |
| `c6faef8` | 2026-10-05 16:12 | Add private Claude profiles and gated vendor terminal login |
| `b3b44a6` | 2026-10-05 16:31 | Integrate explicit managed accounts and remote consent with synchronous listener revocation |
| `4e3fe92` | 2026-10-05 16:33 | Add bounded local image understanding and retain failed model rubric |
| `3325622` | 2026-10-05 16:33 | Measure 10k typed database queries with independent paging and scope oracles |
| `f5bca2c` | 2026-10-05 16:33 | Preserve incomplete startup measurements and retain native diagnostic evidence |
| `31ac9fe` | 2026-10-05 16:37 | Reduce derived query allocations while rechecking cached-read permissions |
| `5223a8f` | 2026-10-05 16:44 | Record exact development candidate evidence and unresolved release gates |
| `44f7262` | 2026-10-05 16:52 | Exercise P2 canonical data and inert portability through packaged service journeys |
| `4900d4b` | 2026-10-05 16:53 | Constrain cited AI output to source spans and strengthen memory quality checks |
| `d74a719` | 2026-10-05 17:29 | Expose scope-safe semantic related content with current-source navigation |
| `31d5b5c` | 2026-10-05 17:29 | Add protected Windows DPAPI and Linux Secret Service account backends |
| `3731433` | 2026-10-05 17:35 | Add native worker portability foundation for Windows and Linux |
| `9b05e73` | 2026-10-05 17:36 | Cancel credential prompts before publishing account state |
| `a00d1b5` | 2026-10-05 17:39 | Fence workspace upgrades with full snapshots and preserve rollback compatibility |
| `7d7ff75` | 2026-10-05 17:41 | Record offline P0 P1 P2 candidate evidence and qualification limits |
| `b953fc7` | 2026-10-05 17:49 | Allow validated system ICU data for contained Claude startup |
| `f96f185` | 2026-10-05 17:56 | Verify macOS update kits and restore reviewed compatible workspaces |
| `b06c057` | 2026-10-05 18:07 | Add restricted FFmpeg audio decoding across native targets |
| `8a96bdc` | 2026-10-05 18:12 | Use native platform locks and descriptor identities in workspace storage |
| `5140324` | 2026-10-05 18:13 | feat(ai): add a verified higher-capacity local assistant pack |
| `6fdd20d` | 2026-10-05 18:13 | docs(ai): record strict memory and larger-model qualification evidence |
| `7b7391a` | 2026-10-05 18:14 | Abstain on exact source pixels without suppressing faint image content |
| `d8193c3` | 2026-10-05 18:18 | Reconcile update audio model and platform implementation evidence |
| `02eb8c0` | 2026-10-05 18:28 | Add bounded miniaudio capture workers for Windows and Linux |
| `f077123` | 2026-10-05 18:45 | Keep contract and requirements generation reproducible on fresh checkouts |
| `f780919` | 2026-10-05 18:54 | Add pinned portable English OCR with truthful engine provenance |
| `06c89a7` | 2026-10-05 18:56 | Add independent actual-model outcomes for exposed writing and structured modes |
| `baec062` | 2026-10-05 18:56 | Preserve typed AI drafts and specify canonical schema and outline formats |
| `5a79a78` | 2026-10-05 18:57 | Require correction of generated code fences without rewriting source |
| `76e642e` | 2026-10-05 18:57 | Record fixed-oracle local AI mode outcomes and retained code-draft failure |
| `422102f` | 2026-10-05 18:57 | Preserve literal AI code and constrain complete database drafts |
| `5cb3f72` | 2026-10-05 18:59 | Add offline Windows and Linux desktop packaging sources |
| `855568d` | 2026-10-05 19:00 | Record source-only desktop platform verification evidence |
| `2f2f9c2` | 2026-10-05 19:09 | Register synthetic Secret Service exports before starting its reader |
| `c473568` | 2026-10-05 19:13 | Align macOS capture builds with pinned miniaudio and bounded consent |
| `0563b72` | 2026-10-05 19:18 | Separate generated code transport from literal source text |
| `90d5085` | 2026-10-05 19:18 | Separate code evidence metadata from editable source |
| `9232675` | 2026-10-05 19:19 | Measure local generation RSS and recheck cancellation before source publication |
| `6c2ed70` | 2026-10-05 19:28 | Sample native worker memory directly inside offline execution |
| `ba2b569` | 2026-10-05 19:29 | Report absent RSS samples as unavailable rather than zero |
| `7374c10` | 2026-10-05 19:46 | Reconcile selected stack and portable foundation evidence without release claims |
| `13a5750` | 2026-10-05 19:48 | Record all-mode local AI pass with actual sampled memory and retained failures |
| `6094a4d` | 2026-10-05 19:50 | Cover capture rebinding in the desktop build orchestration fixture |
| `dd74fbb` | 2026-10-05 20:01 | Add conservative Windows workspace privacy and file publication boundaries |
| `cb12af4` | 2026-10-05 20:02 | Add contained Windows WebView2 document helper source |
| `8883464` | 2026-10-05 20:05 | Record Windows document helper source verification boundaries |
| `7f6b196` | 2026-10-05 20:19 | Add bounded offline PDF adapters and validated portable document packages |
| `3886587` | 2026-10-05 20:27 | Record frozen macOS candidate checks and unresolved UI and startup failures |
| `e620c9a` | 2026-10-05 20:35 | Reconcile scoped requirement coverage without changing historical qualification |
| `86bc77c` | 2026-10-05 20:40 | Preserve whole words across rich text in search and review evidence |
| `9106ad9` | 2026-10-05 20:48 | Verify formatted-word recovery through the packaged native interface |
| `931b6a5` | 2026-10-05 20:59 | Require authenticated rollback child readiness before handoff |
| `550ec64` | 2026-10-05 21:01 | Record rollback readiness candidate and retained startup tail evidence |
| `f5075f7` | 2026-10-05 21:02 | Fail closed on Linux PDF containment and bound process diagnostics |
| `2fcc5b6` | 2026-10-05 21:06 | Clarify disabled Linux document path and Windows PDF behavior |
| `3f7c623` | 2026-10-05 21:07 | Emit bounded native startup stage traces for qualification |
| `d1fc07d` | 2026-10-05 21:08 | Keep optional startup tracing off the normal snapshot path |
| `e481c6a` | 2026-10-05 21:14 | Add bounded local support bundles with explicit reviewed content consent |
| `de7fe2a` | 2026-10-05 21:15 | Record bounded startup traces and controlled five-launch investigation |
| `94914c6` | 2026-10-05 21:16 | Refuse reused startup benchmark evidence paths |
| `63d450d` | 2026-10-05 21:17 | Record startup harness evidence preservation regression |
| `eb521bb` | 2026-10-05 21:34 | Record scoped support privacy evidence and reconcile current coverage |
| `317b782` | 2026-10-05 21:46 | Record integrated candidate checks and native support export evidence |
| `94cdb29` | 2026-10-07 13:08 | Make performance harness fail incomplete or slow measurements and preserve evidence |
| `c7f0857` | 2026-10-07 13:17 | Add candidate-bound completion qualification records and gates |
| `26f71bb` | 2026-10-07 13:18 | Add independent expanded local AI corpora and offline evaluation runner |
| `137522b` | 2026-10-07 13:39 | Add explicit Developer ID signing and resumable notarization |
| `226fb85` | 2026-10-07 13:40 | Preserve incomplete AI evaluation and clarify formatting preferences |
| `8a95b53` | 2026-10-07 13:59 | Refine release acceptance scopes and map proven service cases |
| `c3428d7` | 2026-10-07 14:05 | Add fail-closed Linux process-domain qualification foundation |
| `6449c9f` | 2026-10-07 14:08 | Add private bounded diagnostics with reviewed support export |
| `65a14ed` | 2026-10-07 14:09 | Audit diagnostic history handles and protect Windows root locks |
| `fee69fb` | 2026-10-07 15:12 | Reconcile remaining-plan execution and current qualification gaps |
| `9294718` | 2026-10-07 15:13 | Add pinned platform publisher authentication and portable inventories |
| `74a6e35` | 2026-10-07 15:21 | Add positive recovery publication and startup admission foundation |
| `28da3df` | 2026-10-07 15:35 | Make cancelled auth callback regression tolerate intentional connection closure |
| `0bff6ed` | 2026-10-07 15:43 | Record integrated validation and complete P0 model failures |
| `d9471a8` | 2026-10-08 00:42 | Complete P0 AI repairs, macOS acceptance mappings, and native measurements |
| `e1d45e9` | 2026-10-08 16:10 | Record integrated macOS P0 candidate results and remaining gates |
| `b67e814` | 2026-10-08 17:30 | Repair cited AI answers and bounded database navigation with retained qualification evidence |
| `03e6c38` | 2026-10-08 19:35 | Clarify cited response context and correct retained view accounting |
| `d9e045c` | 2026-10-08 22:24 | Preserve source scope and facts in local writing prompts |
| `f6aa26a` | 2026-10-09 00:07 | Record integrated macOS P0 follow-up candidate and remaining gates |
| `b0ab2a4` | 2026-10-09 12:01 | Bind AI evaluation controls and native checkpoint diagnostics |
| `8c408b0` | 2026-10-09 13:18 | Record b0ab2a4 macOS checkpoint and unresolved gates |
| `12c56b8` | 2026-10-09 20:39 | Preserve authored AI prose and complete qualification helper copies |
| `299e43d` | 2026-10-12 16:52 | Record actual AI generation controls and lock display-context holdouts |
| `679bdc4` | 2026-10-15 17:13 | Record accepted functionality-first macOS scope and progress |
| `ee14f0c` | 2026-10-15 17:14 | Integrate reviewed candidate-7 controls and fixtures into feature branch |
| `0ed5350` | 2026-10-15 17:51 | Add semantic review identity and guarded grouped undo foundations |
| `2b421a4` | 2026-10-15 17:53 | Complete canonical task editing and durable extraction identities |
| `950cea5` | 2026-10-15 17:58 | Integrate encrypted native backup/restore |
| `6e713bc` | 2026-10-15 17:58 | Clarify encrypted payload and restore transformations |
| `5e2fab1` | 2026-10-15 18:01 | Record integration and complete freshness dependencies |
| `2279bf0` | 2026-10-15 18:02 | Integrate retained three-way page merge |
| `a3f9e54` | 2026-10-15 18:02 | Preserve import identity and local presentation |
| `3e8fc20` | 2026-10-15 18:25 | Add versioned manual skills with scoped execution |
| `97201e7` | 2026-10-15 18:25 | Integrate acknowledged meeting capture controls |
| `45ef173` | 2026-10-15 18:33 | Integrate typed collection re-import |
| `5cb0951` | 2026-10-15 18:33 | Preflight import graphs and reconstruct evidenced baselines |
| `33906a3` | 2026-10-15 18:44 | Checkpoint persisted reviewed autofill |
| `92955e5` | 2026-10-15 18:45 | Integrate layouts, scheduling and scalar undo repair |
| `1f7a26c` | 2026-10-15 18:45 | Bind explicit CSV source keys |
| `cc1b0ca` | 2026-10-15 18:48 | Integrate transcript correction and retention |

</details>

<a id="evidence"></a>
## Evidence and retained source records

The facts needed to understand current progress are above. These links provide audit detail, not separate current trackers. Candidate artifacts are outside the project on this machine; a move to another computer requires preserving/copying their actual evidence, not assuming these paths will resolve.

- [Latest repaired full P2 development app](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/Trove-repaired.app)
- [Repaired candidate and frozen inputs](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/qualification-repaired-v1/candidate.json)
- [Actual build and bounded verification outcomes](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/repaired-build-smoke-execution-v1.json)
- [Repaired candidate status — 1 passed / 65 pending](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/13/01a1363a-7a42-7700-930e-3083f64ff417/macos-feature-integration-20261015/repaired-candidate-status-v1.json)
- [Candidate 7 checkpoint — recorded 12 October](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/checkpoint-report-candidate-7-interim-v1.md)
- [Candidate 7 frozen record](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/qualification-candidate-7/candidate.json)
- [Candidate 7 qualification status — 3 passed / 52 pending](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/source-validation/candidate-7-status-20261012-v1/output.log)
- [Candidate 7 source validation summary](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/source-validation-summary-v1.json)
- [Candidate 7 serial AI/performance handoff](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/candidate-7-serial-execution-handoff-v1.json)
- [Candidate 7 table/support handoff](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/native-table-support-candidate-7-execution-handoff-v1.json)
- [Candidate 7 editor/reopen handoff](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/native-editor-reopen-candidate-7-execution-handoff-v1.json)
- [Candidate 7 final recorded table preflight refusal](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-next-20261011-1810/native-table-candidate-7-preflight-refusal-v3.json)
- [Candidate 6 historical results](/Users/suryasriramduvvuri/.codex/visualizations/2026/10/07/01a11771-2f4b-7551-8ea0-b738e67080db/trove-p0-repair-20261009-1104/checkpoint-report-draft-candidate-6-v8.md)
- [October 12 Notion comparison — 39 features / 12 recommendations](/Users/suryasriramduvvuri/Documents/Codex/2026-10-12/https-www-notion-com-product-features-2/outputs/Trove_Notion_Feature_Gap_Review.html)
- [Original 51-item machine registry — legacy scoped state](docs/completion/registry.json)
- [Original acceptance backlog — historical source wording](docs/remaining-work.md)
- [Machine requirements coverage overlay](docs/completion/requirements-overlay.json)
- [Qualification procedure and evidence rules](docs/completion/qualification.md)
- [Accepted D001/D002/D003 contracts](docs/phase-0/decisions.md)
