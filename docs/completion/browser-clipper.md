# Browser clipper implementation and qualification

PARITY-009 is implemented as a bundled Manifest V3 extension, an explicitly enabled native loopback bridge and a durable owner-reviewed canonical import. [Installation and usage](../../browser-clipper/README.md) cover developer-mode loading, one-time pairing, selecting a destination and final saving. Manual paste/share remains available.

## Runtime and trust boundary

The native owner approves a 32-character extension ID and a bounded destination allowlist. Starting pairing binds an ephemeral `127.0.0.1` port and creates a five-minute, one-use 256-bit secret. The bridge rejects every other Host and browser Origin, does not allow ambient cookies, and exchanges the code for a session bearer credential. Re-pair, revoke, workspace switch and app shutdown close the old listener. Session credentials are held only in native memory and extension session storage; account or cloud authentication is not involved.

The bridge supports only JSON POST `/pair`, `/destinations` and `/capture`. It cannot commit a draft, read arbitrary files, fetch arbitrary URLs, run a workspace command or start a shell. Approved destination IDs stay fixed while their current titles/revisions can be refreshed. Requests are bounded to 4 MiB and one capture at a time; identity plus exact payload digest make transport retries idempotent. Native durable plan provenance also retains the request identity digest across app restarts. An interrupted preview can remain available in the general imports inventory; a conflicting reused identity is rejected rather than overwritten.

The extension has no background worker or standing content script. A toolbar action grants `activeTab` for a bounded article/main-content, selection or URL capture. Source DOM is cloned, stripped of executable controls/attributes and left unchanged. Native HTML adaptation independently sanitizes the resulting source. The original source artifact, canonical blocks and explicit omissions remain available in Trove. All captures state partial source completeness.

Images require a separate selected-origin acquisition action. Each fetch omits credentials/referrers and rejects redirects; optional host permission is removed afterward. Supported bytes are PNG/JPEG/GIF only, at most eight images and 2 MiB total. Native validation checks actual media type, dimensions (20 megapixels maximum) and hashes before managed asset ingestion. Unacquired sources remain readable references. The native bridge never makes an image network request.

The browser retains one draft locally before submitting. It deletes that copy only after receiving a native durable-plan receipt. Unavailable/revoked app sessions leave the draft available for re-pair/retry. Received drafts survive restart and appear in **Capture → Browser drafts**. Final native owner saving rechecks destination scope, active status and expected revision before creating a new child clip or an unfiled Inbox clip. A stale destination requires a separate **Refresh destination for review** action, then another explicit save. Existing destination text is preserved.

## Qualification evidence

- `internal/clipper/bridge_test.go`: exact Host/Origin, unpaired and other-extension denial, one-use and expired pairing, immutable idempotency, destination allowlist/revision, body budget, credential revocation and listener shutdown.
- `internal/workspace/capture_clipper_test.go`: uncommitted preview, canonical HTML sanitization, image consent/type validation, acquired asset provenance, same-request retry, changed-payload rejection, owner-only refresh, stale selected destination guard, actual child placement and durable reopening.
- `app_clipper_test.go`: no implicit listener, replacement pairing closes the old endpoint, explicit revoke and app shutdown remove native sessions. Native app compilation and the existing provider disconnect regression also pass.
- `frontend/tests/clipper.browser.spec.ts`: actual native Capture UI with a controlled native-service fixture. It preserves pairing details through asynchronous work, displays inert source text and omissions, rejects stale save, separates refresh from save, saves through imports and keeps received drafts after revocation.
- `TestClipperBrowserQualification` plus `frontend/tests/clipper-extension.mjs`: actual unpacked MV3 extension, browser `activeTab` action, real local bridge and real canonical importer in an isolated profile/temp workspace. Article, selected formatting, URL and explicit raster acquisition pass; received drafts do not mutate pages before owner review, then commit and reopen offline. A closed listener leaves the browser draft intact across popup reload. Source DOM remains unchanged. Executed on macOS with Brave Chromium **154.0.8037.58**. Headless mode opens the same popup page if browser chrome does not expose its popup target. The image permission grant is an explicit fixture; fetch/storage/bridge/import use real implementations. Interactive optional-origin permission prompts remain an installation smoke check.
- `tests/test_clipper_packaging.py` plus Phase 5 packaging tests: deterministic ZIP, stable key-derived extension identity, exact self-contained file inventory, restricted permissions, exclusive output and required packaged resources. Seven tests pass. No extension store publication is claimed.

Re-run the real integration using an installed Chromium executable supporting unpacked extensions:

```sh
TROVE_CLIPPER_BROWSER='/Applications/Brave Browser.app/Contents/MacOS/Brave Browser' go test -tags sqlite_fts5 ./internal/workspace -run TestClipperBrowserQualification -v -count=1
```

The test is explicitly opt-in and does not touch the user's browser profile. Set `TROVE_NODE` if Node is not on PATH. Other default Go/browser tests require no online site. The standalone package command is `python3 scripts/package-clipper.py --output /new/path/Trove-Web-Clipper.zip`; the desktop build also copies the extension folder into app Resources and release packaging checks its presence. macOS native + Chromium MV3 is the implemented package; Safari, Firefox, Windows/Linux desktop qualification and store distribution are separate.

## Primary browser references

The implementation follows Chrome's [activeTab permission model](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab), [extension cross-origin request guidance](https://developer.chrome.com/docs/extensions/develop/concepts/network-requests), and the official [Extensions DevTools protocol](https://chromedevtools.github.io/devtools-protocol/tot/Extensions/) for isolated automated action qualification. No vendor site data or browser login is required by the test.
