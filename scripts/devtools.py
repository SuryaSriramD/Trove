"""Shared, side-effect-free developer tool/cache configuration.

Importing this module never creates directories, downloads inputs or writes
qualification evidence. Explicit CLI cache values override TROVE_CACHE_DIR.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
from native_targets import host_target

ROOT = Path(__file__).resolve().parents[1]


def baseline():
    return json.loads((ROOT / 'baseline/baseline.lock.json').read_text())


def cache_dir(value=None, *, environ=None, system=None, home=None):
    env = os.environ if environ is None else environ
    if value is not None or env.get('TROVE_CACHE_DIR'):
        return Path(value if value is not None else env['TROVE_CACHE_DIR']).expanduser().resolve()
    home = Path.home() if home is None else Path(home)
    system = platform.system() if system is None else system
    if system == 'Darwin':
        return home / 'Library/Caches/Trove'
    if system == 'Windows':
        return Path(env.get('LOCALAPPDATA', home / 'AppData/Local')) / 'Trove/Cache'
    return Path(env.get('XDG_CACHE_HOME', home / '.cache')) / 'trove'


def models_dir(value=None, cache=None):
    return Path(value or os.environ.get('TROVE_MODELS_DIR') or cache_dir(cache) / 'models').expanduser().resolve()


def workspace_cache(cache=None, root=ROOT):
    """Keep CMake's absolute source paths separate across worktrees."""
    identity = hashlib.sha256(str(Path(root).resolve()).encode()).hexdigest()[:16]
    return cache_dir(cache) / 'workspaces' / identity


def python_env(cache=None):
    # Content-address requirements so upgrades do not mutate an older env.
    identity = hashlib.sha256((ROOT / 'requirements-dev.txt').read_bytes()).hexdigest()[:12]
    return cache_dir(cache) / 'python' / ('requirements-' + identity)


def go_archive_pin(target=None):
    target = target or host_target()
    pins = json.loads((ROOT / 'baseline/go-targets.lock.json').read_text())
    if pins['version'] != baseline()['go']['version'] or target not in pins['targets']:
        raise RuntimeError('No reviewed Go archive for target: ' + target)
    return pins['targets'][target]


def tool_command(tool, *args):
    """Invoke Windows package-manager shims without cmd.exe shell interpolation."""
    path = shutil.which(tool)
    if platform.system() != 'Windows' or not path or Path(path).suffix.lower() not in ('.cmd', '.bat'):
        return [path or tool, *map(str, args)]
    if tool == 'pnpm':
        for relative in ('node_modules/pnpm/bin/pnpm.cjs', 'node_modules/corepack/dist/pnpm.js'):
            entry = Path(path).parent / relative
            if entry.is_file():
                return [shutil.which('node') or 'node', str(entry), *map(str, args)]
    raise RuntimeError('Install a native executable or known Node entry point for ' + tool)


def go_binary(cache=None):
    """Prefer the selected pinned cache; reject incomplete or floating tools."""
    cached = cache_dir(cache) / 'go/bin' / ('go.exe' if os.name == 'nt' else 'go')
    candidate = str(cached) if cached.is_file() else shutil.which('go')
    if not candidate:
        raise RuntimeError('Pinned Go is missing; run scripts/bootstrap.py --fetch with this cache.')
    env = dict(os.environ, GOTOOLCHAIN='local')
    version = subprocess.check_output([candidate, 'version'], env=env, text=True).strip()
    if version != 'go version go' + baseline()['go']['version'] + ' ' + host_target().replace('-', '/'):
        raise RuntimeError('Go version differs from baseline: ' + version)
    goroot = Path(subprocess.check_output([candidate, 'env', 'GOROOT'], env=env, text=True).strip())
    if not (goroot / 'src/runtime/runtime.go').is_file():
        raise RuntimeError('Go installation lacks standard-library sources: ' + str(goroot))
    return candidate


def build_env(cache=None, *, source=None, offline=True):
    env = dict(os.environ if source is None else source)
    selected = cache_dir(cache)
    env['TROVE_CACHE_DIR'] = str(selected)
    env.setdefault('GOCACHE', str(selected / 'go-cache'))
    env.setdefault('GOMODCACHE', str(selected / 'go-mod'))
    env.setdefault('PLAYWRIGHT_BROWSERS_PATH', str(selected / 'playwright'))
    env['GOTOOLCHAIN'] = 'local'
    if offline:
        env['COREPACK_ENABLE_NETWORK'] = '0'
        env['GOPROXY'] = 'off'
        env['GOSUMDB'] = 'off'
    return env


def file_digest(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()
