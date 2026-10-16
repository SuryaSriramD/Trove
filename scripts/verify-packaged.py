#!/usr/bin/env python3
"""Launch the real .app twice and verify durable service fixture contents.

Uses a fresh retained private HOME/TMPDIR/workspace. This is service integration
through Wails OnDomReady, not interactive UI or OS-global permission acceptance.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys

from native_fixture_environment import (OS_GLOBAL_LIMITATION, native_fixture_environment,
                                        validate_native_fixture_environment)
from qualification_fixtures import private_directory, sync_directory

ROOT = Path(__file__).resolve().parents[1]


def write_json(path, value):
    with open(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as output:
        json.dump(value, output, indent=2, allow_nan=False)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    sync_directory(path.parent)


def file_identity(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('input contains a link or special object: ' + str(path))
    digest = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    after = path.lstat()
    if (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_mode) != (
            after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_mode):
        raise ValueError('input changed while hashing: ' + str(path))
    return {'sha256': digest.hexdigest(), 'bytes': info.st_size, 'mode': stat.S_IMODE(info.st_mode)}


def tree_identity(root):
    if root.is_symlink() or not root.is_dir():
        raise ValueError('input root must be an existing unlinked directory')
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError('input contains a link or special object: ' + str(path))
        if path.is_file():
            result[path.relative_to(root).as_posix()] = file_identity(path)
    return result


def source_identity():
    # These are harness and fixture source inputs, not a claim that a previously
    # built app was compiled from this checkout. The packaged bytes bind the app.
    paths = [ROOT / 'scripts' / name for name in (
        'verify-packaged.py', 'native_fixture_environment.py', 'qualification_fixtures.py')]
    paths += [ROOT / 'main.go', ROOT / 'go.mod', ROOT / 'go.sum']
    paths += sorted(ROOT.glob('app*.go'))
    paths += sorted((ROOT / 'internal/integration').glob('*.go'))
    return {path.relative_to(ROOT).as_posix(): file_identity(path) for path in paths}


def input_identity(app, models):
    return {'appFiles': tree_identity(app), 'harnessAndFixtureSourceFiles': source_identity(),
            'selectedModelFiles': tree_identity(models) if models is not None else {}}


def workspace_identity(workspace):
    return {'exists': workspace.exists(), 'files': tree_identity(workspace) if workspace.exists() else {}}


def run_child(command, env, log, cwd, timeout):
    process = subprocess.Popen(command, env=env, cwd=cwd, stdout=log,
                               stderr=subprocess.STDOUT, start_new_session=True)
    timed_out = False
    try:
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
    return process.returncode, timed_out


def run(args, *, timeout=600):
    # Resolve the parent only so dangling/existing final paths are rejected.
    evidence = args.evidence.absolute()
    evidence.parent.mkdir(parents=True, exist_ok=True)
    evidence = evidence.parent.resolve(strict=True) / evidence.name
    private_directory(evidence)
    fixture = private_directory(evidence / 'runtime')
    app = args.app.resolve(strict=True)
    binary = app / 'Contents/MacOS/Trove'
    if not binary.is_file():
        raise ValueError('packaged executable missing')
    models = args.models.resolve(strict=True) if args.native and args.models else None
    env, isolation = native_fixture_environment(fixture / 'environment', fixture / 'workspace')
    env['TROVE_INTEGRATION_QUIT'] = '1'
    journey = 'p2-' if args.p2 else 'p1-' if args.p1 else 'p0-' if args.p0 else ''
    steps = [journey + 'create', journey + 'verify'] + (['native'] if args.native else [])
    wrapper = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)'] if args.offline else []
    before = input_identity(app, models)
    started = {'schemaVersion': 2, 'recordedAt': datetime.now(timezone.utc).isoformat(),
               'app': str(app), 'fixture': str(fixture), 'steps': steps,
               'inputIdentityBefore': before, 'isolation': isolation,
               'observer': 'automated packaged service fixture', 'humanAcceptance': False}
    write_json(evidence / 'packaged-started.json', started)
    checks, errors = [], []
    network_denied = False
    try:
        if args.offline:
            probe = subprocess.run(wrapper + [sys.executable, '-c', 'import socket; socket.socket().connect(("127.0.0.1", 9))'],
                                   env=env, cwd=isolation['temporary'], capture_output=True, text=True, timeout=15)
            if probe.returncode == 0 or 'Operation not permitted' not in probe.stderr or 'sandbox_apply' in probe.stderr:
                raise ValueError('OS offline policy could not be established: ' + probe.stderr)
            network_denied = True
        for step in steps:
            if input_identity(app, models) != before:
                raise ValueError('bound inputs changed before step ' + step)
            env['TROVE_MODEL_ROOT'] = str(models) if step == 'native' and models else isolation['modelRoot']
            validate_native_fixture_environment(env, isolation)
            report = fixture / (step + '.json')
            shell = fixture / f'{step}-shell.json'
            env.update(TROVE_INTEGRATION_STEP=step, TROVE_INTEGRATION_REPORT=str(report), TROVE_SHELL_REPORT=str(shell))
            fixture_before = workspace_identity(Path(isolation['workspace']))
            with (evidence / f'packaged-{step}.log').open('x') as log:
                code, timed_out = run_child(wrapper + [str(binary)], env, log, isolation['temporary'], timeout)
            try:
                outcome = json.loads(report.read_text())
                if not isinstance(outcome, dict) or outcome.get('step') != step:
                    raise ValueError('Application report has wrong step identity')
            except (OSError, ValueError) as error:
                outcome = {'step': step, 'passed': False, 'error': str(error)}
            outcome.update(processExitCode=code, timedOut=timed_out,
                           installedWeightsAvailable=step == 'native' and models is not None,
                           modelRoot=env['TROVE_MODEL_ROOT'], fixtureInventoryBefore=fixture_before,
                           fixtureInventoryAfter=workspace_identity(Path(isolation['workspace'])))
            if shell.exists():
                outcome['shell'] = json.loads(shell.read_text())
            outcome['passed'] = outcome.get('passed') is True and code == 0 and not timed_out
            write_json(evidence / f'packaged-{step}.json', outcome)
            checks.append(outcome)
            print(f'{step}: {"passed" if outcome["passed"] else "FAILED"}', flush=True)
            if not outcome['passed']:
                break
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        errors.append(str(error))
    try:
        after = input_identity(app, models)
    except (OSError, ValueError) as error:
        after = None
        errors.append('Final input inventory unavailable: ' + str(error))
    unchanged = before == after
    result = dict(started, recordedAt=datetime.now(timezone.utc).isoformat(), checks=checks,
                  appBinarySha256=before['appFiles']['Contents/MacOS/Trove']['sha256'],
                  inputIdentityAfter=after, inputsUnchanged=unchanged, errors=errors,
                  networkDeniedByOS=network_denied,
                  passed=not errors and unchanged and len(checks) == len(steps) and all(x['passed'] for x in checks),
                  limitations=['Ad-hoc developer signature; not Developer ID or notarized.',
                               'Executed on this Mac with minimal PATH; a separate clean-machine installation is unverified.',
                               'Service integration fixture; interactive editing and accessibility use separate tests.',
                               'Current harness/fixture source inventory does not assert the packaged app was built from this checkout.',
                               'Workspace snapshots are before/after byte evidence; expected fixture writes are not automatic canonical-preservation proof.',
                               OS_GLOBAL_LIMITATION])
    name = 'packaged-p2-integration.json' if args.p2 else 'packaged-p1-integration.json' if args.p1 else 'packaged-integration.json'
    write_json(evidence / name, result)
    return 0 if result['passed'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, default=ROOT / 'build/Trove.app')
    parser.add_argument('--models', type=Path)
    parser.add_argument('--native', action='store_true')
    parser.add_argument('--p0', action='store_true', help='Run the P0 data journey in the packaged process')
    parser.add_argument('--p1', action='store_true', help='Run the P1 linked-view/formula/automation/recovery journey')
    parser.add_argument('--p2', action='store_true', help='Run the P2 charts/forms/actions/graph/conversation/portability journey')
    parser.add_argument('--offline', action='store_true', help='Deny all network operations in the app and every child process')
    parser.add_argument('--evidence', type=Path, default=ROOT / 'evidence/phase1/packaged')
    args = parser.parse_args()
    if sum([args.p0, args.p1, args.p2]) > 1:
        parser.error('Choose one data journey per invocation')
    if args.models and not args.native:
        parser.error('--models requires --native; data journeys always use a private empty model root')
    return run(args)


if __name__ == '__main__':
    raise SystemExit(main())
