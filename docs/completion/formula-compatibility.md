# Formula compatibility: evaluator v1 and v2

PARITY-005 is a bounded local compatibility contract. It does not claim unrestricted Notion formula parity. The app's **Formula editor and compatibility** panel exposes the supported function registry, preview diagnostics, result type, explicit preview clock/timezone and limits.

## Version and evidence rules

Existing native and imported definitions with no `raw.evaluatorVersion` remain evaluator v1. Import translation remains the established v1 translator. Unsupported source expressions retain their original text and imported cached value; a cached value is never labeled as computed.

Editing an existing formula in the panel explicitly opts into evaluator v2. A current successful preview against a selected record enables Save; changing expression, sample, clock, timezone or snapshot makes that preview stale. A database without records must acquire a sample record before using this editor. The initial property-creation form remains v1, followed by explicit v2 editing when needed.

`property.setFormula` requires the formula, collection, owning page and statically referenced property revisions. Scope, database locks and the existing dependency-cycle validator still apply. Saving preserves `raw.originalExpression`, `raw.importedCachedValue` and other import evidence. It stores the current `translatedExpression`, inert `translatedAST`, SHA-256 `expressionHash`, `evaluatorVersion: 2`, `editedAt` and `editedBy` separately. Reopening, export and backup retain the canonical property envelope. Formula preview writes no canonical values.

## Supported semantics

| Category | Contract |
| --- | --- |
| Types | Null, text, boolean, exact rational numeric intermediates, typed date, scoped page reference, external person reference, and homogeneous flat lists. Nulls may appear in a list. Result decimals round to 12 fractional places; intermediate numbers are bounded to 4096 bits. |
| Operators | `+ - * /`, integer `%` (remainder has dividend's sign), comparisons, strict `&& || !`. Text `+` concatenates. Equality distinguishes null, zero, false and empty text. No implicit text/boolean/number coercions. |
| Scalar functions | `prop`, short-circuit `ifElse`, `empty`, `length`, `lower`, `upper`, `contains`, `concat`, `abs`, `round`, `min`, `max`, `format`, `toNumber`. `round` ties away from zero. `length(text)` counts Unicode code points. `toNumber` accepts strict decimal text without locale separators. |
| Lists | `list`, `first`, `last`, `at`, `slice`, `join`, `unique`, `sort`, `reverse`; `length`/`contains` accept lists in v2. Indexes are zero-based; out-of-range `at` returns null. Slice is half-open with nonnegative start/end, clipped to length. Sort accepts numbers, text or dates, with null first; text ordering is code-point/UTF-8 lexical order, not locale collation. Unique retains first occurrence. No nested lists or callback/member-access expressions. |
| References | A relation property exposes scoped page references with ID, record ID and current title; `name`/`id` read a page/person reference. Hidden or unavailable targets produce a diagnostic instead of leaking their identity. Identity/author values retain external source IDs/display names without implying those people have a Trove account. Rich-text property runs concatenate into text. Other supported scalar property values remain typed; unsupported complex property inputs produce diagnostics. |
| Relation traversal | Configure a rollup property for cross-record property traversal, then reference that property with `prop`. Dynamic callbacks, arbitrary property-member access and recursive traversal are unsupported. Existing rollup aggregation and cycle rules remain in force. |
| Regex | `regexMatch` and `regexReplace` use bounded Go RE2 syntax. No lookaround/backreferences. Replacement is literal text, including `$1`; it is not capture substitution. |
| Dates | `date(ISO text)` creates a typed all-day or RFC3339 date in the explicit evaluation timezone. `dateStart`/`dateEnd` select a boundary; absent end returns start. `dateAdd` accepts elapsed minutes/hours for timed values, or calendar days/weeks/months/years. Calendar month-end clamps; nonexistent local wall times reject; repeated wall times choose the earlier instant. Results are restricted to years 0001–9999. |
| Date differences | `dateBetween(end,start,"minutes"/"hours")` measures elapsed time, including fractions; `"days"/"weeks"` measures civil-date boundaries in the explicit evaluation timezone. Fractional weeks are allowed. Month/year difference is deliberately unsupported. |
| Date formatting | `formatDate` accepts `YYYY MM DD HH mm ss Z` and non-letter separators. Locale month names and other proprietary tokens reject. `format(date)` returns its start ISO text. |
| Clocks | `now()` and `today()` keep v1's text results, using the explicit clock and timezone; wrap with `date()` for typed arithmetic. Preview requires an explicit instant/timezone. Query-time derived values keep the existing minute-stable clock and UTC default. |

Version 1 keeps its prior function/operator behavior, including unsupported v2 calls. In the v2 registry, `sinceVersion` identifies when a function name first appeared; list overloads for `length` and `contains` are v2 additions.

## Bounds and failure behavior

The compiler enforces 8,192 expression bytes, AST depth 48 and 1,024 nodes; evaluation has a 1,024-unit work budget. List operations and property-list reads consume work, so a 256-item list is not a promise that every combination of operations fits the budget. Lists contain at most 256 items. Regex patterns are at most 1,024 bytes, input/output text 65,536 bytes and replacement matches 1,024. Serialized v2 results are at most 65,536 bytes. Variadic legacy functions retain their 64-argument bound. A failed bound produces a useful diagnostic, not a partial list or silently truncated result.

A v2 evaluation memoizes property reads locally. Scoped relation target reads also consume the enclosing derived-query work budget and observe its cancellation context. The CORE-007 query cache's independent aggregate size/work limits remain authoritative. Error or unsupported imported formulas retain captured evidence; successful computed results and their evaluator version remain separate.

## Validation and qualification

Independent literal expected values cover every published function and operator, exact decimal math, Unicode, blanks, list behavior, reference display, regex errors/bounds, leap/month-end arithmetic, DST gaps/repeated times and fractional elapsed time. Service tests cover scoped relation denial, property revisions, cycle rejection, unsupported save rejection, source/cache preservation and reopening with computed v2 values. Browser checks cover failed/successful preview, save guards, explicit version selection, retained source presentation and invalidating a preview after editing. Existing v1 and rollup fixtures run alongside them.

This is implementation and development-test evidence. Signed build qualification and Windows/Linux qualification remain separate release gates.
