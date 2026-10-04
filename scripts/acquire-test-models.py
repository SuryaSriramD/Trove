#!/usr/bin/env python3
"""Explicit, SHA-pinned qualification download. Models are never application-bundled."""
import argparse,hashlib,json,pathlib,urllib.request,shutil,concurrent.futures
from devtools import cache_dir, models_dir

ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('--cache',type=pathlib.Path,default=cache_dir());p.add_argument('--root',type=pathlib.Path,default=None);p.add_argument('--pack',action='append',help='Acquire only this catalog pack ID (repeatable; default all)');a=p.parse_args();a.root=models_dir(a.root,a.cache);a.root.mkdir(parents=True,exist_ok=True)
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for chunk in iter(lambda:f.read(1<<20),b''):h.update(chunk)
 return h.hexdigest()
def acquire(pack):
 final=a.root/(pack['id']+'.model');part=final.with_suffix('.download')
 if final.exists() and final.stat().st_size==pack['sizeBytes'] and digest(final)==pack['sha256']:
  (a.root/(pack['id']+'.json')).write_text(json.dumps(pack,indent=2)+'\n');return pack['kind']+' already verified'
 print('Downloading '+pack['name']+' ('+str(pack['sizeBytes'])+' bytes)',flush=True)
 with urllib.request.urlopen(pack['url'],timeout=60) as src,part.open('wb') as dst:shutil.copyfileobj(src,dst)
 if part.stat().st_size!=pack['sizeBytes'] or digest(part)!=pack['sha256']:raise RuntimeError('model verification failed: '+pack['id'])
 part.replace(final);(a.root/(pack['id']+'.json')).write_text(json.dumps(pack,indent=2)+'\n');return pack['kind']+' verified'
packs=json.loads((ROOT/'native/models/catalog.json').read_text())
if a.pack:
 unknown=set(a.pack)-{pack['id'] for pack in packs}
 if unknown:p.error('Unknown catalog pack IDs: '+', '.join(sorted(unknown)))
 packs=[pack for pack in packs if pack['id'] in a.pack]
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
 for result in pool.map(acquire,packs):print(result,flush=True)
