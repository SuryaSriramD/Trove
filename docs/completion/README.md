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

C1's six defect repairs are implemented with targeted regressions. C2 source implementation now includes table structure, canonical colors/highlights, code/image controls, hybrid Q&A, local audio decoding, historical value mapping, persisted database columns/calendar resizing, sqlite-vec retrieval and online backup. The reproducible toolchain has an offline bootstrap and doctor. These source and regression results do not constitute release qualification.

Database queries now use scoped, sealed cursors and parameterized typed SQLite predicates. Shell snapshots load active page bodies lazily, the quick switcher uses scoped body search, and table/gallery views page and virtualize their results. Shiki and PDF.js use bundled assets. Exact model token budgeting is implemented, while broader AI and memory quality checks retain failures and remain open.

C4 implementation includes rich layout/reusable content, explicitly fetched bookmark previews and consented provider embeds, timeline scales/grouping/dependencies, unified values-and-body templates, versioned formula editing, native rich PDF export, nested import preservation, and shared reviewed local-AI/MCP creation. The integrated Go suite before formula/PDF/SDK integration passed 416 tests with 14 optional checks skipped; 46 frontend unit tests and the two new approval-interface browser cases passed. New packages supply affected checks separately; these are development checks, not a frozen release candidate. Notion database/view migration, the paired browser clipper, and automation configuration are now integrated with affected checks. Managed provider integration, P2 expansion and platform qualification continue. The official MCP SDK and real SDK-client/stdio tests are integrated.

The Codex desktop now connects isolated account authorization, an account-specific model catalog, reviewed context/tool selection, contained execution, cancellation and compatible session controls. Seven provider/account/approval browser fixtures pass; real account inference remains unqualified. Claude contained startup currently fails on the installed runtime and remains gated.

A native tokenizer stall was traced to unwanted Metal initialization and fixed without increasing the timeout. A real local model passed cancellation/forget-during-stream and a narrow named-page creation task with explicit approval and retry deduplication. This does not override the separate failed factual/memory quality rubrics. No release gate is marked complete.

See [recovery evidence](recovery.md), [retrieval evidence](retrieval.md), and the [machine-readable registry](registry.json). The registry records owners, phases, interface boundaries, independent expected acceptance outcomes, evidence destinations, implementation commits and qualification status for all 51 items. Planned scenarios are not passing tests.

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
| AI-002 | E, F | C2, C4, C6 | in progress |
| AI-003 | E | C2, C4, C6 | in progress |
| EXT-001 | A, F | C5 | in progress |
| EXT-002 | A, F | C5 | in progress |
| EXT-003 | A | C4, C5 | implemented |
| EXT-004 | A, F | C5 | in progress |
| EXT-005 | A, E | C5 | in progress |
| REL-001 | A, F | C3, C5, C6, C7, C8 | open |
| REL-002 | B, D, E | C3, C5, C6, C7, C8 | open |
| REL-003 | B, F | C3, C5, C6, C7, C8 | open |
| REL-004 | D, E | C3, C5, C6, C7, C8 | open |
| REL-005 | F | C3, C5, C6, C7, C8 | open |
| REL-006 | F | C3, C5, C6, C7, C8 | open |
| REL-007 | A, B, C, E | C3, C5, C6, C7, C8 | open |
| REL-008 | F | C0, C1, C2, C3, C4, C5, C6, C7, C8 | in_progress |
| REL-009 | F | C7, C8 | open |
| GOV-001 | A | C0 | implemented |
| GOV-002 | A, F | C0 | implemented |
| GOV-003 | A | C0, C2, C3, C5, C8 | in_progress |
| GOV-004 | A | C0 | awaiting_external_source |
| GOV-005 | A | C0, C2, C3, C5, C8 | in_progress |
| OPT-001 | E, F | C6 | in progress |
| OPT-002 | A, E | C6 | in progress |
| OPT-003 | E, F | C6 | open |

## Evidence policy

Retain historical reports unchanged. New evidence records source identity, command, fixture identity, hardware, outcome and limitations. Freeze application/helper/model hashes before native qualification. A changed candidate needs affected checks again. Missing or skipped native/model/account/device checks remain unqualified.
