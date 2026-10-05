"""Read-only, target-native desktop dependency checks shared by builder and doctor."""
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
from native_targets import require_native_host
ROOT = Path(__file__).resolve().parents[1]

def command(argv, env, cwd=ROOT):
    return subprocess.check_output([str(x) for x in argv], env=env, cwd=cwd, text=True).strip()

def windows_webview2(minimum, registry=None):
    """Read documented runtime registration, without launching installers or network tools."""
    if registry is None:
        import winreg as registry
    guid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    locations = [(registry.HKEY_LOCAL_MACHINE, r'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients' + chr(92) + guid),
                 (registry.HKEY_CURRENT_USER, r'Software\Microsoft\EdgeUpdate\Clients' + chr(92) + guid)]
    versions = []
    for hive, path in locations:
        try:
            with registry.OpenKey(hive, path, 0, registry.KEY_READ | registry.KEY_WOW64_64KEY) as key:
                value, kind = registry.QueryValueEx(key, 'pv')
                if kind == registry.REG_SZ and re.fullmatch(r'\d+\.\d+\.\d+\.\d+', value):
                    versions.append(tuple(map(int, value.split('.'))))
        except OSError:
            continue
    if not versions or max(versions) < tuple(map(int, minimum.split('.'))):
        raise ValueError('Install the approved offline x64 WebView2 Evergreen Standalone Runtime; this tool never downloads it')
    return '.'.join(map(str, max(versions)))

def prerequisites(target, env):
    require_native_host(target)
    pins=json.loads((ROOT/'packaging/desktop-prerequisites.json').read_text())[target]
    result={'host': platform.platform(), 'pins': pins}
    compiler=env.get('CC','gcc')
    result['compiler']=command([compiler,'--version'],env)
    machine=command([compiler,'-dumpmachine'],env)
    if target=='windows-amd64':
        if sys.getwindowsversion().build<22000 or not machine.startswith('x86_64-w64-mingw32'):
            raise ValueError('Windows 11 x64 and x64 MinGW GCC are required for the CGO desktop build')
        result['webview2Runtime'] = windows_webview2(pins['minimumWebView2'])
    else:
        release=Path('/etc/os-release').read_text()
        if not re.search(r'^ID=ubuntu$',release,re.M) or not re.search(r'^VERSION_ID="24\.04"$',release,re.M) or not machine.startswith('x86_64-linux-gnu'):
            raise ValueError('The desktop build requires Ubuntu 24.04 x64')
        result['pkgConfig']={p:command(['pkg-config','--modversion',p],env) for p in ['gtk+-3.0','webkit2gtk-4.1','libsoup-3.0']}
        for package,version in pins['pkgConfig'].items():
            if result['pkgConfig'][package]!=version:
                raise ValueError('Review the pinned WebKitGTK environment before building: '+package)
        packages=list(pins['packages'])+pins['additionalBuildPackages']
        result['packages']={p:command(['dpkg-query','-W','-f=${Version}',p],env) for p in packages}
        for package,version in pins['packages'].items():
            if result['packages'][package]!=version:
                raise ValueError('Pinned Ubuntu package differs: '+package)
        result['osRelease']=release
    return result

