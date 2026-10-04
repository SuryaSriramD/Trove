# Phase 5 — P1 personal-use qualification

Trove 0.5.0 packages the seven P1 work packages with the existing offline core and paired local MCP relay. The release is a **personal-use candidate**. An automated pass qualifies the supported contracts below; it does not establish full TRD feature parity or close unobserved native-device and live-account acceptance gates.

## Reproduce the candidate

Use the pinned toolchain, frontend dependencies, built native helpers, and explicit model packs from the [baseline](../phase-0/baseline.md). The qualifier does not download anything or request account credentials.

```sh
/private/tmp/trove-phase0-venv/bin/python scripts/verify-phase5.py --app build/phase5-candidate-4/Trove.app
```

Choose a new unused app path. There is no skip option that produces qualifying evidence. Each attempt has its own `evidence/phase5/runs/<run>/verification.json`; [latest-run.json](../../evidence/phase5/latest-run.json) points to the most recent attempt. Failed and interrupted runs are retained. Source, native/model/dependency inventories, logs, browser reports and candidate files are hashed; source or bundle drift fails the run. The original P0/P1 preview artifacts remain intact.

After a successful automated aggregate, create the personal-use ZIP from its immutable run report:

```sh
/private/tmp/trove-phase0-venv/bin/python scripts/package-phase5.py \
  --verification evidence/phase5/runs/<run>/verification.json \
  --output build/releases/Trove-0.5.0-p1-candidate.zip
```

The packager verifies source, bundle and evidence hashes, all required gates, the ad-hoc signature and the extracted archive before retaining the ZIP and its SHA-256 manifest. The app, relay and native helpers must remain executable before and after extraction. It refuses existing destinations or stale/failed evidence. Archiving does not promote `releaseApproved`.

## Verification families

The executable selectors are in [phase5_policy.py](../../scripts/phase5_policy.py); the generated report gives each family's exact tests and logs, as well as the 4A–4G package mapping. A missing, skipped or failed required test cannot become a passing family. Offline tests require exact identities in the OS-denied phase, independently of online fixture passes.

| Family | P0 regression and P1 qualification |
| --- | --- |
| V01 Identity | Canonical P0 records; two sources in a container; linked gallery/timeline placement; shared edits, moves, retries and restart. |
| V02 Import reconciliation | P0 ZIP review/resume; inert HTML; API pagination, root consent, denied authentication, interrupted transport, redirects and explicit partial outcomes. Live API access is not inferred from injected transport fixtures. |
| V03 Provenance | Supplied people/place/time/comment evidence, unsupported payloads, formula source/cache, import captures and source mappings survive restoration without fabricated metadata. |
| V04 Editor | Existing clipboard/composition/structural browser tests; merged-table marks and original cells; tabs/ownership; reversible recovery and find/replace across marked runs. Native clipboard remains a separate gate. |
| V05 Formulas | Independent decimal/blank/type/Unicode/clock values; scoped relation traversal and empty aggregation; unsupported expressions, static dependencies, cycles and work limits. |
| V06 Recovery | P0 page/trash/record/asset checks; thirty populated P1/canonical tables compared field by field; native restart, backups, restored formulas/views/history/receipts and attachment-byte hashes. External authorization is renewed. |
| V07 Automation | Origins, atomic effects/receipts, replay, failed retries, skipped conditions, daily/weekly calendars, missed runs and half-hour DST. |
| V08 Offline operation | OS denial negative control; actual native models/transcription; P0 and P1 packaged create/restart/restore; empty-model-cache local workflows; exact P1 offline regression set. |
| V09 AI evidence | Stale/deleted/contradictory sources, exact quotes, scoped memory, current citations, reviewed database/code proposals and actual P0 local inference. General P1 model-output quality is not inferred. |
| V10 Tool boundaries | Canonical scope/revision/approval services, external-root confinement, paired MCP stdio subprocess and actual packaged relay against loopback HTTP/SQLite, transport failures, cancellation and revoked grants. |

Browser fixtures test renderer behavior; packaged fixtures drive real services through the Wails lifecycle. Both are retained because neither substitutes for the other. Native helpers run with actual installed models under OS network denial. Physical audio permissions and interactive controls remain separate observations.

