# P0 release provenance and recovery procedure

This record covers the macOS Apple Silicon P0 candidate. It does not depend on P1 provider integrations, external accounts, a database server, or developer runtimes on the recipient's machine. Build tools are developer dependencies. Model packs are separate optional local files.

## Reproducible inventory

Run `python3 scripts/record-release-provenance.py` on the build host after the final source changes. The script is offline; absent local dependencies are an error rather than permission to resolve floating versions. Its outputs are:

- `evidence/phase3/provenance.json`: the actual macOS production Go dependency closure, the installed frontend/build dependency superset, native source revisions/archive hashes, installed model file hashes, and migration-source hashes.
- `evidence/phase3/THIRD-PARTY-NOTICES.txt` and `evidence/phase3/notices/`: retained upstream attribution and license texts. These files belong with the distributed application and its release archive.
- `docs/phase-3/licenses/`: pinned upstream model cards, model license texts, sqlite-vec runtime MIT license, and the missing binding-license investigation record.

`go.sum` supplies Go module content and module-file hashes. `frontend/pnpm-lock.yaml` supplies package archive integrity digests. The inventory preserves both exact versions and those hashes; it does not equate a package manifest's license label with review of every embedded component. Frontend notices deliberately include build dependencies. The build toolchain's Go license is also retained. Native workers use pinned source archives and embedded Metal code; system frameworks are OS dependencies, not redistributed libraries.

The application's final executable/resource hashes, signature status, source-stability result, and distribution archive hash must come from the final build/release evidence. This provenance document does not substitute a source hash for an executable hash. Regenerate the inventory and final artifact hashes after any rebuild or code-signing change.

## Optional model packs

All installed files are checked against the complete SHA-256 and size in `native/models/catalog.json`. The inventory independently reads and hashes the local pack files. A missing pack is recorded as absent; it does not disable editing, import, or keyword search. Model licenses and source revisions are retained independently of runtime source licenses.

| Pack | Role | Bytes | Declared license | Runtime |
| --- | --- | ---: | --- | --- |
| TinyStories 15M Q4 | Runtime qualification only | 19,077,344 | MIT | llama.cpp |
| BGE small English F16, 384 dimensions | Embedding | 67,308,128 | MIT | llama.cpp |
| Whisper tiny English | Speech | 77,704,715 | Apache-2.0 | whisper.cpp |
| Qwen2.5 0.5B Instruct Q5 | Compact assistant | 522,186,592 | Apache-2.0 | llama.cpp |
| Qwen2.5 1.5B Instruct Q5 | Assistant qualification | 1,285,494,304 | Apache-2.0 | llama.cpp |

The pack catalog binds architecture, format, runtime commit, role, memory estimate, context length, and embedding dimensions as applicable. The exact hashes and pinned license URLs are machine-readable in the catalog and provenance inventory. Qwen 1.5B, BGE, and Whisper together require **1,430,507,147 bytes** for weights alone; application resources, transfer staging, runtime memory, workspace data, and disk reserve are additional. The acquisition service remains the authority for admission and verified publication. A partially transferred file is not an installed pack.

