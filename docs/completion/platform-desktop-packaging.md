# C7/C8 desktop build and installation sources

Windows 11 x64 and Ubuntu 24.04 x64 now have a target-native, offline desktop packaging path, target-aware developer setup, and launch routing. **Neither target has a qualified build or release.** The available execution host is macOS arm64. POSIX installer tests use disposable files and simulated platform commands; PowerShell was reviewed as source but was not executed.

## Platform contract

The pinned Wails v2.16.0 source supports a Windows named-mutex/message-window single-instance path and a Linux session-D-Bus path. Trove supplies the fixed `local.trove.desktop` identity. First and second launches accept zero arguments or exactly one URL passing the existing `validateDeepLink` parser; arbitrary paths, flags, mixed arguments, and working-directory payloads are rejected. Link delivery preserves normal workspace selection and does not open another workspace. macOS keeps its existing URL event path. Wails can fall back to an additional process if OS instance messaging fails; canonical workspace locks remain authoritative. Actual OS delivery remains unqualified.

Windows packages use Wails' `wv2runtime.error` tag, application/System32 DLL search paths, and an explicit offline WebView2 prerequisite. Builder/doctor/installer read the documented per-machine and per-user WebView2 `pv` registrations and require at least Wails' pinned minimum 94.0.992.31. The actual runtime is checked again by Wails at launch. No runtime installer or downloader runs automatically. The installed Evergreen version must be recorded for each release; its update policy is separate from Trove. Source: [Microsoft runtime distribution and detection](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution), [Wails Windows distribution strategies](https://wails.io/docs/guides/windows).

Linux uses `webkit2_41`. The reviewed publisher pin is WebKitGTK 2.52.6, Ubuntu package revision `2.52.6-0ubuntu0.24.04.1`, with matching WebKitGTK/JavaScriptCore development and runtime packages. The builder checks exact package and pkg-config values; the installer checks the runtime package. This pin is publisher metadata, not a tested host image. Security updates require reviewing the lock and rerunning qualification, not silently accepting a different engine. Source: [Ubuntu noble-updates WebKitGTK package](https://packages.ubuntu.com/noble-updates/libwebkit2gtk-4.1-dev).

Runtime dependencies also include GTK 3, libsoup 3, a user D-Bus session and Secret Service, xdg-utils/desktop-file-utils, and libpulse0 plus the owned local PulseAudio/PipeWire socket for microphone capture. The worker bundle separately records native dependencies and unavailable capabilities. The target bundle includes pinned Tesseract English OCR. Windows additionally includes the isolated WebView/PDF.js document helper source contract; Linux PDF extraction/export is unavailable pending enforceable whole-process containment, its helper is omitted, and packaging rejects bundles that advertise or contain it. The PDF viewer and HTML export remain available. Packaging rejects changed helper, model, policy-extension, or document-asset identities. System audio and screenshot capture remain outside this bundle; all actual Windows/Linux device, OCR, and PDF execution remains unqualified. The PDF helper additionally requires WebView2 141.0.3537.50 on Windows; Wails’ lower shell prerequisite does not establish PDF capability.

## Reproducible setup and offline inputs

`baseline/go-targets.lock.json` pins official Go 1.27.1 archives for Darwin arm64, Windows amd64 ZIP, and Linux amd64 tar. Existing Darwin archive identity is unchanged. [Official Go download metadata](https://go.dev/dl/?mode=json&include=all) supplied target SHA256/size identities. Windows/Linux archives have not been downloaded or executed in this work package.

`bootstrap.py --target` must match the real host before directories or downloads are created. It chooses the host archive, validates hashes, rejects unsafe ZIP members, and verifies the installed Go OS/architecture as well as version. `--fetch` remains the explicit development acquisition switch; normal setup is offline. Windows pnpm shims use a recognized Node entry point with structured arguments, without shell interpolation. Corepack network acquisition is disabled in offline environments.

`doctor.py --target ... --json` reports native-host mismatch honestly, checks platform-specific dependencies, and no longer requires Apple clang/macOS SDK on Windows/Linux. Native Windows workers require MSVC/Windows SDK plus Ninja and MSYS2 build tools; Wails CGO additionally requires x64 MinGW GCC. Linux requires the pinned Ubuntu environment and C++ tools. Existing Python, Node, pnpm, CMake, module, source archive, and model checks remain.

On the corresponding target host, after explicitly provisioning the recorded toolchains and cached dependencies:

```text
python scripts/bootstrap.py --target windows-amd64 --cache <cache> --seed-cache <verified-archive-cache>
python scripts/doctor.py --target windows-amd64 --cache <cache> --json
python scripts/build-native.py --target windows-amd64 --cache <cache> --output <new-native-bundle>
python scripts/build-desktop-portable.py --target windows-amd64 --cache <cache> --native-bundle <new-native-bundle> --release-notices <reviewed-notices> --version 0.1.0 --build-number 1 --output <new-desktop-bundle>
```

Use `linux-amd64` for the equivalent Ubuntu sequence. Fresh dependency acquisition is a separate `bootstrap.py --fetch` action; desktop/native builders do not download dependencies. `--plan` is read-only on any host; `--check` requires the actual target host. All plan/check outputs explicitly leave platform qualification false.

## Package and ownership boundaries

The portable layout is `bin/Trove[.exe]`, `bin/trove-mcp[.exe]`, `bin/trove-host[.exe]`, `bin/trove-format[.exe]`, and `Resources/{native,browser-clipper,web,release}`. The existing resource lookup accepts this layout. Model weights remain external; the package contains only the reviewed catalog.

The builder requires committed source, pinned offline Go/frontend inputs, a matching production native bundle and release notices. Native hashes, platform/architecture, runtime revisions, microphone sidecar identity and fixture exclusions are checked. It builds in an unused staging directory, runs the freshly built `trove-format` for authoritative storage compatibility, records Git/baseline/toolchain/dependency identities, and emits a full hash inventory before publication. Checksums detect changes; they do not establish publisher authenticity. Outputs remain unsigned and explicitly unqualified. `scripts/build-desktop.py` is unchanged.

Installers use fixed per-user app locations and ownership receipts, with no workspace chooser or workspace deletion:

- Windows: `%LOCALAPPDATA%\Programs\Trove`, a Start Menu shortcut, and the current-user `trove` URL protocol registration. Non-elevated Windows 11 x64 only. Existing app paths/registrations are preserved. Uninstall requires exact receipt, manifest, file inventory, registry ownership/command, and shortcut target/arguments. An owned partial registration can omit nodes not yet created. An interruption before its ownership value is written leaves an ambiguous registry key preserved for inspection; interrupted-install recovery still requires target evidence.
- Linux: `${XDG_DATA_HOME:-$HOME/.local/share}/Trove/app` plus `applications/local.trove.desktop.desktop`. Non-root Ubuntu 24.04 x64 only. The Desktop Entry registers `x-scheme-handler/trove` and the cache is updated; no existing default handler or mimeapps.ini setting is overwritten. Actual desktop handler selection requires target qualification.

Both paths reject links/reparse entries, altered package bytes and conflicting registrations. Uninstall refuses unexpected files/directories or changed ownership, and preserves workspaces, model caches, credentials and settings. These are per-user application installers; they are not signed platform update/rollback implementations. A new distribution should not be installed over an existing one until the independent upgrade/rollback gate is implemented and verified.

## Evidence and remaining gates

`evidence/completion/platform-desktop-packaging/manifest.json` binds the implementation commit, test logs and plan/doctor outputs. Validation includes affected macOS Go race tests, existing developer-tool tests, target manifest/path/hash/fixture tests, hostile ZIP inputs, and disposable simulated-OS installer acceptance tests. Initial path-validation failures are retained with their fixes. No account, private workspace, microphone, model inference, or network download was used by these checks.

REL-009 and the relevant release checks remain open: actual target C++/Go/frontend builds, PowerShell execution, clean installation, WebView startup, first/second-instance OS links, permissions/clipboard/filesystem, model-free and offline model paths, capture/OCR/provider containment, backup portability, signing/authenticity, verified updates/compatible rollback, failure recovery, uninstall on real machines, performance, and the applicable P0/P1/P2 matrix. A passing plan, compiler fixture, source review, or macOS test cannot close them.
