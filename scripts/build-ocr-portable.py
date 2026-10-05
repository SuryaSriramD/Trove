#!/usr/bin/env python3
"""Build descriptor-only offline OCR from pinned source on the native target host."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request
from devtools import ROOT, cache_dir, file_digest
from native_targets import TARGETS, host_target, require_native_host


def fetch_pin(destination, url, maximum):
    with tempfile.NamedTemporaryFile(dir=destination.parent, prefix='ocr-download-', delete=False) as out:
        pending = Path(out.name)
        try:
            with urllib.request.urlopen(url, timeout=90) as r:
                total = 0
                while chunk := r.read(65536):
                    total += len(chunk)
                    if total > maximum: raise RuntimeError('OCR download exceeds bound')
                    out.write(chunk)
            out.flush()
            pending.replace(destination)
        finally:
            pending.unlink(missing_ok=True)


def source(cache, name, pin, fetch):
    archive = cache / (name + '.tar.gz')
    if fetch and not archive.exists():
        fetch_pin(archive, pin['url'], 128 << 20)
    if not archive.is_file() or file_digest(archive) != pin['sha256']:
        raise RuntimeError('Missing or changed pinned source: ' + str(archive))
    target = cache / pin['sourceDirectory']
    # Verify any reused source against the authenticated archive, including types.
    expected = set()
    with tarfile.open(archive) as tar:
        for m in tar.getmembers():
            rel = Path(m.name)
            if not rel.parts or rel.parts[0] != pin['sourceDirectory'] or '..' in rel.parts or rel.is_absolute() or not (m.isdir() or m.isfile()):
                raise RuntimeError('Unsafe OCR source member: ' + m.name)
            expected.add(rel)
            path = cache / rel
            if path.is_symlink():
                raise RuntimeError('Symlink in OCR source: ' + str(path))
            if m.isdir():
                path.mkdir(parents=True, exist_ok=True)
            else:
                raw = tar.extractfile(m).read()
                if path.exists():
                    if not path.is_file() or file_digest(path) != hashlib.sha256(raw).hexdigest():
                        raise RuntimeError('Modified OCR source: ' + str(path))
                else:
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(raw)
    for existing in target.rglob("*"):
        if existing.is_symlink() or existing.relative_to(cache) not in expected:
            raise RuntimeError("Unexpected file in pinned OCR source: " + str(existing))
    return target


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--target', choices=TARGETS)
    p.add_argument('--cache', type=Path, default=cache_dir() / 'ocr')
    p.add_argument('--output', type=Path, default=ROOT / 'build/native')
    p.add_argument('--fetch', action='store_true', help='Explicitly fetch exact pinned build inputs; runtime never downloads.')
    p.add_argument('--plan', action='store_true')
    a = p.parse_args(); target = a.target or host_target()
    pin = json.loads((ROOT / 'native/ocr.lock.json').read_text())
    if a.plan:
        print(json.dumps({'target': target, 'pin': pin, 'language': 'English printed text only', 'qualification': 'Native execution evidence required independently.'}, indent=2)); return
    require_native_host(target)
    if target == "linux-amd64":
        release = Path("/etc/os-release").read_text()
        if not re.search(r"^ID=ubuntu$", release, re.M) or not re.search(r'^VERSION_ID="24\.04"$', release, re.M):
            raise RuntimeError("Initial Linux OCR builds require Ubuntu 24.04 LTS.")
    cache = a.cache.resolve(); cache.mkdir(parents=True, exist_ok=True)
    out = a.output.resolve(); out.mkdir(parents=True, exist_ok=True)
    if any((out / name).exists() for name in ('ocr-worker', 'ocr-worker.exe', 'ocr-worker.json', 'eng.traineddata')):
        raise RuntimeError('Choose an output without existing OCR artifacts; do not overwrite a candidate.')
    tess = source(cache, 'tesseract', pin['tesseract'], a.fetch)
    lept = source(cache, 'leptonica', pin['leptonica'], a.fetch)
    model = cache / 'eng.traineddata'
    if a.fetch and not model.exists():
        fetch_pin(model, pin['model']['url'], 32 << 20)
    if not model.is_file() or file_digest(model) != pin['model']['sha256']: raise RuntimeError('Missing or changed pinned English model')
    cmake = shutil.which('cmake')
    if not cmake or 'version 4.3.2' not in subprocess.check_output([cmake, '--version'], text=True): raise RuntimeError('Pinned CMake 4.3.2 required')
    env = dict(os.environ)
    for key in ('CFLAGS', 'CXXFLAGS', 'CPPFLAGS', 'LDFLAGS', 'LD_PRELOAD', 'LD_LIBRARY_PATH', 'DYLD_LIBRARY_PATH', 'CMAKE_PREFIX_PATH', 'PKG_CONFIG_PATH'):
        env.pop(key, None)
    commands = []
    def run(cmd):
        commands.append(list(map(str,cmd))); subprocess.run(commands[-1], check=True, env=env)
    build = cache / ('build-' + target); install = build / 'install'
    common = ['-G','Ninja','-DCMAKE_BUILD_TYPE=Release','-DBUILD_SHARED_LIBS=OFF','-DCMAKE_INSTALL_PREFIX='+str(install),'-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded']
    if target == 'darwin-arm64': common += ['-DCMAKE_OSX_ARCHITECTURES=arm64','-DCMAKE_OSX_DEPLOYMENT_TARGET=27.0']
    run([cmake,'-S',lept,'-B',build/'lept',*common,'-DBUILD_PROG=OFF','-DSW_BUILD=OFF',*['-DENABLE_'+x+'=OFF' for x in ('ZLIB','PNG','GIF','JPEG','TIFF','WEBP','OPENJPEG')]])
    run([cmake,'--build',build/'lept','--parallel','4']);run([cmake,'--install',build/'lept'])
    run([cmake,'-S',tess,'-B',build/'tess',*common,'-DLeptonica_DIR='+str(install/'lib/cmake/leptonica'),'-DSW_BUILD=OFF','-DOPENMP_BUILD=OFF','-DGRAPHICS_DISABLED=ON','-DDISABLED_LEGACY_ENGINE=ON','-DENABLE_NATIVE=OFF','-DBUILD_TRAINING_TOOLS=OFF','-DBUILD_TESTS=OFF','-DDISABLE_TIFF=ON','-DDISABLE_ARCHIVE=ON','-DDISABLE_CURL=ON','-DINSTALL_CONFIGS=OFF','-DWIN32_MT_BUILD=ON'])
    run([cmake,'--build',build/'tess','--target','libtesseract','--parallel','4']);run([cmake,'--install',build/'tess','--component','Development'])
    binary = out / ('ocr-worker.exe' if target == 'windows-amd64' else 'ocr-worker')
    # Link only the two pinned static libraries plus OS runtime/crypto.
    library = list((build/'tess').rglob('tesseract55.lib' if target=='windows-amd64' else 'libtesseract.a'))
    if target=='windows-amd64' and not library: library=list((build/'tess').rglob('tesseract*.lib'))
    leptonica = list((install/'lib').glob('leptonica*.lib' if target=='windows-amd64' else 'libleptonica.a'))
    if len(library)!=1 or len(leptonica)!=1: raise RuntimeError('Cannot identify exact static OCR archives')
    includes = [tess/'include',build/'tess/include',install/'include']
    if target=='windows-amd64':
        run(['cl','/nologo','/std:c++17','/EHsc','/utf-8','/O2','/MT','/DTESS_API=',*['/I'+str(x) for x in includes],ROOT/'native/ocr-worker.cpp',*library,*leptonica,'bcrypt.lib','/Fe:'+str(binary)])
        dependencies=subprocess.check_output(['dumpbin','/DEPENDENTS',str(binary)],text=True)
        dlls=re.findall(r'^\s+([\w.-]+\.dll)\s*$',dependencies,re.M|re.I)
        if not dlls or any(x.lower() not in {'kernel32.dll','bcrypt.dll','advapi32.dll'} and not x.lower().startswith('api-ms-win-') for x in dlls): raise RuntimeError('Unexpected OCR DLL dependency: '+dependencies)
    else:
        run(['clang++' if target=='darwin-arm64' else 'g++','-std=c++17','-O2','-Wno-deprecated-declarations',*['-I'+str(x) for x in includes],ROOT/'native/ocr-worker.cpp',*library,*leptonica,'-lm','-lpthread',*(['-lcrypto'] if target=='linux-amd64' else []),'-o',binary])
        if target=='darwin-arm64':
            run(['codesign','--force','--sign','-',binary]);run(['codesign','--verify','--strict',binary])
            dependencies=subprocess.check_output(['otool','-L',str(binary)],text=True)
            if any(not line.strip().split(' ')[0].startswith(('/usr/lib/','/System/Library/')) for line in dependencies.splitlines()[1:]): raise RuntimeError('Unbundled OCR dependency: '+dependencies)
        else:
            dependencies=subprocess.check_output(['ldd',str(binary)],text=True)
            allowed={'libstdc++.so.6','libm.so.6','libgcc_s.so.1','libc.so.6','libpthread.so.0','libdl.so.2','libcrypto.so.3'}
            if 'not found' in dependencies or any(x not in allowed for x in re.findall(r'^\s*(\S+)\s+=>',dependencies,re.M)): raise RuntimeError('Unbundled OCR dependency: '+dependencies)
    shutil.copyfile(model,out/'eng.traineddata')
    license_dir=out/'ocr-licenses';license_dir.mkdir(exist_ok=True)
    shutil.copyfile(tess/'LICENSE',license_dir/'Tesseract-Apache-2.0.txt')
    shutil.copyfile(lept/'leptonica-license.txt',license_dir/'Leptonica-BSD-2-Clause.txt')
    # tessdata_fast uses the same unmodified Apache-2.0 license text.
    shutil.copyfile(ROOT/'docs/completion/licenses/tessdata-fast-LICENSE',license_dir/'tessdata-fast-Apache-2.0.txt')
    manifest={'schemaVersion':1,'name':'ocr-worker','os':TARGETS[target]['os'],'architecture':TARGETS[target]['arch'],'runtime':pin['runtime'],'sha256':file_digest(binary),'modelSha256':pin['model']['sha256'],'testOnly':False,'sourceSha256':file_digest(ROOT/'native/ocr-worker.cpp'),'platformSourceSha256':file_digest(ROOT/'native/worker-platform.hpp'),'pins':pin,'commands':commands,'dependencies':dependencies}
    (out/'ocr-worker.json').write_text(json.dumps(manifest,indent=2)+'\n')
    print(binary)

if __name__=='__main__': main()
