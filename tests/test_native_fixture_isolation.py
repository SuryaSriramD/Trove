"""Real synthetic child processes; no packaged app, credentials or OS permissions."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from native_fixture_environment import native_fixture_environment, validate_native_fixture_environment

spec = importlib.util.spec_from_file_location('verify_packaged', ROOT / 'scripts/verify-packaged.py')
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)

CHILD = '''import json, os, pathlib, tempfile, time
home = pathlib.Path.home()
config = home / 'Library/Application Support/Trove/reminders'
config.mkdir(parents=True, exist_ok=True)
(config / 'desktop.json').write_text('synthetic child state')
workspace = pathlib.Path(os.environ['TROVE_WORKSPACE'])
workspace.mkdir(exist_ok=True)
(workspace / 'fixture.txt').write_text('persisted synthetic state')
step = os.environ['TROVE_INTEGRATION_STEP']
result = {'step': step, 'passed': True, 'observed': {'environment': dict(os.environ),
          'temporary': tempfile.gettempdir(), 'config': str(config)}}
pathlib.Path(os.environ['TROVE_INTEGRATION_REPORT']).write_text(json.dumps(result))
'''


class NativeFixtureIsolationTests(unittest.TestCase):
    def app(self, root, extra=''):
        app = root / 'Trove.app'
        binary = app / 'Contents/MacOS/Trove'
        binary.parent.mkdir(parents=True)
        binary.write_text('#!' + sys.executable + '\n' + CHILD + extra)
        binary.chmod(0o700)
        helper = app / 'Contents/Resources/native/fixture-worker'
        helper.parent.mkdir(parents=True)
        helper.write_bytes(b'sealed synthetic helper')
        return app

    def args(self, root, app, **changes):
        return SimpleNamespace(app=app, evidence=root / 'evidence', models=None,
                               native=False, offline=False, p0=True, p1=False, p2=False, **changes)

    def invoke(self, args, **kwargs):
        with contextlib.redirect_stdout(io.StringIO()):
            code = driver.run(args, **kwargs)
        return code, json.loads((args.evidence / 'packaged-integration.json').read_text())

    def test_spawned_child_private_home_temp_workspace_models_and_parent_untouched(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            old_home = root / 'parent-home'
            reminder = old_home / 'Library/Application Support/Trove/reminders/desktop.json'
            reminder.parent.mkdir(parents=True)
            reminder.write_bytes(b'{"active":true,"binding":"synthetic-parent"}')
            hostile = {'HOME': str(old_home), 'TMPDIR': str(root), 'OPENAI_API_KEY': 'fake-test-secret',
                       'ANTHROPIC_API_KEY': 'fake-provider-secret', 'GH_TOKEN': 'fake-token',
                       'HTTP_PROXY': 'http://unwanted.invalid', 'TROVE_WORKSPACE': str(root / 'wrong-workspace'),
                       'TROVE_MODEL_ROOT': str(root / 'wrong-models'), 'TROVE_PROVIDER_CONFIG': 'fake-config',
                       'DYLD_INSERT_LIBRARIES': '/fake/injection', 'XDG_CONFIG_HOME': str(old_home),
                       'PYTHONPATH': '/fake/python/injection'}
            before = reminder.read_bytes()
            args = self.args(root, self.app(root))
            with patch.dict(os.environ, hostile):
                parent_before = dict(os.environ)
                code, report = self.invoke(args)
                self.assertEqual(dict(os.environ), parent_before)
            self.assertEqual(code, 0)
            self.assertEqual(reminder.read_bytes(), before)
            self.assertFalse((root / 'wrong-workspace').exists())
            self.assertFalse((root / 'wrong-models').exists())
            self.assertTrue(report['inputsUnchanged'])
            self.assertFalse(report['humanAcceptance'])
            self.assertFalse(report['isolation']['osGlobalServicesIsolated'])
            for step in report['checks']:
                observed = step['observed']; env = observed['environment']
                self.assertEqual(env['HOME'], report['isolation']['home'])
                self.assertEqual(Path(observed['temporary']), Path(report['isolation']['temporary']))
                self.assertTrue(Path(env['HOME']).is_relative_to(args.evidence))
                self.assertEqual(Path(env['HOME']).stat().st_mode & 0o777, 0o700)
                self.assertTrue(Path(env['TROVE_MODEL_ROOT']).is_relative_to(args.evidence))
                self.assertEqual(Path(observed['config']).parent, Path(report['isolation']['darwinUserConfigDir']) / 'Trove')
                for name in ('OPENAI_API_KEY', 'ANTHROPIC_API_KEY', 'GH_TOKEN', 'HTTP_PROXY',
                             'TROVE_PROVIDER_CONFIG', 'DYLD_INSERT_LIBRARIES', 'PYTHONPATH'):
                    self.assertNotIn(name, env)
                self.assertFalse(step['installedWeightsAvailable'])
            self.assertFalse(report['checks'][0]['fixtureInventoryBefore']['exists'])
            self.assertEqual(report['checks'][0]['fixtureInventoryAfter'], report['checks'][1]['fixtureInventoryBefore'])
            self.assertIn('Contents/Resources/native/fixture-worker', report['inputIdentityBefore']['appFiles'])
            self.assertIn('scripts/native_fixture_environment.py', report['inputIdentityBefore']['harnessAndFixtureSourceFiles'])

    def test_existing_empty_nonempty_and_linked_evidence_refused_before_child(self):
        for kind in ('empty', 'nonempty', 'link', 'dangling'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                root = Path(temp); args = self.args(root, self.app(root))
                if kind in ('empty', 'nonempty'):
                    args.evidence.mkdir()
                    if kind == 'nonempty': (args.evidence / 'prior.json').write_text('preserved')
                else:
                    target = root / 'target'
                    if kind == 'link': target.mkdir()
                    args.evidence.symlink_to(target)
                with patch.object(driver, 'run_child') as child, self.assertRaises(FileExistsError):
                    driver.run(args)
                child.assert_not_called()
                if kind == 'nonempty': self.assertEqual((args.evidence / 'prior.json').read_text(), 'preserved')

    def test_changed_helper_cannot_pass_or_launch_second_step(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            app = self.app(root, "pathlib.Path(__file__).parents[1].joinpath('Resources/native/fixture-worker').write_bytes(b'changed')\n")
            code, result = self.invoke(self.args(root, app))
            self.assertEqual(code, 1)
            self.assertFalse(result['passed']); self.assertFalse(result['inputsUnchanged'])
            self.assertEqual(len(result['checks']), 1)
            self.assertIn('bound inputs changed before step p0-verify', result['errors'])

    def test_wrong_report_step_is_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            app = self.app(root, "result['step']='wrong'; pathlib.Path(os.environ['TROVE_INTEGRATION_REPORT']).write_text(json.dumps(result))\n")
            code, result = self.invoke(self.args(root, app))
            self.assertEqual(code, 1); self.assertEqual(len(result['checks']), 1)
            self.assertFalse(result['checks'][0]['passed'])
            self.assertIn('wrong step identity', result['checks'][0]['error'])

    def test_timeout_never_promotes_preexisting_success_report(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            app = self.app(root, 'time.sleep(30)\n')
            code, result = self.invoke(self.args(root, app), timeout=.3)
            self.assertEqual(code, 1)
            self.assertTrue(result['checks'][0]['timedOut'])
            self.assertFalse(result['checks'][0]['passed'])
            self.assertTrue(result['inputsUnchanged'])

    def test_missing_final_input_is_failed_receipt_not_discarded_run(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            app = self.app(root, "pathlib.Path(__file__).unlink()\n")
            code, result = self.invoke(self.args(root, app))
            self.assertEqual(code, 1); self.assertFalse(result['inputsUnchanged'])
            self.assertEqual(len(result['checks']), 1)
            self.assertTrue((root / 'evidence/packaged-started.json').is_file())

    def test_external_model_root_only_reaches_explicit_native_step(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve(); models = root / 'selected-models'; models.mkdir()
            (models / 'fixture-model').write_bytes(b'not an actual model')
            args = self.args(root, self.app(root)); args.models = models; args.native = True
            code, result = self.invoke(args)
            self.assertEqual(code, 0)
            self.assertEqual([x['installedWeightsAvailable'] for x in result['checks']], [False, False, True])
            self.assertEqual(result['checks'][2]['modelRoot'], str(models))
            self.assertTrue(result['inputsUnchanged'])
            self.assertEqual((models / 'fixture-model').read_bytes(), b'not an actual model')

    def test_environment_refuses_reuse_link_and_late_home_substitution(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            env, receipt = native_fixture_environment(root / 'runtime', root / 'workspace')
            with self.assertRaises(FileExistsError): native_fixture_environment(root / 'runtime', root / 'workspace')
            (root / 'linked').symlink_to(root / 'runtime')
            with self.assertRaises(FileExistsError): native_fixture_environment(root / 'linked', root / 'workspace')
            home = Path(env['HOME']); home.rename(home.with_name('original-home'))
            home.symlink_to(home.with_name('original-home'))
            with self.assertRaises(ValueError): validate_native_fixture_environment(env, receipt)

    def test_environment_requires_private_permissions_and_matching_paths(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            env, receipt = native_fixture_environment(root / 'runtime', root / 'workspace')
            wrong = dict(env, HOME=str(root))
            with self.assertRaises(ValueError): validate_native_fixture_environment(wrong, receipt)
            Path(env['TMPDIR']).chmod(0o755)
            with self.assertRaises(ValueError): validate_native_fixture_environment(env, receipt)


if __name__ == '__main__':
    unittest.main()
