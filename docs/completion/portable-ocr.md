# Portable local OCR

Windows 11 x64 and Ubuntu 24.04 x64 now have an offline OCR implementation and native build recipe. Their native execution and packaged UI are **unqualified**. The same C++ helper was built and exercised on macOS arm64; that evidence does not qualify either target OS. macOS keeps its existing Apple Vision helper unless a portable OCR sidecar is explicitly bundled.

## Engine and provenance

The portable engine is Tesseract **5.5.1**, statically linked with Leptonica **1.85.0**, using the English `tessdata_fast` LSTM model at commit `87416418657359cb625c412a48b6e1d6d41c29bd`. Its SHA-256 is `7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2`. All URLs, archive hashes, versions, and licenses are in [`native/ocr.lock.json`](../../native/ocr.lock.json).

This initial portable capability recognizes **English printed text**. It does not claim multilingual, handwriting, or scanned-document-suite qualification. Apple Vision's OS-supported language detection remains distinct. The visible OCR panel and query results report the actual engine, language contract, source revision, and extractor version. The portable extractor version includes the full model hash; stored document extraction metadata and search-section headings retain its identity. The original image remains the source. OCR-derived text remains separate from generative image interpretation.

Tesseract provides an [in-memory traineddata API and raw image input](https://github.com/tesseract-ocr/tesseract/blob/5.5.1/include/tesseract/baseapi.h); Trove uses those paths and rejects auxiliary file reads. The upstream [model comparison](https://tesseract-ocr.github.io/tessdoc/Data-Files.html) identifies `tessdata_fast` as an integer LSTM model with a speed/accuracy tradeoff. Tesseract and the [English model repository](https://github.com/tesseract-ocr/tessdata_fast/tree/87416418657359cb625c412a48b6e1d6d41c29bd) use Apache-2.0; [Leptonica](https://github.com/DanBloomberg/leptonica/blob/1.85.0/leptonica-license.txt) uses its two-clause BSD license. The bundle includes all three notices in `ocr-licenses/`.

## Input and execution boundary

- Only a direct user OCR request may start extraction. PNG/JPEG input is limited to 20 MiB and 20 megapixels, with one operation per kernel and a 60-second deadline.
- Go validates the original header and decodes the full raster. It composites alpha over white and converts to gray without resizing, sharpening, or applying EXIF rotation. Encoded orientation and possible recognition omissions are disclosed. Tiny and faint source marks survive this preparation.
- The worker receives only two explicit descriptors: a bounded `TOCR` gray raster and the pinned English model. It receives no workspace handle, source path, or user environment. Tesseract's image codecs, archive/CURL support, training programs, legacy engine, OpenMP, and graphics support are disabled. Leptonica image codecs are disabled.
- Target OS/architecture/runtime/helper/model identities are validated. A malformed portable manifest cannot downgrade to Apple Vision. The exact verified binary is copied into an owner-private directory and hashed again before execution. The worker independently hashes its model bytes. No runtime downloads occur.
- Windows uses the existing limited inherited-handle mapping and kill-on-close Job Object with a 1 GiB process bound. A start byte is sent only after job assignment. Unix uses a private process group, monitored RSS, and whole-group termination. Deadline/cancellation returns no extraction. The worker also has a 50-second recognition deadline; output is limited to 10,000 lines, 1 MiB of text, and 2 MiB of JSON.
- Before persistence, the kernel checks cancellation, current provider identity, current permission, and source revision. Confidence must be finite in `[0,1]`; lines below `0.5` are omitted with a warning. Engine/model changes cannot publish under the prior engine's identity. The interface discards results after a workspace, grant, snapshot, or image-selection change.

The descriptor protocol reduces the parser's inputs; this is not a claim that Unix process groups constitute an OS filesystem sandbox.

## Reproducible build

Use the repository's pinned CMake 4.3.2 and the target's declared compiler/toolchain. Source acquisition is explicit; subsequent builds are offline. An example native-host build is:

```sh
python3 scripts/build-ocr-portable.py --target linux-amd64 \
  --cache /path/to/trove-cache/ocr --fetch --output /path/to/empty-ocr-bundle
```

Use `windows-amd64` from an x64 MSVC developer environment or `darwin-arm64` on the supported Mac. The script rejects cross-host builds, changed archives/source files, unexpected source files, incompatible model bytes, and existing OCR artifacts in the output. It checks dynamic-library dependencies and records commands, hashes, source pins, and dependency output in `ocr-worker.json`. `--plan` performs no build or qualification.

`build-native-portable.py` invokes this builder using `<cache>/ocr`, includes `ocr_english`, and keeps PDF/document helpers and system audio capture explicitly unavailable. Populate the OCR cache with the command above before building the complete portable bundle. The OCR sidecar and English data must ship together with the helper; no ambient `tesseract` command is used.

## Evidence and remaining release work

[`evidence/completion/portable-ocr/manifest.json`](../../evidence/completion/portable-ocr/manifest.json) binds the exact sources, worker, English data, fixture, logs, and hardware. The unchanged printed-text fixture requires both `TROVE LOCAL OCR 2026` and `Review sources before approval` at the existing 0.5 threshold. The actual portable helper passed both. Other checks cover pixel bounds and faint/tiny/alpha preservation, malformed/oversized images, invalid target/model/test manifests, altered model denial, active-child cancellation, permission revocation, engine replacement, cancelled persistence, visible provenance, and superseded UI results.

Windows/Linux Go compilation and build-plan checks passed. They do **not** demonstrate native Tesseract execution, Job Object behavior, target OCR accuracy, clean installation, or packaged WebView behavior. Run the same real fixture and cancellation/security checks on each native target, then the applicable release matrix and representative image suite. Broader recognition quality remains unqualified. No screenshot is acquired by this package; Windows/Linux interactive screenshot adapters and portable PDF extraction/export remain separate open work.
