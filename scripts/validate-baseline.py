#!/usr/bin/env python3
"""Build and run the locked Phase 0 probes; never imply release qualification.

Use --fetch once to download verified tools/sources. Caches use the persistent developer cache, never a global toolchain install.
Setup without qualification is scripts/bootstrap.py. Default runs are offline.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import urllib.request

from devtools import build_env, cache_dir, workspace_cache

ROOT = Path(__file__).resolve().parents[1]
LOCK = json.loads((ROOT / "baseline/baseline.lock.json").read_text())
EVIDENCE = ROOT / "evidence/baseline.json"
PENDING = [
    "Signed/notarized Wails installer and bundled worker launch on a clean Mac without developer tools",
    "Model manifests, license review, hash validation, CPU/Metal chat and embedding inference, timestamped transcription",
    "Native permissions, capture, clipboard, deep links, lifecycle, restart and restore",
    "Supported minimum macOS version, memory/thermal budgets, cancellation and worker crash containment",
    "sqlite-vec newer-runtime upgrade/delete stress qualification before P0 product release",
    "Windows and Linux independent qualification",
]


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def inputs():
    paths = [ROOT / "scripts/validate-baseline.py", ROOT / "scripts/devtools.py"]
    paths += [p for p in (ROOT / "baseline").rglob("*") if p.is_file()
              and not p.name.startswith("._") and not any(x in p.parts for x in ("node_modules", "dist", "build"))]
    return {str(p.relative_to(ROOT)): digest(p) for p in sorted(paths)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fetch", action="store_true", help="allow verified downloads and package installation")
    parser.add_argument("--cache-dir", type=Path, default=cache_dir())
    parser.add_argument("--check-evidence", action="store_true", help="check recorded Phase 0 pass and input hashes; do not claim a fresh host run")
    parser.add_argument("--release", action="store_true", help="also enforce product release gates (currently unverified)")
    args = parser.parse_args()
    if args.check_evidence:
        recorded = json.loads(EVIDENCE.read_text())
        logs_valid = all((ROOT / check["log"]).is_file() and digest(ROOT / check["log"]) == check["logSha256"]
                         for check in recorded["checks"] if "log" in check)
        valid = recorded["phase0Status"] == "verified" and recorded["inputSha256"] == inputs() and logs_valid
        print("Recorded Phase 0 evidence: " + ("current" if valid else "missing, stale or failed"))
        return 0 if valid and not args.release else 1

    cache = args.cache_dir.resolve()
    cache.mkdir(parents=True, exist_ok=True)
    logdir = ROOT / "evidence/baseline-logs"
    logdir.mkdir(parents=True, exist_ok=True)
    checks = []
    env = build_env(cache, offline=not args.fetch)
    env.update({"CGO_ENABLED": "1", "GOFLAGS": ""})
    if not args.fetch:
        env["GOPROXY"] = "off"

    def run(name, command, cwd=ROOT, timeout=600, extra_env=None):
        target_env = dict(env)
        target_env.update(extra_env or {})
        proc = subprocess.run([str(x) for x in command], cwd=cwd, env=target_env,
                              stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        log = logdir / (name + ".log")
        log.write_text(proc.stdout)
        checks.append({"id": name, "status": "verified" if proc.returncode == 0 else "failed",
                       "command": [str(x) for x in command], "exitCode": proc.returncode,
                       "log": str(log.relative_to(ROOT)), "logSha256": digest(log)})
        if proc.returncode:
            raise RuntimeError(name + " failed; see " + str(log.relative_to(ROOT)))
        print(name + ": verified", flush=True)
        return proc.stdout

    def archive(name, pin):
        path = cache / (name + ".tar.gz")
        if not path.exists() or digest(path) != pin["sha256"]:
            if not args.fetch:
                raise RuntimeError(name + " pinned archive unavailable; rerun with --fetch")
            download = path.with_suffix(".download")
            with urllib.request.urlopen(pin["url"], timeout=120) as response, download.open("wb") as out:
                shutil.copyfileobj(response, out)
            if digest(download) != pin["sha256"]:
                raise RuntimeError(name + " SHA-256 mismatch")
            download.replace(path)
        checks.append({"id": name + "-source-integrity", "status": "verified", "sha256": digest(path), "source": pin["url"]})
        return path

    def extract(path):
        with tarfile.open(path) as bundle:
            for member in bundle.getmembers():
                destination = (cache / member.name).resolve()
                if cache not in destination.parents:
                    raise RuntimeError("unsafe archive path")
                if member.issym() or member.islnk():
                    raise RuntimeError("archive links are not accepted")
            bundle.extractall(cache)

    def verify_native_source(path, source, name):
        expected = set()
        with tarfile.open(path) as bundle:
            for member in bundle.getmembers():
                if not member.isfile():
                    continue
                target = cache / member.name
                expected.add(target)
                stream = bundle.extractfile(member)
                archive_hash = hashlib.sha256(stream.read()).hexdigest()
                if not target.is_file() or target.is_symlink() or digest(target) != archive_hash:
                    raise RuntimeError(name + " extracted source differs from pinned archive: " + member.name)
        actual = {p for p in source.rglob("*") if p.is_file() and not p.name.startswith("._")}
        # Upstream whisper CMakeLists.txt:47 writes this metadata into its source
        # tree on configuration. It is not an input to the native library build.
        if name == "whisper":
            actual.discard(source / "bindings/javascript/package.json")
        if actual != expected:
            raise RuntimeError(name + " extracted source has unexpected files; use a fresh --cache-dir")
        checks.append({"id": name + "-extracted-source-integrity", "status": "verified", "fileCount": len(expected)})

    failure = None
    try:
        gomod_text = (ROOT / "baseline/go/go.mod").read_text()
        for pin in LOCK["goModules"].values():
            if not re.search(r"^\s*" + re.escape(pin["module"]) + r"\s+" + re.escape(pin["version"]) + r"\s*$", gomod_text, re.M):
                raise RuntimeError("Go module pin differs from baseline lock: " + pin["module"])
        package = json.loads((ROOT / "baseline/frontend/package.json").read_text())
        versions = dict(package["dependencies"], **package["devDependencies"])
        mappings = {"react": "react", "react-dom": "reactDom", "typescript": "typescript", "vite": "vite",
                    "@tiptap/core": "tiptap", "@tiptap/pm": "tiptap", "@tiptap/react": "tiptap", "@tiptap/starter-kit": "tiptap"}
        for package_name, key in mappings.items():
            if versions[package_name] != LOCK["frontend"][key]:
                raise RuntimeError("Frontend pin differs from baseline lock: " + package_name)
        checks.append({"id": "manifest-consistency", "status": "verified"})
        if platform.system() != "Darwin" or platform.machine() != "arm64":
            raise RuntimeError("Phase 0 qualification requires native Darwin arm64")
        checks.append({"id": "host-architecture", "status": "verified", "os": platform.system(), "arch": platform.machine()})
        run("host-version", ["sw_vers"])
        for tool in ("node", "pnpm", "cmake"):
            actual = run(tool + "-version", [tool, "--version"]).strip()
            expected = LOCK["toolchain"][tool]
            if expected not in actual:
                raise RuntimeError(tool + " version differs from lock: " + actual)
        run("clang-version", ["clang", "--version"])
        run("sdk", ["xcrun", "--show-sdk-version"])
        go = cache / "go/bin/go"
        goarchive = archive("go", LOCK["go"])
        if not go.exists():
            extract(goarchive)
        env["PATH"] = str(go.parent) + os.pathsep + env["PATH"]
        if "go" + LOCK["go"]["version"] + " darwin/arm64" not in run("go-version", [go, "version"]):
            raise RuntimeError("Go toolchain mismatch")
        gomod = ROOT / "baseline/go"
        run("go-modules", [go, "mod", "verify"], gomod)
        storage = cache / "storage-probe"
        run("storage-build", [go, "test", "-mod=readonly", "-c", "-tags", "fts5 sqlite_omit_load_extension", "-o", storage, "."], gomod)
        run("storage", [storage, "-test.v"])
        deps = run("storage-linked-libraries", ["otool", "-L", storage])
        if "libsqlite3" in deps:
            raise RuntimeError("storage linked system SQLite instead of bundled amalgamation")
        shell = cache / "wails-probe"
        run("wails-build", [go, "build", "-mod=readonly", "-tags", "desktop production fts5 sqlite_omit_load_extension", "-o", shell, "./shell"], gomod,
            extra_env={"CGO_LDFLAGS": "-framework UniformTypeIdentifiers"})
        if "arm64" not in run("wails-architecture", ["file", shell]):
            raise RuntimeError("Wails binary is not arm64")
        run("wails-linked-libraries", ["otool", "-L", shell])
        # Temp staging avoids installing thousands of files on a removable volume.
        frontend = cache / "frontend"
        shutil.copytree(ROOT / "baseline/frontend", frontend, dirs_exist_ok=True,
                        ignore=shutil.ignore_patterns("node_modules", "dist", "._*"))
        install = ["pnpm", "install", "--frozen-lockfile", "--ignore-scripts", "--store-dir", cache / "pnpm-store"]
        if not args.fetch:
            install.append("--offline")
        run("frontend-install", install, frontend)
        run("frontend-build", ["pnpm", "build"], frontend)
        for name, pin in LOCK["workers"].items():
            tar = archive(name, pin)
            source = cache / pin["sourceDirectory"]
            if not source.exists():
                extract(tar)
            verify_native_source(tar, source, name)
            build = workspace_cache(cache) / (name + "-baseline-build")
            options = ["-DCMAKE_BUILD_TYPE=Release", "-DCMAKE_OSX_ARCHITECTURES=arm64", "-DBUILD_SHARED_LIBS=OFF",
                       "-DGGML_METAL=ON", "-DGGML_METAL_EMBED_LIBRARY=ON"]
            if name == "llama":
                options += ["-DLLAMA_BUILD_IS_DEV=OFF", "-DLLAMA_BUILD_COMMON=OFF", "-DLLAMA_BUILD_EXAMPLES=OFF", "-DLLAMA_BUILD_TESTS=OFF",
                            "-DLLAMA_BUILD_TOOLS=OFF", "-DLLAMA_BUILD_SERVER=OFF", "-DLLAMA_BUILD_APP=OFF", "-DLLAMA_OPENSSL=OFF"]
            else:
                options += ["-DWHISPER_BUILD_IS_DEV=OFF", "-DWHISPER_BUILD_EXAMPLES=OFF", "-DWHISPER_BUILD_TESTS=OFF", "-DWHISPER_BUILD_SERVER=OFF", "-DWHISPER_CURL=OFF"]
            run(name + "-configure", ["cmake", "-S", source, "-B", build] + options)
            run(name + "-build", ["cmake", "--build", build, "--parallel", "4"], timeout=1800)
            libraries = [build / "src" / ("lib" + name + ".a")] + sorted((build / "ggml/src").rglob("*.a"))
            binary = cache / (name + "-probe")
            run(name + "-link", ["clang++", "-std=c++17", "-arch", "arm64", "-I", source / "include", "-I", source / "ggml/include",
                ROOT / "baseline/native" / (name + "-probe.cpp")] + libraries + ["-framework", "Accelerate", "-framework", "Metal", "-framework", "Foundation", "-o", binary])
            output = run(name + "-runtime", [binary])
            if "CPU :" not in output or "EMBED_LIBRARY = 1" not in output:
                raise RuntimeError(name + " did not expose embedded Metal and CPU capability")
            if "arm64" not in run(name + "-architecture", ["file", binary]):
                raise RuntimeError(name + " binary is not arm64")
            deps = run(name + "-linked-libraries", ["otool", "-L", binary])
            if "/opt/homebrew" in deps or any(".dylib" in line and "/usr/lib/" not in line for line in deps.splitlines()[1:]):
                raise RuntimeError(name + " depends on an unbundled non-system dylib")
    except Exception as err:
        failure = str(err)
        print("baseline: " + failure, file=sys.stderr)
    evidence = {"schemaVersion": 1, "recordedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "phase0Status": "failed" if failure else "verified", "releaseStatus": "unverified",
                "scope": "Native arm64 compile, linkage and no-model runtime capability probes; storage features; frontend production build. Not a packaged product acceptance test.",
                "host": {"system": platform.system(), "machine": platform.machine(), "release": platform.release()},
                "checks": checks, "failure": failure, "releaseGates": [{"requirement": x, "status": "unverified"} for x in PENDING], "inputSha256": inputs()}
    EVIDENCE.parent.mkdir(exist_ok=True)
    EVIDENCE.write_text(json.dumps(evidence, indent=2) + "\n")
    print("Evidence: evidence/baseline.json", flush=True)
    if args.release:
        print("Release qualification remains unverified; see releaseGates in evidence.")
    return 1 if failure or args.release else 0


if __name__ == "__main__":
    sys.exit(main())
