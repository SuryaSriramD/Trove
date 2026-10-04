# Official MCP transport

The paired bridge now uses the official `github.com/modelcontextprotocol/go-sdk` v1.8.0, pinned by Go module checksums and upstream commit `3f3b699b2b67e1ed033a63d6651671dab53c2d32`. The SDK owns protocol negotiation, tool dispatch, session handling, cancellation and current-protocol request lifecycle. Every paired connection owns separate SDK servers/transports. Authentication and the live workspace grant are checked on every HTTP request. No browser origin is accepted, and the SDK's localhost/DNS-rebinding protection remains enabled.

The endpoint supports legacy stateful HTTP (including the existing 2025-03-26 relay) and 2026-07-28 stateless HTTP. Stateful sessions expire after five idle minutes. Request bodies are bounded to 1 MiB, tool concurrency to 32, and new sessions are refused when the connection already has 32 sessions. Disconnect cancels calls, closes SDK sessions and revokes the canonical grant. MCP session deletion only ends that protocol session; the person can separately revoke the paired connection in Trove.

Tools remain the explicit selected capability list. The SDK never receives a database handle or filesystem tool. Calls use the one owning kernel and shared reviewed proposals. Search defaults use the granted scope and a bounded limit; record queries expose cursor/limit configuration. Local reads remain available when no connection is enabled.

## Numeric request-ID compatibility

The pinned SDK decodes numeric request IDs via `float64`, so adjacent integers above 2^53 can collide. Trove's direct HTTP boundary rejects non-integer or out-of-range numeric IDs and instructs clients to use strings. The stdio relay losslessly encodes request/cancellation IDs as strings, and restores the exact caller ID on each response. It does not rewrite tool arguments. This compatibility adapter leaves protocol handling with the official SDK and preserves existing large-ID cancellation behavior.

The relay also retains the SDK-issued session and negotiated version across HTTP requests. It continues reading stdin while calls execute, reserves cancellation capacity, bounds responses and refuses redirects so credentials stay at the paired endpoint.

## Evidence and remaining scope

`provider_sdk_test.go` uses the actual official SDK client against the real SQLite-backed server for discovery, reviewed creation, duplicate retries, cancellation, revocation, session isolation and read defaults. `TestPhase5V10PairedRelayProtocolConsentAndCancellation` launches the compiled stdio relay over OS pipes and tests scoped reads, denied tools, stale proposals, approvals, adjacent large numeric IDs and cancellation. Affected provider/relay tests pass under the race detector; see `evidence/completion/c5-sdk-race.txt`.

This closes the official-SDK implementation component of GOV-005. Managed Claude/Codex processes, remote HTTPS authentication and real-account handoffs remain separate C5 work. Local SDK-client/compiled-relay evidence is not a substitute for those account and deployed-connectivity checks. Final release provenance must include the SDK's full combined Apache-2.0/MIT transition notice and dependency licenses; the existing release inventory collects Go module license files.

References verified on 2026-10-04: [official SDK](https://github.com/modelcontextprotocol/go-sdk/tree/v1.8.0), [protocol documentation](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md), [transport source](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/mcp/streamable.go), and [request-ID decoding](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/internal/jsonrpc2/messages.go).
