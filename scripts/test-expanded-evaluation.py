import copy,hashlib,importlib.util,json,pathlib,tempfile,unittest
spec=importlib.util.spec_from_file_location('evaluation',pathlib.Path(__file__).with_name('run-expanded-evaluation.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class EvidenceAccounting(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
  self.root=pathlib.Path(self.temp.name).resolve()
 def canonical(self,case='a'):
  directory=self.root/case;directory.mkdir(exist_ok=True)
  snapshot={'schemaVersion':1,'testName':'Runner/'+case,'scope':'Independent test snapshot',
   'tables':{name:{'schema':'CREATE TABLE '+name+'(id TEXT)','columns':['id'],'rows':[]} for name in m.CANONICAL_REQUIRED_TABLES},
   'excludedTables':{name:'Reviewed generated or derived state' for name in m.CANONICAL_EXCLUDED_TABLES},
   'assetRoots':{'files':True,'recordings':False},'assetDirectories':['files'],'assets':{},'mutationHistoryCount':0}
  audit={'schemaVersion':1,'unchanged':True,'rowsUnchanged':True,'assetsUnchanged':True,'mutationHistoryCountUnchanged':True}
  for phase in ['before','after']:
   path=directory/(phase+'.json');raw=json.dumps(snapshot).encode();path.write_bytes(raw)
   audit[phase]={'path':str(path),'sha256':hashlib.sha256(raw).hexdigest(),'size':len(raw),'retained':True,
    'tableRows':{name:0 for name in snapshot['tables']},'assetFiles':0,'assetBytes':0}
  return audit
 def summarize(self,events,expected,parent):return m.summarize(events,expected,parent,self.root)
 def mutate_snapshot(self,audit,phase,change):
  binding=audit[phase];path=pathlib.Path(binding['path']);snapshot=json.loads(path.read_bytes());change(snapshot)
  raw=json.dumps(snapshot).encode();path.write_bytes(raw);binding.update(size=len(raw),sha256=hashlib.sha256(raw).hexdigest())
  binding.update(tableRows={name:len(t['rows']) for name,t in snapshot['tables'].items()},assetFiles=len(snapshot['assets']),assetBytes=sum(a['size'] for a in snapshot['assets'].values()))
 def report_event(self,report):return {'Test':'Runner/'+report['fixture'],'Action':'output','Output':'fixture.go:1: '+json.dumps(report)+'\n'}
 def test_locked_holdout_family_has_distinct_inventory(self):
  import json
  name,parent,count=m.FAMILIES['p0-holdouts']
  corpus=json.loads((m.ROOT/'internal/workspace/testdata/expanded-evaluation'/name).read_text())
  self.assertEqual(count,12);self.assertEqual(parent,'TestRealLocalP0ClosureHoldouts')
  self.assertEqual(len(corpus['cases']),count)
  self.assertEqual(len({c['id'] for c in corpus['cases']}),count)
  self.assertEqual(m.FAMILIES['p0'],('p0-qa-memory.json','TestRealLocalExpandedP0QAMemory',24))
 def events(self,status='pass',passed=True):
  return [{'Test':'Runner/a','Action':'output','Output':'fixture.go:1: '+__import__('json').dumps({'fixture':'a','passed':passed,'canonicalUnchanged':True,'mutationHistoryCountUnchanged':True,'canonicalInventory':self.canonical()})+'\n'},{'Test':'Runner/a','Action':status}]
 def test_complete(self): self.assertTrue(self.summarize(self.events(),['a'],'Runner')['automatedCorpusPassed'])
 def test_skipped(self): self.assertFalse(self.summarize(self.events('skip'),['a'],'Runner')['automatedCorpusPassed'])
 def test_missing(self): self.assertFalse(self.summarize(self.events(),['a','b'],'Runner')['automatedCorpusPassed'])
 def test_failed_oracle(self): self.assertFalse(self.summarize(self.events(passed=False),['a'],'Runner')['automatedCorpusPassed'])
 def test_missing_report(self): self.assertFalse(self.summarize(self.events()[1:],['a'],'Runner')['automatedCorpusPassed'])
 def test_duplicate(self): self.assertFalse(self.summarize(self.events()*2,['a'],'Runner')['automatedCorpusPassed'])
 def test_unexpected(self): self.assertFalse(self.summarize(self.events(),[],'Runner')['automatedCorpusPassed'])
 def test_report_mismatch(self):
  e=self.events();e[0]['Test']='Runner/b';self.assertFalse(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_manual_review(self):
  e=self.events();e[0]['Output']='{"fixture":"a","automatedTaskPassed":true,"canonicalUnchanged":true,"manualSemanticReviewRequired":true}\n'
  self.assertEqual(self.summarize(e,['a'],'Runner')['manualSemanticReviewPending'],['a'])
 def test_fragmented_report(self):
  e=self.events(); output=e[0]['Output']; e[0]['Output']=output[:18]
  e.insert(1,dict(e[0],Output=output[18:41])); e.insert(2,dict(e[0],Output=output[41:]))
  self.assertTrue(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_interleaved_reports_do_not_mix(self):
  a=self.events(); b=self.events();b[0]['Test']='Runner/b';b[0]=self.report_event({'fixture':'b','passed':True,'canonicalUnchanged':True,'mutationHistoryCountUnchanged':True,'canonicalInventory':self.canonical('b')});b[1]['Test']='Runner/b'
  atext=a[0]['Output'];a[0]['Output']=atext[:20]
  e=[a[0],b[0],dict(a[0],Output=atext[20:]),a[1],b[1]]
  self.assertTrue(self.summarize(e,['a','b'],'Runner')['automatedCorpusPassed'])
 def test_truncated_report_fails(self):
  e=self.events();e[0]['Output']=e[0]['Output'][:25]
  self.assertFalse(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
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
 def test_count_only_legacy_report_cannot_qualify(self):
  e=self.events();r=json.loads(e[0]['Output'].split(': ',1)[1]);r.pop('canonicalInventory');e[0]=self.report_event(r)
  self.assertFalse(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_missing_canonical_snapshot_cannot_qualify(self):
  e=self.events();r=json.loads(e[0]['Output'].split(': ',1)[1]);pathlib.Path(r['canonicalInventory']['after']['path']).unlink()
  self.assertFalse(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_changed_snapshot_bytes_cannot_qualify(self):
  e=self.events();r=json.loads(e[0]['Output'].split(': ',1)[1]);pathlib.Path(r['canonicalInventory']['after']['path']).write_text('{}')
  self.assertFalse(self.summarize(e,['a'],'Runner')['automatedCorpusPassed'])
 def test_changed_row_with_rebound_hash_cannot_hide_behind_count(self):
  audit=self.canonical();self.mutate_snapshot(audit,'after',lambda s:s['tables']['pages']['rows'].append([{'type':'text','value':'Y2hhbmdlZA=='}]))
  r={'fixture':'a','passed':True,'canonicalUnchanged':True,'mutationHistoryCountUnchanged':True,'canonicalInventory':audit}
  self.assertFalse(self.summarize([self.report_event(r),{'Test':'Runner/a','Action':'pass'}],['a'],'Runner')['automatedCorpusPassed'])
 def test_changed_asset_bytes_with_rebound_hash_cannot_qualify(self):
  import base64
  audit=self.canonical()
  for phase,content in [('before',b'original'),('after',b'tampered')]:
   self.mutate_snapshot(audit,phase,lambda s:s['assets'].update({'files/asset':{'bytes':base64.b64encode(content).decode(),'sha256':hashlib.sha256(content).hexdigest(),'size':len(content)}}))
  r={'canonicalInventory':audit,'mutationHistoryCountUnchanged':True}
  self.assertIsNotNone(m.validate_canonical_inventory(r,self.root,'Runner/a'))
 def test_required_memory_rows_cannot_be_excluded(self):
  audit=self.canonical()
  for phase in ['before','after']:self.mutate_snapshot(audit,phase,lambda s:s['tables'].pop('memory_entries'))
  self.assertIsNotNone(m.validate_canonical_inventory({'canonicalInventory':audit},self.root,'Runner/a'))
 def test_foreign_case_inventory_cannot_be_substituted(self):
  self.assertIsNotNone(m.validate_canonical_inventory({'canonicalInventory':self.canonical('b')},self.root,'Runner/a'))
 def test_review_exclusions_require_explicit_family_contract(self):
  audit=self.canonical()
  for phase in ['before','after']:
   self.mutate_snapshot(audit,phase,lambda snap:snap['excludedTables'].update({'personal_review_runs':'Derived receipt','personal_review_model_runs':'Derived binding'}))
  report={'canonicalInventory':audit,'mutationHistoryCountUnchanged':True,'family':'reviews'}
  self.assertIsNotNone(m.validate_canonical_inventory(report,self.root,'Runner/a'))
  self.assertIsNone(m.validate_canonical_inventory(report,self.root,'Runner/a',m.REVIEW_EXCLUDED_TABLES))
  for phase in ['before','after']:
   self.mutate_snapshot(audit,phase,lambda snap:snap['excludedTables'].update({'personal_review_activation':'not permitted'}))
  self.assertIsNotNone(m.validate_canonical_inventory(report,self.root,'Runner/a',m.REVIEW_EXCLUDED_TABLES))
 def test_snapshot_link_cannot_be_substituted(self):
  audit=self.canonical();path=pathlib.Path(audit['after']['path']);raw=path.read_bytes();path.unlink()
  other=self.root/'foreign.json';other.write_bytes(raw);path.symlink_to(other)
  self.assertIsNotNone(m.validate_canonical_inventory({'canonicalInventory':audit},self.root,'Runner/a'))
 def test_false_inventory_boolean_cannot_override_equality(self):
  audit=self.canonical();audit['assetsUnchanged']=False
  self.assertIsNotNone(m.validate_canonical_inventory({'canonicalInventory':audit},self.root,'Runner/a'))
 def request_report(self,use_memory=False):
  return {'fixture':'a','passed':True,'canonicalUnchanged':True,'mutationHistoryCountUnchanged':True,
   'canonicalInventory':self.canonical(),'useMemory':use_memory,'generationAttempts':1,
   'modelPackId':'locked-model','corpusSHA256':'locked-corpus'}
 def request_expectation(self,use_memory=False):
  return {'useMemory':use_memory,'modelPackId':'locked-model','corpusSHA256':'locked-corpus'}
 def request_summary(self,report,expected=None):
  if expected is None:expected={'a':self.request_expectation()}
  return m.summarize([self.report_event(report),{'Test':'Runner/a','Action':'pass'}],['a'],'Runner',self.root,expected)
 def test_actual_request_controls_accept_explicit_false_and_true(self):
  for value in (False,True):
   with self.subTest(useMemory=value):
    self.assertTrue(self.request_summary(self.request_report(value),{'a':self.request_expectation(value)})['automatedCorpusPassed'])
 def test_legacy_missing_control_cannot_qualify(self):
  for key in ('useMemory','generationAttempts','modelPackId','corpusSHA256'):
   with self.subTest(field=key):
    report=self.request_report();report.pop(key)
    result=self.request_summary(report)
    self.assertFalse(result['automatedCorpusPassed'])
    self.assertTrue(any('request contract a:' in error for error in result['errors']))
 def test_memory_control_wrong_type_or_value_cannot_qualify(self):
  for value in (None,0,1,'false',True):
   with self.subTest(value=value):
    report=self.request_report();report['useMemory']=value
    self.assertFalse(self.request_summary(report)['automatedCorpusPassed'])
 def test_generation_count_is_observed_integer_one(self):
  for value in (None,False,True,0,2,'1',1.0):
   with self.subTest(value=value):
    report=self.request_report();report['generationAttempts']=value
    self.assertFalse(self.request_summary(report)['automatedCorpusPassed'])
 def test_wrong_model_or_corpus_cannot_qualify(self):
  for key in ('modelPackId','corpusSHA256'):
   with self.subTest(field=key):
    report=self.request_report();report[key]='different-input'
    self.assertFalse(self.request_summary(report)['automatedCorpusPassed'])
 def test_request_controls_cannot_use_foreign_case(self):
  result=self.request_summary(self.request_report(),{'b':self.request_expectation()})
  self.assertFalse(result['automatedCorpusPassed'])
  self.assertIn('request contract missing for case: a',result['errors'])
  self.assertIn('request-contract inventory differs from locked case selection',result['errors'])
 def test_request_controls_cannot_cover_unselected_cases(self):
  self.assertFalse(self.request_summary(self.request_report(),{'a':self.request_expectation(),'b':self.request_expectation()})['automatedCorpusPassed'])
 def test_invalid_locked_memory_control_cannot_qualify(self):
  expected=self.request_expectation();expected['useMemory']=0
  self.assertFalse(self.request_summary(self.request_report(),{'a':expected})['automatedCorpusPassed'])
 def test_p0_families_cannot_omit_request_contract(self):
  for family in ('p0','p0-holdouts'):
   result=m.summarize([],[],m.FAMILIES[family][1],self.root)
   self.assertFalse(result['automatedCorpusPassed'])
   self.assertIn('P0 qualification requires locked request controls',result['errors'])
if __name__=='__main__':unittest.main()
