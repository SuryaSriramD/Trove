# Native startup stage evidence

Sources: `ec851df` followed by overhead correction `ce0973d`. [Candidate and evidence manifest](../../evidence/completion/startup-trace-ce0973d/manifest.json); [normal launch report](../../evidence/completion/startup-trace-ce0973d/normal-startup/shell.json); [stage analysis](../../evidence/completion/startup-trace-ce0973d/stage-analysis.json).

`TROVE_STARTUP_TRACE` now has a production implementation. The supplied absolute output path must not already exist. Each process emits at most 14 fixed stage names with monotonic elapsed milliseconds, capped at 4 KiB in a mode-0600 file. No workspace path, title, content, argument, token, account or environment value enters the trace. Invalid, existing or unwritable sinks disable tracing without failing startup. Writes are ordinary bounded writes; there is no synchronous fsync or power-loss durability guarantee. Normal disabled tracing does not inspect snapshot JSON; even enabled tracing stops inspecting it after the first recorded snapshot.

Stages distinguish process entry, the launch-channel check, Wails dispatch, native initialization, workspace opening, startup completion, DOM readiness, snapshot readiness, renderer shell readiness, rollback commit and shutdown. These are lifecycle milestones, not a profiler: intervals can include asynchronous work and scheduling, and do not identify a root cause by themselves.

## Checks and controlled sample

The corrected focused race run passed 26 top-level tests (88 pass events including subtests). Tests read the actual emitted file, verify fixed fields, monotonic order, private permissions, size bound, concurrent deduplication and preservation of existing/symlink targets. The earlier synchronous prototype also emitted real native Wails stages; its log is preserved and is **functional emission evidence only**, superseded by the correction.

After all peer/root heavy builds and tests paused, the corrected clean-source ad-hoc candidate ran exactly five normal launches on a cloned, existing synthetic 1,000-page workspace. It used a private HOME, empty model directory and OS network denial. Every launch emitted actual trace and shell-report files and shut down normally. No inference, account access or device action ran. The executable remained unchanged.

Go process initialization to rendered canonical snapshot plus two frames was **739.506, 556.906, 718.660, 828.908 and 1530.865 ms**. All five satisfy the benchmark's explicit 2,500 ms interpretation of approximately two seconds. Parent launch-through-shutdown timings remain separately recorded and include work outside that Go-shell interval. OS caches were not flushed; this is a developer-machine, synthetic-workspace sample.

In the slowest sample, workspace open took 11.967 ms, native initialization 18.100 ms, and startup-complete to DOM-ready took 668.667 ms. DOM-ready to snapshot-ready was 253.805 ms and snapshot-ready to shell-ready 143.815 ms. This supports investigation of the UI/browser startup interval for that sample; it does not identify the cause of a different launch.

The earlier integrated `be6fc81` candidate's **8399.656 ms Go-shell outlier remains a failed threshold observation and is not erased by these five passes**. Its trace environment variable had no implementation, so its cause cannot be recovered from missing stage data. The rollback candidate's separate 9.293-second parent exec-to-ack time is a different metric and must not be compared directly with the Go-shell threshold.

This `ce0973d` artifact predates later integrated rich-text/support changes. It is a focused startup investigation candidate, not a qualified integrated release. No generalized performance, clean-install, signing/notarization or platform release gate closes here. Future outliers on a new candidate can now retain bounded stage evidence without adding snapshot parsing or synchronous durability overhead when normal tracing is disabled.

## Evidence reuse guard

Follow-up harness source `32c7e59` refuses any preexisting planned report, log, trace or sample before launching an application or overwriting per-run output, including a dangling trace symlink. It also refuses an existing final result path and exclusively creates logs. This is necessary because native trace creation intentionally preserves an existing file. Seven Python harness tests pass; the new regression seeds a later iteration's evidence, proves no process starts, and confirms prior bytes/links remain intact. The guard does not change the app binary or the five measurements above; no timing rerun was needed.
