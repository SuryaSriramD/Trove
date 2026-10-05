# Verified rollback startup handoff

Source: `3f069dc6ab2d9a4f2c00d38e9d8ab45d992ad6c4`. Evidence: [manifest](../../evidence/completion/rollback-readiness-3f069dc/manifest.json).

Rollback now requires a signed application capability (`TroveRollbackHandshakeVersion=1`) before launching. Existing Developer ID, publisher, format, application inventory and restored-workspace checks remain mandatory. Older applications without this capability can still be inspected/restored according to existing rules, but cannot perform an automatic launch handoff.

The parent passes a fresh 256-bit nonce and the exact reviewed path, workspace ID and format over private inherited pipes. Frames are strictly parsed and bounded. The child verifies the parent PID, marks descriptors close-on-exec before Wails/helper dispatch, and holds the owning workspace lock. Its pending SQLite pools use read-only mode and query-only connections: no fallback creation, migration, additive initializers, recovery or jobs run. Renderer mutations, native pickers, exports, uploads, workspace switching, providers and other side effects are blocked; only the canonical snapshot bootstrap is dispatched. ShellReady runs independently of qualification-report environment variables.

After the renderer has displayed the snapshot and yielded two frames, the parent authenticates the child response and sends commit. Only then does the child activate writable pools and complete runtime initialization/job recovery. Its final authenticated acknowledgement precedes the old application's successful return and quit. Readiness is bounded to 45 seconds; committed initialization has a separate 15-second acknowledgement deadline. Cancellation before commit kills/reaps the retained child process; late cancellation waits for the final acknowledgement. A failed acknowledgement keeps the original app open and requires a fresh restored copy before retrying if initialization may have started. Unverified child termination retains the selection and reports an error rather than silently redirecting a potentially live child.

Workspace preference capture, replacement and failure restoration now share a persistent cross-process OS lock. Restoration preserves a newer selection written by another process. Lock contention is bounded and the lock inode is not deleted.

## Verification

- Exact DELETE-journal database bytes and journal mode remain unchanged during pending startup; attempted canonical/schema writes fail read-only. Existing running jobs remain running until committed initialization recovers them.
- Protocol tests cover bounded/duplicate/malformed frames, PID/nonce/destination mismatches, early exit, absent readiness, initialization failure, cancellation, late cancellation and process reaping.
- Native binding guards and a separate-process preference writer regression run under the race detector.
- Full affected workspace suite passed (314 top-level tests plus subtests); full desktop suite passed (42 top-level tests), followed by the additional core gate regression. The updates suite passed. Explicit native qualification skips in ordinary runs remain visible.
- The first full-suite attempt hit sandbox-denied fixture sockets (clipper and local relay). Its log is retained. The affected full suites then passed with their required disposable loopback listeners enabled.
- An actual ad-hoc Wails child built from the clean source commit acknowledged the rendered, locked disposable workspace and completed initialization. `vcs.modified=false`; executable SHA-256 is recorded in the manifest. Its test also proves the production verifier rejects the ad-hoc bundle and verifies ownership release after cleanup. Private temporary HOME/workspace were used; no account, model inference or device action ran.

The clean-source native acknowledgement took **9.293 seconds**. An earlier uncommitted build of the same implementation took **1.278 seconds**. Both raw results remain. These are parent wall-clock measurements from executable launch through handshake acknowledgement, distinct from the Go-initialization-to-shell benchmark; **9.293 seconds does not establish a shell-threshold failure**. This establishes functional handoff behavior, **not startup performance qualification**; the previously observed long-tail launch remains unresolved.

Developer ID signed/notarized pair execution, independent clean-install/rollback qualification and platform release evidence remain open. The test-only native harness does not add a product signature bypass. Application inventory includes the reused `be6fc81` development bundle's native/resources and the newly built executable; this is a focused development candidate, not a newly qualified full release.
