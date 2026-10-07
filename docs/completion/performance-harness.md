# Completion performance harness

This package repairs reporting and evidence preservation; it does not resolve the application's startup delay or qualify a release. The original `be6fc81` observation of **8,399.656 ms** remains a failed threshold result. The [manifest](../../evidence/completion/performance-harness/manifest.json) binds the source files, retained failing regression run and passing focused checks.

## Shell measurements and diagnostics

Normal `benchmark-shell.py` runs now return nonzero unless every requested launch completes, the executable stays unchanged, and every profile meets the existing **2,500 ms** interpretation of approximately two seconds. `measurementPassed` retains its narrow collection meaning. `shellTargetPassed` is the threshold result; missing readiness, timeout, partial profiles and slow successful launches cannot pass it. Existing final reports, logs, traces and samples are never overwritten, including dangling links. Final result creation is exclusive.

Use `--diagnostic-slow-ms 2500` only for a separate investigation run. If the owned process is still running after that parent elapsed interval, the runner attempts one bounded native sample before waiting for the remaining process deadline. The trigger measures parent launch through shutdown, not the app's Go-initialization-to-shell metric; it can therefore observe startup or shutdown. Sampling may perturb execution. Reports set `instrumented: true` and `shellTargetPassed: false` for the entire diagnostic run, even if no sampling was needed. Diagnostic exit zero means collection completed only. Neither normal nor diagnostic success sets `releaseQualified`.

The sampler may be unavailable or the process may exit during sampling; `diagnosticSamplingAttempted` does not claim a sample exists. Only an existing sample receives a path in the report. A later timeout does not repeat sampling into the same file. The ordinary 60-second process deadline and termination cleanup remain bounded.

Example normal run, after other builds and model work have stopped:

```sh
python scripts/benchmark-shell.py --app <frozen-app> \
  --profiles <benchmark-service.json> --iterations 20 --output <new-shell.json>
```

For investigation, use a different output path and add `--diagnostic-slow-ms 2500`. Twenty iterations over the added empty profile and 100/1,000/10,000-page fixture profiles yield 80 fresh processes. This package has not run those launches. OS caches are not flushed, and no independent clean-Mac claim follows.

## Hybrid retrieval at every requested scale

`benchmark-phase3.py` and its actual Go benchmark now run installed-model hybrid retrieval on **every requested profile**. `--hybrid-iterations` defaults to **30** and is forwarded explicitly; keyword iterations default to 100. Both counts are bounded to 5–1,000. At most 16 distinct positive page counts up to 100,000 are accepted. Existing workload contents and independent query assertions are unchanged; each scale indexes its own workspace with the verified native embedding worker.

```sh
python scripts/benchmark-phase3.py --cache <pinned-cache> --models <installed-models> \
  --native-bundle <frozen-native-bundle> --pages 100,1000,10000 \
  --iterations 100 --hybrid-iterations 30 --output <new-service-directory>
```

The runner requires the exact requested profile sequence and finite nonnegative samples matching each requested keyword/hybrid count. A successful child exit with hybrid results only for the first profile now fails completeness. `--skip-hybrid` remains an explicitly model-free measurement and cannot establish hybrid performance. `qualifiedMeasurement` concerns collection, coverage and unchanged source/resources; a release evaluator must separately apply the keyword/hybrid thresholds to the recorded samples and required profiles. Service measurements exclude Wails IPC and paint.

Each service output directory must be new. The Go executable also reserves its output with exclusive creation before workspace setup, preserving an existing file or dangling link even when called directly. An interrupted direct run may retain an empty reserved report; it is invalid evidence and requires a new path for retry. Raw failure evidence is retained rather than reused.

## Verification

The first 14-test run against the original production harnesses recorded six failing assertions and three errors for the not-yet-added diagnostic interface. The actual regressions include false-success startup exit, first-profile-only hybrid acceptance, incorrect sample/profile acceptance and evidence overwrite. All **16 focused Python tests now pass**, including added bounds and single-sample timeout coverage. The Go package compiles; an actual built Go executable passes two existing-output refusal checks without creating a workspace. No models or timed workloads were invoked. Existing sqlite auto-extension deprecation output is retained in the compile log.

The source package does not change any historical candidate identity, recorded timings or release status. Actual hybrid measurements at larger scales and controlled normal startup measurements on a newly frozen app remain pending.
