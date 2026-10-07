# Remaining-work implementation checkpoint — 7 October 2026

This continues the accepted C0–C8 plan. The 51 backlog acceptance statements remain binding. The registry still records **34 implemented, 11 in progress, five open and one awaiting research** at their stated implementation scope. These counts do not mean release readiness. No platform/tier is qualified.

The integrated source checkpoint is `28da3df`; the latest previously packaged application is [e481c6a](development-candidate-e481c6a.md). New source packages below have independent evidence and must be built and qualified together before their results can support a new candidate. Historical failures and reports are preserved. The latest integrated source checks are recorded in [the checkpoint evidence](../../evidence/completion/integrated-2026-10-07/manifest.json).

## Integrated packages

| Package | Implemented change | Evidence and limits |
| --- | --- | --- |
| Qualification | Versioned candidate/platform/tier records; distinct model-free, installed-model, native, physical-device, live-integration, distribution and independent-install groups; exact evidence/input identities; fail-closed missing/skipped/mismatched checks. | [Runner](qualification.md), [mapping](qualification-mapping.md). 34 mapping/runner checks pass. 54 reviewed decompositions preserve original criteria; macOS P0 has 395 required criteria, two mapped and still pending, 393 unmapped. All nine platform/tier plans remain unqualified. |
| Performance tooling | Shell latency failure now produces a failed gate; diagnostic sampling cannot qualify performance. Hybrid measurements execute every requested populated scale with exact sample accounting. Existing evidence destinations cannot be overwritten. | [Harness evidence](../../evidence/completion/performance-harness/manifest.json). New controlled workload runs remain pending; the 8,399.656 ms historical launch is unexplained. |
| Operational diagnostics | Bounded fixed-label events; private rooted storage; ownership, ACL and link checks; separate worker/committed-job outcomes; explicit support-export consent and immutable preview bytes. | [Diagnostics](operational-diagnostics.md). Final affected suite: 45 pass; Settings browser suite: five pass; Windows compile passes. Native Windows execution and complete startup/resource diagnostics remain pending. |
| macOS signing | Explicit Developer ID identity/team selection; ordered helper signing and hash rebinding; sealed app; resumable notarization/stapling and final archive verification. | [Signing](macos-production-signing.md). 175 tooling checks pass at that source scope, including 14 signing checks. No signing identity, Apple submission or public distribution was exercised. |
| Expanded local AI | 24 P0 Q&A/memory cases, 42 held-out mode cases, offline kernel-backed runner, strict task accounting, disk preflight and atomic manifests. | [Evaluation](expanded-evaluation.md). Latest complete 7B P0 run: **21 pass, three fail**, zero missing outcomes; all 24 preserve canonical content. The preference-prompt clarification did not repair the repeated failure. Earlier incomplete/disk-failed evidence remains. No full-corpus pass. |
| Linux process ownership | Shared lifecycle and Linux cgroup-v2 backend with aggregate limits, clone3 admission and kill/drain verification for trusted qualification fixtures. | [Process domain](linux-process-domain.md). Host contracts and cross-compilation pass; native Linux checks are pending. PDF/provider admission remains refused because same-UID isolation and crash cleanup are not established. |
| Platform publisher authentication | Tagged publisher identities, authenticated portable inventories, Linux Ed25519 verification and Windows native catalog verification; legacy macOS identity compatibility remains. | [Publisher foundation](platform-publisher.md). 21 update, one manifest-command and 21 app-recovery top-level tests pass; three native/signing checks skip. Production trust pins are empty. Windows/Linux product update selection, launch and rollback remain disabled. |
| Recovery publication | Positive completion receipts, mandatory startup admission, exact restored inventories and bound owner-transfer provenance. | [Recovery foundation](windows-recovery-protocol.md). Independent review completed; 370 top-level host tests pass, 11 skip. Windows/Linux cross-compiles only. Windows recovery remains disabled pending native durability proof and remaining producer integration. |

The first diagnostics review found ancestor redirection, unordered retention and premature job-success reporting. A second review found foreign hard-linked history and Windows delete-sharing issues. All five findings were repaired; the before/after source evidence is kept separately. This review does not substitute for native platform testing.

