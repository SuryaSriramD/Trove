# Automatic pre-migration recovery snapshots

This package implements the storage portion of REL-006. It does not qualify a signed updater, public distribution, an independent installation, or Windows/Linux behavior. All execution evidence uses disposable synthetic workspaces on the development Mac.

## Compatibility contract

`internal/workspace/compatibility.go` exports `CurrentUserVersion = 3`, `CurrentStorageGeneration = 1`, `CompatibilityDescriptor`, `CurrentCompatibility()`, `(*Kernel).DescribeCompatibility(ctx)`, and `ReadWorkspaceCompatibility(ctx, workspacePath)`. The read-only inspector does not create a workspace, execute initializers, obtain an owner grant, or remove incomplete markers. Unsupported future formats return an error with the descriptor fields read so far.

`cmd/trove-format` emits the current descriptor as JSON for signed package metadata. All canonical schema changes, including additive migrations in registered package initializers, must advance `CurrentStorageGeneration`. Generation values are monotonic. Version 3 establishes the fence against pre-framework binaries, which only accept user versions through 2. Future framework binaries must keep the generation check before running any initializer.

Existing user-version 1 and 2 workspaces have generation 0. They receive a full snapshot before the framework upgrade. New workspaces have no old user content to snapshot; an incomplete-creation marker protects their initialization. Ordinary opening of a current workspace does not repeat the snapshot or rehash a historical snapshot.

## Durable ordering

The single-kernel ownership lock is held throughout the upgrade; jobs and capture services have not started. Recovery state is re-read after obtaining ownership.

1. Inspect the existing database read-only. Refuse a future user version or storage generation before writable SQLite configuration or package initialization.
2. Create `.migration-snapshots/<snapshotId>` with its own `INCOMPLETE` marker. Use SQLite's incremental online backup API from a pinned read transaction.
3. Copy every regular file below `files/` and `recordings/`, including assets retained only for history and original unfinished audio/journals. Do not normalize recording state in this raw restore point. Do not recurse into snapshot directories, caches, model packs, provider credential stores, or neighboring workspaces.
4. Write the manifest, verify every size and SHA-256, verify the database's format/workspace identity/integrity/foreign keys, sync the files and directories, and remove only the snapshot's own marker. Sync the snapshot parent and workspace directory.
5. Atomically publish and sync `migration-journal.json`, binding the snapshot ID, manifest digest, and before/target descriptors. Then exclusively create and sync the workspace's migration `INCOMPLETE` marker. No canonical initializer executes before these steps.
6. Run the canonical and additive initializers. In one transaction, stamp user version 3, storage generation 1, the migration ID, and manifest digest. SQLite connections use `synchronous=FULL`; checkpoint and sync the database before declaring completion.
7. Re-verify the snapshot and resulting database, durably mark the journal complete, and remove only the matching migration marker. Retain the snapshot and complete journal.

A failure before the journal is published leaves the original format untouched and the failed snapshot unqualified. A pending journal fences normal opening even if the process stopped just before writing the workspace marker. A failure during initialization preserves the snapshot and leaves the workspace fenced. It is never snapshotted again as if the partially migrated state were an intact baseline.

If the exact target format, migration ID, and manifest digest were committed before interruption, reopening verifies both the snapshot and current database before finishing journal/marker cleanup. Missing or corrupt snapshots, journal metadata, mismatched completion identities, or unrelated restore markers cannot take this path.

## Recovery APIs

