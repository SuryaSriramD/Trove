# Trove remaining work

Audit baseline: 1 October 2026. Requirements: Trove PRD and TRD version 3.1. Audited application: Phase 6 development preview 0.6.0, built at build/phase6-preview-1/Trove.app.

This backlog records the remaining work identified by the code, requirements, and release-evidence audit. Trove has a substantial working foundation, but P0 and P1 are not yet fully satisfied. Completing development phases and passing the current tests do not establish complete product acceptance. The immediate objective is reliable everyday use, with content preservation and recovery verified before further feature expansion.

Implementation is underway in the [C0–C8 completion record](completion/README.md) and [51-item registry](completion/registry.json). This file does not mark any fixes as release-qualified. Existing implementations should be retained and extended. Items described as qualification gaps require evidence; they do not imply that the underlying feature is absent or necessarily broken.

## How to use this backlog

- **P0** means the first usable local replacement. **P1** means the complete personal-use target. **P2** means optional enhancements. These are product release levels, not defect severity.
- **Defect** means observed incorrect behavior, with either a reproduced failure or a concrete implementation path identified by the audit.
- **Requirement gap** means part of an accepted PRD/TRD behavior is missing.
- **Qualification gap** means implementation exists but the required evidence is incomplete.
- **Scope decision** means the supported boundary or a technical deviation must be resolved explicitly. It is not authorization to quietly reduce the PRD/TRD.
- **Optional extension** means additional scope that must not become an accidental P0/P1 release blocker.
- An unchecked item remains open. Mark it complete only after implementation, relevant tests, and evidence tied to the actual build are available.
- Dependencies identify the main ordering constraints. Independent preparation, fixtures, and interface work can proceed earlier.

### Ownership

| Owner | Responsibility |
| --- | --- |
| A | Kernel, contracts, permissions, jobs, automation, integration |
| B | Editor, navigation, desktop interface |
| C | Databases, properties, views, formulas |
| D | Migration, files, export, history, recovery |
| E | Search, AI, memory, intelligence |
| F | Native workers, audio, models, packaging, platform qualification |

Each owner is responsible for relevant tests. One coordinating owner must approve shared schema/service changes and integrate work across branches or worktrees.

## Current evidence and its limits

The audit independently reran these checks against the current source and packaged preview:

| Check | Result |
| --- | --- |
| Python contract and ledger tests | 59 passed |
| Go tests and subtests under the race detector | 337 passed; one opt-in native OCR test skipped |
| Frontend type checking | Passed |
| Frontend unit tests | 21 passed |
| Application browser tests | 34 passed |
| Database browser tests | 6 passed |
| Packaged offline P0 create, restart, and restoration journey | Passed |
| Packaged offline P1 data and recovery journey | Passed |
| Packaged headless-host authentication, restart, and ownership transfer | Seven checks passed |
| Current source and recorded Phase 6 artifact hashes | Matched |

The ordinary Go run excluded the opt-in real-model tests. Real-model quality, physical microphone operation, and separate clean-machine installation were not freshly qualified by this audit. Browser tests largely use injected service responses; real SQLite, packaged-process, and host checks are separate evidence. Historical native inference/OCR results remain valid only for their stated builds and fixtures.

Two additional targeted reproductions failed: ordinary meeting-history restoration and exclusion of trashed-only attachments from portable exports. Those failures are not covered by the passing suite above.

Primary evidence: [P1 qualification](phase-5/qualification.md), [P1 release assessment](../evidence/phase5/release-assessment.json), [P2 qualification](phase-6/qualification.md), and [Phase 6 verification](../evidence/phase6/latest-run.json). The Phase 6 report still records releaseApproved as false.

## Delivery order

| Order | Outcome | Main items |
| --- | --- | --- |
| 1 | Prevent unintended disclosure, failed recovery, and content loss | FIX-001 through FIX-003 |
| 2 | Finish dependable P0 authoring, retrieval, audio, and data handling | Remaining FIX items and CORE items |
| 3 | Complete committed P1 workflows and integrations | PARITY, AI, and EXT items |
| 4 | Qualify one final build and reconcile release claims | REL and GOV items |
| 5 | Expand optional capabilities only after an explicit scope decision | OPT items |

Release engineering and governance work can begin immediately and run alongside product fixes. P0 qualification must not wait for optional P1 providers. P2 work must not be used to close unrelated P0/P1 gates.

## Confirmed correctness defects

### FIX-001 Exclude trashed-only attachments from portable exports

- [ ] **P0; critical; owner D; no dependency.**
- **Observed:** A page is trashed and excluded from export, but its attachment remains in the ZIP. Portable export claims to exclude trash while selecting all file records. HTML export inherits this behavior.
- **Work:** Select assets from the exported live content graph, including necessary covers, media, property files, and deliberately retained provenance. Define and disclose any additional evidence included. Keep full native backup semantics separate.
- **Acceptance:** A trashed-only or otherwise unreferenced private asset is absent from current-content exports; a shared asset still referenced by a live page remains present; Markdown/CSV and HTML exports behave consistently. Verify archive bytes, not just manifest counts.
- **Evidence:** [export.go](../internal/workspace/export.go), [export_html.go](../internal/workspace/export_html.go). PRD sections 12 and 16.6.

### FIX-002 Restore meeting history when transcript segments still exist

