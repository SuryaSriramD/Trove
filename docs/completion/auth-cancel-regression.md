# Authentication cancellation regression repair

The integrated race run at `74a6e35a92f9e935067474a747f9f4900da405ee` failed `TestSIWCCancelledCredentialCleanupSerializesNextSignIn`: an HTTP EOF was followed by a callback-completion timeout. The original failed run remains a failed integrated checkpoint. This package repairs the test and does not reclassify that checkpoint as passing.

Cancellation deliberately closes the loopback server (`manager.go`). The test launched the normal successful-callback fixture helper in a worker goroutine; that helper called `t.Fatal` on EOF. `Fatal` exited the worker before its following `close(callbackDone)`, so the test then manufactured a second timeout. The sibling cancellation matrix already permits the callback connection to close. No production data race or credential leak was established by this failure.

Only `manager_prompt_test.go` changes. A bounded HTTP request now returns its outcome to the main test goroutine and always defers completion signaling. A cancellation response or recognized connection closure is accepted only after independent assertions prove the attempt is cancelled, the correct credential was deleted exactly once using a valid bounded cleanup context, and no cancelled credential remains. Unexpected status, unrelated transport error or timeout still fails. The existing checks that a subsequent sign-in cannot overtake cleanup, succeeds afterwards, retains its own credential, and survives cancellation of the completed attempt are preserved. Production authentication and cancellation code and the normal successful-callback helper are unchanged; no timeout was relaxed.

The focused regression passed **30 race-detector repetitions**, exercising 29 HTTP cancellation responses and one actual closed-transport outcome. The complete affected auth package then passed three race-detector repetitions; exact top-level/subcase/skip counts are in the manifest. These use local synthetic credentials and loopback HTTP only. They provide no real-provider account or native credential-store release evidence.

The initial sandboxed repeat could not bind the loopback listener: all 30 attempts failed before reaching a callback. An isolated socket probe returned `PermissionError: [Errno 1] Operation not permitted`. That separate log is retained. Approved loopback-capable runs exercised the actual test; this environment failure is not substituted for or conflated with the earlier integrated EOF failure.

Reproduce with the pinned offline Go environment:

```sh
go test -count=30 -race -json ./internal/providers/auth -run '^TestSIWCCancelledCredentialCleanupSerializesNextSignIn$'
go test -count=3 -race -json ./internal/providers/auth
```

Run with permission to bind local loopback sockets. Re-run the broader integrated suite at the resulting integrated source before claiming a passing integration checkpoint. Logs and source hashes are recorded in `evidence/completion/auth-cancel-regression/manifest.json`.
