# Local automation configuration — PARITY-010

The Automation panel now creates and edits bounded, owner-authored local rules. Every form save makes a new disabled version. The user can inspect the actual effects, explicitly enable/disable or pause/resume the rule, run a reviewed occurrence, and inspect up to 200 retained recent runs. Failed runs from the current rule version can be reviewed and retried with the original occurrence identity. Older versions remain visible but cannot be executed through a newer definition.

## Supported configuration

| Area | Supported behavior |
| --- | --- |
| Schedule | Daily or weekly, weekday, local hour/minute and explicit IANA timezone. Missing DST times skip; repeated times use the first occurrence, including non-hour transitions. |
| Missed occurrences | Skip and log by default; explicit catch-up of 1–20 oldest occurrences, then a logged skip of the remaining interval. Trove must be running with the workspace open. |
| Events | A chosen collection, entity kind, or all eligible local changes. User changes are eligible; reviewed AI changes require explicit opt-in. Import, restore, and automation origins never cascade. The preview picker shows allowed origins among the latest 200 committed events. |
| Conditions | One selected record/property, `eq`, `ne`, or `is_empty`; values use typed controls and preserve exact decimal strings. The record is explicit, not implicitly substituted from the triggering event. |
| Local actions | Create a page, update its title/archive state, create a database record, set or clear a property, or change an existing canonical task's status. Later actions can fill properties of a record created earlier in the same rule. |
| Typed values | Plain rich text, exact decimal, checkbox, select/status/multi-select, dates with optional end and explicit timezone/offset, URL/email/phone, and relations. Existing unsupported value kinds remain retained when editing; arbitrary object authoring is not exposed. |
| Recurring templates | Select an immutable direct-user capture, title and destination. Copies its nested body, compatible defaults and independent owned tasks through the shared template planner. Captured button actions remain inert. |

`{{occurrence}}` substitutes the intended UTC occurrence time in authored action text, including a copy's title. Captured template body content stays exactly as captured. No raw command JSON, event ID or generated identity must be entered in the UI. Record choices are searchable with 200 visible matches; template choices are capped at 200 and report truncation.

## Template authority and atomicity

A saved `template.instantiate` action contains only `templateId`, immutable SHA-256 `templateHash`, title, and optional destination collection. The saved-rule schema does not accept `template.apply`, temporary preview IDs, approval IDs or arbitrary mutation kinds. Only the local user with owner scope can save, configure and run rules. This does not make AI-generated changes autonomous: model proposals still require their separate reviewed approval path.

Every template occurrence builds a new plan using the canonical capture/default/body/task/attachment validation. New page, record, block, table-cell and layout identities are deterministic for the tuple of rule ID, rule version, occurrence, action index and source identity. Interactive template copies retain their random-ID allocator. The capture hash and all source/destination/reference guards are checked again when the normal `automation.run` batch commits.

All local effects and the successful occurrence receipt commit in one SQLite transaction. A failed occurrence leaves no partial page or record graph. Retry after a lost response or reopen observes the retained rule/version/occurrence receipt and does not clone again. Three recorded failures pause the rule. Rule edits retain old evidence and start a new version; editing is not a retry of an old occurrence.

Preview runs the real canonical handlers and commit validators inside a rolled-back savepoint. It persists no content, receipt or committed event. Locked databases, invalid property options, broken references, incompatible captured schemas, stale revisions and changed capture hashes fail explicitly. A failed or refreshed preview cannot leave an earlier UI preview active.

Recurring templates deliberately do not persist cross-schema mappings. If captured property identities, types, versions or configurations no longer match the destination, the occurrence reports a conflict and the user must capture a compatible template. Defaults are not silently dropped or coerced. This bounded behavior is separate from the interactive template library's explicit one-time property mapping.

## Bounds and verification

Rules remain limited to 32 authored actions and 256 KiB. Expanded plans are limited to 1,200 canonical commands, 4 MiB and 1,000 revision guards, including combined template references. Individual template bounds remain 400 blocks, 64 owned tasks and 2 MiB per capture. The existing runtime scans at most 1,000 rules per tick and 200 events per rule per tick; this work does not add an always-on service or unrestricted external-vendor automation compatibility.

Backend race tests cover existing DST, non-hour DST, origin exclusions, missed/catch-up behavior, atomic rollback, failure pausing and retries, plus deterministic template preview/reopen identities, table-cell remapping, preserved defaults, capture hashes, incompatible schemas, database locks, owner/scope/AI restrictions and real typed-value preview validation. Browser fixtures exercise weekly authoring, precise decimals, task/property actions, immutable template selection, explicit enabling, reviewed execution, pause, editing, recent-event selection and conflict reporting. These verify implementation behavior; packaged desktop/platform release qualification remains a separate gate.
