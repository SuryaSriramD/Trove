# Phase 4 — P1 development preview

The seven work packages are implemented as a development preview on the existing canonical workspace kernel. Three collaborating agents handled editor, databases/automation, and migration/capture; the integration work adds local AI, provider adapters, desktop lifecycle wiring and verification. The prior P0 candidate and its recorded qualification remain separate artifacts. This source change does not promote any unfinished P1 gate into a release claim.

| Package | Delivered | Evidence and limits |
| --- | --- | --- |
| [4A editor](4a.md) | H4, reversible merges, tabs/columns, reusable blocks, offline cards, navigation/mentions, reviewed buttons, locks, replacement, templates and references | Canonical codec/backend/browser fixtures; rich nested layout controls and deep-link shell routing remain limited. |
| [4B databases](4b.md) | Relations/reciprocals, rollups, bounded formulas, shared/gallery/timeline views, record hierarchy/dependencies/templates/locks and identity metadata | Independent decimal/clock/cycle fixtures; published compatibility matrix, no complete proprietary formula parity claim. |
| [4C migration/export](4c.md) | HTML ZIP, folders/documents, optional Notion capture, offline HTML ZIP and readable PDF | Preserved original evidence and accurate partial outcomes; live account/API parity and full-fidelity PDF layout remain unqualified. |
| [4D capture/index](4d.md) | Reviewed browser share/paste, interactive screenshots, approved-root file/repository indexing | Scope/exclusions/move/delete fixtures; native screenshot permission/UI and full AST language parsing are not established. |
| [4E automation](4e.md) | Local event/condition/action rules, recurrence, previews/history/retry/pause, atomic dedup, origin rules and missed-run handling | Restart/DST/failure fixtures; runs only while Trove is open; initial form exposes a subset of richer service actions. |
| [4F local AI](4f.md) | Structured databases, Mermaid, code and organization drafts, reviewed broader actions and related/duplicate suggestions | Registry/revision/approval tests; installed-model quality for every mode needs separate assessment. |
| [4G adapters](4g.md) | Paired local MCP, reviewed proposals, cancellation/revocation, provenance, handoff previews and separate Claude/Codex protocol adapters | Managed vendor modes and remote transport are disabled pending real authentication, distribution, entitlement and OS containment qualification. |

## Verified development build

The final [verification run](../../evidence/phase4/runs/20260930T180437Z-f9ad4ca0/verification.json) passed all eight checks on 2026-09-30: 49 Python contract tests, 242 Go test/subtest results across eight packages under the race detector, TypeScript checks, 16 frontend unit tests, 28 application browser tests, three database browser tests, packaging and unchanged-source verification. The browser runs had no skips or retries. The new ad-hoc-signed preview is `build/phase4-preview-2/Trove.app` and includes the paired MCP relay. This qualifies the recorded development checks, not the remaining live-account, native-device, vendor-distribution or release gates.

## Run

Use the pinned dependencies and Python environment described in the repository baseline. No new model pack or vendor account is needed for local tests.

```sh
/private/tmp/trove-phase0-venv/bin/python scripts/verify-phase4.py --app build/phase4-preview-next/Trove.app
```

Choose an unused application path. The verifier records Python contracts, Go race tests, frontend type/unit/browser suites, source hashes and packaging logs under `evidence/phase4/runs`; `evidence/phase4/latest-run.json` points to the most recent attempt. A successful run is implementation evidence, not approval of all P1 shipping gates. Browser checks use mocked service responses; service integration tests use real temporary SQLite workspaces. Existing native-model qualification is not rerun by this P1 development runner.

The built preview includes `Contents/MacOS/trove-mcp`; pair a client in Providers to obtain its configuration. The endpoint is created only after context approval, binds only to loopback, and expires at app exit. Both vendor selectors explain their unavailable state. Local operation has no provider dependency.

## Persistence and origin boundaries

New services register additive tables during workspace open. Native workspace backups include them. Portable export includes a `phase4Metadata` envelope for page locks, immutable templates, button definitions, database locks/hierarchy, rule definitions and external source references. Re-import retains such operational definitions as inert evidence; imported rules or filesystem roots are never silently authorized. Canonical blocks and supported references use the ordinary schema, history, search and export contracts. Supported portable database schemas, typed values, formulas, relations and views are reconstructed together in a final guarded transaction after imported page identities exist; unsupported or incomplete evidence remains explicit and retained.

Database and automation mutations use generated command schemas. Formula caches are visibly distinct from live calculations. Automation effects and their deduplication rows share the canonical transaction, with event origin `automation`; reviewed AI changes use `ai`. Import and restore do not become eligible user changes. Revocation is checked at the transaction commit fence. Providers never receive direct SQLite access.

The requirements ledger maps every P1 row to a Phase 4 package and marks product completion conservatively as partial, with focused implementation evidence documented separately. Remaining qualifications are explicit in each package document; broad gate completion cannot be inferred from a unit-test pass.
