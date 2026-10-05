#!/usr/bin/env python3
"""Build an offline Windows/Ubuntu desktop bundle on the selected native host.

Inputs must already be installed and verified. No package-manager install,
bootstrap, native compilation or network acquisition is performed here.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile

from devtools import baseline, build_env, cache_dir, go_binary
from desktop_package import APP_ID, TARGETS, binary_target, copy_verified, copy_verified_native, digest, inventory, no_link, validate_native
from desktop_prerequisites import command, prerequisites

ROOT = Path(__file__).resolve().parents[1]

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--target',choices=TARGETS,required=True)
    p.add_argument('--cache',type=Path,default=cache_dir())
    p.add_argument('--native-bundle',type=Path)
    p.add_argument('--release-notices',type=Path)
    p.add_argument('--output',type=Path)
    p.add_argument('--version')
    p.add_argument('--build-number',type=int)
    p.add_argument('--plan',action='store_true')
    p.add_argument('--check',action='store_true',help='Inspect native host prerequisites only; no qualification claim')
    a=p.parse_args()
    if a.plan:
        print(json.dumps({'target':a.target,'hostRequired':TARGETS[a.target],'prerequisites':json.loads((ROOT/'packaging/desktop-prerequisites.json').read_text())[a.target],
          'inputs':['pinned Go and offline module cache','pinned Node and installed locked frontend dependencies','verified native target bundle','third-party notices'],
          'layout':'bin/Trove[.exe], Resources/native, Resources/browser-clipper, Resources/web',
          'networkAcquisition':False,'platformQualified':False,'signing':'Separate target-specific release gate','sourceOnly':True},indent=2));return
    env=build_env(a.cache);env['CGO_ENABLED']='1';env['GOOS'],env['GOARCH']=a.target.split('-')
    details=prerequisites(a.target,env)
    go=go_binary(a.cache);lock=baseline()
    details['go']=command([go,'version'],env)
    node=shutil.which('node')
    if not node or command([node,'--version'],env)!='v'+lock['toolchain']['node']:
        raise ValueError('Install the pinned Node version before building; build never acquires tools')
    details['node']=command([node,'--version'],env)
    if a.check:
        print(json.dumps({'target':a.target,'prerequisites':details,'platformQualified':False},indent=2));return
    if not a.native_bundle or not a.release_notices or not a.output or not a.version or not re.fullmatch(r'\d+\.\d+\.\d+',a.version) or not a.build_number or not 0<a.build_number<2**64:
        raise ValueError('Build requires native bundle, release notices, unused output, three-component version and positive build number')
    output=a.output.absolute()
    if output.exists() or output.is_symlink():raise ValueError('Choose a new immutable output path')
    native=validate_native(a.native_bundle,a.target,lock)
    for f in ['provenance.json','THIRD-PARTY-NOTICES.txt']:
        if not (a.release_notices/f).is_file():raise ValueError('Release notices are incomplete')
    if not (a.release_notices/'notices').is_dir():raise ValueError('License notice directory is missing')
    if digest(ROOT/'frontend/pnpm-lock.yaml')!=digest(ROOT/'baseline/frontend/pnpm-lock.yaml'):
        raise ValueError('Frontend lock differs from reviewed baseline')
    for folder in ['frontend/node_modules/typescript','frontend/node_modules/vite']:
        info=json.loads((ROOT/folder/'package.json').read_text())
        expected=lock['frontend']['typescript' if folder.endswith('typescript') else 'vite']
        if info['version']!=expected:raise ValueError('Installed frontend tool differs from baseline')
    if command(['git','status','--porcelain','--untracked-files=normal'],env):
        raise ValueError('Commit reviewed inputs before creating an immutable desktop candidate')
    command([go,'mod','verify'],env)
    command([node,ROOT/'frontend/node_modules/typescript/bin/tsc','--noEmit'],env,ROOT/'frontend')
    command([node,ROOT/'frontend/node_modules/vite/bin/vite.js','build'],env,ROOT/'frontend')
    output.parent.mkdir(parents=True,exist_ok=True)
    stage=Path(tempfile.mkdtemp(prefix='.trove-desktop-',dir=output.parent))
    try:
        (stage/'bin').mkdir();(stage/'Resources').mkdir()
        copy_verified_native(a.native_bundle,stage/'Resources/native',a.target,lock)
        # Current reviewed catalogue is authoritative; workers never acquire packs.
        shutil.rmtree(stage/'Resources/native/models')
        copy_verified(ROOT/'native/models',stage/'Resources/native/models')
        copy_verified(ROOT/'browser-clipper',stage/'Resources/browser-clipper')
        copy_verified(ROOT/'frontend/dist',stage/'Resources/web')
        copy_verified(a.release_notices,stage/'Resources/release')
        tags=['desktop','production','fts5','sqlite_omit_load_extension']
        tags += ['wv2runtime.error'] if a.target=='windows-amd64' else ['webkit2_41']
        suffix='.exe' if a.target=='windows-amd64' else ''
        binaries={}
        for name,source in [('Trove','.'),('trove-mcp','./cmd/trove-mcp'),('trove-host','./cmd/trove-host'),('trove-format','./cmd/trove-format')]:
            binary=stage/'bin'/(name+suffix)
            cmd=[go,'build','-mod=readonly','-trimpath','-tags',' '.join(tags),'-o',binary,source]
            if a.target=='windows-amd64' and name=='Trove':cmd[2:2]=['-ldflags','-H windowsgui']
            command(cmd,env)
            if not binary_target(binary,a.target):raise ValueError('Built executable has wrong target: '+name)
            binaries[name]={'sha256':digest(binary),'dependencies':command(['objdump','-p',binary],env) if suffix else command(['ldd',binary],env)}
            if 'not found' in binaries[name]['dependencies']:raise ValueError('A desktop shared library is missing')
        compatibility=json.loads(command([stage/'bin'/('trove-format'+suffix)],env))
        if set(compatibility)!={'userVersion','storageGeneration'} or any(type(v)!=int or v<1 for v in compatibility.values()):raise ValueError('Invalid authoritative workspace format metadata')
        for name in (['install.ps1','uninstall.ps1','package.ps1'] if suffix else ['install.sh','uninstall.sh']):
            shutil.copyfile(ROOT/'packaging/portable'/name,stage/name);(stage/name).chmod(0o755)
        manifest={'schemaVersion':1,'appId':APP_ID,'version':a.version,'buildNumber':a.build_number,'target':a.target,
          'requiredWebKitGTKPackage':details['pins'].get('packages',{}).get('libwebkit2gtk-4.1-0'),'workspaceCompatibility':compatibility,'sourceCommit':command(['git','rev-parse','HEAD'],env),'sourceChanges':command(['git','status','--porcelain','--untracked-files=no'],env),
          'baselineSHA256':digest(ROOT/'baseline/baseline.lock.json'),'prerequisiteLockSHA256':digest(ROOT/'packaging/desktop-prerequisites.json'),
          'recordedAt':datetime.now(timezone.utc).isoformat(),'buildHost':details,'executables':binaries,'nativeCapabilities':native.get('capabilities',[]),
          'nativeUnavailable':native.get('unavailable',[]),'platformQualified':False,'releaseSigning':'Unqualified; unsigned build is not an approved distribution',
          'files':inventory(stage)}
        (stage/'desktop-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
        checksum=inventory(stage)
        (stage/'files.sha256').write_text(''.join(f"{x['sha256']}  {x['path']}\n" for x in checksum))
        # No artifact is published until all copied bytes and output identities exist.
        os.rename(stage,output)
        print(output/'desktop-manifest.json')
    except BaseException:
        shutil.rmtree(stage)
        raise

if __name__=='__main__':
    try:main()
    except (ValueError,RuntimeError,OSError,subprocess.CalledProcessError) as e:
        print(str(e),file=sys.stderr);sys.exit(1)
