# Phase 2 integration handoff

Five package agents implemented 2A–2E in dependency-ready waves. The four available concurrency slots required the coordinator plus three package agents at a time; each of the five requested packages received its own agent. The shared kernel, desktop adapters, native helpers and UI are integrated in one build.

## Product workflows

| Package | Entry point | Implemented behavior |
| --- | --- | --- |
| 2A | Import | Safe ZIP inventory and progress; Markdown/CSV parsing; hierarchy, attachments and links; editable CSV inference; explicit conflict choices; resumable reviewed commits and background jobs; five-outcome accounting with separate source omissions and unknown completeness; portable export. |
| 2B | Search | Revision-bound FTS5 for pages, tasks, files and transcripts; PDFKit/DOCX/text extraction; exact block/timestamp navigation; deletion and rebuild; optional local semantic/hybrid retrieval and opt-in automatic upkeep. |
| 2C | AI or the AI section on a page | Local streamed answers with validated source links, writing operations, structured drafts, readable before/after review, guarded page/task changes, saved conversations/cancellation, approved memory lifecycle and evidence invalidation. |
| 2D | Meetings or a meeting page | Microphone capture and local upload; journaled audio before recognition; resumable minute-sized Whisper jobs; editable timestamped transcripts and seeking; constrained structured summaries and reviewed action items. |
| 2E | Settings → History and recovery | Page-owned value and transcript restoration, schema conflicts, subtree review, complete verified native backups, staged restore into a new directory, recording recovery, search rebuild, external backup retention with explicit cleanup. |

Jobs shows progress, errors, cancellation and retry of saved inputs. Retry retains source guards and committed units. Source changes require a fresh job when the original inputs no longer apply. Background native work uses the existing scheduler and does not hold the canonical mutation writer during inference.

The package details, format limits and service surfaces are in [2A](2a.md), [2B](2b.md), [2C](2c.md), [2D](2d.md) and [2E](2e.md).

The contract release is 1.2.0 with numeric transport version 1 and the existing canonical snapshot format. New memory commands and transcript entity guards are additive. The desktop package reports version 0.2.0.

## Data and review boundaries

Imports report every discovered item as Preserved, Adapted, Partial, Unsupported but retained, or Failed. Rejected archive paths remain accounted for and the original ZIP remains evidence. CSV display values never establish source formulas or views. Portable manifests retain rich canonical metadata even when the readable re-import cannot recreate its interactive semantics; native backup is the full-fidelity restoration route.

AI generation does not write canonical objects. A reviewed proposal freezes the requested commands, readable before/after content and revision guards. Current task/database records remain authoritative. Memory is retrieved only when approved, live, in scope and enabled; source changes, trash, supersession and forgetting invalidate its use. Existing conversations do not become a second memory store for future retrieval.

Meeting generation uses the bundled llama.cpp grammar sampler to constrain output shape and permitted source labels. The service separately checks exact transcript evidence and source revisions. Invalid output remains a visible failed job and cannot create tasks or decisions without review.

Native backups verify every declared asset and recording object, preserve history and source evidence, and restore to an unused directory through a verified staging area. Unused approvals and transient capabilities are revoked during restore. Page restore changes the selected page's values and body without rewinding shared schemas or unrelated records.

## Reproducible qualification

Run on the development Mac with the pinned tools and verified model cache:

```sh
/private/tmp/trove-phase0-venv/bin/python scripts/verify-phase2.py --models /private/tmp/trove-phase1-models
```

The runner records contract tests, all Go tests with the race detector and actual native model integration, TypeScript checks, editor/database unit and browser tests, the packaged build, and real app create/restart/native checks. It enables the real one-hour audio fixture. Inputs are SHA-256 fingerprinted before and after the run; source changes prevent promotion. `evidence/phase2/latest-run.json` records the latest attempt and `evidence/phase2/verification.json` is written only by an all-pass run. Per-run logs remain in `evidence/phase2/runs/`.

Native model packs are explicitly installed into a separate cache, never bundled or downloaded during ordinary inference. The catalog pins source revisions, sizes, SHA-256 and licenses for the instruction, embedding and speech packs. The compact 0.5B instruction pack remains available, while meeting and service Q&A fixtures use Qwen2.5 1.5B Instruct Q5. A real PDFKit text-layer fixture verifies the packaged extraction helper.

The complete clean-directory restore fixture compares canonical tables for page bodies, databases, links, views, history, assets, import evidence, memory states and transcripts, then searches restored content and resumes interrupted audio. It does not substitute for a physical second-Mac installation.

## Qualified checkpoint

The final run passed **165 Go tests/subtests**, 49 Python contract tests, 20 desktop browser workflows, database browser tests, frontend unit/type checks, production packaging, and real application create/restart/native checks. Source fingerprints were unchanged throughout the run. See [the source-bound verification report](../../evidence/phase2/runs/20260930T162800Z-e92de946/verification.json).

The exact tested development app is preserved at `build/phase2-qualified/Trove.app`, with its source archive at `build/phase2-qualified/source.tar.gz` and binary/source digests in `build/phase2-qualified/qualification.json`. Its executable SHA-256 is `cf6a25b86660e6f899cb321771531848753ea069c698fbb736902089ad05e330`. The shared checkout can advance into Phase 3 without changing this checkpoint.

The final native hour fixture processed all 60 minute units, produced 694 segments, and allowed 47 same-page edits; maximum observed edit latency was 235.89 ms under the race detector. Browser regressions additionally verify clean and dirty editor synchronization, in-flight saves, append-conflict retry, preserved caret/Undo, and refusal to overwrite an overlapping remote edit. Transcription atomically rebases only its destination page; meeting and recording inputs remain fenced.

## Remaining release qualification

- Developer ID signing/notarization and installation on a separate clean Mac.
- Actual microphone permission/device capture, native IME candidate selection and VoiceOver.
- Broad answer/summary usefulness, multilingual and noisy meeting accuracy. Native fixtures establish grounded answers and cited structured meeting notes on explicit source examples.
- Real large Notion exports and maximum-size archive responsiveness, large semantic corpora and physical external-disk interruption tests.
- The one-hour native speech fixture repeats public JFK audio to exercise actual recognition, durable chunking and concurrent edits. It is not a representative one-hour meeting accuracy evaluation.

System audio and reliable speaker identification remain outside this v1 package. Broader autonomous workspace-agent behavior remains P1.
