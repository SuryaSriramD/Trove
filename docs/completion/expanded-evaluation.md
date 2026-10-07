# Expanded local AI evaluation

This package adds executable evaluation breadth and one narrow cited-prompt clarification after a real formatting failure. Inference code, existing task rubrics, model selection and the shared proposal boundary remain unchanged. It is development evidence only. AI-002/003, OPT-001/003 and platform release qualification remain open.

## Executable corpora

The independently declared JSON inputs live in `internal/workspace/testdata/expanded-evaluation/`. They contain synthetic, invented project facts rather than private workspace content.

| Corpus | Current implementation | Meaning of a passing run |
| --- | --- | --- |
| P0 Q&A / memory | 24 cases: nine domains with/without approved formatting and historical memories, plus three absent/explicit-negative pairs | Correct field/value relationships, exact current-source supporting quotations, requested heading/order, abstention versus explicit negative evidence, current canonical citations and no canonical mutation. |
| Held-out modes | 42 cases, three for each of the existing 14 writing/structured modes | Independent task outcomes: preserved facts and numeric values; requested structure; exact typed database schemas; exact diagram edges; parsed Go function signature/body preservation and requested comments; proposal preparation without application. |

Both runners create disposable actual SQLite workspaces and seed pages/blocks/approved memories through canonical commands. They use `PrepareAIWithBudget`, the selected pack's native tokenizer/generator, `GeneratePreparedAI`, `FinishAI` and applicable proposal preparation. They do not bypass retrieval with supplied `AIEvidence`, fabricate model output or approve proposed writes. The exact 8,192-token context and 512-token output budget remain unchanged; each task has a three-minute deadline. The selected pack is mandatory, with no automatic fallback or download.

The P0 oracle is a deliberately bounded statement grammar, declared separately from production prompts: each claim must state its requested field/value, each quote must be an exact span of the current source supporting that value, and absent evidence must produce the existing abstention. Additional, missing, old or mismatched claims fail. This does not prove general semantic entailment outside these tasks. Structural tests poison the golden outcomes with invented claims, invented quotes, extra/missing claims and another fact's quote, demonstrating that these errors are rejected.

Open-ended writing, explanation and organization still require manual semantic review. Their automated reports explicitly carry `manualSemanticReviewRequired`; fact preservation is not renamed comprehensive quality. Code output is parsed as Go and compared to the independently expected function structure without comments; extra declarations, changed signatures/logic and missing requested comments fail. Database and diagram oracles check independent typed properties and exact edges, not production serialization output.

The original 14 fixed cases, strict memory/capacity cases, prior model failures and four held-out cases are unchanged. Neither new corpus replaces those gates. The optional verified 7B pack is the first intended target; smaller packs retain their recorded capability failures and are not implicitly qualified.

## Prepared but unfinished families

`context.json`, `memory-lifecycle.json`, `related.json` and `reviews.json` each define 12 scenarios and expected outcomes. These are preparation specifications, **not complete executable corpora or acceptance evidence**. Their statuses explicitly state which runner/input work remains. Existing production regressions still cover several individual boundaries, but those checks do not establish execution of these new sets.

`images.json` contains 24 slots. Six original deterministic diagrams and six pixel-boundary fixtures are present as PNGs. The six actual screenshot and six photograph slots are `authorized_source_pending`; no generated diagram substitutes for a photograph or real UI capture. Each pending source must be authorized, hashed and paired with locked questions/expected facts before any model run. Image runner integration and broad quality are still pending. Existing four-image/raw-model failures remain retained.

For context work, actual model-token framing/reservation and beginning/middle/end relevance must be measured; a character or word count cannot qualify native token behavior. Memory needs stale/forgotten restore, deletion, revocation and concurrent-change scenarios. Related-content needs real embeddings and independently labeled relevance, including a closer inaccessible page. Reviews need actual occurrence/approval/retry/source-freshness behavior, not only prose quality.

## Reproduction and evidence

Run model-free structural and evidence-accounting checks with the pinned toolchain:

```sh
go test -mod=readonly -tags 'fts5 sqlite_omit_load_extension' ./internal/workspace -run '^TestExpanded' -count=1
PYTHONDONTWRITEBYTECODE=1 python3 scripts/test-expanded-evaluation.py
```

The full native runner requires explicit local paths; use an unused output directory so failed attempts cannot be overwritten:

```sh
python3 scripts/run-expanded-evaluation.py \
  --family p0 \
  --go /absolute/pinned/go/bin/go \
  --go-mod-cache /absolute/pinned/go-mod \
  --go-build-cache /absolute/disposable/go-cache \
  --bundle /absolute/verified/native-bundle \
  --models /absolute/verified/models \
  --pack 01990000-0000-7000-8000-000000000106 \
  --output /absolute/new-evidence-directory
```

Use `--family modes` for all 42 held-out mode cases. Repeat `--case <fixture-id>` for a focused retry; the report explicitly sets `fullFamilyRun=false` and never counts that retry as a full-corpus pass. The macOS runner builds the test binary with offline Go resolution, denies networking through the OS sandbox during build/execution, omits ambient provider credentials, records source state and production/corpus/helper/model/binary/output digests, and checks unchanged inputs afterward. Output records split across Go log events are reassembled per test before accounting. Manifest replacement is atomic; at least 4 GiB of free space is required on both evidence and private temporary volumes before starting. This harness reserve is not a new product storage requirement. External scratch may hold build caches/evidence, but must not be presented as qualified private runtime storage. Missing/duplicate/skipped cases, absent/mismatched reports, failed task or no-write assertions, malformed logs and nonzero execution all prevent an automated-corpus pass. Another operating system must supply separately qualified network-denial behavior; the script refuses to silently drop the macOS boundary.

