# Acceptance scope and scenario mapping review — 2026-10-08

This is a review of acceptance definitions and test assertions, not qualification of a Trove build. It uses the existing requirements ledger, completion registry and original `remaining-work.md`; those historical sources are unchanged. No product tests, app builds, models, providers, devices or performance benchmarks were run for this package.

## Scope rules

The policy contains 54 explicit decompositions. Each keeps the original parent criterion, a SHA-256 of its original identity/text/tier/scope, a rationale, the required child inventory and every character of binding acceptance text (apart from whitespace). Children contain literal source fragments, release, platform, context and evidence kind. Missing fragments, missing child scopes or changed source text fail validation. This inventory makes accidental omissions detectable; its interpretation still requires code/document review and is not a human release approval.

| Source scope | P0 | P1 / P2 | Preserved obligation |
| --- | --- | --- | --- |
| FIX-003; R03/R04; V04 | Existing editing and imported formatting/layout preservation | Expanded native layout, tab, H4 and merged-cell editing | Actual handlers, identities and every declared round trip |
| CORE-001; PRD/TRD table rows | Table structure and original/span preservation | Reversible merge/unmerge | No silent loss of populated content |
| CORE-007; TRD-CONSTRAINT-022 | Table loading/rendering and committed P0 query semantics | Gallery and advanced P1 formula/rollup behavior | Representative workload, bounded memory, correct scopes/cursors and bridge/paint evidence |
| AI-002; V09/V10 | P0 local modes, evidence and guarded local tools | P1 modes and external reviewed tools | The complete quality, citation, omission, approval and measurement rules for each scope |
| AI-003 | Memory evaluation, freshness, restoration and historical-backup explanation | P1 semantic/lexical related content | No forgotten/stale evidence or hidden source precedence |
| FR-013; R01; V01/V02 | Markdown/CSV/native backup and ZIP import/canonical identity | HTML/PDF exports; HTML/API sources and linked views | Fidelity and native support remain separate claims |
| REL-001/002/004/007/008; V03/V06/V08 | Applicable P0 candidate workflows | Separately repeated P1 and, for the release items, P2 obligations | Exact candidate evidence, representative profiles, real paths and regressions |
| REL-003; PRD-MEDIA-008 | Microphone/upload-first recording | P1 screenshot capture; P2 selected OCR/vision | Actual consent/interruption and original bytes; system audio remains separately platform-qualified |
| Already split person/place/H4 ledger rows | Authoritative P0 preservation portion | Authoritative P1 editable portion | Full quoted source, provenance and exclusions remain retained |
| R07 | — | P1 rules/recurrence; P2 A048 row actions | Independently qualify the selected tier |
| REL-009 | Windows or Linux target's selected scope | Earlier tiers remain required as each target expands | macOS evidence never qualifies another platform |
| GOV-004 | Separate open research-parity programme obligation | Remains open until the actual report is reconciled | Existing accepted ledger requirements are still release obligations |

The P0 report therefore does not require P1 gallery, external providers or the missing 307-behavior report. It still requires every applicable original P0 criterion. The unavailable independent clean Mac remains a release prerequisite and is reported honestly. A local temporary directory, empty PATH, new user or VM is supplementary evidence only.

## Executable mappings admitted

Two mappings use reviewed executable service assertions:

1. `backlog:FIX-001/archive-bytes` → `TestCurrentContentExportsExcludeTrashAndUnreferencedAssets`. The test inspects portable and HTML archive entry names/content, rejects trash-only and unreferenced private bytes, and compares retained shared/live attachment bytes. Helpers are covered by the same pinned test-file hash. This is current-content service export evidence.
2. `requirement:PH2-E:1/unrelated-records` → `TestRestoreSubtreeIsAtomicAndPreservesUnrelatedValues`. The test compares shared schemas/options before/after page restore, asserts the other record retains its current value, and checks stale subtree restoration cannot partially commit. This covers the narrow non-rewind invariant only.

Both execute within `p0-service-regressions`, using exact selectors. Their source file hashes, named test bodies, assertion anchors, expected outcomes and contexts are pinned in policy. They apply to each platform/tier only when the runner executes on that actual native target with fresh bound evidence. No prior test report is imported. The name `TestRestoreSecondInstallationPreservesCanonicalHistoryAndEvidence` describes same-host temporary-directory service testing; it is deliberately not mapped to REL-005 clean-machine evidence.

Other tempting matches were left open: the vector generation tests do not by themselves prove complete generation/rebuild/compaction/performance acceptance; online-backup fault injection does not prove physical crash/disk/device behavior; serialization and mocked browser tests do not replace visible native workflows.

