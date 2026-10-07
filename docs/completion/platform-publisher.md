# Platform publisher authentication foundation

This package adds authenticated portable release inventories and canonical publisher identities. It does **not** enable Windows/Linux update selection, installation, launch or rollback. The macOS reviewed update/readiness handshake remains in place. Evidence is in `evidence/completion/platform-publisher/manifest.json`; native release qualification remains separate.

## Identities and compatibility

`updates.Identity` now supports an explicit `platform` and a value-type `publisher` (`kind`, `id`):

| Platform | Kind | Publisher ID |
| --- | --- | --- |
| macOS (`darwin`) | `apple-developer-id` | Verified ten-character Apple Team ID |
| Linux | `linux-ed25519-sha256` | Lowercase SHA256 of the configured raw 32-byte Ed25519 public key |
| Windows | `windows-authenticode-sha256` | Lowercase SHA256 of the native-verified leaf certificate DER |

Old TeamID-only macOS identity records and format-1 `Trove.app` recovery kits still canonicalize to the same Apple publisher. Conflicting TeamID/tag fields, unknown platforms, kind/platform substitutions, duplicate JSON keys and invalid IDs fail. Equality compares canonical values and complete inventories; pointer allocation and serialized legacy/tagged representation do not determine authority. Reviews still bind exact bytes, target, expiry and readiness tokens.

An upgrade must retain the same platform and publisher ID, architecture and bundle ID, increase the build number, and preserve compatible storage generations. Adding a second trusted key/certificate does not itself authorize a cross-publisher upgrade. Rotation needs a separately designed, reviewed migration policy.

## Trusted policy and signing order

`packaging/update-publishers.json` deliberately contains empty public-key/certificate lists. It authorizes no portable release. No production signing key, certificate or placeholder pin is supplied or generated. A release operator must provide independently approved public material through trusted packaging configuration. An artifact, recovered kit or renderer request cannot establish that trust.

`cmd/trove-release-manifest` requires `--artifact`, `--identity`, `--publisher-policy` and a new `--output`. Policy must be outside the artifact; output may be outside it or at its fixed `trove-release.json` name. Existing output is refused. The command produces **unsigned** canonical bytes only; it never signs, installs or reads a private key.

Build and sign any executable payload first. Then:

1. Provide explicit release metadata and externally approved public policy.
2. Generate the canonical manifest using `go run ./cmd/trove-release-manifest --artifact <artifact-directory> --identity <metadata.json> --publisher-policy <trusted-public-policy.json> --output <artifact-directory>/trove-release.json` with the pinned toolchain.
3. For Linux, the external signer signs the exact bytes returned by `updates.LinuxSigningMessage(manifest)` (ASCII `Trove portable release v1`, one NUL, then canonical manifest bytes). Write the raw 64-byte Ed25519 signature as `trove-release.ed25519`.
4. For Windows, build a SHA256 catalog containing the exact manifest member, then Authenticode-sign `trove-release.cat` using the authorized vendor signing flow. The library requires the primary verified signer to match the configured certificate pin.
5. Run the corresponding platform verifier before freezing or staging the artifact. Re-run after copying. Freeze actual bytes and evidence only after native qualification.

The host must explicitly supply approved `PublisherPolicy` when the future portable update flow is connected. There is no automatic policy discovery from candidate content. The existing desktop selector remains macOS-only.

## Exact artifact inventory

The signed manifest contains every payload file and directory, path, logical mode, size and content digest. The fixed manifest and the platform signature/catalog cannot recursively include their own hashes: they are authenticated separately and their exact bytes are bound to the returned **full** inventory. A foreign-platform authentication file is refused. Missing, extra or modified payload, malformed/ambiguous manifests, symlinks, special files, writable POSIX objects and changed authentication bytes fail verification. Inventory limits remain 10,000 objects and 4 GiB; manifests are limited to 4 MiB.

Linux Ed25519 verification is implemented and exercised with real cryptographic test signatures. Portable library recovery kits use format 2 and `previous/Trove` plus `update/Trove`; every copied tree is re-authenticated. Those tests run on macOS and do not qualify native Linux filesystem durability or release behavior.

