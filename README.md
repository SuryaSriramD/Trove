# Trove

[Phase 6 optional P2 packages](docs/phase-6/implementation.md) add charts, local forms, scoped row buttons, a traceable knowledge graph, bounded local OCR, scheduled reviewed digests, conversation-export import, persistent page display settings and an authenticated headless host. The implementation backlog, stable agent ownership and Phase 0 contract extensions are documented there. Run `/private/tmp/trove-phase0-venv/bin/python scripts/verify-phase6.py --app build/phase6-preview-next/Trove.app` to verify and package a fresh development preview. [Hosting and verified owner transfer](docs/phase-6/hosting.md) remain single-owner operations, without offline multi-device synchronization. The [Phase 6 qualification record](docs/phase-6/qualification.md) records the actual checks and limits.

The [Phase 5 P1 personal-use qualification](docs/phase-5/release.md) packages advanced editing/databases, migration/capture, local automation, expanded reviewed AI and paired local MCP as Trove 0.5.0. Build and qualify a new candidate with `/private/tmp/trove-phase0-venv/bin/python scripts/verify-phase5.py --app build/phase5-candidate-4/Trove.app`. This reruns affected P0 workflows, native models and packaged P1 restart/restore under OS network denial, with evidence organized by V01–V10. Use a new path for every run. Managed Claude Code/Codex and remote MCP remain disabled; live-account and native UI gaps remain explicit. See the [release assessment](docs/phase-5/qualification.md) for the recorded result and remaining approval gates. The [Phase 4 preview](docs/phase-4/implementation.md) and P0 evidence below remain historical checkpoints.

Trove 0.3.0 is a P0 desktop release candidate for macOS 27 on Apple Silicon. It includes Notion/Markdown/CSV import, page and database editing, keyword and optional semantic search, local AI with reviewed changes and approved memory, recordings and transcription, and native backup/restore. It is built with Go, Wails 2, React/TypeScript, Tiptap and SQLite.

Start with the [P0 installation and user journey](docs/phase-3/release.md). The [qualification assessment](docs/phase-3/qualification.md) distinguishes automated results, packaged native UI observations, and remaining limitations. Exact [benchmark hardware, workspace profiles and timings](docs/phase-3/performance.md), plus [dependency/model hashes, licenses, migrations and rollback procedures](docs/phase-3/release-provenance.md), accompany the candidate.

The installed application requires no product account or separately installed Node, Python, Ollama, Docker, or database server. Editing, import, and keyword search work without weights. Install optional verified model packs explicitly in Settings for local AI, semantic retrieval, and English transcription. Those operations use installed local packs and have no silent cloud fallback. Model weights are separate from the application and workspace backups.

This candidate uses an ad-hoc signature. Developer ID signing/notarization and installation on an independent clean Mac are known distribution limits, not claims established by minimal-PATH execution on the development Mac. The release assessment records the exact artifact and the scope of its acceptance evidence.

## Build and qualify the P0 candidate

The following tools are needed only to build or test from source. Provision the pinned toolchain/cache described below, install `frontend/pnpm-lock.yaml` with `pnpm install --frozen-lockfile` in `frontend/`, and acquire the explicit qualification model packs. Then run:

```sh
python3 scripts/build-native.py
python3 scripts/acquire-test-models.py
python3 scripts/verify-phase3.py --app build/p0-qualification-01/Trove.app
```

Choose a new, unused application path for each qualification run; the runner refuses to overwrite an existing candidate. `verify-phase3.py` runs contracts, Go race tests, frontend type/unit/browser tests, and the offline provenance inventory; builds the self-contained application with its notices; denies network access for actual native inference and the packaged create/restart/restore journey; and verifies source and bundle stability. The packaged data journey uses an empty model cache, while the native step uses the explicitly installed packs. Individual results, hashes and logs are recorded under `evidence/phase3/runs/`, with the latest report at `evidence/phase3/latest-run.json`. Automated success does not replace the native UI/device observations in the qualification assessment.

Reproduce the service and packaged shell measurements separately, after the source and candidate are stable:

```sh
python3 scripts/benchmark-phase3.py
python3 scripts/benchmark-shell.py --app build/p0-qualification-01/Trove.app
```

