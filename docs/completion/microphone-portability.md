# C7/C8 bounded microphone capture

The Windows 11 x64 and Ubuntu 24.04 x64 microphone implementations and target-native build recipes are present. They are **not target-qualified**. This package was tested on Mac with a device-free fixture and the existing Swift synthetic helper. No real microphone was opened, no OS permission decision was exercised, and REL-009 remains open.

## Requirements and interfaces

The TRD sections 19 and 30.3 specify a small native miniaudio capture helper and independently qualified platform adapters. macOS keeps its existing Swift/AVFoundation helper and manifest identity. The portable worker uses miniaudio 0.11.25 from the existing pinned llama.cpp source archive; the exact header digest is recorded in native/microphone.lock.json. Source, compiler, helper and test identities are retained in [c7-c8-microphone-evidence.json](../../evidence/completion/c7-c8-microphone-evidence.json).

Protocol version 1 is preserved: stdin carries one framed bounded request, stdout carries hello/progress/result/error frames, slot 3 carries cancellation or parent EOF, and slot 4 is an already opened empty regular PCM staging file. The helper receives no recording pathname, URL, workspace database or arbitrary backend option. Request and control fields are strict; the Windows parent assigns its existing bounded kill-on-close job before sending the request. The model and recording services retain responsibility for explicit user recording gestures and durable workspace asset ownership.

The output remains mono 16 kHz float32 little-endian PCM. Requests are bounded to two hours and 512 MiB, with the smaller time/byte limit controlling capture. The callback performs no file I/O, allocations, locking or device lifecycle operations; a 32,768-frame single-producer/single-consumer ring caps queued PCM at 128 KiB. The writer flushes each consumed batch before reporting progress. Invalid/non-finite samples, ring overflow, device interruption, or five seconds without new frames fail explicitly. The caller's recording file is retained on cancellation and failure. These are bounded-memory and ordinary cancellation guarantees; actual-device scheduling, power-loss and crash-recovery qualification remain outstanding. The callback design follows the [miniaudio manual](https://miniaud.io/docs/manual/index.html) and [upstream capture example](https://github.com/mackron/miniaudio/blob/master/examples/simple_capture.c).

## Platform boundaries

| Platform | Production backend | Pending evidence |
|---|---|---|
| macOS arm64 | Existing Swift/AVFoundation helper unchanged | Actual microphone permission/device and packaged recording journey remain separate |
| Windows 11 x64 | Miniaudio WASAPI shared capture, default input, internal 16 kHz conversion | MSVC compilation, Windows job/handle behavior, real device/denial/disconnect/cancel |
| Ubuntu 24.04 x64 | Miniaudio PulseAudio backend; local user server, including PipeWire Pulse compatibility | Native GCC/libpulse compilation, distro dependency inventory, actual desktop session/device/denial/disconnect/cancel |

Windows miniaudio initializes COM using dynamically loaded system libraries. Its loader is restricted to a fixed named set in System32; arbitrary DLL names and current-directory lookup are excluded. Linux uses an explicit local Unix socket under /run/user/<uid>/pulse/native, checks its owner and the runtime directory, disables autospawn, and does not compile ALSA/plugin backends. An absent server fails with DEVICE_UNAVAILABLE. No remote Pulse server or inherited backend configuration is accepted from the request. This is a narrow capture interface, not a general OS sandbox.

Optional event/result fields report backend, permissionState, deviceState and signalObserved. A recognized access-denied result reports denied; ambiguous native failures report unknown. Opening the native stream reports access_opened rather than claiming that a particular OS permission dialog was shown. Silence is retained as valid PCM but does not establish that permission or device signal worked. No-device and denied states never fall back to synthesized audio.

## Build and bundle contract

Inspect the recipe without building:

    python3 scripts/build-microphone.py --target windows-amd64 --output <output> --plan
    python3 scripts/build-microphone.py --target linux-amd64 --output <output> --plan

Build on the matching target host with the verified native cache:

    python3 scripts/build-microphone.py --target windows-amd64 --cache <native-cache> --output <output>
    python3 scripts/build-microphone.py --target linux-amd64 --cache <native-cache> --output <output>

Windows requires an x64 MSVC developer prompt and an identified Windows SDK. Ubuntu requires 24.04 x64, GCC/G++, libpulse-dev and libpulse0. The selected miniaudio source is pinned; exact target compiler/SDK/package versions are recorded by the builder, but a reproducible target toolchain image has not yet been frozen. The builder does not download dependencies. MIT-0 miniaudio and nlohmann notices are retained. The resulting logical microphone-worker filename gains .exe on Windows, and microphone-worker.json binds its digest, architecture, production runtime and source identities. build-native-portable.py includes this helper in manifest v2 and records capability record. It rejects fixture sidecars. Older bundles that lack capture continue to report unavailable.

The separate --fixture-only build compiles MA_NO_DEVICE_IO, has runtime suffix -fixture, exposes only record_synthetic, and writes microphone-fixture.json with testOnly=true and deviceBackendCompiled=false. It cannot produce a production bundle. NewSupervisor rejects this runtime even if someone inserts it into a production manifest. The production binary does not compile the waveform branch or self-test. Synthetic capture is not a product setting.

On the current Mac, fixture verification used:

    python3 scripts/build-microphone.py --fixture-only --cache <native-cache> --output <fixture-output>
    TROVE_MICROPHONE_FIXTURE=<fixture-output> TROVE_NATIVE_INTEGRATION=1 go test -race ./internal/native -run 'Test(PortableMicrophone|MicrophoneProduction|RealMicrophone|LegacyMacMicrophone)' -count=1 -v

The legacy synthetic regression additionally supplies TROVE_MICROPHONE_LEGACY_BUNDLE. To test the old control/parent-exit route independently, omit the fixture variable and set TROVE_NATIVE_BUNDLE to the existing Mac bundle. The device-free fixture itself is never an acceptable candidate helper.

## Required target qualification

On each target, first compile the production worker and inspect source/SDK/dependency identities. Exercise an explicitly authorized microphone, permission denial, absent input, silent input, default-device loss/change, long recording, output stalls/ring overflow, cancellation while opening/recording, app termination, durable retained bytes and offline operation. Verify actual output duration/rate/channel/sample bounds and the source asset after app reopen. Windows must additionally pass handle/job/ACL cleanup; Ubuntu must pass the selected PulseAudio/PipeWire desktop session and packaging dependencies. Repeat against the exact packaged release candidate. The Mac fixture and Go cross-compilation evidence cannot close any of those target device gates.
