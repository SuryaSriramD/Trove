#!/usr/bin/env python3
"""Verify a real authenticated host process, restart and exclusive snapshot transfer."""
import argparse
import base64
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import uuid
from urllib.request import Request, urlopen
from urllib.error import HTTPError

ROOT = Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--host',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a=p.parse_args();a.host=a.host.resolve()
checks=[]
def uid():
 raw=bytearray(os.urandom(16));raw[:6]=int(time.time()*1000).to_bytes(6,'big');raw[6]=(raw[6]&15)|112;raw[8]=(raw[8]&63)|128
 return str(uuid.UUID(bytes=bytes(raw)))
with tempfile.TemporaryDirectory(prefix='trove-p2-host-') as temp:
 root=Path(temp);source=root/'source';target=root/'destination';tokenfile=root/'owner-token'
 subprocess.run([a.host,'--init-token','--token-file',tokenfile],check=True)
 token=tokenfile.read_text().strip()
 def start(path,create=False):
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  log=(root/(path.name+'.log')).open('a')
  args=[str(a.host),'--workspace',str(path),'--token-file',str(tokenfile),'--assets',str(ROOT/'frontend/dist'),'--listen',f'127.0.0.1:{port}']
  if create:args.append('--create')
  process=subprocess.Popen(args,stdout=log,stderr=subprocess.STDOUT);url=f'http://127.0.0.1:{port}'
  for _ in range(100):
   if process.poll() is not None:raise RuntimeError((root/(path.name+'.log')).read_text())
   try:
    with urlopen(url,timeout=.3) as response:
     if response.status==200:return process,url,log
   except OSError:time.sleep(.1)
  process.terminate();raise RuntimeError('Host did not start')
 def stop(process,log):
  process.terminate();process.wait(timeout=15);log.close()
 def api(url,method,request={}):
  req=Request(url+'/api',data=json.dumps({'method':method,'request':request}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
  with urlopen(req,timeout=30) as response:result=json.load(response)
  if not result.get('ok'):raise RuntimeError(str(result))
  return result['result']
 process,url,log=start(source,True)
 try:
  try:urlopen(Request(url+'/api',data=b'{}'),timeout=5);raise AssertionError('Unauthenticated API accepted')
  except HTTPError as error:assert error.code==401
  checks.append('unauthenticated API denied')
  w=api(url,'workspace.snapshot');wid=w['workspaceId'];page=uid()
  def envelope():return {'contractVersion':1,'workspaceId':wid,'requestId':uid()}
  def batch(commands,guards):return api(url,'batch',{**envelope(),'mutation':{'scopeGrantId':w['scopeGrantId'],'idempotencyKey':uid(),'expectedRevisions':guards},'commands':commands})
  def guard(kind,id,rev):return {'target':{'entityType':kind,'entityId':id},'revision':rev}
  batch([{'kind':'page.create','pageId':page,'title':'Hosted Phase 6 acceptance'}],[guard('page',page,'0')])
  batch([{'kind':'page.display.set','pageId':page,'fullWidth':True,'presentation':False}],[guard('page',page,'1')])
  checks.append('canonical create and persistent display settings via HTTP')
  uploaded=Request(url+'/upload',data=json.dumps({'name':'conversation-notes.txt','data':base64.b64encode(b'host file provenance').decode()}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
  with urlopen(uploaded) as response:handle=json.load(response)['selectionHandle']
  file=api(url,'prepareFile',{**envelope(),'selectionHandle':handle})
  file_id=file['fileId']
  batch([{'kind':'file.attach','fileId':file_id,'ingestHandle':file['ingestHandle'],'pageId':page}],[guard('page',page,'2'),guard('file',file_id,'0')])
  with urlopen(Request(url+'/files/'+file_id,headers={'Authorization':'Bearer '+token})) as response:assert response.read()==b'host file provenance'
  checks.append('bounded uploaded bytes become authenticated canonical attachment')
  concurrent=subprocess.run([a.host,'--workspace',source,'--token-file',tokenfile,'--assets',ROOT/'frontend/dist'],capture_output=True,text=True,timeout=10)
  assert concurrent.returncode!=0 and 'WORKSPACE_LOCKED' in concurrent.stderr
  checks.append('second owner rejected')
 finally:stop(process,log)
 process,url,log=start(source)
 try:
  assert api(url,'readPage',{**envelope(),'pageId':page})['page']['title']=='Hosted Phase 6 acceptance'
  assert api(url,'pageDisplay.get',{**envelope(),'pageId':page})['fullWidth'] is True
  checks.append('host restart preserves canonical content and settings')
 finally:stop(process,log)
 moved=subprocess.run([a.host,'--workspace',source,'--transfer-to',target],check=True,capture_output=True,text=True,timeout=60)
 result=json.loads(moved.stdout);assert result['workspaceId']==wid and result['assets']==1
 blocked=subprocess.run([a.host,'--workspace',source,'--token-file',tokenfile,'--assets',ROOT/'frontend/dist'],capture_output=True,text=True,timeout=10)
 assert blocked.returncode!=0 and 'transferred' in blocked.stderr
 checks.append('verified transfer fences old owner')
 process,url,log=start(target)
 try:
  assert api(url,'pageDisplay.get',{**envelope(),'pageId':page})['fullWidth'] is True
  with urlopen(Request(url+'/files/'+file_id,headers={'Authorization':'Bearer '+token})) as response:assert response.read()==b'host file provenance'
  checks.append('new owner reads transferred settings and verified attachment')
 finally:stop(process,log)
report={'result':'passed','checks':checks,'scope':'real host process; loopback HTTP; source restart; verified mode transfer'}
a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
