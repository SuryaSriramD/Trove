# Bounded SmolVLM preprocessing profile

The code-owned `smolvlm-500m-idefics3-bounded-v1` profile applies only to the exact model and projector hashes in `internal/native/vision_profile_v1.json`, with the pinned llama.cpp runtime. Its SHA-256 covers every JSON byte, including the transport limits and reference caveat. It is not selected by downloaded model metadata, workspace content or a request option.

The pinned projector (SHA-256 `4b2fca922e549e9b9608f4ba56f93565dd5220e935ba5570a2a864a0fcc988a6`) declares IDEFICS3, image size512, patch size16, merge4 and RGB mean/std0.5, but omits `clip.vision.preproc_image_size`. The pinned runtime therefore takes its legacy padded resize branch. The profile supplies longest edge2048 to the existing IDEFICS3 branch. It resizes with Lanczos, rounds dimensions up to512 tiles, uses the refined image for the overview, and keeps RGB normalization. The runtime adaptation validates the loaded shape, absent geometry, unset custom pixel limits and color settings before loading tensors. Its default0 leaves unrelated packs unchanged.

The build reconstructs a private source tree from the verified upstream archive and applies four exact, hash-checked source adaptations. It does not modify the cached upstream checkout, model/projector bytes or model metadata. All adapted translation units and the worker compile together against the same headers. Profile JSON, adapter and native header are included in release source provenance.

The Go request uses metadata schema2 for this exact pair. The verified helper must advertise the exact profile hash before receiving work and return the same profile ID/hash with its final result. Old or incompatible helpers fail without model execution. The supervisor retains existing input handles, hashes, resource ceilings and cancellation. Unrelated pairs continue through schema1; a profile cannot be attached to them. Historical image drafts retain absent profile fields. Newly acknowledged provenance is stored with the derived draft, never backfilled.

## Transport boundary

This is a **bounded-transport** profile. The unchanged Go transport preserves original source bytes, accepts PNG/JPEG up to20MiB/20MP, composites alpha over white with integer-floor rounding, uses encoded orientation, and caps the longest edge at1024 using nearest sampling. Model geometry2048 operates on those transported pixels. It is not equivalent to feeding raw source pixels through the upstream processor. The retained comparison independently exposes disappearance of an off-grid impulse during transport and the one-step alpha rounding difference. The existing detail-loss guard remains active. Any transport resampling change needs a separate reviewed profile/version and controlled evidence; no such change is part of this profile.

All existing limits remain: normalized input up to5MiB/1024 per side, one image, context at most8192, answer at most512 tokens, four threads,300-second deadline,128KiB result ceiling, unchanged scheduler memory budget and cancellation. More image tiles consume more of the same context/memory budget; this change does not grant a larger budget. Native model quality, latency and resource behavior require a later controlled run and are not established by geometry tests.

## Reference and deterministic evidence

The exact GGUF repository revision is `72e986006ef53e37cdd3f6d4241c90b0f01df376`. Its retained files do not identify the original conversion processor revision. The independent reference uses the base model processor config at `a7da5b986cb59b408707209984f360a5f4ad7e47` (config SHA-256 `6cb6e36d6fcb88ca1502c4a26750715dc3e7dedddc9a8f17b27d8d167d1457e7`) and actual IDEFICS3 geometry methods from Transformers4.49.0 commit `a22a4378d97d06b7a1d9abad6e0086d30fdea199`, with retained Pillow/NumPy array plumbing. The base config's recorded4.46.0 version is stale for this comparison: that implementation rejects2048. No claim of exact original-conversion equivalence is made.

Official inputs:

- [Pinned GGUF repository](https://huggingface.co/ggml-org/SmolVLM-500M-Instruct-GGUF/tree/72e986006ef53e37cdd3f6d4241c90b0f01df376)
- [Locked processor configuration](https://huggingface.co/HuggingFaceTB/SmolVLM-500M-Instruct/blob/a7da5b986cb59b408707209984f360a5f4ad7e47/preprocessor_config.json)
- [Locked reference implementation](https://github.com/huggingface/transformers/blob/a22a4378d97d06b7a1d9abad6e0086d30fdea199/src/transformers/models/idefics3/image_processing_idefics3.py)
- [Pinned runtime geometry](https://github.com/ggml-org/llama.cpp/blob/d2e54583c7452353eb35d40431281f6ee984332f/tools/mtmd/mtmd-image.cpp)

The retained no-inference comparison has12 independent geometry/color/impulse/alpha fixtures. It compiles the unchanged pinned `mtmd-image.cpp` only, with explicit header values, and compares normalized tensor values against the locked reference methods. The legacy grid differs on12/12 fixtures; explicit2048 matches all float values on12/12 for the same transported pixels. Three RGB controls pass in both paths. These are processor measurements, not image-understanding or release acceptance. The complete scripts, exact sources, original failed shim attempt, corrected comparison, pixel outputs and hashes live in the coordinator's `macos-quality-20261016/vision-preprocessing` evidence directory.

Focused checks after source review and approval:

```sh
# Export new synthetic fixture copies only to a fresh, explicit directory.
TROVE_VISION_PREPROCESS_OUT=/private/tmp/fresh-vision-fixtures go test -race ./internal/native -run '^TestVisionPreprocessingTransportFixtures$' -count=1
# No models, native helper or app execution.
go test -race ./internal/native -run '^TestVisionProfile' -count=1
go test -tags 'fts5 sqlite_omit_load_extension' -race ./internal/workspace -run '^TestImageProfile' -count=1
# Optional small native guard executable links JSON only, never a model/backend.
TROVE_VISION_RUNTIME_SOURCE=/absolute/path/to/verified/llama.cpp python3 -m unittest discover -s tests -p test_vision_profile.py
```

The retained comparison is separately reproducible from `runtime-probe-build-v2.json` and `run-comparison-v2.py` with its exact installed Python/Pillow/NumPy versions. Use a copied evidence directory and fresh outputs; preserve the original evidence bytes. It is not a production inference runner.
