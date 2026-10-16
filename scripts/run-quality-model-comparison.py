#!/usr/bin/env python3
"""Bounded model-only comparison; historical runners retain pack106 defaults."""
import argparse
import hashlib
import importlib.util
import json
import os
import pathlib
import platform
import shutil
import signal
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
BASELINE = 'cc304d106cc711ff639d412f8e85350d8394d4b5'
PINS = {'01990000-0000-7000-8000-000000000105': {'sha256': 'b46661073c18e5b56a41fa320975f866a00def1ff08feef4718e013258896f8c', 'bytes': 1285494304, 'manifestSHA256': 'e4d19c3f59c4b86943788b55420cd4ee31cd14a08253ece6909e4eb682791816', 'manifestBytes': 778, 'estimatedMemoryBytes': 3221225472}, '01990000-0000-7000-8000-000000000109': {'sha256': '2e998d7e181c8756c5ffc55231b9ee1cdc9d3acec4245d6e27d32bd8e738c474', 'bytes': 5444831936, 'manifestSHA256': '9151e6010b37de1bf1382a69bfdc004f134214dc79cf834593a9bb473feeb4e0', 'manifestBytes': 791, 'estimatedMemoryBytes': 10468982784}}
RUNTIME = 'd2e54583c7452353eb35d40431281f6ee984332f'
LIMIT = 10 * 1024**3
REVIEW_CORPUS = 'internal/workspace/testdata/expanded-evaluation/personal-review-transfer-v1.json'
REVIEW_SHA = '0230b8e02b5da1cdad29643186b2ed87bfb8a875f463f5561a44d1e3987d3e84'
ALLOWED = frozenset((
    'internal/workspace/ai_independent_mode_controls_test.go',
    'internal/workspace/ai_quality_model_comparison_test.go',
    'scripts/run-independent-mode-controls.py',
    'scripts/run-quality-model-comparison.py',
    'scripts/test-quality-model-comparison.py',
))
EXTRA = ('scripts/run-quality-model-comparison.py', 'scripts/test-quality-model-comparison.py', 'tests/test_review_transfer.py')
TESTS = {'modes': 'TestRealLocalIndependentModeModelComparison', 'reviews-transfer': 'TestRealLocalReviewTransferModelComparison'}


def fail(message):
    raise ValueError(message)


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def binding(path, validator):
    return {'sha256': validator.digest(path), 'bytes': path.stat().st_size}


def reviewed_delta(before, current, review):
    if review.get('schemaVersion') != 1 or review.get('baselineCommit') != BASELINE:
        fail('review baseline identity differs')
    changes = {name: {'before': before.get(name), 'after': current.get(name)}
               for name in sorted(set(before) | set(current)) if before.get(name) != current.get(name)}
    if set(changes) != ALLOWED or review.get('sourceDeltas') != changes:
        fail('only the five exact reviewed harness deltas are allowed')
    return changes


def validate_pack(pack, manifest, manifest_binding, model_binding):
    if pack not in PINS:
        fail('unreviewed comparison pack')
    pin = PINS[pack]
    if manifest_binding != {'sha256': pin['manifestSHA256'], 'bytes': pin['manifestBytes']} or model_binding != {'sha256': pin['sha256'], 'bytes': pin['bytes']}:
        fail('actual model/manifest bytes differ from immutable approved pin')
    expected = {'id': pack, 'sha256': pin['sha256'], 'sizeBytes': pin['bytes'], 'kind': 'chat', 'format': 'gguf', 'contextTokens': 8192, 'runtimeCommit': RUNTIME, 'estimatedMemoryBytes': pin['estimatedMemoryBytes']}
    if any(manifest.get(k) != value for k, value in expected.items()) or manifest['estimatedMemoryBytes'] > LIMIT:
        fail('model runtime, context or unchanged admission estimate differs')


def input_parity(report, prior):
    keys = ('generatorSystem', 'generatorPrompt', 'generatorGrammar', 'generatorGrammarSHA256', 'generatorOutputCap', 'outputCap', 'contextTokens')
    return [key for key in keys if key not in report or key not in prior or report[key] != prior[key]]


def execution_errors(report, family, prior=None):
    errors = []
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != 1:
        errors.append('observed exactly one generation required')
    if report.get('nativeCallbackCompleted') is not True or report.get('nativeCallbackError') != '<nil>':
        errors.append('successful native callback missing')
    cap = 512 if family == 'modes' else 1024
    for key, expected in (('generatorOutputCap', cap), ('outputCap', cap), ('contextTokens', 8192)):
        if type(report.get(key)) is not int or report[key] != expected: errors.append(key + ' differs')
    if type(report.get('promptTokens')) is not int or report['promptTokens'] < 0 or report['promptTokens'] + cap + 16 > 8192:
        errors.append('actual reserved budget missing')
    if type(report.get('peakWorkerRSSBytes')) is not int or not 0 < report['peakWorkerRSSBytes'] <= LIMIT:
        errors.append('observed RSS exceeds unchanged admission budget or is missing')
    for key in ('generatorSystem', 'generatorPrompt', 'generatorGrammar', 'rawDraft'):
        if not isinstance(report.get(key), str): errors.append(key + ' missing')
    if not report.get('generatorSystem') or not report.get('generatorPrompt') or not report.get('rawDraft'):
        errors.append('empty actual input or raw output')
    if report.get('generatorGrammarSHA256') != hashlib.sha256(report.get('generatorGrammar', '').encode()).hexdigest():
        errors.append('actual grammar digest differs')
    if family == 'modes':
        if report.get('preparedSystem') != report.get('generatorSystem') or report.get('preparedPrompt') != report.get('generatorPrompt'):
            errors.append('actual callback differs from prepared input')
        for key in ('prepareError', 'sourceScopeError', 'finishError'):
            if report.get(key) != '<nil>': errors.append(key + ' failed')
        if report.get('nativeCompletionContract') != 'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos':
            errors.append('native completion contract differs')
    else:
        if type(report.get('generatorInvocations')) is not int or report['generatorInvocations'] != 1:
            errors.append('review invoked generator other than once')
        for key in ('sourceScope', 'reservedBudget', 'canonicalPreserved'):
            if report.get('assertions', {}).get(key) is not True: errors.append('review execution invariant failed: ' + key)
    if prior is not None:
        errors += ['actual input differs from retained106: ' + key for key in input_parity(report, prior)]
    return errors