The runner records sampled generation-worker RSS, token budget and wall latency. It does not measure full application/GPU peak memory, manual correction time or native Wails interaction, and never marks performance or release qualification true. Use a separately scheduled quiet benchmark to evaluate performance; a functional run amid development activity is not controlled performance evidence.

## Initial recorded checks

The package's initial evidence is under `evidence/completion/expanded-evaluation/`:

- 90 model-free subcases pass: 24 poisoned-oracle checks, 24 canonical seed/publication checks and 42 mode-outcome checks. Three parent tests also pass.
- Fifteen evidence-accounting tests pass, including skipped/missing/duplicate/mismatched report rejection, split/interleaved/truncated output, atomic-write failure retention and free-space preflight.
- Two real 7B P0 canaries pass with networking denied, using the existing e481c6a helper bundle: `delivery-plain` and `delivery-memory`. Both retain canonical content and current citations. The observed 33.7s/17.5s latencies and 4.61/4.34 GB sampled worker RSS were collected during other development work; they are functional observations only.
- The first structural attempt failed on the new fixture's incorrect `pages.page_id` lookup; the canonical schema uses `pages.id`. Only that test setup was corrected, with its log retained. The first native launch used the repository root instead of the package fixture directory and failed before inference; its log is retained. The corrected package-directory canaries passed.

Neither the 24-case nor the 42-case real-model set has passed or established quality. The later complete P0 run below executed all 24 cases and retained three failures. Full native-candidate journeys, representative authorized content, manual correction effort, independent installation and per-platform evidence remain separate gates.

## First complete-corpus attempt: incomplete and failed

The 24-case attempt on source `bc964e2` began with a clean worktree, the verified 7B pack and the existing e481c6a native helpers, under OS network denial. Seven task reports passed before/around two failures:

- `research-memory` returned the two correct current facts but added a third claim, “The heading is Research brief.” The requested heading plus two factual lines became four lines. This is a real format/instruction-following failure; current-fact/citation authority and canonical no-write checks remained intact.
- `service-plain` failed to create the native generation request because the internal disk was full; saving the failed draft also reported a full database/disk. Fifteen subsequent cases have no recorded result. The evidence runner could not persist its final manifest, so the surviving initial manifest remains `started`. No complete test terminal inventory exists.

The raw logs and initial manifest are retained under `full-p0-incomplete/`. `reaccounted.json` parses the identical raw bytes with the repaired split-record accounting and explicitly fails the corpus. The initial parser missed the long research report because Go split its JSON across events; fixing that parser exposes the failure and does not relax the task oracle.

The cited system instruction now clarifies that formatting preferences control presentation and should not become extra factual claims unless the user explicitly asks about them. No fixed claim count, fixture name, answer fact, grammar clipping or post-generation deletion was added. The later unchanged-fixture retry below repeats the original failure: this clarification is **not an effective repair**. Model-free checks establish unaffected boundaries, not a repaired model outcome.

The affected model-free Go checks pass after the prompt clarification; the recorded report remains separate from real-model quality.

The internal data volume showed only 410 MiB available at failure. Only this task's disposable 292 MiB Go build cache was removed; later free space measured 2.7 GiB. The external disk has ample space for build caches/evidence but does not substitute for native private runtime storage. The runner now refuses another inference run below its 4 GiB headroom check. No user workspace, model pack, unrelated cache or prior evidence was deleted.

## Complete P0 retry on 74a6e35

After private-storage headroom exceeded the unchanged preflight minimum, all **24 cases completed: 21 passed and three failed**. The full run took 596.9 seconds; this was functional evaluation, not a controlled performance qualification. It used the same locked corpus, verified 7B pack, e481c6a helper bundle and denied-network policy. Production/corpus/model/helper inputs remained unchanged, every case produced a report, and there were no malformed or missing outcomes. Exact identities and raw results are retained in [the new manifest](../../evidence/completion/expanded-evaluation/full-p0-74a6e35/manifest.json).

| Failed case | Observed behavior | Unresolved acceptance |
| --- | --- | --- |
| `research-memory` | Correct facts and preferred title, followed by an extra factual claim explaining the heading preference. | Answer only the requested subject while applying relevant preferences. The earlier prompt clarification did not repair this failure. |
| `service-plain` | Correct support window repeated twice, followed by the correct escalation owner. | Honor the requested structure without duplicate claims. This retry reached inference; its failure is distinct from the earlier disk failure. |
| `project-negative` | Abstained even though the source explicitly stated that the project has no sponsor. | Distinguish explicit negative evidence from absent evidence. |

All 24 cases preserved canonical content; generation and publication completed in the three failing cases. Valid source attribution and no-write behavior therefore do not imply answer quality. No oracle, source fact, claim count or failed output was changed to obtain a pass.

Next repair should investigate typed memory-role separation in context construction, duplicate claim handling and explicit-negative reasoning with independent holdouts. A recommendation to preserve canonical memory kind separately from display titles has been reviewed as a direction only; it is not implemented or evaluated by this report. The 42 held-out mode cases and other unfinished families above remain pending.
