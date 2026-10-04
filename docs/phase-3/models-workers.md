# P0 model and native worker qualification

Status: native model and worker checks passed after Phase 2 completed. Acquisition acceptance passed, and the network-denied run against an immutable copy of the Phase 2 package passed instruction generation, embedding, speech transcription, reviewed meeting summaries, cancellation/crash recovery, transcript input fences and the complete one-hour fixture. Actual microphone permission/device behavior is evaluated separately by the packaged-app gate; synthetic audio is not evidence for that gate.

## Scope

The P0 desktop uses the bundled, pinned llama.cpp, whisper.cpp and microphone helpers. Model weights live in a separate local cache. Ordinary generation, embedding and transcription never invoke acquisition. There is no cloud provider in these paths. The application reports a missing or incompatible pack instead of installing weights or calling a provider implicitly.

The public pack catalogue is `native/models/catalog.json`; each entry records the byte length, SHA-256, source revision, upstream license and license URL, matching native runtime commit, architecture and memory estimate. Native helper digests and revisions are recorded by the build in `Contents/Resources/native/manifest.json`. No pack hashes changed during this work.

| Pack | P0 role | Installed size | Declared memory estimate | Tested limits |
| --- | --- | ---: | ---: | --- |
| Qwen2.5 1.5B Instruct Q5 | Local assistant and reviewed meeting notes | 1,285,494,304 bytes | 3 GiB | Source-grounded answer and constrained meeting-note fixtures; no general usefulness certification |
| Qwen2.5 0.5B Instruct Q5 | Compact local assistant | 522,186,592 bytes | 2 GiB | Streamed source-grounded answer fixture |
| BGE small English F16 | Local semantic retrieval | 67,308,128 bytes | 512 MiB | 384-dimensional native embedding and retrieval fixtures; English |
| Whisper tiny English | Compact local transcription | 77,704,715 bytes | 512 MiB | Public English speech, durable minute units and one-hour duration fixture; not noisy-meeting or multilingual accuracy |
| TinyStories 15M Q4 | Runtime test fixture only | 19,077,344 bytes | 512 MiB | Not offered as an instruction assistant |

BGE and Whisper are supported P0 packs. Their previous `qualificationOnly` markers were removed; the provenance file now states their narrow tested limits. Existing installed manifests can be refreshed by choosing acquisition again: a valid local file is verified and the manifest updated without a network transfer. TinyStories retains its runtime-only marker.

## Acquisition failures and recovery

The manager now checks that the model volume has the required remaining bytes plus a 16 MiB reserve before acquisition/import. A server that ignores a resumed Range request is admitted again for the full replacement size before old partial bytes are discarded. Available disk is checked during copying at intervals of at most 8 MiB, and write failures caused by exhausted space or quota are reported as resource exhaustion.

A transfer interrupted by cancellation or disk exhaustion keeps its partial bytes and ETag metadata. A new manager resumes HTTP acquisition using Range and If-Range. Interrupted local imports have a persisted visible state and can be retried by selecting the local file again; the app does not retain access to arbitrary source paths. Files with an invalid digest or container magic are never installed. Corrupt complete transfers now show a corrupt state, discard their untrusted partial bytes, and can be downloaded again; previously a complete corrupt `.part` caused every retry to fail on the same bytes.

`evidence/phase3/model-acquisition.jsonl` records race-detector tests for incompatible architecture/runtime/format/identity/hash/memory/kind, digest failure and successful retry, invalid magic, local import interruption, HTTP interruption across manager restart, low-disk admission, disk exhaustion during transfer and resume, invalid Content-Range, HTTPS downgrade rejection, lease/removal fencing and missing-pack behavior. The low-disk tests inject the available-space response; they do not fill the physical user volume.

## Offline execution and lifecycle

The native qualification command explicitly denies all network operations for the test process and child helpers:

```sh
GOCACHE=/private/tmp/trove-phase0-tools/go-cache \
GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod \
TROVE_NATIVE_INTEGRATION=1 TROVE_LONG_AUDIO_INTEGRATION=1 \
TROVE_MODEL_ROOT=/private/tmp/trove-phase1-models \
TROVE_NATIVE_BUNDLE=/private/tmp/trove-phase3-native.Ej0EhX \
/usr/bin/sandbox-exec -p '(version 1) (allow default) (deny network*)' \
/private/tmp/trove-phase0-tools/go/bin/go test -race -count=1 -json \
  -tags 'fts5 sqlite_omit_load_extension' \
  ./internal/native ./internal/workers ./internal/workspace \
  -run 'TestReal|TestTranscriptCommitAtomically|TestTranscriptAtomicRebase|TestTranscriptRebase'
```