- [ ] **P0; critical; owners A and D; no dependency.**
- **Observed:** Restore an earlier meeting-page revision after renaming it while transcript segments still exist. The operation fails with an unknown transcript_segment entity error.
- **Work:** Add correctly scoped authorization and revision handling for transcript segments throughout the restore path. Retain atomicity and the selected-page recovery boundary.
- **Acceptance:** Ordinary meeting restore and recovery after transcript deletion both pass. Test existing, edited, deleted, stale, and unauthorized segments; preserve timing and audio references; do not rewind unrelated records.
- **Evidence:** [permissions.go](../internal/workspace/permissions.go), [recovery_metadata.go](../internal/workspace/recovery_metadata.go), [history.go](../internal/workspace/history.go), [restore_test.go](../internal/workspace/restore_test.go). PRD R06; TRD section 16.1 and V06.

### FIX-003 Preserve rich formatting and links during table and layout edits

- [ ] **P0 preservation and P1 editing; critical; owner B; no dependency.**
- **Observed:** Editing a table cell or text inside columns/tabs replaces its rich text with a plain-text value. Unrelated marks and links in the same cell/paragraph are lost.
- **Work:** Use structure-preserving editing transactions or an appropriate rich editor. Preserve unaffected marks, links, child identities, and recovery evidence.
- **Acceptance:** Import a cell and layout paragraph containing bold text and links, change one character through the visible control, save, reopen, undo/redo, export, and restore. Unaffected formatting and targets remain intact. Test the actual change handlers, not only serialization helpers.
- **Evidence:** [AdvancedBlocks.tsx](../frontend/src/editor/AdvancedBlocks.tsx). PRD section 16.2.1; TRD section 25.1 and V04.

### FIX-004 Open the correct linked database view on its owning page

- [ ] **P1; high; owners B and C; no dependency.**
- **Observed:** Page-owned views are reduced to collection IDs; the component then selects the collection's first/default view. Multiple views of one collection can collapse into one component.
- **Work:** Pass and retain the owning view identity separately from collection identity.
- **Acceptance:** Two pages can display different filtered views of one collection; each opens with its own configuration. Multiple views on the same page remain distinct while sharing the same records through rename, move, restart, and restore.
- **Evidence:** [App.tsx](../frontend/src/App.tsx), [DatabaseView.tsx](../frontend/src/database/DatabaseView.tsx). PRD section 16.3.1; TRD V01.

### FIX-005 Make copied block links navigable

- [ ] **P1; high; owners B and F; no dependency.**
- **Observed:** Copy block link produces a page-and-block URL, but the editor handler recognizes only page-only URLs. Native shell routing was not found in the audited paths.
- **Work:** Support page/block links in the editor and qualify desktop URL routing with scoped workspace resolution.
- **Acceptance:** A copied link pasted into another page opens and focuses the correct block. External launch routing is tested in the packaged app. Missing/deleted targets and unavailable workspaces produce useful errors. Existing search-citation navigation remains functional.
- **Evidence:** [nodes.tsx](../frontend/src/editor/nodes.tsx), [Editor.tsx](../frontend/src/editor/Editor.tsx). PRD section 16.1; TRD sections 23 and 32.

### FIX-006 Correct nested toggle visibility and collapse persistence

- [ ] **P0; high; owner B; no dependency.**
- **Observed:** Collapse hides direct children but can leave grandchildren visible. The audited collapse state is local component state.
- **Work:** Apply visibility to the complete descendant subtree and implement the documented persisted presentation state without altering content ownership.
- **Acceptance:** A toggle containing nested lists, paragraphs, and technical blocks hides all descendants; reopening restores them intact. Save/reopen, moving children, and undo/redo preserve the agreed collapse behavior.
- **Evidence:** [nodes.tsx](../frontend/src/editor/nodes.tsx). PRD section 16.1; TRD block registry.

## Remaining P0 functionality and technical foundations

### CORE-001 Complete simple table structure controls

- [ ] **P0; owner B; depends on FIX-003.**
- Add/remove rows and columns, edit headers, and honor stored widths and header semantics. The current insertion creates a fixed 3-by-2 table, with text/merge controls but no complete structure editor.
- **Acceptance:** Users can grow and shrink tables without dropping populated cells silently; header rendering, undo, import, export, and restart preserve structure. P1 merge/unmerge continues to preserve original cell content.
- **Evidence:** [Editor.tsx](../frontend/src/editor/Editor.tsx), [AdvancedBlocks.tsx](../frontend/src/editor/AdvancedBlocks.tsx). PRD section 16.1.

### CORE-002 Add rich-text colors and highlights

- [ ] **P0; owners A and B; shared contract change required.**
- Extend the canonical rich-text schema, generated types, editor, safe import/paste adapters, rendering, and export for the promised color/highlight behavior. The audited mark schema cannot represent them natively.
- **Acceptance:** Supported colors/highlights survive editing, mixed paste, undo, restart, and portability. Older content remains compatible; unsupported incoming values are retained safely.
- **Evidence:** [domain.schema.json](../contracts/domain.schema.json), [Editor.tsx](../frontend/src/editor/Editor.tsx). PRD section 16.1; TRD registry completion criterion.

### CORE-003 Finish code and image block controls

- [ ] **P0; owner B; no dependency beyond existing file services.**
- Code blocks need the promised copy, wrap, filename, and optional line-number controls, with stored settings respected. Images need resize/alignment, zoom/open-original behavior, and rendering of stored dimensions alongside existing caption/alt text.
- **Acceptance:** Controls work in the packaged app, source bytes remain unchanged, settings survive restart/round trips, and file access remains scoped to managed IDs.
- **Evidence:** [nodes.tsx](../frontend/src/editor/nodes.tsx). PRD sections 16.1 and 16.7.

### CORE-004 Connect workspace Q&A to hybrid retrieval

