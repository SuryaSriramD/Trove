# Trove completion programme

This is the active C0–C8 implementation record. The original six phases remain historical. All 51 backlog acceptance criteria remain binding; implementation, targeted verification, and platform release qualification are recorded separately. No completion-phase label grants release approval.

Baseline source commit: `632d0cd0141dc2dcdc7922274757e10083623616`. Integration branch: `integration`. Editor, recovery and native/tooling work use separate managed worktrees. Shared schema sources are authoritative; regenerate Go and TypeScript consumers together.

## Phase gates

| Phase | Integration gate |
| --- | --- |
| C0 | Development and verification foundation |
| C1 | Correctness and content preservation |
| C2 | P0 product foundation |
| C3 | macOS P0 qualification |
| C4 | P1 workspace and reviewed local AI |
| C5 | External integrations and macOS P1 qualification |
| C6 | P2 image understanding and personal reviews |
| C7 | Windows 11 x64 release |
| C8 | Ubuntu 24.04 x64 release and final reconciliation |

C1 prevention work precedes P0 qualification. C2 editor, databases, recovery, retrieval and audio can proceed independently after shared interfaces are agreed. P1 and P2 work do not block the P0 candidate. Windows and Linux each require their own candidate and clean-install evidence.

## Current checkpoint

C1 defect repairs and the C2/C4 product source packages are integrated: editor preservation and controls, scaled database queries/views, hybrid retrieval/sqlite-vec, online backup and historical mapping, restricted FFmpeg decoding, rich layouts/templates/formulas/PDFs, Notion API reconstruction, browser clipping, visible automation, and reviewed local-AI/MCP actions. See the package links in the [51-item registry](registry.json); its planned evidence directories are not passing checks.

The [requirements reconciliation](requirements-reconciliation.md) adds a [current coverage overlay](requirements-overlay.json) for all 312 product requirements and explicitly accounts for the 26 contract/boundary rows. It preserves the original ledger statuses and reports scoped implementation coverage, not blanket completion.

The optional 7B pack passes [all 14 fixed AI mode tasks](ai-modes.md), the original strict memory/authority rubric and four [held-out evidence cases](memory-capacity.md). The product image path passes four fixed fixtures with original-pixel blank-image abstention; the [raw model failure remains](local-image-understanding.md). Generative reviews retain deterministic fallback and separate approval of every proposed write. Broad task quality, correction effort, controlled latency/resources and native candidate qualification remain open.

Claude/Codex managed execution, account/context controls, contained discovery, cancellation and compatible session/handoff records have source and fixture evidence. Real authorized inference, completed-turn resume and cross-provider handoffs remain unqualified. Remote MCP has official-SDK/TLS fixtures; a real hosted client remains unqualified. See [managed runtimes](managed-runtimes.md), [Claude startup](claude-icu-startup.md) and [remote MCP](remote-mcp.md).

New Mac builds use [miniaudio capture](macos-miniaudio.md); actual microphone/TCC/device evidence remains open. Windows/Linux have [native workers](platform-native-workers.md), [packaging/routing](platform-desktop-packaging.md), [protected credentials](platform-credentials.md) and [OCR](portable-ocr.md) source packages. [Windows storage](windows-storage.md) now creates private directories and audits existing ACLs without changing them, but new-workspace creation, migration, backup, restore and transfer remain explicitly unavailable pending recovery durability. [Portable PDF helper sources](portable-documents.md) and their browser/protocol fixtures are not target execution evidence. Linux native PDF extraction/export is disabled pending enforceable process containment; PDF viewing and HTML export remain available. Neither platform is release-qualified.

[Verified macOS updates and recovery](verified-updates.md), [pre-migration snapshots](pre-migration-snapshots.md), [reproducible code generation](codegen-reproducibility.md) and [selected-component alignment](selected-stack-alignment.md) close scoped source gaps. Signing/notarization, clean installation/restore, actual devices, live accounts, representative datasets and current candidate performance remain separate release gates.

## Candidate-specific evidence

| Candidate | Recorded scope and limits |
| --- | --- |
| [5d7cdb0](development-candidate-5d7cdb0.md) | 544 Go tests with 21 skips and packaged offline journeys; startup failed with missing readiness events. Temporary binaries were lost; durable reports remain. |
| [4900d4b](development-candidate-4900d4b.md) | 620 tests/subtests with 27 skips and packaged P0/P1/P2/native journeys. Later memory/image/source repairs do not change its then-failing rubrics or binary. |
| [be6fc81](development-candidate-be6fc81.md) | 870 service tests/subtests (44 skips), 60 desktop tests/subtests (1 skip), 121 tooling tests and packaged P0/P1/P2/native journeys. Native formatted-word search failed; one of 20 startup samples took 8,399.656 ms and failed its threshold. Later Windows/PDF/rollback/search source is outside this candidate. |