The service benchmark preserves synthetic workspace profiles for the shell benchmark to clone. Review the raw profile timings and target results as well as command success; the performance document states each measurement's scope. `scripts/verify-packaged.py --p0 --native --offline --app <candidate.app> --models <installed-pack-directory>` can reproduce the packaged fixture independently. These qualification commands need the host permissions required to run Wails, native helpers, the browser, and the OS network-denial sandbox.

For an ordinary development build, use `python3 scripts/build-desktop.py` after building the native helpers. Frontend development uses `frontend/node_modules/.bin/vite --host 127.0.0.1`; canonical content requires the Wails application. TinyStories is a runtime test pack; the P0 user guide identifies the supported instruction, embedding and speech packs.

The first launch creates a workspace in the system application-support directory under `Trove/workspaces/default`. `TROVE_WORKSPACE` selects a development/test workspace. Settings opens another workspace, manages verified model packs, previews/restores page history, and creates consistent backups. Native backups include the database, content-addressed assets, recording journals, and a verified manifest; incomplete exports are clearly marked. Restore publishes into a new directory and preserves the original workspace and backup.

## Implementation and historical evidence

Phase 2 implementation details remain in [the integration handoff](docs/phase-2/integration.md), [migration/export](docs/phase-2/2a.md), [search/extraction](docs/phase-2/2b.md), [AI/memory](docs/phase-2/2c.md), [recordings/meetings](docs/phase-2/2d.md), and [recovery/restore](docs/phase-2/2e.md). Preserved Phase 1 and Phase 2 application checkpoints and their verification scripts are historical evidence; the current P0 gate is `verify-phase3.py`.

Start with [the workstream handoff](docs/phase-0/workstreams.md), [canonical identity](docs/phase-0/domain.md), [the three resolved decisions](docs/phase-0/decisions.md), [service and mutation contracts](docs/phase-0/services.md), [jobs and worker protocol](docs/phase-0/jobs.md), and [versioned registries](docs/phase-0/registries.md). Exact native/UI pins and Apple Silicon validation are in [the baseline](docs/phase-0/baseline.md).

The source of truth is `contracts/*.schema.json`, `contracts/registries.json` and the SQL contract migrations in `schemas/`. [Example payloads](contracts/examples/) include canonical workspace identity, shared tasks/views, registry values, re-import, requests/results, committed events, citations and worker messages. TypeScript and Go types are generated from those schemas. `contracts/service.go` exposes the typed, shell-independent Go interface. Python reference modules remain executable conformance oracles; production Go services live in `internal/workspace`.

## Verify

Use Python 3.12+, the pinned toolchain from the baseline and a local virtual environment. The virtual environment/cache can live outside this checkout; this is faster on removable volumes.

```sh
python3 -m venv /tmp/trove-phase0-venv
/tmp/trove-phase0-venv/bin/python -m pip install -r requirements-dev.txt
/tmp/trove-phase0-venv/bin/python scripts/extract_requirements.py
/tmp/trove-phase0-venv/bin/python scripts/validate-baseline.py --fetch
/tmp/trove-phase0-venv/bin/python scripts/verify_phase0.py
```

The baseline bootstrap downloads pinned dependencies into `/private/tmp/trove-phase0-tools`; it needs Node/pnpm/CMake/Xcode as documented. The root Go validator dependency is pinned by `go.mod` and `go.sum`; bootstrap it with `go mod download` using the same Go cache if absent. The complete command using that toolchain is:

```sh
PATH=/private/tmp/trove-phase0-tools/go/bin:$PATH \
GOCACHE=/private/tmp/trove-phase0-tools/go-cache \
GOMODCACHE=/private/tmp/trove-phase0-tools/go-mod go mod download
```

`verify_phase0.py` runs contract/domain/persistence/ledger tests, compiles Go and TypeScript consumers and verifies that recorded native evidence still matches source and log hashes. Add `--fresh-baseline` to rerun native probes. It writes [verification evidence](evidence/phase0-verification.json). Native evidence distinguishes compilation/capability probes from future installer and model-inference tests.

Regenerate after schema edits:

```sh
python3 scripts/build_protocol_schemas.py
python3 scripts/build_examples.py
python3 scripts/generate_types.py
```

`build_protocol_schemas.py` is the authoring source for service/jobs schemas; the domain schema and registry are edited directly. Never edit generated types. Contract changes require updated schemas, fixtures, semantic rules, migration/fallback decisions and evidence in the [requirements ledger](docs/phase-0/requirements-ledger.json). The ledger separates implemented from verified and Phase0 from P0/P1/P2 release completion.
