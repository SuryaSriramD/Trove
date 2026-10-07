"""Exercise runner contracts with recorded fake service outcomes, never models."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('service_benchmark', ROOT / 'scripts/benchmark-phase3.py')
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


class ServiceBenchmarkTests(unittest.TestCase):
    def invoke(self, result, *, existing=False):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        native = root / 'native'
        native.mkdir()
        (native / 'manifest.json').write_text('{}')
        models = root / 'models'
        models.mkdir()
        (models / 'embedding.model').write_bytes(b'fixture weights never executed')
        catalog = root / 'native/models'
        catalog.mkdir()
        (catalog / 'catalog.json').write_text(json.dumps([{
            'id': 'embedding', 'kind': 'embedding',
            'sha256': benchmark.sha(models / 'embedding.model')
        }]))
        output = root / 'evidence'
        if existing:
            output.mkdir()
            (output / 'benchmark-service.json').write_bytes(b'prior evidence')
        calls = []
        def run(command, **kwargs):
            calls.append(command)
            if 'go' == command[0]:
                Path(command[command.index('-output') + 1]).write_text(json.dumps(result))
            return subprocess.CompletedProcess(command, 0, stdout='fixture host', stderr='')
        args = ['benchmark-phase3', '--native-bundle', str(native), '--models', str(models),
                '--output', str(output), '--pages', '100,1000,10000', '--iterations', '5']
        with patch('sys.argv', args), patch.object(benchmark, 'ROOT', root), \
             patch.object(benchmark, 'inputs', return_value={'source.go': 'unchanged'}), \
             patch.object(benchmark, 'cache_dir', return_value=root), \
             patch.object(benchmark, 'models_dir', return_value=models), \
             patch.object(benchmark, 'go_binary', return_value='go'), \
             patch.object(benchmark, 'build_env', return_value={}), \
             patch.object(benchmark.subprocess, 'run', side_effect=run):
            try:
                code = benchmark.main()
            except SystemExit as exc:
                code = exc.code
        return code, calls, output

    @staticmethod
    def profiles():
        return {'profiles': [dict(pages=n,
                    keyword={'count': 5, 'samplesMs': [1.0] * 5},
                    hybrid={'count': 30, 'samplesMs': [2.0] * 30})
                for n in [100, 1000, 10000]]}

    def test_all_scales_request_explicit_hybrid_iteration_count(self):
        code, calls, output = self.invoke(self.profiles())
        self.assertEqual(code, 0)
        command = next(c for c in calls if c[0] == 'go')
        self.assertIn('-hybrid-iterations', command)
        self.assertEqual(command[command.index('-hybrid-iterations') + 1], '30')
        report = json.loads((output / 'benchmark-environment.json').read_text())
        self.assertTrue(report['profileCompletenessPassed'])

    def test_first_profile_only_hybrid_is_an_incomplete_measurement(self):
        result = self.profiles()
        del result['profiles'][1]['hybrid']
        del result['profiles'][2]['hybrid']
        code, _, output = self.invoke(result)
        self.assertEqual(code, 1)
        report = json.loads((output / 'benchmark-environment.json').read_text())
        self.assertFalse(report['qualifiedMeasurement'])
        self.assertFalse(report['profileCompletenessPassed'])

    def test_wrong_sample_count_or_duplicate_profile_cannot_pass(self):
        for variant in ('short', 'duplicate'):
            with self.subTest(variant=variant):
                result = self.profiles()
                if variant == 'short':
                    result['profiles'][2]['hybrid']['samplesMs'].pop()
                else:
                    result['profiles'][2]['pages'] = 100
                code, _, _ = self.invoke(result)
                self.assertEqual(code, 1)

    def test_existing_evidence_is_refused_before_any_host_or_worker_command(self):
        code, calls, output = self.invoke(self.profiles(), existing=True)
        self.assertNotEqual(code, 0)
        self.assertEqual(calls, [])
        self.assertEqual((output / 'benchmark-service.json').read_bytes(), b'prior evidence')


if __name__ == '__main__':
    unittest.main()
