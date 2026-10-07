# Expanded local AI evaluation

This package adds executable evaluation breadth without changing production prompts, inference code, existing task rubrics, model selection or the shared proposal boundary. It is development evidence only. AI-002/003, OPT-001/003 and platform release qualification remain open.

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

Use `--family modes` for all 42 held-out mode cases. The macOS runner builds the test binary with offline Go resolution, denies networking through the OS sandbox during build/execution, omits ambient provider credentials, records source state and input/binary/output digests, and checks unchanged inputs afterward. Missing/duplicate/skipped cases, absent/mismatched reports, failed task or no-write assertions, malformed logs and nonzero execution all prevent an automated-corpus pass. Another operating system must supply separately qualified network-denial behavior; the script refuses to silently drop the macOS boundary.

The runner records sampled generation-worker RSS, token budget and wall latency. It does not measure full application/GPU peak memory, manual correction time or native Wails interaction, and never marks performance or release qualification true. Use a separately scheduled quiet benchmark to evaluate performance; a functional run amid development activity is not controlled performance evidence.

## Initial recorded checks

The package's initial evidence is under `evidence/completion/expanded-evaluation/`:

- 90 model-free subcases pass: 24 poisoned-oracle checks, 24 canonical seed/publication checks and 42 mode-outcome checks. Three parent tests also pass.
- Nine evidence-accounting tests pass, including skipped, missing, duplicate and mismatched report rejection.
- Two real 7B P0 canaries pass with networking denied, using the existing e481c6a helper bundle: `delivery-plain` and `delivery-memory`. Both retain canonical content and current citations. The observed 33.7s/17.5s latencies and 4.61/4.34 GB sampled worker RSS were collected during other development work; they are functional observations only.
- The first structural attempt failed on the new fixture's incorrect `pages.page_id` lookup; the canonical schema uses `pages.id`. Only that test setup was corrected, with its log retained. The first native launch used the repository root instead of the package fixture directory and failed before inference; its log is retained. The corrected package-directory canaries passed.

The complete 24-case and 42-case real-model sets have not yet passed or established quality. Full native-candidate journeys, representative authorized content, manual correction effort, independent installation and per-platform evidence remain separate gates.
