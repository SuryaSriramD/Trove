import fcntl
import importlib.util
import json
import os
import sys
from pathlib import Path
import tempfile
import unittest
import contextlib
import io
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('native_performance', ROOT / 'scripts/record-native-performance.py')
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)


class NativePerformanceTests(unittest.TestCase):
    def diagnostic(self, sequence=1, finalized=False, **changes):
        value = {'schemaVersion': 1, 'sequence': sequence, 'finalized': finalized, 'capped': False,
                 'ready': True, 'visible': True, 'focused': True, 'stopped': False,
                 'pendingFrames': 0, 'pendingTransports': 0,
                 'counters': {name: 0 for name in driver.DIAGNOSTIC_COUNTERS}}
        value.update(changes)
        return value

    def test_renderer_readiness_requires_live_visible_focused_uncapped_state(self):
        self.assertTrue(driver.renderer_ready(self.diagnostic()))
        for changes in ({'ready': False}, {'visible': False}, {'focused': False},
                        {'stopped': True}, {'finalized': True}, {'capped': True}):
            self.assertFalse(driver.renderer_ready(self.diagnostic(**changes)))
        self.assertFalse(driver.renderer_ready(None))

    def test_incremental_diagnostics_reject_partial_final_schema_and_identity_changes(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'diagnostics.jsonl'
            first = json.dumps(self.diagnostic()).encode()
            path.write_bytes(first[:40]); path.chmod(0o600)
            reader = driver.RendererDiagnostics(path)
            self.assertIsNone(reader.poll())
            with path.open('ab') as out: out.write(first[40:] + b'\n')
            self.assertEqual(reader.poll()['sequence'], 1)
            with path.open('ab') as out: out.write(json.dumps(self.diagnostic(2, True)).encode() + b'\n')
            self.assertTrue(reader.poll(final=True)['finalized'])
            path.rename(path.with_suffix('.old'))
            path.write_bytes(first + b'\n'); path.chmod(0o600)
            with self.assertRaises(ValueError): reader.poll()

    def test_diagnostics_reject_unknown_content_duplicate_keys_and_invalid_counts(self):
        invalid = [self.diagnostic(title='private'), self.diagnostic(pendingFrames=17),
                   self.diagnostic(ready=1), self.diagnostic(sequence=True),
                   self.diagnostic(counters={'private': 1}),
                   self.diagnostic(counters={name: True for name in driver.DIAGNOSTIC_COUNTERS})]
        with tempfile.TemporaryDirectory() as temp:
            for index, value in enumerate(invalid):
                path = Path(temp) / str(index); path.write_text(json.dumps(value) + '\n'); path.chmod(0o600)
                with self.assertRaises(ValueError): driver.RendererDiagnostics(path).poll()
            path = Path(temp) / 'duplicate'; path.write_text(json.dumps(self.diagnostic()).replace('"ready": true', '"ready": true, "ready": true') + '\n'); path.chmod(0o600)
            with self.assertRaises(ValueError): driver.RendererDiagnostics(path).poll()

    def test_owned_identity_rejects_wrong_executable_sampler_failure_and_timeout(self):
        binary = Path('/private/candidate/Trove.app/Contents/MacOS/Trove')
        with patch.object(driver.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=str(binary) + '\n')):
            self.assertTrue(driver.owned_executable_matches(42, binary))
        for result in (SimpleNamespace(returncode=0, stdout='/private/other/Trove\n'),
                       SimpleNamespace(returncode=1, stdout=str(binary))):
            with patch.object(driver.subprocess, 'run', return_value=result):
                self.assertFalse(driver.owned_executable_matches(42, binary))
        with patch.object(driver.subprocess, 'run', side_effect=OSError('sensitive')):
            self.assertFalse(driver.owned_executable_matches(42, binary))

    def test_collection_never_qualifies_missing_readiness_timeout_or_forced_exit(self):
        good = dict(process_exit=0, timed_out=False, interrupted=False, trace_valid=True,
                    unchanged=True, memory_available=True, memory_failed=False, missing=[],
                    capped=False, retained=True, worker_required=False, worker_seen=False,
                    readiness=True, diagnostics_valid=True)
        self.assertTrue(driver.collection_complete(**good))
        for bad in ({'readiness': False}, {'timed_out': True}, {'interrupted': True},
                    {'process_exit': -15}, {'diagnostics_valid': False}, {'missing': ['input_to_frames']},
                    {'worker_required': True}, {'capped': True}):
            self.assertFalse(driver.collection_complete(**(good | bad)))

    def test_driver_owned_lifecycle_normal_hidden_wrong_process_and_deadline(self):
        for scenario in ('normal', 'hidden', 'wrong-process', 'deadline', 'interrupt'):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as temp:
                root = Path(temp).resolve()
                app = root / 'Trove.app'; binary = app / 'Contents' / 'MacOS' / 'Trove'
                binary.parent.mkdir(parents=True); binary.write_bytes(b'fixture executable, never launched')
                output = root / 'evidence'
                test = self

                class Process:
                    pid = 777
                    returncode = None
                    calls = 0
                    terminated = False
                    did_interrupt = False

                    def __init__(self, argv, env, **kwargs):
                        test.assertEqual(argv[-1], str(binary))
                        test.assertTrue(Path(env['HOME']).is_relative_to(output))
                        test.assertTrue(Path(env['TMPDIR']).is_relative_to(output))
                        test.assertEqual(Path(kwargs['cwd']), Path(env['TMPDIR']))
                        test.assertTrue(Path(env['TROVE_MODEL_ROOT']).is_relative_to(output))
                        test.assertNotIn('OPENAI_API_KEY', env)
                        test.assertFalse(any(key.startswith('DYLD_') for key in env))
                        workspace = Path(env['TROVE_WORKSPACE']); workspace.mkdir(mode=0o700)
                        (workspace / '.owner.lock').write_bytes(b'')
                        (workspace / 'workspace.db').write_bytes(b'closed fake workspace')
                        trace = {'schemaVersion': 1, 'finalized': True, 'sampleLimitPerMetric': 512,
                                 'metrics': {'snapshot_dispatch': {'milliseconds': [10], 'dropped': 0}}}
                        driver.private_json(Path(env['TROVE_PERFORMANCE_REPORT']), trace)
                        self.path = Path(env['TROVE_PERFORMANCE_DIAGNOSTICS'])
                        self.state = test.diagnostic(visible=scenario != 'hidden')
                        self.path.write_text(json.dumps(self.state) + '\n'); self.path.chmod(0o600)

                    def finish(self, code):
                        if self.returncode is None:
                            self.returncode = code
                            with self.path.open('a') as out:
                                out.write(json.dumps(test.diagnostic(2, True, visible=scenario != 'hidden')) + '\n')

                    def poll(self):
                        self.calls += 1
                        if scenario == 'interrupt' and self.calls == 2 and not self.did_interrupt:
                            self.did_interrupt = True
                            raise KeyboardInterrupt()
                        if scenario not in ('deadline', 'interrupt') and self.calls > 5:
                            self.finish(0)
                        return self.returncode

                    def terminate(self): self.terminated = True; self.finish(-15)
                    def wait(self, timeout): return self.returncode
                    def kill(self): self.finish(-9)

                times = iter(range(0, 10000, 10 if scenario == 'deadline' else 1))
                argv = ['driver', '--app', str(app), '--scenario', 'idle', '--duration-seconds', '30',
                        '--minimum-samples', '1', '--output', str(output)]
                memory = {'available': True, 'appRssBytes': 1024, 'workerRssBytes': 0,
                          'otherDescendantRssBytes': 0, 'workerCount': 0, 'otherDescendantCount': 0}
                with patch.object(sys, 'argv', argv), patch.object(driver.platform, 'system', return_value='Darwin'), \
                     patch.object(driver.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stderr='Operation not permitted')), \
                     patch.object(driver.subprocess, 'Popen', Process), patch.object(driver, 'sample_memory', return_value=memory), \
                     patch.object(driver, 'owned_executable_matches', return_value=scenario != 'wrong-process'), \
                     patch.object(driver.time, 'sleep'), patch.object(driver.time, 'monotonic', side_effect=lambda: next(times)), \
                     contextlib.redirect_stdout(io.StringIO()):
                    code = driver.main()
                result = json.loads((output / 'result.json').read_text())
                self.assertEqual(code, 0 if scenario == 'normal' else 1)
                self.assertEqual(result['localObservationRecorded'], scenario == 'normal')
                self.assertFalse(result['sampleAccountingComplete'])
                self.assertFalse(result['actualForegroundWindowIndependentlyObserved'])
                self.assertEqual((output / 'readiness.json').exists(), result['rendererReadinessObserved'])
                self.assertEqual(json.loads((output / 'process.json').read_text())['ownedProcessId'], 777)
                if scenario == 'deadline': self.assertTrue(result['timedOut'])
                if scenario == 'interrupt': self.assertTrue(result['interrupted'])

    def test_memory_retains_only_owned_aggregate_metrics(self):
        rows = '10 1 100 /private/Trove.app/Contents/MacOS/Trove\n11 10 50 /private/llama-worker\n12 11 5 /private/helper with spaces\n13 1 999999 /private/unrelated-secret\n14 10 7 /private/microphone-worker\n'
        result = driver.owned_memory(rows, 10)
        self.assertEqual(result, {'available': True, 'appRssBytes': 102400, 'workerRssBytes': 58368,
                                 'otherDescendantRssBytes': 5120, 'workerCount': 2, 'otherDescendantCount': 1})
        self.assertNotIn('private', json.dumps(result))
        self.assertNotIn('pid', json.dumps(result).lower())
        with self.assertRaises(ValueError): driver.owned_memory(rows, 999)
        with self.assertRaises(ValueError): driver.owned_memory('10 1 -1 secret', 10)
        with self.assertRaises(ValueError): driver.owned_memory('10 1 100 /private/unrelated', 10)
        oversized = '10 1 100 /private/Trove\n' + ''.join(f'{n} 10 1 /private/llama-worker\n' for n in range(11, 524))
        with self.assertRaises(ValueError): driver.owned_memory(oversized, 10)

    def test_missing_memory_is_unavailable_never_zero(self):
        with patch.object(driver.subprocess, 'run', side_effect=OSError('private sensitive error')):
            result = driver.sample_memory(123)
        self.assertEqual(result, {'available': False, 'reason': 'process_sample_unavailable'})

    def test_trace_rejects_unknown_fields_labels_nonfinite_and_unfinalized_data(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'trace.json'
            valid = {'schemaVersion': 1, 'finalized': True, 'sampleLimitPerMetric': 512,
                     'metrics': {'database_bridge': {'milliseconds': [10, 20], 'dropped': 0}}}
            path.write_text(json.dumps(valid))
            self.assertEqual(driver.summarize(driver.read_trace(path))['database_bridge']['medianMs'], 15)
            invalid = [dict(valid, finalized=False), dict(valid, schemaVersion=True), dict(valid, title='private'),
                       dict(valid, metrics={'private title': {'milliseconds': [1], 'dropped': 0}}),
                       dict(valid, metrics={'database_bridge': {'milliseconds': [float('nan')], 'dropped': 0}}),
                       dict(valid, metrics={'database_bridge': {'milliseconds': [True], 'dropped': 0}}),
                       dict(valid, metrics={'database_bridge': {'milliseconds': [1] * 513, 'dropped': 0}})]
            for item in invalid:
                path.write_text(json.dumps(item))
                with self.assertRaises(ValueError): driver.read_trace(path)
            path.write_text('{"schemaVersion":1,"schemaVersion":1,"finalized":true,"sampleLimitPerMetric":512,"metrics":{}}')
            with self.assertRaises(ValueError): driver.read_trace(path)

    def test_existing_output_and_input_links_are_preserved(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            path = root / 'result.json'
            driver.private_json(path, {'original': True})
            with self.assertRaises(FileExistsError): driver.private_json(path, {'changed': True})
            self.assertEqual(json.loads(path.read_text()), {'original': True})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            link = root / 'alias'
            link.symlink_to(path)
            with self.assertRaises(FileExistsError): driver.private_json(link, {'changed': True})
            with self.assertRaises(ValueError): driver.inventory(root)

    def test_cloning_requires_closed_fixture_and_preserves_bytes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / 'source'; source.mkdir(mode=0o700)
            (source / '.owner.lock').write_bytes(b'')
            (source / 'workspace.db').write_bytes(b'independent fixture bytes')
            with (source / '.owner.lock').open('rb') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaises(BlockingIOError): driver.clone_closed_workspace(source, root / 'blocked')
            before = driver.clone_closed_workspace(source, root / 'copy')
            self.assertEqual(driver.inventory(source), before)
            self.assertEqual(driver.inventory(root / 'copy'), before)
            retained = driver.retain_closed_workspace(root / 'copy')
            self.assertEqual(retained['inventory'], before)
            self.assertTrue(retained['fsynced'])

    def test_retention_refuses_live_and_linked_workspaces(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = driver.private_directory(root / 'runtime')
            (source / '.owner.lock').write_bytes(b'')
            (source / 'workspace.db').write_bytes(b'bytes which must remain unchanged')
            before = driver.inventory(source)
            with (source / '.owner.lock').open('rb') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaises(BlockingIOError):
                    driver.retain_closed_workspace(source)
            alias = root / 'alias'; alias.symlink_to(source, target_is_directory=True)
            with self.assertRaises(ValueError): driver.retain_closed_workspace(alias)
            with self.assertRaises(FileExistsError): driver.private_directory(source)
            self.assertEqual(driver.inventory(source), before)


if __name__ == '__main__':
    unittest.main()
