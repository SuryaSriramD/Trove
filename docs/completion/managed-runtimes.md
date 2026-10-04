# Managed runtime implementation and qualification

This is development evidence for EXT-001/002/005. Managed runtime transport and
desktop integration are under construction; no live account is qualified.

The host discovers user-installed native executables in standard installation
locations, resolves the actual path, hashes it, and checks its version inside a
network-disabled private process. It snapshots the exact bytes before execution.
It does not download or redistribute a vendor runtime. Each session has private
state outside the workspace. Only the selected account token reaches a Codex
child, through its environment; no vendor configuration or credential directory
from the user's home is inherited.

The macOS Seatbelt policy denies external content, filesystem writes, symlink and
data-volume aliases, shell execution and unapproved network access. The runtime
can read system libraries and its own selected executable/state. Inference permits
outbound networking, but adds no workspace file grant. System bootstrap access
was narrowed against the installed macOS dyld requirements. Windows/Linux managed
containment remains unavailable until those platform implementations are tested.

Codex uses the official app-server stdio protocol with SIWC's public API route.
The trusted host registers only selected Trove dynamic tools and answers every
native shell/file/permission approval with denial. Tools resolve through the same
revocable ProviderBridge and reviewed ActionPlan service as MCP. Runtime output
does not itself approve proposals. The model is explicit; a reroute fails the run.
EOF/partial output never means success. Output, frames, runtime duration and
shutdown have bounds. Compatible resume binds account, billing route, model,
context/tool scope, runtime digest, adapter version and containment policy.

## Evidence

`c5-runtime-system-roots.txt` uses actual Codex 0.153.4 and Claude Code 2.1.283.
The canary verifies native denial of external reads/writes, a symlink escape,
`/System/Volumes/Data` alias access, shell execution and network; private state
still works. Both unchanged executables run `--version`. Codex also initializes
and accepts the selected dynamic-tool schema in a real isolated app-server.

An empty app-server thread has no saved rollout; actual resume returns that
explicit error. Earlier diagnostic attempts are retained. Completed/interrupted
turn resume needs a real authorized inference run and remains unqualified. The
offline probe does not demonstrate authentication, entitlement, streaming quality,
remote connectivity or Claude execution beyond discovery. No account credentials
were used, and the test's network policy prevented inference.

Race tests in `c5-managed-race.txt` verify denied runtime approvals, tool/session
binding, malformed/oversized streams, cancellation, resume identity mismatches,
owner-only execution records, interrupted-record recovery, and managed proposals
that cannot commit before explicit review. Service coverage is distinct from
native/live-account evidence.

## Interfaces checked

The adapter was checked against the installed app-server's generated experimental
JSON schemas and current official [app-server documentation](https://learn.chatgpt.com/docs/app-server)
and [SIWC app-server integration](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server).
Dynamic tools include the protocol's `type: function` tag and return typed content
items. Native session history stays in the private runtime directory. Model choices
must come from the selected account's [current catalog](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference);
catalog membership alone is not proof that an inference request will succeed.
