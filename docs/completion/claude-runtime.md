# Managed Claude Code adapter and qualification

This package implements the Claude portion of EXT-001 against the unchanged,
user-installed official CLI. It does **not** qualify EXT-001 as complete. On the
current macOS host, Claude Code 2.1.283 passes contained discovery and signed-out
status but crashes during contained native startup. Managed login and execution
therefore remain disabled by an actual capability probe, including when called
directly through the adapter.

## Host integration

- `QualifyClaudeRuntime(ctx, Discover(ctx, "claude_code"))` returns a `Runtime`.
  Display `Available` and `Reason` in the capability panel. Discovery alone is
  insufficient. The qualification probe uses an empty temporary profile, no
  network and `auth login --help`; it does not sign in or invoke a model.
- `ClaudeAuthStatus(ctx, runtime, accountState)` invokes `auth status` inside the
  private account profile. It returns public status, account label, a stable
  account fingerprint and explicit billing mode. The host never opens, copies,
  parses or migrates a credential file. Unknown account identity, method or
  inference provider cannot start a managed run.
- `ClaudeAuthLoginCommand(ctx, runtime, accountState, billing)` returns a native
  command hook only after qualification, with an explicit deadline of at most
  ten minutes. The host must attach the vendor's terminal/PTY directly, cancel
  when it closes, and neither inspect nor persist terminal input/output. Login
  must be a distinct owner action. The [private profile interface](claude-profiles.md)
  now supplies an explicit native Terminal handoff. Actual Terminal/account login
  remains unqualified. Trove must not ask the user to
  paste an OAuth code or token into a Trove field.
- Login keeps the normal deny-inbound policy. Official documentation describes
  entering a browser-returned code directly into the vendor terminal when the
  local callback is unreachable. That fallback still requires an actual account
  qualification on a supported runtime. No browser callback listener exception
  is granted. `anthropic_api` selects the vendor's `--console` login; there is no
  automatic API fallback from `claude_subscription`.
- `ClaudeAuthLogout(ctx, runtime, accountState)` invokes the vendor logout command
  with a ten-second bound. It does not implement credential deletion itself.
- `RunClaude(ctx, ClaudeConfig, prompt, resume, emit)` uses shared `Event` and
  `ManagedCheckpoint` types. `Identity` uses provider `claude_code`, auth mode
  `vendor_owned` and adapter version `2`. Supply a private per-account
  `StateDirectory`, a distinct child `SessionDirectory` under `sessions`, exact
  model, scope hash, scoped loopback MCP endpoint/token/connection and selected
  tool manifest. Optional `ContextManifest` is valid JSON of at most 64 KiB,
  containing the previewed page ID/title/revision metadata. It is sent as
  untrusted data; actual authority remains in the scoped MCP service.

The per-account private HOME and `CLAUDE_CONFIG_DIR` are passed only to the
selected vendor process. Each conversation has a separate working directory.
The exact runtime bytes are snapshotted outside writable account state. MCP
credentials use a temporary owner-only file removed after the run, never process
arguments. Vendor credentials are neither inherited from ambient environment nor
copied from an existing installation. Credential persistence, including the
vendor's private-file fallback when macOS Keychain is unavailable, remains vendor
owned. The host must not grant Keychain or home access to make a failed flow pass.

## Execution boundary

The adapter uses `--restricted`, no native tools, an explicit MCP tool allowlist,
strict MCP configuration, no inherited settings, disabled hooks/memory, no Chrome
integration and denied native permission prompts. The outer macOS containment
remains authoritative. Only outbound networking needed by the selected vendor
and scoped MCP route is enabled during a run. Workspace files, shell, additional
executables, Keychain and unrelated user state remain denied.

The stream parser requires the reviewed model/session, exactly the connected
Trove MCP server, every selected tool and no other tool except the CLI's inert
`EndConversation` marker. Native permission requests, tool/model/session drift,
plugin loading, malformed frames and incomplete output fail closed. Partial text
and completed assistant messages are reconciled without duplicate text. A
successful vendor result and successful native process exit are both required
before the host receives completion. Frames are bounded to 1 MiB, total stream to
8 MiB, prompt to 64 KiB, execution to ten minutes and vendor turns to 32.
Cancellation kills the contained process group and closes a blocked stream.
Resume binds account, explicit billing, model, context/tool scope, adapter,
runtime hash and containment policy; mismatches require a fresh reviewed run.
Every actual MCP call still rechecks the host's scope/revocation and proposal
review boundary. This adapter cannot approve its own proposal.

## Evidence on 2026-10-05

`go test -race ./internal/providers -count=1` passes. Tests cover text streaming,
partial/complete deduplication, complete-result handling, malformed/oversized
frames, early EOF, denied native tools/permissions, omitted selected tools,
model/session/MCP/plugin drift, cancellation, resume identity mismatch, separate
billing identities, secret-free arguments and bounded context metadata.

With `TROVE_PROVIDER_INTEGRATION=1`, real native tests pass for:

1. IPv4/IPv6 wildcard and loopback listener denial, plus shell denial in the
   exact login command policy.
2. The installed Claude 2.1.283 reporting signed out from a fresh private profile.
3. Actual startup failure producing `Available=false`, and the same failure
   preventing both login command creation and execution.
4. Shared native containment denying workspace reads/writes, symlink and
   `/System/Volumes/Data` aliases, shell and unapproved network while private
   state remains usable.

The startup probe fails with SIGTRAP in the installed native Bun runtime. Narrow
investigations of process-fork, dynamic-code generation and notification-center
lookup did not make it work; none of those permissions was added. A proposed
Seatbelt `localhost` bind/inbound allowance admitted a wildcard socket and actual
traffic on a non-loopback interface. It was removed, and the committed login
policy grants no inbound exception. No unsandboxed launch is used as a fallback.

No live account sign-in, inference, account switching, refresh, completed or
interrupted live resume, paid entitlement, revocation or real-client MCP request
was performed. A startup probe passing on another runtime would only establish
startup capability. Release still requires the supported real-account task and
negative cases in EXT-001, native terminal qualification and candidate-specific
platform evidence. Windows/Linux containment remains unavailable.

## Vendor support and billing boundaries

Current official [legal and compliance guidance](https://code.claude.com/docs/en/legal-and-compliance)
permits hosting or preinstalling the unmodified CLI under its stated terms with
end-user authentication and direct vendor billing. Trove currently discovers a
user installation; it does not redistribute a runtime, resell access, implement
Claude.ai OAuth, collect subscription credentials or claim that an Agent SDK
integration inherits subscription eligibility. The CLI's own authentication
methods are not patched or bypassed; unqualified modes remain unavailable for
managed execution. Release ownership must recheck the applicable terms before
packaging or offering a hosted route.

The adapter was checked against official
[authentication](https://code.claude.com/docs/en/authentication),
[CLI reference](https://code.claude.com/docs/en/cli-reference),
[headless execution](https://code.claude.com/docs/en/headless),
[streaming output](https://code.claude.com/docs/en/agent-sdk/streaming-output) and
[session](https://code.claude.com/docs/en/agent-sdk/sessions) documentation.
The accepted CLI protocol family is 2.1.268+, which includes the required
restricted/permission-prompt controls and public configuration status. This
protocol-family check does not replace native capability or live qualification.