- [ ] **P0; owner E; no dependency.**
- Ask Workspace currently omits the search mode, which defaults to keyword. Wire the existing hybrid retrieval into AI context preparation when compatible embeddings are available. Preserve an explicit, truthful model-unavailable fallback.
- **Acceptance:** Paraphrased/conceptual questions retrieve relevant differently worded notes. Scope restrictions, current-source checks, deleted evidence, memory invalidation, and citation navigation remain correct. Include a real local-model/desktop acceptance case.
- **Evidence:** [ai.go](../internal/workspace/ai.go), [search.go](../internal/workspace/search.go). PRD sections 8 and 17; TRD sections 10 and 27.

### CORE-005 Decode and normalize common uploaded audio

- [ ] **P0; owners F and E; decoder/build decision required.**
- Add bounded, local decoding/resampling for the declared supported audio formats. Current recognition accepts only 16 kHz mono PCM WAV or raw float32. Complete the restricted decoder contract, including codec notices and distribution review.
- **Acceptance:** Supported MP3/M4A/AAC and ordinary stereo/44.1/48 kHz WAV inputs are normalized and transcribed locally. Keep originals; bound resource use; deny arbitrary network inputs; support cancellation/retry and recoverable unsupported/corrupt inputs. Publish the exact supported codec matrix.
- **Evidence:** [audio.go](../internal/workers/audio.go), [app_recording.go](../app_recording.go). PRD section 9; TRD section 30.3.

### CORE-006 Provide schema-aware historical value restoration

- [ ] **P0 recovery boundary; owners A, C, and D; depends on FIX-002 for related recovery regressions.**
- Existing recovery safely rejects changed/missing property schemas but lacks the required usable preview and explicit mapping path for retained historical values.
- **Acceptance:** A schema change exposes incompatible old values in a preview; the user can map or deliberately retain them as evidence. Applying restoration creates a new revision, preserves the current recovery point, and does not rewind shared schemas or unrelated records.
- **Evidence:** [recovery_metadata.go](../internal/workspace/recovery_metadata.go), [history.go](../internal/workspace/history.go). TRD section 16.1; V06.

### CORE-007 Complete database query scaling and virtualization

- [ ] **P0 table and P1 gallery; owners A and C; shared query/cursor contract review required.**
- Compile supported filters/sorts into parameterized database queries, paginate without reevaluating the entire collection for every page, and virtualize table rows/gallery cards. Current loading drains every 200-row page and renders all results.
- **Acceptance:** Large representative collections load incrementally with bounded memory and visible-row rendering. Typed filters, formula/rollup behavior, stable sorting, scopes, and cursor invalidation remain correct. Measure bridge/paint as well as service time; do not reuse page-search benchmarks as database-view evidence.
- **Evidence:** [database.go](../internal/workspace/database.go), [service.ts](../frontend/src/database/service.ts), [DatabaseView.tsx](../frontend/src/database/DatabaseView.tsx), [AdvancedDatabase.tsx](../frontend/src/database/AdvancedDatabase.tsx). TRD sections 24.2 and 24.3.

### CORE-008 Honor core view configuration and calendar range editing

- [ ] **P0; owner C; coordinate with CORE-007.**
- Respect saved visible-property ordering and widths, provide the promised column controls, and support calendar date-range resizing in addition to moving/rescheduling.
- **Acceptance:** Property order, width, visibility, sort/group configuration, and calendar ranges survive restart and linked-view use. Resizing changes duration through guarded mutations and preserves timezone semantics.
- **Evidence:** [DatabaseView.tsx](../frontend/src/database/DatabaseView.tsx). PRD section 16.3; TRD section 24.2.

### CORE-009 Resolve semantic index implementation and scale

- [ ] **P0 technical contract and qualification; owners A and E; coordinate with CORE-004.**
- Retrieval currently scans JSON vectors and scores them in Go. Implement the selected sqlite-vec retrieval path, or obtain an explicit architecture decision supported by realistic performance evidence and update the technical baseline.
- **Acceptance:** Exact model/hash/dimension generations are maintained; incompatible changes rebuild derived indexes without affecting notes; scope/revision checks hold; large-corpus latency and memory meet the declared benchmark profile.
- **Evidence:** [search_semantic.go](../internal/workspace/search_semantic.go). TRD sections 9, 19, and 30. The existing hybrid timing evidence covers only 100 pages.

### CORE-010 Resolve the backup snapshot contract

- [ ] **P0 technical contract; owners A and D; no dependency.**
- Current backups use VACUUM INTO while holding the application writer lock. Implement the specified SQLite online backup API with asset pinning, or record an explicitly accepted alternative with measured edit-blocking limits.
- **Acceptance:** Concurrent edits and recording remain within documented responsiveness bounds; database/asset consistency, cancellation, insufficient disk, crash recovery, staged restore, and source preservation pass. Do not label existing backups corrupt solely because their implementation differs.
- **Evidence:** [backup.go](../internal/workspace/backup.go). TRD section 16.

## Remaining P1 personal-workspace behavior

### PARITY-001 Complete rich layout and reusable-block editing

- [ ] **P1; owner B; depends on FIX-003.**
- Add/delete tab and column groups, support the declared rich child block types in layouts, and render reusable content with its supported rich structure rather than only a text card.
- **Acceptance:** Code, media, tables, and nested content can be edited through supported layout controls without flattening. Identity, moves, duplication, source updates, cycle rejection, undo, preservation, and recovery remain correct.
- **Evidence:** [AdvancedBlocks.tsx](../frontend/src/editor/AdvancedBlocks.tsx), [Phase 4A limits](phase-4/4a.md). PRD section 16.1.

