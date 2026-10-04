# Unified database and page templates — PARITY-004

The page template library and database template controls now use the same kernel path. A direct user captures an immutable source page at an expected page revision. For a page-backed record, the capture includes editable property defaults, their schema definitions, the page body and owned todo tasks. Selecting it creates one new page-backed record with both the defaults and body.

## Contract and commit boundary

- `editor.saveTemplate` accepts the authoritative `TemplateCaptureRequest`. It remains explicitly restricted to direct user actors/origin and requires `expectedPageRevision`. Existing `editor.template` reads retain their original page/body response shape.
- `template.preview` accepts `TemplatePreviewRequest` with a saved template, title, destination collection and optional typed property decisions. `TemplatePreviewResult` returns conflicts, current property definitions, effect counts, warnings, allocated destination identities, expected revisions and the exact `template.apply` command.
- `template.apply` contains only the preview ID. The kernel stores and binds its resolved actions, all allocated IDs, template content hash, actor, scope grant, policy revision and ten-minute expiry. The ordinary batch approval, idempotency, history, lock, scope and revision machinery applies. A retry of the same successful batch replays its result.
- Preview executes the actual canonical commands and commit validators inside a rolled-back savepoint. Invalid values, references, reciprocal relations, locks or task semantics cannot leave partial canonical content behind. Apply rechecks template identity/hash and every bound guard. An edited source or destination requires a new preview.

`buildTemplatePlan` is the common in-package planner for future recurring consumers. A recurring occurrence must build a fresh plan under its authorized execution context, bind its allocated IDs once and submit it through the normal automation approval/job boundary. A preview ID is temporary, not a reusable schedule definition. This package does not activate recurring template schedules.

## Copy and reference rules

- Every new page, record, block, table cell and layout group receives a fresh ID. Block parents, layout membership, internal synced references and canonical page/block links are remapped together. Rich text marks/colors, toggle state, code, tables, media, nested lists and layout bodies retain their canonical data.
- Each direct todo task is captured and cloned once per application, including its own defaults and body. Repeated occurrences point to that one new task. Separate applications never share those mutable task records. An explicit external synced block remains a reference to its existing source and is permission checked.
- Immutable file identities/bytes are shared through normal attachment references. Source and destination access checks still apply. Preview verifies referenced asset bytes. File/property/reference permissions are rechecked on application.
- Same-database defaults use their captured property identities only while type, version and configuration still match. Cross-database or changed-schema defaults require an explicit mapping and current typed value, or an explicit omission. Duplicate, unknown and conflicting mappings are rejected. Canonical title identity is created from the requested title; derived/system values are recomputed through their normal paths.
- Actions embedded in button blocks become inert readable JSON with a preview warning. A template never silently clones an executable action aimed at the original page. Imported template/action definitions remain retained evidence and are not inserted into the active template library.

Bounds are 400 blocks across the captured body and owned tasks, 64 owned tasks, 256 values per record/destination properties, 2 MiB per capture, 1,200 internal actions and 1,000 revision guards. Previews expire after ten minutes and are limited to 32 active previews per actor. Legacy body-only templates remain usable; a legacy direct todo without an owned-task capture requires recapture so it cannot accidentally share the original task.

## Qualification

Synthetic backend expectations cover captured defaults after source edits; rich nested bodies and internal link/synced remapping; two independent task-bearing copies; repeated todo deduplication; reopen; table/layout identities and dimensions; immutable assets visible from the destination scope; explicit external references and denied hidden references; cross-database typed mapping; duplicate/unknown decisions; changed schemas; stale destination revisions; template hash changes; expiry; collection locks; direct-user capture; and exact AI approval/idempotent replay. Portable import tests assert templates remain inactive.

Chrome tests mount the actual shared `TemplateLibrary` controls. They exercise capture from a shell snapshot without values, combined-effect preview, exact kernel-command submission, explicit typed mapping, changed-title invalidation and rejected stale previews. These fixtures qualify Trove behavior; they do not claim live external-vendor qualification.
