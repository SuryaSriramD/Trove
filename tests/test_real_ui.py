import importlib.util
from pathlib import Path
import unittest
import json
import tempfile

spec = importlib.util.spec_from_file_location('real_ui', Path(__file__).resolve().parents[1] / 'scripts/run-real-ui.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def report(status='passed', retry=0):
    return {'suites': [{'suites': [{'specs': [{'title': 'declared', 'ok': status == 'passed',
        'tests': [{'expectedStatus': 'passed', 'results': [{'status': status, 'retry': retry}]}]}]}]}]}


class RealUIAccountingTests(unittest.TestCase):
    def test_exact_single_pass(self):
        self.assertTrue(runner.assess(report(), ['declared'])['passed'])

    def test_missing_or_required_skipped_is_not_pass(self):
        for status in ('skipped', 'interrupted', 'failed', 'timedOut'):
            with self.subTest(status=status):
                result = runner.assess(report(status), ['declared', 'missing'])
                self.assertFalse(result['passed'])
                self.assertEqual(result['outcomes']['declared'], status)
                self.assertEqual(result['outcomes']['missing'], 'unexecuted')

    def test_retry_duplicate_extra_or_top_level_error_is_not_pass(self):
        retried = report(retry=1)
        duplicate = report()
        duplicate['suites'].extend(duplicate['suites'][:])
        extra = report()
        extra['suites'].append({'specs': [{'title': 'undeclared'}]})
        error = report()
        error['errors'] = [{'message': 'worker exited'}]
        for value in (retried, duplicate, extra, error):
            with self.subTest(value=value):
                self.assertFalse(runner.assess(value, ['declared'])['passed'])

    def test_expected_failure_cannot_be_accepted_as_pass(self):
        value = report()
        value['suites'][0]['suites'][0]['specs'][0]['tests'][0]['expectedStatus'] = 'failed'
        self.assertFalse(runner.assess(value, ['declared'])['passed'])

    def test_failed_spec_with_passed_attempt_cannot_pass(self):
        value = report()
        value['suites'][0]['suites'][0]['specs'][0]['ok'] = False
        self.assertFalse(runner.assess(value, ['declared'])['passed'])

    def test_empty_report_and_no_declared_cases_do_not_pass(self):
        self.assertFalse(runner.assess({}, ['declared'])['passed'])
        self.assertFalse(runner.assess({}, [])['passed'])


class RealUICaptureHistoryInventoryTests(unittest.TestCase):
    @staticmethod
    def case_report(titles):
        specs = []
        for title in titles:
            item = report()['suites'][0]['suites'][0]['specs'][0]
            item['title'] = title
            specs.append(item)
        return {'suites': [{'specs': specs}]}

    def test_historical_thirty_four_passes_do_not_cover_capture_history(self):
        old = ('rich', 'task', 'due', 'record-lifecycle', 'layout-review', 'p2-data', 'p2-action',
               'conversation-display', 'schedule-week', 'schedule-dst', 'import-merge', 'import-resume',
               'skills', 'personal-review', 'graph', 'table-rich', 'layout-rich')
        for stage in ('create', 'reopen'):
            with self.subTest(stage=stage):
                previous = [f'real-{workflow}-{stage}' for workflow in old]
                added = [f'real-capture-review-{stage}', f'real-history-recovery-{stage}']
                self.assertEqual(runner.EXPECTED[stage][:-2], previous + added)
                result = runner.assess(self.case_report(previous), runner.EXPECTED[stage][:-2])
                self.assertFalse(result['passed'])
                self.assertEqual({name: result['outcomes'][name] for name in added}, dict.fromkeys(added, 'unexecuted'))
                complete = runner.assess(self.case_report(previous + added), runner.EXPECTED[stage][:-2])
                self.assertTrue(complete['passed'])
                self.assertEqual(len(complete['outcomes']), 19)

    def test_missing_or_unexpected_capture_boundary_is_never_observed_denial(self):
        with tempfile.TemporaryDirectory() as temporary:
            absent = runner.capture_boundary_evidence(temporary)
            self.assertFalse(absent['productAcceptance'])
            self.assertEqual(absent['stages'], {'create': {'state': 'unobserved'}, 'reopen': {'state': 'unobserved'}})
            (Path(temporary) / 'create-capture-host-boundary.json').write_text(json.dumps({'ok': True}))
            unexpected = runner.capture_boundary_evidence(temporary)
            self.assertEqual(unexpected['stages']['create']['state'], 'unexpected_boundary_result')
            self.assertFalse(unexpected['productAcceptance'])

    def test_exact_host_denial_is_evidence_of_boundary_not_feature_acceptance(self):
        with tempfile.TemporaryDirectory() as temporary:
            value = {'schemaVersion': 1, 'feature': 'external-folder-indexing', 'productAcceptance': False,
                     'method': 'index.roots', 'httpStatus': 200, 'ok': False, 'errorCode': 'SCOPE_DENIED',
                     'message': 'This operation requires the desktop adapter'}
            path = Path(temporary) / 'create-capture-host-boundary.json'
            path.write_text(json.dumps(value))
            actual = runner.capture_boundary_evidence(temporary)
            self.assertFalse(actual['productAcceptance'])
            self.assertEqual(actual['stages']['create']['state'], 'observed_host_denial')
            self.assertEqual(actual['stages']['create']['sha256'], runner.digest(path))
            self.assertEqual(actual['stages']['reopen']['state'], 'unobserved')


class RealUIHostBindingTests(unittest.TestCase):
    def test_clean_override_requires_exact_checkout_and_no_dirty_files(self):
        runner.validate_host_override('same', 'same', '')
        for executing, source, status in [('old', 'new', ''), ('same', 'same', ' M internal/workspace/import_commit.go'), ('same', 'same', '?? untracked.go')]:
            with self.subTest(executing=executing, source=source, status=status):
                with self.assertRaises(RuntimeError):
                    runner.validate_host_override(executing, source, status)

    def test_missing_dirty_or_ambiguous_provenance_is_rejected(self):
        self.assertEqual(runner.clean_host_revision('build vcs.revision=source\nbuild vcs.modified=false'), 'source')
        for value in ['build vcs.modified=false', 'build vcs.revision=source\nbuild vcs.modified=true',
                      'build vcs.revision=one\nbuild vcs.revision=two\nbuild vcs.modified=false']:
            with self.subTest(value=value), self.assertRaises(RuntimeError):
                runner.clean_host_revision(value)


class RealUILockAutomationInventoryTests(unittest.TestCase):
    def test_historical_thirty_eight_passes_do_not_cover_locks_and_automation(self):
        for stage in ('create', 'reopen'):
            with self.subTest(stage=stage):
                old = runner.EXPECTED[stage][:-2]
                added = [f'real-page-lock-{stage}', f'real-manual-automation-{stage}']
                self.assertEqual(len(old), 19)
                self.assertEqual(runner.EXPECTED[stage][-2:], added)
                result = runner.assess(RealUICaptureHistoryInventoryTests.case_report(old), runner.EXPECTED[stage])
                self.assertFalse(result['passed'])
                self.assertEqual({name: result['outcomes'][name] for name in added}, dict.fromkeys(added, 'unexecuted'))
                complete = runner.assess(RealUICaptureHistoryInventoryTests.case_report(old + added), runner.EXPECTED[stage])
                self.assertTrue(complete['passed'])
                self.assertEqual(len(complete['outcomes']), 21)

    def test_each_new_case_skip_duplicate_retry_or_expected_failure_cannot_pass(self):
        for stage in ('create', 'reopen'):
            for target in runner.EXPECTED[stage][-2:]:
                for invalid in ('skipped', 'duplicate', 'retry', 'expected_failure'):
                    with self.subTest(stage=stage, target=target, invalid=invalid):
                        value = RealUICaptureHistoryInventoryTests.case_report(runner.EXPECTED[stage])
                        specs = value['suites'][0]['specs']
                        case = next(item for item in specs if item['title'] == target)
                        if invalid == 'skipped':
                            case['tests'][0]['results'][0]['status'] = 'skipped'
                            case['ok'] = False
                        elif invalid == 'duplicate':
                            specs.append(case)
                        elif invalid == 'retry':
                            case['tests'][0]['results'][0]['retry'] = 1
                        else:
                            case['tests'][0]['expectedStatus'] = 'failed'
                        self.assertFalse(runner.assess(value, runner.EXPECTED[stage])['passed'])


if __name__ == '__main__':
    unittest.main()