License evidence is retained at the exact [sqlite-vec runtime commit](https://raw.githubusercontent.com/asg017/sqlite-vec/639fca5739fe056fdc98f3d539c4cd79328d7dc7/LICENSE-MIT) and each model publisher revision listed in `licenses/sources.json`. The cached `sqlite-vec-go-bindings` v0.1.6 module and [pinned binding repository tree](https://api.github.com/repos/asg017/sqlite-vec-go-bindings/git/trees/529a0138d40e50087db7043e361601f91d711e81) lack an explicit license. The P0 product therefore removes that dependency. `internal/sqlitevec` contains the byte-identical pinned MIT C runtime, its header generated from the verified upstream template, and a small original Trove registration adapter. `internal/sqlitevec/upstream.json` records the source hashes, URLs, license, and header substitutions. Historical baseline probes retain their old module but are not part of the shipped executable. Registration and real `vec0` insertion/update/deletion/transaction rollback/reopen stress tests cover the replacement.

## Persisted formats and migrations

| Format/marker | Current behavior | Source |
| --- | --- | --- |
| Domain snapshot | Format 1.0.0; shared registries retain representations | `contracts/`, `schemas/workspace.sql` |
| Service contract | Schema release 1.2.0; numeric transport version 1 | `contracts/service.schema.json` |
| SQLite `user_version` | 2; values greater than 2 are refused | `internal/workspace/kernel.go` |
| Initial canonical/service schema | New workspace applies `workspace.sql`, `services.sql`, and kernel metadata in one transaction | `schemas/`, `applySchemaScripts` |
| Version 1 → 2 | `VACUUM INTO pre-migration-2-<id>.db` before the schema transaction | `internal/workspace/kernel.go` |
| Service runtime migrations | 1: cancellation, authorization, source/unit receipts and destinations; 2: archived timestamp; 20: source-aware memory links | `job_commits.go`, `evidence_lifecycle.go` |
| Recovery metadata | `kernel_meta.recoverySchemaVersion=1`; unsupported versions refused | `internal/workspace/files.go` |
| Additive initializers | Import reconciliation, search/extraction/vector state, AI runs/proposals, meeting drafts, recording directories | `internal/workspace/` initializer sources, hashed in inventory |
| Native backup | Format 2 includes recording journal objects; reader accepts formats 1 and 2 with explicit validation | `backup.go`, `restore.go` |

The workspace owner holds an exclusive file lock before migration. Canonical schema migration scripts run in a single transaction. Additional runtime initializers use individual transactions; the entire startup sequence is not claimed to be one global transaction. Reopening retries idempotent initialization and recovers interrupted jobs. Search/vector state is derived and may be rebuilt; canonical pages, attachment references, history, and recordings are authoritative.

The automatic `pre-migration-2` file is a database snapshot, **not a full workspace backup**. It does not separately capture attachment files or recording journals. Operators must retain a complete native backup for release rollback. No destructive down-migration is implemented, and opening a workspace with an older candidate is not a supported substitute for restoring a compatible backup.

## Backup, upgrade, and rollback procedure

1. Before replacing an installed candidate, stop active recording cleanly and create a native workspace backup to a new directory. Wait for the backup to finish. Preserve its `manifest.json`, database, assets, and recording objects together. A directory with `INCOMPLETE` is not a recoverable completed backup.
2. Record the backup identity/event cursor and retain the previous application archive with its executable/archive hashes. Keep installed model packs and their exact catalog manifests available separately; native workspace backup does not bundle model weights.
3. Install the new application, open the workspace, and confirm pages/databases, recording sources, keyword search, and memory state. Verify that installed pack IDs are recognized before testing local AI/transcription. Preserve the complete pre-upgrade backup until the candidate passes the release journey.
4. On failure, quit the application. Preserve the failing workspace intact for diagnosis and recovery. Reinstall the previously verified application. Restore its compatible pre-upgrade native backup into a **new, nonexistent destination** using the backup restore flow. Keep the failed workspace and original backup until restored content is checked.
5. Restore verifies declared sizes and hashes, rejects unsafe paths and unsupported formats, and copies into a staging directory. It opens the staged database, revokes old approvals/selection handles, rebuilds derived search, checkpoints WAL, writes a restore receipt, and publishes only with the durable `INCOMPLETE` marker removed. Existing workspace directories are never overwritten.
6. Open the restored workspace and verify the backup event cursor/content, attachment access, recording retention, keyword search, and memory behavior. Re-enable/rebuild semantic indexing with an installed compatible pack as needed. Retry interrupted jobs explicitly when their source or authorization requires review.

Do not move only `workspace.db` while the application is running: committed content can still be in its WAL, and recording journals/assets are separate files. Do not delete the failing workspace as part of rollback. For this first release there is no prior generally released version; backward compatibility is limited to tested candidate/format combinations recorded by qualification evidence.

The automated migration rollback, restart, tampered-backup rejection, recording preservation, and restore tests validate implementation paths. The final packaged restore journey is a separate gate. A verified developer-host backup test does not establish installation or recovery on an independent clean Mac.
