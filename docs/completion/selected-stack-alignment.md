# Selected technical components

GOV-005 now has source implementation for the selected components identified by the completion audit. This is a component-alignment claim. Each product behavior, platform, model, device and distribution requirement still needs its own acceptance evidence; no release gate is closed by this table.

| Selected contract | Current implementation | Evidence and remaining qualification |
| --- | --- | --- |
| sqlite-vec retrieval | Statically registered sqlite-vec v0.1.9; `vec0` cosine KNN, scoped/current metadata and model-generation compatibility | [Retrieval](retrieval.md): deletion, rollback, reopen and compaction coverage. Native target and representative relevance/performance qualification remain separate. |
| SQLite online backup | Pinned read snapshot with `SQLiteConn.Backup`, incremental 128-page steps, cancellation and pinned asset/recording descriptors | [Recovery](recovery.md), [pre-migration snapshots](pre-migration-snapshots.md). Independent installation restoration, physical interruption and platform filesystem qualification remain open. |
| Official MCP SDK | `github.com/modelcontextprotocol/go-sdk` v1.8.0; stdio and explicitly enabled authenticated remote transport through the owning kernel | [SDK](mcp-sdk.md), [remote MCP](remote-mcp.md). Real accounts/hosted interoperability remain unqualified. |
| PDF.js viewer | Bundled pdfjs-dist 6.3.289 with local worker/font/CMap/WASM/ICC assets, bounded render controls and disabled XFA | `frontend/src/editor/PDFBlock.tsx`, frontend tests and candidate checks. Rich PDF export uses a separate native print adapter; it is not presented as viewer evidence. |
| Shiki highlighting | Shiki 4.5.0, local core/JavaScript regex engine and bundled languages/themes | `frontend/src/editor/codePresentation.ts`; no runtime CDN dependency. Native editing and candidate visual checks remain separate. |
| Native capture | Pinned miniaudio 0.11.25. New Mac builds select CoreAudio after explicit OS consent; Windows uses WASAPI and Linux the owned local PulseAudio/PipeWire socket | [macOS alignment](macos-miniaudio.md), [portable capture](microphone-portability.md). Legacy Swift bundles stay readable. No actual microphone/TCC or target-native qualification is claimed. |
| Declared local audio decoding | Pinned restricted FFmpeg 9.0.2, descriptor-based input and bounded PCM normalization | [Decoding](audio-decoding.md): actual Mac WAV/MP3/AAC/M4A fixtures and cancellation. Target-native runtime and public redistribution qualification remain open. |

Pins, hashes and notices live with their respective source locks and evidence. Updating a component or native binary requires affected checks and a new applicable candidate; historical passes do not qualify changed artifacts. No silent substitute or inferred deviation approval is used to close this source-alignment item.
