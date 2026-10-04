# Opt-in local model personal reviews

Personal reviews retain the deterministic digest. A plan can additionally select an installed local instruction model, review categories, approved-memory use, and permitted page/task action fields. Both modes use the existing daily/weekly wall-clock schedule and explicit local activation. Model analysis never applies canonical changes by itself.

## Using a review

1. Open **Intelligence → Personal review**. Choose the page that stores the plan, one or more source pages, recurrence, weekday/time/time zone and missed-run policy.
2. Leave local model analysis off for a deterministic digest. To enable analysis, choose a verified installed chat model and one or more categories: summary, risks, decisions or suggested next steps. Approved relevant memories are a separate choice.
3. Choose permitted proposed changes and existing destination pages. Options are review append, page rename, page append, task creation, task status and task due date. Task creation targets the existing canonical Tasks collection; updates target existing task pages with canonical status/due bindings. Exact titles must resolve uniquely. No unrestricted command, external action, property mutation, memory edit or arbitrary code field is accepted.
4. Check the explicit local-analysis consent and save. Imported plans remain inactive. Review their sources, model, categories and destinations before local activation; references to unavailable imported source identities require a newly configured plan.
5. Prepare now or leave the workspace open until the schedule is due. Queued and running analysis can be cancelled. Failures/cancellations allow an explicit retry, at most three started attempts on the same captured evidence. Changed evidence requires a fresh review.
6. Read the analysis and its source quotes. **Review proposed changes** shows the shared action planner's destination, before and after. **Approve these changes** applies exactly that guarded proposal. Keeping or dismissing a draft writes none of its proposed content. Analysis with no proposed/configured actions remains readable without a mutation.

## Authority and lifecycle

A durable occurrence starts `queued`, is claimed as `generating`, and finishes `ready`, `failed` or `cancelled`. Only the separate approved proposal makes it `accepted`; dismissal records `dismissed`. Model worker failure cannot starve the schedule/automation maintenance loop. Interactive AI and reviews share one app-scoped native-generation flight; shutdown cancels it. A process interruption leaves a cancelled attempt on reopen rather than rerunning an in-flight model call silently.

Captured guards bind the plan block/hash/activation, source and destination pages, related records/collections and approved memory identities/revisions. Checks repeat after tokenization, after generation, before preview and inside the canonical commit transaction. Page/record/schema changes, forgotten or stale memories, deactivation and changed plan configuration invalidate pending writes. AI provenance, exact command/guard approval, atomic receipts and repeated-apply idempotency are the existing shared `AIProposal` boundary.

Every model attempt is permanently linked to its review receipt. Preparing an alternative proposal through the generic AI API cannot bypass review activation or replace the currently reviewed proposal, including after a retry. Portable exports retain the canonical plan configuration; they do not restore local activation, queued jobs, model attempts or proposal receipts.

## Deliberate bounds

- Up to 32 selected source pages and 32 permitted destination pages, with up to 1,000 captured revision guards.
- At most four source excerpts per selected page (current canonical record fields and indexed title/body excerpts), each bounded to 1,200 bytes, and up to ten approved relevant memories. Record fields are read in the same guarded snapshot, with up to 32 scalar fields; formula/rollup caches are excluded. Capture has a 64 KiB text budget with memory reservation. A query supplies the review focus; this is not exhaustive retrieval of every matching block or all database rows.
- The verified model's actual tokenizer counts chat framing, system instructions, selected evidence and output reservation. The UI reports omitted/shortened context. If no source fits, analysis fails with an actionable error.
- At most 12 cited claims and eight model-proposed page/task actions; optional review append is composed by the kernel. Selected categories and allowed action kinds constrain the native grammar; field-level and destination checks run again against validated output.
- A quoted source must match captured evidence. Exact-quote verification establishes the source basis, not semantic entailment of every generated interpretation; people must inspect the draft. Suggestions must be identified as suggestions. Unsupported or invented quotes fail without applying changes.
- The shared planner bounds a text append to 4,000 characters and 200 canonical commands. Oversized review-plus-citations appends fail visibly; no silent truncation creates an apparently complete review.
- Scheduled reviews run only while this workspace's owner process is running. Daily/weekly IANA schedules retain existing DST, occurrence deduplication, one-missed-run catch-up/skip and failure isolation behavior. This is not an OS wake-up service.

## Verification and release status

The dedicated kernel tests exercise no writes before approval, atomic approval/idempotency, denied alternate AI proposals, changed evidence, cancellation/retry/restart, deactivation, selected categories/fields, memory forgetting during generation, canonical task status/due changes, source changes during tokenization and inactive portable reimport. Existing deterministic review and shared AI/action planner tests remain in the focused suite.

Four browser scenarios cover explicit model consent and persisted weekly/categories/fields/destinations, read-only preview and explicit approval, cancellation/retry, and the retained graph/OCR/deterministic review screen. TypeScript and the production frontend build pass. Existing third-party bundle-size and module-directive warnings remain.

`TestRealLocalPersonalReviewCitedProposal` uses the real verified Qwen2.5 1.5B Instruct Q5 model, native tokenizer and grammar generation on a fixed synthetic blocker fixture. Its first run correctly rejected an unselected category; the grammar was tightened to the selected categories and enabled action kinds, after which the unchanged factual rubric passed with a live exact quote and separate explicit append approval. This is one targeted model fixture, not broad factual quality, packaged desktop qualification, hardware coverage, Windows/Linux support or signed-release evidence. Prior unrelated model-quality failures remain open.
