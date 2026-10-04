# Recovery completion work

## Current-content export (FIX-001)

Markdown/CSV and HTML exports select attachments from exported live pages,
including covers, file properties, recordings and saved inert template/action
content. Automatic import-root inventory attachments are not current content.
Full import archives, manifests, multi-item CSV/API responses and unrelated
assets are omitted. The result and manifest explicitly describe this policy.

Live-item provenance is retained where it can be isolated safely. Multi-item
conversation JSON is split into export-only source objects containing every
supplied field for the exported conversation; the original managed JSON is
unchanged. Recognized portable manifests are imported as inert evidence. Legacy
automatically created manifest pages are excluded from current-content exports.
Native backups continue to retain all original source and historical assets.

## Meeting history (FIX-002)

Transcript authorization follows the owning meeting page. Restore recreates
guarded segment identities before their scope recheck, while preserving timing,
speaker, recording and block references. Revision conflicts reject the entire
operation. Existing, edited and deleted segment paths have regression coverage.

## Online native backup (CORE-010)

The SQLite online backup API copies a read transaction fixed while the workspace
writer is briefly serialized. The same boundary pins the canonical asset set.
Copying proceeds in 128-page steps, yielding for 1 ms between unfinished steps;
the workspace writer lock is released before the first copy step. Cancellation
is checked between steps. The read transaction closes immediately after copying
the database, so subsequent asset copying does not prolong WAL retention.

Recording journals and open source descriptors are captured under the recording
mutex through that database boundary. Large audio copies and hashing happen
after releasing it. An active recording exports only its last acknowledged
checkpoint, with an interrupted journal in the backup; live capture is unchanged.
Interrupted/stopped drafts retain their complete raw file, including an
unjournaled crash tail. Open descriptors keep these captured sources available
if live finalization later removes their paths. Beginning a new recording uses
the same boundary lock, preventing a half-created journal/source pair.

Database/asset digests, recording segment hashes, incomplete markers, pin
cleanup, staged restoration and source-preservation checks remain mandatory.
Existing version 1/2 backup formats remain readable; this implementation change
does not invalidate older valid backups.

### Qualification

`TestOnlineBackupAllowsEditsAndCaptureWithConsistentRecovery` pauses copying a
4 MiB database after its first bounded step. Editor commit and recording
checkpoint/finalization must each complete within 2 seconds **while copying is
still paused**. This is a deterministic lock-regression bound, not a measured
large-library or device-level service guarantee. The local run observed about
0.8 ms for the editor commit and 37 ms for checkpoint plus finalization.

The test restores the result into a second installation and verifies the old
title/asset set and acknowledged recording prefix, while the original workspace
retains its newer edit and finalized recording. Cancellation and injected
`ENOSPC` exercise failure publication and pin cleanup. Existing tests cover
corrupt assets, interrupted backup startup cleanup, stopped-recording crash
tails, unsafe manifests and complete staged restoration. Real low-disk device
qualification and representative large-workspace latency remain release checks.

## Schema-aware historical values (CORE-006)

History preview returns incompatible historical values, their captured property
definitions, the corresponding current definition when available, and eligible
current destinations. Service evidence metadata can describe unknown historical
versions without widening the live property registry. An unresolved preview
cannot be applied. The recovery
panel offers two explicit choices for every incompatible value:

- Retain the old value as inert page provenance while keeping the corresponding
  current typed value, if one exists.
- Map an explicitly reviewed value to an editable current property in the same
  collection. Type, option identity, reference permissions and relation target
  validation use the current schema. Mapping cannot overwrite another historical
  property or merge multiple source values into one destination implicitly.

The second preview binds decisions to the existing history restore command;
the mutation does not accept unchecked client-side mapping data. Stored original
and resolved snapshot hashes, actor/grant binding, expiry, one-use preview state
and revision guards reject stale reviews. A new edit in the review UI disables
restore until the decisions are reviewed again.

Applying creates a new revision. The previous current page/values remain in
history; shared property definitions and unrelated records remain current.
Retained evidence is bounded to 256 values and 1 MiB, and is never executed as
commands or activated as typed properties. Subtree recovery asks for individual
review when a page has incompatible values rather than silently choosing a
mapping for it.

Go tests cover changed, removed and unknown historical schemas, exact retained values, compatible
historical restoration, explicit typed mapping, stale and duplicate reviews,
unknown decisions, target conflicts, option validation, cross-scope files,
unchanged unrelated records, and the public result schema. Browser tests exercise
the actual recovery controls with a deterministic service fixture; backend
semantics are verified separately against real SQLite workspaces.