### PARITY-002 Complete bookmarks and optional online embeds

- [ ] **P1; owners B and D; explicit network consent required at use time.**
- Implement opt-in metadata fetching/caching and the declared sandboxed live embed behavior. Current embeds always display inert local text and open sources externally.
- **Acceptance:** No network request occurs without the intended explicit action. Supported sources display in a restricted context, unsafe content remains inert, and offline/source-unavailable behavior uses the cached fallback. Never execute imported HTML automatically.
- **Evidence:** [AdvancedBlocks.tsx](../frontend/src/editor/AdvancedBlocks.tsx), [Phase 4A](phase-4/4a.md). PRD sections 16.1 and 16.7.

### PARITY-003 Complete timeline behavior

- [ ] **P1; owner C; depends on FIX-004 and coordinate with CORE-007.**
- Implement actual day/week/month/quarter/year scale behavior, grouping, and dependency overlays. The existing scale selector mainly changes its label while geometry remains unchanged.
- **Acceptance:** Scale changes alter the visible time axis meaningfully; saved grouping and dependency relationships are visible; date ranges and configured ordering are respected after restart. No scheduling optimizer is required.
- **Evidence:** [AdvancedDatabase.tsx](../frontend/src/database/AdvancedDatabase.tsx). PRD section 16.3; TRD section 24.2.

### PARITY-004 Unify database templates with values and page bodies

- [ ] **P1; owners B and C; shared template contract review required.**
- Complete one usable template flow containing both configured default property values and page-body content. Existing record-value templates and page-body templates are separate partial flows.
- **Acceptance:** A selected template creates a new page-backed record with both parts, fresh/remapped identities, preserved references, normal approval/revision rules, and no accidental shared mutable template content.
- **Evidence:** [AdvancedDatabase.tsx](../frontend/src/database/AdvancedDatabase.tsx), [Phase 4A](phase-4/4a.md), [Phase 4B](phase-4/4b.md). PRD sections 16.2 and 16.3.

### PARITY-005 Close the formula compatibility contract

- [ ] **P1; owner C; scope decision plus implementation/qualification where needed.**
- Map the published interpreter subset against R05: result types, lists, page/person references, relation traversal, blanks, dates/timezones, formatting, regex bounds, clocks, cycles, and diagnostics. Implement committed missing semantics or explicitly resolve unsupported behavior in the compatibility plan.
- **Acceptance:** Independent expected-output fixtures cover every claimed feature; original expressions and captured values remain distinct from live results; unsupported expressions preserve evidence and report useful errors. Full proprietary Notion-function parity is not required.
- **Evidence:** [Formula compatibility matrix](phase-4/4b.md). PRD section 16.4.2; TRD section 24.4 and V05.

### PARITY-006 Complete Notion API database and source reconstruction

- [ ] **P1; owners C and D; depends on stable advanced database contracts.**
- Extend page/block capture to authorized database containers/data sources, schemas, records, views, and paginated property values where the supported API supplies them. Retain distinct source identities and reconcile field-level outcomes. Implement attachment-reference refresh and other channel behavior required by available source data.
- **Acceptance:** Multiple sources in one container remain distinct; linked views share canonical records; interrupted pagination/downloads resume without duplication; permissions never look like empty content; original formulas/caches/provenance remain separate; captured content works after disconnect.
- **Evidence:** [import_notion.go](../internal/workspace/import_notion.go), [Phase 4C limits](phase-4/4c.md). PRD R01/R02/R03; TRD section 8.1 and V01–V03.

### PARITY-007 Preserve reconstructable nested import structure

- [ ] **P1 API/HTML fidelity; owners B and D; coordinate with PARITY-001 and PARITY-006.**
- Replace flattening of supported nested non-page blocks with canonical hierarchy reconstruction when source relationships are available. Audit HTML, synced/layout relationships, comments, identities, places, and transcript evidence against each channel's actual payloads.
- **Acceptance:** Supported source structure is reconstructed; unavailable or unsupported semantics retain raw/readable evidence and accurate partial outcomes. Known omissions and unknown completeness stay separate. Do not invent source metadata or claim unavailable comments/transcripts were captured.
- **Evidence:** [import_notion.go](../internal/workspace/import_notion.go), [Phase 4C](phase-4/4c.md). PRD sections 7 and 16.6; TRD section 26.

### PARITY-008 Render usable rich page PDFs

- [ ] **P1; owners B, D, and F; depends on corrected rendering/preservation paths.**
- Replace the current text rendition with printable page rendering for supported rich content, including images, tables, diagrams, equations, and meaningful layout.
- **Acceptance:** Export representative rich pages offline; inspect pagination, clipping, missing glyphs, and attachment handling; retain Unicode and readable fallbacks. Pixel-perfect Notion PDF reproduction is not required. State platform support explicitly.
- **Evidence:** [export_pdf.go](../internal/workspace/export_pdf.go), [Phase 4C](phase-4/4c.md). PRD section 16.6.

### PARITY-009 Deliver the browser clipper workflow

- [ ] **P1; owners B, D, and F; depends on existing safe capture/import interfaces.**
- Provide the specified browser extension and approved local bridge/share route to capture article/text/URL/images into Inbox or a selected page. Current manual paste/share remains useful but does not complete that workflow.
- **Acceptance:** Pairing, source provenance, sanitization, selected destination, explicit resource acquisition, revoked pairing, unavailable app, and offline retained content are tested. No unauthenticated arbitrary-write endpoint is introduced.
- **Evidence:** [Phase 4D](phase-4/4d.md). PRD section 16.7; TRD section 28.

### PARITY-010 Expose usable automation configuration

