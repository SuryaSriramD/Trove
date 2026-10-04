#!/usr/bin/env python3
"""Explicit, SHA-pinned qualification download. Models are never application-bundled."""
import argparse,hashlib,json,pathlib,urllib.request,shutil,concurrent.futures
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('--root',type=pathlib.Path,default=pathlib.Path('/private/tmp/trove-phase1-models'));a=p.parse_args();a.root.mkdir(parents=True,exist_ok=True)
def acquire(pack):
 final=a.root/(pack['id']+'.model');part=final.with_suffix('.download')
 if final.exists() and hashlib.sha256(final.read_bytes()).hexdigest()==pack['sha256']:
  (a.root/(pack['id']+'.json')).write_text(json.dumps(pack,indent=2)+'\n');return pack['kind']+' already verified'
 with urllib.request.urlopen(pack['url']) as src,part.open('wb') as dst:shutil.copyfileobj(src,dst)
 if part.stat().st_size!=pack['sizeBytes'] or hashlib.sha256(part.read_bytes()).hexdigest()!=pack['sha256']:raise RuntimeError('model verification failed: '+pack['id'])
 part.replace(final);(a.root/(pack['id']+'.json')).write_text(json.dumps(pack,indent=2)+'\n');return pack['kind']+' verified'
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
 for result in pool.map(acquire,json.loads((ROOT/'native/models/catalog.json').read_text())):print(result,flush=True)
