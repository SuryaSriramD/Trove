#!/usr/bin/env python3
"""Run reproducible P0 service benchmarks and bind timings to host/source data."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def inputs():
    paths = [ROOT / s for s in ['go.mod', 'go.sum', 'native/models/catalog.json',
                               'scripts/benchmark-phase3.py', 'scripts/phase3-benchmark/main.go']]
    for directory in ['internal', 'contracts', 'schemas', 'native']:
        paths += [p for p in (ROOT / directory).rglob('*') if p.is_file() and not p.name.startswith('._')
                  and p.suffix in ('.go', '.json', '.sql', '.c', '.h', '.tmpl', '.cpp', '.hpp', '.swift') and not p.name.endswith('_test.go')]
    return {str(p.relative_to(ROOT)): sha(p) for p in sorted(set(paths))}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pages', default='100,1000,10000')
    parser.add_argument('--iterations', type=int, default=100)
    parser.add_argument('--models', type=Path, default=Path('/private/tmp/trove-phase1-models'))
    parser.add_argument('--native-bundle', type=Path, default=ROOT / 'build/native')
    parser.add_argument('--skip-hybrid', action='store_true')
    parser.add_argument('--output', type=Path, default=ROOT / 'evidence/phase3')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
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
    shutil.copytree(args.native_bundle, snapshot, ignore=shutil.ignore_patterns('._*', '.DS_Store'))
    resource_hashes = {str(p.relative_to(snapshot)): sha(p) for p in sorted(snapshot.rglob('*')) if p.is_file()}
    result_path = args.output / 'benchmark-service.json'
    go = shutil.which('go') or '/private/tmp/trove-phase0-tools/go/bin/go'
    env = dict(os.environ, GOPROXY='off', GOTOOLCHAIN='local', GOCACHE='/private/tmp/trove-phase0-tools/go-cache',
               GOMODCACHE='/private/tmp/trove-phase0-tools/go-mod')
    command = [go, 'run', '-mod=readonly', '-tags', 'fts5 sqlite_omit_load_extension', './scripts/phase3-benchmark',
               '-pages', args.pages, '-iterations', str(args.iterations), '-models', str(args.models), '-bundle', str(snapshot), '-output', str(result_path)]
    if not args.skip_hybrid:
        command += ['-hybrid']
    log_path = args.output / 'benchmark.log'
    with log_path.open('w') as log:
        process = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    after = inputs()
    changed = sorted(p for p in before.keys() | after.keys() if before.get(p) != after.get(p))
    resources_after = {str(p.relative_to(snapshot)): sha(p) for p in sorted(snapshot.rglob('*')) if p.is_file()}
    resource_changes = sorted(p for p in resource_hashes.keys() | resources_after.keys() if resource_hashes.get(p) != resources_after.get(p))
    report = {'schemaVersion': 1, 'recordedAt': datetime.now(timezone.utc).isoformat(), 'host': host,
              'modelPacks': model_records, 'sourceBeforeSha256': before, 'sourceChangedDuringRun': changed,
              'nativeBundleSource': str(args.native_bundle), 'nativeBundleSnapshot': str(snapshot), 'nativeResourceSha256': resource_hashes,
              'nativeResourcesChangedDuringRun': resource_changes,
              'command': command, 'exitCode': process.returncode, 'logSha256': sha(log_path),
              'serviceResultSha256': sha(result_path) if process.returncode == 0 and result_path.exists() else None,
              'metadataErrors': failures, 'qualifiedMeasurement': process.returncode == 0 and not changed and not resource_changes and not failures,
              'scope': 'Measured Go service performance only; no desktop shell or UI blocking claim.'}
    (args.output / 'benchmark-environment.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report[k] for k in ['qualifiedMeasurement', 'exitCode', 'metadataErrors', 'sourceChangedDuringRun']}, indent=2))
    if process.returncode:
        print(log_path.read_text()[-6000:])
    return 0 if report['qualifiedMeasurement'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