- [ ] **P1; owners A and B; no dependency beyond current rule services.**
- Expose the supported weekly schedules, typed conditions, record/property/task actions, recurring templates, and missed-run controls through usable reviewed configuration. The current form exposes only a subset of service capability.
- **Acceptance:** Users can configure the documented subset without hand-authoring internal commands; preview/run history/pause/retry work; origin exclusions, DST, deduplication, sleep/restart, and bounded catch-up remain correct. No always-on service or unrestricted vendor automation parity is required.
- **Evidence:** [Phase 4E](phase-4/4e.md). PRD section 16.8; TRD section 14.1 and V07–V08.

## AI assistance and quality

### AI-001 Complete reviewed workspace-agent actions

- [ ] **P1; owners A and E; depends on CORE-004 and stable tool contracts.**
- Bridge model-generated plans to valid typed action proposals instead of requiring users to supply internal identifiers/revision JSON. Add the bounded tool-use orchestration needed for the promised create/update page/task/database workflows.
- **Acceptance:** A representative natural-language task becomes an understandable proposal, is applied only through approved scoped services, and handles cancellation, stale revisions, retries, failure, and partial progress without duplicate effects. Code and arbitrary shell commands are never executed by default.
- **Evidence:** [ai_advanced.go](../internal/workspace/ai_advanced.go), [AIPanel.tsx](../frontend/src/ai/AIPanel.tsx), [Phase 4F](phase-4/4f.md). PRD section 17; TRD section 27.

### AI-002 Qualify context budgeting and every exposed AI mode

- [ ] **P0/P1; owners E and F; depends on CORE-004; coordinate with AI-001.**
- Review the current small prompt, selection, evidence, and output caps against supported model contexts and representative notes/code. Improve chunking/multi-step processing where needed. Evaluate summaries, rewrites, Q&A, databases, Mermaid, code help, and organization suggestions with actual supported local packs.
- **Acceptance:** Long inputs have useful bounded behavior and explicit omissions; code replacement cannot drop omitted source; malformed outputs stay drafts; citations resolve to supplied current evidence; no silent cloud fallback. Record correctness, citation validity, latency, memory, and correction effort rather than relying only on schema-valid output.
- **Evidence:** [ai.go](../internal/workspace/ai.go), [app_ai.go](../app_ai.go), [Phase 4F](phase-4/4f.md). PRD sections 8 and 17; TRD sections 10 and 27.

### AI-003 Qualify memory and semantic related-content workflows

- [ ] **P0 memory and P1 related content; owner E; depends on CORE-004.**
- Extend actual-model with/without-memory evaluation for preferences, project constraints, contradictory and superseded decisions, source edits/deletion, and forgetting. Ensure the intended related-page/semantic-backlink surfaces use semantic candidates when available and label lexical heuristics accurately.
- **Acceptance:** Memory improves the declared task set without overriding newer authoritative records; stale/forgotten entries remain excluded during concurrent changes, including entries restored in those states; decisions cite evidence. Explain that restoring an older backup can recover earlier memory states: forgetting does not retroactively erase historical backups. Semantic and lexical suggestions are distinguishable and scope-safe. Existing memory lifecycle services remain the foundation.
- **Evidence:** [memory.go](../internal/workspace/memory.go), [advanced_editor.go](../internal/workspace/advanced_editor.go), [Phase 4F](phase-4/4f.md). PRD sections 16.5, 17, and 21; TRD section 31 and V09.

## External integrations

### EXT-001 Complete managed Claude Code execution

- [ ] **P1; owners A and F; depends on the shared provider/tool boundary.**
- Implement and qualify discovery/version checks, the unchanged official runtime, vendor-owned authentication, contained process launch, bounded streaming, approvals, cancellation, compatible resume, disconnect, and durable execution provenance. Retain separate explicitly selected billing modes.
- **Acceptance:** A supported real account completes a scoped task; denied tools, interrupted output, process failure, stale changes, revocation, and disconnect are handled correctly. Native filesystem/shell capabilities cannot bypass Trove permissions. Do not copy subscription credentials or enable the mode before containment is enforceable.
- **Evidence:** [Provider implementation boundary](phase-4/4g.md), [providers.go](../internal/workspace/providers.go). PRD FR-018 and section 10; TRD sections 13.1–13.2. Recheck official vendor methods before implementation/release.

### EXT-002 Complete managed Codex execution

- [ ] **P1; owners A and F; depends on the shared provider/tool boundary.**
- Implement the currently supported official account authorization and app-server execution route, including credential storage/refresh, identity validation, runtime/version qualification, initialization, streaming, approvals, resume, and disconnect. Validate the document's provider assumptions against official guidance when doing the work.
- **Acceptance:** A real completed request proves account/model eligibility; refresh/resume, account switching, denied tools, expired/revoked authorization, interrupted streams, and cancellation pass. Credentials remain out of logs/workspace/model prompts. API billing is never an automatic fallback.
- **Evidence:** [Phase 4G](phase-4/4g.md), [adapter.go](../internal/providers/adapter.go). PRD FR-018; TRD sections 13.1 and 13.3.

### EXT-003 Add reviewed MCP page and task creation

- [ ] **P1; owner A; shared permission and grant design required.**
- Safely extend the paired bridge beyond edits to existing approved content. New objects need explicit parent/destination scope, review, and ownership rules rather than silently widening a page grant.
- **Acceptance:** Authorized page/task creation works after review; invalid destinations, repeated requests, revoked grants, stale parents, and disconnected clients cannot create or duplicate content. Delete and arbitrary-file access retain their separate policies.
- **Evidence:** [providers.go](../internal/workspace/providers.go). PRD section 18; TRD section 12.

