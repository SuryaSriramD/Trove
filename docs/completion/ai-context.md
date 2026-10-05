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

A later combined developer run (`evidence/completion/c4-real-plan-forget.jsonl`) failed before generation because native token-count workers exceeded their 30-second deadline. Neither task passed in that attempt. This is a retained runtime reliability finding, not a factual-rubric result; investigate worker startup/load under concurrent development activity before release.

The failure was reproduced in `c5-tokenizer-diagnostic.jsonl`. A one-second native sample found `llama_backend_init → ggml_backend_registry → ggml_backend_metal_reg → ggml_metal_library_init` waiting for the Metal compiler. Vocabulary-only work was initializing the GPU. The worker now disables Metal device enumeration for tokenization and explicit CPU operations, and skips backend initialization for tokenization. GPU generation still uses Metal. Supervisor timeouts now return their actual cancellation cause instead of a misleading process-signal error.

The rebuilt development bundle `/private/tmp/trove-completion-native-v2` uses llama-worker SHA-256 `e2a14be8524c76491e0b4779604fd33f151905594c1eec96621c79596ca131c3`. Against the same Qwen 1.5B pack, `c5-tokenizer-fixed.jsonl` passes actual token counting, long-input marker retention, incomplete-output rejection and cancellation (51.41 seconds overall; token counting 10.10 seconds). The original 30-second tokenizer bound is unchanged. This correction does not resolve the separate factual/memory-quality failures.

`c5-real-plan-forget.jsonl` also passes on that helper: forgetting approved memory during real streamed generation invalidates the result and removes it from the next context, and a named page-creation request produces a proposal that creates content only after explicit approval. Total duration was 85.65 seconds. The fixtures are synthetic; no representative-account or release claim follows.

## Bounded memory/citation repair investigation — 2026-10-05

The current cited-mode context uses JSON objects to separate source text from titles, source numbers and memory labels. Substantive current page content precedes standalone page-title excerpts; approved memories remain distinct. Actual token counting still chooses the bounded context and reports shortened/omitted evidence. General writing/code/agent/review modes retain their existing context format.

Supporting quote generation is now restricted to exact spans of the included source text. There are at most 256 distinct choices, each at most 512 Unicode characters, and at most 64 KiB of worker grammar. Selection uses full short excerpts and sentence/clause/bounded word segments, not all possible substrings. If a context's exact-quote grammar cannot fit, preparation tries a smaller context and reports the resulting omissions/shortening. Quotes that slice words/numbers are excluded; source text is escaped as grammar string data. The independent canonical quote matcher, ambiguity rejection, current source/memory checks and actual worker token limit remain in place.

A narrow numeric invariant additionally rejects a factual amount attached to a quote that does not contain that numeric literal. This prevents the observed budget claim carrying only a launch-month quote. It does not prove semantic entailment, derived arithmetic or nonnumeric factual correctness. Literal numeric formatting must match source evidence.

The real-model investigation retained four attempts:

- `c6-memory-explicit-evidence.jsonl`: JSON evidence boundaries alone did not fix the existing failures.
- `c6-memory-evidence-first.jsonl`: generating a quote before the claim and prioritizing substantive sources passed the original with-memory case; without-memory still inserted a word into its supporting quote.
- `c6-memory-exact-span.jsonl`: exact quote choices produced a nominal pass under the earlier oracle, but inspection found a budget claim attached to a December-only quote and four lines instead of the requested three. That apparent pass is **superseded and is not quality evidence**.
- `c6-memory-final-strict-rubric.jsonl`: the oracle now additionally requires the exact requested/default heading, exactly two claims plus one heading, and each displayed supporting quote to contain the known fact asserted by its claim. Facts, source text, current-versus-historical values and existing citation checks were not relaxed. Both with/without-memory cases **still fail**: Qwen supplies the correct heading and a supported budget claim, but omits the launch fact from claim text. Forgetting during streaming still passes.

The final implementation keeps claim-before-quote generation, source ordering, exact quote choices and strict independent validation. The four-attempt investigation stopped without relabeling the remaining completeness/format failures. The restored helper is byte-identical to the previously corrected v2 llama worker (`e2a14be8524c76491e0b4779604fd33f151905594c1eec96621c79596ca131c3`); the same verified Qwen 1.5B pack was used. Source/helper/library/model/report identities are retained in `c6-memory-*.json` evidence. No cloud fallback, provider account or extra model download was used.

Affected context/quote/AI/memory/review race regressions include summary/explanation context, exact code-selection preservation, escaped/Unicode source spans, candidate/grammar bounds, incorrect numeric support, stale sources and approval protections. AI-002 and AI-003 remain unqualified. Completing the fixed answer rubric, then adding independent task/format/quality rubrics for all exposed modes and a representative corpus, remains necessary.

## Higher-capacity local pack follow-up

The original strict task now passes on the optional pinned Qwen2.5 7B Q3 pack using the unchanged production prompt, context builder, quote grammar and numeric guards. The 1.5B baseline reproduces its earlier omission and remains unqualified for this task. See [the capacity comparison](memory-capacity.md) and `evidence/completion/memory-capacity/` for exact identities, the unchanged original oracle, additional evidence cases and limitations. This is development integration evidence, not a replacement for candidate-specific release checks.
