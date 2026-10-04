import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
spec=importlib.util.spec_from_file_location('phase5_runner',ROOT/'scripts/verify-phase5.py')
runner=importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class P1RunnerTests(unittest.TestCase):
    def test_family_reporting_records_all_ten_without_colliding_with_check_name(self):
        run=runner.Qualification.__new__(runner.Qualification)
        run.tests=[]
        run.checks=[]
        with contextlib.redirect_stdout(io.StringIO()):
            run.record_families()
        self.assertEqual({c['name'] for c in run.checks},{f'family-V{i:02}' for i in range(1,11)})
        self.assertTrue(all(c['result']=='failed' and c['familyName'] for c in run.checks))


if __name__=='__main__':
    unittest.main()