### EXT-004 Complete the supported remote MCP route

- [ ] **P1 optional connection feature; owners A and F; depends on transport/authentication design.**
- Implement the explicitly opt-in authenticated remote transport/connectivity needed for eligible hosted clients, or formally resolve this unfulfilled release commitment. Laptop loopback alone is not reachable by a hosted client.
- **Acceptance:** Supported real-client connectivity, authentication/revocation, scope, approvals, disconnect, failure recovery, and credential handling pass. The bridge still forwards to the one owning kernel; local workflows work with the connection disabled.
- **Evidence:** [Phase 4G](phase-4/4g.md), [app_providers.go](../app_providers.go). PRD sections 10 and 18; TRD sections 13 and 32.3.

### EXT-005 Qualify provider handoffs and client interoperability

- [ ] **P1; owners A, E, and F; depends on EXT-001 through EXT-004 as applicable.**
- Exercise supported real clients and cross-provider handoffs. Verify source-revision manifests, execution provenance, approved results, pending work, and fresh receiving authorization.
- **Acceptance:** Handoffs exclude secrets, hidden reasoning, unrelated history, and uncommitted changes. Resume remains provider/account/version compatible. One active mutating provider run is enforced; duplicate retries cannot repeat effects. No unannounced provider/billing/context switch occurs.
- **Evidence:** [Phase 4G](phase-4/4g.md). PRD section 10.2; TRD sections 13.1–13.3 and V10.

## Release qualification and operational work

### REL-001 Freeze and identify the final release candidate

- [ ] **P0 and each later release; owners A and F; follows the chosen release's fixes.**
- Produce one immutable candidate with source, app, helper, model, and evidence identities. Run relevant regressions after integration and preserve failed attempts. Do not transfer historical candidate passes to changed binaries without justification.
- **Acceptance:** Source/bundle hashes remain stable through qualification; required checks cannot pass through missing/skipped evidence; candidate-specific limitations and approval state are explicit.

### REL-002 Finish native end-to-end acceptance

- [ ] **P0/P1; owners B, D, E, and F; depends on REL-001 and applicable fixes.**
- On the final packaged app, exercise file/model pickers, import preview/progress, rich clipboard, editing/save/reopen, search/citation navigation, model selection, memory controls, linked views/formulas, automation, interactive backup/restore, and subsequent quit/restart.
- **Acceptance:** Record actual user-visible behavior on that exact build, including restored content and selected workspace. Mocked-browser/service fixtures cannot substitute for native observations. Resolve the previously interrupted final-candidate checks.
- **Evidence:** [Outstanding native acceptance](phase-5/qualification.md).

### REL-003 Qualify physical audio and screenshot capture

- [ ] **P0 microphone; P1 screenshot; owners B and F; depends on CORE-005 where uploaded formats are involved.**
- Test permission grant/denial, actual microphone duration/bytes, stop/save, playback/seek, transcription progress, editing during processing, screenshot retention, ordinary picker cancellation, and process cleanup.
- **Acceptance:** Device/user-consent outcomes are visible and recoverable; captured originals survive interruption; no unrelated private audio/screen content is used as a fixture. System-audio capture remains separately platform-qualified and need not block a microphone/upload-first release.

### REL-004 Validate real migration and meeting datasets

- [ ] **P0/P1; owners D, E, and F; depends on applicable import/audio fixes.**
- Use authorized representative small/medium/large Notion exports and varied natural meetings/voice notes. Validate actual HTML/API accounts for the channels being released. Include nested structures, unsupported content, links, assets, schema changes, noisy speech, pauses, and the declared language support.
- **Acceptance:** Reconcile every discovered item, compare meaningful source content and asset bytes, report source omissions, and measure task/transcript quality. Repeated public speech and fixture HTTP servers remain useful regression inputs but do not establish general fidelity or live-account compatibility.

### REL-005 Qualify complete installation and model-pack paths

- [ ] **P0; owner F; depends on REL-001.**
- Test a separate clean Mac without developer tools. Exercise the model-free app, explicit in-app model import/acquisition, and the promised complete offline distribution. Verify format/architecture/runtime/hash/license/space checks and interrupted downloads.
- **Acceptance:** Core app installation needs no Node, Python, Ollama, Docker, database server, or product account. Installed packs support offline AI/transcription. Backup/restore succeeds on a second clean installation. An empty PATH on the development Mac is not equivalent evidence.

### REL-006 Finish distribution, update, and recovery qualification

- [ ] **Desktop update/recovery baseline and public distribution gate; owner F with D; final evidence depends on REL-001.**
- Complete the desktop baseline for verified updates, pre-migration snapshots, matching app/data rollback, external backup destinations, and uninstall-with-data-preservation behavior. Separately, qualify public distribution through appropriate signing/notarization and Gatekeeper checks, package notices, and model redistribution evidence. Update/recovery obligations must not be deferred until public distribution.
- **Acceptance:** Published artifacts have verified hashes and reproducible manifests; upgrades and rollback preserve canonical data; failed updates retain recoverable state; uninstall does not delete workspaces without an explicit request. Keep the current ad-hoc build labeled as such until these checks pass.

### REL-007 Benchmark realistic end-to-end performance

