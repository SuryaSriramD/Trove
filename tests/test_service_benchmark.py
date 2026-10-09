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
    def invoke(self, result, *, existing=False, native_setup=None, during_run=None):
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
        if native_setup:
            native_setup(native)
        snapshot_root = root / 'snapshot'
        snapshot_root.mkdir()
        output = root / 'evidence'
        if existing:
            output.mkdir()
            (output / 'benchmark-service.json').write_bytes(b'prior evidence')
        calls = []
        def run(command, **kwargs):
            calls.append(command)
            if 'go' == command[0]:
                if during_run:
                    during_run(native, Path(command[command.index('-bundle') + 1]))
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
             patch.object(benchmark.tempfile, 'mkdtemp', return_value=str(snapshot_root)), \
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

    def test_complete_copy_matches_frozen_inventory_including_appledouble_and_dot_executable(self):
        from qualification_helpers import helper_inventory, validate_helper_copy
        def setup(native):
            for name in ('._catalog.json', '._provenance.json'):
                (native / 'models' / name).write_bytes(bytes.fromhex('0005160700020000') + bytes(4088))
            (native / '._worker').write_bytes(b'executable content with a misleading metadata name')
            (native / '.DS_Store').write_bytes(b'Finder metadata is also retained')
        code, _, output = self.invoke(self.profiles(), native_setup=setup)
        self.assertEqual(code, 0)
        report = json.loads((output / 'benchmark-environment.json').read_text())
        frozen = helper_inventory(Path(report['nativeBundleSource']))
        validate_helper_copy(report['nativeBundleCopy'], frozen)
        self.assertEqual(report['nativeResourceSha256'], {p: v['sha256'] for p, v in frozen['files'].items()})
        self.assertEqual(report['nativeBundleCopy']['sourceInventory'], frozen)
        self.assertEqual(report['nativeBundleCopy']['snapshotInventory'], frozen)
        for name in ('models/._catalog.json', 'models/._provenance.json', '._worker', '.DS_Store'):
            self.assertIn(name, report['nativeResourceSha256'])

    def test_source_or_snapshot_changes_make_zero_exit_run_unqualified(self):
        for target, mutation in [('source', 'change'), ('snapshot', 'change'),
                                 ('snapshot', 'remove'), ('snapshot', 'add'), ('snapshot', 'link')]:
            with self.subTest(target=target, mutation=mutation):
                def mutate(source, snapshot):
                    root = source if target == 'source' else snapshot
                    worker = root / '._worker'
                    if mutation == 'change':
                        worker.write_bytes(b'changed worker')
                    elif mutation == 'remove':
                        worker.unlink()
                    elif mutation == 'add':
                        (root / 'unexpected-worker').write_bytes(b'new executable')
                    else:
                        worker.unlink()
                        worker.symlink_to(source / 'manifest.json')
                code, _, output = self.invoke(self.profiles(),
                    native_setup=lambda native: (native / '._worker').write_bytes(b'original worker'),
                    during_run=mutate)
                self.assertEqual(code, 1)
                report = json.loads((output / 'benchmark-environment.json').read_text())
                self.assertEqual(report['exitCode'], 0)
                self.assertFalse(report['qualifiedMeasurement'])
                self.assertTrue(report['metadataErrors'])
                copy = report['nativeBundleCopy']
                if mutation == 'link':
                    self.assertEqual(copy['validationErrors'], ['snapshot_inventory_unreadable_or_unsafe'])
                else:
                    expected = 'unexpected-worker' if mutation == 'add' else '._worker'
                    self.assertIn(expected, copy[target + 'Changes'])


if __name__ == '__main__':
    unittest.main()
