# Claude profiles and native login handoff

The source implementation now includes private profile creation, public vendor
status, explicit Terminal login, cancellation, logout and a trusted resolver for
managed execution. The installed Claude 2.1.283 still fails the actual contained
startup check. Login and runs remain disabled; these source changes do not close
EXT-001's native account qualification.

## Interface and storage

`ClaudeAccounts` receives `workspace`, `runtimeAvailable`, `selectedProfileId` and
`onSelect(profileId, billingMode)`. Its controls use `providers.claude.list`,
`create`, `status`, `login`, `cancel` and `logout`. Public profiles expose labels,
explicit billing, public vendor status, login-attempt receipts and failed-logout
state. There is no login-code, access-token or credential field. Selecting a
profile does not start a run; creating it does not start authentication.

`App.resolveClaudeProfile(ctx, profileID, expectedBilling, runtime)` returns a
partial `providers.ClaudeConfig` and display label to the trusted managed host.
It refreshes public vendor identity and verifies startup, billing and login state.
The run's account selector uses the profile ID; the runtime checkpoint uses the
vendor account fingerprint. Context, model, session directory, bridge and tools
are supplied separately after their normal owner review. Failed logout remains
blocked on disk across restart until the vendor confirms signed-out status.

The app chooses `<UserConfigDir>/Trove/provider-claude` outside the workspace.
Renderer requests cannot select paths. An owner-only root lock prevents a second
app process from changing profile metadata. Symlink storage, public files and
invalid identities are rejected. A vendor-writable state root cannot be replaced
with a symlink and turned into a wider filesystem grant on the next launch. Each profile's writable vendor directory is
`profiles/<UUID>/state`; session working directories remain below its `sessions`
child. Host launch control lives separately under `attempts/<UUID>`, beyond the
vendor's allowed filesystem scope. The host stores profile metadata and public
receipts, never terminal input/output or vendor credential-file contents.

## Explicit terminal lifecycle

1. The owner selects a profile and its immutable billing route, then chooses
   sign-in. The app stops a managed run using that profile and performs the real
   contained startup gate before preparing or opening a terminal.
2. Trove creates private control data and an owner-executable `.command` script.
   The script contains only exactly shell-quoted paths to this app executable
   and its control file. `/usr/bin/open -a Terminal` runs only for this explicit
   action. Terminal is not launched by list/status/create or on app startup.
3. The app executable handles `--trove-claude-login <control>` before creating any
   Wails bridge. It accepts only the fixed private root and matching profile,
   billing, attempt and expiry. An exclusive start marker prevents replay. The
   actual runtime startup/hash gate is applied again inside the helper.
4. The unchanged vendor process receives the terminal descriptors directly.
   Trove does not read, parse, buffer or log terminal bytes. Vendor login uses
   the same outer containment as runs, with no callback listener or Keychain
   exception. If supported by the runtime, a browser code is entered only in the
   vendor's terminal. No Trove OAuth implementation or code-capture API exists.
5. A private cancellation marker, app shutdown or workspace switch requests
   cancellation; the helper watches every 100 ms. SIGHUP, SIGINT and SIGTERM also
   cancel the vendor process group. A ten-minute deadline bounds the helper.
   An attempt cancelled before Terminal starts cannot start later.
6. The helper writes a bounded public receipt. `completed` means the vendor
   command exited successfully, **not** that the account has been verified.
   Public status must be refreshed before selection, and the run resolver always
   refreshes it again. No success is inferred from an open terminal or EOF.

The control directory, runtime snapshot and profile metadata are not writable by
the contained vendor. Unknown/expired controls and a second launch fail closed.
No embedded terminal emulator, shell-based credential handling or unsandboxed
vendor fallback is used. All terminal and host-file paths are locally selected.

## Verification and remaining qualification

Provider tests pass under the race detector. Synthetic native commands verify
one-time claims, exact launch quoting, cross-profile/billing/expiry rejection,
startup gating, cancellation, late-launch refusal, private metadata, persisted
logout failure and shared checkpoint identity. Token formatting/JSON regressions
also cover the ephemeral MCP bearer. Dotted canonical tools such as `search.query`
remain intact through the CLI's MCP allowlist.

Three browser tests exercise the actual account component with native-response
fixtures: explicit billing and selection, cancellation and failed logout, and
read-only metadata when startup is unavailable. Native route tests reject path
injection, code fields, unknown billing, wrong workspaces, external actors and
revoked owner grants before touching host profile storage. Frontend type checking
passes. An initial browser fixture parse error was corrected before all three
cases passed; this was fixture failure, not account evidence.

Actual Terminal launching, manual-code authentication, account switching,
refresh/revocation, Console/Keychain behavior, terminal close/crash cleanup and a
real scoped inference/resume still need explicit qualification on a runtime that
starts within containment. No real account or live inference was used here.
Synthetic subprocess tests are not evidence of those vendor or desktop flows.
Windows/Linux managed execution and native login remain unavailable.
