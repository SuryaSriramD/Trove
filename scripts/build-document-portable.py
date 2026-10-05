#!/usr/bin/env python3
"""Build pinned offline WebView PDF helpers and assets on the native target host."""
import argparse
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import subprocess
import sys
from audio_build import verify_source
from devtools import ROOT, baseline, cache_dir, file_digest
from native_targets import require_native_host
from document_bundle import RUNTIMES, MIN_WEBVIEW2, LINUX_UNAVAILABLE, asset_path, assets, validate
from desktop_package import inventory

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--target',required=True,choices=['windows-amd64','linux-amd64']);p.add_argument('--cache',type=Path,default=cache_dir());p.add_argument('--output',type=Path,required=True);p.add_argument('--plan',action='store_true');a=p.parse_args()
    if a.plan:
        print(json.dumps({'target':a.target,'runtime':RUNTIMES[a.target.split('-')[0]],'minimumWebView2':MIN_WEBVIEW2 if a.target.startswith('windows') else None,'networkAcquisition':False,'sourceOnly':True,'executionQualified':False,'runtimeAvailable':a.target.startswith('windows'),'unavailableReason':LINUX_UNAVAILABLE if a.target.startswith('linux') else None,'inputs':['native target toolchain','pinned offline frontend dependencies','pinned WebView runtime and JSON header','Windows: cached official WebView2 SDK1.0.3537.50; Ubuntu: WebKitGTK2.52.6 dev packages']},indent=2));return
    if a.target.startswith("linux"):
        raise ValueError(LINUX_UNAVAILABLE)
    require_native_host(a.target)
    cache=cache_dir(a.cache);out=a.output.resolve();os_name=a.target.split('-')[0]
    if a.output.is_symlink() or any((out/x).exists() for x in ['document-worker.json','document-assets','document-worker','document-worker.exe','document-web-extension.so']):raise ValueError('Choose unused document output entries')
    lock=baseline();node=shutil.which('node')
    if not node or subprocess.check_output([node,'--version'],text=True).strip()!='v'+lock['toolchain']['node']:raise ValueError('Pinned Node is required; tools are never acquired during build')
    if file_digest(ROOT/'frontend/pnpm-lock.yaml')!=file_digest(ROOT/'baseline/frontend/pnpm-lock.yaml'):raise ValueError('Frontend lock differs')
    for tool in ['vite','typescript','pdfjs-dist']:
        installed=json.loads((ROOT/'frontend/node_modules'/tool/'package.json').read_text())['version']
        expected='6.3.289' if tool=='pdfjs-dist' else lock['frontend'][tool]
        if installed!=expected:raise ValueError('Frontend dependency differs: '+tool)
    subprocess.run([node,ROOT/'frontend/node_modules/typescript/bin/tsc','--noEmit'],cwd=ROOT/'frontend',check=True)
    subprocess.run([node,ROOT/'frontend/node_modules/vite/bin/vite.js','build'],cwd=ROOT/'frontend',check=True)
    out.mkdir(parents=True,exist_ok=True)
    if os_name=='windows':
        subprocess.run([sys.executable,ROOT/'scripts/build-document-windows.py','--cache',cache,'--output',out],check=True)
    else:
        release=Path('/etc/os-release').read_text()
        if 'ID=ubuntu\n' not in release or 'VERSION_ID="24.04"' not in release:raise ValueError('Ubuntu24.04 required')
        packages=['libwebkit2gtk-4.1-dev','libwebkit2gtk-4.1-0','libjavascriptcoregtk-4.1-dev','libjavascriptcoregtk-4.1-0']
        identities={x:subprocess.check_output(['dpkg-query','-W','-f=${Version}',x],text=True) for x in packages}
        if any(x!='2.52.6-0ubuntu0.24.04.1' for x in identities.values()):raise ValueError('Pinned WebKitGTK packages required')
        source=verify_source(cache,'llama',lock['workers']['llama']);includes=['-I'+str(source/'vendor/nlohmann')]
        compiler=subprocess.check_output(['g++','-dumpfullversion'],text=True).strip()
        if compiler.split('.')[0]!='13':raise ValueError('UbuntuGCC13 required')
        commands=[];dependencies={}
        env=dict(os.environ)
        for key in ['CPATH','C_INCLUDE_PATH','CPLUS_INCLUDE_PATH','CXXFLAGS','CFLAGS','CPPFLAGS','LDFLAGS','LIBRARY_PATH','LD_PRELOAD','LD_LIBRARY_PATH','PKG_CONFIG_PATH','PKG_CONFIG_LIBDIR','PKG_CONFIG_SYSROOT_DIR','GCC_EXEC_PREFIX','COMPILER_PATH']:env.pop(key,None)
        for name,src,pkg,extra in [('document-worker','document-worker-linux.cpp',['webkit2gtk-4.1','gtk+-unix-print-3.0'],[]),('document-web-extension.so','document-web-extension-linux.cpp',['webkit2gtk-web-extension-4.1'],['-shared','-fPIC'])]:
            flags=shlex.split(subprocess.check_output(['pkg-config','--cflags','--libs',*pkg],text=True,env=env))
            cmd=['g++','-std=c++17','-O2','-Wall','-Wextra',*extra,*includes,str(ROOT/'native'/src),*flags,'-o',str(out/name)];commands.append(cmd)
            subprocess.run(cmd,env=env,check=True)
            dependencies[name]=subprocess.check_output(['ldd',str(out/name)],text=True)
            if 'not found' in dependencies[name]:raise ValueError('Document native dependency missing')
        (out/'document-worker-linux-build.json').write_text(json.dumps({'packages':identities,'compiler':compiler,'commands':commands,'dependencies':dependencies,'executionQualified':False,'sourceSha256':{str(x.relative_to(ROOT)):file_digest(x) for x in [ROOT/'native/document-worker-linux.cpp',ROOT/'native/document-web-extension-linux.cpp',ROOT/'native/document-policy.hpp']}},indent=2)+'\n')
    notice=out/'notices';notice.mkdir(exist_ok=True)
    if os_name=='linux':
        shutil.copyfile(source/'licenses/LICENSE-jsonhpp',notice/'document-json-LICENSE.txt')
        for package in ['libwebkit2gtk-4.1-0','libjavascriptcoregtk-4.1-0','libgtk-3-0t64','libsoup-3.0-0']:
            shutil.copyfile(Path('/usr/share/doc')/package/'copyright',notice/('document-'+package+'-COPYRIGHT.txt'))
    shutil.copyfile(ROOT/'frontend/node_modules/pdfjs-dist/LICENSE',notice/'PDFjs-LICENSE.txt')
    destination=out/'document-assets';destination.mkdir()
    for entry in inventory(ROOT/'frontend/dist'):
        name=entry['path'];source=ROOT/'frontend/dist'/name
        if name.startswith('pdfjs-assets/') and ('license' in source.name.lower() or 'copying' in source.name.lower()):
            shutil.copyfile(source,notice/('PDFjs-'+name.replace('/','-')));continue
        if asset_path(name):
            target=destination/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(source,target)
            if file_digest(target)!=entry['sha256']:raise ValueError('Frontend asset changed during copy')
    binary=out/('document-worker.exe' if os_name=='windows' else 'document-worker')
    record={'schemaVersion':1,'name':'document-worker','os':os_name,'architecture':'amd64','runtime':RUNTIMES[os_name],'sha256':file_digest(binary),'testOnly':False,'assets':assets(destination),'sourceOnlyQualification':True}
    if os_name=='windows':record['minimumWebView2']=MIN_WEBVIEW2
    else:record['webExtensionSha256']=file_digest(out/'document-web-extension.so')
    validate(out,a.target,record)
    (out/'document-worker.json').write_text(json.dumps(record,indent=2)+'\n');print(out/'document-worker.json')
if __name__=='__main__':main()
