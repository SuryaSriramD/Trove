# Phase 6 — optional database packages

Owner C retains database ownership. Owner A owns the shared service schemas and migration/contract integration. These packages depend on stable collections, typed properties, canonical record mutations, authorization and revision fencing; they are independent of OCR, knowledge graph, review scheduling and hosting.

## Delivered workflows

| Package | Local workflow | Acceptance boundary |
| --- | --- | --- |
| Charts | Expand **Charts, local forms and row buttons**, choose a grouping property, aggregation and bar/donut display, then save. Read the accessible value table and open source record pages. | One saved chart per collection, calculated from all current authorized non-trashed records in a read transaction. Supports count, sum, average, min and max; grouped by title, text, select, status, checkbox or date. Numeric, computed formula and computed rollup values may be aggregated. |
| Local forms | Enable the form, choose required fields and save; enter a title and typed fields, then submit. | One form per collection; validated atomic creation of a canonical record and page. Uses local workspace services only. No hosted endpoint or public share link is required or created. |
| Database row buttons | Configure a label, property, value and confirmation preference. Run the button beside a particular record. | Up to sixteen saved actions per collection, each setting one supported field on that record. A confirmation dialog names the record, property and value. Cancel performs no mutation. Service validation independently rejects an unconfirmed action configured to require confirmation. |

Existing database layouts remain available. Charts intentionally cover the collection, including rows omitted by the selected view; this is stated beside the chart. Group tables expose exact displayed decimal values and source navigation. Donut display rejects negative aggregates and directs users to bars. Bar widths are normalized with integer arithmetic before converting bounded ratios into SVG coordinates, including decimals too large or too small for JavaScript numbers.

## Contracts and persistence

The shared contract owner added the following commands to `BatchRequest` and strict query schemas:

- `database.p2Configure`: `collectionId` and the full chart/form/button configuration. Updates require the collection revision guard.
- `database.formSubmit`: `collectionId`, allocated `recordId` and `pageId`, `title`, and typed property/value pairs. Requires collection, new record/page, and submitted-property guards.
- `database.rowButtonRun`: `collectionId`, `recordId`, `buttonId`, and `confirmed`. Requires collection, row, page and affected-property revision guards. The service loads the saved action; the caller cannot override its destination or value.
- `databaseP2`: scoped collection configuration, revision, readable representation and resource limits.
- `databaseChart`: scoped collection aggregation, revision, source record IDs, exact decimal values and exact rational fractions.

Additive initialization creates `p2_database_config` keyed by collection and `p2_form_submissions` keyed by record. The latter records the configuration's collection revision and submission timestamp. All writes occur inside the workspace `Mutation` transaction. Database locks, stale revisions, idempotent replay, canonical change events and non-user exact proposal approvals continue to apply. Cross-collection button destinations and unsupported form fields are rejected.

Successful form submissions and row actions create/update ordinary record values. Existing content projection extracts those values into search after the committed dirty-page event; retrieval and cited AI read the canonical record content. `databaseP2` returns a readable account of configuration and actions; `databaseChart` returns traceable sources and the interpretation of nulls and averaging. Neither treats imported cached formulas as newly computed values.

Native backups preserve live settings and submission provenance. Portable exports include `p2DatabaseConfigs` and `p2FormSubmissions` in the versioned additive metadata. Portable import preserves that metadata as **inert source evidence**, following the existing rule/action policy: record data and properties are reconstructed, while imported controls require explicit review and recreation. Portable import does not silently activate row actions. The original IDs and configuration are retained for reconciliation. Settings are not independently restored by page-body history; use native workspace recovery for the live configuration.

## Defined limits

- At most 10,000 non-trashed collection records and 100 chart groups. Exceeding either produces an explicit resource error, never a truncated aggregate. A chart error leaves its settings editable.
- Sum/min/max use rational arithmetic and preserve the input decimal scale. Count is an integer. Averages show at most twelve decimal places and include `exactFraction` for full mathematical provenance. Null numeric values do not enter sum/average/min/max; an all-null group has no numeric aggregate. Count includes every authorized record.
- Derived calculations retain the shared evaluator's 10,000-operation and relation limits. Failed, unsupported or captured derived values stop numeric aggregation with an explicit diagnostic.
- Forms accept at most 64 supplied fields; the required list has at most 64 properties. Supported types: text, number, select, multi-select, status, checkbox, date, URL, email and phone. Title is always required. False and numeric zero satisfy required fields; blank text and empty selections do not. The UI date field creates an all-day value with an explicit local timezone.
- Configurations are capped at 64 KiB and 16 row buttons. A button is a same-row property assignment. External providers, arbitrary scripts, destructive actions, cross-record relations and file attachment actions are outside this package.
- One chart and one form per collection are supported. Saved view-specific charts, hosted forms and dashboard/feed/map interfaces remain outside this delivery.

## Acceptance evidence

Backend tests in `internal/workspace/p2_database_test.go` cover precise signed decimals beyond IEEE-754 range, nulls and repeating averages, source scope isolation, resource rejection, persisted configuration after restart, atomic invalid form rollback, committed property extraction, submission provenance, disabled forms, wrong-collection fields, confirmation, selected-record scope, stale saved actions, idempotency, non-user approvals, database locks and portable evidence retention.

Frontend unit tests in `frontend/tests/p2-database.test.ts` cover extreme decimal chart normalization, signed donut rejection and required-field semantics. The database browser suite adds three P2 workflows: chart source navigation and saved display after reload; required form validation followed by one guarded submission; and cancelled/confirmed row actions with the exact revision guard set. The same suite preserves the existing table/list/board/calendar regression tests.

Run the focused checks from the repository root:

```sh
PATH=/private/tmp/trove-phase0-tools/go/bin:$PATH GOCACHE=/private/tmp/trove-phase0-tools/go-cache GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod go test -tags sqlite_fts5 ./internal/workspace -run '^TestP2(Charts|LocalForms|RowButtons|Portable)' -count=1
(cd frontend && node --experimental-strip-types --test tests/p2-database.test.ts)
(cd frontend && ./node_modules/.bin/tsc --noEmit)
(cd frontend && TROVE_TEST_URL=http://127.0.0.1:4176 ./node_modules/.bin/playwright test --config src/database/tests/playwright.config.ts)
```

Browser tests use the real React UI and a shared-contract service fixture. Backend integration tests separately exercise real SQLite persistence and authorization. The fixture is not evidence of native shell packaging or hosted multi-client behavior.
