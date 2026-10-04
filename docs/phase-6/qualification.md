# Phase 6 qualification

The nine optional packages are implemented as a bounded development preview. The [backlog, stable assignments and contract definition](implementation.md) identify ownership and dependencies; [databases](databases.md), [intelligence](intelligence.md), [conversation import/display](migration-editor.md) and [hosting](hosting.md) state the supported workflows and limits. Product scope outside those boundaries remains separately tracked in the requirements ledger.

The [final verification run](../../evidence/phase6/runs/20261001T131342Z-3b67a87a/verification.json) passed all ten checks on 2026-10-01: 59 Python tests, 337 Go test/subtest passes under the race detector, TypeScript compilation, 21 frontend unit tests, 34 application browser tests, six database browser tests, packaging, host build, the seven-step real host journey and unchanged-source verification. Browser tests had no skips or retries. One opt-in native OCR test was skipped in the ordinary Go suite and passed separately against the actual helper.

An additional [packaged-host browser smoke](../../evidence/phase6/host-ui.json) passed against the exact packaged executable and frontend: authentication, page creation/rename, persisted full-width settings and graph navigation produced no uncaught browser errors. The [screenshot](../../evidence/phase6/host-ui.png) records that real workspace UI.

The verified build is `build/phase6-preview-1/Trove.app` (0.6.0), containing the desktop, headless host, paired MCP relay and OCR helper. [Artifact hashes](../../evidence/phase6/runs/20261001T131342Z-3b67a87a/artifacts.json) identify those exact binaries. The application passes deep/strict ad-hoc signature verification. This establishes the stated development acceptance checks, with the distribution and capability limits below.

## Acceptance coverage

| Package | Persistence / service | UI | Projection and portability |
| --- | --- | --- | --- |
| Charts | Collection-guarded settings; exact arithmetic; scoped current records | Bar/donut display, exact values and source navigation | Readable calculation/source representation; settings retained in native snapshots and inert portable metadata |
| Local forms | Required typed fields; canonical atomic record creation; submission provenance | Local configuration and validated submission | Canonical record extraction, AI/retrieval, export and restoration |
| Row buttons | Stored own-row action; scope, revisions, confirmation, idempotency and non-user approval | Review, cancel, confirm and stale-review invalidation | Canonical record changes; portable definitions remain inert |
| Knowledge graph | Scoped projection of explicit links and labelled retrieval associations | Visual graph, evidence disclosure and page navigation | Uses canonical locators; graph regenerates from imported/restored content |
| OCR | Authorized immutable file descriptor, bounded local worker and revision-checked derived text | Availability, selection, extraction and quality warnings | Searchable image-linked text; original image portable, native extraction retained |
| Personal review | Hash-bound local activation, daily/weekly recurrence, receipts, deduplication, failure isolation, source/memory validation | Plan controls, readable drafts, explicit approve/dismiss | Canonical plan/digest blocks; imported plans inactive; native schedules/receipts retained |
| Conversation import | Canonical parser/service separation; roles, branch graph, timestamps and original evidence | Existing reviewed importer with JSON selection | Searchable pages and turns, stable reimport, portable roundtrip/provenance |
| Display preferences | Guarded per-page settings; restart/history/native and portable restoration | Full width, presentation mode and exit | Presentation stays separate from content; canonical page text remains retrievable |
| Hosting | Token authentication, origin/cookie/TLS policy, exclusive owner, verified snapshot transfer | Compiled UI browser bridge and bounded client uploads | Same kernel, local files/jobs, explicit worker packs and snapshot inventory |

Independent review corrected transfer-fence failure retention, destination-marker durability, review-plan failure isolation and a memory-content/revision race. Regression tests cover those failure cases.

## Native evidence and limits

The actual Apple Vision helper recognized both printed fixture lines exactly under OS network denial. Its JSON evidence preserves the fixture/helper hashes and the initial cold-start timeout. The actual Go adapter subsequently completed in 31.97 seconds under the final 60-second deadline. See `evidence/phase6/local-ocr-smoke.json` and `evidence/phase6/local-ocr-adapter-tests.jsonl`. This evidence establishes that specific local printed-text case, not broad OCR quality. General image-description models are not included.

Personal review is a deterministic cited digest with an explicitly approved append action. It does not claim generative autonomous task management. Charts/forms are saved local collection controls; row buttons set one supported field on their selected row. Portable operational definitions remain inert until reconfigured locally. These are explicit supported boundaries, not claims of complete proprietary feature parity.

The host is one authenticated workspace owner. The automated journey exercises a real process, HTTP edits, file upload/download, restart, rejection of a second owner, durable source fencing and verified transfer. It does not qualify arbitrary remote infrastructure, network filesystem locking, Windows/Linux distribution or offline multi-device synchronization. Native desktop signing/notarization and independent clean-machine installation remain separate from this ad-hoc development build.

The separate 307-behavior Notion research report and fixtures were not supplied. Full audit-row traceability cannot be claimed. Feed/dashboard/map interfaces, a new annotation editor, reusable agent skills and AI autofill remain proposals.
