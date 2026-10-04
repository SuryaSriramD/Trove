#!/usr/bin/env python3
"""Run the Phase 0 contract gate and retain test-level, artifact-bound evidence."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
class EvidenceResult(unittest.TextTestResult):
 def __init__(self,*args,**kwargs):super().__init__(*args,**kwargs);self.details=[]
 def addSuccess(self,test):super().addSuccess(test);self.details.append({'test':test.id(),'result':'passed'})
 def addFailure(self,test,err):super().addFailure(test,err);self.details.append({'test':test.id(),'result':'failed'})
 def addError(self,test,err):super().addError(test,err);self.details.append({'test':test.id(),'result':'error'})
 def addSkip(self,test,reason):super().addSkip(test,reason);self.details.append({'test':test.id(),'result':'skipped','reason':reason})
def hashfile(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--cache-dir',type=Path,default=Path('/private/tmp/trove-phase0-tools'));parser.add_argument('--fresh-baseline',action='store_true');args=parser.parse_args()
 os.chdir(ROOT)
 checks=[]
 def run(name,cmd,env=None):
  p=subprocess.run([str(x) for x in cmd],cwd=ROOT,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
  out=ROOT/'evidence'/(name+'.log');out.write_text(p.stdout)
  print(f'{name}: '+('passed' if p.returncode==0 else 'FAILED'))
  if p.returncode:print(p.stdout[-8000:])
  checks.append({'name':name,'command':' '.join(map(str,cmd)),'result':'passed' if p.returncode==0 else 'failed','log':str(out.relative_to(ROOT)),'logSha256':hashfile(out)})
 suite=unittest.defaultTestLoader.discover(str(ROOT/'tests'))
 with (ROOT/'evidence/contract-tests.log').open('w') as output:
  result=unittest.TextTestRunner(stream=output,verbosity=2,resultclass=EvidenceResult).run(suite)
 print(f'python contracts: {result.testsRun} tests, '+('passed' if result.wasSuccessful() else 'FAILED'))
 if not result.wasSuccessful():print((ROOT/'evidence/contract-tests.log').read_text()[-12000:])
 go=shutil.which('go') or args.cache_dir/'go/bin/go'
 env=dict(os.environ);env.update({'GOCACHE':str(args.cache_dir/'go-cache'),'GOMODCACHE':str(args.cache_dir/'go-mod'),'GOTOOLCHAIN':'local','GOPROXY':'off'})
 run('go-contracts',[go,'test','-count=1','-v','./contracts'],env)
 tsc=shutil.which('tsc') or args.cache_dir/'frontend/node_modules/.bin/tsc'
 run('typescript-contracts',[tsc,'-p','contracts/tsconfig.json'])
 run('baseline-evidence',[sys.executable,'scripts/validate-baseline.py',* ([] if args.fresh_baseline else ['--check-evidence'])])
 tracked=[ROOT/name for name in ('README.md','requirements-dev.txt','go.mod','go.sum')]
 for directory in ['contracts','schemas','scripts','tests','docs/phase-0']:
  tracked += [p for p in (ROOT/directory).rglob('*') if p.is_file() and not p.name.startswith('._') and '__pycache__' not in p.parts and p.suffix!='.pyc']
 report={'contractVersion':'1.0.0','recordedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'Phase0 executable contracts, not product implementation','result':'passed' if result.wasSuccessful() and not result.skipped and all(c['result']=='passed' for c in checks) else 'failed','pythonLog':{'path':'evidence/contract-tests.log','sha256':hashfile(ROOT/'evidence/contract-tests.log')},'baselineEvidenceSha256':hashfile(ROOT/'evidence/baseline.json'),'tests':result.details,'checks':checks,'inputSha256':{str(p.relative_to(ROOT)):hashfile(p) for p in sorted(tracked)},'limitations':['Reference Python transaction/job/domain implementations are conformance oracles; production Go services remain product work.','Baseline record distinguishes native compile/capability probes from signed installation and model inference.','PRD/TRD cited R00 audit source was not supplied; original 307 audit rows are not claimed verified.']}
 (ROOT/'evidence/phase0-verification.json').write_text(json.dumps(report,indent=2)+'\n')
 return 0 if report['result']=='passed' else 1
if __name__=='__main__':sys.exit(main())
