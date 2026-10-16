#!/usr/bin/env python3
"""Record implementation-bound Phase 1 evidence, with release gates separate."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid
from urllib.request import urlopen

from devtools import build_env, cache_dir, go_binary, models_dir

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--cache', type=Path, default=cache_dir())
p.add_argument('--models', type=Path, default=None)
p.add_argument('--skip-package', action='store_true')
args = p.parse_args()
args.cache = cache_dir(args.cache)
args.models = models_dir(args.models, args.cache)
evidence = ROOT / 'evidence/phase1'
evidence.mkdir(parents=True, exist_ok=True)
run_evidence = evidence / 'runs' / (datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8])
run_evidence.mkdir(parents=True)

def fingerprint_inputs():
    paths = [ROOT / p for p in ['go.mod', 'go.sum', 'wails.json', 'baseline/baseline.lock.json']]
    paths += list(ROOT.glob('*.go'))
    for directory in ['contracts', 'schemas', 'internal', 'native', 'packaging', 'frontend/src', 'frontend/tests', 'scripts', 'tests']:
        paths += [p for p in (ROOT / directory).rglob('*') if p.is_file() and not p.name.startswith('._') and '__pycache__' not in p.parts]
    paths += [ROOT / 'frontend/package.json', ROOT / 'frontend/pnpm-lock.yaml']
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(set(paths))}

inputs = fingerprint_inputs()
env = build_env(args.cache)
env.update(CGO_LDFLAGS='-framework UniformTypeIdentifiers',
           TROVE_NATIVE_INTEGRATION='1', TROVE_MODEL_ROOT=str(args.models))
go = go_binary(args.cache)
node = shutil.which('node') or 'node'
checks = []
tests = []
def run(name, cmd, cwd=ROOT):
    log = run_evidence / (name + '.log')
    with log.open('w') as out:
        proc = subprocess.run([str(x) for x in cmd], cwd=cwd, env=env, stdout=out, stderr=subprocess.STDOUT)
    checks.append({'name': name, 'command': [str(x) for x in cmd],
                   'result': 'passed' if proc.returncode == 0 else 'failed',
                   'exitCode': proc.returncode, 'log': str(log.relative_to(ROOT)),
                   'logSha256': hashlib.sha256(log.read_bytes()).hexdigest()})
    print(name + ': ' + checks[-1]['result'], flush=True)
    if proc.returncode:
        print(log.read_text()[-7000:], flush=True)
    return proc.returncode == 0

run('contracts', [sys.executable, '-m', 'unittest', 'discover', '-s', 'tests', '-v'])
run('go-race', [go, 'test', '-race', '-count=1', '-json', '-tags',
               'desktop production fts5 sqlite_omit_load_extension', './...'])
for line in (run_evidence / 'go-race.log').read_text().splitlines():
    try:
        event = json.loads(line)
    except ValueError:
        continue
    if event.get('Test') and event.get('Action') in ('pass', 'fail', 'skip'):
        tests.append({'test': event['Package'] + '.' + event['Test'], 'result': {'pass': 'passed', 'fail': 'failed', 'skip': 'skipped'}[event['Action']]})
run('frontend-types', [ROOT / 'frontend/node_modules/.bin/tsc', '--noEmit'], ROOT / 'frontend')
run('frontend-codec', [node, '--experimental-strip-types', '--test', *sorted(p for p in (ROOT / 'frontend/tests').glob('*.test.ts') if not p.name.startswith('._'))])
server = None
server_log = None
try:
    try:
        urlopen('http://127.0.0.1:4173', timeout=2).close()
    except Exception:
        server_log = (run_evidence / 'vite.log').open('w')
        server = subprocess.Popen([node, str(ROOT / 'frontend/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '4173', '--strictPort'], cwd=ROOT / 'frontend', stdout=server_log, stderr=subprocess.STDOUT)
        for _ in range(50):
            try:
                urlopen('http://127.0.0.1:4173', timeout=1).close(); break
            except Exception:
                time.sleep(.2)
    run('frontend-browser', [node, 'node_modules/@playwright/test/cli.js', 'test'], ROOT / 'frontend')
    run('database-browser', [node, ROOT / 'frontend/node_modules/@playwright/test/cli.js', 'test', '--config', 'playwright.config.ts'], ROOT / 'frontend/src/database/tests')
finally:
    if server:
        server.terminate(); server.wait(timeout=10)
        server_log.close()
if not args.skip_package:
    if run('desktop-build', [sys.executable, 'scripts/build-desktop.py', '--cache', args.cache]):
        run('packaged-restart-native', [sys.executable, 'scripts/verify-packaged.py', '--models', args.models, '--native', '--evidence', run_evidence / 'packaged'])
final_inputs = fingerprint_inputs()
changed = sorted(name for name in inputs.keys() | final_inputs.keys() if inputs.get(name) != final_inputs.get(name))
checks.append({'name': 'source-stability', 'result': 'failed' if changed else 'passed', 'changedPaths': changed})
report = {'recordedAt': datetime.now(timezone.utc).isoformat(), 'scope': 'product',
          'result': 'passed' if all(c['result'] == 'passed' for c in checks) and not any(t['result'] != 'passed' for t in tests) else 'failed',
          'tests': tests, 'checks': checks, 'inputSha256': inputs,
          'runEvidencePath': str((run_evidence / 'verification.json').relative_to(ROOT)),
          'releaseGates': [
              {'gate': 'Developer ID signing and notarization', 'verified': False, 'reason': 'No valid signing identity on this host.'},
              {'gate': 'Installation on a separate clean Mac', 'verified': False, 'reason': 'Only this development Mac is available.'},
              {'gate': 'Native IME candidate selection and VoiceOver', 'verified': False, 'reason': 'Browser composition/accessibility tests do not establish native assistive behavior.'},
              {'gate': 'Real microphone authorization and recording', 'verified': False, 'reason': 'Synthetic capture tested; no private audio captured.'},
          ]}
(run_evidence / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
(evidence / 'latest-run.json').write_text(json.dumps(report, indent=2) + '\n')
if report['result'] == 'passed':
    (evidence / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
raise SystemExit(0 if report['result'] == 'passed' else 1)
