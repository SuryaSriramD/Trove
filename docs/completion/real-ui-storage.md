# Real UI and storage integration checks

`scripts/run-real-ui.py` drives the compiled UI through Chromium against the production `trove-host` packaged inside an existing macOS app. It creates a new local APFS workspace, authenticates through the existing owner-token login, and changes content only through visible controls. Read-only production snapshots independently check exact canonical values and identities. The host is stopped normally and started again before the reopen checks.

Suite version 2 retains the original four rich-page/task case identities and adds six cases. Five independent workflows run before and after a real host restart:

- Unicode rich-page creation with bold formatting.
- Task creation, rename, completion and Tasks-to-Home consistency.
- Due-date editing through Tasks, Home and the record page, with explicit timezone and rejected invalid timezone preserving canonical state.
- Database record promotion with explicit due-property binding, detachment cancellation/application, and grouped undo preserving a later title edit and stable record/page/property identities.
- Collection layout, record override and reset preserving record values and the existing review event; a subsequent date-value edit makes that same review stale. The automated review event tests persistence, not human acceptance of content.

The reopen cases compare exact canonical values, identities, operation state, layouts and review history with saved pre-restart observations. These are bounded UI/service/storage checks. Native Wails rendering, input methods, accessibility, microphone, reminders, AI quality, live accounts, performance and broader feature workflows require their own checks.

Run from the desired checkout with existing pinned dependencies available in `frontend/node_modules`:

```sh
python3 scripts/run-real-ui.py \
  --app /Users/your-name/Applications/Trove.app \
  --evidence /private/tmp/trove-real-ui-unused-run \
  --go /path/to/pinned/go/bin/go \
  --node /path/to/node
```

The app is read-only and must have valid nested signatures and clean Go build provenance. No model packs are loaded. The browser uses the packaged frontend by default. `--assets /absolute/compiled/frontend` explicitly tests a separately built frontend overlay against the installed host; the manifest identifies that boundary and hashes the actual served assets separately from the installed app. An overlay result is not an installed-app result. The runner does not compile, install, register, update or create app bundles.

Each evidence path must be unused. `manifest.json` uses schema version 1 and binds the checkout, installed host source and binary, full app inventory, served assets, native helpers, frontend source snapshot, runner/config/spec files, fixed fixture, expected cases and actual reports. Results distinguish failed, skipped, interrupted, invalid and unexecuted cases. The next process stage is not attempted after a failed creation stage. Zero retries are configured; repeated or undeclared results cannot pass. Host lifecycle failure or changed inputs prevents an overall pass even if individual assertions passed.

The private token is created with mode 0600 and removed at cleanup. Browser traces are disabled to keep login input out of trace archives. The runner retains only its disposable workspace, logs, screenshots, canonical observations and manifests; it does not select or modify the user's normal workspace or preferences. It refuses non-local/non-APFS workspace output. Do not reuse or edit a previous result directory to rerun a failed case.

The lane has its own `frontend/playwright.integration.config.ts`, so ordinary mocked component tests do not silently become storage tests. `tests/test_real_ui.py` verifies that missing, skipped, retried, duplicate, unexpected and expected-failure results cannot be counted as passes. A successful fixture remains a bounded automated observation, separate from human acceptance and release qualification.
