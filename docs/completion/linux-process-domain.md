# Linux process-domain foundation

This package implements a shared, one-shot process-domain lifecycle and a Linux cgroup-v2 backend for **trusted qualification fixtures only**. It does not enable Linux PDF extraction/export, providers or a release. The existing unavailable paths remain unchanged. The [evidence manifest](../../evidence/completion/linux-process-domain/manifest.json) records host tests, Linux compilation and every pending native case.

## Implementation boundary

`internal/processdomain.ProcessDomain` owns `Execute(context, Command)` and `Close(context)`. Configuration requires explicit memory, PID/thread, CPU quota/period, wall-time and drain-time limits. Commands use an absolute host-selected executable/directory, explicit bounded arguments/environment and optional file-backed stdio. There is no implicit shell, PATH lookup, ambient environment, inherited controller FD or caller-selected process attributes.

`New` rejects `Document`, `Provider`, unknown purposes and invalid limits before accessing the delegation. Non-Linux systems return unavailable for the fixture backend. The Linux constructor requires an existing absolute delegation owned by the supervisor, without symlink components or group/world write permission, on cgroup v2. CPU, memory and PID controllers must already be enabled. It never changes ancestor controller policy, moves host processes or asks for privileges.

A random, exclusively created child domain receives verified read-back limits: `memory.max`, zero swap, group OOM, `pids.max`, `cpu.max`, and zero allowed nested cgroup depth/count. Aggregate memory peak/current, PID count/events, CPU usage/throttling and OOM counters must be readable. Missing controls or mismatched values fail admission.

The pinned Go runtime's `UseCgroupFD` uses `clone3(CLONE_INTO_CGROUP)` so the initial process belongs to its domain before user code executes. Unsupported kernels, permissions or syscall restrictions fail startup; there is no start-then-migrate or process-group fallback. [Go's Linux process attributes](https://pkg.go.dev/syscall#SysProcAttr) expose this boundary.

After normal leader exit, cancellation or the wall deadline, the owner writes `cgroup.kill` and polls the recursive `cgroup.events` populated state. It also requires the leader to be reaped. Only then can the directory be removed, with its inode checked against the original FD. Failed cleanup preserves the domain/handles for a bounded `Close` retry; it never reports successful drain or removal. Close on an unused domain also verifies emptiness and is bounded by the configured drain timeout. A missing final usage counter makes the execution fail even if cleanup succeeded.

The [kernel cgroup-v2 specification](https://docs.kernel.org/admin-guide/cgroup-v2.html) defines inherited membership, hierarchical limits, recursive populated state and `cgroup.kill`. It also explains the delegation write permissions that permit migration within a delegated subtree. Those permissions are why an owned cgroup is not sufficient isolation from an untrusted worker running under the supervisor's UID.

## Qualification and security limits

Every report keeps `containmentQualified: false`; its scope is `trusted-qualification-fixture`. `drainVerified` only describes the owned domain and reaped leader, not an audit of arbitrary code's filesystem/network authority or past migration. The fixture launcher is not connected to product PDF/provider entrypoints.

Before admitting hostile document input or provider runtime execution, further work must establish private namespace/filesystem boundaries that prevent access to delegation controls and unrelated host data, supported privilege separation, appropriate network/tool permissions and hostile-input behavior. A private cgroup namespace alone is not a full filesystem sandbox. Supervisor-crash recovery/ownership reconciliation is also absent: normal cancellation cleanup does not establish crash cleanup. No security demand can bypass these gaps by selecting the fixture purpose through a renderer or model proposal.

The initial native target remains Ubuntu 24.04 LTS x64. Required kernel interfaces include `openat2`, cgroup-v2 `clone3` admission, recursive kill, memory peak/swap/OOM, PID and CPU controllers. Feature/read-back checks decide availability; merely identifying Ubuntu or a kernel version cannot qualify it. No runtime, service, privileged broker or machine was installed by this package.

## Verification

On the current macOS host, **11 top-level lifecycle/contract tests and 4 subcases pass with the Go race detector**. One explicit Linux qualification marker is skipped as pending. Independent fixture assertions check that rejected requests never launch, failed admission is not success, a successful parent cannot leave descendants, cancel/wall limits drain, unreadable/live domains are never removed, failed kill cannot pass, usage failures remain failures, cleanup can retry, and Close cannot wait indefinitely. The Linux x64 test executable compiles offline; compilation is not execution evidence.

The Linux test file includes these native scenarios, all still pending here:

| Scenario | Independent expected outcome |
| --- | --- |
| Invalid cgroup FD / ordinary filesystem | No user-code marker, no ordinary-directory mutation, no fallback launch. |
| Child, grandchild and `setsid` child | Every recorded membership remains in the owned domain; cancellation leaves no live fixture. |
| Successful parent with orphan | The detached child is killed before success returns. |
| PID/thread pressure | Kernel PID-limit event increments; all survivors drain. |
| Aggregate memory pressure | Two individually sub-limit allocations trigger the domain OOM counter; execution fails and drains. |
| CPU pressure | Kernel throttling increments under the configured quota; cleanup succeeds. |
| Same-UID migration attempt | Result is recorded honestly; a second owned domain catches any intentionally moved fixture, and provider/document requests still refuse. |

Run the existing host checks with the pinned toolchain and offline cache:

```sh
go test -count=1 -race -json ./internal/processdomain
```

For native execution, an operator must first supply a dedicated, empty, owned cgroup-v2 delegation with CPU/memory/PID controllers enabled and enough parent budget. The suite does not provision or discover it. In that Linux environment, use the pinned Go toolchain and retain a new JSON output path:

```sh
TROVE_LINUX_DOMAIN_TEST_ROOT=<explicit-delegation> \
  go test -count=1 -json ./internal/processdomain \
  -run 'TestLinux(Native|Atomic|Rejects)' > <new-native-evidence.jsonl>
```

Absent native host/delegation yields explicit skips, not passes. Once a delegation is explicitly selected, missing controllers, permissions and unsupported syscalls fail the native cases rather than silently skipping. Preserve kernel/OS/architecture, source/test binary identity, selected delegation policy, raw test outcomes and post-run domain cleanup evidence with the release candidate. Migration denial on one configured machine does not prove that this package supplies a provider sandbox.

The separate existing PDF refusal regressions also remain part of the package's evidence. Actual Linux enforcement, private isolation, WebKitGTK document execution, provider containment, crash recovery and candidate qualification remain open.
