#!/usr/bin/env python3
"""Build pinned Windows/Ubuntu CPU worker bundles on their native target host.

This is a worker bundle builder, not a Windows installer or Linux desktop package.
Use --plan anywhere. Actual builds reject a mismatched host and record provenance.
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
import subprocess
import sys
import tarfile

from devtools import cache_dir, file_digest, workspace_cache
from native_targets import TARGETS, cpu_options, host_target, require_native_host

ROOT = Path(__file__).resolve().parents[1]


def verified_source(cache, name, pin):
    archive = cache / (name + '.tar.gz')
    if not archive.is_file() or file_digest(archive) != pin['sha256']:
        raise RuntimeError('Missing verified source archive: ' + str(archive) + '; acquire with scripts/bootstrap.py --fetch --components native')
    with tarfile.open(archive) as tar:
        for entry in tar.getmembers():
            if not entry.isfile():
                continue
            destination = cache / entry.name
            if not destination.resolve().is_relative_to(cache.resolve()):
                raise RuntimeError('Archive path escapes native cache')
            if not destination.is_file() or destination.is_symlink() or file_digest(destination) != hashlib.sha256(tar.extractfile(entry).read()).hexdigest():
                raise RuntimeError('Native source differs from lock: ' + entry.name)
    return cache / pin['sourceDirectory']


def retain_notices(output, sources):
    notices = output / 'notices'
    notices.mkdir(exist_ok=True)
    for name, source in sources.items():
        shutil.copyfile(source / 'LICENSE', notices / (name + '-LICENSE.txt'))
    source = sources['llama']
    for directory in ('stb', 'miniaudio', 'hash', 'sheredom'):
        for item in (source / 'vendor' / directory).rglob('*'):
            if item.is_file() and any(word in item.name.lower() for word in ('license', 'copying')):
                shutil.copyfile(item, notices / ('vision-' + str(item.relative_to(source / 'vendor')).replace(chr(92), '-').replace('/', '-')))
    for relative in ('stb/stb_image.h', 'miniaudio/miniaudio.h', 'sheredom/subprocess.h'):
        item = source / 'vendor' / relative
        text = item.read_text()
        notice = text[:text.index('#ifndef SHEREDOM_SUBPROCESS_H_INCLUDED')] if relative.startswith('sheredom') else text[text.rfind('/*'):]
        (notices / ('vision-' + item.stem + '-LICENSE.txt')).write_text(notice)
    sha1 = (source / 'vendor/hash/sha1/sha1.c').read_text()
    (notices / 'vision-hash-sha1-PUBLIC-DOMAIN.txt').write_text(sha1[:sha1.index('*/') + 2] + '\n')
    json_header = (source / 'vendor/nlohmann/json.hpp').read_text()
    (notices / 'nlohmann-COPYRIGHT.txt').write_text('\n'.join(sorted({line for line in json_header.splitlines() if 'SPDX-FileCopyrightText:' in line or 'SPDX-License-Identifier:' in line})) + '\n')
    for name in ('smolvlm-500m-model-card.md', 'smolvlm-APACHE-2.0.txt', 'nlohmann-json-3.12.0-LICENSE.MIT'):
        shutil.copyfile(ROOT / 'docs/phase-3/licenses' / name, notices / name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', choices=TARGETS)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--output', type=Path)
    parser.add_argument('--plan', action='store_true')
    args = parser.parse_args()
    target = args.target or host_target()
    spec = TARGETS[target]
    if args.plan:
        print(json.dumps({'target': target, **spec, 'mode': 'native host only', 'models': 'separate portable hash-pinned packs',
            'qualification': 'No platform execution is established by this plan.'}, indent=2))
        return 0
    require_native_host(target)
    if target == 'darwin-arm64':
        raise RuntimeError('Use scripts/build-native.py for the existing macOS Metal/Swift bundle.')
    if platform.system() == 'Linux':
        release = Path('/etc/os-release').read_text()
        if not re.search(r'^ID=ubuntu$', release, re.M) or not re.search(r'^VERSION_ID="24\.04"$', release, re.M):
            raise RuntimeError('The initial Linux worker target requires Ubuntu 24.04 LTS.')
    cache = cache_dir(args.cache)
    output = (args.output or ROOT / 'build' / ('native-' + target)).resolve()
    if output.exists() and any(output.iterdir()):
        raise RuntimeError('Choose an empty output directory; do not mix candidates or target binaries.')
    lock = json.loads((ROOT / 'baseline/baseline.lock.json').read_text())
    cmake = subprocess.check_output(['cmake', '--version'], text=True).splitlines()[0]
    if cmake != 'cmake version ' + lock['toolchain']['cmake']:
        raise RuntimeError('CMake differs from the pinned build tool: ' + cmake)
    sources = {name: verified_source(cache, name, lock['workers'][name]) for name in ('llama', 'whisper')}
    dependencies = {}
    if spec['os'] == 'linux':
        dependencies['systemPackages'] = subprocess.check_output(['dpkg-query', '-W',
            'gcc', 'g++', 'libc6', 'libstdc++6', 'libssl-dev', 'libssl3t64', 'libpulse-dev', 'libpulse0'], text=True)
    else:
        dependencies['VCToolsVersion'] = os.environ.get('VCToolsVersion')
        dependencies['WindowsSDKVersion'] = os.environ.get('WindowsSDKVersion')
        if not dependencies['VCToolsVersion'] or not dependencies['WindowsSDKVersion']:
            raise RuntimeError('Run in an x64 Native Tools developer prompt with an identified MSVC toolset and Windows SDK.')
    output.mkdir(parents=True, exist_ok=True)
    commands, helpers, configurations = [], [], []
    for runtime in ('llama', 'whisper'):
        build = workspace_cache(cache) / ('workers-' + target + '-' + runtime)
        options = ['-DCMAKE_BUILD_TYPE=Release', '-DTROVE_RUNTIME=' + runtime,
            '-DTROVE_RUNTIME_SOURCE=' + str(sources[runtime]), '-DTROVE_JSON_INCLUDE=' + str(sources['llama'] / 'vendor/nlohmann')]
        options += cpu_options()
        if spec['os'] == 'windows':
            options += ['-DCMAKE_C_COMPILER=cl', '-DCMAKE_CXX_COMPILER=cl', '-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded']
        if runtime == 'llama':
            options += ['-DLLAMA_BUILD_IS_DEV=OFF', '-DLLAMA_BUILD_COMMON=OFF', '-DLLAMA_BUILD_TESTS=OFF', '-DLLAMA_BUILD_TOOLS=OFF',
                '-DLLAMA_BUILD_EXAMPLES=OFF', '-DLLAMA_BUILD_SERVER=OFF', '-DLLAMA_BUILD_APP=OFF', '-DLLAMA_OPENSSL=OFF',
                '-DLLAMA_BUILD_MTMD=ON', '-DLLAMA_SUBPROCESS=OFF', '-DMTMD_VIDEO=OFF']
            names = ['llama-worker', 'vision-worker']
        else:
            options += ['-DWHISPER_BUILD_IS_DEV=OFF', '-DWHISPER_BUILD_EXAMPLES=OFF', '-DWHISPER_BUILD_TESTS=OFF', '-DWHISPER_BUILD_SERVER=OFF', '-DWHISPER_CURL=OFF']
            names = ['whisper-worker']
        for cmd in (['cmake', '-S', str(ROOT / 'native'), '-B', str(build), '-G', 'Ninja'] + options,
                    ['cmake', '--build', str(build), '--target'] + names + ['--parallel', '4']):
            commands.append(cmd)
            subprocess.run(cmd, check=True)
        configurations.append({'runtime': runtime, 'cache': (build / 'CMakeCache.txt').read_text(),
            'compilerConfiguration': {str(p.relative_to(build)): p.read_text() for p in (build / 'CMakeFiles').rglob('CMake*C*Compiler.cmake')}})
        for name in names:
            filename = name + ('.exe' if spec['os'] == 'windows' else '')
            binary = build / 'workers' / filename
            shutil.copyfile(binary, output / filename)
            (output / filename).chmod(0o755)
            if spec['os'] == 'windows':
                deps = subprocess.check_output(['dumpbin', '/DEPENDENTS', str(binary)], text=True)
                imported = re.findall(r'^\s+([\w.-]+\.dll)\s*$', deps, re.M | re.I)
                # /MT avoids redistributing a floating VC runtime. All imports must
                # be Windows system libraries or universal CRT API-set forwarders.
                allowed = {'kernel32.dll', 'advapi32.dll', 'bcrypt.dll', 'user32.dll', 'ole32.dll', 'shell32.dll', 'ws2_32.dll', 'ntdll.dll', 'secur32.dll', 'crypt32.dll'}
                unknown = [x for x in imported if x.lower() not in allowed and not x.lower().startswith('api-ms-win-')]
                if not imported or unknown:
                    raise RuntimeError('Review unexpected native DLL dependencies: ' + repr(unknown or deps))
            else:
                deps = subprocess.check_output(['ldd', str(binary)], text=True)
                if 'not found' in deps:
                    raise RuntimeError('Missing worker shared library: ' + deps)
                imported = re.findall(r'^\s*(\S+)\s+=>', deps, re.M)
                allowed = {'libcrypto.so.3', 'libstdc++.so.6', 'libm.so.6', 'libgcc_s.so.1', 'libc.so.6', 'libpthread.so.0', 'libdl.so.2', 'librt.so.1'}
                if any(name not in allowed for name in imported):
                    raise RuntimeError('Review unexpected native ELF dependencies: ' + deps)
            helpers.append({'name': name, 'sha256': file_digest(output / filename), 'runtime': runtime + '.cpp',
                'runtimeCommit': lock['workers'][runtime]['commit'], 'architecture': spec['arch']})
            (output / (filename + '.dependencies.txt')).write_text(deps)
    subprocess.run([sys.executable, str(ROOT / 'scripts/build-audio-decoder.py'), '--target', target,
        '--cache', str(cache), '--output', str(output)], check=True)
    subprocess.run([sys.executable, str(ROOT / 'scripts/build-microphone.py'), '--target', target,
        '--cache', str(cache), '--output', str(output)], check=True)
    subprocess.run([sys.executable, str(ROOT / 'scripts/build-ocr-portable.py'), '--target', target,
        '--cache', str(cache / 'ocr'), '--output', str(output)], check=True)
    microphone = json.loads((output / 'microphone-worker.json').read_text())
    if microphone['testOnly'] or not microphone['deviceBackendCompiled']:
        raise RuntimeError('A synthetic fixture cannot enter a production native bundle.')
    helpers.append(microphone['helper'])
    shutil.copytree(ROOT / 'native/models', output / 'models')
    retain_notices(output, sources)
    fixtures = output / 'fixtures'; fixtures.mkdir()
    shutil.copyfile(sources['whisper'] / 'samples/jfk.wav', fixtures / 'jfk.wav')
    manifest = {'schemaVersion': 2, 'platform': spec['os'], 'architecture': spec['arch'], 'helpers': helpers,
        'minimumOS': spec['minimumOS'], 'baselineLockSHA256': file_digest(ROOT / 'baseline/baseline.lock.json'),
        'capabilities': ['generate', 'tokenize', 'embed', 'transcribe', 'image_generate', 'audio_decode', 'record', 'ocr_english'],
        'unavailable': ['system audio capture', 'native document/PDF helpers'],
        'executionQualification': 'Not established by build. Run model, protocol, cancellation and packaged desktop checks on this target.'}
    (output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    (output / 'build-provenance.json').write_text(json.dumps({'recordedAt': datetime.now(timezone.utc).isoformat(),
        'target': target, 'host': platform.platform(), 'python': platform.python_version(), 'cmake': cmake, 'dependencies': dependencies,
        'sourcePins': lock['workers'], 'commands': commands, 'configurations': configurations,
        'sourceSha256': {str(p.relative_to(ROOT)): file_digest(p) for p in sorted((ROOT / 'native').glob('*')) if p.is_file()},
        'distribution': 'Unsigned worker bundle; no installer, WebView, signing, update or platform release qualification.'}, indent=2) + '\n')
    print(output / 'manifest.json')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
