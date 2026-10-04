# Context budgeting and local-model qualification

AI-002 and AI-003 remain in progress. This is development evidence, not a macOS release gate.

The native adapter now counts the actual installed instruction model's tokens, including its chat framing and special tokens. The tokenizer loads the vocabulary without tensor weights. It evaluates a bounded batch of context candidates and reserves output space before generation. Source omissions and shortened excerpts are reported; an exact text/code selection is either included in full or rejected. The worker independently enforces the same context limit. Output that hits the token limit is retained as an incomplete draft and cannot be applied as a successful replacement.

Selected code uses the canonical source rather than a whitespace-collapsed search excerpt. A replacement changes only the unique exact selection; whole-block replacement still requires complete source evidence. Retrieval and approved memories are checked again after token counting and before publishing or applying an answer.

Cited answer, summary and explanation modes use bounded structured claims. The application matches supporting quotes against supplied current evidence and adds source labels itself. Case and whitespace wrapping may differ, but words, numbers and punctuation must match; a substring inside a different number or word is rejected. The displayed quote preserves canonical source bytes. The streaming UI shows provisional readable text. An invented or ambiguous supporting quote fails the draft and prevents application. Matching a quote does **not** establish that every model-written claim follows from it; semantic correctness remains an evaluation requirement.

## Checks

- `internal/workspace/ai_context_test.go`: long exact selections, explicit omissions, context exhaustion, concurrent source changes, and failed-draft application denial.
- `internal/workspace/ai_generation_test.go`: readable streaming, strict structured output, exact source attribution and numeric/Unicode boundaries.
- `internal/native/token_budget_test.go`: optional real Qwen 1.5B test. An 18,804-byte prompt counted as 3,045 tokens and retrieved its synthetic marker; a four-token incomplete answer was rejected. See `evidence/completion/checkpoint-1/token-budget-real-model.jsonl`.
- `internal/workspace/ai_local_evaluation_test.go`: actual Qwen 1.5B with/without approved formatting memory and an older contradictory decision. The rubric requires current facts in the answer itself, source support for each known fact, and the expected heading. Forgetting during real streamed generation must invalidate the result and exclude the memory from the next request.

The memory rubric exposed invented quote prefixes and misattributed facts in earlier attempts. Those failures are retained under `evidence/completion/checkpoint-1/`. The older `memory-structured-citations.jsonl` check only established syntactically valid labels; its apparent pass is superseded by the stronger claim-support rubric and must not be treated as a quality pass. The final contract run and its source/helper/model/hardware identities are in `memory-final-contract.jsonl` and `memory-final-contract-inputs.json`. The independent forget-during-stream check passed in `memory-final-rubric.jsonl`; the answer-quality cases in that run failed.

Broader task families, every exposed advanced mode, peak worker RSS, correction effort, representative language/data coverage, semantic related-content UI and packaged offline model execution still need qualification. No cloud fallback or automatic generated write was added.
