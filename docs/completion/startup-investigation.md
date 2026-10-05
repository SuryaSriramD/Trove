# Native startup investigation — 5 October 2026

The two missing readiness events on candidate `5d7cdb0` remain unresolved. They
have not been relabelled as successful startup or removed from the original
candidate report. The old binary and temporary workspace were unavailable after
the development host reset, so its exact failure could not be replayed.

A new diagnostic app was built from source `5d7cdb0` with the retained
[diagnostic-only patch](../../evidence/completion/startup-investigation-2026-10-05/diagnostic-only.patch).
The patch records native startup, DOM-ready, snapshot, renderer and animation-frame
milestones. It is evidence tooling, **not a proposed production fix**. Its binary,
source archive, patch, helpers, OS and hardware are bound in the
[manifest](../../evidence/completion/startup-investigation-2026-10-05/manifest.json).
The app is ad-hoc signed and uses copied baseline helpers. No model inference,
real microphone, vendor account or private workspace was involved.

On this app, 30 empty-workspace probes and 20 synthetic 10,000-page probes all
completed. An additional five of each passed using the corrected benchmark
runner: 60 successful native launches in total. Empty-workspace timings were
0.78–2.29 seconds; the 20 large-workspace probes were 0.76–1.78 seconds. The final
runner maxima were 0.96 seconds and 1.50 seconds respectively. Every launch used a
new process and an OS network-denial policy; disk caches were not flushed.

Two traces showed the shell initially hidden, with animation callbacks arriving
when it became visible about 0.25–0.51 seconds later. That supports visibility
throttling as one plausible explanation for a missing animation milestone; it
does **not** establish the cause of the earlier 60-second failures. The new
instrumentation also changes timing, so these successful probes cannot prove that
the original intermittency is fixed. REL-007 still requires the next release
candidate's full workload and platform qualification.

The investigation found and fixed a separate measurement error: a profile with
one fast successful launch and a subsequent timeout previously displayed
`approximatelyTwoSeconds: true`. The runner now requires every requested sample
to complete, records requested/attempted/completed counts, and records timeouts
independently of process exit code. A process that handles termination and returns
zero remains a failed launch. Malformed timings also fail without discarding
prior samples. Failure logs, optional startup traces and a bounded sample of the
stalled test process are retained beside the benchmark report.

Six deterministic runner tests cover partial profiles, clean-exit timeouts,
invalid timings, complete fast and slow samples, and invalid iteration counts.
They pass. The [retained evidence](../../evidence/completion/startup-investigation-2026-10-05/runner/shell-benchmark.json)
is a diagnostic result for this development app, not a replacement release gate.
