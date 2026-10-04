# Phase 0 implementation handoff

The shared contract set is version `1.0.0`. Phase 0 is complete when every workstream can consume the same persisted identities, validated payloads, mutation envelope and worker protocol. It does not mean the P0 desktop product has shipped. Product acceptance, native support and import preservation remain distinct claims in [requirements-ledger.json](requirements-ledger.json).

The supplied PRD/TRD version 3.1 define product scope. Their text extractions and SHA-256 digests are tracked in the ledger. The original R00 research report with 307 behaviors and the C00 precedent documents were not supplied. Every audit ID explicitly cited by the PRD/TRD is retained, including expanded ID ranges. This is not a claim to have reconstructed the unavailable original audit. Source findings or upstream documentation do not count as implementation evidence.

## Shared entry points

| Consumer | Canonical contract | Conformance evidence |
| --- | --- | --- |
| Every workstream | `contracts/domain.schema.json`, `schemas/workspace.sql`, `docs/phase-0/domain.md` | `tests/test_domain.py` |
| Editor, database, import/export, AI | `contracts/registries.json`, `docs/phase-0/registries.md` | `tests/test_contracts.py`, `tests/test_domain.py` |
| Renderer, importers, tools, adapters | `contracts/service.schema.json`, `schemas/services.sql`, `docs/phase-0/services.md` | `tests/test_contracts.py`, `tests/test_services.py` |
| Background work and native supervisors | `contracts/jobs.schema.json`, `docs/phase-0/jobs.md` | `tests/test_contracts.py`, `tests/test_jobs.py` |
| Tasks, re-import and project memory | `docs/phase-0/decisions.md` and corresponding domain examples | `tests/test_domain.py` |
| Native experiments and packagers | Dependency pins and `scripts/validate-baseline.py` | `evidence/baseline*.json`; each result records its actual qualification scope |
| Maintainers and reviewers | `requirements-ledger.json` | `tests/test_ledger.py`, `evidence/phase0-verification.json` |

Example payloads are in [`contracts/examples`](../../contracts/examples), including the complete domain graph, every registry type, import plans/resolutions, atomic batches, conflict/event responses, search citations and worker messages. The [`protocol manifest`](../../contracts/examples/protocol-manifest.json) maps protocol fixtures to their definitions. Generated [Go types](../../contracts/types_generated.go), the [Go service interface](../../contracts/service.go) and [TypeScript consumers](../../contracts/generated/consumer.ts) establish compile-checked handoff shapes. Consumers must reuse these shapes and the registry's named adapters. A mock response or importer intermediate object is not a new storage representation. Python conformance implementations specify and exercise invariants; the production Go services must satisfy the same fixtures and implement equivalent transactions, validation and policy.

## Accountable work packages

Roles below are accountable owners, not claims that a person has already accepted an assignment. Assign one developer or agent to each role before product work starts; keep the role identifier in the ledger stable when people change. A work package may contain requirements from more than one release. Its earliest release does not pull later features into P0.

| Package | Owner role | Responsibility | Depends on |
| --- | --- | --- | --- |
| WP00 Shared contracts | `architecture` | Contract versions, cross-package decisions, freeze and compatibility changes | PRD/TRD and this request |
| WP01 Native baseline and packaging | `native-platform` | Exact pins, CGO/SQLite capabilities, Darwin arm64 workers, signing and installers | Shared service/worker versions; model manifest |
| WP02 Workspace kernel | `workspace-kernel` | Migrations, IDs, single writer, revisions, atomic operations, tasks, history and policy | Domain and service contracts |
| WP03 Editor and navigation | `editor` | Registry-backed editor, readable fallbacks, serialization, extraction/export, editor quality | WP02; registry contract |
| WP04 Databases | `database` | Collections, page-backed records, typed properties, query AST, views | WP02; property/view registries |
| WP05 Import/export | `import-export` | Inventory, intermediate representation, provenance, mapping, three-way re-import and reports | WP02/WP04; all registry adapters |
| WP06 Search, AI and memory | `search-ai` | Derived projections, hybrid retrieval, validated citations, scoped approved memory | WP02; locators; registry extraction; WP07 |
| WP07 Jobs and media | `jobs-media` | Persistent queue, leases/recovery, bounded worker protocol, audio/transcripts | WP02; worker contract; WP01 |
| WP08 Integrations/automation | `integrations` | P1 provider/MCP adapters, tools, deterministic rule/schedule execution | WP02/WP07; qualified external interfaces |
| WP09 Recovery and release quality | `quality-release` | Acceptance fixtures, restore, hostile input, native/offline gates and evidence | All delivered work packages |
| WP10 Enhancements/hosting | `architecture` | Explicitly scheduled P2 functionality; single-owner hosting | Applicable P0/P1 packages; separate approval of scope |

The owner of a source-of-truth object owns its service operation. Importers and workers return validated proposals or results, and the workspace service commits them. UI caches, view layouts, indexes and memory retrieval never become competing authorities. The service authorizes every actor at commit, including internal/background actors.

## Work permitted in parallel now

