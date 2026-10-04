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
