# Trove — project progress

**The single current development and progress tracker for this project.** Read and update this file for every meaningful implementation change, investigation, test result, decision, blocker and handoff. This is not an automatically running monitor.

Last updated: **2026-10-15 21:28 IST**. User-facing dates use **Asia/Kolkata (IST, UTC+05:30)**; source artifacts may retain UTC timestamps.

This file consolidates the original 51-item backlog, the newly selected desktop scope, current qualification, next actions and development history. Earlier status pages are historical snapshots. Machine-readable contracts, the legacy registry and immutable qualification records remain supporting inputs/evidence; they are not additional human progress dashboards. Their frozen bytes must not be rewritten to make a status look current.

[Current snapshot](#current-snapshot) · [Next actions](#next-actions) · [Decisions](#decisions) · [Milestones](#milestones) · [Original 51 items](#original-backlog) · [New desktop work](#new-desktop-work) · [Detailed workstreams](#detailed-workstreams) · [Blockers](#blockers) · [Update rules](#update-rules) · [Development log](#development-log) · [Commit history](#commit-history) · [Evidence](#evidence)

<a id="current-snapshot"></a>
## Current snapshot

**Trove has substantial implementation, but no platform/tier is release-qualified or approved.** The functionality-first macOS implementation programme is now active. Existing candidate results remain historical and are not passes for the changed source.

| Measure | Recorded result | Meaning |
| --- | --- | --- |
| Original backlog | 51 items: 34 source-implemented, 11 in progress, five open, one awaiting source | These are the legacy registry's scoped implementation labels, reconciled here on 13 October. They are not 34 release-complete features. |
| Latest development package | Candidate 7, source `299e43d`, macOS arm64 | Frozen 12 October 2026 at 17:12:01 IST; ad-hoc signature. |
| Candidate 7 gates | **3 passed / 52 pending / 0 failed**, out of 55 | Most required work is unexecuted. Zero failed receipts does not mean historical defects were fixed. |
| Candidate 7 macOS P0 criteria | **395 mapped / 2 proved** | Mapping defines the test; it does not supply a pass. This is not a percentage of working product features. |
| Source checks for candidate 7 | 654 service race passes, 54 skips; 59 desktop race passes, one skip; 56 frontend unit passes; TypeScript and frontend build; 238 tooling passes; four affected browser passes | Recorded source checks, not rerun by this tracking update. Required skips remain unresolved. |
| Candidate 7 completed gates | Service regressions, build provenance, packaged P0 journey | Actual create/edit/save/reopen, backup/restore and OS-denied-network journeys; installed chat/embedding/speech helper smoke; six independent content/asset preservation assertions. |
| Candidate 7 still unexecuted | AI: 101 cases and 51 manual assessments; fresh startup/search/database; required foreground workflows | Exact handoffs exist. No receipt can be inferred from a prepared procedure. |
| Historical candidate 6 failures | 78/89 combined AI passes; 11 failures. One startup took 6,252.767 ms against 2,500 ms | Keep as historical failures. Candidate 7 has no adopted production prompt/context repair. |
| Release state | `releaseQualified=false`, `releaseApproved=false` | Signing/notarization, native/device, representative-data, clean-install and other gates remain open. |

Candidate 7's focused 22-case prompt experiment was rejected and production prompts were restored. A later startup diagnostic did not reproduce the earlier outlier and did not qualify performance. The package omits two AppleDouble metadata sidecars from the helper source tree; common helper bytes match and the inventory difference is disclosed in its build evidence.

**Item-count percentages requested on 15 October (DEV-009):** Original backlog source labels are 34/51 implemented (**66.7%**) and 17/51 unfinished (**33.3%**): 11 in progress (21.6%), five open (9.8%) and one awaiting source (2.0%). Candidate-7 gate execution is separately 3/55 passed (**5.5%**) and 52/55 pending (**94.5%**). None of the 11 active G groups has completed acceptance; four have existing contract/specification/foundation progress and seven are planned. G11 remains deferred. These are proportions of named item sets, not effort, time remaining or overall product readiness; overlapping legacy/G/CONN rows cannot be added into one completion denominator. All original final candidate/platform acceptances remain pending. Percentages are rounded to one decimal.

### Source and checkout identity

| Location | Branch / source | Current interpretation |
| --- | --- | --- |
| Primary project: `/Volumes/HD-B1/HD-Projects/Trove` | `codex/macos-feature-completion`, current integrated source `daf4877` | Baseline `ee14f0c`; shared foundations `0ed5350`, G02 `2b421a4`, G03 `950cea5`/`6e713bc` and G01 page merge `2279bf0`/`a3f9e54` integrated. See the development log for subsequent commits and current verification. |
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
| 3 | G01 / G02 / G08–10: shared foundations | In progress | Implement real three-way merge, canonical task editing/extraction identity, grouped undo and semantic freshness identity | Meaningful conflict/retry/preservation tests and versioned contracts |
| 4 | G03 / G05 / G06 | G03/G05/G06 integrated; native acceptance pending | Encrypted portable backup, recording controls/correction/retention and reminder-only helper | Usable native workflow plus failure/recovery evidence |
| 5 | G07–10 / AI-* / EVAL-* | G07–10 integrated; evaluation repairs in progress | Verify integrated skills/autofill/freshness/layouts/rescheduling; complete remaining evaluation runners | Focused current-source checks; full historical candidate-7 baseline is not a prerequisite |
| 6 | CONN-00–06 / EXT-* / P2-01–09 | Six initial adapters and Connections UI integrated; gaps active | Complete Notion capture depth, direct connected-source AI context, remaining evaluation runners and confirmed managed-provider/P2 gaps | Production integration; real-account/device prerequisites tracked separately |
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
| M4 — Live integrations | Existing managed providers/inbound MCP and CONN-00–06 | Initial adapters/interface integrated; remaining gaps and live acceptance pending | Authorized live flows, revocation, resume and reviewed effects verified |
| M5 — P2 packages | P2-01–09 and expanded model/image/review work | Source foundations; qualification pending | Each package passes its declared persistence, authority and recovery scope |
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
| G01 | Re-import consistency — page/typed/CSV/attachments/conversations integrated; schema deletion/conversion gaps active | A,D / D002, FIX-001/002 | Audit ZIP/API paths against base/local/incoming rules, visible conflicts, complete-source deletion and idempotent stale-review rejection; preserve raw bytes and local edits. |
| G02 | One task authority — implementation integrated; acceptance pending | A,B,C / D001 | Verify page todo, Home/Tasks and database projections agree for title/status/due, promotion, duplicate extraction, deletion, restart and restore. |
| G03 | OS protection + encrypted portable backups — implementation integrated; acceptance pending | D,F / CORE-010, REL-005/006 | Document copied-backup/lost-device boundaries; add passphrase-encrypted native archive, complete authenticated staging, verified preview and fresh-destination restore. Wrong key/tampering/truncation/crash must not activate a destination. Native format compatibility stays. |
| G04 | Concrete quality and support contract — partly specified | A,E,F / EVAL-*, REL-007 | Retain existing thresholds, name actual hardware/OS/model/language/workloads, lock new independent rubrics before measurement, report task/citation/speech quality and correction effort. New accuracy percentages and RAM targets remain proposals. |
| G05 | Meeting controls and retention — implementation integrated; native acceptance pending | B,D,E,F / CORE-005, REL-003/004 | Implement/verify visible input and start/pause/resume/stop, consent/permission/device/low-disk handling, recoverable gaps, correction history/source anchors, stale derived outputs, and distinct audio/transcript/index/backup retention. No hidden pruning. |
| G06 | Personal background reminders — integrated; native acceptance pending | A,B,F / task authority, native notifications | Separate reminder time from due date; opt-in agent, acknowledged schedule publication, snooze/dismiss/inbox and sleep/restart/DST/restore handling. Agent never opens canonical workspace SQLite; Complete task uses desktop owner. Track uncertain OS delivery honestly. |
| G07 | Reusable AI skills — implementation integrated; acceptance pending | A,B,E / AI-001, permission/review contracts | Version instructions/inputs/output/scopes/tools; manual invocation, selected sources, cancellation and provider handoff. Imported skills are inert; no new permission from their text. Proposed writes use existing guarded approval. |
| G08 | AI database autofill — implementation integrated; acceptance pending | A,B,C,E / AI-001, G07 | Selected rows/properties, typed values, blank-first behavior, overwrite preview, provenance, source/schema/model revisions, cancellation/resume and grouped undo. Stale outputs cannot overwrite manual edits. |
| G09 | Page review/freshness lifecycle — implementation integrated; acceptance pending | A,B,E / history, search/AI evidence | Store human-reviewed revision/date/expiry; invalidate after content change/expiry; expose queue and citation/search status. Separate from generative personal review and do not label a badge machine-proven truth. |
| G10 | Record layouts + previewed dependency rescheduling — implementation integrated; acceptance pending | A,B,C / PARITY-003/004, guarded mutations | G10-LAYOUT: stable property-ID layouts, sections/header/sidebar, overrides/reset without data deletion. G10-SCHEDULE: explicit full affected-date preview, duration preservation, cycle/constraint checks, calendar/timezone rules, atomic apply and grouped undo. Existing dependency links remain compatible. |
| G11 | Phone capture and continuity — deferred by user | B,D,F / desktop completion | Record desktop-first positioning and revisit a companion only after desktop work. No mobile client or two-way mobile sync is being implemented in this plan. |
| G12 | Broad connected-app platform — initial adapters/UI integrated; gaps active | A,D,E / CONN-00–06 | Six named adapters, selected-source refresh and explicitly reviewed external writes; real authorization, provenance, stale/offline/denied/deleted distinctions and revocation. No automatic two-way sync. |

Implementation defaults to review before coding: age passphrase streaming around a verified native backup, no custom cipher; no remembered passphrase by default; reminder agent owns only derived scheduling/delivery state with registration epochs; manual skills/autofill; finish-to-start, preserved-duration, explicit-calendar rescheduling; no ambient connector context sent to external models. These choices are a plan, not implementation evidence.

<a id="detailed-workstreams"></a>
## Detailed workstreams

### Evaluation implementation, execution and review

| ID | Family | Implementation / input state | Execution / acceptance remaining |
| --- | --- | --- | --- |
| EVAL-01 | Candidate-7 AI baseline: 101 cases / 51 manual reviews | Frozen runner/inputs available | All seven families and manual reviews unexecuted; preserve one-generation rule, actual request controls and immutable outcomes. Under D-P11 this historical campaign does not block development; changed products require their own evaluation. |
| EVAL-02 | 42 held-out mode cases, 14 modes | Runner exists | Full run and required semantic review; independent fact/format/schema/diagram/code oracles; no automatic application of proposals. |
| EVAL-03 | 12 context cases | Executable runner integrated; three focused real-model cases passed | Implement real-token framing/reservation, beginning/middle/end relevance, bounded omissions, complete-code protection and changed-source invalidation. |
| EVAL-04 | 12 memory-lifecycle cases | Executable runner integrated; four focused real-model cases passed | Implement source edit/delete/forget/revoke/concurrent-change and old-backup restoration scenarios with live retrieval guards. |
| EVAL-05 | 12 related-content cases | Executable runner integrated; two focused real-embedding cases passed | Label independent semantic relevance; exclude nearer inaccessible/current sources; verify freshness and lexical/semantic disclosure. |
| EVAL-06 | 12 personal-review cases | Executable runner integrated; three of four selected native cases passed, one exact-output failure retained | Exercise actual occurrence/approval/idempotency/retry/cancel/restart/freshness and inactive portable import, not prose quality alone. |
| EVAL-07 | 24 image cases | Runner/independent consumer under verification; 12 synthetic/boundary inputs locked, six photos + six screenshots unavailable | Obtain authorized sources and lock facts/questions/hashes; execute original-pixel/OCR/interpretation/abstention and native resource checks. |

For every family, retain source/helper/model/fixture identities, required skipped results, raw failures and the independent before/after evidence required by its declared scope. Preserve each frozen candidate-7 fixture exactly, including inherited empty/context-only cases; those cases do not prove populated asset/audio preservation. Add separately declared populated preservation cases for future qualification without retrofitting candidate 7. Candidate-7 budgets and oracles remain frozen; later product/UI/model changes require their own declared evaluation. Fixture counts are not claims of representative usefulness.

### Additional P1 acceptance

These rows make existing REL-002/REL-008 and editor/navigation acceptance explicit; they do not add to the original 51-item count. The [requirements overlay](docs/completion/requirements-overlay.json) records their source coverage and remaining native gates.

| ID | Deliverable / source state | Owner / parent scope | Remaining work and completion evidence |
| --- | --- | --- | --- |
| P1-01 | Selected local folder/repository indexing — source implemented; native acceptance pending | A,D,E,F / REL-002, REL-008 | Verify explicit root selection with the native picker, permission boundaries, exclusions, symlinks and file identity; exercise changes, deletion, revoked access, restart and recovery. Search must respect selected scope and current source access. Retain independent filesystem/content evidence for the [external index implementation](internal/workspace/index_external.go). |
| P1-02 | Native accessibility, navigation and input — source foundations; native acceptance pending | B,F / REL-002, REL-008 and editor/navigation packages | Exercise keyboard navigation/shortcuts, VoiceOver, RTL/mixed text, IME and clipboard in the actual focused packaged app; verify deep-link launch routing, restart, undo/history and unchanged unrelated content. Browser fixture passes cover only their declared scope. |

### Connector delivery

All CONN rows are **planned, not implemented or live-qualified**. Existing Notion capture and inbound MCP are foundations only.

| ID | Adapter / owner | Initial read and refresh | Explicitly reviewed writes / constraints |
| --- | --- | --- | --- |
| CONN-00 | Shared framework / A,D,E | Native credentials, account/grant/selection, external snapshot/provenance, cursors, bounded refresh while app runs, freshness/offline status | Durable proposal/outbox/receipt; permission/version recheck; timeout-after-send is unknown outcome requiring reconciliation, not blind retry. Restore/import credentials and activation stay inert. |
| CONN-01 | Google Calendar / A,D | Selected calendars, recurrence/exceptions, timezones/cancellations | Create/update/cancel; preview attendees and notification effects; native browser OAuth/PKCE and incremental token recovery. |
| CONN-02 | Google Drive / A,D | Individually selected files via scoped picker; supported download/export | Upload exports/new files, explicitly replace supported selected files; no promise of unrestricted Drive or structural Docs/Sheets editing. |
| CONN-03 | Gmail / A,D,E | Selected labels/query/thread sets and explicitly acquired attachments | Local compose, reviewed send/reply; public restricted-scope verification; Gmail-derived external-AI context disabled by default until separately qualified. |
| CONN-04 | GitHub / A,D | Selected installed repositories: issues/PRs/comments/documentation | Issue/comment updates and PR creation from existing branches; no code push, merge, workflow dispatch or admin. Native device flow and account/installation limits. |
| CONN-05 | Notion / A,D,C | Selected shared roots and existing faithful import; MCP connected context | Reviewed child pages, blocks and properties. Plan: direct import via user-owned selected-page token and public-client MCP; no confidential OAuth secret in desktop binary. |
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
