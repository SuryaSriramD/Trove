import importlib.util
import os
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('database_benchmark', ROOT / 'scripts/benchmark-database.py')
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


class DatabaseBenchmarkRetentionTests(unittest.TestCase):
    def test_retention_is_explicit_and_never_reuses_or_resolves_a_final_link(self):
        self.assertIsNone(benchmark.retention_destination(None))
        with self.assertRaises(ValueError): benchmark.retention_destination(Path('relative'))
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            destination = root / 'fixture'
            self.assertEqual(benchmark.retention_destination(destination), destination)
            destination.mkdir()
            with self.assertRaises(ValueError): benchmark.retention_destination(destination)
            link = root / 'dangling'; link.symlink_to(root / 'missing')
            with self.assertRaises(ValueError): benchmark.retention_destination(link)
            self.assertTrue(os.path.lexists(link))

    def test_missing_mismatched_or_unreopenable_retention_never_completes(self):
        self.assertTrue(benchmark.retention_complete(None, {}))
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp).resolve() / 'fixture'
            retained = {'retainedFixture': {'ready': True, 'outsideTimedSamples': True, 'workspacePath': str(path)}}
            self.assertFalse(benchmark.retention_complete(path, retained))
            path.mkdir()
            self.assertFalse(benchmark.retention_complete(path, retained))
            (path / 'workspace.db').write_bytes(b'test receipt only; native Go test proves reopening')
            self.assertTrue(benchmark.retention_complete(path, retained))
            self.assertFalse(benchmark.retention_complete(path, {}))
            retained['retainedFixture']['workspacePath'] = str(path / 'other')
            self.assertFalse(benchmark.retention_complete(path, retained))


if __name__ == '__main__': unittest.main()
