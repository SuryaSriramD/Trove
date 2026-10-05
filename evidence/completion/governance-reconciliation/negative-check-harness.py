import importlib.util,json,tempfile,shutil,sys
from pathlib import Path
root=Path(sys.argv[1]).resolve() if len(sys.argv)>1 else Path(__file__).resolve().parents[3]
spec=importlib.util.spec_from_file_location('completion_validator',root/'scripts/validate-completion.py');v=importlib.util.module_from_spec(spec);spec.loader.exec_module(v)
with tempfile.TemporaryDirectory(prefix='trove-governance-check-',dir='/private/tmp') as temporary:
 dst=Path(temporary)
 # Copy only mutable metadata. All other paths are read-only source references.
 for p in root.iterdir():
  if p.name!='docs':(dst/p.name).symlink_to(p,target_is_directory=p.is_dir())
 (dst/'docs').mkdir()
 for p in (root/'docs').iterdir():
  q=dst/'docs'/p.name
  if p.name not in ['completion','phase-0']:q.symlink_to(p,target_is_directory=p.is_dir());continue
  q.mkdir()
  for f in p.iterdir():
   target=q/f.name
   if f.name in ['requirements-ledger.json','registry.json','requirements-overlay.json']:shutil.copyfile(f,target)
   else:target.symlink_to(f,target_is_directory=f.is_dir())
 baseline={p:(dst/p).read_text() for p in ['docs/phase-0/requirements-ledger.json','docs/completion/registry.json','docs/completion/requirements-overlay.json']}
 cases=[]
 def check(name,path,mutate,expected):
  for p,s in baseline.items():(dst/p).write_text(s)
  target=dst/path;data=json.loads(target.read_text());mutate(data);target.write_text(json.dumps(data))
  errors,_=v.validate(dst);assert any(expected in e for e in errors),(name,errors)
  cases.append({'case':name,'result':'rejected_as_expected','expectedDiagnostic':expected})
 ledger='docs/phase-0/requirements-ledger.json';overlay='docs/completion/requirements-overlay.json';registry='docs/completion/registry.json'
 check('Historical verification flag cannot be silently changed',ledger,lambda m:m['requirements'][0].update(verified=False),'historical ledger content changed')
 check('Missing product requirement cannot pass mapping',overlay,lambda m:m['requirements'].pop(),'product mappings are duplicated, missing or outside product scope')
 check('Coverage association cannot become release approval',overlay,lambda m:m['requirements'][0].update(releaseQualified=True),'mapping became a completion claim')
 check('Nonexistent evidence cannot be counted',overlay,lambda m:m['packages']['kernel']['evidencePaths'].append('evidence/does-not-exist.json'),'missing file: evidence/does-not-exist.json')
 check('Candidate identity cannot borrow another manifest',registry,lambda m:m['developmentCandidates'][0].update(sourceCommit='0'*40),'candidate manifest source differs')
 check('Planned folder cannot be represented as passing evidence',registry,lambda m:m['items'][0].update(evidence_target_status='passed'),'planned evidence path lacks explicit status')
 check('Missing research cannot be closed by documentation',registry,lambda m:next(i for i in m['items'] if i['id']=='GOV-004').update(implementation='implemented'),'missing research incorrectly closed')
 print(json.dumps({'validatorMutationChecks':cases,'productTestsExecuted':False,'releaseQualified':False},indent=2))
