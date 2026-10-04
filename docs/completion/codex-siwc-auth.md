# Codex account authentication boundary

Status: implemented and tested with synthetic OAuth fixtures. This package does
not qualify managed Codex execution or real account/model eligibility.

## Implemented contract

`internal/providers/auth` provides `NewManager`, `List`, `Begin`, `Attempt`,
`Cancel`, `Retry`, `AccessToken`, `SignOut`, and `Close`. Production endpoints are
fixed to OpenAI. Only the trusted Go host receives `Token.Value`; JSON and ordinary
Go formatting redact the bearer value. Renderer-visible account records contain
registration identity, state, permission, and expiry. Authorization URLs, callback
codes, PKCE values, and tokens are never returned to the renderer or written to
workspace content, audit records, logs, or browser storage.

First registration uses `dynamic_agent_client`, the stable host UUID URI,
`agent_name_hint=Trove`, S256 PKCE, and independent random state/nonce. The listener
binds to `127.0.0.1` before the browser opens, with `/auth/callback` kept unchanged.
Only its port varies; the exact URI is reused for code exchange. The callback's
issued client ID is used for exchange and registration. Returning sign-in keeps
that registration's issued ID and validated subject; different callback clients
or verified subjects cannot replace it. A code-exchange `invalid_grant` offers an
explicit retry retaining the issued pending ID without fabricating a verified
account. Attempts expire after ten minutes and cancellation prevents late saves.

The pinned maintained `coreos/go-oidc/v3` verifier validates RS256 signatures using
OpenAI discovery/JWKS, issuer, audience and expiry. Trove separately checks nonce,
subject, authorized party for multiple audiences, issued-at bounds, and returning
identity. Network requests reject redirects, non-OpenAI hosts and non-HTTPS URLs;
responses are capped at 256 KiB and each HTTP request at 15 seconds. The callback
rejects wrong state, duplicate security parameters, other origins/hosts/paths and
non-GET requests. Callback responses have no-store, no-referrer and restrictive
CSP headers and never echo provider errors or tokens.

Both `chatgpt.tokens.use.direct` and `resource.invoke` are required before
`AccessToken` can authorize inference. Sign-in lacking plan permission remains a
visible signed-in account with plan usage disabled. An explicit enable action
reauthorizes the existing registration with complete scopes and `prompt=consent`.
There is no inferred plan permission, model entitlement, or API billing fallback.

## Storage, refresh and sign-out

The desktop adapter chooses the fixed host directory under the OS application
configuration root: `Trove/provider-auth`. It rejects any overlap with the selected
workspace. Registration metadata is atomically written with owner-only permissions;
it contains no tokens. A process-lifetime, non-inherited exclusive file lock
prevents two Trove instances from racing rotating refresh tokens. A second owner
fails visibly, rather than reading another process's stale state.

On macOS with cgo, generic-password records use the native Security framework,
with a Trove/host-root service namespace and registration-specific account keys.
Records do not synchronize to other devices. Each secret record includes its host,
registration, subject and issued-client binding. Keychain failure has no plaintext
fallback. Other OS builds fail closed until their credential backend is qualified.
No existing vendor credential files are read or copied.

Refresh serializes within the manager and across processes, sends the issued
client ID and resource, omits scope, and persists the replacement refresh/access
pair together. An omitted refresh scope retains the prior grant. Optional refreshed
ID tokens must validate against the same identity. Refresh timing respects returned
`earliest_refresh_at`; temporary errors retain credentials, while known terminal
refresh codes clear unusable tokens and require sign-in. A credential-store failure
cannot turn a registration into a usable account.

Sign-out disables the selected local registration, cancels its pending sign-in,
clears its protected tokens, and attempts remote revocation with a bounded retry
using the discovery-advertised endpoint. Its refresh token exists only in memory
for that attempt. The result distinguishes remote revocation from local cleanup;
unconfirmed revocation points the user to ChatGPT Settings. Failed local deletion
leaves a visible retryable cleanup state, blocks inference and new sign-in, and
retains the account/client mapping. Other account registrations remain untouched.

## Desktop integration

`app_provider_auth.go` exposes `providerAuthDispatch` for `providers.auth.*`,
`getProviderAuth()` for the host runtime, and `stopProviderAuth()` for lifecycle
shutdown. Requests validate the authoritative service schema and live workspace
owner grant before any credential-store access. There is no renderer token endpoint.

The main dispatcher must route `providers.auth.*` before general provider methods.
Call `getProviderAuth()` outside existing `a.mu` locks. Close authentication after
stopping managed runs on workspace close/application shutdown. Before sign-out,
stop all managed runs that already received this registration's access token;
manager revocation alone cannot withdraw a token already passed to a child process.
Account switches must invalidate incompatible execution/resume identity and retain
an explicit billing selection. These run/lifecycle hooks belong to the managed
runtime integration package.

`ProviderAccounts.tsx` takes `workspace`, `selectedRegistrationId` and `onSelect`.
It is mounted by the parent provider panel during runtime integration. Saved
accounts sharing an email still have distinct labels. Sign-in completion requires
explicit selection; permission-disabled accounts cannot be selected. The component
supports cancellation, issued-client retry, reauthorization and selected sign-out.

## Verification and release limits

- Race-enabled Go tests exercise real loopback listeners with synthetic RSA-signed
  tokens and an injected credential store/transport/clock. They cover PKCE/state,
  signature/issuer/audience/nonce/expiry, account/client mismatch, same-email
  isolation, persistent host identity, cross-manager locking, serialized refresh,
  temporary/terminal errors, permission-disabled sign-in, cancellation during
  exchange, retry, failed credential saves/deletion, revocation retry and redaction.
- Native desktop tests reject untrusted request fields, injected endpoints/paths,
  other workspaces, external/AI actors and revoked owner grants before storage.
- Actual Chrome component tests cover account selection, isolated sign-out,
  unconfirmed revocation, cancellation and no automatic account activation.
- TypeScript and production frontend build pass. Native Keychain code compiles on
  macOS; tests use an injected store and do not read or modify existing credentials.

Still unqualified: real sign-in/consent, the signed application's Keychain prompts
and access-control behavior, real token rotation/revocation, actual account/model
eligibility, and provider runtime containment/resume/stream completion. These
require the separate release qualification workflow and a user-authorized real
account. The authentication package does not enable an uncontained runtime.

## Official references checked 2026-10-04

- [Registration and sign-in](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [Accounts and sessions](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)
- [Token reference](https://developers.openai.com/siwc/token-sharing-open-source/token-reference)
- [Errors and recovery](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)
- [Codex app-server](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server)
- [Published OIDC discovery](https://auth.openai.com/.well-known/openid-configuration)

These document the current route. A completed inference turn, not a model catalog
or valid ID token alone, verifies access to the selected model for that request.
