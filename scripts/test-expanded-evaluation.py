import importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('evaluation',pathlib.Path(__file__).with_name('run-expanded-evaluation.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class EvidenceAccounting(unittest.TestCase):
 def events(self,status='pass',passed=True):
  return [{'Test':'Runner/a','Action':'output','Output':'fixture.go:1: '+__import__('json').dumps({'fixture':'a','passed':passed,'canonicalUnchanged':True})},{'Test':'Runner/a','Action':status}]
 def test_complete(self): self.assertTrue(m.summarize(self.events(),['a'],'Runner')['automatedCorpusPassed'])
 def test_skipped(self): self.assertFalse(m.summarize(self.events('skip'),['a'],'Runner')['automatedCorpusPassed'])
 def test_missing(self): self.assertFalse(m.summarize(self.events(),['a','b'],'Runner')['automatedCorpusPassed'])
 def test_failed_oracle(self): self.assertFalse(m.summarize(self.events(passed=False),['a'],'Runner')['automatedCorpusPassed'])
 def test_missing_report(self): self.assertFalse(m.summarize(self.events()[1:],['a'],'Runner')['automatedCorpusPassed'])
 def test_duplicate(self): self.assertFalse(m.summarize(self.events()*2,['a'],'Runner')['automatedCorpusPassed'])
 def test_unexpected(self): self.assertFalse(m.summarize(self.events(),[],'Runner')['automatedCorpusPassed'])
 def test_report_mismatch(self):
  e=self.events();e[0]['Test']='Runner/b';self.assertFalse(m.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_manual_review(self):
  e=self.events();e[0]['Output']='{"fixture":"a","automatedTaskPassed":true,"canonicalUnchanged":true,"manualSemanticReviewRequired":true}'
  self.assertEqual(m.summarize(e,['a'],'Runner')['manualSemanticReviewPending'],['a'])
if __name__=='__main__':unittest.main()
