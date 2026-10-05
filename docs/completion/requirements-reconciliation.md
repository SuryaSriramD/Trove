# Current requirements and completion evidence

The original [requirements ledger](../phase-0/requirements-ledger.json) contains **338 rows: 312 product requirements, 12 contract requirements and 14 excluded/proposed boundaries**. Its original requirement rows, status flags, source anchors, acceptance criteria, test catalog and historical evidence are unchanged. In particular, 287 product rows still have their original unset implementation/verification flags; the newer work was never reconciled into those historical fields.

The additive [current implementation overlay](requirements-overlay.json) maps every product ID to named source/evidence packages, relevant completion backlog items and remaining gates. The other 26 IDs are explicitly accounted for as historical contracts or unchanged scope boundaries. The overlay records the reviewed source commit and hashes the original ledger content so a validator can detect accidental historical changes.

A mapping means **scoped coverage exists**. It does not mean every behavior in the requirement is implemented, that old tests ran on the reviewed commit, or that a product/platform is qualified. Every mapped row inherits the package's stated scope and remaining gates. These limits matter especially for broad FR/V/R/ADR and TRD constraint rows. A link to an acceptance test's source means that scenario exists; the linked report establishes only the execution it actually records.

## Three records with distinct purposes

| Record | What it establishes |
| --- | --- |
| Historical ledger | Original source/acceptance mappings and the actual historical contract/product reports; original statuses stay intact. |
| Current overlay and 51-item registry | Current source locations, package evidence, backlog ownership, partial coverage and remaining implementation/qualification work. |
| Immutable candidate manifest | Exact application/helper/model/source identities and results for that candidate alone. Later source packages never amend an older binary's identity. |

Every registry `evidence_target` is explicitly a **planned destination**, not evidence that a directory or passing run exists. Actual records are linked through `implementation_evidence` and `development_candidate_evidence`. Missing research remains unresolved under GOV-004; no part of the absent 307-behavior report is invented.

## Reconciled results and limits

- Notion API capture, the paired browser clipper and visible automation configuration now have direct package/source references. Notion API fixtures are synthetic; the clipper includes an actual isolated Chromium extension/bridge/import fixture. Neither establishes all live-account/browser/platform behavior.
- Provider handoff/lifecycle source and fixture evidence is linked under EXT-005. Actual authorized inference, compatible completed/interrupted-turn resume and real cross-provider handoffs remain unqualified.
- The 14 fixed local AI mode tasks pass on source `ab3ff13` with the optional 7B pack. The original strict memory/authority and held-out cases have separate 7B evidence. Earlier smaller-model, raw blank-image and intermediate generation failures remain retained. These are narrow task results; broad quality, user correction effort and native/platform qualification remain open.
- New macOS builds select miniaudio/CoreAudio capture. Production backend compilation and device-free protocol fixtures do not establish actual microphone permission, device capture or signed-app TCC attribution.
- Windows private-storage/publication and native-worker/credential/packaging/OCR/document-helper source packages remain distinct from target execution. Windows new-workspace creation, migration, backup, restore and transfer explicitly remain unavailable pending a qualified directory/marker durability protocol. Linux/Windows full native and clean-install evidence is still required.
- OPT-003 links the existing P2 implementation and `4900d4b` packaged checks for actual chart values, confirmed actions, graph citations, original conversation bytes/branches, display persistence, backup/reopen and inert portable settings. That scoped result does not close all P2 or hosting acceptance.

TRD-CONSTRAINT-020 is explicitly partial: narrower worker/index/automation diagnostics and token redaction exist, but a complete redacted diagnostics system and optional user-generated support-bundle flow with explicit content inclusion were not located. This mapping leaves that acceptance work unresolved.

The candidate inventory keeps `5d7cdb0` and `4900d4b` separate. The earlier missing-readiness startup failures remain in their original reports. The [be6fc81 candidate](development-candidate-be6fc81.md) has its own committed manifest: service/packaged checks pass at their recorded scope, but native formatted-word search and the startup latency threshold fail. All 20 launches returned readiness; the 8,399.656 ms outlier remains a failure. The 100-page hybrid sample does not qualify larger semantic workloads. Source packages after that commit cannot borrow its checks.

## Validation

Run `python3 scripts/validate-completion.py`. It reads existing files only and verifies complete ledger/overlay accounting, preserved historical content, all 51 backlog IDs and acceptance links, scoped package references, real evidence paths, candidate manifest/source identity and unqualified release status. It does not run product tests or turn an association into a pass. The [reconciliation validation report](../../evidence/completion/governance-reconciliation/manifest.json) records seven passing historical-ledger tests, complete mapping validation and seven deliberately invalid metadata probes rejected as expected. These are documentation checks, not product acceptance evidence. The existing `tests/test_ledger.py` remains the validator of the original source anchors, test/evidence catalog and historical status rules.

GOV-003 remains ongoing: every later implementation and candidate needs reconciliation at its real scope. Missing external research/accounts/devices and incomplete platform behavior remain open rather than being closed through documentation.
