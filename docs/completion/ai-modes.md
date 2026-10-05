# Local AI mode evaluation

**All 14 fixed writing/structured mode tasks pass** on source `ab3ff13` with the explicitly installed Qwen2.5 7B Instruct Q3 pack. The single final run exercises actual local generation, saved drafts and applicable proposal preparation with networking denied. No generated action is approved and canonical content remains unchanged. This is bounded developer evidence, not general model-quality or release qualification.

The independently defined inputs and outcome checks are unchanged: preserved facts/units, headings/bullets, canonical database field types/configuration, diagram edges, Go syntax/signature/return expressions and comments, and no canonical mutations. The test file changed only to collect/report memory; the manifest includes that exact diff and verifies unchanged fixture/check sections.

| Tasks | Final outcome |
| --- | --- |
| Summarize, explain, rewrite, shorten, expand, outline, generate | 7/7 pass |
| Database, schema, Mermaid | 3/3 pass |
| Code explain, refactor, comment | 3/3 pass |
| Organization suggestions | 1/1 pass |

The initial run passed 11/14, with failures in outline structure, schema configuration and fenced code comments. Database/schema prompts now use the canonical contract; bounded required-field grammar and independent shape validation prevent incomplete structures being reported as completed. The kernel still validates real property semantics and proposal authority. The pinned llama grammar parser also passed 18 model-free acceptance/rejection cases.

Code output now uses typed JSON transport with one literal source string. A strict decoder rejects duplicate/extra fields, trailing values, malformed UTF-8/surrogates, NUL and declared size limits. Source content, CRLF, quotes, escapes and legitimate Markdown fences are preserved without trimming or fence removal. Partial protocol is never published as code. Cancellation is rechecked before publication; all writes still need review and approval. JSON-encoded evidence and exact selections distinguish editable source from metadata, and their encoded size participates in tokenizer budgeting.

An intermediate generic fence-stripping implementation was withdrawn because it could damage legitimate Markdown source. Its apparent code-comment pass does not count. The initial typed-source attempt then copied evidence labels; that failure is also retained. Independent review confirmed the repaired source-preservation, budgeting and publication boundaries. A conservative review guard still refuses an unedited added whole-fence wrapper for known programming languages; it never changes source bytes.

The final 14-mode run took 615 seconds. Recorded generation latencies range from 26.2 to 118.8 seconds and sampled worker resident memory from 4.05 to4.56 GiB. Concurrent development builds/tests occurred, so these observations are **not controlled performance benchmarks**. The supervisor samples generation-worker RSS every 250ms; tokenizer, complete application footprint, exact transient peaks and full GPU/driver memory are not measured. Earlier zero readings meant unavailable because the offline sandbox rejected the setuid `ps` sampler. The final run uses the [direct native sampler](native-memory-sampling.md). No manual correction was needed for these 14 outcomes; real user correction time was not measured.

The [manifest and raw logs](../../evidence/completion/ai-modes/manifest.json) bind source, test binary, unchanged task criteria, helper/model hashes, hardware, retained failures and affected race results. Exact context is 8192 tokens with a 512-token output cap. The optional 7B pack's [memory/authority and held-out tests](memory-capacity.md) remain separate from these 14 modes; the original smaller-model failure remains retained.

Broader representative task sets, long-context quality, measured user correction effort, controlled latency/concurrency, native interface/candidate and platform checks remain open under AI-002/003.
