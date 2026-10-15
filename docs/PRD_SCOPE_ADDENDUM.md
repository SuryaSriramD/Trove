# Trove PRD scope addendum — personal desktop features

**Version:** 1.0  
**Date:** 13 October 2026  
**Status:** Accepted scope for implementation; implementation and release acceptance are tracked separately.  
**Applies to:** [Trove PRD v3.1](../Trove_PRD.docx) and [Trove TRD v3.1](../Trove_TRD.docx), dated 30 September 2026.  
**Current progress and decisions:** [PROGRESS.md](../PROGRESS.md).  
**Scope authority:** User request to add the annotated Notion-review features to the PRD, reconciled with decisions D-P01–D-P10 and accepted contracts D001–D003.

## 1. Purpose and relationship to the existing PRD

This addendum makes the selected research recommendations explicit product requirements. It adds personal reminders, reusable AI skills, AI database autofill, page freshness, richer meeting controls, reusable record layouts, reviewed dependency rescheduling, encrypted portable backups and selected connected-app workflows. It also tightens migration, task consistency, quality and recovery acceptance.

Read this document alongside the existing PRD. The additions and explicit changes in §3 supersede the older wording only for those named behaviors. Unaffected PRD requirements, accepted architecture decisions, platform boundaries and original backlog acceptance remain in force. The original Word documents are retained as the v3.1 baseline; this file does not silently publish a rewritten v3.2.

The October research assessed requirements, not the current implementation. The repository already contains substantial functionality and accepted task/re-import contracts. Existing implementation must be inspected and completed or verified; it must not be rebuilt solely because the earlier review called something a gap.

This is a requirements specification, not a second progress dashboard. Record implementation, verification, blockers, source identities and development history only in `PROGRESS.md`. Requirement IDs below reuse G01–G12 and their existing child workstreams. An acceptance scenario is a required outcome, not a claim that a test has passed.

## 2. Product goal and delivery boundaries

Trove shall provide a private personal desktop workspace in which the user can import knowledge, organize structured work, retrieve evidence, record and transcribe meetings locally, review suggested actions, and maintain explicitly approved memory. Core local work shall remain usable without a product account, connectors or external AI providers. Installed model packs are required for the corresponding local AI operations.

The accepted delivery order is:

| Delivery boundary | Included scope | Meaning |
| --- | --- | --- |
| Core correctness and macOS P0 — M1/M2 | G01–G05 and relevant existing recovery/install/quality requirements | Close existing promises and the selected encrypted-backup/meeting-control additions before the applicable release claims. |
| Full personal desktop — P1/M3 | G06–G10 | Deliver reminders, skills, autofill, freshness, record layouts and reviewed rescheduling. These additions do not become accidental P0 prerequisites. |
| Connected workflows — P1 integration milestone M4 | G12 / CONN-00–06 | Deliver each named adapter with independent live acceptance. Connector failure or absence must not disable local P0 work. |
| Deferred mobile decision | G11 | Retain the phone-capture need and revisit a companion after desktop completion. A mobile app or offline multi-device sync is not committed by this addendum. |
| Existing later/platform work | P2 packages; Windows and Linux milestones | Retain their existing scope and qualify separately. macOS ships first; another platform's open gates do not imply macOS failure or platform readiness. |

No delivery date, new hardware minimum, completion percentage or release approval is established here.

## 3. Changes to the baseline PRD/TRD

