# P0 performance qualification

The completed service benchmark is bound to hardware, model bytes, production source hashes, and a stable copy of native executables. `evidence/phase3/benchmark-environment.json` reports `qualifiedMeasurement: true`, no metadata errors, no production source changes, and no native resource changes during the run. Raw timings are in `evidence/phase3/benchmark-service.json`; the final run completed at **2026-09-30 16:55:26 UTC**, including the final recording-duration backend fix.

These are public Go service measurements: request validation, SQLite retrieval, authorization, native query embedding when enabled, and response serialization. The measured service source remains unchanged for candidate 2. Wails bridge latency, browser rendering, native first-window appearance, and perceived editor responsiveness require the separate packaged application checks below.

## Defined host and fixtures

| Parameter | Measured configuration |
| --- | --- |
| Hardware | Mac16,12; Apple M4; 10 CPU cores; 24 GiB unified memory |
| OS | macOS 27.0, build 26A428, arm64 |
| Power | AC power; battery charging |
| Workspace/native snapshot storage | Internal macOS Data volume; synthetic workspaces in the user's temporary directory, copied native helpers under `/private/tmp` |
| Available storage at start | 15,075,332 KiB on the Data volume, as reported by `df`; volume reported 97% used |
| Native runtime | Pinned llama.cpp `d2e54583c7452353eb35d40431281f6ee984332f`; actual copied executable hashes recorded with the run |
| Keyword model configuration | No embedding provider/model manager attached to the workspace; model-free keyword path |
| Hybrid pack | BGE small English F16; 67,308,128 bytes; 384 dimensions; SHA-256 `f0b2fef971e8366438bfd2d9aefea1b0115919389448806d290237f638bae999` |
| Hybrid inference | Two CPU threads, 512-token context, no GPU offload, no reranker |
| Cache conditions | Five query warmups; ordinary warm OS filesystem caches; every query embedding starts a fresh native worker and loads the model |

The deterministic `p0-pages-v1` fixture creates two paragraph blocks per page, approximately 913 UTF-8 bytes of title/body per page, 25 project labels, and common search terms. It uses the normal mutation service, revision guards, canonical snapshots, and FTS indexing. It contains no attachments, database records, transcripts, or saved memories. This is a reproducible page-search profile, not a claimed representative distribution of every Notion export.

| Profile | Pages | Paragraph blocks | Canonical title/body bytes | Final workspace file bytes |
| --- | ---: | ---: | ---: | ---: |
| Small | 100 | 200 | 91,300 | 4,436,336 |
| Medium | 1,000 | 2,000 | 913,000 | 14,520,320 |
| Large | 10,000 | 20,000 | 9,130,000 | 137,375,744 |

The small final workspace includes semantic vectors; the other two do not. File sizes include the current database/journal files and are not physical disk allocation. No disk cache purge, thermal isolation, or controlled real-world dataset distribution is claimed. The native-worker agent confirmed its heavy inference qualification had finished before the final benchmark; other light desktop activity remained possible.

## Measured results

Keyword queries cycle through `release`, `telescope`, `café`, `project07`, and an absent term. Each requests at most 20 results under an unrestricted owner grant. There are 100 measured queries per profile after five warmups. The harness verifies successful, non-degraded responses and expected hit/absence behavior. Percentiles use nearest rank; all individual samples remain in the JSON.

| Operation | Profile | Samples | Median | p95 | Maximum | TRD target for this profile |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Keyword query | 100 pages | 100 | 1.48 ms | 2.15 ms | 2.54 ms | Pass: every observed sample <300 ms |
| Keyword query | 1,000 pages | 100 | 3.94 ms | 5.20 ms | 6.18 ms | Pass: every observed sample <300 ms |
| Keyword query | 10,000 pages | 100 | 24.14 ms | 33.97 ms | 38.31 ms | Pass: every observed sample <300 ms |
| Hybrid query, native BGE | 100 pages | 10 | 216.69 ms | 239.45 ms | 239.45 ms | Pass: every observed sample <1 second; no reranking |
| Idle edit commit | 100 pages | 20 | 0.91 ms | 1.44 ms | 2.06 ms | Diagnostic service timing |
| Idle edit commit | 1,000 pages | 20 | 0.88 ms | 1.42 ms | 1.70 ms | Diagnostic service timing |
| Idle edit commit | 10,000 pages | 20 | 0.73 ms | 1.16 ms | 1.35 ms | Diagnostic service timing |
| Warm kernel reopen | 10,000 pages | 5 | 4.84 ms | 5.51 ms | 5.51 ms | Diagnostic; excludes application shell |

Hybrid retrieval at 1,000 and 10,000 pages remains **unmeasured**. Scoped-query correctness has separate regression coverage, but these latency claims apply to the measured unrestricted owner scope. The samples do not establish a maximum latency bound for all machines, workspaces, or concurrent workloads.

Initial FTS indexing took 26.90 ms, 237.83 ms, and 2,796.26 ms for the three profiles. Populating the 10,000-page workspace through ordinary page/block mutation commits took 13.99 seconds. These are fixture setup timings, not Notion archive import throughput. Native BGE indexing of the small profile took 10.47 seconds; the benchmark renews job leases using the same heartbeat interval as the production runner.

## Fresh worker/model overhead

Ten additional single-query native embedding requests recorded existing worker progress milestones. Mean time from admission to the **Model loaded** milestone was **166.59 ms**; mean time from that milestone to **Embedding computed** was **5.38 ms**; complete native requests averaged **181.14 ms**. On a per-sample basis, **92.0%** of total elapsed time occurred before the model-loaded milestone.

