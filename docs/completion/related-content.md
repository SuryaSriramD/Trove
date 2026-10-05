# Reviewed related-content surface (AI-003)

Implemented against source baseline `ce56f122d6970b745108fe51dcf012d462f117a7`. The source and evidence SHA-256 manifest in `evidence/completion/related-content-manifest.json` identifies this package independently of a later integration/cherry-pick commit. No schema or generated-contract changes are required.

## Product behavior

The editor's **Templates and related pages → Related content and possible duplicates** surface now uses the same `Suggestions` component as the AI panel. **Find related content** explicitly starts retrieval. Merely opening the panel performs no inference. Cards show a page title, source excerpt, evidence revision, and semantic/keyword label. Results are suggestions only: the implementation creates no links and applies no mutations.

Semantic candidates are distinct from normalized-text duplicate and shared-vocabulary heuristics. Repeated chunks are combined into one page card, and the current page is excluded. Unlinked title mentions and shared-word references have separate lexical labels. Opening either a retrieved card or a lexical reference calls `resolveCitation`; only a `current` source can navigate. Stale, deleted or inaccessible sources report an error.

Requests and citation resolutions are discarded when their component's workspace, grant, snapshot cursor, source page or source revision changes. Find clears the previous result and supersedes any pending navigation. Retrieval uses saved canonical content, as stated in the panel.

## Retrieval and access contract

- The semantic query uses the current title plus indexed body, capped at 4,096 UTF-8 bytes including the omission marker. Query truncation is disclosed.
- Only the explicitly configured embedding model is requested. A disabled model never invokes the worker, even if old vectors remain. Missing/unavailable or incompatible model generations use the existing labeled keyword fallback. A changed model or consent epoch during retrieval discards the semantic interpretation and performs a fresh keyword query.
- The internal `excludePageId` selector is validated and parameterized in both keyword and vector candidate queries, before their result limit. Malformed selectors fail; the selector never changes grant scope. Public search request schemas are unchanged.
- The top 200 chunks are bounded before deduplication into at most 20 related pages. Reaching either bound is disclosed. Vocabulary comparison retains its existing first-1,000-accessible-pages and 16,000-byte-per-page bounds; shortened pages are not called exact duplicates.
- Fresh canonical checks after embedding validate the live grant, source revision, candidate revisions, deletion and scope. Source changes or revocation fail the request. Changed/unavailable candidates are omitted with a diagnostic. Inactive memories and memories whose evidence changed or disappeared cannot be suggestion sources or candidates.
- A changed workspace event cursor is disclosed as potentially incomplete retrieval even if all remaining citations are current. Retrieval is bounded and does not claim exhaustive semantic coverage.

The connected graph defect is fixed using the same configured-model/epoch guard. Previously `graph.query` could choose retained vectors without consulting the disabled setting. It now excludes the focus page before retrieval limits and rechecks the explicit-link snapshot as well as retrieved nodes after embedding. A changed focus rejects the graph; changed nodes and their edges are removed with a warning. Keyword fallback edges retain their existing `keyword` kind. The graph UI and canonical links are unchanged.

## Acceptance evidence

Recorded on macOS 27.0.1 (`26A434`), arm64, using the pinned Go 1.27.1 toolchain, Node 25.5.0 and repository-locked frontend dependencies. Tests use temporary SQLite workspaces and deterministic embedding coordinates, without an external account or live model.

| Check | Evidence | Result |
| --- | --- | --- |
| Affected retrieval, graph, advanced editor and related-content service race suite | `related-content-final-race.jsonl` | 25 top-level tests and 24 subcases pass; package 28.785 s |
| Focused related-content/graph acceptance | `related-content-graph.jsonl` | 9 top-level tests and 19 subcases pass |
| Actual AdvancedPanel/Suggestions browser rendering through a controlled bridge | `related-content-browser.json`, `.txt` | 7 tests pass |
| Readable cards with product styles | `related-content-cards.png` | Inspected: distinct semantic/lexical sections, excerpt and revision visible, no raw JSON |
| TypeScript | `related-content-typescript.txt` | `tsc --noEmit`, exit 0 (empty successful output) |
| Production frontend | `related-content-build.txt` | Vite build, exit 0; existing chunk-size/timing warnings retained |

The service oracle deliberately places an inaccessible neighbor and more than 200 current-page chunks closer than an accessible conceptual page. The allowed conceptual page must still be retrieved. Tests also cover disabled/unavailable embeddings, source edit/delete, candidate edit/delete, grant revocation, forgotten/stale memories, malformed exclusions, and graph consent disable/re-enable with the same model but a new epoch.

Browser cases assert that no `ai.suggestions` request is sent before Find, cards are readable and deduplicated, self cards are absent, fallback labels/omissions are visible, current citations resolve before navigation, stale citations cannot open, four context changes discard pending results, and pending citation resolution cannot navigate a replacement page. The browser bridge is controlled fixture data, not a native Wails bridge or model-quality evaluation.

The initial fallback test fixture assumed broad term matching, but the existing keyword engine requires all literal query terms. The fixture was corrected to contain all those terms without changing production keyword semantics (`related-content-go-initial.jsonl` retains the failure). The first cold Vite browser run timed out on the initial page navigation while dependencies compiled; six other tests passed. Its report is retained as `related-content-browser-cold-start.*`; the completed warm runs passed all seven. Earlier passing service/race logs are retained alongside the final run.

## Reproduction

From the checkout root, use the pinned Go binary with `GOTOOLCHAIN=local`, `GOPROXY=off`, the pinned module cache and a writable disposable `GOCACHE`:

```sh
go test -mod=readonly -race -tags 'fts5 sqlite_omit_load_extension' ./internal/workspace \
  -run 'Test(Related|P2Graph|AdvancedAISuggestions|AdvancedEditor|Search|Vector|Semantic)' \
  -count=1 -timeout=3m -json
```

From `frontend`, run `node node_modules/typescript/bin/tsc --noEmit` and `node node_modules/vite/bin/vite.js build`. Start Vite on loopback, then run:

```sh
TROVE_TEST_URL=http://127.0.0.1:4187 \
TROVE_BROWSER_REPORT=../evidence/completion/related-content-browser.json \
node node_modules/@playwright/test/cli.js test related.browser.spec.ts
```

## Still unqualified

This package establishes implementation and fixture acceptance. It does not close AI-003's real-model quality evaluation or native candidate gate. A packaged macOS candidate still needs related-content checks with a compatible installed local embedding model, realistic personal content, native citation navigation, model settings/consent transitions and concurrent edits. Deterministic coordinates do not demonstrate embedding relevance or semantic accuracy. Windows/Linux and independent clean-install evidence remain separate release work.