The [60-launch instrumented startup investigation](startup-investigation.md) did not reproduce or explain the earlier missing-readiness events. [Database measurements](database-performance.md) record expensive derived queries and scoped improvements. Each report's source, workload and outcome remain binding; no release gate is marked complete.

## External prerequisites

The research report has not been supplied. Independent clean machines, authorized datasets, live integration accounts, reachable HTTPS and Apple signing access have unknown availability. Coding continues while these remain unresolved. No credentials belong in this record.

## Item index

| Item | Owners | Phases | Implementation |
| --- | --- | --- | --- |
| FIX-001 | D | C1 | implemented |
| FIX-002 | A, D | C1 | implemented |
| FIX-003 | B | C1 | implemented |
| FIX-004 | B, C | C1 | implemented |
| FIX-005 | B, F | C1 | implemented |
| FIX-006 | B | C1 | implemented |
| CORE-001 | B | C2 | implemented |
| CORE-002 | A, B | C2 | implemented |
| CORE-003 | B | C2 | implemented |
| CORE-004 | E | C2 | implemented |
| CORE-005 | F, E | C2 | implemented |
| CORE-006 | A, C | C2 | implemented |
| CORE-007 | A, C | C2, C4 | implemented |
| CORE-008 | C | C2 | implemented |
| CORE-009 | A, E | C2 | implemented |
| CORE-010 | A, D | C2 | implemented |
| PARITY-001 | B | C4 | implemented |
| PARITY-002 | B, D | C4 | implemented |
| PARITY-003 | C | C4 | implemented |
| PARITY-004 | B, C | C4 | implemented |
| PARITY-005 | C | C4 | implemented |
| PARITY-006 | C, D | C4 | implemented |
| PARITY-007 | B, D | C4 | implemented |
| PARITY-008 | B, D | C4 | implemented |
| PARITY-009 | B, D | C4 | implemented |
| PARITY-010 | A, B | C4 | implemented |
| AI-001 | A, E | C4 | implemented |
| AI-002 | E, F | C2, C4, C6 | in_progress |
| AI-003 | E | C2, C4, C6 | in_progress |
| EXT-001 | A, F | C5 | in_progress |
| EXT-002 | A, F | C5 | in_progress |
| EXT-003 | A | C4, C5 | implemented |
| EXT-004 | A, F | C5 | implemented |
| EXT-005 | A, E | C5 | in_progress |
| REL-001 | A, F | C3, C5, C6, C7, C8 | open |
| REL-002 | B, D, E | C3, C5, C6, C7, C8 | open |
| REL-003 | B, F | C3, C5, C6, C7, C8 | open |
| REL-004 | D, E | C3, C5, C6, C7, C8 | open |
| REL-005 | F | C3, C5, C6, C7, C8 | open |
| REL-006 | F | C3, C5, C6, C7, C8 | in_progress |
| REL-007 | A, B, C, E | C3, C5, C6, C7, C8 | in_progress |
| REL-008 | F | C0, C1, C2, C3, C4, C5, C6, C7, C8 | in_progress |
| REL-009 | F | C7, C8 | in_progress |
| GOV-001 | A | C0 | implemented |
| GOV-002 | A, F | C0 | implemented |
| GOV-003 | A | C0, C2, C3, C5, C8 | in_progress |
| GOV-004 | A | C0 | awaiting_external_source |
| GOV-005 | A | C0, C2, C3, C5, C8 | implemented |
| OPT-001 | E, F | C6 | implemented |
| OPT-002 | A, E | C6 | implemented |
| OPT-003 | E, F | C6 | in_progress |

## Evidence policy

Retain historical reports unchanged. New evidence records source identity, command, fixture identity, hardware, outcome and limitations. Freeze application/helper/model hashes before native qualification. A changed candidate needs affected checks again. Missing or skipped native/model/account/device checks remain unqualified.

The [local support bundle](support-bundle.md) provides metadata-only defaults, exact preview and explicit optional page-content consent before native save. Broader diagnostics and packaged/platform verification remain partial. [Authenticated rollback readiness](rollback-readiness.md) and the [rich-text search repair](richtext-search.md) have separate source/evidence scopes; neither changes earlier candidate failures.
