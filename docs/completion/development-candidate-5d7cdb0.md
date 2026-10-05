# Development candidate 5d7cdb0

This is a checked development snapshot, not an approved release. Later provider
and image changes require their own affected checks. The exact source, app files,
native helpers, used model packs and hardware are recorded in
[the manifest](../../evidence/completion/candidate-5d7cdb0/manifest.json).

Source: `5d7cdb038bde74458df57d511cb949af5a892358`.
Application binary SHA-256:
`4d6edbcdc267d2d168e431542ec1fbadb8b6ec78dc47e9b91bed0881e3d0d37b`.
The application was built from an isolated source snapshot, production frontend
assets and copied native resources, then ad-hoc signed for the development Mac.
Temporary build artifacts were lost when the execution environment restarted;
their durable manifests and reports remain. A rebuilt application is a new
candidate unless its complete identity is independently verified.

## Passing checks

The combined Go run passed 544 tests, skipped 21 explicitly optional checks, and
reported no failures. Frontend unit tests and the production build passed.
Skipped tests remain listed in the manifest; the combined run does not silently
replace native, model, account or device evidence with fixtures.

Packaged checks ran in separate processes with system-only PATH and OS-denied
network access. All recorded steps passed:

- [P0 journeys](../../evidence/completion/candidate-5d7cdb0/p0/packaged-integration.json):
  import, canonical page/record edits, keyword search without models, stale and
  forgotten memory exclusion, retained interrupted recording bytes, reopen and
  backup/restore equivalence.
- [P1 journeys](../../evidence/completion/candidate-5d7cdb0/p1/packaged-p1-integration.json):
  linked gallery identity, exact formula values distinguished from captured
  values, reusable content, inert offline cards, automation occurrence
  deduplication, asset hashes, reopen and restoration.
- [Native workers](../../evidence/completion/candidate-5d7cdb0/native/packaged-integration.json):
  bounded TinyStories generation, BGE 384-dimensional embeddings, Whisper on
  the public JFK fixture and synthetic microphone bytes. TinyStories establishes
  worker framing only; synthetic audio does not qualify a physical microphone.

## Performance and retained failure

The [service benchmark](../../evidence/completion/candidate-5d7cdb0/performance/benchmark-service.json)
used 100, 1,000 and 10,000 synthetic pages, with two paragraph blocks per page.
Twenty keyword queries per profile had maxima of 3.64, 8.28 and 39.23 ms. These
measure validation, retrieval and response serialization through Go Dispatch;
they exclude Wails latency, painting, typed database queries and hybrid inference.
Source and helper hashes stayed unchanged throughout the measurement.

The [packaged shell benchmark](../../evidence/completion/candidate-5d7cdb0/performance/shell-benchmark.json)
**failed measurement integrity**. All five launches at 100 and 1,000 pages
reported readiness, with maxima of 1,219.99 and 1,252.36 ms. The empty and
10,000-page profiles each completed one launch, then the second launch produced
no readiness report within 60 seconds and was terminated. The completed large
sample was 1,901.07 ms. Timing only the successful launches does not qualify
startup reliability or the approximately two-second target. This failure remains
open pending diagnosis and a new candidate's repeated launch checks.

## Still unqualified

No independent clean Mac, physical microphone/permission journey, live provider
or Notion account, public HTTPS deployment, Developer ID signing/notarization,
update/rollback/uninstall journey, Windows or Linux qualification is established
here. Local model factual/memory quality failures remain open. The native helpers
in this snapshot target macOS 27.0 arm64; broader OS compatibility is not implied.
