import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('phase5_policy', ROOT / 'scripts/phase5_policy.py')
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class P1EvidencePolicyTests(unittest.TestCase):
    def complete_evidence(self):
        tests, checks = [], []
        for _, names, gates in policy.FAMILIES.values():
            tests.extend({'test': policy.WORKSPACE + name, 'result': 'passed'} for name in names)
            checks.extend({'name': name, 'result': 'passed'} for name in gates)
        return tests, checks

    def test_every_trd_family_and_p1_package_is_explicit(self):
        self.assertEqual(set(policy.FAMILIES), {f'V{i:02}' for i in range(1,11)})
        self.assertEqual(set(policy.PACKAGES), {f'4{x}' for x in 'ABCDEFG'})
        for package in policy.PACKAGES.values():
            self.assertTrue(package['limits'])
            self.assertTrue(set(package['families']) <= set(policy.FAMILIES))

    def test_empty_and_missing_evidence_cannot_qualify(self):
        self.assertTrue(all(f['result']=='failed' for f in policy.evaluate_families([],[])))
        tests,checks=self.complete_evidence()
        self.assertTrue(all(f['result']=='passed' for f in policy.evaluate_families(tests,checks)))
        checks=[c for c in checks if c['name']!='packaged-p1-completeness']
        families={f['id']:f for f in policy.evaluate_families(tests,checks)}
        self.assertEqual(families['V08']['result'],'failed')
        self.assertEqual(families['V06']['result'],'failed')

    def test_skipped_failed_and_similarly_named_tests_do_not_count(self):
        tests,checks=self.complete_evidence()
        target=policy.WORKSPACE+policy.FAMILIES['V05'][1][0]
        for state in ['skipped','failed']:
            families={f['id']:f for f in policy.evaluate_families(tests+[{'test':target,'result':state}],checks)}
            self.assertEqual(families['V05']['result'],'failed')
        tests=[dict(t,test=t['test']+'Other') if t['test']==target else t for t in tests]
        families={f['id']:f for f in policy.evaluate_families(tests,checks)}
        self.assertEqual(families['V05']['result'],'failed')


if __name__=='__main__':
    unittest.main()
