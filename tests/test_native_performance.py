import fcntl
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('native_performance', ROOT / 'scripts/record-native-performance.py')
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)


class NativePerformanceTests(unittest.TestCase):
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


if __name__ == '__main__':
    unittest.main()
