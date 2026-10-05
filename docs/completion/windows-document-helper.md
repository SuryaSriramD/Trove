# Windows document helper implementation

This package implements the Windows C++ half of the portable document worker. It does **not** establish a Windows build, WebView runtime result, PDF visual result, or release. The shared Go launch/staging layer, pinned PDF.js pages, Linux implementation, and asset inventory packaging are owned by the portable-document workstream and integrate separately.

## Contracts and authority

The interface is `document-worker.exe render|extract <absolute-private-document-directory>`. The helper first reads exactly one start-barrier byte (`0x01`) from stdin. That byte carries no document content: the parent releases it only after assigning the helper to a Job Object that permits the WebView child processes. Parent cancellation closes/kills/reaps that job and removes the private staged parent. The helper independently enforces a 110-second deadline, including startup, status polling and stdout writes. The corresponding target cancellation tests remain open.

The parent supplies an owned, protected-DACL staging parent, a `document` child and an empty sibling `webview-profile`. The helper checks the current owner, protected DACL and permitted principals; opens and holds directory components without delete sharing; rejects reparses; and opens staged resources without write/delete sharing. Files must be regular disk files with one hard link. It never serves the profile or resource inventory.

`resource-map.json` (at most 1 MiB / 4,096 resources) contains schema version 1 and exact relative-name/size/SHA256 entries. Only the agreed entry pages, verified application/PDF.js assets, render snapshot, managed UUID images, or extraction input are permitted. Resource bytes are rehashed when served. URL handling accepts only GET at the exact `https://trove-document.invalid/` origin, with canonical inventory paths and no query, fragment, credentials, port, escapes, alternate data stream, traversal or arbitrary file lookup.

The platform-neutral `document-policy.hpp` validates map, resource budgets, duplicate keys, JSON depth, case collisions, URI access and status. It is reusable by Linux. The OS-specific implementation checks handles, ACLs, SHA256 and browser operations.

## WebView behavior

The helper uses official COM interfaces through Microsoft SDK headers and WRL, retaining inherited vtable layouts. It statically links the WebView2 loader. It requires `_22` request filtering for **all** request-source kinds, `_7` printing for render mode, and an in-private controller. Older/missing capabilities fail closed. Navigation is restricted to the expected one-shot entry transitions. Frames, new windows, permissions and downloads are denied; host objects, app messaging, developer tools, default dialogs, password storage, autofill and accelerator keys are disabled. Every intercepted request receives a deny response before allowed content is read.

Only two fixed status expressions are evaluated; returned JSON is parsed as data. The response CSP permits the pinned local script/style/font/image/worker paths needed by PDF.js and the print renderer, while denying frames, forms, objects, remote connections and imported inline scripts. Content is never mapped to a whole native directory. These are WebView content restrictions and a process-lifecycle job, not an arbitrary native-code network sandbox. Actual offline/disconnected behavior and WebView engine background activity require target verification.

A private environment disables OS-account single sign-on, extensions and automatic crash-data submission, and rejects all ambient `WEBVIEW2_*` overrides. Applicable WebView2 policy values are rejected in HKLM/HKCU (32/64-bit views) by querying only their existence; the environment's actual user-data folder must then equal the locked private profile before controller creation. Only the installed stable runtime channel is allowed. [Microsoft environment/registry override contract](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/webview2-idl). The crash setting is `IsCustomCrashReportingEnabled=TRUE`, following the explicit [Windows API contract](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2environmentoptions3) and [privacy documentation](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/data-privacy). No diagnostic/reporting endpoint is called by this implementation or its checks.

Render mode waits for `trovePrintStatus`, retains bounded warnings, and prints A4 with backgrounds and existing margins. It reserves `result.pdf` using `CREATE_NEW` and a handle that prevents replacement, then freezes the completed regular file with read-only sharing, verifies `%PDF-`, bounds it to 256 MiB and freezes its digest. Only the subsequent validation phase can fetch that generated file. Pinned PDF.js must report 1–2,000 physical pages. The helper writes a bounded `result.json` with runtime identity and streams the same verified PDF bytes. The reserved-file/WebView overwrite behavior remains a mandatory real-Windows test; failure has no unsafe fallback.

Extract mode serves at most 128 MiB of PDF input and accepts at most 10,000 pages and 16 MiB of decoded UTF-8 text, preserving form-feed page separators. JSON bridge data has a separate 96 MiB + 64 KiB ceiling for escaping overhead. Count/print status and reports are at most 64 KiB. Missing, malformed, password-protected, over-limit or failed preparation does not produce a successful extraction.

## Pinned build inputs

The official [Microsoft.Web.WebView2 1.0.3537.50 NuGet package](https://www.nuget.org/packages/Microsoft.Web.WebView2/1.0.3537.50), exact header/static-loader/license members, and the existing llama-vendored nlohmann 3.12.0 header/license are hash pinned in `native/document-windows.lock.json`. The SDK was downloaded from its official public NuGet endpoint for inspection: 8,957,746 bytes; SHA256 `5ea526bbd728adda0da4d31219267e96460494a427e4894c4e09d9f320f4b9aa`. No WebView runtime was downloaded or installed.

The SDK's compatible-runtime target is explicitly 141.0.3537.50. This is a **document-helper prerequisite**, separate from Wails' older shell minimum. The actual WebView version is recorded in successful render reports. [Compatible-version API](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2environmentoptions), [all-source request filtering](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2_22), [PrintToPdf](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2_7).

Inspect without building:

```text
python scripts/build-document-windows.py --output <unused-output> --plan
```

On Windows 11 x64, use an x64 MSVC v143-or-newer developer prompt with the Windows SDK and WRL, verified native source cache and the exact official `.nupkg` placed in that cache:

```text
python scripts/build-document-windows.py --cache <verified-cache> --output <native-bundle>
```

The builder performs no network acquisition. It verifies all input hashes, compiles `/MT` with the static loader, checks PE x64 identity and system-only DLL imports, retains redistribution notices, and writes `document-worker-windows-build.json`. Compiler/Windows SDK versions and header hashes are recorded on the actual target. A tested Windows compiler image is not yet available; its identity must be frozen during qualification. The shared builder later writes the asset-binding `document-worker.json`. SDK source files, static libraries and weights are not committed.

## Verification boundary

The five recorded macOS-host checks compile and execute the platform-neutral C++ policy, validate SDK archive/member identities and duplicate rejection, exercise read-only plan output, and prove cross-host build refusal before output creation. The actual official package and four selected member hashes were independently checked. Independent review covers the Windows source; it is not a substitute for compilation.

Still required: MSVC/WRL compilation and import review, installed runtime selection, ACL/reparse/hard-link fixtures on NTFS, Job Object start/cancel/child cleanup, denial of remote/worker/file/navigation attempts in WebView, malformed/encrypted/large PDF cases, extracted text comparison, full visual multi-page export inspection, warning preservation, output overwrite/sharing behavior, deadlines and browser crashes, clean-host offline packaging, and platform release acceptance. None is inferred from the portable policy tests.
