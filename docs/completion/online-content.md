# Bookmarks and consented online players

PARITY-002 adds explicitly requested bookmark metadata fetches and optional known-provider frames. Importing, mounting, refreshing or exporting a block never grants network consent.

## Bookmark cache

`editor.bookmarkPreview` is a schema-validated, direct-user query scoped to an authorized page. The request must include `consent: "fetch_once"`. It returns bounded title/description text, the requested URL and a fetch timestamp; it does not mutate the workspace. The editor saves accepted fields through the ordinary guarded page transaction. Optional `cachedSourceUrl` and `fetchedAt` fields retain compatibility with existing bookmark blocks. A changed URL presents old metadata as belonging to the previous source, not as a preview of the new URL.

The fetcher accepts public HTTPS destinations on the standard port, disables ambient proxies/cookies/authorization, validates every redirect, rejects private/reserved/metadata addresses and pins validated resolved IPs into the socket dial. TLS still verifies the original hostname. Limits are eight seconds, two followed redirects, 64 KiB response headers and 1 MiB decoded HTML. UTF-8/ASCII HTML is supported; other charsets and unavailable/oversized/non-HTML sources retain the saved fallback. Parsing extracts text only, does not execute HTML/scripts, and does not fetch images, icons or linked resources.

## Live embeds

Only parsed YouTube video IDs and Vimeo numeric video IDs (including supported `h` query hashes) produce live player URLs. Arbitrary parameters, credentials, nonstandard ports, non-HTTPS URLs and lookalike hosts do not enter the frame. Other sources retain escaped offline text and an explicit external link.

Each view begins with an inert `srcDoc` using an empty sandbox and `default-src 'none'`. Enabling a player creates a new direct cross-origin iframe with `allow-scripts allow-same-origin allow-presentation`. The provider needs its own origin for storage; it never shares the app origin. No same-origin permission is attached to any `srcDoc`. Forms, popups, downloads and top navigation remain disallowed, and no player JavaScript API/parent-message bridge is installed. The parent document restricts `frame-src` to `https://www.youtube-nocookie.com` and `https://player.vimeo.com`, with no self source. Provider navigation into Trove's origin is blocked. App response anti-framing headers are also required. Root integration commit `877f75f` supplies `frame-ancestors 'none'` and `X-Frame-Options: DENY` in native AssetServer middleware and the hosted shell, while preserving stricter attachment sandbox policies. This package depends on that integration. Vite dev/preview supplies equivalent headers.

Stopping the player, going offline, closing/reloading the view, or changing the URL/provider destroys the frame and revokes local consent. Nothing persists an online-enable flag. Cached text stays available while a player is running or unavailable. Provider tracking, availability, authentication and media licensing remain controlled by the provider; the UI does not claim successful playback merely because an iframe loaded.

## Validation

- 40 frontend unit tests passed, including finite provider parsing and source-matched cache checks.
- 19 editor browser regressions passed. Consent and frame-boundary fixtures verify no initial request, explicit requests only, parent/bridge isolation, blocked app-origin navigation, cache reuse, URL/provider revocation, offline stop and reload reset.
- Real SQLite tests pass for consent/actor/scope rejection, pinned dial addresses, mixed-private DNS rejection, redirect/byte bounds, inert text extraction, history/restart and cache export/reimport.
- Live Chromium smoke checks loaded the real YouTube official demo player and the Vimeo official player example, with visible player controls/video elements. This validates player loading, not every provider entitlement, account or media playback condition.
- An explicit live service smoke fetched the public `https://example.com` page and returned its actual title through the guarded transport.
- Production TypeScript/Vite build and contracts tests passed. Optional live provider checks are enabled only with `TROVE_LIVE_EMBED_SMOKE=1`; ordinary test runs do not contact these providers. `TROVE_LIVE_BOOKMARK_SMOKE=1` enables the live metadata test.

The initial opaque-origin player design was rejected by the real YouTube player because browser storage APIs were unavailable. The shipped direct-provider design keeps a separate provider origin and is covered by the access/CSP tests. Packaged desktop/WebKit qualification remains part of release verification, rather than being inferred from Chromium fixtures.

Official references checked on 2026-10-04: [YouTube player parameters](https://developers.google.com/youtube/player_parameters), [YouTube embedding and privacy mode](https://support.google.com/youtube/answer/171780), and [Vimeo player embedding examples](https://developer.vimeo.com/player/sdk/basics).
