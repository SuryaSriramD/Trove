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
