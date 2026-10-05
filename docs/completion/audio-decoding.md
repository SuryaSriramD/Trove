# Local meeting audio decoding (CORE-005, GOV-005, C7/C8)

New native bundles use a pinned, restricted, statically linked FFmpeg 9.0.2 decoder on macOS arm64, Windows x64 and Ubuntu x64. This implements the selected decoder in TRD §19 and §30.3. The macOS reference helper passed all four committed declared-format fixtures before the build default changed. Existing verified CoreAudio sidecars remain readable on macOS arm64 for compatibility with earlier candidates; no new build silently selects CoreAudio.

The imported recording remains canonical, retaining its bytes, filename and file identity. Normalization produces disposable 16 kHz mono float32 PCM. Failed, unsupported or cancelled normalization removes derived staging and retains the original for retry or export. The existing bounded 16 kHz mono PCM WAV/raw-float fast path remains available.

## Supported boundary

- WAV containing unsigned 8-bit PCM, signed little-endian 16/24/32-bit PCM or little-endian float32/64 PCM; MP3; AAC/ADTS; M4A containing supported AAC/PCM audio.
- Mono/stereo, 8–192 kHz; at most 512 MiB original bytes and two hours normalized output.
- One audio stream. Encrypted media, other codecs/profiles, video tracks, stream configuration changes and damaged input produce recoverable errors where detected. This is not unrestricted FFmpeg format support.
- Input comes from read-only descriptor slot 3 and derived output from slot 4. The bounded versioned JSON request contains only schemaVersion. Paths, URLs, codec options and arbitrary FFmpeg arguments are never accepted.
- Custom AVIO callbacks enforce input bounds and a deadline. External track opening is denied. All protocols, network support, plugin autodetection, encoders, muxers, filters, devices and hardware acceleration are disabled in the compiled configuration.
- The allowlist is recorded in native/audio-decoder.lock.json. Build verification fails on an unexpected enabled decoder, demuxer, parser or protocol.
- Decoding uses one codec thread, bounded frame/packet allocations and a 64 MiB individual FFmpeg allocation ceiling. The supervisor enforces a three-minute wall limit, 512 MiB RSS budget, output-size budget, and cancellation/reaping.
- The request is written only after process containment succeeds. Windows uses the inherited handle map and kill-on-close Job; Unix uses the existing process group.
- Both worker and supervisor check finite, bounded PCM and output shape. The helper hash, runtime pin, OS and architecture must match the sidecar.

Input snapshots and caller-owned derived PCM use private staging directories. Windows creates the owner-only protected DACL in the initial directory-creation syscall; files inherit its single-user grant. Unix creates mode-0700 directories and mode-0600 files. Success cleanup, failure and cancellation remove the derived directory. Abrupt machine/process loss still needs platform crash-cleanup qualification.

## Build and distribution

Acquire the pinned archive explicitly; builds are offline:

    python3 scripts/bootstrap.py --fetch --components native --cache <cache>
    python3 scripts/build-audio-decoder.py --cache <cache> --output <native-bundle>

The selected source archive SHA256 is 84960df915059e8754fef2cd7c9afeb614062b1b5458ec471eecee619ee04e98. Its detached release signature was verified against the fingerprint published by FFmpeg: FCF986EA15E6E293A5644F10B4322F04D67658D8. The archive, version, signature URL, fingerprint and restricted codec set are committed in the lock.

Inspect target requirements without building:

    python3 scripts/build-audio-decoder.py --target windows-amd64 --plan
    python3 scripts/build-audio-decoder.py --target linux-amd64 --plan

Actual builds require the matching OS/architecture. Windows requires x64 MSVC, the Windows SDK, MSYS2 bash and GNU make; assembly is disabled, so NASM is not required by this recipe. Ubuntu requires 24.04 x64, GCC/G++, bash and GNU make. The complete native bundle additionally requires the prerequisites in platform-native-workers.md. Native target toolchain/SDK versions remain to be frozen and executed on those hosts.

The existing Mac native/desktop builders and Windows/Linux worker-bundle builder include the helper automatically. No end-user FFmpeg installation is used. Only system runtime libraries may remain dynamically linked, and build output inventories them.

The restricted configuration is LGPL v2.1 or later: GPL, version-3 and nonfree additions are disabled. Every bundle includes audio-relink with the exact upstream source archive, notices, compiled application object, static libraries, configuration, source/header copies, relocation-independent relink script and build provenance. The relink recipe was actually exercised on the development Mac. Keep this material with distributions. Final license/codec-patent review and terms permitting the required debugging of modifications remain public-distribution gates; this inventory does not constitute legal approval.

Primary sources: [FFmpeg release verification](https://ffmpeg.org/download.html), [custom descriptor-backed AVIO example](https://ffmpeg.org/doxygen/trunk/avio_read_callback_8c-example.html), [official platform build instructions](https://ffmpeg.org/platform.html), [FFmpeg licensing](https://ffmpeg.org/legal.html). The exact selected release's configure script, LICENSE.md and doc/platform.texi were also inspected.

## Evidence and remaining gates

Candidate identities and retained logs are in evidence/completion/c7-c8-audio-evidence.json. The evidence binds the helper digest, source hashes, source archive, fixture manifest, build configuration and platform-specific results.

Passed on the development Mac:

- Actual restricted helper decodes all hash-pinned synthetic WAV/MP3/AAC/M4A fixtures to finite, non-silent mono PCM with the expected duration and 48 kHz stereo source metadata; original SHA256 values remain unchanged.
- Truncated known container, unsupported input/channel profile, NaN/infinity/over-range source PCM and malformed/extended/oversized requests fail safely.
- Cancellation after observing real derived output stops and reaps the helper promptly. Supervisor identity, input/output bounds, memory-monitor portability and descriptor transport regressions pass.
- Private normalized/PCM output lifecycle, invalid-float cleanup and canonical failed-transcription retry retain original bytes and identity.
- Static configuration policy and host-selection tests pass. The included Mac relink kit rebuilds the helper from its provided object/libraries.

Windows and Linux Go native test executables cross-compile on Mac. They were not executed. Windows C++/MSVC and Linux C++/GCC decoder builds, actual Job/DACL/handle behavior, full target fixture runs, runtime dependencies, packaged UI, clean installs and cross-platform backup tests remain unqualified. Speech accuracy, representative long meetings, real microphones, public signing and independent clean-Mac qualification also remain separate release gates.

Run the focused checks on each actual target with its native helper:

    TROVE_NATIVE_INTEGRATION=1 TROVE_NATIVE_BUNDLE=<native-bundle> go test -race -count=1 -run 'Test(AudioDecoder|RealAudioDecoder|PrivateStaging|NativeWorkerDescriptor)' ./internal/native
    go test -race -count=1 -tags 'fts5 sqlite_omit_load_extension' -run 'Test(NormalizedAudio|RestrictedAudio|AudioRejects)' ./internal/workers

The fixtures are generated sine tones, not personal recordings. Fixture regeneration uses scripts/generate-audio-fixtures.py and records its encoder/version/hash recipe. This does not substitute for representative speech data.
