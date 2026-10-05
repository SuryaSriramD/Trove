# Contained Claude startup: ICU dependency correction

Claude Code 2.1.283 now passes version, general help, and authentication help in a fresh, networkless private profile. This closes the reproduced native startup defect; it does not qualify live authentication, inference, account switching, refresh, revocation, or completed-turn resume.

## Reproduction and cause

The tested official runtime is unchanged: SHA-256 `d8cb1e5c79684cc12a8bfc813e3a2073406921b6245744b3009be3ab5651d21e`, embedding Bun 1.4.3 (`7c1d0ed28`), on macOS 27.0.1 build 26A434, arm64. Under the original policy, `--version` exited successfully, while `--help` and `auth login --help` trapped after approximately 100 ms. No account state was present and networking was denied.

The symbolicated stack points to JavaScriptCore's lazy default `Intl.Collator` initialization, reached by `String.localeCompare` while sorting command help. The installed native binary links Apple's `libicucore.A.dylib`; the separate public data archive is `/usr/share/icu/icudt78l.dat`. The policy allowed the runtime library but omitted that data file.

Independent evidence establishes causality:

| Probe | Result |
| --- | --- |
| Original production policy, unchanged Claude `--version` | Exit 0 |
| Original policy, unchanged Claude general/authentication help | SIGTRAP |
| Same policy, copied public ICU data inside private state and `ICU_DATA` pointed there | Authentication help exits 0 |
| Original policy plus only a literal `file-read-data` grant for the system ICU archive | Authentication help exits 0 |
| Independent C program calling `ucol_open("en_US")`, original policy | `U_FILE_ACCESS_ERROR`, no collator |
| Same C program with the literal data-read grant | Usable collator; ICU's nonfatal `U_USING_DEFAULT_WARNING` |

The C fixture does not load Claude or access an account. This separates the operating-system data dependency from guesses about JIT, process forks, notifications, or authentication. ICU's [data-loading documentation](https://unicode-org.github.io/icu/userguide/icu_data/) and Apple's [ICU implementation](https://github.com/apple-oss-distributions/ICU/blob/main/icu/icu4c/source/common/putil.cpp) describe the separate data directory and its override behavior. The current [vendor environment documentation](https://code.claude.com/docs/en/env-vars) was also checked; no alternate authentication or bare-mode workaround was introduced.

## Implemented boundary

`macOSICUReadPolicy` inspects only `/usr/share/icu`, with fixed `/usr` and `/usr/share` ancestors. All directories and selected files must be root-owned, not symlinks, and not writable by group or other users. Only regular `icudt<two-or-three-digit-version><l|b>.dat` files between 1 byte and 128 MiB are eligible. The inventory is bounded to 64 entries and eight data files.

The generated rule permits data reads of those exact files. It does not permit directory listing, adjacent locale files, writes, executable mapping, extra binaries, user home content, Keychain services, or new network access. The deployed fix reads the protected operating-system asset directly; the successful private-copy experiment is retained only as diagnostic evidence.

The containment identity advances to `trove-provider-macos-v2`. Existing checkpoint and reviewed-scope bindings therefore require a fresh review when the policy changes. The actual capability probe remains in place, so another unsupported runtime or operating-system environment still fails closed.

## Verification and remaining qualification

The actual macOS provider race suite passes, including Claude stream/proposal protocol fixtures, version/help startup, signed-out private-profile status, Codex's offline initialization/empty-resume refusal, and shared cancellation and identity checks. The independent ICU regression asserts failure without the exact rule and success with it.

Native denial probes still reject synthetic workspace reads/writes, symlink and data-volume aliases, shell execution, unapproved outbound networking, and IPv4/IPv6 loopback/wildcard listeners. Additional checks deny listing `/usr/share/icu` and reading the existing neighboring `icutzformat.txt`. Native provider route tests also pass.

Exact checks, runtime/data/source hashes, and retained diagnostic logs are in `evidence/completion/claude-icu-startup/manifest.json`. These are source-level and installed-runtime checks on this development Mac. A packaged candidate, native Terminal account flow, authorized live requests, handoffs and platform-specific release qualification remain separate gates. No user credentials or private workspaces were read.

## Diagnostic reporting disclosure

During investigation, a GET intended to read the Bun stack-symbolication URL unexpectedly returned “Crash Report Sent” and stated that its redacted report had been sent to the Bun team. That URL encoded only this synthetic no-account crash's stack addresses, runtime version, platform and runtime feature flags. It contained no prompt, workspace content, account, credential, or authentication operation. No further reporting endpoint was called. Reporting URLs are removed from retained logs to prevent accidental resubmission; original local-log SHA-256 values and this endpoint response are recorded in the evidence manifest. The decoded top frames were `WTFCrashWithInfo`, the lazy `IntlCollator` initializer, `defaultCollator`, and `stringProtoFuncLocaleCompare`.
