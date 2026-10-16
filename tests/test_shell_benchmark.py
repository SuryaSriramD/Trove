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
spec = importlib.util.spec_from_file_location('shell_benchmark', ROOT / 'scripts/benchmark-shell.py')
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


class ShellBenchmarkTests(unittest.TestCase):
    def run_scenarios(self, scenarios, iterations, **options):
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

            def __init__(self, _args, *, env, cwd, stdout, stderr):
                self.scenario = next(remaining)
                assert Path(env['HOME']).is_relative_to(fixture.resolve())
                assert Path(env['TMPDIR']).is_relative_to(fixture.resolve())
                assert Path(cwd) == Path(env['TMPDIR'])
                assert Path(env['TROVE_MODEL_ROOT']).is_relative_to(fixture.resolve())
                assert 'OPENAI_API_KEY' not in env
                assert not any(key.startswith('DYLD_') for key in env)
                self.returncode = None
                self.waits = 0
                workspace = Path(env['TROVE_WORKSPACE'])
                workspace.mkdir(mode=0o700, exist_ok=True)
                (workspace / '.owner.lock').touch()
                (workspace / 'workspace.db').write_bytes(b'closed synthetic workspace')
                if 'report' in self.scenario:
                    Path(env['TROVE_SHELL_REPORT']).write_text(json.dumps(self.scenario['report']))
                stdout.write('synthetic native log\n')

            def wait(self, timeout=None):
                self.waits += 1
                if self.waits <= self.scenario.get('timeoutWaits', 1 if self.scenario.get('timeout') else 0):
                    raise subprocess.TimeoutExpired('synthetic app', timeout)
                self.returncode = self.scenario.get('exitCode', 0)
                return self.returncode

            def terminate(self):
                # Wails can handle termination and exit cleanly. That must not
                # turn the preceding startup timeout into a passing sample.
                self.returncode = 0

            def kill(self):
                self.returncode = -9

        with patch.object(benchmark.subprocess, 'Popen', Process), patch.object(benchmark, 'capture_stalled_process') as sampler:
            result = benchmark.measure(app, [{'pages': 0, 'paragraphBlocks': 0, 'workspacePath': None}],
                                     iterations, fixture, root / 'durable-diagnostics', timeout=.1, **options)
            self.sample_calls = sampler.call_count
            return result

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
        self.assertTrue(result['shellTargetPassed'])
        self.assertFalse(result['instrumented'])
        self.assertEqual(self.sample_calls, 0)
        retained = result['profiles'][0]['retainedWorkspace']
        self.assertTrue(retained['fsynced'])
        self.assertTrue(retained['unchangedDuringRetention'])
        self.assertEqual(retained['inventory']['bytes'], len(b'closed synthetic workspace'))

    def test_retention_failure_does_not_promote_fast_samples(self):
        with patch.object(benchmark, 'retain_closed_workspace', side_effect=ValueError('changed fixture')):
            result = self.run_scenarios([{'report': {'usableShellMs': 1000}}], 1)
        self.assertFalse(result['measurementPassed'])
        self.assertFalse(result['shellTargetPassed'])
        self.assertEqual(result['profiles'][0]['completedIterations'], 1)
        self.assertTrue(result['profiles'][0]['sampleCollectionComplete'])
        self.assertFalse(result['profiles'][0]['complete'])
        self.assertFalse(result['profiles'][0]['approximatelyTwoSeconds'])
        self.assertIsNone(result['profiles'][0]['retainedWorkspace'])
        self.assertIn('changed fixture', result['failures'][0]['reason'])

    def test_complete_slow_samples_are_valid_measurements_but_miss_target(self):
        result = self.run_scenarios([{'report': {'usableShellMs': 2700}}], 1)
        self.assertTrue(result['measurementPassed'])
        self.assertFalse(result['profiles'][0]['approximatelyTwoSeconds'])
        self.assertFalse(result['shellTargetPassed'])

    def test_cli_fails_a_complete_measurement_above_the_target(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            profiles = root / 'profiles.json'
            profiles.write_text('{"profiles": []}')
            output = root / 'result.json'
            result = {'measurementPassed': True, 'shellTargetPassed': False,
                      'instrumented': False, 'profiles': [{'approximatelyTwoSeconds': False}]}
            with patch('sys.argv', ['benchmark-shell', '--app', str(root / 'Trove.app'),
                                   '--profiles', str(profiles), '--output', str(output)]), \
                 patch.object(benchmark, 'measure', return_value=result):
                self.assertEqual(benchmark.main(), 1)
            self.assertEqual(json.loads(output.read_text()), result)
            fixtures = root / 'result-fixtures'
            self.assertTrue(fixtures.is_dir())
            self.assertEqual(fixtures.stat().st_mode & 0o777, 0o700)
            output.unlink()
            with patch('sys.argv', ['benchmark-shell', '--app', str(root / 'Trove.app'),
                                   '--profiles', str(profiles), '--output', str(output)]), \
                 patch.object(benchmark, 'measure') as measure:
                with self.assertRaises(FileExistsError):
                    benchmark.main()
                measure.assert_not_called()

    def test_diagnostic_sampling_is_not_a_passing_timing_run(self):
        result = self.run_scenarios([{'timeout': True, 'report': {'usableShellMs': 1200}}], 1,
                                    diagnostic_slow_ms=25)
        self.assertTrue(result['measurementPassed'])
        self.assertTrue(result['instrumented'])
        self.assertFalse(result['shellTargetPassed'])
        self.assertTrue(result['profiles'][0]['samples'][0]['diagnosticSamplingAttempted'])
        self.assertFalse(result['releaseQualified'])
        self.assertEqual(self.sample_calls, 1)

    def test_diagnostic_mode_cannot_promote_even_without_sampling(self):
        result = self.run_scenarios([{'report': {'usableShellMs': 1200}}], 1,
                                    diagnostic_slow_ms=25)
        self.assertTrue(result['measurementPassed'])
        self.assertTrue(result['instrumented'])
        self.assertFalse(result['shellTargetPassed'])
        self.assertFalse(result['profiles'][0]['samples'][0]['diagnosticSamplingAttempted'])

    def test_diagnostic_sampling_never_overwrites_its_first_sample_on_timeout(self):
        result = self.run_scenarios([{'timeoutWaits': 2, 'report': {'usableShellMs': 1200}}], 1,
                                    diagnostic_slow_ms=25)
        self.assertFalse(result['measurementPassed'])
        self.assertTrue(result['failures'][0]['timedOut'])
        self.assertFalse(result['shellTargetPassed'])
        self.assertEqual(self.sample_calls, 1)

    def test_diagnostic_trigger_must_be_bounded_before_process_deadline(self):
        for value in [0, -1, float('nan'), float('inf'), 100]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.run_scenarios([], 1, diagnostic_slow_ms=value)

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