def review_gate_errors(events, cases):
    first, second = 'TestReviewTransferSealedGoldensAndPoisons', 'TestReviewTransferCapturedScopeAndDrafts'
    expected = {first, second}
    structural = {'duplicate-key', 'unknown-field', 'trailing-object', 'missing-claims', 'extra-claim', 'unapproved-action', 'unselected-quote'}
    for case in cases:
        expected.update((first + '/' + case['id'], second + '/' + case['id']))
        expected.update(first + '/' + case['id'] + '/' + name for name in structural | {p['kind'] for p in case['poisons']})
    terminals = [e for e in events if e.get('Test') and e.get('Action') in ('pass', 'fail', 'skip')]
    if len(terminals) != len(expected) or {e['Test'] for e in terminals} != expected or any(e['Action'] != 'pass' for e in terminals):
        return ['exact54 review golden/poison/scope terminals did not all pass']
    return []


def retained_reports(path, family, modes, validator, cases):
    manifest = json.loads(path.read_text(), object_pairs_hook=validator._unique_json)
    ids = [c['id'] for c in cases]
    test = modes.TEST if family == 'modes' else 'TestRealLocalReviewTransfersV1'
    if manifest.get('state') != 'finished' or manifest.get('summary', {}).get('inputsUnchanged') is not True:
        fail('retained baseline is unfinished or changed')
    if family == 'modes' and (manifest.get('sourceCommit') != BASELINE or manifest.get('phase') != 'before' or manifest.get('summary', {}).get('executionComplete') is not True):
        fail('mode baseline identity or completion differs')
    if family == 'reviews-transfer' and manifest.get('family') != family:
        fail('review baseline family differs')
    if family == 'modes':
        if set(manifest.get('outputs', {})) != modes.OUTPUTS or any(manifest.get(key) is not True for key in ('goldenPoisonScopePassed', 'actualParserGoldenPoisonsPassed', 'consumerChecksPassed', 'manualPreflightPassed')):
            fail('mode baseline lacks complete unchanged admission gates')
    else:
        gate = manifest.get('sealedOracleScopeGates', {})
        if set(manifest.get('outputs', {})) != {'build.log', 'results.jsonl'} or gate.get('passed') is not True or gate.get('exitCode') != 0 or validator.digest(path.parent / 'oracle-gates.log') != gate.get('sha256'):
            fail('review baseline lacks retained sealed scope/oracle gates')
    for name, sha in manifest.get('outputs', {}).items():
        if pathlib.Path(name).name != name or validator.digest(path.parent / name) != sha: fail('retained baseline output changed')
    if not {'build.log', 'results.jsonl'}.issubset(manifest.get('outputs', {})): fail('baseline lacks raw output bindings')
    events, errors = modes.read_events(path.parent / 'results.jsonl')
    reports, problems = modes.extract_reports(events, validator, test, ids)
    terminals, terminal_errors = modes.native_terminals(events, test, ids)
    errors += problems + terminal_errors
    excluded = None if family == 'modes' else validator.REVIEW_EXCLUDED_TABLES
    for case in ids:
        report = reports.get(case, {})
        errors += [case + ': ' + e for e in execution_errors(report, family)]
        if report.get('modelPackId') != modes.PACK: errors.append('retained baseline is not106')
        if report.get('corpusSHA256') != (modes.CORPUS_SHA256 if family == 'modes' else REVIEW_SHA): errors.append('retained corpus binding differs')
        error = validator.validate_canonical_inventory(report, path.parent / 'canonical', test + '/' + case, excluded)
        if error: errors.append(case + ': ' + error)
    if errors: fail('retained baseline accounting invalid: ' + repr(errors))
    return manifest, reports


MINIMUM_FREE = 4 * 1024**3
FIXED_PATH = '/usr/bin:/bin:/usr/sbin:/sbin'
RUNTIME_KEYS = frozenset(('TROVE_NATIVE_INTEGRATION', 'TROVE_CHAT_EVAL_PACK', 'TROVE_MODEL_ROOT', 'TROVE_NATIVE_BUNDLE', 'TROVE_AI_CANONICAL_EVIDENCE_DIR'))


class ComparisonStopped(Exception):
    def __init__(self, reason, detail=None):
        super().__init__(reason)
        self.reason, self.detail = reason, detail