- [ ] **P0/P1; owners A, B, C, E, and F; depends on CORE-007 and CORE-009.**
- Measure large databases, realistic page bodies, attachment-heavy imports, large hybrid corpora, simultaneous recording/indexing/AI, and memory pressure. Include UI bridge/render time, not only service calls. Define hardware, model packs, corpus shape, cold/warm conditions, and percentiles.
- **Acceptance:** Verify the declared approximately-two-second shell, under-300-ms common keyword search, under-one-second hybrid retrieval excluding optional reranking, and responsive editing against the agreed profiles. Record limits honestly; a 100-page hybrid benchmark or 10,000 simple pages is not evidence for all workloads.
- **Evidence:** [Existing qualified benchmark scope](phase-5/qualification.md).

### REL-008 Add regression coverage for the audit gaps

- [ ] **P0/P1; all owners; each fix carries its own regression.**
- Preserve the two temporary audit reproductions as durable repository tests when fixing them. Add actual-control tests for rich-text preservation, deep nested toggles, linked-view placement, copied block links, table structure, timeline scale, common audio, and semantic Q&A.
- **Acceptance:** Tests fail on the audited behavior and pass after the fix. Use independent expected outcomes and archive/content inspection; avoid tests that simply repeat implementation logic. Run affected existing suites and real integration paths before closing an item.

### REL-009 Qualify additional platforms separately

- [ ] **Later platform rollout; owner F; not a blocker for an explicitly macOS-only initial release.**
- For each promised Windows/Linux build, qualify installers/WebView prerequisites, SQLite/native workers, capture/decoding, clipboard, permissions, filesystem semantics, offline packs, restart, and restoration.
- **Acceptance:** Publish platform-specific supported features and evidence. Do not infer cross-platform readiness from Go portability or the macOS build.

## Governance and architecture follow-through

### GOV-001 Establish source control and coordinated integration

- [ ] **Engineering foundation; owner A; can start immediately.**
- Initialize Git and create a reviewed baseline after confirming ignore rules exclude workspaces, credentials, models, caches, and generated build artifacts. The audited folder was not a Git repository. Use separate branches/worktrees for parallel implementation and one owner for shared contracts/migrations.
- **Acceptance:** Changes have reviewable history, shared baseline updates reach dependent workstreams, integrated checks run before promotion, and no user data/secrets enter commits. This documentation task does not itself initialize Git.

### GOV-002 Make development and qualification reproducible

- [ ] **Engineering foundation; owners A and F; can start immediately.**
- Replace fragile assumptions about temporary tool/cache paths with a documented bootstrap and configurable tool locations. During the audit, the documented temporary Python environment referenced a runtime that no longer existed; checks required an available Python 3.12 interpreter with the existing dependencies.
- **Acceptance:** A new contributor can provision the pinned toolchain and run checks without another chat's temporary environment. Record exact native build inputs and distinguish build dependencies from end-user runtime requirements.
- **Evidence:** [README setup](../README.md), [baseline](phase-0/baseline.md), [requirements-dev.txt](../requirements-dev.txt).

### GOV-003 Reconcile the requirements ledger and completion claims

- [ ] **All releases; owner A with every feature owner; update after each completed item.**
- Map every accepted requirement to real implementation, supported scope, test identity, build-bound evidence, and an open gap where applicable. Reconcile broad partial/not-started ledger rows with focused passing tests rather than calculating a misleading completion percentage.
- **Acceptance:** Native editing, import reconstruction, fallback preservation, and verification are separate claims. A passing verification family does not close unimplemented breadth. Preserve historical reports; publish a new assessment for new evidence.
- **Evidence:** [Requirements ledger](phase-0/requirements-ledger.json), [Phase 5 qualification](phase-5/qualification.md).

### GOV-004 Obtain and reconcile the referenced research evidence

- [ ] **Full audit-parity claim; owner A with D; source material required.**
- Obtain the separately referenced 307-behavior Notion research report and its fixture/source provenance. Reconcile accepted audit IDs with R01–R08, release scope, implementation, and V01–V10.
- **Acceptance:** Each accepted behavior has traceable evidence or an explicit unresolved state. Missing source wording is not invented. Research proposals do not become accepted features automatically. The CodeSwitchOS documents are design precedent, not a prerequisite for fixing known product defects.
- **Evidence:** PRD sections 15.1 and 22; TRD section 29.1; ledger unavailableSources.

### GOV-005 Resolve selected-stack deviations explicitly

- [ ] **Technical conformance; owner A with relevant specialists; coordinate with CORE-009 and CORE-010.**
- Review the audited differences: scalar vector scanning versus sqlite-vec retrieval, custom MCP protocol subset versus the selected official Go SDK, native PDF rendering versus PDF.js, highlight.js versus Shiki, backup snapshot API, and native capture implementation versus the selected baseline.
- **Acceptance:** Implement the selected contract or record an explicitly accepted ADR with behavior, maintenance, security, licensing, offline, and qualification consequences. A different library is not automatically a user-visible bug; silently claiming exact TRD conformance is not acceptable.
- **Evidence:** TRD section 19; [frontend dependencies](../frontend/package.json), [Phase 4G](phase-4/4g.md), [backup.go](../internal/workspace/backup.go), [search_semantic.go](../internal/workspace/search_semantic.go).

## Optional P2 continuation

The existing P2 packages are real bounded implementations. Limits alone are not defects. Keep the following work separate from P0/P1 stabilization.

### OPT-001 Qualify and optionally expand local image intelligence

- [ ] **P2; owners E and F; optional scope decision.**
- Existing Apple Vision OCR is text extraction for a bounded input set. General local image description/understanding is absent. Decide whether to implement that separate capability, and qualify the exact supported OCR formats/languages/layouts.
- **Acceptance:** Use representative images and document quality/resource limits. Additional handwriting, scanned-PDF, rotation, or language support is required only when explicitly selected. Images remain local unless the user separately invokes an external integration.

