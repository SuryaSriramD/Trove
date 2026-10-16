#!/usr/bin/env python3
"""Run visible Chromium gestures against an unchanged packaged host and SQLite.

This is UI/service/storage evidence, not native WebView, accessibility, AI-quality,
physical-device, live-account, performance or release acceptance. Never reuses a
workspace, retries a scenario, rebuilds the app, or changes the host allowlist.
"""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
EXPECTED = {stage: [f"real-rich-{stage}", f"real-task-{stage}"] for stage in ("create", "reopen")}
LIMITATIONS = ["Chromium UI plus production host/services/SQLite; not native Wails WebView acceptance.",
               "No model inference, physical devices, live accounts, performance or release qualification.",
               "Only rich-page and task title/completion continuity are covered; broader workflows remain pending."]

def now():
    return datetime.now(timezone.utc).isoformat()

def digest(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(chunk)
    return value.hexdigest()

def inventory(root):
    result = {}
    for path in sorted(Path(root).rglob('*')):
        if path.is_symlink():
            result[str(path.relative_to(root))] = {'link': os.readlink(path)}
        elif path.is_file():
            result[str(path.relative_to(root))] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    return result

def object_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()

def save(path, value):
    staging = path.with_suffix(path.suffix + '.writing')
    staging.write_text(json.dumps(value, indent=2) + '\n')
    staging.replace(path)

def assess(report, expected):
    """Missing, skipped, interrupted, duplicate and retried tests never pass."""
    seen = {}
    def walk(suites):
        for suite in suites:
            for spec in suite.get('specs', []):
                seen.setdefault(spec['title'], []).append(spec)
            walk(suite.get('suites', []))
    walk(report.get('suites', []))
    outcomes = {}
    for title in expected:
        specs = seen.get(title, [])
        if not specs:
            outcomes[title] = 'unexecuted'
            continue
        tests = specs[0].get('tests', [])
        results = tests[0].get('results', []) if len(tests) == 1 else []
        if len(specs) != 1 or len(tests) != 1 or len(results) != 1:
            outcomes[title] = 'invalid'
        elif results[0].get('retry', 0) != 0 or tests[0].get('expectedStatus') != 'passed':
            outcomes[title] = 'invalid'
        elif results[0].get('status') == 'passed' and specs[0].get('ok') is True and tests[0].get('expectedStatus') == 'passed':
            outcomes[title] = 'passed'
        else:
            outcomes[title] = 'invalid' if results[0].get('status') == 'passed' else results[0].get('status', 'incomplete')
    return {'outcomes': outcomes, 'unexpectedTests': sorted(set(seen) - set(expected)),
            'errors': report.get('errors', []),
            'passed': bool(expected) and all(v == 'passed' for v in outcomes.values()) and set(seen) == set(expected) and not report.get('errors')}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, required=True)
    parser.add_argument('--assets', type=Path, help='Explicit compiled frontend overlay; never rewrites installed assets')
    parser.add_argument('--evidence', type=Path, required=True, help='New empty output path on local APFS')
    parser.add_argument('--go', type=Path, required=True, help='Existing Go executable, used only to read binary provenance')
    parser.add_argument('--node', default='node')
    parser.add_argument('--chrome', type=Path, default=Path('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'))
    args = parser.parse_args()
    evidence = args.evidence.expanduser().resolve()
    if evidence.exists():
        parser.error('Evidence must use an unused path; prior runs are immutable.')
    app = args.app.expanduser().resolve()
    binary = app / 'Contents/MacOS/trove-host'
    assets = args.assets.expanduser().resolve() if args.assets else app / 'Contents/Resources/web'
    native = app / 'Contents/Resources/native'
    playwright = ROOT / 'frontend/node_modules/@playwright/test/cli.js'
    for required in (binary, assets / 'index.html', native / 'manifest.json', args.go, playwright, args.chrome):
        if not required.is_file():
            parser.error(f'Missing prerequisite: {required}')
    evidence.mkdir(parents=True, mode=0o700)
    evidence.chmod(0o700)
    workspace = evidence / 'workspace'
    token = evidence / 'owner-token.private'
    manifest_path = evidence / 'manifest.json'
    manifest = {'schemaVersion': 1, 'kind': 'trove-real-ui-storage', 'startedAt': now(), 'state': 'running', 'passed': False,
                'app': str(app), 'servedAssets': str(assets), 'frontendOverlay': bool(args.assets), 'workspace': str(workspace), 'expected': EXPECTED, 'stages': [], 'limitations': LIMITATIONS}
    save(manifest_path, manifest)
    process = None
    log = None
    inputs = None
    try:
        env = {k: v for k, v in os.environ.items() if not k.startswith(('DYLD_', 'TROVE_'))}
        env['GOTOOLCHAIN'] = 'local'
        device = subprocess.check_output(['/bin/df', '-P', str(evidence)], text=True).splitlines()[-1].split()[0]
        mounts = subprocess.check_output(['/sbin/mount'], text=True).splitlines()
        selected = [line for line in mounts if line.startswith(device + ' on ')]
        flags = selected[0].rsplit(' (', 1)[-1].rstrip(')').split(', ') if len(selected) == 1 else []
        manifest['filesystem'] = {'device': device, 'mountRecord': selected, 'type': flags[0] if flags else None}
        if not flags or flags[0] != 'apfs' or 'local' not in flags:
            raise RuntimeError('Disposable UI workspace requires local APFS')
        subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(app)], check=True, capture_output=True)
        source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        status = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=normal'], cwd=ROOT, text=True)
        provenance = subprocess.check_output([str(args.go), 'version', '-m', str(binary)], env=env, text=True)
        (evidence / 'host-build-info.txt').write_text(provenance)
        revision = [line.split('vcs.revision=', 1)[1].strip() for line in provenance.splitlines() if 'vcs.revision=' in line]
        if len(revision) != 1 or 'vcs.modified=false' not in provenance:
            raise RuntimeError('Installed host lacks clean-source provenance')
        manifest['source'] = {'harnessCommit': source, 'harnessWorkingTreeStatus': status, 'installedHostCommit': revision[0]}
        manifest['hostBuildInfoSHA256'] = digest(evidence / 'host-build-info.txt')
        app_inventory = inventory(app)
        save(evidence / 'app-inventory.json', app_inventory)
        harness_paths = ['scripts/run-real-ui.py', 'frontend/playwright.integration.config.ts', 'frontend/tests/real-storage.integration.spec.ts']
        inputs = {'appInventorySHA256': object_digest(app_inventory), 'webAssetsSHA256': object_digest(inventory(assets)),
                  'nativeHelpersSHA256': object_digest(inventory(native)), 'hostSHA256': digest(binary),
                  'harnessFiles': {name: digest(ROOT / name) for name in harness_paths},
                  'frontendSourceSHA256': object_digest(inventory(ROOT / 'frontend/src'))}
        manifest['inputs'] = inputs
        fixture = {'schemaVersion': 1, 'pageTitle': 'Real storage — café తెలుగు',
                   'body': 'A carefully saved idea: café, తెలుగు and 日本語.',
                   'initialTaskTitle': 'Prepare real storage review', 'taskTitle': 'Finish real storage review',
                   'expectedRichMarks': ['bold'], 'expectedTaskStatusGroup': 'done',
                   'expectedCases': EXPECTED}
        save(evidence / 'fixture.json', fixture)
        manifest['fixtureSHA256'] = digest(evidence / 'fixture.json')
        subprocess.run([str(binary), '--init-token', '--token-file', str(token)], env=env, check=True, capture_output=True)
        with socket.socket() as selected:
            selected.bind(('127.0.0.1', 0))
            port = selected.getsockname()[1]
        url = f'http://127.0.0.1:{port}'
        for stage in ('create', 'reopen'):
            stage_dir = evidence / stage
            stage_dir.mkdir(mode=0o700)
            command = [str(binary), '--workspace', str(workspace), '--token-file', str(token), '--listen', f'127.0.0.1:{port}', '--assets', str(assets), '--native-bundle', str(native)]
            if stage == 'create':
                command.append('--create')
            log = (stage_dir / 'host.log').open('w')
            process = subprocess.Popen(command, env=env, stdout=log, stderr=subprocess.STDOUT)
            entry = {'stage': stage, 'startedAt': now(), 'hostPID': process.pid, 'hostCommand': command,
                     'outcomes': {name: 'unexecuted' for name in EXPECTED[stage]}, 'passed': False}
            manifest['stages'].append(entry)
            save(manifest_path, manifest)
            deadline = time.monotonic() + 30
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f'{stage} host exited before readiness: {process.returncode}')
                try:
                    with urllib.request.urlopen(url, timeout=1) as response:
                        if b'Owner token' in response.read():
                            break
                except OSError:
                    pass
                if time.monotonic() >= deadline:
                    raise RuntimeError(f'{stage} host readiness timed out')
                time.sleep(0.1)
            test_env = dict(env, TROVE_REAL_UI_EVIDENCE=str(evidence), TROVE_REAL_UI_STAGE=stage,
                            TROVE_REAL_UI_URL=url, TROVE_REAL_UI_TOKEN_FILE=str(token), CHROME_PATH=str(args.chrome))
            test_command = [args.node, str(playwright), 'test', '--config', str(ROOT / 'frontend/playwright.integration.config.ts')]
            entry['testCommand'] = test_command
            with (stage_dir / 'test.log').open('w') as test_log:
                result = subprocess.run(test_command, env=test_env, cwd=ROOT / 'frontend', stdout=test_log, stderr=subprocess.STDOUT, timeout=180)
            entry['testExitCode'] = result.returncode
            report_path = stage_dir / 'playwright.json'
            report = json.loads(report_path.read_text()) if report_path.exists() else {}
            entry.update(assess(report, EXPECTED[stage]))
            process.send_signal(signal.SIGTERM)
            entry['hostExitCode'] = process.wait(timeout=20)
            entry['hostStoppedAt'] = now()
            process = None
            log.close()
            log = None
            entry['passed'] = entry['passed'] and result.returncode == 0 and entry['hostExitCode'] == 0
            entry['results'] = {name: digest(stage_dir / name) for name in ('playwright.json', 'test.log', 'host.log') if (stage_dir / name).is_file()}
            save(manifest_path, manifest)
            print(f'{stage}: {"passed" if entry["passed"] else "FAILED"}', flush=True)
            if not entry['passed']:
                break
        manifest['inputsUnchanged'] = inputs['appInventorySHA256'] == object_digest(inventory(app)) and inputs['webAssetsSHA256'] == object_digest(inventory(assets)) and inputs['frontendSourceSHA256'] == object_digest(inventory(ROOT / 'frontend/src')) and all(digest(ROOT / name) == sha for name, sha in inputs['harnessFiles'].items()) and manifest['fixtureSHA256'] == digest(evidence / 'fixture.json')
        manifest['passed'] = len(manifest['stages']) == 2 and all(stage['passed'] for stage in manifest['stages']) and manifest['inputsUnchanged']
        manifest['state'] = 'completed' if manifest['passed'] else 'failed'
    except (Exception, KeyboardInterrupt) as error:
        manifest['state'] = 'interrupted' if isinstance(error, KeyboardInterrupt) else 'failed'
        manifest['error'] = f'{type(error).__name__}: {error}'
    finally:
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            manifest['cleanupHostExitCode'] = process.returncode
        if log is not None:
            log.close()
        if token.exists():
            token.unlink()
        manifest['credentialRemoved'] = not token.exists()
        outcomes = {title: 'unexecuted' for titles in EXPECTED.values() for title in titles}
        for stage in manifest['stages']:
            outcomes.update(stage['outcomes'])
        manifest['outcomes'] = outcomes
        manifest['counts'] = {state: list(outcomes.values()).count(state) for state in sorted(set(outcomes.values()))}
        manifest['results'] = {str(path.relative_to(evidence)): digest(path) for path in evidence.rglob('*') if path.is_file() and path.suffix in ('.json', '.png') and path != manifest_path and not path.is_relative_to(workspace)}
        manifest['finishedAt'] = now()
        save(manifest_path, manifest)
    print(json.dumps({'state': manifest['state'], 'passed': manifest['passed'], 'counts': manifest['counts'], 'evidence': str(evidence)}))
    return 0 if manifest['passed'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