| Existing section | Addition or clarification | Requirement IDs |
| --- | --- | --- |
| PRD §7; TRD §§5, 8, 26 | Apply accepted three-way merge, source-completeness and conflict-review contracts consistently across import paths. | G01; D002 |
| PRD §§6, 11, 16.3–16.4; TRD §5 | Tasks are canonical record facets; todo blocks and task surfaces reference/project that record. | G02; D001 |
| PRD §12; TRD §§15–16 | Promote encrypted portable native backups from “optional encryption later” to the accepted delivery scope. Rely on OS disk protection for the live workspace; app-managed workspace encryption is not selected. | G03; D-P03 |
| PRD §§3, 14, 19–21; TRD §§18, 29–32 | Declare supported workloads, hardware/model conditions and independent quality rubrics, retaining existing qualification limits. | G04 |
| PRD §§9, 12.1, 17; TRD §§11, 16, 30.3–31 | Specify recording controls, participant disclosure, correction provenance, storage faults and separate retention controls. | G05 |
| PRD §§6, 11, 16.8, 18.2; TRD §14 | Add opt-in personal reminders while the main app/window is closed. This is a narrow exception to while-running schedule behavior; general automation stays within its existing boundary. | G06; D-P06 |
| PRD §§15.1, 17, 21; TRD §§12, 27, 31 | Promote reusable AI skills from proposal to P1 manual workflows with bounded scope and reviewed writes. | G07 |
| PRD §§15.1, 16.4, 17; TRD §§24, 27 | Promote AI database autofill from proposal to P1 selected-row/property generation with typed, reviewed results. | G08 |
| PRD §§16.5, 21; TRD §§5, 9, 31 | Add ordinary-page review/freshness independently of approved-memory freshness. | G09 |
| PRD §§16.3–16.4; TRD §24 | Add reusable record-page layouts and explicit previewed dependency rescheduling. Dependency visualization alone does not satisfy rescheduling. | G10-LAYOUT; G10-SCHEDULE; D-P07 |
| PRD §§18.2, 20.2; TRD §32.2 | Preserve desktop-first positioning and the mobile deferral. A headless host is not mobile offline synchronization. | G11; D-P02 |
| PRD §§7, 10, 16.7, 18.1; TRD §§8, 13, 28 | Add six selected live adapters, bounded refresh/search/previews and explicitly reviewed external writes. Bring outbound third-party MCP from TRD §13's later scope into this integration milestone. Local import, inbound MCP and outbound MCP remain distinct behaviors. | G12; CONN-00–06; D-P04–05 |

Charts, local forms and row-action properties remain P2. General dashboards, map/feed interfaces, native annotation editors and unselected research suggestions are not promoted simply because they appeared in the research. Teamspaces, multiplayer collaboration, SAML/SCIM, organization administration and initial cloud workspace sync remain excluded.

## 4. Shared behavior requirements

- **Local operation:** No new feature may silently send local content to a provider. External accounts, selected sources and model/provider choice must be visible and opt-in. Network-dependent features must clearly identify offline or unavailable results.
- **One write authority:** Canonical workspace changes shall use the existing owning kernel, scoped services, revision checks, history and idempotency. A helper, reminder process or external connector shall not become an independent SQLite writer.
- **Current evidence:** Source IDs, revisions, timestamps and stable locations shall survive changes where possible. Changed, deleted, denied or unavailable sources must not be treated as current evidence.
- **Project and memory scope:** Preserve [D003](phase-0/decisions.md). Backlinks, database relations, page-tree placement and connector results do not implicitly expand project scope. Stale, forgotten or out-of-scope memories shall be excluded at retrieval time, even while index cleanup is pending.
- **Review:** AI suggestions, imported instructions and external tool annotations cannot grant permissions. Applying a write must use the appropriate existing approval contract; destructive or external effects must show their concrete scope.
- **Recovery:** New canonical state must participate in migrations, history and compatible native backup/restore. Restoring data must not silently reactivate credentials, reminders, automations, skills or external writes.
- **Usability:** Exposed workflows need keyboard and accessible controls, useful empty/error states, cancellation and visible partial outcomes. Data must remain readable when a model, provider, permission or renderer is unavailable.

## 5. Core correctness and P0 additions

### G01 — Safe re-import and migration reconciliation

**Base:** PRD §7; TRD §§5.1, 8.1, 26; accepted [D002](phase-0/decisions.md).  
**Delivery:** Reconcile existing implementations and complete missing behavior; do not choose a competing merge design.

**Required behavior**

1. Retain the last accepted source baseline, incoming capture, local identity and source provenance. Compare base/local/incoming using the accepted atomic-field rules.
2. Preserve local-only edits, apply reviewed incoming-only changes, identify convergence and expose both-side conflicts. Unresolved conflicts must not silently overwrite local data.
3. Bind resolution to exact local/source revisions and scope. Reject stale review, recompute the plan and deduplicate retries. Advance the accepted baseline only under D002's resolution rules.
4. Distinguish proven source deletion from inaccessible, partial, omitted or failed enumeration. Upstream moves with stable IDs must not duplicate content. Ambiguous identity requires review.
5. Account for every discovered item as preserved, adapted, partial, unsupported-but-retained or failed. Keep raw expressions, cached values and live calculations separate; retain original bytes when needed for provenance, history or unresolved conflicts.

