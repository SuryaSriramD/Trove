# P0 desktop release candidate

Trove 0.3.0 is a self-contained macOS 27 Apple Silicon application. The candidate
uses an ad-hoc signature. Public Developer ID signing/notarization and a second
clean Mac remain separate distribution checks. The qualification report records
the exact candidate hash and distinguishes automated, interactive, and unverified
acceptance; a successful build alone does not complete the release gate.

## Install and start

Expand the release ZIP and move `Trove.app` into a local Applications folder, then
open it. No product account, Node, Python, Ollama, Docker, or database server is
needed. Development tools listed in the repository README are only for building
or testing from source. This candidate supports macOS 27 and Apple Silicon; other
operating systems and Intel Macs are not qualified.

The first launch creates a local workspace under
`~/Library/Application Support/Trove/workspaces/default`. Settings can create or
open another workspace. A successful native selection is remembered for restart.
If its drive is missing, reconnect it or choose a different workspace; Trove does
not silently replace it with an empty workspace.

## Configure local models

Pages, databases, Notion import and keyword search work with no model weights.
In Settings → Local models, download a pack explicitly or choose Import file to
verify an existing offline copy. Only the exact catalogue files are accepted.
Keep these optional model files separately when transferring to an offline Mac.

| Capability | Pack | Weight size |
| --- | --- | ---: |
| Local assistant and reviewed meeting notes | Qwen2.5 1.5B Instruct Q5 | 1.29 GB |
| Smaller assistant | Qwen2.5 0.5B Instruct Q5 | 522 MB |
| Semantic/hybrid retrieval | BGE small English F16 | 67 MB |
| English transcription | Whisper tiny English | 78 MB |

The 1.5B assistant, embedding and speech packs total 1.43 GB, excluding the app,
workspace, memory and transfer staging. Packs are pinned by size, SHA-256,
architecture and native runtime. Downloads show progress and can resume;
interrupted local imports can be retried by choosing the file again. Invalid
files cannot become installed packs. Missing packs produce explicit errors;
there is no cloud inference fallback.

## Run the P0 journey

1. Import a Notion Markdown/CSV ZIP through Import. Review item counts, inferred
   CSV property types, omissions and conflict choices before committing. Reuse
   the source namespace for a re-import. Progress and committed units survive
   interruption. Original source files remain available in the import report.
2. Open the imported pages and databases, edit them, and confirm Saved. Keyword
   Search works immediately without a model. Semantic retrieval is an optional
   local setting that requires the embedding pack and a completed index.
3. In AI, select an installed instruction model and ask about a page or workspace
   sources. Review source links and any proposed changes before approving them.
4. In Meetings, name a recording and choose Record microphone. Respond to the
   macOS microphone prompt, then confirm that the saved byte count increases.
   Stop and save the meeting. Choose the speech pack and transcribe locally;
   timestamped text is committed progressively. Imported audio supports 16 kHz
   mono WAV or raw float32 PCM; unsupported formats are retained with a visible
   processing error.
5. In AI → Approved memory, propose an enduring preference or evidence-linked
   fact, review it, and approve it explicitly. Disable or forget it when needed.
   Changed or removed sources invalidate evidence-backed memory. Current task
   and database state remains authoritative.
6. Quit and reopen Trove. Confirm the workspace, committed edits and recordings.
   A recording interrupted by a crash appears for recovery; committed audio
   remains available even when transcription has not completed.
7. In Settings → History and recovery, create a native backup in a separate
   location. Choose a completed backup to review, then restore into a new folder.
   Open that restored workspace and confirm pages, records, audio, memory state
   and search. Model weights are kept separately from workspace backups.

## Qualification and rollback

The benchmark definitions and exact measurements are in [performance.md](performance.md).
Dependency/model hashes, licenses, migrations and the complete backup/rollback
procedure are in [release-provenance.md](release-provenance.md). Retain the release
ZIP and its SHA-256 together with a verified native backup before upgrading.
Rollback restores a compatible backup into a new directory; it never overwrites
the failing workspace or attempts an in-place schema downgrade.

The source-bound automated gate is `scripts/verify-phase3.py`. It tests a fresh,
immutable app, uses an empty model cache for the data journey, denies network
access during actual inference and packaged checks, and records every result in
`evidence/phase3/`. Interactive native observations and final artifact hashes
must accompany that report. Fixed model fixtures establish functionality, not
general answer quality or noisy/multilingual meeting accuracy. P1 providers and
advanced parity features are not prerequisites for this release.
