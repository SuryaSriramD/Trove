#!/usr/bin/env python3
"""Launch the real .app executable twice and verify durable service contents.

Uses isolated test workspace state; the ordinary default workspace is untouched.
OnDomReady in Wails drives the fixture and quits via the regular lifecycle.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import sys

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--app', type=Path, default=ROOT / 'build/Trove.app')
p.add_argument('--models', type=Path)
p.add_argument('--native', action='store_true')
p.add_argument('--p0', action='store_true', help='Run the full P0 data journey in the packaged process')
p.add_argument('--p1', action='store_true', help='Run the P1 linked-view/formula/automation/recovery journey')
p.add_argument('--offline', action='store_true', help='Deny all network operations in the app and every child process')
p.add_argument('--evidence', type=Path, default=ROOT / 'evidence/phase1')
args = p.parse_args()
if args.p0 and args.p1:
    p.error('Choose one data journey per invocation')
evidence = args.evidence
evidence.mkdir(parents=True, exist_ok=True)
fixture = Path(tempfile.mkdtemp(prefix='trove-packaged-'))
binary = args.app / 'Contents/MacOS/Trove'
env = dict(os.environ)
for name in list(env):
    if name.startswith(('DYLD_', 'TROVE_')):
        env.pop(name)
env.update(PATH='/usr/bin:/bin:/usr/sbin:/sbin',
           TROVE_WORKSPACE=str(fixture / 'workspace'),
           TROVE_INTEGRATION_QUIT='1')
steps = (['p1-create', 'p1-verify'] if args.p1 else (['p0-create', 'p0-verify'] if args.p0 else ['create', 'verify'])) + (['native'] if args.native else [])
wrapper = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)'] if args.offline else []
if args.offline:
    # A negative control confirms OS enforcement before accepting offline evidence.
    probe = subprocess.run(wrapper + [sys.executable, '-c', 'import socket; socket.socket().connect(("127.0.0.1", 9))'], capture_output=True, text=True)
    if probe.returncode == 0 or 'Operation not permitted' not in probe.stderr or 'sandbox_apply' in probe.stderr:
        raise SystemExit('OS offline policy could not be established: ' + probe.stderr)
checks = []
for step in steps:
    env['TROVE_MODEL_ROOT'] = str(args.models if step == 'native' and args.models else fixture / 'empty-model-cache')
    report = fixture / (step + '.json')
    env.update(TROVE_INTEGRATION_STEP=step, TROVE_INTEGRATION_REPORT=str(report))
    env['TROVE_SHELL_REPORT'] = str(fixture / f'{step}-shell.json')
    with (evidence / f'packaged-{step}.log').open('w') as log:
        process = subprocess.run(wrapper + [str(binary)], env=env, stdout=log,
                                 stderr=subprocess.STDOUT, timeout=600)
    outcome = json.loads(report.read_text()) if report.exists() else {'passed': False, 'error': 'Application did not produce a report'}
    outcome['processExitCode'] = process.returncode
    outcome['installedWeightsAvailable'] = step == 'native' and bool(args.models)
    shell = fixture / f'{step}-shell.json'
    if shell.exists(): outcome['shell'] = json.loads(shell.read_text())
    outcome['passed'] = outcome.get('passed') is True and process.returncode == 0
    (evidence / f'packaged-{step}.json').write_text(json.dumps(outcome, indent=2) + '\n')
    checks.append(outcome)
    print(f'{step}: {"passed" if outcome["passed"] else "FAILED"}', flush=True)
    if not outcome['passed']:
        break
result = {'recordedAt': datetime.now(timezone.utc).isoformat(),
          'app': str(args.app), 'appBinarySha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
          'fixture': str(fixture), 'checks': checks,
          'networkDeniedByOS': args.offline,
          'passed': len(checks) == len(steps) and all(x['passed'] for x in checks),
          'limitations': ['Ad-hoc developer signature; not Developer ID or notarized.',
                          'Executed on this Mac with minimal PATH; a separate clean-machine installation is unverified.',
                          'Service integration fixture; interactive editing and accessibility use separate tests.']}
(evidence / ('packaged-p1-integration.json' if args.p1 else 'packaged-integration.json')).write_text(json.dumps(result, indent=2) + '\n')
raise SystemExit(0 if result['passed'] else 1)