## Dedicated macOS P0 observation procedures

The [procedure catalog](qualification-procedures.json) adds 393 exact observation mappings for the remaining macOS P0 criteria. Each retains the original criterion text/context/hash, a deliberately assigned fixture profile, concrete native actions or source-contract inspection, independently expected values and criterion-specific evidence requirements. The policy pins the entire procedure file hash and its exact case ID. Fixture recipes are preparation specifications, not actual authorized dataset/model identities or results. All outcomes remain pending.

The 42 groups distinguish rich text, technical blocks, tables/preservation, media, navigation/input, databases/tasks, imports/exports, history/backup/faults, search, AI generation/memory, speech/microphone, models/workers, offline/performance, storage/security/jobs/diagnostics, build, installation/distribution and governance. Broad minimum release gates remain separate. Local memory/context and functional speech checks use declared fixture corpora and independent holdouts. Quality/improvement claims have separate representative-data groups, and the broad representative-AI/audio gates remain required. Device, representative import/speech, separate-Mac and distribution groups retain their explicit resource prerequisites.

No new mapping changes an original acceptance criterion or its tier/platform scope. Repeated claim-separation criteria receive their own scenario and procedure identity rather than sharing a generic pass. For example, imported H4, spans, tabs and Person/place fields keep their P0 preservation scope; native P1 editing is not inferred. Tests and procedures cannot substitute a local directory, new user or VM for independent installation. The observation template now emits only the v1 schema fields, even when its policy scenario includes context and pinned procedure metadata.

## Exact remaining coverage

The current dry run has zero passed criteria and no qualified candidate. All 395 macOS P0 criteria now have a mapping, but all still await actual candidate evidence. The remaining target/tier mappings are unchanged. All 51 backlog parents and all original product criterion parents remain accounted for. Counts increase at later tiers because shared obligations are assessed in their explicit release contexts, not because additional product features were invented.

| Target | Tier | Required | Scenario mappings | Still unmapped |
| --- | --- | ---: | ---: | ---: |
| macOS arm64 | P0 | 395 | 395 (2 service, 393 observation) | 0 |
| macOS arm64 | P1 | 623 | 2 | 621 |
| macOS arm64 | P2 | 661 | 2 | 659 |
| Windows x64 / Linux x64 (each) | P0 | 396 | 2 | 394 |
| Windows x64 / Linux x64 (each) | P1 | 624 | 2 | 622 |
| Windows x64 / Linux x64 (each) | P2 | 662 | 2 | 660 |

[The earlier machine-readable gap inventory](../../evidence/completion/qualification-mapping/gaps.json) retains the 7 October state. Regenerate the current inventory read-only using `scripts/qualify-completion.py plan` and `coverage`. Neither the counts nor mapping availability is a passed product acceptance result.

Remaining work is actual fixture/oracle preparation and execution of the macOS P0 procedures, plus case-by-case mapping and execution for the other tiers/targets. This includes native editor/database/navigation journeys; real local-model quality and context/memory rubrics; representative import/audio fixtures and actual consent/cancellation; backup/restore/durability/device failures; recorded performance workloads; independent installation and pack setup; signing/notarization/updates/rollback; P1 real Notion/provider/MCP clients and containment; P2 image/review/package behavior; Windows/Linux native paths; and the separate research reconciliation. A broad successful observation cannot close an exact dedicated scenario. Research reconciliation currently has a read-only pending inventory; accepting its eventual evidence requires a dedicated follow-up rather than reusing a product release receipt.

## Verification

Focused runner tests exercise original-source retention, mixed-tier/platform scope, the unresolved research track, dropped literal fragments/child scopes, scope/hash mismatch, fixture-only substitution for native installation, changed/missing source assertions, skipped/missing tests and candidate-bound evidence corruption. The isolated synthetic positive-predicate test still proves qualification is achievable when every applicable criterion and gate truly passes; no product candidate uses that synthetic policy. Publication approval remains separate and false.

The [current tooling and nine-plan verification](../../evidence/completion/qualification-mapping/p0-procedures-2026-10-08/final/manifest.json) also covers mapped-template schema round trips, procedure-byte drift, sibling-criterion substitution, missing procedures and omitted fixture resources. It checks that every target/tier's acceptance catalog hash is unchanged from the earlier inventory. The first retained attempt remains alongside the final record: a resource-separation assertion caught decomposed backlog IDs left in their old groups; those exact child-ID assignments now have regression assertions. These are qualification-tool tests, not product acceptance observations.
