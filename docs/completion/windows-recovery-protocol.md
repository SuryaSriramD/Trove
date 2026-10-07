# Recovery publication foundation (C7 source package)

This package implements a reusable positive-completion protocol and connects it to ordinary backup restoration and mandatory workspace startup admission. It does **not** qualify or enable Windows recovery operations. Windows creation, migration, backup, restore, interrupted recovery, and owner transfer still return the existing unsupported-durability error. `SyncDirectory` has not been weakened. Already-compatible Windows workspaces without new recovery metadata retain their existing normal-edit policy.

## Protocol and authority

1. **Begin:** exclusively reserve a new private destination, acquire its owner lock, and publish a canonical intent containing the operation ID, source manifest digest/revision, and each payload path, size and SHA-256. No destination is adopted or overwritten.
2. **Stage:** create bounded, exclusive staged files under the pinned root. Read exactly the declared bytes, hash them, flush, and independently verify the retained bytes. Short, long, corrupt and cancelled streams cannot become verified work.
3. **Verify:** recheck every staged object and publish an operation/intent/root-identity-bound verification receipt.
4. **Commit:** move each verified file with OS-enforced no-replace semantics. A resumed object must have its exact target or its exact staged source, never both. Recheck the complete inventory, reject unknown payload and staged objects, flush the directories, and only then publish the positive completion receipt. Repeated commit validates existing completion instead of overwriting it.
5. **Recover:** explicit native/internal recovery may resume an already-verified partial publication. Startup never silently resumes a partial operation. Failures before verification, ambiguous sources/targets, incomplete record writes and absent completion remain blocked.
6. **Activate:** public workspace startup reads the operation binding from the checkpointed database using immutable SQLite, validates receipts and the exact baseline, performs immutable compatibility checks, then publishes activation immediately before opening SQLite for writes. Read-only reviewed rollback cannot activate a pending baseline. Later ordinary edits retain valid activation and are not incorrectly compared to the original database hash.

Receipts bind the operation, reviewed source manifest, exact payload and destination filesystem identity. Losing every sidecar still fails admission because the checkpointed database carries the operation ID. Copied receipts in another directory, replayed operation IDs, changed manifests and substituted bytes do not grant admission. An owner-transfer move uses a separately guarded, same-volume, no-replace **whole-directory** relocation that retains filesystem identity. Its provenance file is included in the verified payload before completion.

The protocol retains descriptor ownership checks. POSIX descendant directory creation and payload/record publication are relative to pinned directories, using Darwin `renameatx_np(RENAME_EXCL)` or Linux `renameat2(RENAME_NOREPLACE)`. An injected descendant symlink cannot create directories or move staged originals outside the root. Windows uses the existing fixed-local-NTFS, private ACL, protected owner-handle and locked-parent publication checks; it still stops before a directory durability claim.

## Portable backups and older workspaces

A backup of an activated workspace contains its database binding but not its old directory's admission sidecars. After the reviewed backup object has been copied and its exact manifest hash rechecked under the private copy's owner lock, restore clears only that inherited publication binding. It preserves canonical identity, schema compatibility and historical migration metadata. Ordinary restore then allocates a fresh operation and publishes a new exact baseline. Compatible rollback and pre-migration snapshot restore retain their existing publication gates; their verified copies also discard only the obsolete admission binding. A pre-migration snapshot's copied database can therefore differ in this service-owned metadata field; its source snapshot remains byte-for-byte unchanged.

No public Open bypass or renderer-controlled flag was added. Every public Open/Create path performs admission. Pre-protocol compatible workspaces with no binding or protocol sidecars retain their earlier behavior, including ordinary edits. Future schema versions fail before an activation receipt, WAL or SHM is created. Restore destinations inside the backup, including symlink aliases, are rejected before staging can change the source tree.

## Verification and remaining qualification

Final host validation: **370 top-level tests passed, 11 skipped** (707 passing test nodes including subtests), with the race detector and `sqlite_fts5`; both affected packages passed. Windows x64 and Linux x64 platformfs test binaries compiled successfully but were not executed. Evidence and exact source hashes are recorded in `evidence/completion/windows-recovery/manifest.json`. The host suite includes independent byte/content expectations, simulated process-stop boundaries, explicit owner reacquisition, tampered/absent receipts, unverified WAL, lost owner names, unknown staged/payload files, cancelled streams, preserved original backup bytes, backup-of-restored-workspace portability, reviewed startup, unsupported versions and owner-transfer provenance. These tests simulate interruption at declared boundaries; they do not simulate hardware write reordering or sudden power loss.

`TestWindowsRecoveryPublicGatesDoNotReserveOrModify` is a target test for the disabled public surface. `TestWindowsRecoveryNativePrivateStagingRemainsUnqualified` requires `TROVE_WINDOWS_RECOVERY_NATIVE=1` on a Windows 11 x64 fixed local NTFS target; it exercises private staging, ownership and exact-byte checks but deliberately expects commit to stop at the unsupported directory barrier. Compiling that test is not target execution.

Still required before Windows recovery can be enabled:

- A reviewed native Windows durability design and evidence for ordered intent, verified bytes, completion and activation under process kill and power-loss/reorder conditions.
- Native NTFS ACL/owner-handle/no-overwrite tests, interruption/cancellation and lost-directory-entry cases on the exact candidate.
- Integration of the qualified protocol into every currently gated producer (new/create/migrate/backup/restore/transfer) and its target startup/recovery paths.
- Windows-specific clean installation, update/rollback, backup portability and the applicable acceptance matrix. macOS host tests and cross-compiles do not close these requirements.
