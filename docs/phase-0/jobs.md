# Jobs and native workers v1

`contracts/jobs.schema.json` defines persisted jobs, typed payloads, progress events, private worker requests and results. `schemas/services.sql` persists leases, attempts, checkpoints and committed result units. `reference_jobs.py` is a conformance oracle; the production Go scheduler implements this contract.

## Durable state machine

| Current | Allowed next | Meaning |
| --- | --- | --- |
| queued | running, cancelled | Await capacity or cancel before any work |
| running | succeeded, retry_wait, failed, cancel_requested, interrupted | A leased attempt owns work |
| retry_wait | running, cancelled | Retry only when persisted nextAttemptAt is due |
| cancel_requested | cancelled, interrupted | Worker is stopping; late success cannot commit |
| interrupted | retry_wait, failed, cancelled | Restart reconciles checkpoint, attempts and cancellation |
| succeeded, failed, cancelled | none | Terminal; explicit retry creates a new job with a retryOf relation in history |

Claim with `BEGIN IMMEDIATE`, compare state/revision and due time, increment attempt and mint a fresh random lease token. The supervisor heartbeats every 10 seconds with a 30-second lease; clocks use a monotonic timer while alive and persisted UTC instants across restarts. Backward wall-clock jumps are detected; restart reconciles jobs owned by the previous process while holding the exclusive workspace lock. Each checkpoint/result validates the lease token, job state and source revision again. An expired or replaced worker is fenced from commits even if its process later returns successfully.

Retry only classified transient failures (worker crash, temporary I/O, bounded memory pressure), at most five attempts including the first; default three. Exponential backoff starts at 1 second, doubles to a 60-second cap, with persisted bounded jitter. Invalid content/schema, missing approval, source conflicts and incompatible models fail without blind retry. Jobs waiting for user action retain diagnostics and a terminal failure; retry is explicit after the cause is corrected. The typed payload tag must match JobKind (for example TRANSCRIBE_AUDIO requires transcribe). Progress totals may be unknown; never invent a percentage. Sequence is monotonic per attempt, with `(jobId,attempt)` as the progress stream identity.

Cancellation is durable. Mark cancel_requested before signalling the worker, stop new units, request cooperative cancellation, then terminate its process group after 2 seconds and force-kill after 5 seconds. Only a supervisor-confirmed stop becomes cancelled. A success transaction that already committed wins and stays succeeded; a cancellation transaction that wins first rejects the result. Recording flushes durable segments before cancellation finishes. Cancelled jobs retain source audio/staging and report already committed units.

On startup inspect persisted running/cancel_requested jobs and live process ownership under the workspace lock. Reconcile completed unit receipts and retained files before retry. Expired running jobs become interrupted, then retry_wait if attempts and inputs permit, otherwise failed. Expired cancellation requests become cancelled. A job/unit idempotency key protects every canonical result transaction, stored with `(jobId,unitKey)`; a lost worker response cannot duplicate transcript segments, import rows or AI-approved writes. This is at-most-once local committed effect, not a claim of exactly-once execution. External side effects require their own reconciliation policy.

## Scheduler and resource contract

Default admission limits: one heavy native inference/transcription worker at once, two CPU extraction/index workers, one import coordinator, one writer and eight read connections. Recording and interactive edits have priority; chat follows, transcription next, indexing/import last. The heavy worker limit applies jointly to llama chat, llama embeddings and whisper. Increase only after a platform qualification record. Pause background indexing on memory pressure; unload idle models after five minutes.

Every native request declares at most 8 CPU threads, 16 GiB resident-memory budget (further capped at 50% of physical RAM minus a 2 GiB OS reserve), 2-hour wall time, 64 MiB total output and 32,768 output tokens. A default interactive request is 120 seconds/4,096 tokens; transcription is duration-dependent within the hard bound. Token/context budgets must also fit the selected pack. Reject a request if the model plus activation estimate cannot fit. The supervisor samples resource use and terminates violating workers; hard OS containment is platform qualification, not implied by monitoring. No swap-thrashing retry loop. Inputs use verified kernel handles and declared byte size, never renderer paths. Acquisition requires an explicit model-pack action; a worker never downloads a model or falls back to cloud inference.

## Private pipe protocol

Use two private pipes with 4-byte unsigned big-endian byte length followed by UTF-8 JSON. One frame is 1..1,048,576 bytes; reject its length before allocating. Read in chunks no larger than 64 KiB. No shell interpolation. stdout contains protocol frames only; stderr is redacted diagnostics with a 256 KiB ring and 1 MiB per-attempt retained log cap. A partial frame at EOF, invalid UTF-8/JSON, nonfinite number, unknown type/version, duplicate terminal event or wrong request/lease ID fails the attempt.

The worker sends a `hello` with protocolVersion, exact native commit, architecture and capabilities before requests. The supervisor matches the build manifest and rejects unknown versions. One active request per worker. Each request binds requestId, jobId, leaseToken, cancellationId, modelPackId, approved input handles and resource limits. The wrapper receives read-only descriptors/duplicated handles through the platform transport; opaque handle IDs alone do not authorize opening a path. Never pass workspace.db or raw credentials. `WorkerCancel` travels on the control pipe so cancellation cannot wait behind token backpressure.

Outputs are progress, token, result, error or cancelled. Sequence starts at zero and strictly increases per request. Token backpressure pauses the producer; progress may coalesce. A bounded outbound queue is 128 frames and 4 MiB; terminal frames are retained until acknowledged or recovered through the job record. Result refers to a supervisor-owned bounded result handle; input/output handle digests and sizes are verified before consumption. Result bytes validate as `WorkerResult`. Large embeddings/transcripts are streamed in bounded chunks into temporary storage; each vector is finite and exactly declared dimension, vector count matches locations, transcript offsets and source IDs are validated. Workers return proposed data; only workspace services can commit it.

Native resource isolation supplements supervision: minimal environment, controlled working directory, no ambient workspace write authority, no network-capable decoder inputs, executable hash verification, and eventual signed package helpers. Phase 0 validates native build/load compatibility on Darwin arm64. Release packaging, OS permissions, model inference accuracy/performance and full process containment remain independently recorded P0 tests.
