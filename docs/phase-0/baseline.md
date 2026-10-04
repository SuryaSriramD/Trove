# Baseline decision and Apple Silicon qualification

The implementation baseline is a Go workspace kernel, Wails **2**, React/TypeScript/Vite, Tiptap/ProseMirror, bundled SQLite with FTS5, pinned sqlite-vec CGO integration, and bundled llama.cpp and whisper.cpp workers. Domain services own writes and remain independent of Wails. macOS Apple Silicon is the first qualification target; a portable schema does not establish Windows/Linux readiness.

The machine-readable authority is [`baseline.lock.json`](../../baseline/baseline.lock.json). The isolated [`baseline/go`](../../baseline/go/go.mod) module and [`baseline/frontend`](../../baseline/frontend/package.json) package are executable integration probes, not production application services. The root Go module contains generated transport contracts and the pinned JSON Schema validator; the isolated baseline module keeps desktop/native build dependencies separate.

| Component | Selected pin | Evidence mechanism |
| --- | --- | --- |
| Go | 1.27.1, darwin/arm64 | Official archive SHA-256; compiled CGO tests and Wails shell |
| Wails | v2.16.0 | Full module commit and go.sum; native arm64 executable link |
| React / React DOM | 19.3.0 | Exact package versions and pnpm integrity lock; production bundle |
| TypeScript / Vite | 7.0.2 / 8.3.1 | Strict TS type check and production build |
| Tiptap core/PM/React/starter-kit | 3.31.3 | Same-version packages; editor component included in bundle |
| mattn/go-sqlite3 | v1.14.52; SQLite 3.53.4 | `fts5 sqlite_omit_load_extension`; no system libsqlite3 linkage |
| sqlite-vec official Go binding | v0.1.6; embedded runtime v0.1.6 | Full module and runtime commits; actual vector query/delete/reinsert |
| llama.cpp | v0.5.0; `d2e54583c7452353eb35d40431281f6ee984332f` | Archive SHA-256; arm64 static CPU/Metal build and process probe |
| whisper.cpp | v1.9.4; `927cfce34f31707e17f2bff35c349632fb9e2c3a` | Archive SHA-256; arm64 static CPU/Metal build and process probe |

The recorded development toolchain is Node 25.5.0, pnpm 11.19.0, CMake 4.3.2, Apple clang 21.0.0 and the macOS 27 SDK. These are build inputs, not end-user requirements. This run establishes compatibility on the recorded host only; the minimum supported macOS version is a release qualification decision.

## Reproduce the Phase 0 gate

```sh
python3 scripts/validate-baseline.py --fetch
python3 scripts/validate-baseline.py
python3 scripts/validate-baseline.py --check-evidence
```

`--fetch` permits pinned dependency/tool downloads. The default run is offline and uses the populated temporary cache (`--cache-dir` overrides it). No global Go installation is required. Go modules are checked by `go.sum`; pnpm installs with a frozen integrity lock and install scripts disabled; native and Go archives must match SHA-256 before extraction. No tag or branch is resolved during validation. The first run needs the recorded Node/pnpm/CMake/Xcode toolchain available.

The validator writes [`evidence/baseline.json`](../../evidence/baseline.json), source hashes and individual command logs. `--check-evidence` checks the recorded pass against the current probe input hashes; it is explicitly not a fresh host run. Exit zero from the default validator means all Phase 0 checks ran successfully:

1. Native Darwin arm64 host and exact selected Go/Node/pnpm/CMake versions.
2. One compiled CGO test binary uses bundled SQLite, FTS5 and sqlite-vec together. It verifies WAL, foreign keys, full-text search, nearest-neighbor search, delete/reinsert and SQLite online backup.
3. Wails 2 links an arm64 executable against the native frameworks. The direct Go probe supplies `-framework UniformTypeIdentifiers`, matching the Wails CLI's macOS build logic.
4. React/TypeScript/Vite/Tiptap compile to production static assets.
5. Both pinned native libraries build statically with CPU and embedded Metal support; separate arm64 processes link and expose those capabilities. `otool` confirms that no Homebrew or unbundled native dylib is required.