**Acceptance**

- **G01-A1:** Import, edit both the source and local copy, then re-import. The user can inspect the differences and choose a resolution without losing either retained version.
- **G01-A2:** Exercise source-only, local-only, converged, null-versus-missing, moved, deleted and denied-source cases through ZIP and the applicable HTML/API paths. Results conform to D002 and the source's actual completeness.
- **G01-A3:** Interrupt capture/application, retry and submit a stale resolution. No duplicate canonical object or attachment is created, and newer local edits survive.

### G02 — Consistent tasks across all surfaces

**Base:** PRD §§6, 11, 16.3–16.4; TRD §5; accepted [D001](phase-0/decisions.md).

**Required behavior**

1. A task shall remain one canonical record with its own page body and `TaskFacet`. Todo blocks reference that record; title, completion and due-date surfaces must not create competing copies.
2. Promoting a database record to a task shall preserve its record/page IDs. The Tasks/Home views shall project facets across collections without duplicating records.
3. Deleting a todo occurrence shall not delete its task. Record trash/restore, facet detachment, status mapping and duplicate meeting extraction shall follow explicit reviewed outcomes.
4. Reminder scheduling and meeting-derived actions shall use the same canonical task identity. Imported assignees remain provenance rather than collaboration accounts.

**Acceptance**

- **G02-A1:** Edit title, status and due date from a page occurrence, Tasks/Home and a database. Every surface agrees after restart and native restore.
- **G02-A2:** Promote an existing record, create multiple occurrences, remove one and trash/restore the record. IDs remain stable and no task is silently duplicated or deleted.
- **G02-A3:** Retry extraction/import of an identified task. The existing task is reused or ambiguity is reviewed; equal titles alone do not establish identity.

### G03 — Local protection and encrypted portable backups

**Base:** PRD §12; TRD §§6, 15–16, 32; D-P03.  
**Selected boundary:** OS disk protection for the live workspace plus passphrase-encrypted portable native backups. No SQLCipher migration or app-managed workspace-encryption promise is added.

**Required behavior**

1. Explain separately how the live workspace, recordings, assets, indexes, temporary data and copied backups are protected. Do not describe local storage as E2EE or claim protection from an already-authorized OS user.
2. Offer authenticated encryption for a complete native backup, including its sensitive manifest and retained canonical content. Keep any unavoidable public envelope metadata minimal and documented. Passphrases shall not enter logs, model context or workspace content and shall not be remembered by default.
3. Explain that a forgotten passphrase cannot be recovered by Trove unless a separately designed recovery mechanism exists. Preserve compatibility with existing unencrypted native backups; any unencrypted export must be plainly identified.
4. Restore through private staging, without opening database contents or exposing a verified-content preview until the complete encrypted container authenticates. Authentication, integrity, schema and managed-asset validation must finish before destination activation. Failure, cancellation or insufficient disk must leave the source and existing destination safe.
5. Keep external files, separately installed model weights and credentials outside any unsupported “complete backup” claim. Display exactly what is included and excluded.

**Acceptance**

- **G03-A1:** Restore an encrypted archive on an independent clean installation and compare canonical content, schema, relationships, assets and retained history, not just counts.
- **G03-A2:** Wrong passphrase, tampering, truncation, low disk, cancellation and crash cannot activate a partial destination. Temporary plaintext is handled under a documented cleanup/recovery policy without promising forensic secure erasure.
- **G03-A3:** Existing native archives remain readable, encryption status is visible before export, and no credential or passphrase appears in inspected logs or archive metadata.

An established authenticated format such as the currently proposed age-based container is an engineering choice to review before coding. This PRD requires the behavior, not a custom cipher or an unverified implementation.

### G04 — Measurable quality and support limits

**Base:** PRD §§3, 14, 19–21; TRD §§18, 29–32; existing EVAL-* and REL-* families.

**Required behavior**

