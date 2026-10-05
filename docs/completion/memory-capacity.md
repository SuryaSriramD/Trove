# Local assistant capacity and memory evidence

Implementation: `636462c`. The original memory/current-fact acceptance now passes with the optional **Qwen2.5 7B Instruct Q3** pack. The same rubric still fails with Qwen2.5 1.5B Instruct Q5. This is a development integration result on the recorded Mac, not a packaged release or general model-quality certification.

## Diagnosis and production change

Both current facts were already present in the 388-token prompt (440 tokens with approved memory), far below the 8,192-token context. The 1.5B model completed valid JSON within the unchanged 512-token output limit but omitted the requested launch claim. Repeating the original case reproduced that omission with and without memory. Its preferred heading and forget-during-stream behavior remained correct.

Changing only the selected model to the larger official pack produced the current budget and launch claims, correct exact/default heading, exactly three displayed lines, and supported exact quotations. Source selection, prompt instructions, ChatML framing, greedy sampling, grammar, numeric/quote guards, task oracle and approval boundary were not changed. This comparison identifies a model-specific instruction-following limitation on this task, rather than context loss. It does not establish that model size alone predicts quality on other tasks.

The catalogue adds pack `01990000-0000-7000-8000-000000000106`: 3,808,391,072 bytes, SHA-256 `a96b16179dc6cc9afdf0cf7a96a80c199cbd00b9be207c3465be21cb721cca5e`, official source revision `bb5d59e06d9551d752d08b292a50eb208b07ab1f`, pinned llama.cpp runtime `d2e54583c7452353eb35d40431281f6ee984332f`. It uses the existing single-file import/download verification path and has an 8 GiB worker-admission estimate. The existing host memory budget still applies; installation does not waive admission. Weights are outside Git and remain an explicit optional acquisition/import.

The AI panel initially selects this pack when it is already installed and verified. Users retain the model selector and smaller packs. Generation does not download models, switch to another pack after a failure, or contact a cloud provider. The estimated memory is not a measured peak RSS result.

The official [model card](https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-GGUF/tree/bb5d59e06d9551d752d08b292a50eb208b07ab1f) describes the GGUF family and llama.cpp usage. The pinned [Apache-2.0 license](https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-GGUF/blob/bb5d59e06d9551d752d08b292a50eb208b07ab1f/LICENSE) is retained in `docs/phase-3/licenses/qwen-7b-LICENSE`. Public LFS metadata, size and checksum are retained in the evidence directory. Trove_TRD.docx requires compatible pinned local packs behind runtime interfaces; it does not prescribe a particular Qwen size.

## Evidence and exact scope

All reports are in `evidence/completion/memory-capacity/`.

- `qwen-1.5b-original-strict.jsonl`: the unchanged original task fails both current-fact completeness and requested format; approved heading and streaming erasure remain correct. Total 21.73 seconds.
- `qwen-7b-original-strict.jsonl`: the unchanged original task passes without memory (20.62 seconds), with memory (23.12 seconds), and forgetting during streaming (29.12 seconds). All original checks plus the stricter claim/quote/line-count checks remain in the test.
- `qwen-7b-heldout-interrupted.jsonl`: existing natural-language named page creation passes, including no mutation before explicit approval and repeated-approval deduplication. Additional changed facts/order and separate current-source cases also pass. The investigator interrupted the last case after an apparently stale process observation; the stack showed an ordinary worker wait. This attempt is incomplete, not a demonstrated runtime hang or an aggregate pass.
- `qwen-7b-negative-oracle-v1.jsonl`: a newly added exact-abstention oracle rejected a correct cited negative answer because the source explicitly said the owner was unassigned. The original test source is retained as `negative-oracle-v1.go.txt`. After review, that extra case now requires the supported negative statement and exact quote, with a bounded statement grammar that excludes invented names. A separate case with no owner information retains the exact abstention requirement. This correction did not change the original memory acceptance criteria or any production prompt.
- `qwen-7b-heldout-final.jsonl`: all four additional cases pass with networking denied: changed facts/order (29.74 seconds), separate current sources (32.42 seconds), genuinely absent owner with exact abstention (21.88 seconds), and supported unassigned-owner negative evidence (23.29 seconds). Total 107.84 seconds.
- `affected-race.jsonl`: 27 affected checks passed; four explicitly enabled real-model tests skipped in this ordinary run. Real-model results are supplied separately and skips do not count as evidence.
- `typecheck.txt`: frontend TypeScript check exited successfully. The first attempt used a broken temporary dependency symlink; its failure is retained in `typecheck-stale-dependencies.txt`. The repaired symlink uses the pinned dependency tree.

The tests use synthetic disposable workspace facts. No user workspace, microphone, provider account, ambient credential, or model reporting endpoint was used. The held-out/action runs explicitly deny networking. Model acquisition separately uses only the public pinned publisher URL.

## Reproduction and qualification limits

Set `TROVE_NATIVE_INTEGRATION=1`, `TROVE_CHAT_EVAL_PACK=01990000-0000-7000-8000-000000000106`, `TROVE_NATIVE_BUNDLE` to the verified helper bundle, and `TROVE_MODEL_ROOT` to the verified model cache. Run the workspace tests with the pinned toolchain and `fts5 sqlite_omit_load_extension` tags:

- `TestRealLocalMemoryPreferenceAndCurrentAuthority`
- `TestRealLocalCitedHeldOutFactsAndAbstention`
- `TestRealLocalNamedWorkspacePlan`

Omitting `TROVE_CHAT_EVAL_PACK` preserves the prior 1.5B baseline, including its known failing quality rubric. No test silently substitutes the new model. The original persisted-memory test exercises kernel retrieval, completion and forgetting; extra held-out generation cases supply synthetic evidence directly through the production context builder and are not additional retrieval qualification.

This package does not close all AI-002/AI-003 behavior or any platform release gate. Other modes, varied real workloads, packaged UI behavior with this model, hardware limits, multilingual usefulness and Windows/Linux execution need their own evidence. Preserve the smaller model's quality limitation in release documentation. The source/helper/model identities and exact reports are bound by `manifest.json`.
