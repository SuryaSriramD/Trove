import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('shell_benchmark', ROOT / 'scripts/benchmark-shell.py')
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


class ShellBenchmarkTests(unittest.TestCase):
    def run_scenarios(self, scenarios, iterations):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        app = root / 'Trove.app'
        binary = app / 'Contents/MacOS/Trove'
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b'fixed synthetic app identity')
        fixture = root / 'workspaces'
        fixture.mkdir()
        remaining = iter(scenarios)

        class Process:
            pid = 424242

            def __init__(self, _args, *, env, stdout, stderr):
                self.scenario = next(remaining)
                self.returncode = None
                self.waits = 0
                if 'report' in self.scenario:
                    Path(env['TROVE_SHELL_REPORT']).write_text(json.dumps(self.scenario['report']))
                stdout.write('synthetic native log\n')

            def wait(self, timeout=None):
                self.waits += 1
                if self.scenario.get('timeout') and self.waits == 1:
                    raise subprocess.TimeoutExpired('synthetic app', timeout)
                self.returncode = self.scenario.get('exitCode', 0)
                return self.returncode

            def terminate(self):
                # Wails can handle termination and exit cleanly. That must not
                # turn the preceding startup timeout into a passing sample.
                self.returncode = 0

            def kill(self):
                self.returncode = -9

        with patch.object(benchmark.subprocess, 'Popen', Process), patch.object(benchmark, 'capture_stalled_process'):
            return benchmark.measure(app, [{'pages': 0, 'paragraphBlocks': 0, 'workspacePath': None}],
                                     iterations, fixture, root / 'durable-diagnostics', timeout=.1)

    def test_partial_profile_never_claims_two_second_target(self):
        result = self.run_scenarios([{'report': {'usableShellMs': 1000}}, {}], 5)
        profile = result['profiles'][0]
        self.assertFalse(result['measurementPassed'])
        self.assertFalse(profile['approximatelyTwoSeconds'])
        self.assertFalse(profile['complete'])
        self.assertEqual((profile['completedIterations'], profile['attemptedIterations'], profile['requestedIterations']), (1, 2, 5))
        self.assertEqual(result['failures'][0]['exitCode'], 0)
        self.assertTrue(Path(result['failures'][0]['log']).is_file())

    def test_timeout_is_failure_even_with_report_and_clean_exit(self):
        result = self.run_scenarios([{'timeout': True, 'report': {'usableShellMs': 1200}}], 1)
        self.assertFalse(result['measurementPassed'])
        self.assertEqual(result['profiles'][0]['completedIterations'], 0)
        failure = result['failures'][0]
        self.assertTrue(failure['timedOut'])
        self.assertEqual(failure['exitCode'], 0)
        self.assertEqual(failure['shellReport']['usableShellMs'], 1200)

    def test_complete_fast_samples_pass(self):
        result = self.run_scenarios([{'report': {'usableShellMs': n}} for n in [900, 1400]], 2)
        self.assertTrue(result['measurementPassed'])
        self.assertTrue(result['profiles'][0]['approximatelyTwoSeconds'])
        self.assertEqual(result['profiles'][0]['medianMs'], 1150)

    def test_complete_slow_samples_are_valid_measurements_but_miss_target(self):
        result = self.run_scenarios([{'report': {'usableShellMs': 2700}}], 1)
        self.assertTrue(result['measurementPassed'])
        self.assertFalse(result['profiles'][0]['approximatelyTwoSeconds'])

    def test_invalid_report_fails_without_discarding_prior_samples(self):
        for value in [float('nan'), -1, True, '1000']:
            with self.subTest(value=value):
                result = self.run_scenarios([{'report': {'usableShellMs': 1000}}, {'report': {'usableShellMs': value}}], 2)
                self.assertFalse(result['measurementPassed'])
                self.assertEqual(result['profiles'][0]['completedIterations'], 1)
                self.assertIn('Invalid or missing shell report', result['failures'][0]['reason'])

    def test_existing_evidence_is_never_reused_or_overwritten(self):
        for suffix in ['.log', '.trace', '.sample', '.json', '.symlink']:
            with self.subTest(suffix=suffix), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                app = root / 'Trove.app'
                binary = app / 'Contents/MacOS/Trove'
                binary.parent.mkdir(parents=True)
                binary.write_bytes(b'new candidate')
                fixture = root / 'fixtures'
                diagnostics = root / 'diagnostics'
                fixture.mkdir()
                diagnostics.mkdir()
                prior = fixture / '0-1.json' if suffix == '.json' else diagnostics / ('0-1' + suffix)
                if suffix == '.symlink':
                    prior = diagnostics / '0-1.trace'
                    prior.symlink_to(root / 'missing-target')
                else:
                    prior.write_bytes(b'prior immutable evidence')
                with patch.object(benchmark.subprocess, 'Popen') as start:
                    with self.assertRaisesRegex(ValueError, 'already exists'):
                        benchmark.measure(app, [{'pages': 0, 'paragraphBlocks': 0, 'workspacePath': None}],
                                          2, fixture, diagnostics)
                    start.assert_not_called()
                if suffix == '.symlink':
                    self.assertTrue(prior.is_symlink())
                else:
                    self.assertEqual(prior.read_bytes(), b'prior immutable evidence')
                self.assertFalse((diagnostics / '0-0.log').exists())

    def test_no_requested_samples_is_invalid(self):
        with self.assertRaises(ValueError):
            self.run_scenarios([], 0)


if __name__ == '__main__':
    unittest.main()
