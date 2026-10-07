import importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('evaluation',pathlib.Path(__file__).with_name('run-expanded-evaluation.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class EvidenceAccounting(unittest.TestCase):
 def events(self,status='pass',passed=True):
  return [{'Test':'Runner/a','Action':'output','Output':'fixture.go:1: '+__import__('json').dumps({'fixture':'a','passed':passed,'canonicalUnchanged':True})+'\n'},{'Test':'Runner/a','Action':status}]
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
  e=self.events();e[0]['Output']='{"fixture":"a","automatedTaskPassed":true,"canonicalUnchanged":true,"manualSemanticReviewRequired":true}\n'
  self.assertEqual(m.summarize(e,['a'],'Runner')['manualSemanticReviewPending'],['a'])
 def test_fragmented_report(self):
  e=self.events(); output=e[0]['Output']; e[0]['Output']=output[:18]
  e.insert(1,dict(e[0],Output=output[18:41])); e.insert(2,dict(e[0],Output=output[41:]))
  self.assertTrue(m.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_interleaved_reports_do_not_mix(self):
  a=self.events(); b=self.events();b[0]['Test']='Runner/b';b[0]['Output']=b[0]['Output'].replace('"a"','"b"');b[1]['Test']='Runner/b'
  atext=a[0]['Output'];a[0]['Output']=atext[:20]
  e=[a[0],b[0],dict(a[0],Output=atext[20:]),a[1],b[1]]
  self.assertTrue(m.summarize(e,['a','b'],'Runner')['automatedCorpusPassed'])
 def test_truncated_report_fails(self):
  e=self.events();e[0]['Output']=e[0]['Output'][:25]
  self.assertFalse(m.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_failed_atomic_manifest_retains_previous(self):
  import tempfile
  from unittest.mock import patch
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'manifest.json';p.write_text('previous')
   with patch.object(m.os,'replace',side_effect=OSError('full disk')):
    with self.assertRaises(OSError):m.atomic_json(p,{'new':True})
   self.assertEqual(p.read_text(),'previous')
   self.assertEqual(list(pathlib.Path(d).iterdir()),[p])
 def test_space_preflight_rejects_insufficient_internal_temp(self):
  from unittest.mock import patch
  from types import SimpleNamespace
  with patch.object(m.shutil,'disk_usage',side_effect=[SimpleNamespace(free=50*1024**3),SimpleNamespace(free=2*1024**3)]):
   with self.assertRaises(ValueError):m.require_space(['evidence','private-temp'])
 def test_space_preflight_records_actual_available_bytes(self):
  from unittest.mock import patch
  from types import SimpleNamespace
  with patch.object(m.shutil,'disk_usage',return_value=SimpleNamespace(free=8*1024**3)):
   self.assertEqual(m.require_space(['private-temp']),{'private-temp':8*1024**3})
if __name__=='__main__':unittest.main()
