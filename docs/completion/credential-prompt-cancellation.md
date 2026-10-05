# Credential prompt cancellation repair

Review of auth backend commit `30b62944fdc171fd87def150728c9977411ad534` found a Manager-level defect that its low-level Secret Service cancellation tests did not cover. The sign-in callback held the registration mutex while calling `SecretStore.Put`. A Linux desktop keyring prompt could therefore block both `Manager.Cancel` and `Manager.Close`. If the user unlocked the prompt, the callback could publish a completed, signed-in account before Cancel obtained the mutex and became a no-op.

An independent synthetic-store regression against the unmodified commit reproduced all three outcomes: Cancel did not signal the active store context, the attempt completed, and the account was signed in. Its exact test source and failing output are retained in `evidence/completion/platform-credentials/manager-cancellation-baseline-test.txt` and `manager-cancellation-baseline.txt`.

## Repair

`Manager` retains its existing registration mutex across credential I/O and cleanup, preserving serialization with refresh, sign-out and later sign-ins. A separately protected registry contains only the immutable cancel functions for pending attempts. Cancel and Close signal those functions before waiting for registration state. Close also prevents a new attempt from being registered after shutdown starts.

The final local metadata commit and explicit cancellation share a short ordering boundary. A cancellation that reaches the pending attempt prevents it from being published as signed in. An already committed/completed attempt stays outside Cancel's scope. Caller-context cancellation is checked before and after the metadata write as well.

A canceled credential write may already have reached the native store, even when its returned error reports cancellation. Every attempted write is therefore followed by deletion of that registration's protected record using a fresh five-second cleanup context. Cleanup runs while the registration mutex still excludes a newer write, so it cannot delete a later sign-in's credentials. The registration persists as `signed_out`; failed deletion persists as `sign_out_incomplete`, denies token use and requires cleanup before reauthorization. UI messages remain fixed and expose no native error text or tokens. The temporary marshaled credential buffer is cleared after storage returns.

This changes pending sign-in cancellation. It does not add a general cancellation API for standalone token-refresh or sign-out calls; those continue to use their supplied operation contexts and existing cleanup deadlines. Concurrent catastrophic storage failures can still prevent durable state changes, and their failure is reported rather than claimed successful.

## Verification

Checked on the development Mac (macOS 27.0.1, arm64) with pinned Go 1.27.1. No real account, user keyring or Windows DPAPI record was used.

- Full auth race suite: **20 top-level tests and 26 subcases pass**, no skips, 7.102 seconds (`manager-cancellation-auth-race.jsonl`). This includes the existing private D-Bus daemon, encrypted synthetic Secret Service, identity/scope/rotation and redaction checks.
- New Manager matrix: explicit Cancel, Close and caller cancellation, each with a cancel-aware store, a record stored before cancellation, a late successful write, a late write with a lost acknowledgement, and failed deletion. Assertions cover prompt context cancellation, disabled account state, scoped/bounded cleanup, unrelated account preservation, refusal to return tokens and restart persistence.
- A second sign-in cannot overtake a blocked canceled write/cleanup. After cleanup it can complete, its credentials remain usable, and canceling that already completed attempt leaves them intact.
- Windows amd64 and Linux amd64 auth test binaries compile. Neither binary was executed on its target platform. Their SHA-256 identities, source hashes and evidence hashes are in `manager-cancellation-manifest.json`.
- `git diff --check` passes for the repair.

Reproduce from the checkout root with the pinned toolchain and module cache:

```sh
go test -mod=readonly -race ./internal/providers/auth -count=1 -timeout=90s -json
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -mod=readonly -c ./internal/providers/auth -o /private/tmp/trove-auth-cancellation-windows.test.exe
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -mod=readonly -c ./internal/providers/auth -o /private/tmp/trove-auth-cancellation-linux.test
```

Real GNOME/keyring prompts and packaged Cancel/Close flows, Windows ACL/junction races, cross-user refusal, reboot/crash durability, and complete Windows/Linux release qualification remain open as recorded in [platform-credentials.md](platform-credentials.md). Static Windows path checks are not evidence of dynamic junction-race qualification.
