# P0 data journey qualification

The data service gate passes on the development Mac. This evidence covers the
canonical workspace services and a fixture callable inside the packaged Wails
process. It does not establish native dialog, microphone hardware, OS permission,
or model inference acceptance by itself.

## Reproduction and evidence

From the repository root:

```sh
env GOCACHE=/private/tmp/trove-phase0-tools/go-cache \
  GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod CGO_ENABLED=1 \
  /private/tmp/trove-phase0-tools/go/bin/go test -race -tags sqlite_fts5 \
  ./internal/workspace ./internal/integration -count=1 -json \
  > evidence/phase3/data-journey-tests.jsonl
```

The [race test log](../../evidence/phase3/data-journey-tests.jsonl) records all
workspace and public-service integration tests. No product account, external
database, model weights, model runtime, Python, or Node process is involved in
these fixtures. Go is a build/test dependency, not a desktop runtime dependency.
The final local run completed at 2026-09-30 22:16 IST with **90 workspace tests
and one integration test passing**, no failures or skips, under the race
detector. It includes the search optimization and the original registration
adapter around the licensed vendored sqlite-vec runtime.

## Added P0 acceptance coverage

| Acceptance | Evidence |
| --- | --- |
| Actual process crash preserves committed content | `TestP0SIGKILLPreservesCommittedJourneyAndRestores` starts a separate executable, completes Notion import and page/database changes, records forgotten/stale memories, opens an uncommitted SQL update, and leaves the recording file open. The parent sends SIGKILL and reopens the workspace. Committed content survives; the uncommitted title does not. |
| Durable audio survives crash, backup and restore | The same test writes and syncs two seconds of float32 PCM, checkpoints the first second only, kills the process, backs up the interrupted capture, restores it, and finalizes it as a canonical two-second recording with both segments. |
| Hostile ZIP members are explicitly accounted for | `TestP0HostileZIPInventoryIsAccountedAndRetained` covers traversal, absolute/drive/backslash/NUL paths, case collision, symlink, named pipe, corrupt CRC, oversized declared expansion and suspicious compression ratio. Safe content imports; non-UTF-8 content stays unsupported and retained; every rejected member references the unchanged original ZIP. |
| Hostile hierarchy never partially commits | `TestP0ImportHostileHierarchyRollsBackAndMalformedCSVRetainsEvidence` supplies a cyclic portable manifest and verifies rollback of all canonical pages while preserving source evidence. Malformed CSV retains exact source and readable fallback plus successfully parsed preceding rows. |
| Notion identity and stale-review fencing | `TestP0ImportRenameReconcilesIdentityAndRejectsStaleReview` renames a file with the same Notion external ID, binds to the same canonical page, then edits the page after replacement review. Import rejects without changing its content or leaving a partial import root. |
| Complete public-service data journey | `TestP0PublicServiceJourney` invokes the exact `integration.RunP0` scenario used by packaged qualification, closes and reopens the workspace, and verifies native backup/staged restore. |

Existing suites also cover inventory/hierarchy/link reconciliation, explicit
keep-local conflicts, immutable reviewed import jobs, resumable import cursors,
CSV type conversion fallback and portable exports; page/database edits and
foreign-key/atomicity rules; FTS updates/deletion/rebuild/scopes and explicit
keyword fallback when semantic models are unavailable; memory approval,
forgetting during generation, supersession and source invalidation; recording
checkpoint tampering; history/schema conflicts; backup checksum/path tampering,
approval revocation, and full canonical/history/import-evidence restoration.

## Keyword search release fix

The initial 10,000-page benchmark exposed a common-query cost: keyword search
decoded every matching chunk and location before selecting the requested result
count. Search now applies source-kind and page filters in SQL, streams ranked
candidates, and stops when enough authorized hits have been found. When both the
authenticated grant and optional requested scope cover the whole workspace,
SQLite also applies the result limit before returning rows.

Restricted page/project scopes deliberately have no preliminary SQL limit, so a
high-ranking inaccessible match cannot hide a lower-ranking authorized result.
`TestSearchLimitPreservesScopeAndPageFiltering` verifies granted and requested
page scopes, granted and requested project scopes (including membership lookup
while streaming), explicit page selection, and source-kind filtering. Existing
tests retain revision, deletion, memory, exact-location and hybrid checks.
Performance claims belong to the separately recorded hardware/workspace/model
benchmark; this change alone does not assert that a latency target passed.

## Packaged fixture

`internal/integration/p0.go` exposes
`RunP0(ctx, kernel, ownerContext, "p0-create" | "p0-verify", statePath)`.
Create imports a small Notion-shaped ZIP with hierarchy, CSV and rejected path,
edits the imported page and record, performs exact keyword retrieval, approves
then forgets/invalidate memories, and retains a deliberately interrupted PCM
recording. Verify is intended for a separate app launch with the same state file.
It checks persistence, creates a verified native backup, restores to a fresh
directory, compares canonical snapshots, rechecks search/memory, and finalizes
the restored audio tail. Backup and restored workspace folders remain beside the
state file for inspection.

Memory retrieval is exercised through `PrepareAI`, then the fixture cancels that
run without generating an answer. The two-second silent PCM source establishes
storage/recovery behavior; it is not a microphone or transcription accuracy test.
Actual offline generation/transcription require the separate verified native
model fixtures. The release runner must record its packaged invocation results
before claiming that this fixture passed inside Wails.

## Migration and rollback boundaries

Canonical workspace format remains 1; SQLite `user_version` remains 2. The
recorded runtime migrations are 1 (job/cancellation runtime tables), 2 (archive
timestamps), and 20 (memory evidence identity retention across block/segment
deletion). Additive service schemas initialize through transactional kernel
initializers. No new migration is introduced by the P0 data acceptance work.
The migration fault test proves rollback of an interrupted schema transaction.
Opening a version-1 database saves `pre-migration-2-<id>.db` before upgrading;
unsupported future `user_version` values are rejected.

Before upgrading a real workspace, stop active capture and create/verify a
native backup. Restore that backup to an unused directory if rollback is needed;
keep the newer workspace and its source backup unchanged. Use an application
version compatible with that backup's schema. A bare pre-migration database is
not a substitute for the complete backup's managed assets and recordings. There
is no in-place schema downgrade procedure. Page-history restore creates new
revisions and never makes stale memory evidence current again.

## Practical limits

These fixtures use small synthetic workspaces on one Mac and local filesystem
temporary directories. They are correctness checks, not performance benchmarks.
They do not claim maximum-size Notion export responsiveness, physical power-loss
or external-drive disconnection recovery, natural meeting recognition quality,
or installation on a separate clean Mac. Hostile-input cases are deterministic
regressions rather than an exhaustive fuzzing or security audit. P0 requires no
P1 provider or advanced parity implementation for any scenario recorded here.