Windows uses logical file modes because Go's Windows chmod representation is not an ACL. Files and directories, including the artifact root, are checked for unaccounted named streams; unsupported queries fail closed. Named streams, including `Zone.Identifier`, are currently refused. Do not strip provenance streams to bypass this boundary. A stream-aware, authenticated copy/provenance policy and native ACL/durability checks remain open.

## Native Windows trust boundary

The Windows x64 implementation uses native catalog member verification, with SHA256 member hashing, `WinVerifyTrustEx`, the strong-signature policy, chain revocation checks and an exact pinned verified leaf certificate. It holds native manifest/catalog handles that deny concurrent writes/deletes, checks their exact bytes, and binds the authenticated catalog to the final inventory. It does not accept a parsed certificate, executable checksum, generic fixture verifier or successful signature text as trust evidence.

Missing required native APIs, invalid catalog membership, unknown pins, chain failures and unavailable cached revocation information fail closed. Verification uses cache-only URL retrieval; it does not silently contact a network service or disable revocation. Native trust evaluation is synchronous: context cancellation is checked before and after the call, but cannot preempt an in-progress Windows trust API call. Bounded native cancellation remains a release gate.

The native implementation **cross-compiles but has not run on Windows**. Catalog creation interoperability, real publisher-chain/revocation behavior, file/directory streams, durable staging, ACLs, cancellation, clean installation and update/rollback behavior require a Windows machine and a real authorized signed artifact before promotion. No signing account or credential was used for this package.

## Checks and remaining gates

Executed on macOS arm64 with the pinned Go toolchain:

- Updates package: 21 top-level tests and 43 subcases pass with the race detector; three native-development/signing/Windows-qualification checks explicitly skip.
- Unsigned manifest command: one focused test passes, covering missing policy, candidate-provided policy, canonical output, no signing and no overwrite.
- Affected app update/recovery tests: 21 top-level tests and 62 subcases pass. Legacy saved reviews still pass through the reviewed launch stub; this is not an application launch.
- Frontend TypeScript check passes.
- Final Windows x64 and Linux x64 update test binaries compile; neither is executed on its target OS.

The Windows-native test suite contains signed-catalog/pin/tampered-member checks plus file, child-directory and root-directory stream rejection. On Windows, set both `TROVE_WINDOWS_PUBLISHER_ARTIFACT` and `TROVE_WINDOWS_PUBLISHER_POLICY` to an authorized signed artifact and independently trusted public policy before running `go test -count=1 ./internal/updates`. With both absent, real signing qualification explicitly skips; partial configuration fails. Input release files are retained; mutation tests use owned temporary copies. Native copy failures are qualification failures, not grounds to weaken the verifier.

Before enabling portable product flows, connect trusted policy through native packaging; qualify supported OS/architecture/minimum-runtime checks, exact inventory and signed manifest interoperability, Windows cached revocation provisioning, platform permissions/ACLs and durable copy; implement and test platform-specific reviewed launch/readiness/rollback; then run native tamper, interruption, clean-install, backup and rollback scenarios on the frozen candidate. Existing non-macOS launch refusals remain unchanged. This package supplies no independent machine evidence and closes no release gate by documentation alone.

## Primary implementation references

- [Microsoft WinVerifyTrust](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-winverifytrust): native trust result semantics.
- [WINTRUST_CATALOG_INFO](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/ns-wintrust-wintrust_catalog_info): catalog/member handles and SHA256 catalog context.
- [CryptCATAdminAcquireContext2](https://learn.microsoft.com/en-us/windows/win32/api/mscat/nf-mscat-cryptcatadminacquirecontext2) and [CryptCATAdminCalcHashFromFileHandle2](https://learn.microsoft.com/en-us/windows/win32/api/mscat/nf-mscat-cryptcatadmincalchashfromfilehandle2): native member hashing.
- [CERT_STRONG_SIGN_PARA](https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/ns-wincrypt-cert_strong_sign_para) and [WINTRUST_SIGNATURE_SETTINGS](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/ns-wintrust-wintrust_signature_settings): strong-signature policy and selected signature.
- [WTHelperProvDataFromStateData](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-wthelperprovdatafromstatedata), [WTHelperGetProvSignerFromChain](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-wthelpergetprovsignerfromchain), and [WTHelperGetProvCertFromChain](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-wthelpergetprovcertfromchain): verified-state certificate access. API availability is checked, with no fallback.