The first interval includes manifest validation, full model hash verification, scheduling, process launch, and model loading. It is not an isolated measurement of model file loading. The OS cache was warm, but each worker process and model context was fresh. There is no persistent inference daemon or separately installed model server. No private timing instrumentation was added to the product path.

## Packaged shell

`evidence/phase3/shell-benchmark-candidate-2.json` records the qualified candidate 2 run at **2026-09-30 17:18:57 UTC**, using the same Apple M4 / 24 GiB / macOS 27.0 host and the defined synthetic workspaces above, plus an empty workspace. The executable SHA-256 is `5b3ac5c6a5f764bf39b8c28a56e9093c1fb7dd8d5a29f9faf0599fa5829b6e3e`, matching the packaged dependency audit. All 20 samples completed, the executable stayed unchanged, and every profile passed the declared approximation threshold of **2.5 seconds**. The 10,000-page result is above a strict two-second limit; the exact measurements are retained rather than described as under two seconds.

| Candidate 2 profile | Samples | Median | Maximum | Result |
| --- | ---: | ---: | ---: | --- |
| Empty workspace | 5 | 745.40 ms | 1,003.11 ms | Pass: approximately two seconds |
| 100 pages / 200 blocks | 5 | 840.27 ms | 954.43 ms | Pass: approximately two seconds |
| 1,000 pages / 2,000 blocks | 5 | 911.34 ms | 969.40 ms | Pass: approximately two seconds |
| 10,000 pages / 20,000 blocks | 5 | 2,238.23 ms | 2,315.36 ms | Pass: declared 2.5-second approximation |

The endpoint is Go process initialization through canonical workspace snapshot rendering plus two animation frames, including native validation and Wails initialization. Each sample launches a new packaged app process using system-only `PATH`, no installed model packs, and OS-denied network access. Workspaces are disposable clones of the closed benchmark fixtures. Filesystem caches were not flushed. These measurements exclude installer/Gatekeeper interaction and do not establish responsiveness during background work.

### First candidate history

`evidence/phase3/shell-benchmark.json` records the first candidate, whose executable SHA-256 is `0b7f3a3e5de1f2a0b9dedc790ee258c3a729de9af9a8a229ee9a3a90bc525705`, using the same endpoint and fixture definitions.

| First candidate profile | Median | Maximum | Result |
| --- | ---: | ---: | --- |
| Empty workspace | 736.88 ms | 910.99 ms | Observed samples below two seconds |
| 100 pages | 718.54 ms | 795.51 ms | Observed samples below two seconds |
| 1,000 pages | 831.41 ms | 855.08 ms | Observed samples below two seconds |
| 10,000 pages | 3,108.16 ms | 3,370.18 ms | Target missed; one additional launch exited without a readiness report |

This first candidate did not qualify the shell target (`measurementPassed: false`). Its results remain history, not evidence for the replacement executable. The harness separately records sample integrity and latency: a true `measurementPassed` alone does not establish the timing target; each profile's `approximatelyTwoSeconds` result must also pass. The harness interprets “approximately” as at most 2.5 seconds and retains exact timings for review.

Candidate 2 replaces repeated full-workspace scans in the renderer with page, block, backlink, and child-page indexes, and constructs body-text search documents when a switcher query needs them. Its browser regression covers 10,000 pages and 20,000 blocks, the full eligible page hierarchy, body search, and refresh. Browser readiness starts at the snapshot response, so it does not qualify packaged startup time.

The candidate 2 native results above independently verify the packaged endpoint after this renderer change. The changes do not alter the service benchmark source or native embedding helpers summarized above.

## Background transcription and remaining UI gates

`evidence/phase3/one-hour-native.json`, linked by `models-workers-verification.json`, records the native Whisper tiny English test: **3,600 seconds of repeated public JFK audio in 77.44 seconds**, 694 resulting segments, and 37 successful same-page editor mutations during transcription. Maximum measured edit commit was **258.89 ms**. The source is deliberately repetitive synthetic audio; this does not establish varied meeting accuracy, real microphone acoustics, or an hour of natural conversation.

The following TRD claims remain separate from this report:

- **Other shell workloads:** the packaged target passed for the four stated profiles using the declared 2.5-second approximation. Mixed workspaces, other hardware, and installer/Gatekeeper interaction remain outside this measurement.
- **No visible editing blockage:** the service mutation timings and background transcription test do not measure animation frames, keyboard-to-paint delay, IME behavior, or perceived responsiveness.
- **Import/transcription visible progress and recoverable interruption:** durable job/progress and checkpoint tests are recorded in data-journey and models/workers evidence. Their UI presentation must be checked in the packaged application.
- **Large workspace hybrid latency and large mixed Notion exports:** no performance qualification is claimed beyond the stated profiles.

## Reproduction and evidence history

Run `python3 scripts/benchmark-phase3.py` on a host with the pinned development Go toolchain, prebuilt native resources, and installed test packs. The script makes disposable synthetic workspaces, copies native resources before inference, records CPU/OS/storage/power and source/model/resource hashes, and fails qualification if measured source/resources change. `--skip-hybrid` runs the model-free profiles only. These developer tools are not end-user installation requirements.

Earlier results are retained separately. The preliminary run missed the keyword target at 10,000 pages and had a hybrid maximum above one second; these motivated the bounded keyword query implementation and a controlled final measurement. One resumed run failed because the initial benchmark harness omitted job lease heartbeats; the product runner already renewed them. Another intermediate run completed but flagged a concurrently rebuilt source native manifest even though copied measured resources were stable; the wrapper now checks the hashes of the exact copied resources. The successful final run is the only source-bound result summarized in the table above.
