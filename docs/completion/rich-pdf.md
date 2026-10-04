# PARITY-008 — offline rich page PDF

The macOS export now prints a dedicated canonical page document through the bundled `pdf-renderer` helper. It uses system WebKit, AppKit and PDFKit. It does not require Chrome, a provider account, a package manager or an online rendering service. The qualified platform is Apple Silicon macOS 27.0.1, matching the current native build target. Windows and Linux return an explicit unsupported result with HTML/Print to PDF guidance; those native implementations and Intel macOS remain unqualified.

## What prints

Active page titles and rich text retain Unicode, marks, safe links and canonical foreground/highlight tokens. The print renderer includes managed images and captions, merged/header tables, code with wrapping, Mermaid diagrams, KaTeX equations, nested toggles with all retained descendants expanded, columns, every tab, and the actual reusable source subtree. Cycles and unavailable sources get readable references. Long tables are segmented with repeated headers because the qualified WebKit print API does not repeat them automatically. Segmentation respects row spans. Code may continue across pages; it is not clipped into a scrolling viewport.

Invalid diagrams/equations retain their source. Unusually wide equations fall back to source when a readable fit is not possible. Unsupported images have readable fallback text. Bookmarks use only URL-matched cached metadata. Embeds retain offline text without fetching a player. PDF/audio/video/file attachments are named with captions; their original bytes, database definitions, interactive behavior, history and exact schemas belong in portable export or a native backup. Task references remain references in PDF, with canonical task/database values in portable export. Returned export notes and a final notes section state applicable fallbacks. Pixel-identical Notion reproduction is not claimed.

## Boundaries

The existing native save dialog and owner scope gate remain authoritative. `App.ExportPDF` passes the embedded frontend asset FS directly to the workspace service; renderer dispatch cannot supply a filesystem path, print executable or print asset bundle. One read transaction creates the canonical snapshot. Only referenced image bytes are copied, so unrelated recordings or large document archives are not staged just to print a page. Image hashes are verified before copying. Common PNG/JPEG/GIF dimensions are checked before decoding; unsupported formats use WebKit's isolated image rendering with a readable failure fallback.

The native helper has a nonpersistent WebKit store and no Wails workspace bridge. Its private `trove-print://document` scheme serves only its private staged directory, rejects path traversal/symlinks, and cannot read the workspace database. A restrictive CSP and independently compiled WebKit content rules deny HTTP(S), WebSockets and local-file requests; navigation is restricted to the one print entry. Imported HTML is never executed. Mermaid remains strict and KaTeX disables trusted commands.

Limits are 32 MiB snapshot, 2,000 source/physical pages, 64 MiB per image/resource, 256 MiB copied images, 128 MiB bundled assets and 256 MiB PDF output. The Go operation observes context cancellation and a two-minute deadline; the helper has an independent 110-second watchdog and output-size watchdog. Partial destinations and staging are removed, existing destinations are never overwritten, and malformed or missing completion reports fail closed. The helper is hash checked against its packaged manifest, signed by the build scripts, and required by package verification.

The asynchronous `NSPrintOperation.runModal` completion API is required: the synchronous `run()` path produced uncontrolled pagination in the local macOS 27 probe. No synchronous path ships. The helper sets an explicit print view frame and A4 print settings before the asynchronous operation.

## Qualification

- `TestRichPDFExportRequiresOwnerAndBundledAssets`: authority, missing assets, missing helper manifest and absence of partial output.
- `TestRichPDFExportBundledRenderer`: actual canonical workspace fixture, managed PNG bytes, rich columns, reused source, 38-row table, long code, Unicode, diagram, equation and truthful offline/unsupported fallbacks; exclusive destination preservation.
- `frontend/tests/print.browser.spec.ts`: real renderer, rich layout/source rendering, repeated headers, invalid equation fallback, escaped source text, no app bridge and zero external resource requests.
- `tests/test_phase5_packaging.py`: packaged helper presence/executability requirements.

The actual canonical fixture produced seven A4 pages (181 KiB before later nonvisual snapshot plumbing), rendered with Poppler and visually inspected in full. The diagram, equation, managed image, colored rich columns and reused source were visible. Tables had repeated headers; all code continued across pages; no glyph boxes, clipped columns or hidden attachment content were observed. Text extraction retained café, Japanese, Greek and Devanagari; some math extraction duplicates accessibility characters even when the rendered equation is correct. This is native helper/workspace qualification, not a claim of notarization or a fresh-device installed-app release gate.

Reproduce from a built frontend:

```sh
python3 scripts/build-pdf-renderer.py --output /private/tmp/trove-pdf-native
TROVE_NATIVE_BUNDLE=/private/tmp/trove-pdf-native TROVE_PDF_SMOKE=1 \
  TROVE_PDF_EVIDENCE=/private/tmp/trove-rich.pdf \
  go test -tags sqlite_fts5 ./internal/workspace -run TestRichPDF -count=1
pdfinfo /private/tmp/trove-rich.pdf
pdftoppm -png /private/tmp/trove-rich.pdf /private/tmp/trove-rich-page
```

The print helper API is documented by Apple: [WKWebView printOperation](https://developer.apple.com/documentation/webkit/wkwebview/printoperation(with:)) and [NSPrintInfo job disposition](https://developer.apple.com/documentation/appkit/nsprintinfo/jobdisposition-swift.struct). Current SDK headers and an actual asynchronous native print probe were also checked.
