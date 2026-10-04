#!/usr/bin/env python3
"""Run independent P2 development and regression checks."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import socket
import subprocess
import sys
import time
import uuid
from urllib.request import urlopen

from devtools import build_env, cache_dir, go_binary

ROOT=Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--cache',type=Path,default=cache_dir())
parser.add_argument('--app',type=Path,help='Build a P2 preview at a new unused path')
parser.add_argument('--skip-browser',action='store_true')
args=parser.parse_args()
args.cache=cache_dir(args.cache)
run_dir=ROOT/'evidence/phase6/runs'/(datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+uuid.uuid4().hex[:8])
run_dir.mkdir(parents=True)
checks=[]
excluded={'node_modules','dist','evidence','build','__pycache__','test-results','playwright-report','.git'}
def fingerprint():
 paths=[ROOT/'docs/phase-0/requirements-ledger.json',ROOT/'go.mod',ROOT/'go.sum',ROOT/'frontend/vite.config.mjs',ROOT/'frontend/package.json',ROOT/'frontend/pnpm-lock.yaml']+list(ROOT.glob('*.go'))
 for directory in ['internal','cmd','contracts','schemas','native','frontend/src','frontend/tests','scripts']:
  paths += [p for p in (ROOT/directory).rglob('*') if p.is_file() and not excluded.intersection(p.relative_to(ROOT).parts) and not p.name.startswith('._')]
 return {str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(set(paths))}
inputs=fingerprint()
env=dict(os.environ)
for key in list(env):
 if key.startswith(('TROVE_','DYLD_')):env.pop(key)
env=build_env(args.cache,source=env)
env.update(CGO_LDFLAGS='-framework UniformTypeIdentifiers',GOPROXY='off',GOSUMDB='off',GOTOOLCHAIN='local')
go=go_binary(args.cache)
node=shutil.which('node')
def run(name,command,cwd=ROOT,extra=None):
 log=run_dir/(name+'.log');start=time.monotonic()
 with log.open('w') as out:
  result=subprocess.run([str(c) for c in command],cwd=cwd,env={**env,**(extra or {})},stdout=out,stderr=subprocess.STDOUT)
 checks.append({'name':name,'result':'passed' if result.returncode==0 else 'failed','exitCode':result.returncode,'command':[str(c) for c in command],'log':str(log.relative_to(ROOT)),'logSha256':hashlib.sha256(log.read_bytes()).hexdigest(),'elapsedSeconds':time.monotonic()-start})
 print(name+': '+checks[-1]['result'],flush=True)
 if result.returncode:print(log.read_text(errors='replace')[-6000:],flush=True)
 return result.returncode==0
run('contracts',[sys.executable,'-m','unittest','discover','-s','tests','-v'])
run('go-race',[go,'test','-mod=readonly','-race','-count=1','-json','-p','1','-timeout','10m','-tags','desktop production fts5 sqlite_omit_load_extension','-skip','^TestReal','./...'])
run('frontend-types',[node,ROOT/'frontend/node_modules/typescript/bin/tsc','--noEmit'],ROOT/'frontend')
units=[p for p in (ROOT/'frontend/tests').glob('*.test.ts') if not p.name.startswith('._')]
run('frontend-unit',[node,'--experimental-strip-types','--test',*units])
if not args.skip_browser:
 with socket.socket() as reservation:reservation.bind(('127.0.0.1',0));port=reservation.getsockname()[1]
 url=f'http://127.0.0.1:{port}'
 with (run_dir/'vite.log').open('w') as log:
  server=subprocess.Popen([node,str(ROOT/'frontend/node_modules/vite/bin/vite.js'),'--host','127.0.0.1','--port',str(port),'--strictPort'],cwd=ROOT/'frontend',env={**env,'TROVE_TEST_URL':url},stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
  try:
   ready=False
   for _ in range(100):
    if server.poll() is not None:break
    try:
     with urlopen(url,timeout=.5) as response:ready=response.status==200
     if ready:break
    except OSError:time.sleep(.2)
   if not ready:checks.append({'name':'browser-server','result':'failed'})
   else:
    for name,cwd in [('frontend-browser',ROOT/'frontend'),('database-browser',ROOT/'frontend/src/database/tests')]:
     report=run_dir/(name+'.json')
     run(name,[node,ROOT/'frontend/node_modules/@playwright/test/cli.js','test','--config','playwright.config.ts','--reporter=line,json','--output',run_dir/(name+'-results')],cwd,{'TROVE_TEST_URL':url,'TROVE_BROWSER_REPORT':str(report),'PLAYWRIGHT_JSON_OUTPUT_FILE':str(report),'TROVE_BROWSER_SCREENSHOTS':str(run_dir)})
  finally:
   if server.poll() is None:os.killpg(server.pid,signal.SIGTERM);server.wait(timeout=10)
if args.app:
 app=args.app.resolve()
 if app.exists():checks.append({'name':'unused-package-destination','result':'failed','reason':'Existing candidates are never overwritten'})
 else:run('package',[sys.executable,ROOT/'scripts/build-desktop.py','--cache',args.cache,'--phase6','--output',app])
else:run('frontend-build',[node,ROOT/'frontend/node_modules/vite/bin/vite.js','build'],ROOT/'frontend')
run('host-build',[go,'build','-tags','fts5 sqlite_omit_load_extension','-o',run_dir/'trove-host','./cmd/trove-host'])
run('host-journey',[sys.executable,ROOT/'scripts/verify-host.py','--host',run_dir/'trove-host','--output',run_dir/'host-journey.json'])
end=fingerprint();checks.append({'name':'source-stability','result':'passed' if inputs==end else 'failed','changed':[p for p in set(inputs)|set(end) if inputs.get(p)!=end.get(p)]})
passed=all(c['result']=='passed' for c in checks)
report={'scope':'phase6-development','recordedAt':datetime.now(timezone.utc).isoformat(),'result':'passed' if passed else 'failed','inputSha256':inputs,'checks':checks,'releaseApproved':False,'limitations':['Browser fixtures verify UI behavior; host integration is separately tested against a real temporary workspace.','Managed vendor authentication, distribution and containment and remote MCP remain unqualified and disabled.','Notion live account/API parity and general model-output quality require separate qualification.'],'candidate':str(args.app) if args.app else None}
(run_dir/'verification.json').write_text(json.dumps(report,indent=2)+'\n')
(ROOT/'evidence/phase6/latest-run.json').write_text(json.dumps(report,indent=2)+'\n')
print(report['result']+': '+str(run_dir/'verification.json'),flush=True)
sys.exit(0 if passed else 1)
