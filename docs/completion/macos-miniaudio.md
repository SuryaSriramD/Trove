# macOS miniaudio capture alignment

New macOS native and desktop builds select the pinned miniaudio 0.11.25 CoreAudio worker. This closes the selected-component implementation deviation for capture in TRD sections 19 and 30.3. It does **not** establish release readiness: actual TCC consent/denial, real microphone audio, signed-app attribution, and the packaged recording/reopen journey remain unqualified. The production helper was compiled and safely queried, but no microphone was activated.

## Consent, cancellation and retained bytes

AVFoundation is used only for microphone authorization. The worker checks AVCaptureDevice authorization before initializing CoreAudio. Authorized continues; denied/restricted fail; an undetermined state requests consent asynchronously. Unknown states fail closed. The helper embeds NSMicrophoneUsageDescription and carries the audio-input entitlement. These follow Apple's [macOS capture authorization guidance](https://developer.apple.com/documentation/bundleresources/requesting-authorization-for-media-capture-on-macos) and [asynchronous access API](https://developer.apple.com/documentation/avfoundation/avcapturedevice/requestaccess(for:completionhandler:)). Build and ad-hoc signing do not prove TCC attribution to the signed desktop app.

A shared, owned response object protects the asynchronous callback from using expired stack state. Consent waits at most 20 seconds and polls cancellation. The supervisor additionally requires initial PCM within 30 seconds, independently of the requested recording duration. A zero-byte permission progress event does not satisfy this deadline. Native setup that stops responding is cancelled through the control pipe, then terminated after two seconds and killed after another three seconds. The timer stops after positive PCM progress; the worker's existing five-second data watchdog governs later capture interruptions.

Framing remains protocol version 1. The v2 helper advertises exactly `record`, `capture_controls_v1` and `input_devices_v1`. An `inputs` request enumerates opaque input IDs without requesting authorization or starting capture. The selected ID is checked again at recording start; a missing selected input fails rather than silently using another device. An empty `inputDeviceId` explicitly selects the system default.

Every `record` request includes `inputDeviceId`, `synthetic` and `requestPermission`. Production rejects synthetic capture and unapproved recording before permission/device setup. The desktop requires a participant-disclosure acknowledgement; this records the user's acknowledgement and does not notify participants or establish their consent automatically.

Pause, resume and stop use a unique `controlId` and receive `control_ack` with the exact action, resulting state, captured sample count and duration. The helper drains and flushes captured bytes before acknowledging. The desktop checkpoints those bytes and persists the acknowledgement in the recording journal before reporting the control as complete. Timeouts remain uncertain/recoverable rather than optimistically reporting a pause or completed stop. A repeated acknowledged control does not duplicate its journal effect.

Captured duration comes from 16 kHz mono float32 samples, not elapsed wall time. Pauses stop accepting samples; the journal stores separate pause gaps and interruption gaps at sample offsets. An interruption whose duration is unknown remains explicitly unknown. The 128 KiB ring, two-hour/512 MiB bounds, owned control/audio descriptors and retained staging-file ownership remain. Start requires at least 64 MiB free; later storage failures stop capture while retaining flushed audio for recovery. Permission/device state and signal detection remain separate. Silence is valid data and does not prove microphone access.

## Build and compatibility

    python3 scripts/build-microphone.py --target darwin-arm64 --cache <verified-cache> --output <native-bundle>

The builder uses the verified miniaudio header from the pinned llama.cpp archive, Objective-C++/ARC for consent, and statically selected CoreAudio. It links only Apple system libraries/frameworks. The same production runtime identity, miniaudio-0.11.25-trove-microphone-v2, is used across the three supported target recipes. Source/header/helper hashes, compiler/SDK, embedded plist, entitlement and dependencies are recorded in microphone-worker.json; notices retain the selected MIT-0 license text.

build-native.py selects this helper by default. build-desktop.py rebuilds and validates it even when supplied a legacy Swift bundle, then replaces only the capture manifest record and upgrades legacy metadata to manifest v2. This prevents new candidates from silently retaining Swift while claiming miniaudio alignment. The legacy Swift `trove-microphone-v1` identity remains readable on macOS through the legacy capture boundary. Current input selection and acknowledged controls require the v2 helper. New desktop packages must contain v2 and all three required capabilities; legacy/test-only helpers are not acceptable substitutes for current qualification.

The separate --fixture-only executable excludes all device backends at compile time and cannot enter a production bundle. Its optional --fixture-startup-stall supports deterministic timeout tests. Production contains neither the waveform generator nor a self-test switch. QualificationSmoke explicitly reports production synthetic capture as not_run with releaseQualified=false; actual microphones are never opened by runtime smoke.

## Reusable device-free prerequisite check

With an already verified production bundle, this focused check starts the helper only to inspect its hello and submit requests that cannot authorize capture:

```sh
TROVE_MICROPHONE_PRODUCTION=<verified-native-directory> \
  go test ./internal/native \
  -run '^TestProductionMicrophoneRejectsBeforePermissionOrDevice$' -count=1
```

It requires the exact v2 runtime, framing, architecture and three capabilities. Valid-shaped unapproved capture, forbidden synthetic capture and a missing required input field must all return an error with `permissionState=not_requested`, `deviceState=not_accessed` and zero written bytes. No microphone is opened. The separate `TestRealMicrophoneControlCancellationAndParentExit` name is historical: that test uses synthetic PCM, not a real microphone.

Use the repository's pinned Go/cache configuration and retain the command, source/helper hashes and raw output in a fresh evidence location. Omitted helper variables produce a skip, never a prerequisite pass. The [native acceptance prerequisites](macos-native-acceptance.md) distinguish this static/protocol check from device, login-item, live-account and release qualification.

## Historical evidence and remaining release checks

Exact sources, helper identities, commands and retained logs are in [macos-miniaudio-evidence.json](../../evidence/completion/macos-miniaudio-evidence.json). The original implementation record reports the following on the available Mac; these historical results do not certify the current v2 candidate:

- Production CoreAudio compilation, ad-hoc signature/entitlement, embedded usage-description inspection and dependency checks.
- Production hello and rejection of an unapproved request or synthetic request before any authorization query or device setup.
- Eleven focused race tests using separate fixtures or pre-consent production rejection: exact PCM limits, retained cancellation, parent EOF, startup timeout, no-device consent-state wait outcomes, invalid input, fixture rejection and legacy Swift compatibility.
- Ordinary native race suite and seven targeted Python build/manifest tests.
- Windows/Linux amd64 Go test cross-compilation; no target test executable was run.

The release gate still needs an explicitly authorized real microphone, input selection, acknowledged pause/resume/stop, first-use consent, denial/restriction, cancellation while consent is pending, granted-but-absent device, silent input, device interruption, low-space recovery, long recordings, app termination, exact retained source bytes/gaps after reopen, and signed-app attribution on the exact packaged candidate. Repeat applicable device and lifecycle cases independently on Windows and Linux. No model quality or performance claim is made by this package.
