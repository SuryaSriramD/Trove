# macOS miniaudio capture alignment

New macOS native and desktop builds select the pinned miniaudio 0.11.25 CoreAudio worker. This closes the selected-component implementation deviation for capture in TRD sections 19 and 30.3. It does **not** establish release readiness: actual TCC consent/denial, real microphone audio, signed-app attribution, and the packaged recording/reopen journey remain unqualified. The production helper was compiled and safely queried, but no microphone was activated.

## Consent, cancellation and retained bytes

AVFoundation is used only for microphone authorization. The worker checks AVCaptureDevice authorization before initializing CoreAudio. Authorized continues; denied/restricted fail; an undetermined state requests consent asynchronously. Unknown states fail closed. The helper embeds NSMicrophoneUsageDescription and carries the audio-input entitlement. These follow Apple's [macOS capture authorization guidance](https://developer.apple.com/documentation/bundleresources/requesting-authorization-for-media-capture-on-macos) and [asynchronous access API](https://developer.apple.com/documentation/avfoundation/avcapturedevice/requestaccess(for:completionhandler:)). Build and ad-hoc signing do not prove TCC attribution to the signed desktop app.

A shared, owned response object protects the asynchronous callback from using expired stack state. Consent waits at most 20 seconds and polls cancellation. The supervisor additionally requires initial PCM within 30 seconds, independently of the requested recording duration. A zero-byte permission progress event does not satisfy this deadline. Native setup that stops responding is cancelled through the control pipe, then terminated after two seconds and killed after another three seconds. The timer stops after positive PCM progress; the worker's existing five-second data watchdog governs later capture interruptions.

The protocol, descriptor slots, explicit recording gesture, 16 kHz mono float32 format, 128 KiB ring, two-hour/512 MiB limits, flushing and retained staging-file ownership remain unchanged. Permission/device state and signal detection remain separate. Silence is retained as valid data and does not count as proof of microphone access.

## Build and compatibility

    python3 scripts/build-microphone.py --target darwin-arm64 --cache <verified-cache> --output <native-bundle>

The builder uses the verified miniaudio header from the pinned llama.cpp archive, Objective-C++/ARC for consent, and statically selected CoreAudio. It links only Apple system libraries/frameworks. The same production runtime identity, miniaudio-0.11.25-trove-microphone-v1, is used across the three supported target recipes. Source/header/helper hashes, compiler/SDK, embedded plist, entitlement and dependencies are recorded in microphone-worker.json; notices retain the selected MIT-0 license text.

build-native.py selects this helper by default. build-desktop.py rebuilds and validates it even when supplied a legacy Swift bundle, then replaces only the capture manifest record and upgrades legacy metadata to manifest v2. This prevents new candidates from silently retaining Swift while claiming miniaudio alignment. Existing Swift runtime identities are accepted on macOS for reading old bundles; they are not accepted on Windows/Linux.

The separate --fixture-only executable excludes all device backends at compile time and cannot enter a production bundle. Its optional --fixture-startup-stall supports deterministic timeout tests. Production contains neither the waveform generator nor a self-test switch. QualificationSmoke explicitly reports production synthetic capture as not_run with releaseQualified=false; actual microphones are never opened by runtime smoke.

## Evidence and remaining release checks

Exact sources, helper identities, commands and retained logs are in [macos-miniaudio-evidence.json](../../evidence/completion/macos-miniaudio-evidence.json). The following passed on the available Mac:

- Production CoreAudio compilation, ad-hoc signature/entitlement, embedded usage-description inspection and dependency checks.
- Production hello and rejection of an unapproved request or synthetic request before any authorization query or device setup.
- Eleven focused race tests using separate fixtures or pre-consent production rejection: exact PCM limits, retained cancellation, parent EOF, startup timeout, no-device consent-state wait outcomes, invalid input, fixture rejection and legacy Swift compatibility.
- Ordinary native race suite and seven targeted Python build/manifest tests.
- Windows/Linux amd64 Go test cross-compilation; no target test executable was run.

The release gate still needs an explicitly authorized real microphone, first-use consent, denial/restriction, cancellation while consent is pending, granted-but-absent device, silent input, device interruption, long recordings, app termination, retained source bytes after reopen, and signed-app attribution on the exact packaged candidate. Repeat applicable device and lifecycle cases independently on Windows and Linux. No model quality or performance claim is made by this package.
