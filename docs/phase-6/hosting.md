# P2 headless hosting and owner transfer

`cmd/trove-host` wraps the production workspace kernel in `net/http`, serves the compiled desktop UI through a browser bridge, accepts bounded browser file uploads and serves authenticated workspace attachments. Editing, database operations, import, retrieval, jobs and scheduled local reviews use the same services and SQLite workspace. The desktop and host cannot own the same directory concurrently.

Build and run on the qualified macOS arm64 toolchain:

```sh
GOCACHE=/private/tmp/trove-phase0-tools/go-cache GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod /private/tmp/trove-phase0-tools/go/bin/go build -tags 'fts5 sqlite_omit_load_extension' -o build/trove-host ./cmd/trove-host
build/trove-host --init-token --token-file /private/tmp/trove-owner-token
build/trove-host --workspace /path/to/workspace --token-file /private/tmp/trove-owner-token --assets frontend/dist
```

Use `--create` for a new workspace. The token file is generated privately and is never copied into a snapshot. Open `http://127.0.0.1:7337` and enter that token. Sessions expire after 12 hours and use HttpOnly/SameSite cookies. Requests with a different Origin are denied; API clients may use `Authorization: Bearer <token>`. The listener is loopback by default. LAN/remote listeners require `--tls-cert` and `--tls-key`; plain remote HTTP is refused. Operate TLS directly; an unconfigured proxy is not implicitly trusted.

`--native-bundle` and `--models` attach existing verified local helpers and installed model packs to the worker queue. OCR uses the independently verified optional image helper. Missing packs do not affect ordinary editing. Remote browser capture uses the browser's file picker/upload; the host exposes no remote microphone, arbitrary filesystem picker or provider approval capability. Vendor providers and native desktop workspace switching remain desktop-only. General interactive model generation uses the desktop adapter; hosted background worker operations use explicitly configured installed packs.

## Moving ownership

Stop the current owner, then run:

```sh
build/trove-host --workspace /path/to/old-workspace --transfer-to /path/to/new-workspace
```

The command acquires the source's exclusive kernel lock, creates a consistent full snapshot, verifies hashes and database integrity, restores into private staging, durably fences the old directory with `OWNER_TRANSFERRED`, and then publishes the new directory. Open the new path from desktop or host. The destination receipt records workspace identity, snapshot hash, event cursor and transfer provenance. The old directory remains preserved and refuses to open.

Before the ownership fence, a failed operation leaves the source usable. After the fence, a publication failure keeps verified staging and an `INCOMPLETE` destination rather than making two paths writable. The source receipt records the staging location for manual recovery. Do not delete the fence or run an independent restored copy as another active owner. This is a controlled mode transfer, not a distributed lease, replica merger or offline synchronization protocol. Network filesystems with unreliable OS locks are not qualified workspace storage.

The host package does not change signing, clean-machine, Linux/Windows or remote deployment qualifications. The source-level tests verify authentication, origin protection, cookie policy, TLS requirements, canonical HTTP writes, upload boundaries, lock exclusion, transfer persistence and replay rejection.
