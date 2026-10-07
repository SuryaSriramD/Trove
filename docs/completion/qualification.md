# Candidate-bound completion qualification

The [policy](qualification-policy.json), [record schema](qualification.schema.json) and `scripts/qualify-completion.py` add a current evidence layer. They do not amend historical ledger flags, candidate reports, `validate-completion.py`, or the acceptance criteria in `remaining-work.md`.

The runner distinguishes named software regressions, minimum release gates, acceptance coverage, technical qualification and publication approval. **No existing Trove candidate is qualified by this package.** The policy has two narrowly scoped executable service mappings. All other criteria remain unmapped; even these two stay pending until fresh candidate-bound runs pass. Every broad gate passing would still leave unmapped acceptance pending. The reported `releaseQualified` can become true only when every applicable criterion has an exact mapped scenario, every required gate passes, and all current file/record identities match. `releaseApproved` remains false; publication requires a separate human decision.

## What is bound

Freeze requires a clean committed checkout and a new output directory outside it. It captures a Git-generated source archive; exact source commit/tree; a complete application file/directory inventory; explicitly selected helper, model and fixture files/directories; target, recorded hardware/OS; policy identity; and acceptance catalog identity. Symlinks within inventories and reused output paths are rejected. Bound files must remain available: deleting temporary inputs makes later verification unavailable, not implicitly successful.

Hashing associates selected bytes with a record. It does not prove a binary was compiled from that source or that an observation is true. The separate build-provenance gate needs an actual builder report; manual records identify their observer and retain supporting artifacts. This is an auditable local evidence mechanism, not a cryptographically authenticated remote attestation service. Do not include credentials or private datasets in committed records.

The coverage catalog retains every individual criterion from the 312 original product requirement rows (205 P0, 92 P1 and 15 P2), plus all 51 original backlog acceptance statements. The [scope and mapping review](qualification-mapping.md) explicitly decomposes 54 mixed or narrowly mapped source criteria. Every original parent remains visible; children must cover all of its literal acceptance wording, match its source hash and include every declared `requiredParts` entry. Shared wording may intentionally appear in separate tier contexts. There is no automatic interpretation of text as native support or a passing result. Unreviewed statements retain the conservative source tier and stay unmapped.

Tier scopes are cumulative. CORE-007 table and gallery cases are distinct; selected P1 behavior does not become a P0 prerequisite merely because it shares a source paragraph. Windows and Linux REL-009 obligations are separate from a macOS-only release. GOV-004 remains an unresolved research-parity obligation in `programmeReconciliation`, alongside the unavailable report prerequisite. That separate programme inventory currently reports pending; this runner does not record or automatically close research reconciliation. The P0 requirements-reconciliation gate and accepted ledger amendments, including ADR-015, remain binding. A broad implementation-package association, historical `verified` flag or `complete: true` field never supplies acceptance coverage.

`acceptanceCases` is keyed by `target/tier`, then exact catalog criterion ID. Each mapping must carry the criterion's canonical SHA-256 and one or more exact check/scenario references. Expected text, release context and evidence kind must match. Executable service cases additionally name the exact selected tests and pin the reviewed test files and assertion anchors. A changed policy, criterion, scope or reviewed test file invalidates old evidence and requires review. These source anchors document why a test is relevant; only fresh passing candidate-bound run receipts supply results. Fixture-backed service checks cannot satisfy observation criteria for native apps, models, devices, live accounts or clean-machine installation. Completing the remaining case-level coverage is still work.

## Commands

Read-only inventory and dry run:

```sh
python scripts/qualify-completion.py plan --target darwin-arm64 --tier P0
python scripts/qualify-completion.py coverage --target windows-amd64 --tier P2
```

`plan` returns pending checks and resource availability; it runs no tests and writes no evidence. The user has no separate clean Mac, so `independent-clean-target` defaults to `not_available` for Darwin. A fresh local user or VM is useful supplementary evidence, but must not be described as an independent clean machine. Windows/Linux target availability remains unknown until established.

After building an immutable candidate from the clean checkout, freeze the actual paths. The following placeholders must be replaced with real local inputs; missing assets remain missing rather than receiving invented hashes:

```sh
python scripts/qualify-completion.py freeze \
  --target darwin-arm64 --tier P0 \
  --bundle /actual/Trove.app --helpers /actual/native-bundle \
  --models /actual/selected-packs --fixtures /actual/authorized-fixtures \
  --hardware 'actual recorded hardware' --os-version 'actual OS build' \
  --output /actual/new-qualification-directory
```

`--helpers`, `--models` and `--fixtures` can repeat. Omitting a group is allowed for an incomplete record, but checks requiring it cannot pass. Models and fixtures are never downloaded by this tool. Store records and source inputs durably if later status checks need them.

Execute only an explicitly selected automated check:

```sh
python scripts/qualify-completion.py run \
  --record /actual/new-qualification-directory/candidate.json \
  --check p0-service-regressions --cache /actual/verified-tool-cache
```

The four automation groups select exact existing device-free Go test names for P0 services, P1 workspace behavior, reviewed P1 tools and P2 canonical/review behavior. The native target and clean checkout must match; the pinned Go resolver and offline cache settings are reused. Ambient integration/CGO/tool-selection flags are removed. Every named test and the exact package completion event must occur; failed or skipped subtests, similarly named substitutes, missing tests and nonzero exits fail. There is no generic `TestReal*` invocation and no automatic microphone/account/model activation. These selectors are a bounded regression checkpoint, not the complete acceptance suite. Native/device/model/account gates use actual observations and explicit specialized procedures.

Create an unfilled observation template, hash actual evidence files, then record the completed observation:

```sh
python scripts/qualify-completion.py template \
  --record /actual/new-qualification-directory/candidate.json --check p0-native-editor
python scripts/qualify-completion.py hash --path /actual/native-observations.json
python scripts/qualify-completion.py observe \
  --record /actual/new-qualification-directory/candidate.json \
  --check p0-native-editor --observation /actual/completed-observation.json
python scripts/qualify-completion.py status \
  --record /actual/new-qualification-directory/candidate.json
```

The template contains pending results and no observation, observer or evidence hashes. It is deliberately rejected as finished evidence. Record exact observed outcomes and real artifact bindings; include every policy scenario with its unchanged expected outcome. Required resources have explicit available/unknown/not-available states and supporting evidence. Results may be failed, skipped, pending, not available or unqualified; none counts as passed. Timestamps must follow the freeze. Evidence and model/fixture inventories are checked again during status. A stale or foreign candidate report cannot be substituted just because its tests passed.

Each check directory is single-use. Failures and incomplete output remain retained. A corrected rerun uses a fresh qualification record and actual rerun evidence; it does not overwrite the old attempt or import its receipts into a new identity. Keep previous records alongside the new assessment.

`status` is read-only. Exit 0 means complete technical evidence under the exact policy, never permission to publish. Exit 2 means pending/unqualified evidence; exit 1 is an invalid request/tool error. Status reports `softwareChecks`, `minimumGates`, criterion-level `acceptanceCoverage`, `releaseQualified` and separate `releaseApproved`. Every applicable software and release check is required for the final predicate. Unknown directories, changed inventories, missing raw reports and mismatched policy/source/candidate fields prevent qualification.

## Tests and limits

`tests/test_completion_qualification.py` exercises real file hashes and isolated synthetic records. It covers all target/tier inventories, exact existing test selectors, schema shapes, stale/different candidate evidence, missing or skipped tests, changed/extra application files, replaced model/fixture/report bytes, missing artifacts, dropped required checks and receipts, unavailable resources, false approval flags and duplicate JSON keys. Acceptance tests reject boolean completion and broad scenario substitution, and demonstrate the positive predicate only on an isolated synthetic minimal policy/catalog; corrupting its evidence immediately removes qualification.

This package does not rerun product suites, builds, models, accounts or devices, and does not fill in any manual observation. Current Windows recovery, Linux PDF containment, platform provider/update gaps and the independent-Mac prerequisite remain as reported in their owning packages. Further criterion decomposition/mapping, representative rubrics and real candidate evidence are still required. The scope review records exact remaining unmapped IDs through reproducible `coverage`/`plan` output and a committed dry-run gap inventory.