### OPT-002 Decide whether personal review needs generative analysis

- [ ] **P2; owners A and E; optional scope decision; depends on AI-002 for a generative mode.**
- Current personal review is a deterministic scheduled digest with an explicitly approved append action. Richer generated analysis or configurable task actions require a separate agreed scope.
- **Acceptance:** Preserve occurrence deduplication, activation consent, current-source/memory checks, reviewed writes, and failure isolation. Do not rename a digest as autonomous task management or introduce unreviewed knowledge rewriting.

### OPT-003 Qualify declared P2 package boundaries

- [ ] **P2 package promotion; relevant owners; after applicable correctness fixes.**
- Validate charts/forms/row actions, graph limits, OCR, reviews, conversation imports, display settings, and hosting against their documented supported boundaries using representative real data.
- **Acceptance:** Conversation fixtures preserve branches/roles/timestamps/provenance; graph associations disclose evidence; calculations and submissions remain canonical; operational settings stay inert on portable import; host authentication/TLS/ownership transfer/recovery remain correct. Arbitrary remote infrastructure, network filesystems, and offline multi-device sync are not implied by the existing host tests.
- **Evidence:** [P2 qualification and limits](phase-6/qualification.md), [P2 hosting](phase-6/hosting.md).

## Scope boundaries that must remain explicit

- Formula compatibility does not require every proprietary Notion expression. It requires honest supported semantics, diagnostics, preserved originals/captured values, and independent tests.
- Automation does not require every proprietary trigger/action, external side effects, or an always-on background service.
- Optional reranking, reliable diarization, and system-audio capture beyond the accepted microphone/upload-first boundary are not automatic P0 blockers.
- P2 capability limits should be expanded only through explicit scope decisions, not because an implementation has a cap.
- Feed/dashboard/map interfaces, a new annotation editor, reusable agent skills, and AI autofill remain uncommitted proposals.
- Team collaboration, enterprise administration, product billing infrastructure, mandatory cloud services, and cloud/offline multi-device sync remain excluded from the current baseline.

## Parallel execution plan

| Workstream | Start immediately | Follow after its dependencies |
| --- | --- | --- |
| A Kernel and integration | FIX-002 with D; GOV-001/GOV-002/GOV-003; contract reviews | CORE-006/007/009/010; automation/provider integration |
| B Editor and desktop | FIX-003/FIX-005/FIX-006; CORE-001/002/003 | FIX-004 with C; PARITY-001/002/004/008/009; native acceptance |
| C Databases | FIX-004 with B; CORE-007/008; PARITY-005 compatibility review | PARITY-003/004/006; realistic database benchmarks |
| D Migration and recovery | FIX-001; FIX-002 with A; CORE-006/010 | PARITY-006/007/008/009; real migration and second-install restoration |
| E Search and intelligence | CORE-004; CORE-009 design; AI-002 evaluation fixtures | AI-001/003; real model/meeting quality and optional P2 intelligence |
| F Native and release | CORE-005; reproducible toolchain; clean-machine/signing preparation | EXT-001/002/004/005; final model/device/package qualification |

Keep shared schema/generated-type changes coordinated. Export/recovery changes need cross-review because page bodies, property values, provenance, recordings, and assets overlap. Performance work must retain scope/revision correctness. Provider work must not widen the core permission model merely to make a demo succeed.

## Release gates

### P0 usable replacement

- [ ] Critical disclosure/content-loss/recovery defects are fixed with regression coverage.
- [ ] All committed P0 editor, database, migration, search, AI, audio, memory, history, and portability behaviors are implemented at their declared scope.
- [ ] Representative imports reconcile without silent loss; common declared audio inputs work locally.
- [ ] Final packaged native journey and second clean-install backup/restore pass.
- [ ] Model-free operation and installed-pack offline operation both pass.
- [ ] Benchmark, model/runtime, license, installation, and recovery evidence belongs to the actual candidate.
- [ ] The requirements ledger and release assessment identify all remaining limitations accurately.

### P1 full personal version

- [ ] P0 regressions remain closed after P1 integration.
- [ ] Advanced editor/views/templates/formulas and migration/capture behavior meet their accepted compatibility contracts.
- [ ] Deterministic automation and reviewed AI workflows are usable through the product interface.
- [ ] Committed MCP/provider routes are implemented and qualified; users may keep them disabled without impairing the local core.
- [ ] Real source/account/device cases and V01–V10 evidence cover the claimed scope.
- [ ] Any approved scope or stack changes are recorded explicitly; unavailable features are not counted as completed.

### P2 and public distribution

- [ ] Each optional package is promoted only against its own acceptance boundary.
- [ ] General vision or richer review-agent behavior is claimed only if implemented and evaluated.
- [ ] Publicly distributed platforms and artifacts pass their own installation, signing, update, and recovery gates.
- [ ] No release claim implies unsupported collaboration, sync, unlimited model quality, or blanket Notion parity.

## Source documents

- [Trove PRD version 3.1](../Trove_PRD.docx)
- [Trove TRD version 3.1](../Trove_TRD.docx)
- [Requirements and evidence ledger](phase-0/requirements-ledger.json)
- [P0 qualification](phase-3/qualification.md)
- [P1 package implementation and limits](phase-4/implementation.md)
- [P1 release qualification](phase-5/qualification.md)
- [P2 preview qualification](phase-6/qualification.md)

This backlog is exhaustive for the findings and unresolved gates identified in the 1 October audit, not a claim that every undiscovered defect or missing research behavior has been enumerated. Add newly discovered work with its requirement, evidence, owner, and acceptance criteria rather than silently broadening an existing item.