1. Publish tested hardware, OS, application/helper/model versions, languages, workspace sizes and representative workload shapes. Distinguish keyword search, hybrid retrieval, generation, transcription and UI responsiveness.
2. Retain current qualification limits: 2,500 ms shell startup, under 300 ms keyword samples and under 1,000 ms hybrid samples in their declared harness scopes. Preserve the existing startup/search/database sample matrix and independent correctness oracles; a new numerical database SLA is not implied.
3. Define independent acceptance rubrics for factual correctness, evidence support, citation navigation, abstention, stale-memory exclusion, speech accuracy, action extraction and correction effort before evaluating new capabilities.
4. Measure foreground responsiveness, resource consumption, interruption and recovery on realistic tasks. Use focused regressions during development and bounded full qualification at release; do not create an open-ended system benchmark programme.
5. Preserve failures, skips and historical results. Prepared inputs, mocks, skipped cases, later diagnostics and results from another candidate do not become current release passes.

**Acceptance**

- **G04-A1:** Each release claim resolves to a named profile, exact candidate and independent pass condition with actual results and limitations.
- **G04-A2:** Answerable, ambiguous and unanswerable queries plus clean/noisy representative audio are evaluated with frozen expected outcomes and manual review where required.
- **G04-A3:** New rubrics and performance policies are versioned before execution; frozen candidate-7 evidence is neither rewritten nor used to qualify a changed product.

No new RAM minimum, 90% manual-success gate or 15%/25% speech-error threshold is accepted here. Those earlier numerical proposals require a separately recorded policy decision before they can gate release.

### G05 — Meeting controls, correction and retention

**Base:** PRD §§9, 12.1, 17, 21; TRD §§11, 16, 30.3–31; CORE-005, REL-003/004.

**Required behavior**

1. Display the selected input, actual recording state and elapsed captured time. Provide accessible start, pause, resume and stop controls. Paused time must not be represented as captured audio.
2. Before recording, explain what is captured and provide a participant-disclosure/consent acknowledgement appropriate to the workflow. Do not imply that Trove has notified other participants or established their consent automatically.
3. Handle OS denial, device removal/switch, sleep, interruption and low disk visibly. Preserve durable captured material, mark gaps and support recovery without silently inventing continuity or deleting old content.
4. Keep timestamped transcript segments navigable to original audio where retained. Corrections create history and preserve original evidence; changed timing/content must update or invalidate derived summaries, action suggestions, citations and approved memories. Previously approved tasks and subsequent user edits must not be silently rewound; identify their changed source evidence for review.
5. Expose distinct retention controls for original audio, transcripts, summaries/actions, search indexes, memory, history and backups. Check retained references and explain affected history/evidence before deletion; do not silently break historical recovery. No automatic history pruning is enabled until the user selects a policy.
6. Local transcription remains the P0 path with installed packs. System-audio capture is independently platform-qualified; microphone/upload-first P0 need not wait for it. Diarization is not silently promoted to a mandatory P0 capability.

**Acceptance**

- **G05-A1:** Start/pause/resume/stop a real authorized recording, deny permission and disconnect an input. Indicators and saved audio accurately reflect what occurred.
- **G05-A2:** Interrupt a recording/transcription and simulate low disk. Retained audio is recoverable, gaps/errors are visible and retry does not duplicate transcripts or actions.
- **G05-A3:** Correct a transcript, navigate its evidence, review resulting actions and apply retention choices. Stale derived information is marked or excluded and unrelated source content remains intact.

## 6. Full personal desktop additions — P1

### G06 — Personal background reminders

**Base:** PRD §§6, 11, 16.8, 18.2; TRD §§5, 14; D-P06.

**Required behavior**

1. Create/edit/cancel a personal reminder linked to a task or page, with reminder time distinct from due date. Display timezone and the policy for date-only values, recurring occurrences and timezone changes.
2. Provide OS notifications, a local reminder inbox, snooze/dismiss and navigation to the source. Task completion goes through the canonical task service.
3. After explicit opt-in, reminders shall work while the main app/window is closed using supported platform facilities. Explain notification permissions, login/startup requirements and limitations while the machine is asleep, off, logged out or OS delivery is suppressed.
4. Keep the background component limited to derived scheduling/delivery state. It must not open canonical workspace SQLite or run AI, generic automations or connector refresh. Publish schedule updates with acknowledgement and registration identity so cancellation and workspace changes cannot leave hidden stale registrations.
5. Deduplicate occurrence processing and handle sleep/restart/DST according to a declared missed-reminder policy. Distinguish scheduled/submitted/failed/unknown delivery from evidence that the user actually saw a notification.
6. On workspace switch, deletion, backup restoration or service opt-out, reconcile or revoke registrations. Imported/restored schedules remain inactive until explicitly re-enabled. Notification text must offer a privacy-preserving display choice.