The native probes do not load a model, execute a GPU kernel or implement Trove's worker protocol. Metal capability in this evidence means a compiled and linked backend with embedded shaders, not measured inference performance. The Wails probe is linked without opening a window; launch, permissions and UI behavior belong to native product acceptance.

## Storage and sqlite-vec decision

Use the amalgamation shipped by mattn/go-sqlite3; the `libsqlite3` build tag is forbidden. Register `sqlite_vec.Auto()` before opening connections. Use one serialized writer; every bounded reader connection applies foreign keys, WAL and a 5-second busy timeout. Disable user extension loading. Online backup must operate through the same driver/runtime combination.

The official stable Go binding is still v0.1.6, while upstream sqlite-vec v0.1.9 includes a later DELETE fix. These are different release lines; this lock does not pretend that the old binding embeds v0.1.9. The stable official binding is pinned for the Phase 0 integration spike. Before a product release, the storage owner must qualify an upgrade to the fixed runtime or document a tested mitigation with delete/chunk-compaction stress fixtures. A simple delete/reinsert test is insufficient evidence for that newer fix. The vector index is always disposable and rebuildable; canonical pages/assets never depend on it. Retrieval must recheck source scope, deletion and revision before returning citations, including after an index deletion fails.

## Product release gates remain separate

`python3 scripts/validate-baseline.py --release` also enforces release qualification and currently exits nonzero. This is intentional: Phase 0 supplies the shared baseline, not a distributable product. The evidence lists the remaining gates as **unverified**, rather than marking them implemented:

- Signed/notarized Wails installer and signed bundled helpers on a clean Mac without Go, Node, Python, Ollama or a database server.
- Independently licensed and SHA-256-pinned chat, embedding and speech model packs; real CPU/Metal inference, embedding dimensions, timestamped transcription and offline acceptance.
- Native window lifecycle, permissions, microphone/system capture, clipboard, deep links, restart and restore.
- Worker protocol, cancellation/crash containment, memory/thermal limits and supported minimum OS.
- sqlite-vec upgrade/delete-stress qualification, followed by independent Windows and Linux packaging/capture/offline qualification.

Workers must ship inside the application distribution. Models remain separately identified: LLM/embedding GGUF packs and whisper speech model formats are not interchangeable. Every release manifest records worker executable hashes, exact source commits, native build flags, model formats/hashes/licenses and third-party notices. Updating a pin requires explicit review and rerunning the relevant checks; rebuilding a derived vector index must never migrate canonical notes.

## Upstream provenance

- [Wails upstream stable-v2 statement](https://github.com/wailsapp/wails/blob/60b86280e316f71c6b6de32ece9023743eeccb61/docs/mpress/content/blog/wails-v3-beta.md) and [pinned macOS linker logic](https://github.com/wailsapp/wails/blob/457bf210d5eb976ff3726c8629f7725575163307/v2/pkg/commands/build/base.go).
- [Go official downloads and archive checksums](https://go.dev/dl/), [mattn/go-sqlite3 source](https://github.com/mattn/go-sqlite3/tree/b0be46fa28d17ee0b65c79774ac0dad84b6db068).
- [Official sqlite-vec CGO integration](https://alexgarcia.xyz/sqlite-vec/go.html), [pinned binding](https://github.com/asg017/sqlite-vec-go-bindings/tree/529a0138d40e50087db7043e361601f91d711e81), [v0.1.9 upstream release](https://github.com/asg017/sqlite-vec/releases/tag/v0.1.9).
- [llama.cpp pinned release](https://github.com/ggml-org/llama.cpp/releases/tag/v0.5.0), [whisper.cpp pinned release](https://github.com/ggml-org/whisper.cpp/releases/tag/v1.9.4).
- [React registry metadata](https://registry.npmjs.org/react/19.3.0), [TypeScript registry metadata](https://registry.npmjs.org/typescript/7.0.2), [Vite registry metadata](https://registry.npmjs.org/vite/8.3.1), [Tiptap registry metadata](https://registry.npmjs.org/@tiptap/react/3.31.3). Transitive pins and integrity digests are in the committed pnpm lockfile.

The baseline follows PRD §§20.1–20.3 and TRD §§4.1, 6, 19 and 31. The upstream verification used the Firecrawl developer index for primary integration documentation and official Go/npm/GitHub metadata for immutable versions, then actual local builds for compatibility evidence.
