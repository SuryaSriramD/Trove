#!/usr/bin/env python3
"""Qualify one immutable P0 candidate; automated success is not release approval.

Requires the pinned tool/dependency cache, installed model packs, and freshly
built native helpers. This runner never downloads dependencies or builds native
inference helpers. Run with the OS permissions needed by sandbox-exec, Chrome,
Wails and native workers. Existing candidate bundles are never overwritten.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import time
import traceback
import uuid
from urllib.request import urlopen

from devtools import build_env, cache_dir, go_binary, models_dir

ROOT = Path(__file__).resolve().parents[1]
TAGS = 'desktop production fts5 sqlite_omit_load_extension'
OFFLINE = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)']
EXCLUDED = {'__pycache__', '.git', 'node_modules', 'dist', 'build', 'evidence',
            'test-results', 'playwright-report', '.DS_Store'}


def stamp():
    return datetime.now(timezone.utc).isoformat()


def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(chunk)
    return result.hexdigest()


def ignored(path):
    return any(part in EXCLUDED or part.startswith('._') for part in path.parts)


def source_fingerprint():
    paths = [ROOT / name for name in ('go.mod', 'go.sum', 'wails.json',
                                     'baseline/baseline.lock.json')]
    paths.extend(ROOT.glob('*.go'))
    for name in ('contracts', 'schemas', 'internal', 'cmd', 'native', 'packaging',
                 'frontend/src', 'frontend/tests', 'frontend/public', 'scripts',
                 'tests', 'docs/phase-3/licenses'):
        paths.extend((ROOT / name).rglob('*'))
    # Includes Vite/TypeScript/Playwright configuration and the browser entry.
    paths.extend((ROOT / 'frontend').glob('*'))
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(set(paths))
            if path.is_file() and not ignored(path.relative_to(ROOT))}


def tree_fingerprint(root):
    return {str(path.relative_to(root)): digest(path) for path in sorted(root.rglob('*'))
            if path.is_file() and not any(part.startswith('._') or part == '.DS_Store'
                                          for part in path.relative_to(root).parts)}


def fingerprint_digest(files):
    return hashlib.sha256(json.dumps(files, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def write_json(path, value):
    temporary = path.with_name(path.name + '.tmp')
    temporary.write_text(json.dumps(value, indent=2) + '\n')
    temporary.replace(path)


def stop_process(process):
    if process is None or process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
    except ProcessLookupError:
        pass


class Qualification:
    EVIDENCE_PHASE = 'phase3'
    BUILD_OPTIONS = []

    def __init__(self, args):
        self.args = args
        self.evidence = (ROOT / 'evidence' / self.EVIDENCE_PHASE).resolve()
        self.run_dir = self.evidence / 'runs' / (datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8])
        self.run_dir.mkdir(parents=True)
        self.checks = []
        self.tests = []
        self.started = stamp()
        self.inputs = {}
        self.bundle_before = {}
        self.bundle_after = {}
        self.interrupted = False
        self.go = go_binary(args.cache)
        self.node = shutil.which('node')
        self.env = dict(os.environ)
        # Ambient integration flags must not change the non-native test phase.
        for key in list(self.env):
            if key.startswith(('TROVE_', 'DYLD_')):
                self.env.pop(key)
        self.env = build_env(args.cache, source=self.env)
        self.env.update(GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local', CGO_ENABLED='1',
                        CGO_LDFLAGS='-framework UniformTypeIdentifiers')

    def path(self, path):
        try:
            return str(path.relative_to(ROOT))
        except ValueError:
            return str(path)

    def record(self, name, passed, **details):
        check = {'name': name, 'result': 'passed' if passed else 'failed', **details}
        self.checks.append(check)
        print(name + ': ' + check['result'], flush=True)
        return passed

    def command(self, name, command, cwd=ROOT, env=None, timeout=900):
        command = [str(arg) for arg in command]
        log = self.run_dir / (name + '.log')
        process = None
        code = None
        error = None
        started = time.monotonic()
        try:
            with log.open('w') as output:
                try:
                    process = subprocess.Popen(command, cwd=cwd, env=env or self.env,
                                               stdout=output, stderr=subprocess.STDOUT,
                                               start_new_session=True)
                    code = process.wait(timeout=timeout)
                except BaseException as exc:
                    error = type(exc).__name__ + ': ' + str(exc)
                    output.write('\nQualification runner error: ' + error + '\n')
                    stop_process(process)
                    if isinstance(exc, KeyboardInterrupt):
                        self.interrupted = True
        finally:
            self.record(name, code == 0 and error is None, command=command, cwd=str(cwd),
                        exitCode=code, error=error, elapsedSeconds=time.monotonic() - started,
                        log=self.path(log), logSha256=digest(log) if log.exists() else None)
        if self.interrupted:
            raise KeyboardInterrupt
        if code != 0 or error:
            if log.exists():
                print(log.read_text(errors='replace')[-6000:], flush=True)
            return False
        return True

    def parse_go(self, name, native):
        found = set()
        for line in (self.run_dir / (name + '.log')).read_text(errors='replace').splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get('Test') and event.get('Action') in ('pass', 'fail', 'skip'):
                key = event.get('Package', '') + '.' + event['Test']
                state = {'pass': 'passed', 'fail': 'failed', 'skip': 'skipped'}[event['Action']]
                self.tests.append({'test': key, 'result': state, 'phase': name,
                                   'elapsedSeconds': event.get('Elapsed')})
                if '/' not in event['Test']:
                    found.add(key)
        phase_tests = [item for item in self.tests if item['phase'] == name]
        details = {'topLevelCount': len(found), 'testAndSubtestCount': len(phase_tests),
                   'unsuccessfulTests': [item['test'] for item in phase_tests if item['result'] != 'passed']}
        passed = bool(found) and not details['unsuccessfulTests']
        if native:
            module = re.search(r'^module\s+(\S+)', (ROOT / 'go.mod').read_text(), re.M).group(1)
            expected = set()
            for relative in self.inputs:
                rel = Path(relative)
                if not relative.endswith('_test.go'):
                    continue
                path = ROOT / rel
                package = module + ('/' + rel.parent.as_posix() if rel.parent != Path('.') else '')
                expected.update(package + '.' + test for test in re.findall(r'^func\s+(TestReal\w+)\s*\(', path.read_text(), re.M))
            # Do not allow accidental removal or renaming to silently omit core native gates.
            expected.update({module + '.TestRealLocalAIWorkflow',
                             module + '/internal/native.TestRealMicrophoneControlCancellationAndParentExit',
                             module + '/internal/workers.TestRealOneHourTranscriptionKeepsEditorResponsive'})
            details['missingRequiredTests'] = sorted(expected - found)
            passed = passed and not details['missingRequiredTests']
        self.record(name + '-test-completeness', passed, **details)

    def browser(self):
        with socket.socket() as reservation:
            reservation.bind(('127.0.0.1', self.args.port))
            port = reservation.getsockname()[1]
        base_url = 'http://127.0.0.1:' + str(port)
        browser_env = dict(self.env, TROVE_TEST_URL=base_url, TROVE_BROWSER_SCREENSHOTS=str(self.run_dir))
        log_path = self.run_dir / 'vite.log'
        process = None
        try:
            with log_path.open('w') as output:
                process = subprocess.Popen([self.node, str(ROOT / 'frontend/node_modules/vite/bin/vite.js'),
                                            '--host', '127.0.0.1', '--port', str(port), '--strictPort'],
                                           cwd=ROOT / 'frontend', env=browser_env, stdout=output,
                                           stderr=subprocess.STDOUT, start_new_session=True)
                ready = False
                for _ in range(125):
                    if process.poll() is not None:
                        break
                    try:
                        with urlopen(base_url, timeout=.5) as response:
                            ready = response.status == 200
                        if ready:
                            break
                    except OSError:
                        time.sleep(.2)
                if not self.record('isolated-browser-server', ready and process.poll() is None,
                                   url=base_url, port=port, log=self.path(log_path),
                                   note='Fresh server with --strictPort; no existing server is reused.'):
                    return
                cli = ROOT / 'frontend/node_modules/@playwright/test/cli.js'
                for name, cwd in [('frontend-browser', ROOT / 'frontend'),
                                  ('database-browser', ROOT / 'frontend/src/database/tests')]:
                    report = self.run_dir / (name + '.json')
                    run_env = dict(browser_env, TROVE_BROWSER_REPORT=str(report),
                                   PLAYWRIGHT_JSON_OUTPUT_FILE=str(report))
                    self.command(name, [self.node, cli, 'test', '--config', 'playwright.config.ts',
                                        '--reporter=line,json', '--output', self.run_dir / (name + '-results')],
                                 cwd=cwd, env=run_env, timeout=900)
                    if report.is_file():
                        summary = json.loads(report.read_text())
                        stats = summary.get('stats', {})
                        passed = stats.get('expected', 0) > 0 and all(stats.get(key, 0) == 0 for key in ('unexpected', 'flaky', 'skipped'))
                        self.record(name + '-test-completeness', passed, stats=stats,
                                    report=self.path(report), reportSha256=digest(report))
                    else:
                        self.record(name + '-test-completeness', False, reason='Playwright did not write its JSON report.')
        except Exception as exc:
            self.record('browser-exception', False, error=repr(exc))
        finally:
            stop_process(process)
            if log_path.exists():
                self.record('browser-server-cleanup', process is None or process.poll() is not None,
                            log=self.path(log_path), logSha256=digest(log_path))

    def execute(self):
        self.inputs = source_fingerprint()
        write_json(self.run_dir / 'source-before.json', self.inputs)
        prerequisites = {
            'go': bool(self.go and Path(self.go).is_file()),
            'node': bool(self.node and Path(self.node).is_file()),
            'nativeManifest': (ROOT / 'build/native/manifest.json').is_file(),
            'typescript': (ROOT / 'frontend/node_modules/.bin/tsc').is_file(),
            'playwright': (ROOT / 'frontend/node_modules/@playwright/test/cli.js').is_file(),
            'sandboxExec': Path(OFFLINE[0]).is_file(),
            'unusedCandidatePath': not self.args.app.exists(),
            'modelCache': self.args.models.is_dir(),
        }
        if not self.record('prerequisites', all(prerequisites.values()), checks=prerequisites,
                           candidate=str(self.args.app), reason='Existing candidates are immutable; choose another --app path for a new run.'):
            return
        self.command('contracts', [sys.executable, '-m', 'unittest', 'discover', '-s', 'tests', '-v'])
        self.command('go-race-non-native', [self.go, 'test', '-mod=readonly', '-race', '-count=1', '-json',
                                          '-p', '1', '-timeout', '15m', '-tags', TAGS, '-skip', '^TestReal', './...'])
        self.parse_go('go-race-non-native', native=False)
        self.command('frontend-types', [self.node, ROOT / 'frontend/node_modules/typescript/bin/tsc', '--noEmit'], cwd=ROOT / 'frontend')
        unit_tests = sorted(path for path in (ROOT / 'frontend/tests').glob('*.test.ts') if not path.name.startswith('._'))
        self.command('frontend-unit', [self.node, '--experimental-strip-types', '--test', *unit_tests])
        self.browser()

        notices = self.run_dir / 'release'
        inventory = self.command('release-inventory', [sys.executable, ROOT / 'scripts/record-release-provenance.py',
                                                     '--cache', self.args.cache, '--models', self.args.models, '--output', notices])
        inventory_report = notices / 'provenance.json'
        if inventory and inventory_report.is_file():
            inventory = self.record('release-inventory-completeness', json.loads(inventory_report.read_text()).get('hashVerificationPassed') is True,
                                    report=self.path(inventory_report), reportSha256=digest(inventory_report))
        else:
            inventory = self.record('release-inventory-completeness', False, reason='No successful dependency/model provenance report.')
        if not inventory:
            self.record('candidate-build-and-native-qualification', False, reason='Blocked by failed provenance inventory; no candidate built.')
            return
        if self.args.app.exists():
            self.record('candidate-path-still-unused', False, reason='Candidate appeared during qualification; refusing to overwrite it.')
            return
        if not self.command('desktop-build', [sys.executable, ROOT / 'scripts/build-desktop.py', '--cache', self.args.cache, *self.BUILD_OPTIONS, '--output', self.args.app,
                                             '--release-notices', notices], timeout=1200):
            return
        self.bundle_before = tree_fingerprint(self.args.app)
        write_json(self.run_dir / 'bundle-before.json', self.bundle_before)
        if not self.record('candidate-bundle-created', bool(self.bundle_before) and (self.args.app / 'Contents/MacOS/Trove').is_file(),
                           candidate=str(self.args.app), bundleSha256=fingerprint_digest(self.bundle_before)):
            return

        probe = ('import errno,socket,sys\n'
                 'try:\n socket.socket().connect(("127.0.0.1",9))\n'
                 'except OSError as e:\n'
                 ' print("OS_NETWORK_DENIED" if e.errno in (errno.EPERM,errno.EACCES) else repr(e))\n'
                 ' sys.exit(0 if e.errno in (errno.EPERM,errno.EACCES) else 1)\n'
                 'else:\n sys.exit(1)\n')
        if not self.command('os-network-denial-control', OFFLINE + [sys.executable, '-c', probe], timeout=30):
            self.record('offline-native-qualification', False, reason='OS network denial was not established; offline claims withheld.')
            return
        native_env = dict(self.env, TROVE_NATIVE_INTEGRATION='1', TROVE_LONG_AUDIO_INTEGRATION='1',
                          TROVE_MODEL_ROOT=str(self.args.models),
                          TROVE_NATIVE_BUNDLE=str(self.args.app / 'Contents/Resources/native'))
        native_started = time.time()
        self.command('go-race-real-offline', OFFLINE + [self.go, 'test', '-mod=readonly', '-race', '-count=1', '-json',
                                                       '-p', '1', '-parallel', '1', '-timeout', '15m', '-tags', TAGS,
                                                       '-run', '^TestReal', './...'], env=native_env, timeout=2400)
        self.parse_go('go-race-real-offline', native=True)
        # Native fixtures historically write these stable paths. Retain only
        # fresh output from this invocation, never a prior successful fixture.
        for rel in ('evidence/phase1-native-smoke.json', 'evidence/phase1-native-failure.json',
                    'evidence/phase2/meeting-native.json', 'evidence/phase2/one-hour-native.json'):
            source = ROOT / rel
            if source.is_file() and source.stat().st_mtime >= native_started:
                target = self.run_dir / ('native-' + source.name)
                shutil.copyfile(source, target)
        self.command('packaged-p0-restart-native-offline', [sys.executable, ROOT / 'scripts/verify-packaged.py',
                                                        '--p0', '--native', '--offline', '--app', self.args.app,
                                                        '--models', self.args.models, '--evidence', self.run_dir], timeout=2000)
        packaged = self.run_dir / 'packaged-integration.json'
        if packaged.is_file():
            report = json.loads(packaged.read_text())
            steps = report.get('checks', [])
            self.record('packaged-fixture-completeness', report.get('passed') is True and report.get('networkDeniedByOS') is True
                        and len(steps) == 3 and all(step.get('passed') is True for step in steps)
                        and all(step.get('installedWeightsAvailable') is False for step in steps[:2])
                        and steps[-1].get('installedWeightsAvailable') is True,
                        report=self.path(packaged), reportSha256=digest(packaged),
                        runtimePATH='/usr/bin:/bin:/usr/sbin:/sbin',
                        note='Data create/restart uses a separate empty model cache; native step uses installed verified packs.')
        else:
            self.record('packaged-fixture-completeness', False, reason='Packaged verifier did not produce a report.')

    def finish(self):
        try:
            after = source_fingerprint()
            write_json(self.run_dir / 'source-after.json', after)
            changed = sorted(name for name in self.inputs.keys() | after.keys() if self.inputs.get(name) != after.get(name))
            self.record('source-stability', bool(self.inputs) and not changed, changedPaths=changed,
                        beforeSha256=fingerprint_digest(self.inputs), afterSha256=fingerprint_digest(after))
        except Exception as exc:
            self.record('source-stability', False, error=repr(exc))
        if self.bundle_before:
            try:
                self.bundle_after = tree_fingerprint(self.args.app)
                write_json(self.run_dir / 'bundle-after.json', self.bundle_after)
                changed = sorted(name for name in self.bundle_before.keys() | self.bundle_after.keys()
                                 if self.bundle_before.get(name) != self.bundle_after.get(name))
                self.record('candidate-bundle-stability', not changed, changedPaths=changed,
                            beforeSha256=fingerprint_digest(self.bundle_before), afterSha256=fingerprint_digest(self.bundle_after))
            except Exception as exc:
                self.record('candidate-bundle-stability', False, error=repr(exc))
        passed = bool(self.checks) and all(check['result'] == 'passed' for check in self.checks)
        status = 'automated_passed' if passed else 'failed'
        artifacts = {}
        try:
            for path in sorted(self.run_dir.rglob('*')):
                if path.is_file() and not path.name.startswith('._') and path.name != 'verification.json':
                    artifacts[str(path.relative_to(self.run_dir))] = digest(path)
        except Exception as exc:
            self.record('artifact-fingerprints', False, error=repr(exc))
            passed, status = False, 'failed'
        report = {'schemaVersion': 1, 'scope': 'phase3-p0-automated-qualification', 'status': status,
                  'startedAt': self.started, 'recordedAt': stamp(), 'interrupted': self.interrupted,
                  'releaseApproved': False, 'candidate': str(self.args.app),
                  'modelCache': str(self.args.models), 'inputSha256': self.inputs,
                  'bundleFileSha256': self.bundle_after or self.bundle_before,
                  'checks': self.checks, 'tests': self.tests, 'artifactSha256': artifacts,
                  'runEvidencePath': self.path(self.run_dir / 'verification.json'),
                  'releaseGates': [
                      {'gate': 'Complete interactive journey in the packaged Wails application', 'status': 'pending',
                       'reason': 'Service and browser fixtures do not establish native dialogs, clipboard, permissions, actual microphone/audio devices, and interactive restore. Record separate UI/device evidence.'},
                      {'gate': 'Defined hardware/workspace/model performance acceptance', 'status': 'pending',
                       'reason': 'Review the separate benchmark evidence and actual usable-shell timings; this aggregate runner does not promote timing targets.'},
                      {'gate': 'Installation on a separate clean Mac', 'status': 'pending',
                       'reason': 'Minimal PATH and empty-model-cache execution on this development host is not a clean-machine installation.'},
                      {'gate': 'Developer ID signing and notarization', 'status': 'pending',
                       'reason': 'The automated candidate build uses an ad-hoc signature.'},
                  ],
                  'limitations': ['Automated success is not a usable-release declaration or approval.',
                                  'Native microphone lifecycle uses synthetic PCM; no private microphone capture is requested by this runner.',
                                  'The one-hour native transcription fixture repeats public speech and does not establish natural meeting accuracy.',
                                  'General assistant answer quality requires review beyond fixed real-model source/evidence fixtures.',
                                  'No P1 provider or advanced parity capability is required by these checks.']}
        report = self.enrich_report(report)
        write_json(self.run_dir / 'verification.json', report)
        write_json(self.evidence / 'latest-run.json', report)
        if passed:
            write_json(self.evidence / 'automated-verification.json', report)
        print(status + ': ' + str(self.run_dir / 'verification.json'), flush=True)
        return 0 if passed else 1

    def enrich_report(self, report):
        return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--models', type=Path, default=None)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--app', type=Path, default=ROOT / 'build/phase3-candidate/Trove.app',
                        help='Unused immutable candidate path; existing apps are never overwritten')
    parser.add_argument('--port', type=int, default=0, help='Unique Vite port; 0 selects an unused ephemeral port')
    args = parser.parse_args()
    args.cache = cache_dir(args.cache)
    args.models, args.app = models_dir(args.models, args.cache), args.app.resolve()
    qualification = Qualification(args)
    try:
        qualification.execute()
    except KeyboardInterrupt:
        qualification.interrupted = True
        qualification.record('runner-interrupted', False, reason='Interrupted before all automated checks completed.')
    except Exception as exc:
        error_log = qualification.run_dir / 'runner-exception.log'
        error_log.write_text(traceback.format_exc())
        qualification.record('runner-exception', False, error=repr(exc), log=qualification.path(error_log), logSha256=digest(error_log))
    return qualification.finish()


if __name__ == '__main__':
    raise SystemExit(main())