- `ReadMigrationStatus(ctx, workspacePath) (*MigrationStatus, error)` returns `nil, nil` when no migration exists. Otherwise it reports the state, computed local snapshot path, ID, manifest digest, descriptors, completion time, and safe explanation. It verifies the snapshot; a non-nil status can accompany a verification error and must not be presented as an available verified restore point.
- `VerifyMigrationSnapshot(ctx, snapshotDir) (MigrationSnapshotManifest, error)` checks the bounded manifest, exact file inventory, checksums, and database. It rejects incomplete snapshots, links, unsupported objects, duplicate paths, and unlisted files.
- `RestoreMigrationSnapshotReviewed(ctx, snapshotDir, newDestination, expectedManifest)` is the native review boundary. It verifies and compares the exact previously reviewed manifest, then passes that same parsed value directly to copying. A self-consistent replacement after review is rejected before creating a destination.
- `RestoreMigrationSnapshot(ctx, snapshotDir, newDestination) (MigrationSnapshotManifest, error)` only accepts a new destination. It preserves the source database version and raw recording bytes, verifies copied objects, and removes its own destination marker only after durable verification. It does not open or migrate the restored database. The matching old application can open that original format; opening it in a newer application will take a new pre-migration snapshot.

Restoration never overwrites the failed workspace. The native interface must use trusted local paths and preserve the verification error when recovery material is unavailable. The normal portable `BackupManifest` format remains separate: a pre-migration raw snapshot must retain old-format data and unfinished recording journals that a normal backup may normalize.

## Bounds and qualification limits

A snapshot supports up to 100,000 inventoried content files, 64 GiB per database/content object, 256 GiB total, 32 directory levels, and a 32 MiB manifest. Excessive, linked, or non-regular content fails before canonical migration. Enough local space for a full database and retained files is required; a failed copy does not justify starting an upgrade.

Tests inject faults at journal, initialization, completion-stamp, and marker-cleanup boundaries; corrupt recovery data; simulate an additive initializer that commits partial schema/content and then fails; restore legacy versions; and compare raw attachment/audio bytes. These are deterministic process-boundary simulations, not evidence from pulling power on a physical device. Actual candidate installation/update/rollback, real hardware interruption, external backup destinations, independent clean installations, and platform release qualification remain separate requirements. `synchronous=FULL` strengthens write durability and must be reflected in candidate performance measurements.

## Paired application rollback

`RestoreBackupCompatible(ctx, source, destination, expectedManifest, expectedCompatibility)` restores a reviewed normal backup for its paired saved application. Unlike the ordinary restore flow, it never calls `Open`, runs a package initializer, rebuilds search, or advances the storage format. It initially supports the frozen user-version 3/generation 1 cleanup contract; future generations require an explicit adapter.

The function verifies the exact reviewed manifest and source descriptor, copies into a private fenced staging directory, and transactionally revokes eight kinds of installation permission: pending/approved proposals, restore previews, file-ingest handles, external root approvals and cached chunks, job authorizations, backup destinations, and template previews. It checks the exact known permission-table definitions and refuses missing/altered tables, triggers on cleanup tables, or incoming foreign-key cascade/SET actions. Cleanup is bounded to one million permission rows and 10,000 foreign-key relationships. Canonical rows, other derived indexes, assets, and audio retain the source backup's values. `RestoreResult.IndexRebuilt` is false.

After cleanup, the original descriptor and complete database integrity are checked again, files and directories are synced, and the result is published into a newly reserved destination with its own incomplete marker and restore receipt. Both rollback and raw migration restoration reject destinations inside their source backup, including symlink aliases, to keep the source intact. A failed workspace is never overwritten.

Qualification includes an initializer sentinel that would fail if rollback tried to open/migrate storage, canonical/derived table equality, asset and recording checksums, permission revocation, reviewed-manifest mismatch, legacy/future pairing rejection, unsafe schema cases, and nested destination refusal. Signed application verification and the visible updater/recovery flows are integrated separately.

Final evidence is in `evidence/completion/pre-migration-snapshots/manifest.json`. The full workspace race run precedes the final reviewed-restore wrapper; the complete affected migration/compatible-restore race set was rerun after that wrapper and journal digest binding were added. Native startup, workspace selection, and provider-auth route tests also pass. Skipped external/live checks remain explicitly listed in the evidence summary.
