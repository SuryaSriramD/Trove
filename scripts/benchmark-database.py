#!/usr/bin/env python3
"""Bounded 10k typed-database service benchmark, with host/source/raw evidence."""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile
import time
from devtools import build_env, cache_dir, go_binary

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs():
    paths = [ROOT / name for name in ["go.mod", "go.sum", "baseline/baseline.lock.json", "scripts/benchmark-database.py", "scripts/devtools.py", "internal/workspace/database_benchmark_test.go", "internal/workspace/database_benchmark_fixture_test.go", "internal/workspace/database_test.go", "internal/workspace/kernel_test.go", "internal/workspace/advanced_database_test.go"]]
    for directory in ["internal", "contracts", "schemas"]:
        paths.extend(p for p in (ROOT / directory).rglob("*") if p.is_file() and not p.name.startswith("._") and p.suffix in {".go", ".c", ".h", ".sql", ".json"} and not p.name.endswith("_test.go"))
    return {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(paths))}


def command(args):
    try:
        result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=15)
        return {"command": args, "exitCode": result.returncode, "stdout": result.stdout.strip(), "stderr": result.stderr.strip()}
    except (OSError, subprocess.TimeoutExpired) as exc:
        return {"command": args, "exitCode": None, "error": str(exc)}


def retention_destination(path):
    if path is None:
        return None
    if not path.is_absolute():
        raise ValueError("retained fixture destination must be absolute")
    # Resolve only the existing parent, preserving dangling/existing final links
    # for exclusive creation/refusal by the fixture writer.
    destination = path.parent.resolve(strict=True) / path.name
    if os.path.lexists(destination):
        raise ValueError("retained fixture destination already exists")
    return destination


def retention_complete(path, result):
    if path is None:
        return True
    retained = result.get("retainedFixture", {})
    return (retained.get("ready") is True and retained.get("outsideTimedSamples") is True
            and retained.get("workspacePath") == str(path) and path.is_dir() and not path.is_symlink()
            and (path / "workspace.db").is_file() and not (path / "workspace.db").is_symlink())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--iterations", type=int, default=7)
    parser.add_argument("--cache", type=Path, default=cache_dir())
    parser.add_argument("--output", type=Path, default=ROOT / "evidence/completion/database-10k")
    parser.add_argument("--retain-fixture", type=Path, help="New private destination for the same closed 10k workspace; verified/reopened after all timed samples")
    args = parser.parse_args()
    if not 3 <= args.iterations <= 30:
        parser.error("iterations must be 3..30")
    try:
        retained_path = retention_destination(args.retain_fixture)
    except (OSError, ValueError) as error:
        parser.error(str(error))
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    go = go_binary(args.cache)
    env = build_env(args.cache)
    env.pop("TROVE_DATABASE_BENCHMARK_RETAIN", None)
    if retained_path is not None:
        env["TROVE_DATABASE_BENCHMARK_RETAIN"] = str(retained_path)
    env.update(TROVE_DATABASE_BENCHMARK_OUTPUT=str(output / "results.json"), TROVE_DATABASE_BENCHMARK_ITERATIONS=str(args.iterations))
    host = {"platform": platform.platform(), "machine": platform.machine(), "logicalCPUs": os.cpu_count(), "goVersion": command([go, "version"])}
    if platform.system() == "Darwin":
        host.update(hardware=command(["/usr/sbin/sysctl", "-n", "hw.model", "hw.memsize", "hw.ncpu", "machdep.cpu.brand_string"]), osVersion=command(["/usr/bin/sw_vers"]), power=command(["/usr/bin/pmset", "-g", "batt"]))
    revision = command(["git", "rev-parse", "HEAD"])
    dirty = command(["git", "status", "--short", "--untracked-files=no"])
    before = inputs()
    run = [go, "test", "-mod=readonly", "-tags", "fts5 sqlite_omit_load_extension", "./internal/workspace", "-run", "^TestDatabasePerformance10K$", "-count=1", "-timeout=6m", "-json"]
    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix="trove-database-10k-", dir="/private/tmp" if platform.system() == "Darwin" else None) as scratch:
        env["TMPDIR"] = scratch
        host["workspaceStorage"] = command(["/bin/df", "-k", scratch])
        host["workspaceStorageNote"] = "Disposable workspace on this filesystem; no cache eviction or filesystem cold-start claim."
        with (output / "go-test.jsonl").open("w") as log:
            try:
                process = subprocess.run(run, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=420)
                code = process.returncode
            except subprocess.TimeoutExpired:
                code = 124
    after = inputs()
    changed = sorted(path for path in before.keys() | after.keys() if before.get(path) != after.get(path))
    result_path = output / "results.json"
    result = json.loads(result_path.read_text()) if result_path.exists() else {}
    retained_complete = retention_complete(retained_path, result)
    report = {"schemaVersion": 1, "recordedAt": datetime.now(timezone.utc).isoformat(), "host": host, "sourceRevision": revision, "sourceWorktreeStatusAtStart": dirty, "sourceBeforeSha256": before, "sourceChangedDuringRun": changed, "command": run, "iterations": args.iterations, "exitCode": code, "wallSecondsIncludingCompile": time.monotonic() - started, "serviceResultSha256": digest(result_path) if result_path.exists() else None, "logSha256": digest(output / "go-test.jsonl"), "localMeasurementComplete": code == 0 and result.get("completed") is True and not changed, "releaseQualified": False, "limitations": ["Developer Go service/in-process ProviderBridge boundary only; no Wails IPC, TLS/network, native webview, DOM or paint measurement.", "Synthetic deterministic corpus. Setup inserts canonical rows directly after service-created schema; ingestion/mutation/event cost is excluded.", "Warm local storage and OS caches; no disk-cache flushing, memory-pressure or concurrent recording/indexing/AI load.", "Go allocation deltas and end heap/system counters exclude SQLite C memory and process RSS. Small bounded samples are diagnostic, not a release percentile guarantee.", "Provider pairing remains limited to 100 selected pages: its profile uses the collection root plus 99 record pages within the 10,000-record collection.", "This profile does not qualify shell/search/AI/import latency or overall REL-007."]}
    report["retentionRequested"] = retained_path is not None
    report["retentionComplete"] = retained_complete if retained_path is not None else None
    report["localMeasurementComplete"] = report["localMeasurementComplete"] and retained_complete
    if retained_path is not None:
        report["limitations"].append("Fixture retention closes the source, copies exact closed bytes, and verifies native-reopenable first pages after every timed sample; it does not measure native rendering. Retention remains part of the existing overall timeout.")
    (output / "environment.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({key: report[key] for key in ["localMeasurementComplete", "releaseQualified", "exitCode", "sourceChangedDuringRun", "wallSecondsIncludingCompile"]}, indent=2))
    for case in result.get("cases", []):
        print(f"{case['name']}: rows={case['expectedRows']} first p50={case['firstPage']['p50Ms']:.2f}ms p95={case['firstPage']['p95Ms']:.2f}ms")
    if code:
        print((output / "go-test.jsonl").read_text()[-6000:])
    return 0 if report["localMeasurementComplete"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