## Optional integrations and local operation

| Integration | Available state and qualified boundary |
| --- | --- |
| Local models | Explicitly installed, verified packs; generation/embeddings/transcription stay local. Missing packs never trigger silent downloads or cloud inference. |
| Paired MCP | Opt in to selected pages/tools, then use the bundled `trove-mcp`. Proposal approval stays in Trove. The tested transport is loopback JSON-RPC over stdio; app exit/restart requires fresh pairing. |
| Notion capture | Explicit opt-in page/block capture preview with memory-only token and retained cursors. Fixture qualification does not establish live account compatibility or complete database/schema/view reconstruction. |
| Approved file/repository roots | Explicit native selection; bounded local indexing and persistent references. Full restore revokes access and removes cached snippets; select the roots again to resume. |
| Claude Code / Codex managed modes | Disabled pending real authentication, entitlement, distribution and OS containment qualification. No API billing fallback. |
| Remote MCP | Disabled pending transport and authorization qualification. |

Editing, databases, keyword search, local import/export, history, backups and schedules require no provider account. Schedules and index watchers run while Trove is open. Browser share/paste and readable PDF export retain the documented [Phase 4 limits](../phase-4/implementation.md). Unsupported formulas retain their original source/captured result instead of being presented as freshly computed.

## Fixes found by qualification

- Numeric formula literals and numeric property values now enforce the 4,096-bit budget before returning a value; exponent parsing is bounded before allocation. Dynamic `prop(...)` dependencies are rejected.
- Recurrence uses the actual timezone transition for repeated wall times, including Lord Howe's thirty-minute shift; repeated/fractional instants cannot duplicate local effects.
- Table replacement spans formatting runs while preserving unaffected marks and original merge evidence.
- Native acceptance exposed a false dirty state on locking a saved page. Lock changes now suppress synthetic editor updates; unlocking preserves pending drafts without attempting a write into the locked page. Browser regressions enforce real lock/revision rejection and preserve overlapping-edit conflicts.
- Packaged acceptance exposed a Wails startup race. Renderer requests and integration fixtures now wait for workspace initialization, with bounded waits, cancellation and explicit error diagnostics. Failed startup can recover by opening another workspace.
- A 10,000-page benchmark exposed quadratic validation of ordinary edits. Page and paragraph batches that cannot add graph edges now bypass whole-workspace decoding after lock checks; structural and mixed batches retain full ownership/reference/cycle validation. Allocation-scaling and ordinary-ancestor cycle regressions cover the boundary.
- Model settings now show a checking state until the native-runtime status arrives, instead of briefly reporting the runtime unavailable.
- Paired MCP can process cancellation while tool calls are pending. Concurrent request/notification counts and response sizes are bounded; malformed or failed transport stops without redirect or endpoint fallback. Large integer JSON-RPC IDs remain distinct.
- Clean restoration preserves filesystem references while revoking external-root read authority and clearing disposable snippets. Restored provider sessions remain disconnected.

## Upgrade and rollback

Before using this candidate with an existing workspace, create and verify a full native backup. The canonical workspace format remains 1 and base SQLite `user_version` remains 2; P1 tables are additive. Open a copy or restore into a new directory for evaluation. Do not downgrade a live P1 workspace in place. Roll back by reopening the previous app with its compatible pre-upgrade backup restored into a new directory. Model packs are separate from workspace backups. Local schedules and retained references are included; external root approvals and provider credentials are not reauthorized by restoration.

## Open release gates

Native UI evidence is recorded separately in [qualification.md](qualification.md): clipboard/save/reopen, screenshot permissions/cancellation, actual audio/playback, source review, selected-workspace restart and interactive restore require observations on the exact candidate. A clean independent Mac and Developer ID/notarization are unverified distribution limits. The local build is ad-hoc signed and targets macOS 27 Apple Silicon.

Full P1 scope remains limited by the package boundaries recorded above and in [Phase 4](../phase-4/implementation.md), including rich nested editor controls, full Notion database API reconstruction and general model quality in every new mode. Disabled optional providers do not impair the offline core. They also cannot be described as qualified managed integrations. The qualification report deliberately preserves these distinctions rather than promoting partial evidence to full release approval.
