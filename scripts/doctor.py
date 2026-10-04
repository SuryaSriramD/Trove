#!/usr/bin/env python3
"""Inspect local build prerequisites without installing or writing evidence."""
import argparse
import importlib.metadata
import json
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys

from devtools import ROOT, baseline, build_env, cache_dir, go_binary, models_dir, python_env


def inspect(cache):
    lock = baseline()
    checks = []

    def check(name, passed, detail, required=True):
        checks.append(dict(name=name, passed=bool(passed), required=required, detail=str(detail)))

    check('python', sys.version_info >= (3, 12), sys.version.split()[0] + ' at ' + sys.executable)
    check('native-target', (platform.system(), platform.machine()) == ('Darwin', 'arm64'),
          platform.system() + '/' + platform.machine() + '; baseline supports macOS arm64')
    for line in (ROOT / 'requirements-dev.txt').read_text().splitlines():
        if not line.strip() or line.startswith('#'):
            continue
        name, expected = line.split('==', 1)
        try:
            actual = importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:
            actual = 'missing'
        check('python:' + name, actual == expected, actual + '; expected ' + expected)
    for tool in ('node', 'pnpm', 'cmake', 'clang'):
        try:
            command = shutil.which(tool)
            if not command:
                raise RuntimeError('not on PATH')
            result = subprocess.run([command, '--version'], text=True, capture_output=True, timeout=15)
            output = result.stdout.strip().splitlines()[0] if result.stdout.strip() else result.stderr.strip()
            version = re.search(r'\d+\.\d+\.\d+', output)
            check(tool, result.returncode == 0 and version and version[0] == lock['toolchain'][tool], output)
        except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
            check(tool, False, exc)
    try:
        sdk = subprocess.check_output(['xcrun', '--show-sdk-version'], text=True, stderr=subprocess.PIPE, timeout=15).strip()
        check('macOS SDK', sdk == lock['toolchain']['sdk'].removeprefix('macOS '), sdk)
    except (OSError, subprocess.SubprocessError) as exc:
        check('macOS SDK', False, exc)
    try:
        check('go', True, go_binary(cache))
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        check('go', False, exc)
    for directory in ('frontend', 'baseline/frontend'):
        modules = ROOT / directory / 'node_modules'
        detail = str(modules)
        if modules.is_symlink() and not modules.exists():
            detail += ' (dangling symlink)'
        check(directory + ' dependencies', (modules / 'typescript/bin/tsc').is_file(), detail)
    for name, pin in lock['workers'].items():
        source = cache / pin['sourceDirectory']
        check(name + ' source', source.is_dir() and (cache / (name + '.tar.gz')).is_file(),
              str(source) + '; build-native verifies the pinned archive and source hashes')
    env = build_env(cache)
    modules = Path(env['GOMODCACHE'])
    check('Go module cache', (modules / 'cache/download').is_dir(),
          str(modules) + '; bootstrap verifies modules against go.sum')
    browsers = Path(env['PLAYWRIGHT_BROWSERS_PATH'])
    check('browser binaries', browsers.is_dir() and any(browsers.glob('chromium*')),
          str(browsers) + '; install explicitly with bootstrap --browser', required=False)
    check('native helpers', (ROOT / 'build/native/manifest.json').is_file(),
          'Run build-native.py after bootstrap', required=False)
    packs = models_dir(cache=cache)
    check('qualification models', packs.is_dir() and any(packs.glob('*.model')),
          str(packs) + '; acquire-test-models.py is explicit and verifies pack hashes', required=False)
    return {'schemaVersion': 1, 'scope': 'developer prerequisites, not release qualification',
            'cache': str(cache), 'pythonEnvironment': str(python_env(cache)),
            'goModuleCache': env['GOMODCACHE'], 'goBuildCache': env['GOCACHE'],
            'ready': all(c['passed'] for c in checks if c['required']), 'checks': checks}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', '--cache-dir', type=Path, default=cache_dir())
    parser.add_argument('--json', action='store_true')
    args = parser.parse_args()
    report = inspect(cache_dir(args.cache))
    if args.json:
        print(json.dumps(report, indent=2))
    else:
        print('Cache: ' + report['cache'])
        for check in report['checks']:
            status = 'OK' if check['passed'] else 'MISSING' if check['required'] else 'OPTIONAL'
            print(f"{status:8} {check['name']}: {check['detail']}")
        print('Ready for development' if report['ready'] else 'Run bootstrap.py with Python 3.12+; use --fetch to allow dependency downloads.')
    return 0 if report['ready'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
