# Phase 6 — Conversation import and page display

Owners: D (migration/recovery) and B (editor/desktop). Shared contracts and integration remain owner A's responsibility. These are independent P2 packages; neither requires hosted services.

## Conversation-export import

The ordinary Import screen accepts JSON directly, in a ZIP, or in a selected folder. Preview recognizes these explicit formats:

- ChatGPT: an object or array of objects with `mapping`, `id` or `conversation_id`, `title`, and optional `current_node`. Each mapping node has `parent`, ordered `children`, and an optional `message` containing `id`, `author.role`, `create_time`, and text content.
- Claude: an object or array with `uuid`, `name`, and ordered `chat_messages`. Messages contain `uuid`, `sender`, `created_at`, `text` or text-bearing `content`, and an optional `parent_message_uuid`. When parent IDs are absent, supplied array order defines the linear conversation.

Each conversation becomes a page with readable message headings, roles, source IDs and timestamps, plus canonical parent/reply block links. All supplied branches are retained, including structural nodes. The selected ChatGPT node is labeled. Provenance records provider/conversation/message identities, ordered edges, timestamps, canonical heading IDs and the retained source file. The original JSON remains an exact managed attachment; metadata, images and tool payloads are not executed or invented as text. Missing attachments cannot be reconstructed from pointers.

The parser is pure: `ParseConversationExport` creates canonical import objects; inventory retains bytes and existing workspace import services perform all commits. Identity uses provider plus conversation ID, under the user-selected source namespace. A renamed export reconciles to the same page. Changed source/local content requires the existing keep-local, replace, or duplicate review. Source hashes are per conversation. Ordinary import cancellation, durable cursors, background jobs and committed events are reused.

Limits are 256 conversations per JSON document and 2,000 nodes per conversation, in addition to the importer’s 64 MiB/member, 1 GiB/archive and 10,000-item limits. Duplicate IDs, missing parents/children, inconsistent edges, cyclic graphs and excessive graphs retain the source as an unsupported item with a reason. No partially decoded conversation is silently marked complete. The source completeness report explicitly distinguishes supplied data from unknown provider omissions.

Message text uses standard canonical blocks and therefore participates in existing extraction, keyword/semantic retrieval and cited AI. Source-only metadata stays outside text projections. Markdown/HTML portable exports preserve readable text, exact canonical blocks, links, provenance and original files; canonical import remaps navigation links to new local IDs. Native backup retains all source files, import plans/mappings and page history.

## Page display preferences

Page options now contains **Full width** and **Presentation layout**. Full width uses available content width. Presentation layout hides navigation/editor chrome and provides an explicit exit button; it remains a layout preference, not a content lock. Page locking is the separate existing control.

`pageDisplay.get` returns the authorized page’s settings and current page revision. `page.display.set` runs in the canonical batch service with page/record guards, permission checks, history capture, page locking and committed change events. Its two boolean settings are persisted in the additive `p2_page_display` table. Defaults are false/false. No browser-local preference is used as authority.

Settings survive application restart, native verified backup, reviewed history restoration and portable export/import via each manifest page’s `displayPreferences`. Older history without this field preserves current settings. Display preferences never enter extracted text or change citation coordinates; content and its AI representation retain their meaning.

## Acceptance evidence

`internal/workspace/p2_migration_editor_test.go` verifies:

- ChatGPT multiple branches, source identity, role/timestamp/current-node provenance, readable navigation and searchable branch text.
- Exact retained source bytes, stable re-import identity after renaming, canonical portable round trips, and inert non-text/tool payloads.
- Claude ordered parent/reply reconstruction and explicit malformed graph retention.
- Display defaults, authorized mutation/read scope, revision advance, history restoration, portable import, native backup verification and reopening.

`frontend/tests/p2-display.browser.spec.ts` verifies full-width styling, presentation chrome/exit and persistence after browser restart against service contract fixtures. Existing browser fixtures supply default display settings. The browser fixture complements real service persistence tests; it is not a packaged native UI qualification.

Run:

```sh
PATH=/private/tmp/trove-phase0-tools/go/bin:$PATH \
GOCACHE=/private/tmp/trove-phase0-tools/go-cache \
GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod \
go test -tags sqlite_fts5 ./internal/workspace -run 'TestConversationExport|TestPageDisplay' -count=1
```

Both package boundaries are implemented; the explicit formats/limits above bound compatibility. No unsupported claim is made about untested future provider export revisions or data absent from the source. The missing separate 307-behavior research report remains a traceability gap.
