#!/usr/bin/env python3
"""Build the pinned Windows WebView2 document helper offline on Windows x64.

Supply the hash-verified official SDK package in --cache. This script performs
no downloads or runtime installation. Common document packaging adds assets.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile
from devtools import ROOT, baseline, cache_dir, file_digest
from audio_build import verify_source
from native_targets import require_native_host


def sdk_files(package, pin):
    if not package.is_file() or package.stat().st_size != pin['sizeBytes'] or file_digest(package) != pin['sha256']:
        raise RuntimeError('Place the reviewed official WebView2 SDK .nupkg in the selected cache; the build does not download it')
    result = {}
    with zipfile.ZipFile(package) as archive:
        names = archive.namelist()
        for name, expected in pin['files'].items():
            if names.count(name) != 1:
                raise RuntimeError('SDK package member missing or duplicated: ' + name)
            item = archive.getinfo(name)
            if item.file_size > 16 << 20:
                raise RuntimeError('SDK member exceeds bound')
            data = archive.read(name)
            if hashlib.sha256(data).hexdigest() != expected:
                raise RuntimeError('SDK member differs from reviewed identity: ' + name)
            result[name] = data
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--plan', action='store_true')
    args = parser.parse_args()
    pin = json.loads((ROOT / 'native/document-windows.lock.json').read_text())
    if args.plan:
        print(json.dumps({'target':'windows-amd64','pin':pin,'inputs':['x64 MSVC developer prompt / Windows SDK WRL','verified llama JSON header','cached official WebView2 NuGet SDK'],
            'networkAcquisition':False,'requiresRuntimeDownload':False,'executionQualified':False,'sourceOnly':True},indent=2))
        return 0
    require_native_host('windows-amd64')
    cache = cache_dir(args.cache)
    if not all(os.environ.get(x) for x in ['VCToolsVersion','WindowsSDKVersion','WindowsSdkDir']) or os.environ.get('VSCMD_ARG_TGT_ARCH') != 'x64':
        raise RuntimeError('Use an identified x64 MSVC developer prompt with the Windows 11 SDK and WRL headers')
    if tuple(map(int,os.environ['VCToolsVersion'].split('.')[:2])) < (14,30) or not shutil.which('cl') or not shutil.which('dumpbin'):
        raise RuntimeError('MSVC v143 or newer and dumpbin are required')
    if any(os.environ.get(name) for name in ('CL','_CL_','LINK','_LINK_')):
        raise RuntimeError('Remove ambient MSVC command overrides before the recorded build')
    sdk_version = os.environ['WindowsSDKVersion'].strip('\\/')
    sdk_include = Path(os.environ['WindowsSdkDir']) / 'Include' / sdk_version
    sdk_headers = [sdk_include/'winrt/wrl.h',sdk_include/'um/Windows.h',sdk_include/'um/winuser.h',sdk_include/'um/winbase.h',sdk_include/'um/appmodel.h',sdk_include/'um/shobjidl.h']
    sdk_headers += sorted((sdk_include/'winrt/wrl').glob('**/*.h'))
    if not all(p.is_file() for p in sdk_headers):
        raise RuntimeError('Windows SDK/WRL headers are incomplete')
    sdk = sdk_files(cache/pin['sdk']['filename'],pin['sdk'])
    source = verify_source(cache,'llama',baseline()['workers']['llama'])
    header = source/pin['jsonHeader']['path'];license_path=source/pin['jsonHeader']['licensePath']
    if file_digest(header)!=pin['jsonHeader']['sha256'] or file_digest(license_path)!=pin['jsonHeader']['licenseSha256']:
        raise RuntimeError('JSON dependency differs from reviewed header/license')
    if args.output.is_symlink():raise RuntimeError('Output must not be a symlink')
    output=args.output.resolve()
    if output.is_symlink():raise RuntimeError('Output must not be a symlink')
    if any((output/name).exists() for name in ['document-worker.exe','document-worker-windows-build.json']):
        raise RuntimeError('Refusing to overwrite a document helper candidate')
    output.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='document-windows-',dir=output) as temporary:
        stage=Path(temporary)
        for name,data in sdk.items():(stage/Path(name).name).write_bytes(data)
        binary=stage/'document-worker.exe'
        command=['cl','/nologo','/std:c++17','/Zc:__cplusplus','/O2','/EHsc','/utf-8','/MT','/W4','/DUNICODE','/D_UNICODE','/D_WIN32_WINNT=0x0A00','/DWINVER=0x0A00',
            '/I'+str(stage),'/I'+str(header.parent),str(ROOT/'native/document-worker-windows.cpp'),'/Fe:'+str(binary),'/Fo:'+str(stage/'document-worker.obj'),
            '/link','/SUBSYSTEM:CONSOLE',str(stage/'WebView2LoaderStatic.lib'),'version.lib','shlwapi.lib','ole32.lib','oleaut32.lib','user32.lib','advapi32.lib','bcrypt.lib','shell32.lib']
        completed=subprocess.run(command,cwd=stage,check=True,text=True,capture_output=True)
        with binary.open('rb') as f:
            pe=f.read(64)
            if len(pe)!=64 or pe[:2]!=b'MZ':raise RuntimeError('Missing PE executable')
            offset=struct.unpack_from('<I',pe,60)[0]
            if offset>16<<20:raise RuntimeError('Invalid PE offset')
            f.seek(offset)
            if f.read(6)!=b'PE\0\0\x64\x86':raise RuntimeError('Windows document helper architecture differs')
        dependencies=subprocess.check_output(['dumpbin','/DEPENDENTS',str(binary)],text=True)
        imports=re.findall(r'^\s+([\w.-]+\.dll)\s*$',dependencies,re.M|re.I)
        allowed={'kernel32.dll','user32.dll','advapi32.dll','bcrypt.dll','ole32.dll','oleaut32.dll','shell32.dll','shlwapi.dll','version.dll','ntdll.dll','combase.dll','rpcrt4.dll','userenv.dll','crypt32.dll','wintrust.dll','ucrtbase.dll'}
        if not imports or any(x.lower() not in allowed and not x.lower().startswith('api-ms-win-') for x in imports):
            raise RuntimeError('Unexpected DLL dependency; keep static MSVC/WebView2 loader and review target imports: '+dependencies)
        report={'schemaVersion':1,'platform':'windows','architecture':'amd64','name':'document-worker','sha256':file_digest(binary),
            'runtime':'WebView2','runtimeCommit':pin['runtimeCommit'],'minimumWebView2':pin['minimumWebView2'],'runtimeExecutionQualified':False,'sdk':pin['sdk'],
            'sourceSha256':{str(p.relative_to(ROOT)):file_digest(p) for p in [ROOT/'native/document-worker-windows.cpp',ROOT/'native/document-policy.hpp',ROOT/'native/document-windows.lock.json',Path(__file__).resolve()]},
            'jsonHeaderSHA256':file_digest(header),'jsonLicenseSHA256':file_digest(license_path),'windowsSDKHeaders':{str(p):file_digest(p) for p in sdk_headers},
            'compilerExecutableSHA256':file_digest(Path(shutil.which('cl'))),'compiler':{key:os.environ[key] for key in ['VCToolsVersion','WindowsSDKVersion','VSCMD_ARG_TGT_ARCH']},'command':command,'compileOutput':completed.stdout+completed.stderr,'dependencies':dependencies,
            'host':platform.platform(),'recordedAt':datetime.now(timezone.utc).isoformat()}
        notices=output/'notices';notices.mkdir(exist_ok=True)
        for name,data in [('WebView2-SDK-LICENSE.txt',sdk['LICENSE.txt']),('document-json-LICENSE.txt',license_path.read_bytes())]:
            destination=notices/name
            if destination.exists() and destination.read_bytes()!=data:raise RuntimeError('Existing license notice differs')
            destination.write_bytes(data)
        (stage/'document-worker-windows-build.json').write_text(json.dumps(report,indent=2)+'\n')
        os.rename(binary,output/'document-worker.exe')
        os.rename(stage/'document-worker-windows-build.json',output/'document-worker-windows-build.json')
    print(output/'document-worker-windows-build.json')
    return 0

if __name__=='__main__':
    try:raise SystemExit(main())
    except (OSError,RuntimeError,ValueError,subprocess.SubprocessError) as error:
        print('Windows document build failed: '+str(error),file=sys.stderr);raise SystemExit(1)
