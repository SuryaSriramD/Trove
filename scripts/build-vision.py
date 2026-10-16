#!/usr/bin/env python3
"""Build the pinned CPU-only mtmd image worker into an explicit native bundle."""
import argparse,hashlib,json,pathlib,shutil,subprocess,tarfile
from devtools import cache_dir,workspace_cache
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('--cache',type=pathlib.Path,default=cache_dir());p.add_argument('--output',type=pathlib.Path,required=True);a=p.parse_args();a.cache=cache_dir(a.cache)
lock=json.loads((ROOT/'baseline/baseline.lock.json').read_text());pin=lock['workers']['llama'];src=a.cache/pin['sourceDirectory'];archive=a.cache/'llama.tar.gz'
if not archive.is_file() or hashlib.sha256(archive.read_bytes()).hexdigest()!=pin['sha256']:raise SystemExit('Missing verified llama source archive')
with tarfile.open(archive) as tar:
 for member in tar.getmembers():
  if not member.isfile():continue
  target=a.cache/member.name
  if not target.is_file() or target.is_symlink() or hashlib.sha256(target.read_bytes()).digest()!=hashlib.sha256(tar.extractfile(member).read()).digest():raise SystemExit('Native source differs from pin: '+member.name)
build=workspace_cache(a.cache)/'vision-native-build'
options=['-DCMAKE_BUILD_TYPE=Release','-DCMAKE_OSX_ARCHITECTURES=arm64','-DCMAKE_OSX_DEPLOYMENT_TARGET=27.0','-DBUILD_SHARED_LIBS=OFF','-DGGML_NATIVE=OFF','-DGGML_CPU_ARM_ARCH=armv8.2-a+dotprod','-DGGML_METAL=OFF','-DLLAMA_BUILD_IS_DEV=OFF','-DLLAMA_BUILD_COMMON=OFF','-DLLAMA_BUILD_EXAMPLES=OFF','-DLLAMA_BUILD_TESTS=OFF','-DLLAMA_BUILD_TOOLS=OFF','-DLLAMA_BUILD_SERVER=OFF','-DLLAMA_BUILD_APP=OFF','-DLLAMA_OPENSSL=OFF','-DLLAMA_BUILD_MTMD=ON','-DLLAMA_SUBPROCESS=OFF','-DMTMD_VIDEO=OFF']
subprocess.run(['cmake','-S',str(src),'-B',str(build)]+options,check=True);subprocess.run(['cmake','--build',str(build),'--target','mtmd','--parallel','4'],check=True)
a.output.mkdir(parents=True,exist_ok=True);out=a.output/'vision-worker'
libs=[build/'tools/mtmd/libmtmd.a',build/'src/libllama.a']+sorted((build/'ggml/src').rglob('*.a'))+sorted((build/'vendor').rglob('*.a'))
cmd=['clang++','-std=c++17','-O2','-arch','arm64','-mmacosx-version-min=27.0','-I',str(src/'include'),'-I',str(src/'ggml/include'),'-I',str(src/'tools/mtmd'),'-I',str(src/'vendor/nlohmann'),str(ROOT/'native/vision-worker.cpp')]+list(map(str,libs))+['-framework','Accelerate','-framework','Foundation','-o',str(out)]
subprocess.run(cmd,check=True);subprocess.run(['codesign','--force','--sign','-',str(out)],check=True);subprocess.run(['codesign','--verify','--strict',str(out)],check=True)
for line in subprocess.check_output(['otool','-L',str(out)],text=True).splitlines()[1:]:
 if not line.strip().split(' ')[0].startswith(('/usr/lib/','/System/Library/','/Library/Apple/System/')):raise SystemExit('Unbundled vision dependency: '+line)
manifest=a.output/'manifest.json';m=json.loads(manifest.read_text());m['helpers']=[h for h in m['helpers'] if h['name']!='vision-worker']+[dict(name='vision-worker',sha256=hashlib.sha256(out.read_bytes()).hexdigest(),runtime='llama.cpp',runtimeCommit=pin['commit'],architecture='arm64')];manifest.write_text(json.dumps(m,indent=2)+'\n')
shutil.copytree(ROOT/'native/models',a.output/'models',dirs_exist_ok=True);notices=a.output/'notices';notices.mkdir(exist_ok=True);shutil.copyfile(src/'LICENSE',notices/'vision-llama-LICENSE.txt')
# mtmd uses bundled image/audio helper libraries even though audio/video requests
# are disabled. Carry their original license notices with the binary.
for directory in ('stb','miniaudio','hash','sheredom'):
 for pattern in ('*LICENSE*','*license*','*COPYING*'):
  for source in (src/'vendor'/directory).rglob(pattern):
   if source.is_file():shutil.copyfile(source,notices/('vision-'+str(source.relative_to(src/'vendor')).replace('/', '-')))
# These single-header libraries carry notices in source rather than LICENSE files.
for relative in ('stb/stb_image.h','miniaudio/miniaudio.h','sheredom/subprocess.h'):
 source=src/'vendor'/relative;text=source.read_text()
 if relative.startswith('sheredom'):notice=text[:text.index('#ifndef SHEREDOM_SUBPROCESS_H_INCLUDED')]
 else:notice=text[text.rfind('/*'):]
 (notices/('vision-'+source.stem+'-LICENSE.txt')).write_text(notice)
sha1=(src/'vendor/hash/sha1/sha1.c').read_text()
(notices/'vision-hash-sha1-PUBLIC-DOMAIN.txt').write_text(sha1[:sha1.index('*/')+2]+'\n')
json_header=(src/'vendor/nlohmann/json.hpp').read_text()
(notices/'vision-nlohmann-COPYRIGHT.txt').write_text('\n'.join(sorted({line for line in json_header.splitlines() if 'SPDX-FileCopyrightText:' in line or 'SPDX-License-Identifier:' in line}))+'\n')
for name in ('smolvlm-500m-model-card.md','smolvlm-APACHE-2.0.txt','nlohmann-json-3.12.0-LICENSE.MIT'):
 shutil.copyfile(ROOT/'docs/phase-3/licenses'/name,notices/name)
print(out)
