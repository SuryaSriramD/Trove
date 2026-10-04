# Notion API database capture — PARITY006

Implementation evidence: synthetic API fixtures and actual Chrome controls. No live Notion account was used or qualified. This extends the page/block capture described in `docs/phase-4/4c.md`; the older page/block-only limitation no longer describes new captures.

## Supported capture boundary

The owner selects explicit page, database-container, or data-source roots. The UI selects one root kind per capture; the service accepts a bounded mixed-root list. A database's supplied sources become distinct collections. Container links and linked views reference those collections without copying their records. Query results, templates and overlapping explicit page roots share one page identity. Root and descendant IDs never become local UUIDs directly.

New captures pin `Notion-Version: 2026-03-11` in durable state. Previously saved captures without a version continue with `2025-09-03`. Tokens exist only in the current request/renderer memory and are cleared after capture or inventory completion. Owner scope is required. Public IPC summaries omit raw captured objects; immutable response files and the stored capture retain the full evidence.

Each database, source schema, record query page, page body, property page, view inventory, view, template inventory, and failed attachment retry has a durable task identity. Property IDs are decoded and encoded once as one path segment; already encoded short IDs are not double encoded. Responses advance a cursor only with the capture revision CAS. Duplicate query records and repeated cursor rejection do not duplicate local records. Failed authorization remains pending and contributes retained error evidence. A denied or incomplete record query does not create an apparently empty canonical database.

The existing budgets remain: 20 explicit roots, 64 nesting levels, 10,000 tasks/items/responses, 128 MiB captured source bytes, 4 MiB per API response, 16 MiB per file, and 45 seconds per step. The field-level review inventory is also bounded at 10,000 items; oversized captures require narrower roots. No inaccessible relation target is fetched merely because a supplied reference mentions it.

## Native mappings and honest partial outcomes

| Source material | Result |
| --- | --- |
| Database / data source / page / property / view identities | Separate namespaced import mappings; local container, collection, record and view identities are distinct. |
| Title, rich text, numeric values, checkbox, date, URL/email/phone, select/multi-select | Complete supplied values map into typed local values. Decimal JSON numbers retain their exact digits. Invalid local values remain evidence with field-level outcomes. |
| Status | Explicit standard group labels map to local status groups. Unknown group semantics remain inert rather than guessing completion state. |
| People and authors | External identities remain source identities; they do not become local execution actors. |
| Relations | References within captured sources resolve to canonical records. Missing targets retain the supplied relation evidence instead of activating an incomplete relation. Reciprocal editing semantics remain an explicit partial outcome. |
| Formula | Original expression is retained. Only the existing supported v1 translator can supply `raw.translatedExpression`; there is no automatic v2 opt-in. Per-record `raw.importedCachedValue` comes from the property endpoint as exact source JSON text (`raw.importedCachedValueEncoding: source_json`), preserving digits beyond JavaScript precision. It remains separate from definitions and later local calculation. |
| Rollup | All property pages are read. Incomplete/unsupported results never become a number or zero. Definition and final captured result remain inert; local dependency/aggregation behavior is not inferred. |
| Files | Property arrays and supported media blocks download to managed local files. A failed download resumes by retrieving the source reference again; changed attachment identity blocks the retry. URLs outside the storage allowlist remain explicit partial outcomes. |
| Views | Table/list/board/calendar/gallery/timeline reconstruct compatible property references, visibility, widths, filters, sorts and layout. Linked views retain the same collection. Unknown result-affecting filters, dynamic quick filters, unsupported grouping, separate timeline end-date mappings, and chart/form/map/dashboard behavior become inert views, preserving their full source definition. Cosmetic settings without local equivalents remain a partial outcome. |
| Templates | Template inventory, page body, complete property defaults and default designation are captured. Templates are separate readable pages with inactive typed-default evidence, not database records or active template/recurrence definitions. Saving an active local template remains an explicit user review/edit operation. |

Unsupported fields retain their raw source responses. Source authors/timestamps and other metadata remain supplied provenance; importing does not turn them into the current local actor or local creation time. Comments, permission graphs, historical revisions, automation execution and unavailable source material are not claimed as captured. Known omissions and unknown authorization completeness remain separate in the import plan.

## Review, commit and re-import

Capture never directly writes the canonical content graph. Finish creates the existing owner-reviewed import plan, and ordinary guarded import commands perform the local mutations. Schema/record/view reconstruction uses the same domain validators and guarded collection/property/value/view handlers as other imports. Containers and source-kind mappings commit with the graph.

The connected Notion graph uses one safe re-import policy: **keep the local graph or import a separate copy**. Identical captures retain canonical identities and do not add records or views. Changed source evidence or local page revisions require review. A copy clones the whole connected graph and rewrites page ownership while binding new IDs once in the frozen plan. It does not overwrite the original source mappings. In-place source-schema migration is not inferred. A source identity imported after preview is rejected before initial writes and checked again at final graph commit.

## Verification

`internal/workspace/import_notion_database_test.go` exercises independently expected examples: two sources in one container; two linked views sharing one collection; duplicate query records across cursors; paginated encoded-ID rich text; exact decimals; canonical cross-source relations; separate formula expression/cache; two files with an expired download and source-reference refresh; saved-cursor restart and stale-revision rejection; inactive template body/defaults; unchanged re-import; reviewed whole-graph copy; overlapping page/database roots; denied query versus empty data; repeated property cursors; legacy API-version resume; six view mappings and inert unsupported result semantics.

`frontend/tests/notion-capture.browser.spec.ts` mounts the actual capture component in Chrome with a fixture IPC bridge. It checks typed authorized roots, blocked property cursor display/resume, token clearing, and complete/explicit-partial inventory review. This is UI integration evidence, not a live vendor test.

Full workspace and contract Go suites, TypeScript checking, and the two Chrome cases are run for this package. Root integration should regenerate authoritative schemas/consumers together with the concurrent formula/job protocol additions.

## Primary API evidence checked 2026-10-04

- [2026-03-11 upgrade guide](https://developers.notion.com/guides/get-started/upgrade-guide-2026-03-11): version boundary and changed trash terminology.
- [Retrieve database](https://developers.notion.com/reference/retrieve-database) and [retrieve data source](https://developers.notion.com/reference/retrieve-a-data-source): container/source separation and schema ownership.
- [Property item objects](https://developers.notion.com/reference/property-item-object): short encoded IDs, paginated values, final rollup state, and unsupported calculations.
- [List views](https://developers.notion.com/reference/list-views) and [working with views](https://developers.notion.com/guides/data-apis/working-with-views): view references, linked-source ownership, filters and layout fields.
- [List data-source templates](https://developers.notion.com/reference/list-data-source-templates): bounded template inventory, default designation, pagination and authorized template-page retrieval.

Firecrawl developer-index was attempted first but its service DNS was unavailable; official Notion documentation was inspected with the web tool. The implementation does not rely on unverified vendor/account behavior beyond those published contracts. Live-account qualification remains outstanding.
