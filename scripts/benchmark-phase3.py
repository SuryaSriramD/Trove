#!/usr/bin/env python3
"""Run reproducible P0 service benchmarks and bind timings to host/source data."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import tempfile

from devtools import build_env, cache_dir, go_binary, models_dir
from qualification_helpers import copy_helper_bundle, check_helper_bundle, validate_helper_copy

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def inputs():
    paths = [ROOT / s for s in ['go.mod', 'go.sum', 'native/models/catalog.json',
                               'scripts/benchmark-phase3.py', 'scripts/devtools.py', 'scripts/phase3-benchmark/main.go',
                               'scripts/qualification_helpers.py', 'scripts/completion_qualification.py']]
    for directory in ['internal', 'contracts', 'schemas', 'native']:
        paths += [p for p in (ROOT / directory).rglob('*') if p.is_file() and not p.name.startswith('._')
                  and p.suffix in ('.go', '.json', '.sql', '.c', '.h', '.tmpl', '.cpp', '.hpp', '.swift') and not p.name.endswith('_test.go')]
    return {str(p.relative_to(ROOT)): sha(p) for p in sorted(set(paths))}


def profile_errors(result, pages, iterations, hybrid_iterations, hybrid):
    """Exact recorded coverage, not a zero child exit or a total test count."""
    errors = []
    profiles = result.get('profiles') if isinstance(result, dict) else None
    if not isinstance(profiles, list) or [p.get('pages') if isinstance(p, dict) else None for p in profiles] != pages:
        return ['Recorded profiles differ from the exact requested sequence']
    for profile in profiles:
        for mode, count in [('keyword', iterations)] + ([('hybrid', hybrid_iterations)] if hybrid else []):
            measured = profile.get(mode)
            if not isinstance(measured, dict):
                errors.append(f"Missing {mode} measurements for {profile['pages']} pages")
                continue
            samples = measured.get('samplesMs')
            if (type(measured.get('count')) is not int or measured['count'] != count
                    or not isinstance(samples, list) or len(samples) != count
                    or any(type(value) not in (int, float) or not math.isfinite(value) or value < 0 for value in samples)):
                errors.append(f"Invalid {mode} sample count or values for {profile['pages']} pages")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pages', default='100,1000,10000')
    parser.add_argument('--iterations', type=int, default=100)
    parser.add_argument('--hybrid-iterations', type=int, default=30)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--models', type=Path, default=None)
    parser.add_argument('--native-bundle', type=Path, default=ROOT / 'build/native')
    parser.add_argument('--skip-hybrid', action='store_true')
    parser.add_argument('--output', type=Path, default=ROOT / 'evidence/phase3')
    args = parser.parse_args()
    try:
        pages = [int(value) for value in args.pages.split(',')]
    except ValueError:
        parser.error('--pages must be a comma-separated list of integers')
    if not 1 <= len(pages) <= 16 or len(set(pages)) != len(pages) or any(not 1 <= n <= 100000 for n in pages):
        parser.error('--pages must contain 1..16 distinct counts in 1..100000')
    if not 5 <= args.iterations <= 1000 or not 5 <= args.hybrid_iterations <= 1000:
        parser.error('keyword and hybrid iterations must be 5..1000')
    try:
        args.output.mkdir(parents=True, exist_ok=False)
    except FileExistsError:
        parser.error('Evidence directory already exists; choose a new output directory')
    args.cache = cache_dir(args.cache)
    args.models = models_dir(args.models, args.cache)
    failures = []

    def host_command(command):
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode:
            failures.append({'command': command, 'error': result.stderr.strip()})
        return {'command': command, 'exitCode': result.returncode, 'stdout': result.stdout.strip(), 'stderr': result.stderr.strip()}

    host = {'hardware': host_command(['/usr/sbin/sysctl', '-n', 'hw.model', 'hw.memsize', 'hw.ncpu', 'machdep.cpu.brand_string']),
            'os': host_command(['/usr/bin/sw_vers']), 'architecture': host_command(['/usr/bin/uname', '-m']),
            'workspaceStorage': host_command(['/bin/df', '-k', '/private/tmp']),
            'power': host_command(['/usr/bin/pmset', '-g', 'batt'])}
    model_records = []
    if not args.skip_hybrid:
        for pack in json.loads((ROOT / 'native/models/catalog.json').read_text()):
            if pack['kind'] == 'embedding':
                path = args.models / (pack['id'] + '.model')
                actual = sha(path) if path.exists() else None
                model_records.append({**pack, 'measuredSha256': actual, 'verified': actual == pack['sha256']})
                if actual != pack['sha256']:
                    failures.append({'modelPackId': pack['id'], 'error': 'Model hash verification failed'})
    before = inputs()
    snapshot = Path(tempfile.mkdtemp(prefix='trove-benchmark-native-')) / 'native'
    copied = copy_helper_bundle(args.native_bundle, snapshot)
    resource_hashes = {p: value['sha256'] for p, value in copied['snapshotInventory']['files'].items()}
    result_path = args.output / 'benchmark-service.json'
    go = go_binary(args.cache)
    env = build_env(args.cache)
    command = [go, 'run', '-mod=readonly', '-tags', 'fts5 sqlite_omit_load_extension', './scripts/phase3-benchmark',
               '-pages', ','.join(str(n) for n in pages), '-iterations', str(args.iterations),
               '-hybrid-iterations', str(args.hybrid_iterations), '-models', str(args.models), '-bundle', str(snapshot), '-output', str(result_path)]
    if not args.skip_hybrid:
        command += ['-hybrid']
    log_path = args.output / 'benchmark.log'
    with log_path.open('x') as log:
        process = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    try:
        service_result = json.loads(result_path.read_text())
        incomplete = profile_errors(service_result, pages, args.iterations, args.hybrid_iterations, not args.skip_hybrid)
    except (OSError, ValueError) as error:
        incomplete = ['Missing or invalid service result: ' + str(error)]
    after = inputs()
    changed = sorted(p for p in before.keys() | after.keys() if before.get(p) != after.get(p))
    copy_record = check_helper_bundle(args.native_bundle, snapshot, copied)
    resource_changes = copy_record['snapshotChanges']
    try:
        validate_helper_copy(copy_record, copied['sourceInventory'])
    except ValueError as error:
        failures.append({'nativeBundleCopy': str(error)})
    report = {'schemaVersion': 1, 'recordedAt': datetime.now(timezone.utc).isoformat(), 'host': host,
              'modelPacks': model_records, 'sourceBeforeSha256': before, 'sourceChangedDuringRun': changed,
              'nativeBundleSource': str(args.native_bundle), 'nativeBundleSnapshot': str(snapshot), 'nativeResourceSha256': resource_hashes,
              'nativeResourcesChangedDuringRun': resource_changes, 'nativeBundleCopy': copy_record,
              'command': command, 'exitCode': process.returncode, 'logSha256': sha(log_path),
              'serviceResultSha256': sha(result_path) if process.returncode == 0 and result_path.exists() else None,
              'requestedProfiles': pages, 'keywordIterations': args.iterations,
              'hybridIterations': args.hybrid_iterations if not args.skip_hybrid else None,
              'hybridRequestedForEveryProfile': not args.skip_hybrid,
              'profileCompletenessPassed': not incomplete, 'profileErrors': incomplete,
              'releaseQualified': False,
              'metadataErrors': failures, 'qualifiedMeasurement': process.returncode == 0 and not changed and not resource_changes and not failures and not incomplete,
              'scope': 'Measured Go service performance only; no desktop shell or UI blocking claim.'}
    with (args.output / 'benchmark-environment.json').open('x') as output:
        output.write(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report[k] for k in ['qualifiedMeasurement', 'exitCode', 'metadataErrors', 'sourceChangedDuringRun']}, indent=2))
    if process.returncode:
        print(log_path.read_text()[-6000:])
    return 0 if report['qualifiedMeasurement'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