| Work | Inputs and permitted output | Boundary for integration |
| --- | --- | --- |
| Interface mockups | Shared page/record/view IDs, registry metadata, typed request/response examples; interactive mock screens | Do not persist alternate documents or invent completion states. Replace mocks through the same facade. |
| Importer fixture preparation | Sanitized/generated exports, provenance manifest, expected inventory and fidelity outcomes, base/local/incoming conflict cases | No direct SQLite writes. Fixtures must distinguish source-absent, unknown, unsupported-retained and failed content. |
| Native worker experiments | Pinned sources, worker request/response envelope, approved handles and resource limits; isolated reproducible probes | No workspace database access, unbounded stdout or ad hoc shell commands from payloads. Keep experimental results distinct from signed-app qualification. |
| Packaging experiments | Exact baseline pins, Darwin arm64 target and intended bundled-worker/model manifests | Do not claim clean-machine/offline delivery from a development checkout build. Record actual binary hashes and missing qualification. |

Schema consumers can proceed concurrently after the contract suites pass. Product mutations integrate only after WP02 implements the Go service/persistence boundary. Retrieval depends on canonical text extraction and source locators; AI correctness cannot be verified against fabricated IDs. Import can prepare staged batches before the production service exists, but cannot bypass its eventual revision, scope and idempotency checks.

## Contract freeze and change process

1. A proposal identifies the affected requirement IDs, owner, service/type/registry versions and consumer packages. Include old and new payload examples and the compatibility impact.
2. Add or update schema, DTO definitions, registry behavior, migration and fallback rules together. Preserve raw data when a reader cannot understand a version. No silent renderer migration or replacement of canonical IDs.
3. Add meaningful fixtures for success, invalid input and affected conflict/recovery cases. Test backward readability or explicitly require an upgrade; run consumer suites.
4. Bump the appropriate schema/registry/contract version. A breaking transport or persisted meaning change requires a major contract decision and migration plan. A supported optional additive field still requires versioned schema and reader behavior. Type versions advance for deterministic payload migrations.
5. The accountable role reviews its boundary; architecture resolves cross-package conflicts. Update ledger artifacts and test mappings. Do not set `verified` until the new artifact/build has passing evidence.
6. Freeze by recording the exact contract version, source/artifact hashes and verification run. Any subsequent contract edit invalidates the affected evidence and requires a new run. Consumers pin this version until they intentionally update.

The task, re-import and memory decisions are normative refinements of the PRD/TRD's underspecified examples. The TRD's nullable task page/block fields and raw `todo.checked` description do not authorize a second task state; use the resolved task ownership contract. P0 source-preservation capability does not imply P1 native editing. A P1 full block-link interface does not postpone P0 citation IDs, revision checks or transcript seeking.

## Definition of completion

For a Phase 0 contract requirement, `implemented` means the relevant schema, constraints, behavior specification and representative valid/invalid payloads exist. `verified` means the actual conformance checks passed, their scope covers the acceptance criteria, and the evidence identifies the exact artifacts tested. The ledger itself is checked for source completeness, owner/test mappings and honest state transitions. A document marked accepted is not automatically implemented or verified.

For a product requirement, completion additionally requires production Go persistence/services, editor or interaction behavior where applicable, import/export and readable fallback, extraction/retrieval/AI representation, revision/policy/cancellation behavior and release-specific tests. Native block support, preservation fidelity and full product delivery must be recorded independently. A Python contract test does not prove the Go application is implemented; a browser test does not qualify Wails native permissions/audio.

Evidence records must contain the test IDs and actual result, command, execution timestamp, artifact/build fingerprint, fixture provenance, target platform/runtime versions and unresolved differences as applicable. Keep failed and blocked outcomes available; do not replace them with a plan. `implemented=true` can coexist with `verified=false`; `verified=true` requires `implemented=true` and covering passing evidence. Tests that have not been written are marked planned in the test catalog.

The exit gate is satisfied only when all twelve `PH0-*` requirements have applicable passing evidence, schema examples are executable without inventing missing representations, and baseline evidence states exactly what was validated on macOS Apple Silicon. The P0/P1/P2 product rows remain separate release obligations. Signed clean-machine installers, full offline model inference, production editor behavior and actual imports are later product gates unless the evidence demonstrates they were executed.

Run `python3 scripts/verify_phase0.py` in the [documented baseline environment](baseline.md) to collect the conformance result. [`evidence/phase0-verification.json`](../../evidence/phase0-verification.json) records each Python test plus the Go and TypeScript checks and final artifact hashes. [`evidence/baseline.json`](../../evidence/baseline.json) records 35 Darwin arm64 checks and retains separate unverified product release gates. Each Phase0 ledger row points to concrete named checks through `verificationChecks`; the ledger tests inspect those actual results and command-log hashes. Review the final artifact hashes after the verifier finishes: the ledger itself is an input, so its hash is captured after its validation test.

The accepted Phase0 baseline uses sqlite-vec Go bindings/runtime v0.1.6. The later upstream runtime's DELETE fix is tracked by `BASELINE-SQLITE-VEC-RELEASE`: the workspace-kernel owner must qualify an upgrade or tested mitigation before P0 product release. Native compile/link and simple delete/reinsert evidence do not satisfy that stress-test gate. See [the baseline decision](baseline.md#storage-and-sqlite-vec-decision).

Review individual results and baseline evidence before recording completion; neither a manifest nor a green aggregate result alone proves the acceptance criteria. All product delivery requirements remain independently unverified until their production acceptance tests pass.