The passing log is `evidence/phase3/native-offline-checkpoint.jsonl`. This developer qualification uses Go to run tests; the packaged application uses the bundled native executables and does not require a separately installed development runtime. Synthetic capture establishes descriptor/audio handling only. Actual microphone permissions and device input require the separate packaged-app qualification.

The microphone helper stops when its parent's control pipe reaches EOF and checks cancellation while waiting for the permission response. This prevents a crashed desktop parent from leaving capture or a permission wait active. Its synthetic fixture emits and flushes 50 ms PCM chunks, allowing real control-pipe interruption without capturing private audio. `evidence/phase3/microphone-lifecycle.jsonl` records passing race-detector tests against the rebuilt Swift executable: both explicit cancellation and parent-pipe EOF stop promptly and preserve a nonempty, partial 16 kHz mono float32 recording. The updated helper source was compiled and signed independently; the final package must include that source's rebuilt helper.

During the packaged-app microphone journey, metadata inspection confirmed actual AVAudioEngine capture, growing PCM and durable checkpoints. The saved draft recorded 69,589 ms, 4,453,756 captured float32 bytes and 69 journal segments. Its canonical PCM16 WAV has a valid 44-byte header and a 69.58994-second duration. Only file metadata and the WAV header were inspected; private audio samples were not read. See `evidence/phase3/native-microphone-metadata.json`. TCC attributed the permission request correctly to `local.trove.desktop`; ad-hoc application rebuilds caused changed code requirements and new prompts. User-visible playback and permission interactions remain part of the main packaged-app report.

This journey exposed a separate canonical metadata issue: a captured meeting always started at duration zero until transcription finished. `meeting.create` now derives duration from bounded RIFF headers of its authorized attached file, preserving the captured state and avoiding any model dependency. Unsupported or malformed headers retain unknown duration. `evidence/phase3/recording-duration.jsonl` verifies saved/recovered duration before transcription, malformed header bounds and existing meeting/transcript source fences. This needs no schema migration; previously saved zero-duration meetings are not silently rewritten.

The packaged WKWebView media control did not load duration or advance playback for that valid WAV served directly through Wails' custom scheme. The file handler already uses authorized file access and `http.ServeContent`, and Wails forwards response headers and range requests. Correcting the WAV MIME type alone did not resolve the observed native playback failure. `MeetingPanel` now fetches that same authorized route into a browser-owned Blob URL. It shows loading and failure states, aborts pending requests on selection changes or unmount, stops the previous player, and revokes each obsolete URL. It uses no base64, additional server or cloud service.

The complete browser suite passed 24/24 in `evidence/phase3/large-workspace-browser.json`. Its two new audio tests use generated PCM16 WAV fixtures and verify real four-second metadata, advancing playback, seeking, a second recording's independent duration, old-player pause and URL revocation, unmount cleanup, interrupted loading and a visible authorization failure. TypeScript checking also passed. This browser evidence establishes the media and lifecycle behavior; the rebuilt packaged WKWebView must separately demonstrate playback before the native UI gate can pass.

The initial run in `evidence/phase3/native-offline.jsonl` reached 58 of 60 minutes, then correctly rejected a helper hash mismatch when a concurrent build replaced its executable. The later passing run used an immutable bundle copy, preventing that qualification interference. The earlier failure remains in evidence rather than being overwritten.

The prior Phase 2 aggregate run failed partway through the one-hour fixture when a destination page edit raced a transcript commit. Current source already contains the dedicated atomic transcript commit: destination-page guards rebase under the writer transaction while the original meeting, file, deletion, destination identity, delegation and lease fences remain enforced. The deterministic regression inserts a user edit between guard snapshot and worker commit and verifies that both user content and transcript survive. No weaker source guard was introduced for Phase 3.

## Release interpretation

The one-hour fixture repeats public JFK speech to exactly 3,600 seconds (230,400,000 bytes of mono 16 kHz float32 PCM), transcribes all sixty minute units using the installed Whisper pack, and edits the same meeting page concurrently. It establishes duration, progress, commit and scheduling behavior. It is not a representative meeting accuracy benchmark, and its service-mutation latency does not establish the renderer's “no visible blocking” criterion.

The passing network-denied run completed the recognition workflow in 77.44 seconds, produced 694 transcript segments, and committed 37 same-page edits with a maximum observed mutation latency of 258.89 ms. These are the fixture's observations, not a broad performance claim. See `evidence/phase3/one-hour-native.json` for the exact run and the Phase 3 benchmark record for hardware and workspace assumptions.

Pack removal is refused while a pack is acquired or leased by a worker. Replacing the application must preserve the workspace and model cache; a rolled-back runtime can use only manifests matching its pinned runtime commit. The model manager adds no workspace migration. Full application installation, native permission, backup/restore, benchmark hardware and release promotion are recorded by the Phase 3 release gate.