def private_child_environment(out, go_cache, module_cache):
    """Filesystem isolation only. The two explicit build caches stay shared."""
    private = out / 'private-environment'
    paths = [private, private / 'home', private / 'temporary', private / 'workspace',
             private / 'home/Library', private / 'home/Library/Application Support',
             private / 'home/Library/Caches', private / 'home/.config',
             private / 'home/.cache', private / 'home/.local',
             private / 'home/.local/share', private / 'home/.local/state',
             private / 'home/go']
    for path in paths:
        path.mkdir(mode=0o700)
    env = dict(PATH=FIXED_PATH, LANG='en_US.UTF-8', HOME=str(private / 'home'),
               TMPDIR=str(private / 'temporary') + os.sep, TMP=str(private / 'temporary'),
               TEMP=str(private / 'temporary'), XDG_CONFIG_HOME=str(private / 'home/.config'),
               XDG_CACHE_HOME=str(private / 'home/.cache'), XDG_DATA_HOME=str(private / 'home/.local/share'),
               XDG_STATE_HOME=str(private / 'home/.local/state'), GOPATH=str(private / 'home/go'),
               TROVE_WORKSPACE=str(private / 'workspace'), GOTOOLCHAIN='local', GOPROXY='off',
               GOSUMDB='off', GOENV='off', GOCACHE=str(go_cache), GOMODCACHE=str(module_cache),
               GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null', GIT_TERMINAL_PROMPT='0',
               PYTHONNOUSERSITE='1', PYTHONDONTWRITEBYTECODE='1',
               CC='/usr/bin/clang', CXX='/usr/bin/clang++', AR='/usr/bin/ar')
    receipt = {'schemaVersion': 1, 'root': str(private), 'privateDirectories': [str(p) for p in paths],
               'environment': env.copy(), 'parentEnvironmentInherited': False,
               'sharedBuildCacheExceptions': {'GOCACHE': str(go_cache), 'GOMODCACHE': str(module_cache)},
               'darwinUserConfigDir': str(private / 'home/Library/Application Support'),
               'darwinUserCacheDir': str(private / 'home/Library/Caches'),
               'osGlobalServicesIsolated': False,
               'scope': 'Owned model/compiler fixtures only. No account login, Keychain, notifications, login-item registration or user workspace operations.'}
    validate_child_environment(env, receipt)
    return env, receipt


def validate_child_environment(env, receipt):
    for value in receipt['privateDirectories']:
        path = pathlib.Path(value); info = path.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or path.resolve(strict=True) != path:
            fail('child runtime directory is linked, shared or not owned')
    expected = receipt['environment']
    if any(env.get(k) != value for k, value in expected.items()) or set(env) - set(expected) - RUNTIME_KEYS:
        fail('child environment differs from fixed private allowlist')