**Acceptance**

- **G06-A1:** A configured reminder is submitted through the real OS path with the main app closed; denial and disabled-background behavior are visible in settings/inbox.
- **G06-A2:** Snooze, dismiss, complete, edit and cancel across sleep, restart, timezone/DST changes and concurrent schedule publication. No duplicate canonical effect or unreported stale registration occurs.
- **G06-A3:** Restore a workspace and disable/re-enable the background component. Old epochs cannot reactivate unexpectedly, and uncertain OS outcomes remain explicitly uncertain.

### G07 — Reusable AI skills

**Base:** PRD §§15.1, 17, 21; TRD §§12–13, 27, 31; AI-001 and D003.  
**Scope change:** Reusable skills are accepted P1, beginning with manual invocation.

**Required behavior**

1. Create, edit, duplicate and archive named, versioned skills containing purpose, instructions, inputs, expected output, selected sources and permitted tool categories. A run must retain the exact skill version used.
2. Show source/context selection and provider before execution. Default to the local provider; an external provider requires its own configured authorization and approved context.
3. Skill text and imported skills confer no authority. Enforce current grants, project scope and mutation approval at each action. Imported skills remain inert until reviewed and invoked.
4. Provide progress, cancellation, inspectable output and reviewable change proposals. Retry/resume/provider handoff must preserve scope and deduplicate already committed effects; a provider change must not silently widen tools or disclose more context.
5. Keep skills distinct from approved memory, deterministic rules and P2 scheduled review agents. Defining a skill does not enable autonomous scheduling or continuous execution.

**Acceptance**

- **G07-A1:** Run a meeting-follow-up or project-review skill on selected sources, inspect evidence and approve selected changes. Unselected pages and disallowed tools remain unavailable.
- **G07-A2:** Edit the skill version, cancel/retry a run and change provider explicitly. History identifies each version/provider and prior writes are not duplicated.
- **G07-A3:** Import a skill containing instructions to grant access or execute arbitrary code. It remains inert and cannot bypass the ordinary permission layer.

### G08 — AI database autofill

**Base:** PRD §§15.1, 16.4, 17; TRD §§24, 27; G07 and AI-001.  
**Scope change:** Selected-row/property autofill is accepted P1; unattended/event-triggered autofill is not included initially.

**Required behavior**

1. Let the user choose records, target writable properties, instructions and source fields. Support bounded summaries, classifications and extraction into supported typed values. Computed formula/rollup fields are not writable targets.
2. Default to filling blanks. Show nonempty values and proposed overwrites separately; manual values must not be silently replaced. Unknown or unsupported extraction must produce an explicit no-value/review outcome rather than a fabricated fact.
3. Validate output against property type and current schema. Suggestions to create options or alter schema require separate reviewed schema changes.
4. Record row/source/schema revisions, skill/prompt/model version and generated-value provenance. Recheck them at application time. Stale output must be rejected or reviewed again.
5. Show per-row progress and partial failures; permit cancellation and safe resume. Apply reviewed changes with idempotency, history and grouped undo within the declared batch scope.

**Acceptance**

- **G08-A1:** Fill selected empty fields while leaving other records, existing manual values and computed fields unchanged. Invalid or unsupported values remain reviewable failures.
- **G08-A2:** Edit a row or schema during generation and then apply. Newer values survive; stale suggestions cannot overwrite them.
- **G08-A3:** Interrupt/resume and repeat an applied batch, then undo. Effects are not duplicated, provenance remains inspectable and unrelated edits are preserved.

### G09 — Page review and knowledge freshness

**Base:** PRD §§16.5, 21; TRD §§5, 9, 31.  
**Scope change:** Ordinary-page review is separate from memory approval and generative personal reviews.

**Required behavior**

