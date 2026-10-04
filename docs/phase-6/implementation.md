# Phase 6 — Independent P2 packages

This phase extends the existing Phase 0 contracts and P0/P1 implementation with nine optional packages. The implementation backlog, ownership and acceptance boundary below are independent: a package does not inherit another package's qualification. The final verification report records the source fingerprint and actual checks. Previous release evidence remains historical.

## Stable ownership and backlog

| ID | Owner | Deliverable | Dependencies | Acceptance evidence |
| --- | --- | --- | --- | --- |
| P2-01 | C — Databases | Exact local aggregations with bar/donut display and record provenance | Stable properties, records, queries | `p2_database_test.go`; database UI tests |
| P2-02 | C — Databases | Saved local form settings, required typed values, atomic record submission | Properties, guarded record creation | `p2_database_test.go`; local form browser checks |
| P2-03 | C + A | Saved row actions restricted to the selected record, explicit confirmation | Tools, permissions, revisions | Guard/confirmation/scope/restart fixtures |
| P2-04 | E — Search and intelligence | Navigable explicit and inferred relationships with provenance | Canonical links, retrieval | Graph scope/provenance fixtures; intelligence panel |
| P2-05 | E + F — Native runtime | Optional local image text recognition with bounded inputs and worker output | Files, compatible local OCR runtime | OCR validation/quality-limit tests and native smoke |
| P2-06 | E + A | Daily/weekly local review drafts and explicitly reviewed effects | Retrieval, memory, recurrence, typed commands | Schedule/dedup/stale-evidence/approval fixtures |
| P2-07 | D — Migration and recovery | ChatGPT/Claude conversation export conversion preserving turns, branches and provenance | Canonical importer and commit service | Parser, import/reimport/export and hostile-input fixtures |
| P2-08 | B — Editor | Persistent per-page full width and presentation settings | Canonical page revisions, editor shell | Restart/history/export tests and browser interaction |
| P2-09 | A + F | Authenticated shell-independent host, exclusive owner and verified mode transfer | Kernel, authentication, snapshots, workers | HTTP authorization, canonical write, TLS boundary and transfer tests |

With four collaborating agents, C owns the three database packages; E/F owns graph, OCR and review; B/D owns conversation imports and display; A retains final responsibility for shared schemas, migrations, service contracts, headless hosting and integration. Every owner supplies tests and package documentation. These stable module roles continue the earlier workstreams rather than replacing them.

## Phase 0 contract definition and P2 extension

The normative original contracts remain [canonical domain](../phase-0/domain.md), [services](../phase-0/services.md), [jobs](../phase-0/jobs.md), [registries](../phase-0/registries.md), and [resolved decisions](../phase-0/decisions.md). UUIDv7 identities, decimal revision strings, page-backed records, typed values, registry-backed blocks, source locators, opaque file handles and single-owner persistence are unchanged.

P2 uses service transport version 1. Every canonical write carries workspace/request identity, a live scope grant, idempotency key and expected entity revisions. Services authenticate the actor; clients cannot assert an owner identity. Mutations commit atomically and publish the existing committed event envelope for search, memory, recovery and automation. Workers and parsers produce results/proposals; only workspace services commit them. Additive tables do not replace canonical pages, records, files or provenance.

The schema authoring source is `scripts/build_protocol_schemas.py`. Generated Go and TypeScript types are rebuilt together. Strict P2 requests cover database configuration/chart queries, page display, graph, OCR and personal reviews; P2 commands cover form submission, row actions, page preferences and review decisions. Contract fixtures are published in `contracts/examples/service-p2-*.json`. Package-specific limits are checked in the owning service as well as transport validation.

Native backups preserve additive package storage. Portable imports retain operational settings as evidence until explicitly configured locally; they never grant filesystem access or external-action permission. Display preferences and conversation structure have explicit canonical import handling. OCR and graph outputs carry source identities; accepted review text and form/button record values enter the ordinary content projection, retrieval and export paths.

## Integration order and completion

Identity/contracts → services/files/jobs → parser → reconciliation remains the migration order. Cited intelligence follows projections → retrieval/runtime → source validation → answers. Formulas retain stable records → relations → dependency evaluation. External effects retain services → permissions/revisions → tools → adapters.

Completion evidence covers persistence, UI, extraction, import/export, AI representation and relevant boundary tests per package. Automated service and browser evidence does not imply clean-machine, signed distribution, unrestricted OCR quality, or model quality beyond recorded fixtures. Consult each package's limits and the final verification report.

Feed/dashboard/map interfaces, a new annotation editor, reusable agent skills and AI autofill remain proposals. Hosting does not provide offline multi-device synchronization. The missing 307-behavior Notion research report remains an evidence gap; no complete audit-row traceability is claimed.
