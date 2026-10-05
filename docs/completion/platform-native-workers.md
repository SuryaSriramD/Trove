# C7/C8 native worker portability

This package implements the native-worker transport and build path for the selected Windows 11 x64 and Ubuntu 24.04 x64 targets. It does **not** close REL-009, establish a Windows/Linux release, or qualify model quality. The available execution host is a Mac; Windows/Linux results below are cross-compilation only.

## Interface and source basis

Protocol version 1, request identities, leases, result hashes, cancellation frames, and descriptor slot ordering are retained:

| Slot | Authority |
|---|---|
| 0 / 1 / 2 | Request pipe / framed events / bounded diagnostic log |
| 3 / 4 | Independent cancellation pipe / result staging file |
| 5 | Leased, verified model |
| 6 onward | Approved private input snapshots; image projector occupies slot 8 |

Go documents that Cmd.ExtraFiles is unsupported on Windows. The Windows path uses SysProcAttr.AdditionalInheritedHandles, an explicit whitelist of duplicated inheritable handles. A supervisor-generated environment value maps handles to logical slots in the C++ bootstrap. The ordinary parent environment is not forwarded; duplicate handle-map injection is rejected. The worker clears inheritance and uses binary I/O. [Go os/exec](https://pkg.go.dev/os/exec#Cmd), [Microsoft inherited-handle list](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute), [Microsoft CRT adapter](https://learn.microsoft.com/en-us/cpp/c-runtime-library/reference/open-osfhandle?view=msvc-170).

Windows creates a job with kill-on-close, one active process, and the request's committed-memory cap. The verified helper emits hello and waits; the parent assigns the process to the job **before sending a request**. Failed assignment kills and waits for the child. Cooperative cancellation uses the independent pipe; escalation terminates the job. Monitoring stops before job/process handles close. This is process lifecycle containment, not a general filesystem/network sandbox. [Microsoft Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), [job limits](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-setinformationjobobject).

macOS and Linux retain ExtraFiles and process groups. RAM admission uses sysctl, Linux sysinfo, or Windows GlobalMemoryStatusEx. RSS/storage inspection use platform primitives. POSIX retains directory fsync. Windows flushes receipt files before replacement but does not claim POSIX directory durability; power-loss qualification remains open.

Source pins remain unchanged:

- llama.cpp / mtmd: d2e54583c7452353eb35d40431281f6ee984332f.
- whisper.cpp: 927cfce34f31707e17f2bff35c349632fb9e2c3a.
- Archive hashes in baseline/baseline.lock.json remain authoritative.

The pinned llama API accepts FILE*. Whisper now uses its pinned model-loader callback to read the leased descriptor, including cancellation during loading. mtmd only accepts filenames. POSIX resolves the projector descriptor through /dev/fd/8 or /proc/self/fd/8. Windows stages at most 512 MiB from the approved handle in 64 KiB chunks, rehashes against the request manifest, and flushes in an owner-only protected-DACL directory. CREATE_NEW refuses existing files/reparse points; an open handle denies modification/deletion during loading. RAII removes the projector on ordinary worker exit; the supervisor removes staging after a killed/crashed child is reaped. Abrupt termination of the owning app can leave a private temporary directory; restart cleanup and crash-recovery evidence remain open.

The worker uses platform SHA-256: CommonCrypto, BCrypt, or OpenSSL EVP. No custom cryptography is introduced. Sources: [pinned llama build guide](https://github.com/ggml-org/llama.cpp/blob/d2e54583c7452353eb35d40431281f6ee984332f/docs/build.md), [pinned whisper loader API](https://github.com/ggml-org/whisper.cpp/blob/927cfce34f31707e17f2bff35c349632fb9e2c3a/include/whisper.h), [pinned mtmd API](https://github.com/ggml-org/llama.cpp/blob/d2e54583c7452353eb35d40431281f6ee984332f/tools/mtmd/mtmd.h).

## Manifests and unavailable capabilities

Manifest v2 binds platform and architecture. Helper names remain logical; Windows resolves the corresponding .exe filename. Digests and architecture must match; duplicate helpers and mismatched targets fail validation. Legacy v1 remains Mac arm64 only.

The normative hello schema adds amd64 alongside arm64; Go/TypeScript consumers are regenerated together. The catalog marks model data portable, while executable architecture stays strict. Model/projector IDs, revisions, hashes and sizes are unchanged. Old arm64 receipts still work on Mac; other hosts must use correctly declared portable packs.

Windows/Linux bundles omit Swift helpers. The portable microphone package adds a pinned miniaudio worker with native target build recipes; device execution remains unqualified. See [microphone-portability.md](microphone-portability.md). The portable OCR package adds pinned English Tesseract/Leptonica helpers; native target execution remains unqualified (see [portable-ocr.md](portable-ocr.md)). Native document/PDF helpers remain unavailable. The follow-on audio package adds a pinned restricted static FFmpeg decoder on all three targets; see [audio-decoding.md](audio-decoding.md). Its Mac format fixtures pass; Windows/Linux native compilation and execution remain unqualified. Missing or mismatched capture helpers and unavailable local devices fail closed.

## Build procedure

Inspect plans anywhere:

    python3 scripts/build-native.py --target windows-amd64 --plan
    python3 scripts/build-native.py --target linux-amd64 --plan

Actual builds require the target OS/architecture. They reject mismatched hosts and nonempty output directories, verify source archives/extracted files, and record compiler/dependency identities. They do not download inputs. Explicit acquisition uses Python 3.12+:

    python3 scripts/bootstrap.py --fetch --components native --cache <native-cache>

Native source archives are platform neutral. Target-specific official Go archive pins and bootstrap/doctor checks are now supplied by the [desktop packaging package](platform-desktop-packaging.md); actual Windows/Linux tool execution remains unqualified.

| Target | Required build environment | Runtime dependencies |
|---|---|---|
| Windows 11 x64 | Python 3.12+, CMake 4.3.2, Ninja, x64 MSVC v143 or newer developer prompt, Windows SDK, MSYS2 bash/GNU make, local NTFS cache | Windows system DLLs; static MSVC runtime requested and imports inspected |
| Ubuntu 24.04 x64 | Python 3.12, CMake 4.3.2, Ninja, GCC/G++ 13, libssl-dev, libpulse-dev, procps | glibc, libstdc++, libgcc, libcrypto.so.3, libpulse0, a local per-user PulseAudio/PipeWire socket and mounted /proc |
| macOS arm64 | Existing macOS 27 SDK / Apple clang / Swift setup | Existing Apple frameworks and signed helpers |

On the selected host:

    python3 scripts/build-native.py --target windows-amd64 --cache <native-cache> --output <empty-output>
    python3 scripts/build-native.py --target linux-amd64 --cache <native-cache> --output <empty-output>

Windows/Linux use CPU baseline flags: native host ISA detection, AVX/AVX2/BMI2/FMA/F16C/SSE4.2, GPU backends and OpenMP disabled. llama and whisper use separate CMake trees because their ggml targets differ. mtmd subprocess/video support is off. The existing Mac Metal/Swift path remains available, including --target darwin-arm64.

Outputs: three model workers, restricted audio decoder/sidecar/relink kit, production microphone worker/sidecar, manifest v2, catalog, dependency reports, notices, JFK fixture, build provenance. These are **worker bundles**, not installers. Actual compiler/SDK/Ubuntu package versions are recorded; an exact Windows compiler image and validated Ubuntu dependency snapshot are not frozen. These recipes do not establish reproducible target binaries or release qualification.

## Evidence and open gates

Evidence prefix: evidence/completion/c7-c8-*. Exact sources, outputs and hashes are in c7-c8-native-evidence.json.

| Check | Actual result |
|---|---|
| Mac native + contracts race suites | Passed |
| Binary descriptor bytes, clean environment, cooperative/forced cancellation and reap | Passed on Mac |
| Modified C++ workers against pinned cached libraries | Compiled/linked, ad-hoc signature verified on Mac |
| Real Mac generation, embedding, JFK transcription | Passed; synthetic microphone also ran, not actual microphone |
| Long-input tokenizer, incomplete-output rejection and cancellation | Passed |
| Vision active cancellation, leases and exact token bound | Passed; prior failed image-quality rubric remains unqualified |
| Windows amd64 Go native test executable | Cross-compiled on Mac; Windows tests not executed |
| Linux amd64 Go native test executable | Cross-compiled on Mac; Linux execution not performed |
| Native MSVC/Ubuntu C++ compilation and dependency inventories | Pending target hosts |
| Windows job membership, cleanup, ACL, handles, projector reparse/tamper scenarios | Implementation/tests prepared; target execution pending |
| Desktop/WebView, capture/OCR, target decoder execution, installers, signing, updates, backup portability | Open in owning platform packages |

TestRealQualification includes legacy Mac synthetic microphone capture. Windows/Linux production helpers reject synthetic requests; the separate fixture-only executable exercises the portable protocol without device code. Neither path substitutes for explicit real-device qualification. Portable descriptor/process tests can run independently.

Before closing C7/C8: execute platform tests; build target C++ workers; run real models and hostile-input/cancellation cases; qualify permissions, crash cleanup and power-loss recovery; supply install/update/rollback/uninstall and backup-portability evidence; freeze full platform candidates. Existing AI quality failures and research/account/device/signing prerequisites remain unchanged.
