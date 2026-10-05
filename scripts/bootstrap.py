#!/usr/bin/env python3
"""Prepare pinned development dependencies; never run or write qualification.

Offline by default. --fetch permits dependency downloads. Model weights and
browser binaries are opt-in; no global Python, Go or Node install is modified.
"""
import argparse
import json
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
from urllib.request import urlopen
import venv

from devtools import ROOT, baseline, build_env, cache_dir, file_digest, go_binary, python_env


def archive(cache, name, pin, fetch, seeds=()):
    destination = cache / (name + '.tar.gz')
    if destination.is_file() and file_digest(destination) == pin['sha256']:
        return destination
    # A seed is only trusted after checking the committed baseline digest.
    for seed in seeds:
        for candidate in (seed / destination.name, seed / Path(pin['url']).name):
            if candidate.is_file() and file_digest(candidate) == pin['sha256']:
                shutil.copyfile(candidate, destination)
                return destination
    if not fetch:
        raise RuntimeError(f'{name}: pinned archive is absent or has a different hash. Use --fetch or --seed-cache.')
    with tempfile.NamedTemporaryFile(dir=cache, prefix=name + '-', suffix='.download', delete=False) as out:
        pending = Path(out.name)
        try:
            with urlopen(pin['url'], timeout=120) as response:
                shutil.copyfileobj(response, out)
            out.flush()
            if file_digest(pending) != pin['sha256']:
                raise RuntimeError(name + ': downloaded archive SHA256 differs from baseline')
            pending.replace(destination)
        finally:
            pending.unlink(missing_ok=True)
    return destination


def extract(path, cache, directory):
    destination = cache / directory
    if destination.exists() or destination.is_symlink():
        if destination.is_symlink() or not destination.is_dir():
            raise RuntimeError('Refusing unsafe source destination: ' + str(destination))
        return  # Existing source changes are checked by native qualification/build.
    with tempfile.TemporaryDirectory(dir=cache, prefix='extract-') as temporary:
        stage = Path(temporary)
        with tarfile.open(path) as bundle:
            for member in bundle.getmembers():
                target = (stage / member.name).resolve()
                if stage not in target.parents or member.issym() or member.islnk() or not (member.isfile() or member.isdir()):
                    raise RuntimeError('Unsafe archive member: ' + member.name)
                if Path(member.name).parts[0] != directory:
                    raise RuntimeError('Unexpected archive root: ' + member.name)
            bundle.extractall(stage, filter='data')
        (stage / directory).rename(destination)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', '--cache-dir', type=Path, default=cache_dir())
    parser.add_argument('--print-venv', action='store_true', help='print the selected Python environment path without changing anything')
    parser.add_argument('--fetch', action='store_true', help='allow downloads of committed dependency pins')
    parser.add_argument('--seed-cache', type=Path, action='append', default=[], help='reuse hash-verified archives from an existing cache')
    parser.add_argument('--components', nargs='+', choices=('python', 'go', 'frontend', 'native'),
                        default=['python', 'go', 'frontend', 'native'])
    parser.add_argument('--browser', action='store_true', help='also install the pinned Playwright Chromium binary (requires --fetch)')
    args = parser.parse_args()
    if args.print_venv:
        print(python_env(args.cache))
        return 0
    if sys.version_info < (3, 12):
        parser.error('Run bootstrap using Python 3.12 or newer; the existing .venv is never changed.')
    if args.browser and not args.fetch:
        parser.error('--browser requires --fetch because Playwright may download Chromium')
    if 'go' in args.components and (platform.system(), platform.machine()) != ('Darwin', 'arm64'):
        parser.error('The committed Go toolchain archive targets Darwin arm64; install the pinned Go version independently on Windows/Linux. Native source archives are portable.')
    cache = cache_dir(args.cache)
    cache.mkdir(parents=True, exist_ok=True)
    env = build_env(cache, offline=not args.fetch)
    lock = baseline()

    def run(command, cwd=ROOT):
        print('Run: ' + ' '.join(map(str, command)), flush=True)
        subprocess.run(list(map(str, command)), cwd=cwd, env=env, check=True)

    if 'python' in args.components:
        location = python_env(cache)
        interpreter = location / ('Scripts/python.exe' if sys.platform == 'win32' else 'bin/python')
        if not interpreter.is_file():
            venv.EnvBuilder(with_pip=True).create(location)
        wheels = cache / 'python-wheels'
        wheels.mkdir(exist_ok=True)
        if args.fetch:
            run([interpreter, '-m', 'pip', 'download', '--only-binary=:all:', '--dest', wheels, '-r', ROOT / 'requirements-dev.txt'])
        run([interpreter, '-m', 'pip', 'install', '--no-index', '--find-links', wheels,
             '--require-virtualenv', '-r', ROOT / 'requirements-dev.txt'])
        print('Activate: source ' + str(location / 'bin/activate'), flush=True)
    if 'go' in args.components:
        source = archive(cache, 'go', lock['go'], args.fetch, args.seed_cache)
        extract(source, cache, 'go')
        go = go_binary(cache)
        for directory in (ROOT, ROOT / 'baseline/go'):
            run([go, 'mod', 'download'], directory)
            run([go, 'mod', 'verify'], directory)
    if 'frontend' in args.components:
        for tool in ('node', 'pnpm'):
            actual = subprocess.check_output([tool, '--version'], text=True).strip().removeprefix('v')
            if actual != lock['toolchain'][tool]:
                raise RuntimeError(tool + ' differs from the committed toolchain: ' + actual)
        for directory in (ROOT / 'frontend', ROOT / 'baseline/frontend'):
            modules = directory / 'node_modules'
            if modules.is_symlink():
                if modules.exists():
                    raise RuntimeError('Use a per-worktree dependency install; refusing to modify live symlink ' + str(modules))
                modules.unlink()  # Only the dangling link is removed, never its target.
            command = ['pnpm', 'install', '--frozen-lockfile', '--ignore-scripts', '--store-dir', cache / 'pnpm-store']
            if not args.fetch:
                command.append('--offline')
            run(command, directory)
    if 'native' in args.components:
        for name, pin in lock['workers'].items():
            source = archive(cache, name, pin, args.fetch, args.seed_cache)
            extract(source, cache, pin['sourceDirectory'])
    if args.browser:
        run(['node', ROOT / 'frontend/node_modules/@playwright/test/cli.js', 'install', 'chromium'])
    print(json.dumps({'scope': 'development setup only', 'cache': str(cache),
                      'pythonEnvironment': str(python_env(cache)), 'components': args.components,
                      'browserRequested': args.browser, 'modelDownloads': False,
                      'qualificationEvidenceWritten': False}, indent=2))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        print('Bootstrap failed: ' + str(exc), file=sys.stderr)
        raise SystemExit(1)
