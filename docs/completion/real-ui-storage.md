# Real UI and storage integration checks

`scripts/run-real-ui.py` drives the compiled UI through Chromium against the production `trove-host` packaged inside an existing macOS app. It creates a new local APFS workspace, authenticates through the existing owner-token login, and changes content only through visible controls. Read-only production snapshots independently check exact canonical values and identities. The host is stopped normally and started again before the reopen checks.

Suite version 8 retains all thirty version-7 case identities and assertions, including no additional canonical pages/blocks or omitted-root navigation after a no-op import. Seventeen independent workflows run before and after a real host restart:

- Unicode rich-page creation with bold formatting.
- Task creation, rename, completion and Tasks-to-Home consistency.
- Due-date editing through Tasks, Home and the record page, with explicit timezone and rejected invalid timezone preserving canonical state.
- Database record promotion with explicit due-property binding, detachment cancellation/application, and grouped undo preserving a later title edit and stable record/page/property identities.
- Collection layout, record override and reset preserving record values and the existing review event; a subsequent date-value edit makes that same review stale. The automated review event tests persistence, not human acceptance of content.

- Required and typed local form validation, followed by valid submissions and exact large/signed decimal chart aggregates linked to their source records.
- Reviewed row action cancellation, stale-review invalidation after a date edit, atomic application and grouped undo retaining the later date and title.
- Branched conversation JSON import through the file picker/upload, exact source-byte retention, stable message/parent/reply identities, and full-width/presentation settings preserving content and the review event.

- Working-week dependency scheduling with a complete branch/slack preview, cancellation, stale-plan rejection, atomic application and grouped undo retaining unrelated status/title edits.
- Explicit calendar-day versus Sunday-only scheduling across Berlin fall DST, preserving 25 elapsed eligible hours and showing offset effects.

- Moved HTML source identity explicitly linked to its existing page, retained-base field conflicts, local/remote merge, stable reordered block IDs, stale-review rejection and idempotent re-import.
- A 48-item ZIP import paused by the visible control, with its durable cursor and partial contents preserved across host restart before resuming exactly once.

- Skill validation, version history/restoration as a new version, duplication, archiving and inert JSON import. Exact saved source/tool selections survive restart; manual preview binds a selected subset without starting inference or creating an AI run.
- A deterministic personal-review plan with two explicit sources, dismissed and stale drafts, atomic approved append preserving unrelated content, pause/reactivation and final paused persistence. Generation remains disabled; this does not qualify model analysis or background scheduling.
- Two explicit page links, graph edge evidence tied to source block/revision, navigation through evidence/backlinks, keyboard deletion of one link and exact retained/removed edge state after restart. Retrieval associations remain disabled.

- Rich table creation through visible controls, exact bold/link/color/highlight preservation, a genuine caret edit with persisted outer undo/redo, merged-cell source evidence and restoration, populated-row cancellation/removal/undo, stable cell identities and unchanged unrelated content.
- Rich column creation and editing, stable group/child identities during group moves and removal/undo, duplication and confirmed removal/recovery, navigation and exact restart persistence. Expected mutation revisions and sibling order are checked alongside values; these cases do not inject editor commands or clipboard contents.

The reopen cases compare exact canonical values, identities, operation state, layouts and review history with saved pre-restart observations. These are bounded UI/service/storage checks. Native Wails rendering, input methods, accessibility, microphone, reminders, AI quality, live accounts, performance and broader feature workflows require their own checks.

Run from the desired checkout with existing pinned dependencies available in `frontend/node_modules`:

```sh
python3 scripts/run-real-ui.py \
  --app /Users/your-name/Applications/Trove.app \
  --evidence /private/tmp/trove-real-ui-unused-run \
  --go /path/to/pinned/go/bin/go \
  --node /path/to/node
```

The app is read-only and must have valid nested signatures and clean Go build provenance. No model packs are loaded. The browser uses the packaged frontend by default. `--assets /absolute/compiled/frontend` explicitly tests a separately built frontend overlay against the installed host; the manifest identifies that boundary and hashes the actual served assets separately from the installed app. An overlay result is not an installed-app result. `--host /absolute/standalone/trove-host` selects an explicitly built production host whose embedded clean Go source revision must equal this clean checkout. The manifest separately binds the installed app host and the executing host. Use this for a current-source service and UI fixture checkpoint; an older installed host cannot establish acceptance of newer services. The runner does not compile, install, register, update or create app bundles.

Each evidence path must be unused. `manifest.json` uses schema version 2 and binds the checkout, installed and executing host sources and binaries, full app inventory, served assets, native helpers, frontend source snapshot, runner/config/spec files, fixed fixture, expected cases and actual reports. Results distinguish failed, skipped, interrupted, invalid and unexecuted cases. The next process stage is not attempted after a failed creation stage. Zero retries are configured; repeated or undeclared results cannot pass. Host lifecycle failure or changed inputs prevents an overall pass even if individual assertions passed.

Every child receives a fixed environment allowlist, with a fresh private HOME, TMPDIR, configuration/cache paths and an empty model root. No provider credentials, proxy settings or parent app state are inherited. Host processes start in the private temporary directory. Playwright keeps the frontend working directory for configuration and must receive an absolute `--node` path when Node is not in the fixed system PATH. Private filesystem state does not isolate macOS Keychain, notifications, login-item registration or Cocoa preferences; this lane does not exercise those boundaries.

The private token is created with mode 0600 and removed at cleanup. Browser traces are disabled to keep login input out of trace archives. The runner retains only its disposable workspace, logs, screenshots, canonical observations and manifests; it does not select or modify the user's normal workspace or preferences. It refuses non-local/non-APFS workspace output. Do not reuse or edit a previous result directory to rerun a failed case.

The lane has its own `frontend/playwright.integration.config.ts`, so ordinary mocked component tests do not silently become storage tests. `tests/test_real_ui.py` verifies that missing, skipped, retried, duplicate, unexpected and expected-failure results cannot be counted as passes. A successful fixture remains a bounded automated observation, separate from human acceptance and release qualification.

The host keeps its own admission limit. A visible host-busy error fails the corresponding workflow; the runner does not retry the case or interpret a subsequent unrelated pass as recovery.
