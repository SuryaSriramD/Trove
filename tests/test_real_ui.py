import importlib.util
from pathlib import Path
import unittest

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


if __name__ == '__main__':
    unittest.main()
