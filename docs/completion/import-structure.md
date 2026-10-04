# Nested import reconstruction (PARITY-007)

The import adapters preserve supplied source relationships before canonical
commit. Source files and complete API responses remain inert evidence. These
changes are qualified with synthetic fixtures; no live Notion account or real
export corpus has been qualified by this work.

## Reconstructed structure

- HTML details/summary becomes a toggle with explicit open/closed state. Nested
  list items, paragraphs, quotes, code, tables and managed media retain canonical
  parent ownership instead of being appended to their ancestor's text.
- Explicit `column-list`/`column` HTML groups retain direct child membership and
  supplied percentage widths. Semantic tab lists linked by `aria-controls` to
  direct tab panels retain titles and child groups. Explicit `data-trove-layout`
  groups are also understood. Arbitrary CSS positioning and JavaScript layouts
  are not inferred or executed.
- Markdown indented unordered/ordered lists, continuation blocks and fenced code
  retain their hierarchy. Fenced source keeps indentation relative to the fence.
  Markdown does not supply toggle/column/tab semantics merely because text is
  indented, so these are not invented.
- API children endpoints supply source parent identity. Supported paragraphs,
  lists, quotes, toggles, toggle headings, callouts, code and media retain that
  relationship. Column wrappers become groups owned by their column container.
  Same-page synced references point to a captured original source; unavailable,
  cross-page and cyclic reusable references stay readable inert evidence. Such
  references do not cause requests outside the selected capture roots.
- Canonical portable round trips include toggle headings as well as existing
  toggles, columns, tabs and same-page reusable references.

## Identity and review

Each plan records local block IDs alongside explicit source IDs or positional
evidence. Existing Notion block IDs and explicit HTML element IDs are reused on
reviewed replacement only when the corresponding block still belongs to the
mapped page. Imported canonical block IDs receive the same treatment. Anonymous
DOM paths and Markdown line positions are not treated as durable source IDs
after edits. Separate-copy choices always allocate independent local block IDs.

Unchanged API capture hashes use original source hashes and parent identities;
generated UUIDs and local capture timestamps do not create false conflicts.
Identical source plus an unchanged local page keeps existing content. Incoming
changes or local edits require ordinary re-import review and revision checks.
Each run still has its own retained inventory and import root.

Reviewed replacement updates matching block identities, parks old ownership
inside the transaction, removes only obsolete blocks, and reconstructs the new
parent-first graph. Normal registry, reference and cycle checks run before commit.
Pre-change content remains in page history. Source blocks moved elsewhere are
not stolen back or overwritten by identity reconciliation.

## Evidence and remaining limits

Every API block gets isolated exact JSON evidence alongside the complete response
inventory, so a readable unsupported block need not reference a multi-item batch.
Unsupported children still retain ownership beneath their readable fallback.
Supplied mention/user metadata is explicitly partial: its readable label is
imported and the full identity data stays in source evidence. Unsupported place,
comment and transcript representations remain raw/readable evidence; missing
comments, timings, participants or source relationships are never fabricated.
Known unsupported fields and unknown source completeness remain separate report
categories. Permission failures remain blocked/failed source reads.

The existing toggle-heading registry has no original heading-level field;
toggle ownership is reconstructed and the source heading level is reported as
retained evidence. API database schemas/views/property pagination remain a
separate adapter task. API-specific tabs are not claimed when no supported
payload supplies them. HTML panel detection requires explicit semantic grouping.
Arbitrary layouts and vendor-specific Markdown extensions remain unqualified.

`import_structure_test.go` independently checks expected parents, group members,
widths, labels, rich marks, code whitespace, unsupported outcomes, exact retained
source, reopen, portable export/re-import, identical re-import, reviewed identity
reuse, repeated replacement and independent duplication. Fixture HTTP requests
also prove missing/cyclic synced references do not fetch an external source.