1. Allow the user to mark a page's current content revision as reviewed, recording reviewed-at time and optional review-due/expiry time. Provide an explicit never-reviewed/stale/expired state.
2. Bind review to the content and knowledge-bearing record values it covers. Changing that content or reaching expiry must invalidate current reviewed status. Pure presentation changes shall not falsely renew review or change the covered revision.
3. Provide a review queue/filter and expose freshness in search results and AI evidence. A badge denotes a human review event, not a guarantee that a statement is true.
4. Keep stale knowledge visible for deliberate inspection with clear labels; fresh memory eligibility continues to obey D003. Do not automatically delete or rewrite ordinary pages merely because review is overdue.
5. Preserve review history through export/native restore where supported; recompute current expiry/content compatibility. Restoring an old reviewed revision must not pretend that a new review occurred.

**Acceptance**

- **G09-A1:** Review a page, edit its covered content and pass its review deadline. Status and queue update accurately without altering the underlying knowledge.
- **G09-A2:** Search/cite reviewed and stale pages. Freshness is visible and never presented as machine-verified truth or used to override current contradictory evidence.
- **G09-A3:** Restore an earlier page/workspace revision. Review history remains attributable, elapsed deadlines are honored and stale approved memory remains excluded.

### G10 — Record layouts and reviewed dependency rescheduling

**Base:** PRD §§16.3–16.4; TRD §24; PARITY-003/004; D-P07. These are two independently deliverable child requirements.

**G10-LAYOUT required behavior**

1. Define a reusable record-page layout for a collection using stable property IDs: ordered/grouped property sections, visibility and supported header/sidebar placement. Allow explicit per-record overrides where exposed and a reset to the collection default.
2. Keep layout/presentation state separate from property values and page content. Rename, schema changes, linked views, template creation and property removal must leave understandable, recoverable layouts.
3. Persist layouts with history/native backup. Reset, hide or reorder must not delete a property or its values. A template's initial content remains distinct from the layout used to display a record.

**G10-LAYOUT acceptance**

- **G10-LAYOUT-A1:** Reuse a layout across records, rename a property, create an override and reset it. Values and stable references survive restart, undo and restore.
- **G10-LAYOUT-A2:** Remove/change a schema property and open older records. The UI reports or safely omits invalid layout references without losing retained data.

**G10-SCHEDULE required behavior**

1. Provide an explicit reschedule operation; displaying a dependency or dragging one record shall not silently apply an unreviewed cascade.
2. Preview the complete affected record/date set, before/after dates and reasons. Preserve duration under declared semantics and report missing dates, cycles, contradictory constraints and unsupported dependency types.
3. Specify supported dependency types, gaps, date-only/timed values, timezone, weekends/workdays and calendar policy in the versioned scheduling contract. The proposed first implementation is finish-to-start; broader optimization/resource leveling is not implied.
4. Apply only the reviewed plan with revision checks in an atomic batch; reject stale plans and support grouped undo. External calendar changes, if requested, remain separate connector proposals with their own review and outcomes.

**G10-SCHEDULE acceptance**

- **G10-SCHEDULE-A1:** Move a predecessor in a chain and a branched graph. Preview every affected date before writing; cancellation changes nothing and grouped undo restores the applied batch.
- **G10-SCHEDULE-A2:** Exercise cycle, missing date, weekend/DST and concurrent-edit cases. The declared calendar/constraint policy determines the result; stale or invalid plans cannot partly apply.

## 7. Phone capture — retained deferred scope

### G11 — Phone capture and continuity decision

**Base:** PRD §§18.2, 20.2; TRD §32.2; D-P02.

Desktop completion remains the immediate focus. This addendum retains the identified phone-capture need but does not override the existing user decision to defer mobile implementation. It also does not treat remote access to a P2 headless host as offline multi-device synchronization.

After desktop completion, evaluate a realistic phone-to-desktop capture/retrieval day and record whether a small companion inbox, explicit transfer workflow or full client is necessary. If selected later, specify durable acknowledgement, duplicate prevention, source metadata, conflict/deletion behavior, permissions and disconnected operation before committing delivery. Do not synchronize a live workspace SQLite file through a generic file-sync service.

**G11-A1:** Record the audience/workflow findings, accepted next scope and unresolved limitations in `PROGRESS.md`. Until a mobile scope decision changes, release messaging shall state the tested desktop/platform boundary. This is decision acceptance, not a mobile implementation pass.

## 8. Connected-app workflows — separate integration milestone

### G12 / CONN-00 — Shared connector framework

**Base:** PRD §§7, 10, 16.7, 18.1; TRD §§8, 13, 28; D-P04–05.