The combined regression run exposed a cancellation-test defect: the expected callback connection closure caused the test worker to exit without signalling completion. The independently reviewed [test repair](auth-cancel-regression.md) preserves cancellation, serialized sign-in and credential cleanup/retention checks. It passes 30 focused race repetitions and three complete auth-suite repetitions. The subsequent integrated service race suite passes 609 top-level tests (1,212 including subtests), with 49 explicit skips. Skipped model, account, device and platform checks remain unqualified. Desktop regressions pass 51 top-level tests (115 including subtests), with one skip. TypeScript and all 46 frontend unit tests pass; 208 configured tooling checks pass with one explicit skip. Earlier harness/environment failures are preserved.

## Phase position and next acceptance

| Phase | Current position | Work still required |
| --- | --- | --- |
| C0 | Tracking and executable qualification infrastructure are integrated. | Complete justified criterion-to-scenario mappings and candidate records. Obtain and reconcile the research report. Keep external prerequisites separate. |
| C1–C2 | Original FIX/CORE implementations remain; diagnostics and benchmark gaps have source repairs. | Repair the three reproduced P0 AI quality failures and rerun unchanged rubrics with independent holdouts; finish diagnostics scope; investigate startup outliers; controlled database/hybrid/concurrency profiling and repairs. |
| C3 | Signing path exists; release remains a development candidate. | Freeze integrated binary/helpers/models/fixtures; full packaged/offline/native/device/update matrix; actual signing/notarization and independent clean-Mac install/restore. |
| C4–C5 | Existing advanced workspace and provider sources remain. | Visible-interface acceptance, screenshot permissions, live Notion/provider authentication/inference/resume/revocation, reachable trusted HTTPS MCP, cross-provider handoffs and integrated P1 candidate. |
| C6 | Wider test inputs and specifications are prepared. | Execute 42 held-out mode cases; implement/run the pending context, lifecycle-memory, relevance and review evaluations; supply 12 authorized photo/screenshot image inputs; qualify all 24 image cases, 12 reviews and P2 persistence/recovery/hosting. |
| C7 | Recovery publication and publisher authentication foundations are integrated and independently reviewed; native operations remain gated. | Native recovery durability proof and remaining producer integration; AppContainer/Job containment; capture/login and reviewed update/readiness/rollback adapters; real Windows fault/device/install/portability acceptance. |
| C8 | Trusted cgroup process-domain and publisher authentication foundations are integrated. | Malicious-worker namespaces/filesystem/process isolation, crash cleanup, native WebKit ownership, portal capture, trusted publisher-policy wiring and reviewed update/readiness/rollback, target installation and cross-platform backup qualification. |

The search repair already has a separate native retest on `254d623`; repeating “add the source regression” is stale. Authenticated rollback readiness already has an ad-hoc native handoff result. Both original failing/slow candidates remain in history. Signed-pair, clean-machine and current-candidate performance checks remain open.

## Prerequisites

| Prerequisite | Known situation | Effect |
| --- | --- | --- |
| Separate clean Mac | User confirmed unavailable. | Independent install and second-installation restoration stay pending. |
| Internal temporary storage | The earlier expanded model run exhausted disk. Later headroom exceeded the unchanged 4 GiB minimum and the fresh full P0 run completed without a storage failure. | Recheck headroom before later runs. Current P0 failures are model quality, not a missing run or disk failure. External ExFAT scratch is not treated as private runtime storage. |
| Windows 11 / Ubuntu 24.04 | Availability not established; this session is on macOS. | Cross-compilation is source evidence, not target execution. |
| Apple signing/notarization | Availability not established. | Production tooling can be reviewed/tested locally; distribution gates cannot pass. |
| Authorized Notion/provider accounts and HTTPS | Not established. | Real integration workflows and interoperability remain pending. |
| Representative imports/audio/images | Required authorized datasets and independent expected outcomes are not complete. | Synthetic fixtures cannot close representative-data or physical-device checks. |
| Research report | Not supplied. | GOV-004 and research-parity reconciliation remain open independently of known P0 work. |

Credentials do not belong in this document. Authentication must use supported product/vendor flows. No source implementation or passing fixture count changes these prerequisite states.

## Completion policy

Use [registry.json](registry.json) for ownership and backlog implementation, [requirements-overlay.json](requirements-overlay.json) for scoped PRD/TRD source coverage, and [qualification.md](qualification.md) for candidate acceptance. Link new evidence to exact source/binary/model/fixture identities. Preserve failures. Keep missing, skipped, unrun and mismatched outcomes pending. A changed application or signing step requires a new candidate and affected qualification.
