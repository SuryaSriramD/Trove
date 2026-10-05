# Verified macOS updates and compatible recovery

This package implements the missing application-verification and reviewed rollback workflow for REL-006. **It does not qualify a signed release.** The current development app is ad-hoc signed and correctly cannot authorize production updates.

## Product workflow

Settings offers a native application picker. Trove verifies both the running app and the proposed update, displays their versions, build numbers and publisher, then requires a separate action to save the recovery folder. That folder contains `previous/Trove.app`, `update/Trove.app`, the consistent online workspace backup, and a manifest binding their exact identities. Preparation accepts a new folder outside both apps and the workspace. Interrupted or failed preparation retains an `INCOMPLETE` fence.

Installation uses a manual restart: reveal the verified update in Finder, save edits, quit Trove, and open the saved update. Existing installation files are not overwritten. The previous application and backup remain paired in the recovery folder. Application verification runs again before revealing the saved update.

Rollback is separate and explicit. A native picker selects a completed recovery folder, both app signatures and the backup are reverified, and the user reviews the previous app and backup date. Restore writes a **new** compatible workspace and revokes installation-specific permissions without opening a kernel, running migrations, or rebuilding indexes. The newer workspace remains preserved. The launch button rechecks the signed previous app and every restored database/asset/recording/receipt byte, selects the restored workspace before spawning the old app, supplies that exact path in a scrubbed environment, and quits the current app only after a successful spawn. A failed spawn restores the prior startup selection. No renderer input supplies executable paths, filesystem paths or environment values.

The welcome screen also offers recovery after workspace startup fails. It reviews and restores the exact pre-migration snapshot into a new folder. This path does not require an open kernel. See [pre-migration snapshots](pre-migration-snapshots.md) for storage fencing and compatibility details.

## Verification boundary

- Apple code-signature verification requires sealed resources, the Trove bundle identifier, the same Developer ID team, the Developer ID certificate requirements, and Gatekeeper assessment. Ad-hoc/unsigned/different-publisher apps are rejected. This follows Apple's [code-signing requirements](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements) and [distribution policy guidance](https://developer.apple.com/library/archive/technotes/tn2206/).
- Signed bundle metadata includes the architecture, minimum OS, monotonically increasing build number and storage compatibility. `cmd/trove-format` supplies the authoritative storage values when packaging; Go constants are not duplicated in Python. The executable architecture and host OS are checked before accepting an app.
- Application inventories reject links, special files and unsafe modes, bound paths/object counts/bytes, hash every file, preserve executable modes, and fsync copied files and directories. Copies are reauthenticated before publication. The inventory bound is 10,000 objects and 4 GiB per app.
- Native reviews expire after ten minutes, are one-use and bind the workspace and exact reviewed contents. Backup digest and copy inventory come from the same bounded manifest read. A self-consistent replacement after review is rejected. Migration restore similarly consumes the exact reviewed manifest.
- A recovery operation has an independent cancellation control; cancellation is checked after native dialogs and during copying/hashing. macOS directory pickers themselves remain OS-controlled; cancelling work takes effect when the picker returns. Failed copies are never reported as completed recovery folders.
- Paired rollback currently supports the explicitly frozen format 3 / storage generation 1 permission cleanup contract. A future format must add a reviewed cleanup adapter. Unknown/future formats fail closed.

The source review found and repaired two issues: a manual old-app launch could have opened the newer startup workspace first, and separate backup-manifest reads could have admitted a replacement after review. Dedicated regressions now exercise both failures.

## Evidence and limits

[Evidence manifest](../../evidence/completion/verified-updates/manifest.json) binds implementation file hashes and raw reports. Affected native adapter race checks pass, update boundary race checks pass, visible browser fixtures pass, and packaging tests verify signed metadata and invalid identity rejection. A real ad-hoc packaged candidate is rejected by the production macOS verifier. Test-only signature and launch substitutes are dependency injections in unexported helpers; they are never enabled by a product environment variable or renderer flag.

There is no passing Developer ID/notarization install-and-restart case yet. Real signed old/new app pairs, Gatekeeper behavior on independent clean Macs, physical interruption/power-loss recovery, external volumes, and full uninstall/update/rollback release acceptance remain unqualified. Windows/Linux update UI, publisher verification and application handoff are not implemented by this macOS package. No release gate is closed by these tests.