**Required behavior**

1. Connect explicit accounts and selected resources through supported authorization. Keep tokens in native credential storage, with account identity, scopes, selection, last refresh and disconnect controls visible. Provider authentication details must be rechecked during implementation.
2. Support bounded read/refresh while the app runs, scoped external search and safe authenticated previews for supported content. Index/copy only selected authorized resources and retain source identity, version, capture time and freshness labels.
3. Distinguish a live external result, cached snapshot and user-owned canonical import. Offline, stale, denied, revoked, deleted and incompletely enumerated sources are separate states. Lost access must not silently delete a user's imported copy.
4. Recheck current access before connector-backed retrieval and AI use. Retained imported copies follow explicit local ownership/retention rules; revocation must not be bypassed by silently substituting an unauthorized cached result. External-model sharing requires separate approved context.
5. Preview each external write's account, target, exact changes, recipients and notification/destructive effects. Recheck permission and remote revision; keep a durable proposal/outbox/receipt and reconcile ambiguous outcomes. A timeout after sending is not proof of failure and must not cause a blind duplicate retry.
6. Handle pagination, token rotation, rate limits, cancellation and bounded resumption. Restored connections, credentials and pending external effects stay inert until reauthorized/reviewed.
7. Keep inbound Trove MCP, outbound MCP, migration, external search, refresh, rich previews and writes separately described. No automatic bidirectional editing or background closed-app connector execution is added.

### Selected adapters

| ID | Required read/refresh scope | Required reviewed-write scope and boundary |
| --- | --- | --- |
| CONN-01 — Google Calendar | Selected calendars, events, recurrence/exceptions, timezones and cancellations. A database calendar remains a different view. | Create/update/cancel events with attendees and notification effects shown before execution. |
| CONN-02 — Google Drive | Individually selected files and supported export/download formats; selected-file authorization/picker flow. | Upload exports/new files or explicitly replace supported selected files. No unrestricted Drive scan or generic structural Docs/Sheets editing promise. |
| CONN-03 — Gmail | Selected labels, queries or thread sets and explicitly acquired attachments. | Compose locally and explicitly review send/reply, recipients, attachments and threading. Gmail-derived external-AI context remains disabled by default until separately qualified. |
| CONN-04 — GitHub | Selected installed repositories, issues, PRs, comments and documentation. | Reviewed issue/comment updates and PR creation from existing branches. No code push, merge, workflow dispatch or administration. |
| CONN-05 — Notion | Selected shared roots, faithful capture/import and connected context through supported interfaces. | Reviewed child-page, block and property changes. The desktop binary must not embed a confidential OAuth secret. Existing import remains usable after disconnect. |
| CONN-06 — Outbound MCP | Explicit trusted HTTPS server and selected resources/tools with supported negotiated protocol and resource-bound authorization. | Review the exact chosen tool, arguments and effects. Remote descriptions/annotations do not establish trust or override Trove permissions. This is separate from existing inbound MCP. |

**Acceptance — run separately for each adapter**

- **G12-A1:** Use a real authorized account/server to select a bounded source set, refresh/search/preview supported content and disconnect. Unselected resources are unavailable and cached results disclose freshness.
- **G12-A2:** Switch accounts, revoke access, interrupt pagination, rotate credentials and hit rate limits. Resume cannot widen scope or mistake denied/partial data for deletion.
- **G12-A3:** Preview and execute a permitted write, then exercise stale remote state and timeout-after-send. The receipt records the actual or unknown outcome; reconciliation prevents duplicate effects.
- **G12-A4:** Restore/import connector state and attempt AI retrieval. No credential, activation, ambient context grant or pending external write is silently restored.

Each adapter remains incomplete until its applicable live flow, failure paths and release prerequisites pass. Required app registrations, provider policies and relevant Google/Gmail verification must be recorded as prerequisites; mock HTTP fixtures alone do not prove account compatibility. Microsoft 365 and additional adapters are not selected by this addendum.

## 9. Recovery under failure applies across the additions

This requirement expands G03/G05 and existing CORE-010 / REL-005–006; it is not a competing recovery design.

