import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from native_targets import cpu_options, host_target, require_native_host

class NativeTargets(unittest.TestCase):
    def test_target_builds_require_matching_host(self):
        for target, system, machine in (
            ('darwin-arm64', 'Darwin', 'arm64'),
            ('windows-amd64', 'Windows', 'AMD64'),
            ('linux-amd64', 'Linux', 'x86_64'),
        ):
            self.assertEqual(target, host_target(system, machine))
            require_native_host(target, system, machine)
        with self.assertRaises(ValueError):
            require_native_host('windows-amd64', 'Darwin', 'arm64')
        with self.assertRaises(ValueError):
            host_target('Linux', 'aarch64')

    def test_plans_keep_target_execution_unqualified(self):
        for target in ('windows-amd64', 'linux-amd64'):
            result = subprocess.run([sys.executable, str(ROOT / 'scripts/build-native-portable.py'),
                '--target', target, '--plan'], check=True, capture_output=True, text=True)
            body = json.loads(result.stdout)
            self.assertIn('device execution unqualified', body['capture'])
            self.assertIn('target execution unqualified', body['audioDecoding'])
            self.assertIn('target execution unqualified', body['ocr'])
            self.assertIn('No platform execution', body['qualification'])

    def test_microphone_plans_keep_fixture_device_boundary(self):
        for target in ('windows-amd64', 'linux-amd64', 'darwin-arm64'):
            for fixture in (False, True):
                command = [sys.executable, str(ROOT / 'scripts/build-microphone.py'),
                    '--target', target, '--output', 'unused-plan-output', '--plan']
                if fixture:
                    command.append('--fixture-only')
                result = subprocess.run(command, check=True, capture_output=True, text=True)
                body = json.loads(result.stdout)
                self.assertEqual(fixture, body['fixtureOnly'])
                self.assertIn('Unqualified', body['deviceExecution'])
                self.assertEqual('0.11.25', body['pin']['version'])

    def test_portable_isa_and_pinned_sources(self):
        for flag in ('GGML_NATIVE', 'GGML_AVX', 'GGML_AVX2', 'GGML_BMI2', 'GGML_FMA', 'GGML_F16C', 'GGML_METAL'):
            self.assertIn('-D' + flag + '=OFF', cpu_options())
        spec = importlib.util.spec_from_file_location('portable_builder', ROOT / 'scripts/build-native-portable.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError, 'Missing verified source archive'):
                module.verified_source(Path(directory), 'llama', {'sha256': '0' * 64})

if __name__ == '__main__':
    unittest.main()
