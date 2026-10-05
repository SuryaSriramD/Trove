#!/usr/bin/env python3
"""Build the pinned arm64 worker helpers; no source pin is resolved dynamically."""
import argparse,hashlib,json,pathlib,shutil,subprocess,sys,tarfile
from devtools import cache_dir, workspace_cache

ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('--cache',type=pathlib.Path,default=cache_dir());p.add_argument('--output',type=pathlib.Path,default=ROOT/'build/native');a=p.parse_args();a.cache=cache_dir(a.cache)
lock=json.loads((ROOT/'baseline/baseline.lock.json').read_text());a.output.mkdir(parents=True,exist_ok=True);helpers=[]
for name in ('llama','whisper'):
 pin=lock['workers'][name];src=a.cache/pin['sourceDirectory'];build=a.cache/(name+'-build');archive=a.cache/(name+'.tar.gz')
 if not archive.exists() or hashlib.sha256(archive.read_bytes()).hexdigest()!=pin['sha256']:raise SystemExit('Missing verified source archive; run scripts/bootstrap.py --fetch --components native')
 with tarfile.open(archive) as tar:
  for member in tar.getmembers():
   if not member.isfile():continue
   target=a.cache/member.name
   if not target.is_file() or target.is_symlink() or hashlib.sha256(target.read_bytes()).digest()!=hashlib.sha256(tar.extractfile(member).read()).digest():raise SystemExit('Native source differs from pin: '+member.name)
 options=['-DCMAKE_BUILD_TYPE=Release','-DCMAKE_OSX_ARCHITECTURES=arm64','-DCMAKE_OSX_DEPLOYMENT_TARGET=27.0','-DBUILD_SHARED_LIBS=OFF','-DGGML_NATIVE=OFF','-DGGML_CPU_ARM_ARCH=armv8.2-a+dotprod','-DGGML_METAL=ON','-DGGML_METAL_EMBED_LIBRARY=ON']
 if name=='llama':options+=['-DLLAMA_BUILD_IS_DEV=OFF','-DLLAMA_BUILD_COMMON=OFF','-DLLAMA_BUILD_EXAMPLES=OFF','-DLLAMA_BUILD_TESTS=OFF','-DLLAMA_BUILD_TOOLS=OFF','-DLLAMA_BUILD_SERVER=OFF','-DLLAMA_BUILD_APP=OFF','-DLLAMA_OPENSSL=OFF']
 else:options+=['-DWHISPER_BUILD_IS_DEV=OFF','-DWHISPER_BUILD_EXAMPLES=OFF','-DWHISPER_BUILD_TESTS=OFF','-DWHISPER_BUILD_SERVER=OFF','-DWHISPER_CURL=OFF']
 # Keep product build cache separate from the immutable Phase 0 qualification.
 build=workspace_cache(a.cache)/(name+'-native-build')
 subprocess.run(['cmake','-S',str(src),'-B',str(build)]+options,check=True)
 subprocess.run(['cmake','--build',str(build),'--parallel','4'],check=True)
 libs=[build/'src'/('lib'+name+'.a')]+sorted((build/'ggml/src').rglob('*.a'))
 out=a.output/(name+'-worker');cmd=['clang++','-std=c++17','-O2','-arch','arm64','-mmacosx-version-min=27.0','-I',str(src/'include'),'-I',str(src/'ggml/include'),'-I',str(a.cache/lock['workers']['llama']['sourceDirectory']/'vendor/nlohmann'),str(ROOT/'native'/(name+'-worker.cpp'))]+list(map(str,libs))+['-framework','Accelerate','-framework','Metal','-framework','Foundation','-o',str(out)]
 subprocess.run(cmd,check=True);subprocess.run(['codesign','--force','--sign','-',str(out)],check=True)
 helpers.append(dict(name=out.name,sha256=hashlib.sha256(out.read_bytes()).hexdigest(),runtime=name+'.cpp',runtimeCommit=pin['commit'],architecture='arm64'))
out=a.output/'microphone-worker'
subprocess.run(['swiftc','-module-cache-path',str(a.cache/'swift-module-cache'),'-swift-version','5','-O','-target','arm64-apple-macos27.0','-framework','AVFoundation',str(ROOT/'native/microphone-worker.swift'),'-o',str(out)],check=True)
subprocess.run(['codesign','--force','--sign','-','--entitlements',str(ROOT/'packaging/microphone.entitlements.plist'),str(out)],check=True)
helpers.append(dict(name=out.name,sha256=hashlib.sha256(out.read_bytes()).hexdigest(),runtime='microphone',runtimeCommit='trove-microphone-v1',architecture='arm64'))
(a.output/'manifest.json').write_text(json.dumps(dict(schemaVersion=1,helpers=helpers,minimumMacOS='27.0',buildFlags=['arm64','static','CPU','Metal','embedded Metal shaders','no model downloads'],baselineLockSHA256=hashlib.sha256((ROOT/'baseline/baseline.lock.json').read_bytes()).hexdigest()),indent=2)+'\n')
shutil.copytree(ROOT/'native/models',a.output/'models',dirs_exist_ok=True)
subprocess.run([sys.executable,str(ROOT/'scripts/build-vision.py'),'--cache',str(a.cache),'--output',str(a.output)],check=True)
subprocess.run([sys.executable,str(ROOT/'scripts/build-document-extractor.py'),'--cache',str(a.cache),'--output',str(a.output)],check=True)
subprocess.run([sys.executable,str(ROOT/'scripts/build-audio-decoder.py'),'--cache',str(a.cache),'--output',str(a.output)],check=True)
subprocess.run([sys.executable,str(ROOT/'scripts/build-pdf-renderer.py'),'--cache',str(a.cache),'--output',str(a.output)],check=True)
fixtures=a.output/'fixtures';fixtures.mkdir(exist_ok=True)
shutil.copyfile(a.cache/lock['workers']['whisper']['sourceDirectory']/'samples/jfk.wav',fixtures/'jfk.wav')
notices=a.output/'notices';notices.mkdir(exist_ok=True)
for name in ('llama','whisper'):shutil.copyfile(a.cache/lock['workers'][name]['sourceDirectory']/'LICENSE',notices/(name+'-LICENSE.txt'))
for h in helpers:
 subprocess.run(['codesign','--verify','--strict',str(a.output/h['name'])],check=True)
 deps=subprocess.check_output(['otool','-L',str(a.output/h['name'])],text=True)
 for line in deps.splitlines()[1:]:
  dylib=line.strip().split(' ')[0]
  if not dylib.startswith(('/usr/lib/','/System/Library/','/Library/Apple/System/')):raise SystemExit('Unbundled dependency: '+dylib)
print(a.output/'manifest.json')