| Failure | Required user-visible and data outcome |
| --- | --- |
| Disk full during recording, backup, import or model acquisition | Report the failed operation and retained state; stop or pause safely; no hidden pruning, broken activation or fabricated success. |
| Interrupted backup or restore | Preserve the original workspace and last valid recovery points; distinguish incomplete archives; activate only a fully validated staged destination. |
| Crash or cancellation during AI/autofill/rescheduling | Preserve approved committed work and unapplied proposals; safe retry cannot duplicate effects or overwrite newer revisions. |
| Transcript/source correction | Preserve history and stable evidence where valid; mark dependent generated results stale or require review where anchors no longer support them. |
| Retention/cleanup | Preview affected data and dependent history/evidence; never promise retroactive erasure from independent backups or previously shared external copies. |
| Clean-install restoration | Restore on an independent target with declared compatibility; verify content/assets/history and leave external activation inert. Development-host-only tests are supplementary. |

Low-disk thresholds, size limits and temporary-storage ceilings shall be selected in the corresponding engineering contracts before qualification. This document does not invent numerical limits or new hardware requirements.

## 10. Implementation handoff and acceptance traceability

The following are required work products before an added feature can be called complete:

1. Reconcile the actual branch/source against the accepted scope and current `PROGRESS.md`; preserve user changes and existing implementation. Review source integration separately from feature design.
2. Specify changes to domain/service schemas, migrations, UI, permissions, background lifecycle and portable data. Preserve D001–D003 and the single-writer model.
3. Add versioned mappings from each G/CONN requirement and its acceptance scenarios to the applicable machine-readable contracts, requirements overlay and qualification policy. Do not renumber existing R01–R08/A/legacy IDs or fabricate entries from the missing 307-behavior report.
4. Add independent fixtures and meaningful affected regressions; distinguish service, browser, native, real-account, real-model and clean-install evidence. Update the technical specifications and generated contracts alongside implementation changes.
5. Run applicable acceptance on the actual candidate/platform/tier. Record skipped, failed, blocked and unsupported cases honestly. Preserve frozen historical candidate evidence; changed binaries or policies require their own evidence.
6. Update `PROGRESS.md` with source identity, actual results, limitations and next action. Feature implementation, verification and release approval remain separate states.

| Scope | Existing verification families / delivery links |
| --- | --- |
| G01 | V01–V03; PARITY-006/007; REL-004 |
| G02 | V01; task projection/native editor acceptance; REL-002 |
| G03 | V06, V08; CORE-010; REL-005/006 |
| G04 | V01–V10 as applicable; EVAL-01–07; REL-007/008 |
| G05 | V06, V08–V09; CORE-005; REL-003/004; EVAL speech/memory cases |
| G06 | V07–V08 plus new native reminder/background lifecycle cases |
| G07 | V09–V10; AI-001; EXT-005; versioned skill scope/cancel/retry cases |
| G08 | V05, V09–V10; AI-001; schema/stale-write/undo cases |
| G09 | V06, V09; history/search; EVAL-04 memory eligibility |
| G10-LAYOUT / G10-SCHEDULE | V01, V04–V05, V10; PARITY-003/004; layout and date-plan cases |
| G11 | Recorded scope decision and support-claim review only while deferred |
| G12 / CONN-00–06 | V02–V03, V08–V10 plus adapter-specific live authorization/read/write/revocation cases |

These links are a specification handoff, not completed machine mappings or executed qualification. The next engineering action is to create the affected versioned contracts and acceptance mappings in dependency order, starting with core correctness, backup/meeting behavior and existing source alignment.

## 11. Reference and scope notes

- [Existing PRD](../Trove_PRD.docx) and [TRD](../Trove_TRD.docx): v3.1 baseline.
- [Current decisions, workstreams and research evidence](../PROGRESS.md): authoritative current progress, including D-P01–D-P10, G01–G12 and CONN-00–06.
- [Accepted D001/D002/D003](phase-0/decisions.md): task authority, re-import merge and project-memory scope.
- [Requirements overlay](completion/requirements-overlay.json) and [qualification policy](completion/qualification-policy.json): supporting machine-readable inputs to update during implementation, preserving historical evidence.
- Research basis: the annotated October 12 Notion review, covering 39 feature labels and 12 recommendations. It does not replace the separately missing original 307-behavior source.
- Positioning shall describe tested local storage, local AI/transcription, portable recovery and no required product account. “Offline editing” alone is not the claimed distinction, and this addendum does not assert complete Notion parity.
