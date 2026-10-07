# macOS production signing and notarization

This package adds an explicit Developer ID path to the existing macOS builder and a separate resumable notarization command. Default development builds still use ad-hoc signing. The implementation has fixture evidence only: **175 Python tooling checks pass**, including **14 signing boundary checks**. No real certificate lookup, signing, Keychain account access, Apple submission or clean installation was performed for this package. The [evidence manifest](../../evidence/completion/macos-production-signing/manifest.json) preserves the invocation error and corrected pinned-toolchain run.

## Required inputs and support boundary

The current builder targets **macOS 27.0 or later on Apple silicon (darwin/arm64)**. It does not establish support for earlier macOS versions, Intel Macs, Windows or Linux. A public candidate requires an explicit three-component version, increasing positive build number, reviewed release notices/provenance, the pinned native bundle and a new app/evidence destination. The source commit, full toolchain/model/fixture identities and acceptance results must also be attached by the release qualification layer; the signing inventory alone does not establish those identities.

An authorized release operator must supply the exact SHA-1 fingerprint of an available **Developer ID Application** identity and its ten-character Team ID. The private signing key remains in Apple's supported local signing flow. Notarization additionally needs Xcode command-line tools and an already configured `notarytool` Keychain profile. The command accepts only its profile name, never passwords, tokens or private keys. It never exports credentials or stores vendor output. Missing/incorrect identity, profile or tools blocks the operation; there is no automatic ad-hoc fallback.

Prepare and review model-pack redistribution and third-party notices separately. Signing does not approve their licenses or include model weights. Confirm disk space for the app, submission ZIP, final ZIP and one extraction before a real run. This implementation supports at most 10,000 regular bundle files/4 GiB and a 20,000-entry/5 GiB expanded archive. Unexpected nested code or symlinks are refused until explicitly supported.

## Explicit build and distribution steps

Select the existing pinned cache through `TROVE_CACHE_DIR`. Use the matching pinned Python environment. The following values are placeholders for an authorized operator; no command here has been run with an account.

```sh
python scripts/build-desktop.py \
  --cache <pinned-cache> --native-bundle <verified-native-bundle> \
  --output <new-directory>/Trove.app --release-notices <reviewed-notices> \
  --version <major.minor.patch> --build-number <increasing-number> \
  --signing-identity <exact-40-hex-certificate-fingerprint> --team-id <team-id> \
  --signing-evidence <new-external-evidence-directory>
```

The normal P0 build is the default; existing phase flags select later packages. Supplying signing-related flags without the explicit identity is an error before a build or account command. The production preflight requires both app and evidence paths to be new. Keep a failed partial candidate and its evidence for diagnosis; a build retry requires new destinations.

The builder first checks copied source native bytes against their source bundle, then validates helper manifests, architecture, executable modes, version/minimum OS and the reviewed audio-input-only entitlement files. Each known nested executable is signed and independently verified with the selected team, Developer ID requirement, secure timestamp and hardened runtime. Only then are its manifest and sidecar hashes updated. The outer app is sealed last and verified again. There is no recursive `--deep` signing; recursive verification is used for the final seal. No debug or hardened-runtime exception entitlements are permitted by this policy.

Review the sealed app and `02-signed.json` before explicitly submitting it:

```sh
python scripts/notarize-macos.py --app <signed-app> \
  --signing-evidence <same-evidence-directory> \
  --notary-profile <configured-keychain-profile-name>
```

Alternatively, the initial build accepts `--notarize --notary-profile <name>` for an explicitly authorized combined build/submission. Merely building with a Developer ID does not contact Apple's notary service.

## Retained evidence and retry behavior

| Record | Meaning |
| --- | --- |
| `01-preflight.json` | Public certificate fingerprint and selected team; no certificate/key/credential export. |
| `02-signed.json` | Before/after file SHA-256, size and mode inventories after nested signatures, hash rebinding and outer sealing. |
| `notary-submission.zip`, `03-submitted.json` | Exact uploaded archive hash and Apple's submission ID, retained immediately after the submit response. |
| `notary-wait-*.json`, `04-accepted.json` | Only selected ID/status fields; acceptance must match the retained submission. |
| `05-stapled.json` | App inventory after stapling; subsequent retry must match it exactly. |
| `Trove-notarized.zip`, `06-frozen.json` | Gatekeeper and staple verification, independently extracted byte/mode/signature/staple checks, final archive hash and app inventory. |

A pending submission or an interrupted wait resumes with the same command and retained ID, without uploading again. A rejection cannot staple or freeze a candidate. If the upload outcome is unknown and no receipt was saved, automatic resubmission is refused: an operator must investigate the Apple submission history and retained archive. This version has no automatic recovery/import of an unknown submission ID. Preserve those artifacts; do not delete them to force a retry. A failure after the final ZIP was created also requires inspection rather than overwriting that evidence.

A frozen candidate cannot be resubmitted. Modification, re-signing or rebuilding changes its identity and requires a new candidate and affected qualification. Every record retains `releaseQualified: false` where that flag is present. Accepted notarization establishes distribution integrity, not product acceptance or App Review. Native devices, permissions, real-model tasks, performance, update/rollback and backup restoration still require candidate-specific evidence. The unavailable independent clean Mac remains an open release prerequisite; a fresh HOME or same-machine installation cannot satisfy it.

The regular-file inventory includes executable modes but does not serialize extended attributes. `ditto` preserves resource forks/extended metadata; the extracted final archive must separately pass signature and stapled-ticket validation.

## Verification and official references

Fixture tests independently mutate nested bytes at signing time and assert that all recorded helper hashes match before the outer seal. Negative cases cover absent/wrong identities, unexpected code, changed helper bytes, missing timestamp, wrong signed team, debug entitlements, incomplete build flags, changed sealed app, mismatched/rejected/pending notary responses, unknown upload outcomes, and lost executable mode during extraction. All vendor calls are injected fakes: test success does not prove a real certificate, Apple's service or a distributed download works.

The first broad invocation omitted `TROVE_CACHE_DIR` and failed one generator check because the pinned Go installation was not selected (175 run, one error, one optional skip). The corrected offline invocation explicitly selected the existing pinned cache and passed all 175 checks. No source workaround or tool download was used.

Apple's [distribution signing guidance](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/) supports signing nested executables individually before sealing the outer bundle and using Developer ID timestamps/hardened runtime. The [notarization overview](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) and [custom workflow guidance](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow) describe explicit submission, acceptance, stapling and repackaging. Credentials stay in the supported `notarytool` workflow described by [TN3147](https://developer.apple.com/documentation/technotes/tn3147-migrating-to-the-latest-notarization-tool).
