# Local editor renderers

The editor uses pinned Shiki 4.5.0 and PDF.js (`pdfjs-dist`) 6.3.289 packages. Runtime code, language grammars, themes, workers, fonts, character maps, color profiles and decoding WASM are packaged with the application; no CDN is part of the viewer design.

## Dependency selection

Versions were checked against the official package registry and upstream documentation on 2026-10-04. PDF.js 6.4.299 had been published less than 24 hours before this check. The 6.3.289 pin honors pnpm's existing minimum release age instead of creating an exception. Reassess newer PDF.js releases in the normal dependency update process, including the 6.4.299 fixes for malformed/interactive form documents. The viewer does not run PDF scripting or create an interactive form layer.

Shiki uses its JavaScript regular expression engine targeting ES2018 and loads each language on demand. Unsupported or failed grammar conversion must leave a readable plain source view. Canonical code text and clipboard bytes do not depend on highlighting.

PDF.js uses its legacy display/worker build for wider embedded browser compatibility. PDF.js 6.x removed the historical `isEvalSupported` option, so the viewer must not claim to configure that obsolete option. The application loads display APIs only, does not load the PDF scripting sandbox, and disables XFA. Rendering has bounded image/canvas allocations; these are resource limits, not a guarantee against all malformed documents.

## Official sources

- [Shiki regular expression engines](https://shiki.style/guide/regex-engines)
- [Shiki performance and bundle guidance](https://shiki.style/guide/best-performance)
- [PDF.js rendering examples](https://mozilla.github.io/pdf.js/examples/)
- [PDF.js API reference](https://mozilla.github.io/pdf.js/api/)
- [PDF.js 6.4.299 release notes](https://github.com/mozilla/pdf.js/releases/tag/v6.4.299)

## Implemented behavior and validation

Code previews render Shiki tokens as React text nodes with light and dark theme colors; they never interpret source as HTML. Multi-line syntax, line numbers, wrapping, filename metadata and byte-exact copy are covered. The former direct highlight.js dependency is removed (transitive consumers may still supply their own copy).

PDF blocks render a managed attachment through the local worker with page navigation, zoom, persisted preview page, selectable extracted page text and an original-file link. Ordinary annotation appearances are allowed, while `AnnotationMode.ENABLE_FORMS` excludes interactive form fields and no form display layer is mounted. A corrupt document reports a preview failure without changing or dropping the attachment. PDF actions and scripting are not executed by this viewer. Text extraction is a plain text alternative, not a positioned interactive annotation layer.

Validation on 2026-10-04:

- 35 frontend unit tests passed, including exact source preservation and multi-line Shiki tokens.
- 11 editor browser regressions passed against Vite, covering rich nested edits, toggle state and readonly reveal, clipboard IDs, table structure, colors, code, images, real PDF rendering and corrupt PDF fallback.
- Production TypeScript/Vite build passed. The artifact contains the PDF worker and 200 local PDF support assets; complete frontend output is approximately 20.9 MB before packaging/compression, including existing application bundles and lazily loaded language grammars.
- The 3 renderer browser regressions also passed against Vite's production preview server. The PDF fixture renders actual pixels, loads two pages with a standard font, persists the selected page through save/reload, has no external HTTP resource requests, and includes an ignored PDF JavaScript action.

These checks qualify browser rendering and local build assets. Packaged application/WebKit/Windows runtime checks and the release provenance manifest must be rerun as part of release qualification. Image-heavy, malformed or unusually complex PDFs can still fail the bounded preview and remain available through the managed original link.