class ControlledRun:
    """One owned process group per command, no retries, durable terminal receipts."""
    def __init__(self, out, root, source_commit, go_cache, module_cache, manifest):
        self.out, self.root, self.source_commit, self.manifest = out, root, source_commit, manifest
        self.go_cache, self.module_cache = go_cache, module_cache
        self.env, self.env_receipt = private_child_environment(out, go_cache, module_cache)
        self.receipts = out / 'process-receipts'; self.receipts.mkdir(mode=0o700)
        self.receipt_paths, self.sequence, self.active = [], 0, None
        self.stop_signal, self.finalizing, self.handlers = None, False, {}
        self.paths, self.inputs, self.inputs_complete = [], {}, False
        self.modes, self.validator, self.ids, self.test = None, None, [], None
        self.last_check, self.minimum_observed = 0.0, {}
        self.monitored = sorted({str(out), str(go_cache), str(module_cache)})
        self.reserve = out / '.evidence-reserve'
        self.write_json(out / 'child-environment.json', self.env_receipt)
        self.write_manifest()

    @staticmethod
    def write_json(path, value):
        temporary = path.with_name(path.name + '.tmp')
        with temporary.open('w') as stream:
            json.dump(value, stream, indent=2); stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
        temporary.replace(path)

    def write_manifest(self):
        self.write_json(self.out / 'manifest.json', self.manifest)

    def handle_signal(self, number, frame):
        self.stop_signal = number
        if not self.finalizing:
            raise ComparisonStopped('interrupted', {'signal': number})

    def __enter__(self):
        for number in (signal.SIGINT, signal.SIGTERM):
            self.handlers[number] = signal.signal(number, self.handle_signal)
        return self

    def __exit__(self, kind, error, traceback):
        try:
            if error is not None:
                self.manifest.update(state=error.reason if isinstance(error, ComparisonStopped) else 'failed',
                                     terminalError={'type': type(error).__name__, 'message': str(error),
                                                    'detail': getattr(error, 'detail', None)})
            try:
                self.finalize()
            except Exception as final_error:
                # A failed final audit must never leave an earlier success receipt.
                self.emergency_finalize(final_error)
        finally:
            for number, handler in self.handlers.items(): signal.signal(number, handler)
        return False

    def check(self, force=False):
        if self.finalizing: return
        if self.stop_signal is not None: raise ComparisonStopped('interrupted', {'signal': self.stop_signal})
        now = time.monotonic()
        if not force and now - self.last_check < 0.25: return
        self.last_check = now
        free = {name: shutil.disk_usage(name).free for name in self.monitored}
        for name, value in free.items(): self.minimum_observed[name] = min(self.minimum_observed.get(name, value), value)
        if min(free.values()) < MINIMUM_FREE: raise ComparisonStopped('low_space', free)
        return free

    def digest(self, path):
        h = hashlib.sha256()
        with open(path, 'rb') as stream:
            for chunk in iter(lambda: stream.read(1024**2), b''):
                self.check(); h.update(chunk)
        return h.hexdigest()

    @staticmethod
    def group_exists(pid):
        try: os.killpg(pid, 0); return True
        except ProcessLookupError: return False

    def cleanup_group(self, process):
        result = {'pid': process.pid, 'processGroup': process.pid, 'signals': [],
                  'groupGone': False, 'processReaped': False, 'errors': []}
        # Popen(start_new_session=True) makes this exact child the group owner.
        try:
            for number, grace in ((signal.SIGTERM, 2.0), (signal.SIGKILL, 2.0)):
                if not self.group_exists(process.pid): break
                try: os.killpg(process.pid, number); result['signals'].append(number)
                except ProcessLookupError: break
                deadline = time.monotonic() + grace
                while self.group_exists(process.pid) and time.monotonic() < deadline:
                    process.poll(); time.sleep(0.05)
            try: result['returnCode'] = process.wait(timeout=0.5)
            except subprocess.TimeoutExpired: result['returnCode'] = None
            result['processReaped'] = result['returnCode'] is not None
            result['groupGone'] = not self.group_exists(process.pid)
        except Exception as error:
            result['errors'].append(type(error).__name__ + ': ' + str(error))
        return result

    def run(self, command, cwd, env, log_path, timeout):
        self.sequence += 1
        path = self.receipts / ('%03d-%s.json' % (self.sequence, log_path.name))
        receipt = {'schemaVersion': 1, 'command': command, 'cwd': str(cwd), 'log': str(log_path),
                   'state': 'not_started', 'timeoutSeconds': timeout, 'retryCount': 0,
                   'startedAtUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
        process, failure, cleanup = None, None, None
        started = time.monotonic()
        try:
            if not command or not pathlib.Path(command[0]).is_absolute(): fail('command executable must be absolute')
            validate_child_environment(env, self.env_receipt)
            receipt['freeBytesBefore'] = self.check(force=True)
            with log_path.open('xb') as log:
                process = subprocess.Popen(command, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
                self.active = process
                receipt.update(state='running', pid=process.pid, processGroup=process.pid)
                self.write_json(path, receipt)
                while True:
                    self.check(force=True)
                    code = process.poll()
                    if code is not None: break
                    if time.monotonic() - started >= timeout: raise ComparisonStopped('timeout', {'timeoutSeconds': timeout})
                    time.sleep(0.1)
                receipt.update(state='exited', exitCode=code)
        except BaseException as error:
            failure = error
            receipt.update(state=error.reason if isinstance(error, ComparisonStopped) else 'exception',
                           error={'type': type(error).__name__, 'message': str(error), 'detail': getattr(error, 'detail', None)})
        finally:
            # Repeated signals cannot interrupt owned-group cleanup or its receipt.
            was_finalizing = self.finalizing; self.finalizing = True
            try:
                if process is not None: cleanup = self.cleanup_group(process)
                self.active = None
                receipt.update(cleanup=cleanup, elapsedSeconds=time.monotonic()-started,
                               receivedSignal=self.stop_signal, minimumObservedFreeBytes=dict(self.minimum_observed))
                if log_path.exists(): receipt['logSHA256'] = self.digest(log_path)
                self.write_json(path, receipt); self.receipt_paths.append(path)
            finally: self.finalizing = was_finalizing
        if failure is not None: raise failure
        if cleanup is not None and (not cleanup['groupGone'] or not cleanup['processReaped'] or cleanup['errors']):
            raise ComparisonStopped('cleanup_incomplete', cleanup)
        return receipt['exitCode'], False

    def git(self, *arguments):
        # Even source reads are bounded and cannot inherit user Git environment.
        path = self.out / ('git-%03d.log' % (self.sequence + 1))
        code, _ = self.run(['/usr/bin/git', *arguments], self.root, self.env, path, 60)
        if code: fail('source identity command failed; retained ' + path.name)
        if path.stat().st_size > 1024**2: fail('source identity output exceeds bound')
        return path.read_text().strip()

    def git_blob(self, revision_path):
        path = self.out / ('git-%03d.log' % (self.sequence + 1))
        code, _ = self.run(['/usr/bin/git', 'show', revision_path], self.root, self.env, path, 60)
        if code or path.stat().st_size > 1024**2: fail('source baseline blob unavailable')
        return path.read_bytes()

    def finalize(self):
        self.finalizing = True
        self.manifest['finalized'] = False
        if self.active is not None:
            self.manifest['finalOwnedGroupCleanup'] = self.cleanup_group(self.active); self.active = None
        # Reserved space belongs to this attempt; release only this file so final
        # low-space receipts can still be written. No user/cache/evidence cleanup.
        released = self.reserve.stat().st_size if self.reserve.exists() else 0
        self.reserve.unlink(missing_ok=True)
        final_inputs, identity_errors = {}, []
        for path in self.paths:
            try:
                if not path.is_file() or path.is_symlink(): raise ValueError('missing or linked input')
                final_inputs[str(path)] = {'sha256': self.digest(path), 'bytes': path.stat().st_size}
            except Exception as error: identity_errors.append({'path': str(path), 'error': type(error).__name__ + ': ' + str(error)})
        try: final_head, final_status = self.git('rev-parse', 'HEAD'), self.git('status', '--porcelain', '--untracked-files=all')
        except Exception as error:
            final_head, final_status = None, None; identity_errors.append({'gitError': type(error).__name__ + ': ' + str(error)})
        unchanged = self.inputs_complete and not identity_errors and final_inputs == self.inputs and final_head == self.source_commit and final_status == ''
        final_binding = {'schemaVersion': 1, 'initialBindingsComplete': self.inputs_complete, 'inputsUnchanged': unchanged,
                         'finalSourceCommit': final_head, 'finalSourceStatus': final_status, 'inputs': final_inputs,
                         'errors': identity_errors, 'minimumObservedFreeBytes': self.minimum_observed,
                         'receivedSignal': self.stop_signal, 'ownedReserveReleasedBytes': released}
        self.write_json(self.out / 'final-input-bindings.json', final_binding)
        terminal = {'schemaVersion': 1, 'test': self.test, 'expectedCases': self.ids, 'executionComplete': False,
                    'reportedCases': [], 'terminals': {}, 'errors': [], 'canonicalErrors': {},
                    'executionErrors': {}, 'missingCaseReports': list(self.ids),
                    'observedGenerationAttempts': {}, 'nativeCallbackCompleted': {}, 'noRetry': True}
        raw = self.out / 'results.jsonl'
        if raw.exists() and self.modes is not None and self.validator is not None and self.test:
            try:
                events, malformed = self.modes.read_events(raw)
                reports, errors = self.modes.extract_reports(events, self.validator, self.test, self.ids)
                terminals, term_errors = self.modes.native_terminals(events, self.test, self.ids)
                terminal.update(reportedCases=sorted(reports), missingCaseReports=sorted(set(self.ids)-set(reports)),
                                terminals=terminals, errors=malformed+errors+term_errors)
                excluded = None if self.manifest['family'] == 'modes' else self.validator.REVIEW_EXCLUDED_TABLES
                for case, report in reports.items():
                    terminal['executionErrors'][case] = execution_errors(report, self.manifest['family'])
                    terminal['observedGenerationAttempts'][case] = report.get('generationAttempts')
                    terminal['nativeCallbackCompleted'][case] = report.get('nativeCallbackCompleted')
                    terminal['canonicalErrors'][case] = self.validator.validate_canonical_inventory(report, self.out / 'canonical', self.test + '/' + case, excluded)
                self.write_json(self.out / 'retained-terminal-reports.json', reports)
            except Exception as error: terminal['errors'].append(type(error).__name__ + ': ' + str(error))
        else: terminal['errors'].append('native results unavailable; cases are unexecuted or unobserved')
        terminal['executionComplete'] = bool(self.manifest.get('summary', {}).get('executionComplete')) and unchanged and self.stop_signal is None and not terminal['errors'] and not any(terminal['canonicalErrors'].values()) and not any(terminal['executionErrors'].values())
        self.write_json(self.out / 'terminal-accounting.json', terminal)
        if not terminal['executionComplete']:
            if 'summary' in self.manifest: self.manifest['summary'].update(executionComplete=False, automatedChecksPassed=False, inputsUnchanged=unchanged)
        if self.stop_signal is not None: self.manifest['state'] = 'interrupted'
        self.manifest.update(finalized=True, finalInputsUnchanged=unchanged, receivedSignal=self.stop_signal,
                             finishedAtUTC=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                             terminalAccounting='terminal-accounting.json', finalInputBindings='final-input-bindings.json',
                             processReceipts=[str(p.relative_to(self.out)) for p in self.receipt_paths])
        output_paths = [p for p in self.out.iterdir() if p.is_file() and p.name != 'manifest.json'] + list(self.receipts.glob('*.json'))
        self.manifest['outputs'] = {str(p.relative_to(self.out)): self.digest(p) for p in output_paths}
        self.write_manifest()

    def emergency_finalize(self, error):
        # Preserve raw files and explicitly downgrade accounting if a finalizer
        # operation itself fails (for example, an unavailable filesystem).
        self.finalizing = True
        self.reserve.unlink(missing_ok=True)
        if self.active is not None:
            self.manifest['finalOwnedGroupCleanup'] = self.cleanup_group(self.active)
            self.active = None
        failure = {'type': type(error).__name__, 'message': str(error)}
        binding_path = self.out / 'final-input-bindings.json'
        if not binding_path.exists():
            self.write_json(binding_path, {'schemaVersion': 1, 'inputsUnchanged': False,
                            'initialBindingsComplete': self.inputs_complete, 'finalAuditComplete': False,
                            'errors': [failure], 'receivedSignal': self.stop_signal})
        self.write_json(self.out / 'terminal-accounting.json', {'schemaVersion': 1, 'test': self.test,
                        'expectedCases': self.ids, 'executionComplete': False, 'finalAuditComplete': False,
                        'errors': [failure], 'nativeAttemptsInferred': False, 'noRetry': True})
        self.manifest.update(state='finalization_failed', finalized=False, finalInputsUnchanged=False,
                             finalizationError=failure, terminalAccounting='terminal-accounting.json',
                             finalInputBindings='final-input-bindings.json',
                             processReceipts=[str(p.relative_to(self.out)) for p in self.receipt_paths])
        if 'summary' in self.manifest:
            self.manifest['summary'].update(executionComplete=False, automatedChecksPassed=False, inputsUnchanged=False)
        self.write_manifest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('root', 'source-commit', 'go', 'node', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'output', 'before-modes', 'before-reviews', 'review', 'review-sha256', 'preflight-review'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--pack', choices=sorted(PINS), required=True)
    parser.add_argument('--family', choices=sorted(TESTS), required=True)
    args = parser.parse_args()
    if platform.system() != 'Darwin': parser.error('macOS network denial required')
    root, bundle, models, go, node = [pathlib.Path(p).resolve() for p in (args.root, args.bundle, args.models, args.go, args.node)]
    out = pathlib.Path(args.output).resolve()
    if out.is_relative_to(root) or pathlib.Path(__file__).resolve() != root / 'scripts/run-quality-model-comparison.py': fail('explicit committed checkout and outside evidence required')
    out.mkdir(mode=0o700)
    manifest = {'schemaVersion': 1, 'state': 'preflight', 'family': args.family,
                'modelComparison': True, 'sourceCommit': args.source_commit, 'pack': args.pack,
                'test': TESTS[args.family], 'oneGenerationPerCase': True,
                'performanceQualified': False, 'releaseQualified': False, 'semanticAcceptance': None}
    result = 1
    guard = None
    try:
        guard = ControlledRun(out, root, args.source_commit, pathlib.Path(args.go_build_cache).resolve(), pathlib.Path(args.go_mod_cache).resolve(), manifest)
        with guard:
            guard.check(force=True)
            guard.reserve.write_bytes(bytes(1024**2))
            result = execute_attempt(args, root, bundle, models, go, node, out, guard, manifest)
    except (Exception, KeyboardInterrupt) as error:
        if guard is None:
            manifest.update(state='initialization_failed', finalized=False, finalInputsUnchanged=False,
                            terminalError={'type': type(error).__name__, 'message': str(error)})
            ControlledRun.write_json(out / 'final-input-bindings.json', {'schemaVersion': 1, 'initialBindingsComplete': False, 'inputsUnchanged': False})
            ControlledRun.write_json(out / 'terminal-accounting.json', {'schemaVersion': 1, 'executionComplete': False, 'nativeAttemptsInferred': False, 'errors': ['private child environment initialization failed; no child launched']})
            ControlledRun.write_json(out / 'manifest.json', manifest)
        print(json.dumps({'state': manifest.get('state'), 'error': type(error).__name__ + ': ' + str(error), 'retained': str(out)}))
        return 1
    print(json.dumps(manifest.get('summary', {'state': manifest.get('state')}), indent=2))
    if not manifest.get('finalized') or not manifest.get('finalInputsUnchanged') or manifest.get('receivedSignal') is not None: return 1
    return result


def execute_attempt(args, root, bundle, models, go, node, out, guard, manifest):
    modes = load('quality_mode_controls', root / 'scripts/run-independent-mode-controls.py')
    validator = modes.load_repository_validator(root)
    guard.modes, guard.validator = modes, validator
    guard.test = TESTS[args.family]
    # Minimal known inputs are retained even when the full admission inventory fails.
    guard.paths = [root / 'scripts/run-quality-model-comparison.py', root / modes.CORPUS, root / REVIEW_CORPUS,
                   go, node, models / (args.pack + '.json'), models / (args.pack + '.model')]
    validator.digest = guard.digest
    if guard.git('rev-parse', 'HEAD') != args.source_commit or guard.git('status', '--porcelain', '--untracked-files=all'):
        fail('exact clean reviewed source commit required')
    review_path = pathlib.Path(args.review).resolve()
    if validator.digest(review_path) != args.review_sha256: fail('review artifact hash differs')
    review = json.loads(review_path.read_text(), object_pairs_hook=validator._unique_json)
    if review.get('candidateCommit') != args.source_commit: fail('reviewed candidate commit differs')
    mode_cases = json.loads((root / modes.CORPUS).read_text(), object_pairs_hook=validator._unique_json)['cases']
    review_cases = json.loads((root / REVIEW_CORPUS).read_text(), object_pairs_hook=validator._unique_json)['cases']
    if validator.digest(root / modes.CORPUS) != modes.CORPUS_SHA256 or validator.digest(root / modes.ORIGINAL_CORPUS) != modes.ORIGINAL_SHA256 or validator.digest(root / REVIEW_CORPUS) != REVIEW_SHA:
        fail('immutable request/oracle corpus changed')
    if [c['id'] for c in mode_cases] != modes.IDS or len(review_cases) != 4 or len({c['id'] for c in review_cases}) != 4: fail('full fixed cohort identity differs')
    guard.ids = [c['id'] for c in (mode_cases if args.family == 'modes' else review_cases)]
    before_modes, mode_reports = retained_reports(pathlib.Path(args.before_modes).resolve(), 'modes', modes, validator, mode_cases)
    before_reviews, review_reports = retained_reports(pathlib.Path(args.before_reviews).resolve(), 'reviews-transfer', modes, validator, review_cases)
    preflight = pathlib.Path(args.preflight_review).resolve()
    preflight_data = json.loads(preflight.read_text(), object_pairs_hook=validator._unique_json)
    if modes.validate_manual_review(preflight_data, modes.manual_template(mode_cases)): fail('independent preflight manual review invalid')
    paths = modes.input_paths(root, bundle, models, go, node, args.pack) + [root / p for p in EXTRA] + [review_path, preflight]
    paths = sorted(set(paths))
    guard.paths = paths
    inputs = modes.inventory(paths, validator)
    guard.inputs, guard.inputs_complete = inputs, True
    source = {str(p.relative_to(root)): inputs[str(p)] for p in paths if p.is_relative_to(root)}
    # The baseline never inventoried unchanged tests/test_review_transfer.py; bind
    # its exact baseline Git blob now without pretending it was in the old run.
    prior_source = dict(before_modes['sourceInputs'])
    extra_test = 'tests/test_review_transfer.py'
    old = guard.git_blob(BASELINE + ':' + extra_test)
    prior_source[extra_test] = {'sha256': hashlib.sha256(old).hexdigest(), 'bytes': len(old)}
    changes = reviewed_delta(prior_source, source, review)
    stable = modes.stable_identity(root, bundle, models, go, node, inputs, args.pack)
    stable['manualPreflightReview'] = inputs[str(preflight)]
    allowed_labels = {'model', 'modelManifest', 'pilot', 'harness:internal/workspace/ai_independent_mode_controls_test.go'}
    stable_changes = {k for k in set(stable) | set(before_modes['stableBindings']) if stable.get(k) != before_modes['stableBindings'].get(k)}
    if stable_changes - allowed_labels: fail('runtime, parser, original harness/corpus or preflight binding changed: ' + repr(stable_changes))
    model = json.loads((models / (args.pack + '.json')).read_text(), object_pairs_hook=validator._unique_json)
    validate_pack(args.pack, model, inputs[str(models / (args.pack + '.json'))], inputs[str(models / (args.pack + '.model'))])
    helpers = json.loads((bundle / 'manifest.json').read_text())['helpers']
    if len([h for h in helpers if h.get('name') == 'llama-worker' and h.get('runtimeCommit') == RUNTIME and h.get('sha256') == stable['llama']['sha256']]) != 1: fail('actual pinned helper runtime differs')
    free = guard.check(force=True)
    cases = mode_cases if args.family == 'modes' else review_cases
    ids, test = [c['id'] for c in cases], TESTS[args.family]
    manifest.update({'schemaVersion': 1, 'state': 'started', 'family': args.family, 'modelComparison': True, 'sourceCommit': args.source_commit, 'baselineCommit': BASELINE, 'pack': args.pack, 'test': test, 'caseIds': ids, 'inputs': inputs, 'sourceChanges': changes, 'stableBindings': stable, 'beforeModes': {'path': args.before_modes, **binding(pathlib.Path(args.before_modes), validator)}, 'beforeReviews': {'path': args.before_reviews, **binding(pathlib.Path(args.before_reviews), validator)}, 'initialFreeBytes': free, 'networkPolicy': modes.NETWORK_POLICY, 'canonicalEvidenceDirectory': str(out / 'canonical'), 'performanceQualified': False, 'releaseQualified': False, 'semanticAcceptance': None, 'oneGenerationPerCase': True})
    guard.ids, guard.test = ids, test
    write = lambda: validator.atomic_json(out / 'manifest.json', manifest)
    write(); started = time.monotonic()
    env = dict(guard.env)
    sandbox = ['/usr/bin/sandbox-exec', '-p', modes.NETWORK_POLICY]
    for name, command in [('comparison-consumer', [str(pathlib.Path(sys.executable).resolve()), str(root / 'scripts/test-quality-model-comparison.py')]), ('mode-consumer', [str(pathlib.Path(sys.executable).resolve()), str(root / 'scripts/test-independent-mode-controls.py')]), ('review-consumer', [str(pathlib.Path(sys.executable).resolve()), '-m', 'unittest', 'discover', '-s', 'tests', '-p', 'test_review_transfer.py'])]:
        code, timeout = guard.run(sandbox + command, root, env, out / (name + '.log'), 120)
        if code or timeout: manifest.update(state='consumer_failed', failedGate=name, exitCode=code, timedOut=timeout); write(); return 1
    binary = out / 'evaluation.test'
    build = [str(go), 'test', '-mod=readonly', '-tags', modes.TAGS, '-c', '-o', str(binary), './internal/workspace']
    manifest['buildCommand'] = build; write()
    code, timeout = guard.run(sandbox + build, root, env, out / 'build.log', 900)
    if code or timeout: manifest.update(state='build_failed', exitCode=code, timedOut=timeout); write(); return 1
    manifest['testBinarySHA256'] = validator.digest(binary)
    gates = list(modes.GATES) + ['TestReviewTransferSealedGoldensAndPoisons', 'TestReviewTransferCapturedScopeAndDrafts', 'TestQualityComparisonPackSelection']
    gate = [str(go), 'tool', 'test2json', '-t', '-p', 'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^(' + '|'.join(gates) + ')$', '-test.v', '-test.timeout=10m']
    gate_env = dict(env, TROVE_AI_CANONICAL_EVIDENCE_DIR=str(out / 'canonical-gates'))
    code, timeout = guard.run(sandbox + gate, root / 'internal/workspace', gate_env, out / 'gates.jsonl', 630)
    events, errors = modes.read_events(out / 'gates.jsonl')
    mode_events = [e for e in events if e.get('Test', '').split('/')[0] in modes.GATES]
    review_events = [e for e in events if e.get('Test', '').startswith('TestReviewTransfer')]
    original_ids = [c['id'] for c in json.loads((root / modes.ORIGINAL_CORPUS).read_text())['cases']]
    errors += modes.gate_errors(mode_events, original_ids) + review_gate_errors(review_events, review_cases)
    pack_terminals = [e for e in events if e.get('Test') == 'TestQualityComparisonPackSelection' and e.get('Action') in ('pass', 'fail', 'skip')]
    if len(pack_terminals) != 1 or pack_terminals[0]['Action'] != 'pass': errors.append('pack guard gate failed')
    if code or timeout or errors: manifest.update(state='gates_failed', errors=errors, exitCode=code, timedOut=timeout); write(); return 1
    parser_entry = (root / 'frontend/node_modules/mermaid/dist/mermaid.core.mjs').resolve()
    parser_gate = [str(node), str(root / 'scripts/evaluate-mode-control-mermaid.mjs'), '--root', str(root), '--mode', 'golden']
    code, timeout = guard.run(sandbox + parser_gate, root, env, out / 'mermaid-gates.json', 120)
    parser_report, errors = modes.read_parser_report(out / 'mermaid-gates.json', validator)
    errors += modes.parser_report_errors(parser_report, mode_cases, 'golden', inputs[str(parser_entry)]['sha256'])
    if code or timeout or errors or parser_report.get('passed') is not True: manifest.update(state='parser_gates_failed', errors=errors, exitCode=code, timedOut=timeout); write(); return 1
    manifest['preAdmissionFreeBytes'] = guard.check(force=True)
    if modes.inventory(paths, validator) != inputs or guard.git('rev-parse', 'HEAD') != args.source_commit or guard.git('status', '--porcelain', '--untracked-files=all'): fail('frozen inputs changed before native admission')
    env.update(TROVE_NATIVE_INTEGRATION='1', TROVE_CHAT_EVAL_PACK=args.pack, TROVE_MODEL_ROOT=str(models), TROVE_NATIVE_BUNDLE=str(bundle), TROVE_AI_CANONICAL_EVIDENCE_DIR=str(out / 'canonical'))
    command = [str(go), 'tool', 'test2json', '-t', '-p', 'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^' + test + '$', '-test.v', '-test.timeout=55m']
    manifest.update(state='native_running', runCommand=command); write()
    code, timeout = guard.run(sandbox + command, root / 'internal/workspace', env, out / 'results.jsonl', 3330)
    events, errors = modes.read_events(out / 'results.jsonl')
    reports, extraction = modes.extract_reports(events, validator, test, ids)
    terminals, terminal_errors = modes.native_terminals(events, test, ids)
    errors += extraction + terminal_errors
    before_reports = mode_reports if args.family == 'modes' else review_reports
    excluded = None if args.family == 'modes' else validator.REVIEW_EXCLUDED_TABLES
    checks, accounting = {}, {}
    for case in cases:
        r = reports.get(case['id'], {})
        accounting[case['id']] = execution_errors(r, args.family, before_reports[case['id']])
        if r.get('modelPackId') != args.pack: accounting[case['id']].append('selected model identity differs')
        canonical = validator.validate_canonical_inventory(r, out / 'canonical', test + '/' + case['id'], excluded)
        if canonical: accounting[case['id']].append(canonical)
        if args.family == 'modes': checks[case['id']] = modes.validate_mode(r, case, validator, args.pack)
        else: checks[case['id']] = validator.validate_workflow_report(r, case['id'], {'family': 'reviews-transfer', 'modelPackId': args.pack, 'corpusSHA256': REVIEW_SHA, 'reviewCases': {c['id']: c for c in review_cases}})
    validator.atomic_json(out / 'native-reports.json', reports)
    if args.family == 'modes':
        validator.atomic_json(out / 'manual-review-template.json', modes.manual_template(cases, reports))
        command = [str(node), str(root / 'scripts/evaluate-mode-control-mermaid.mjs'), '--root', str(root), '--mode', 'outputs', '--input', str(out / 'native-reports.json')]
        parser_code, parser_timeout = guard.run(sandbox + command, root, env, out / 'mermaid-results.json', 120)
        parser_report, parser_errors = modes.read_parser_report(out / 'mermaid-results.json', validator)
        parser_errors += modes.parser_report_errors(parser_report, cases, 'outputs', inputs[str(parser_entry)]['sha256'], reports)
        parser_pass = not parser_code and not parser_timeout and not parser_errors and parser_report.get('passed') is True
    else:
        parser_errors, parser_pass = [], None
        validator.atomic_json(out / 'manual-review-inputs.json', {'cases': cases, 'reports': reports, 'semanticAccepted': None})
    unchanged = modes.inventory(paths, validator) == inputs and guard.git('rev-parse', 'HEAD') == args.source_commit and not guard.git('status', '--porcelain', '--untracked-files=all')
    complete = not timeout and not errors and not any(accounting.values()) and unchanged
    summary = {'executionComplete': complete, 'inputsUnchanged': unchanged, 'errors': errors, 'executionErrors': accounting, 'qualityErrors': checks, 'actualMermaidParserPassed': parser_pass, 'parserErrors': parser_errors, 'singleSuccessfulCallbacks': sum(not value for value in accounting.values()), 'expectedCases': len(ids), 'automatedChecksPassed': complete and code == 0 and not any(checks.values()) and (args.family != 'modes' or parser_pass), 'semanticAccepted': None, 'manualSemanticReviewPending': ids, 'performanceQualified': False, 'releaseQualified': False}
    manifest.update(state='finished', exitCode=code, timedOut=timeout, elapsedSeconds=time.monotonic()-started, summary=summary)
    write()
    return 0 if summary['automatedChecksPassed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
