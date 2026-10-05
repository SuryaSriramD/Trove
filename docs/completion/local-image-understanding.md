# Local image understanding — C6 / OPT-001

The implementation supports explicit local questions about one PNG/JPEG attached to the current page. It retains original bytes and displays the source image, page/file revisions and original content hash beside each draft. OCR remains a separate operation and result. Generated descriptions do not enter canonical pages, tasks, records, search, approved memories or portable imports. There is no automatic write or action from image content.

## Native and model contract

Version 2 model manifests bind the pinned SmolVLM 500M Q8 language model to one exact F16 projector. Both artifacts must pass size/SHA-256 checks for the pair to appear ready. Download is an explicit Settings action; offline import asks for both files. Interrupted acquisition resumes, bad companion hashes fail, and active leases prevent removal. Pair removal retains companions referenced by other packs.

The CPU-only `vision-worker` uses mtmd from the existing pinned llama.cpp commit `d2e54583c7452353eb35d40431281f6ee984332f`. It is an optional, separately hash-verified helper. The worker receives verified private descriptors for the model, bounded request metadata, normalized pixels and projector. The product never passes workspace/database paths. The native supervisor retains cancellation, process-group termination escalation, actual sampled memory limits, result hash validation and the existing priority scheduler. Recording preempts image generation. Inference has no download path; build options disable OpenSSL, video and subprocess support.

The two optional Apache-2.0 artifacts total 636,275,712 bytes. The catalog pins ggml-org/SmolVLM-500M-Instruct-GGUF revision `72e986006ef53e37cdd3f6d4241c90b0f01df376`:

- Language model SHA-256: `9d4612de6a42214499e301494a3ecc2be0abdd9de44e663bda63f1152fad1bf4`.
- Projector SHA-256: `4b2fca922e549e9b9608f4ba56f93565dd5220e935ba5570a2a864a0fcc988a6`.

The build retains llama, mtmd dependency notices, the pinned model card and Apache license text. Nested dependency notice names preserve their paths so identically named LICENSE files cannot overwrite one another. The release provenance recorder verifies installed projector hashes in addition to model hashes.

## Bounds and source freshness

- PNG/JPEG only, at most 20 MiB and 20 million encoded pixels. Decode dimensions are checked before allocating pixels. Corrupt input fails without replacing the source.
- Normalize to at most 1,024 pixels on the longest edge using deterministic nearest-neighbor sampling and white alpha composition. Version 1 uses encoded orientation; EXIF rotation is not applied and the UI says so.
- One image, 1–2,048 Unicode question characters, at most four CPU threads, 512 answer tokens, 8,192 total context positions and a five-minute deadline. Actual multimodal input token/position counts plus output reservation must fit before evaluation.
- One active image draft per workspace and one foreground local model request. Up to 100 attached images and the 20 newest drafts are shown with explicit truncation messages.
- A direct workspace-owner request and current page/file scope are required. Page revision, file revision and content hash are checked before and after generation. Midflight edits/revocation discard the result. Cancellation and restart do not replay generation. Portable import retains the image and does not activate or import generated drafts.

Near-uniform images take a deterministic pixel-measurement path. Its result is labeled as a measurement with no model/OCR inference or model identity. This mitigation does not establish that the model can abstain. Repetitive or token-limited model output fails visibly and is not presented as a completed description.

## Recorded checks and limitations

Evidence is under `evidence/completion/c6-vision-*`. `c6-vision-manifest.json` identifies source/test files, models, helper, host and retained reports. The earlier temporary helper/reports did not survive the host interruption. The unchanged four-case rubric was recovered against the rebuilt helper, then recorded with exact fixture hashes after the additive schema fix. Both raw failure reports remain retained.

The raw model rubric remains **FAILED: 3 of 4 cases passed**. The red square is red, the blue circle is on the right and the simple A/B chart is answered “2, 4.” The blank image produces invented black text and repeated prose until 512 tokens. The report intentionally bypasses the production uniform-pixel shortcut and repetition rejection to measure the model itself. It is not reclassified as a pass after adding the shortcut. Small synthetic shapes/chart fixtures do not establish document, photograph, screenshot, handwriting, multilingual or general factual quality.

The actual helper cancellation and exact-token-budget checks pass: cancellation during active generation stops and releases both artifact leases; an actual image-plus-output overflow is rejected before decode. Focused race checks cover pair acquisition/resume/tampering, input dimensions, question bounds, normalization, source scope/freshness, cancellation/revocation, restart and portable import. The preservation test originally assumed exactly one imported file; imports also retain source/archive evidence. Its independent oracle now verifies the original content digest and bytes, without asserting an invalid total file count. The original failure log is retained. App compilation also caught insertion of the new result before existing numbered Go variants; appending the variant preserves existing embedding/transcription consumers, which pass their affected race tests.

The browser scenario covers explicit model/image selection, original preview/source links, running-state controls and cancellation without a canonical batch write. TypeScript compilation and affected native/kernel/contract/app checks are recorded separately. This is implementation evidence on one development Mac, not an independently installed/signed candidate, comprehensive image quality, broader operating-system support or C6 release approval. OPT-001 remains unqualified until the failed abstention rubric and representative image/screenshot evaluations pass against a declared candidate.

## Integration

`app.go` routes `image.start` and `image.cancel` under the lifecycle read lock to `app_image.go`; the shared AI flight registry supplies shutdown/workspace-switch cancellation. Kernel query registration supplies `image.list/start/cancel`, with additive service schemas generated from `scripts/build_protocol_schemas.py`. Jobs schemas add the bounded `image_generate` operation. `P2IntelligencePanel` mounts the Image questions tab. Models exposes `runtime.visionAvailable` and paired acquisition/import progress. Desktop/native build scripts invoke `build-vision.py` after the baseline bundle manifest exists. No migration of canonical content or proposal API is required.
