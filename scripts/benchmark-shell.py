#!/usr/bin/env python3
"""Measure an actual packaged Wails shell on disposable cloned workspaces."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--app', type=Path, required=True)
p.add_argument('--profiles', type=Path, default=ROOT / 'evidence/phase3/benchmark-service.json')
p.add_argument('--iterations', type=int, default=5)
p.add_argument('--output', type=Path, default=ROOT / 'evidence/phase3/shell-benchmark.json')
a = p.parse_args()
fixture = Path(tempfile.mkdtemp(prefix='trove-shell-benchmark-'))
binary = a.app.resolve() / 'Contents/MacOS/Trove'
before = hashlib.sha256(binary.read_bytes()).hexdigest()
profiles = [{'pages': 0, 'paragraphBlocks': 0, 'workspacePath': None}]
profiles += json.loads(a.profiles.read_text())['profiles']
results = []
failures = []
for profile in profiles:
    n = profile['pages']
    workspace = fixture / f'pages-{n}'
    if profile['workspacePath']:
        shutil.copytree(profile['workspacePath'], workspace)
    samples = []
    for i in range(a.iterations):
        report = fixture / f'{n}-{i}.json'
        env = {k: v for k, v in os.environ.items() if not k.startswith(('TROVE_', 'DYLD_'))}
        env.update(PATH='/usr/bin:/bin:/usr/sbin:/sbin', TROVE_WORKSPACE=str(workspace),
                   TROVE_MODEL_ROOT=str(fixture / 'empty-models'),
                   TROVE_SHELL_REPORT=str(report), TROVE_SHELL_QUIT='1')
        started = time.monotonic()
        with (fixture / f'{n}-{i}.log').open('w') as log:
            process = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)', str(binary)],
                                       env=env, stdout=log, stderr=subprocess.STDOUT)
            try:
                process.wait(timeout=60)
            except subprocess.TimeoutExpired:
                process.terminate()
                try: process.wait(timeout=5)
                except subprocess.TimeoutExpired: process.kill(); process.wait()
        outer = (time.monotonic() - started) * 1000
        if process.returncode != 0 or not report.exists():
            failures.append({'pages': n, 'iteration': i, 'exitCode': process.returncode,
                             'reason': 'Renderer did not report a usable shell', 'log': str(fixture / f'{n}-{i}.log')})
            break
        sample = json.loads(report.read_text())
        sample['launchThroughShutdownMs'] = outer
        samples.append(sample)
    values = [s['usableShellMs'] for s in samples]
    results.append({'pages': n, 'paragraphBlocks': profile['paragraphBlocks'], 'samples': samples,
                    'medianMs': statistics.median(values) if values else None,
                    'maximumMs': max(values) if values else None,
                    'approximatelyTwoSeconds': bool(values) and max(values) <= 2500})
    print(f'pages={n}: maximum shell ms={max(values) if values else "FAILED"}', flush=True)
after = hashlib.sha256(binary.read_bytes()).hexdigest()
result = {'recordedAt': datetime.now(timezone.utc).isoformat(), 'app': str(a.app),
          'appBinarySha256': after, 'appUnchanged': before == after,
          'networkDeniedByOS': True, 'modelPacks': [], 'fixture': str(fixture),
          'profiles': results, 'failures': failures,
          'measurementPassed': not failures and before == after,
          'definition': 'Go process initialization to canonical workspace snapshot rendered and two animation frames. Startup includes native validation and Wails initialization. Each sample uses a new app process; OS disk caches are not flushed. The approximate two-second threshold is explicitly interpreted as at most 2.5 seconds, and exact values are retained.',
          'limitations': ['Developer Mac only; hardware and synthetic workspace definitions are in benchmark-environment.json.',
                         'Does not measure installer/Gatekeeper time or guarantee editor responsiveness during background work.']}
a.output.parent.mkdir(parents=True, exist_ok=True)
a.output.write_text(json.dumps(result, indent=2) + '\n')
raise SystemExit(0 if result['measurementPassed'] else 1)
