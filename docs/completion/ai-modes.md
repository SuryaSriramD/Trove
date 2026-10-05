# Local AI mode evaluation

The latest applicable outcomes pass **13 of 14 fixed synthetic mode tasks** with the explicitly installed Qwen2.5 7B Instruct Q3 pack. Code commenting remains a quality failure because the model wraps otherwise valid Go source in Markdown fences. These are developer integration results across affected reruns, not a single all-mode pass on a final packaged candidate or a release qualification.

The independent task oracle was committed before inference and remains unchanged. It checks preserved facts and units, Markdown structure, canonical database field types/configuration, required diagram edges, Go syntax/signature/return expressions and comments, and unchanged canonical content. Generated code is never executed. Each case uses the public prepare, local generate and finish path; applicable cases also prepare a reviewed proposal. No generated write is approved by this evaluation.

| Modes | Latest outcome | Evidence source |
| --- | --- | --- |
| Summarize, explain, rewrite, shorten, expand, generate, code explain, organize | Pass | Initial run, `5292f6d` |
| Outline | Pass after requesting the format expected by the page editor | Affected run, `93c8b66` |
| Database, schema, Mermaid, code refactor | Pass | Final affected run, `0051808` |
| Code comment | Fail: surrounding Markdown fence requires correction | Final affected run, `0051808` |

The initial run passed 11/14; outline, schema and code commenting failed. The first repair aligned database/schema prompts with the existing canonical property contract and made the outline heading requirement explicit. Schema still failed because required top-level fields were omitted. The final adapter adds bounded required-field grammar and independent shape validation; existing canonical property/configuration validation remains authoritative at review. The pinned llama grammar parser passed 18 model-free acceptance/rejection cases, including advanced configuration and size/depth limits.

Independent review found that generic fence removal would corrupt valid Markdown source in code blocks. That implementation was withdrawn. Code modes now retain exact streamed, stored and reviewed bytes, including valid Markdown fences and unselected source. One complete transport fence may be removed only for database/schema/Mermaid structured drafts; nested, incomplete or accompanied fences stay literal. The intermediate code-comment pass obtained through source rewriting does **not** count.

The final review guard (`1e12104`) refuses an unchanged generated whole-fence wrapper for recognized programming languages when the original source was not fenced. It preserves the original draft, explains the required correction and permits a user-edited draft to proceed to review. Markdown, plain text and unknown languages remain literal. This guard is a draft-quality check, not a syntax validator or a code execution policy. Its regression and affected race checks pass: 35 top-level tests plus 8 subtests, no failures or skips. The actual-model code-comment failure remains open.

The [manifest](../../evidence/completion/ai-modes/manifest.json) binds each raw log, source revision, unchanged oracle, helper and model hash, and hardware. The model uses an 8192-token context and 512-token output cap, with networking denied during inference. Recorded generation latencies range from about 11 to81 seconds; these single observations are not a controlled performance benchmark. Peak worker RSS and real user correction time were not captured. The observed correction is removal of two fence lines; no time saving is claimed.

Workspace answering, memory authority/freshness and reviewed named-page creation have [separate 7B evidence](memory-capacity.md). Their stricter original 1.5B failure remains retained. Broader task sets, resource/latency measurement, correction-effort studies, native candidate interface checks and platform qualification remain required by AI-002/003.
