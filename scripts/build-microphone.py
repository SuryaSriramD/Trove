#!/usr/bin/env python3
"""Build the pinned portable microphone worker, or a separate device-free fixture."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess

from devtools import ROOT, baseline, cache_dir, file_digest, workspace_cache
from audio_build import verify_source
from native_targets import TARGETS, host_target, require_native_host


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', choices=TARGETS)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--fixture-only', action='store_true', help='compile synthetic fixture with no device backend; never adds a production bundle manifest')
    parser.add_argument('--fixture-startup-stall', action='store_true', help='test-only stalled startup; requires --fixture-only')
    parser.add_argument('--plan', action='store_true')
    args = parser.parse_args()
    if args.fixture_startup_stall and not args.fixture_only:
        parser.error('--fixture-startup-stall requires --fixture-only')
    target = args.target or host_target()
    pin = json.loads((ROOT / 'native/microphone.lock.json').read_text())
    if args.plan:
        print(json.dumps({'target': target, 'pin': pin, 'fixtureOnly': args.fixture_only, 'startupStallFixture': args.fixture_startup_stall,
            'deviceExecution': 'Unqualified; fixture output never proves device/permission behavior.',
            'requires': 'macOS arm64 Xcode/macOS 27 SDK, Windows x64 MSVC/SDK, or Ubuntu 24.04 GCC/libpulse-dev.'}, indent=2))
        return 0
    require_native_host(target)
    if target == 'linux-amd64':
        release = Path('/etc/os-release').read_text()
        if not re.search(r'^ID=ubuntu$', release, re.M) or not re.search(r'^VERSION_ID="24\.04"$', release, re.M):
            raise RuntimeError('Initial Linux capture builds require Ubuntu 24.04 LTS.')
    cache = cache_dir(args.cache)
    source = verify_source(cache, 'llama', baseline()['workers']['llama'])
    header = source / 'vendor/miniaudio/miniaudio.h'
    if file_digest(header) != pin['headerSha256']:
        raise RuntimeError('miniaudio differs from the pinned capture header.')
    output = args.output.resolve()
    if (not args.fixture_only and (output / 'microphone-fixture.json').exists()) or (args.fixture_only and any((output / name).exists() for name in ('manifest.json', 'microphone-worker.json'))):
        raise RuntimeError('Do not mix device-free fixtures with production helpers.')
    output.mkdir(parents=True, exist_ok=True)
    build = workspace_cache(cache) / ('microphone-' + target + ('-fixture' if args.fixture_only else '-production'))
    build.mkdir(parents=True, exist_ok=True)
    filename = 'microphone-worker' + ('.exe' if target == 'windows-amd64' else '')
    binary = output / filename
    flags = [('/D' if target == 'windows-amd64' else '-D') + 'TROVE_CAPTURE_FIXTURE_ONLY'] if args.fixture_only else []
    if args.fixture_startup_stall:
        flags += [('/D' if target == 'windows-amd64' else '-D') + 'TROVE_CAPTURE_FIXTURE_STARTUP_STALL']
    toolchain = {'python': platform.python_version()}
    if target == 'windows-amd64':
        toolchain.update({name: os.environ.get(name) for name in ('VCToolsVersion', 'WindowsSDKVersion')})
        if not all(toolchain[name] for name in ('VCToolsVersion', 'WindowsSDKVersion')):
            raise RuntimeError('Run in an x64 MSVC developer prompt with an identified toolset and Windows SDK.')
    else:
        toolchain['compiler'] = subprocess.check_output(['clang++' if target == 'darwin-arm64' else 'g++', '--version'], text=True)
        if target == 'linux-amd64':
            toolchain['systemPackages'] = subprocess.check_output(['dpkg-query', '-W', 'gcc', 'g++', 'libc6', 'libstdc++6', 'libpulse-dev', 'libpulse0'], text=True)
        else:
            toolchain['sdk'] = subprocess.check_output(['xcrun', '--show-sdk-version'], text=True).strip()
    cpp = ROOT / 'native/microphone-worker.cpp'
    if target == 'windows-amd64':
        command = ['cl', '/nologo', '/std:c++17', '/O2', '/EHsc', '/utf-8', '/MT',
            '/I' + str(source / 'vendor'), str(cpp), '/Fe:' + str(binary),
            '/Fo:' + str(build / 'microphone-worker.obj'), *flags,
            '/link', 'ole32.lib', 'uuid.lib', 'avrt.lib', 'bcrypt.lib']
    else:
        command = ['clang++' if target == 'darwin-arm64' else 'g++', '-std=c++17', '-O2',
            '-Wno-deprecated-declarations', '-I' + str(source / 'vendor'), str(cpp),
            '-lpthread', '-lm', '-o', str(binary), *flags]
        if target == 'darwin-arm64':
            command += ['-arch', 'arm64', '-mmacosx-version-min=27.0']
            if not args.fixture_only:
                command[1:1] = ['-x', 'objective-c++', '-fobjc-arc', '-fblocks']
                command += ['-framework', 'CoreFoundation', '-framework', 'CoreAudio', '-framework', 'AudioToolbox',
                    '-framework', 'AVFoundation', '-framework', 'Foundation',
                    '-Wl,-sectcreate,__TEXT,__info_plist,' + str(ROOT / 'packaging/microphone-info.plist')]
        if target == 'linux-amd64' and not args.fixture_only:
            command += ['-lpulse']
    subprocess.run(command, cwd=build, check=True)
    if target == 'darwin-arm64':
        subprocess.run(['codesign', '--force', '--sign', '-', *([] if args.fixture_only else ['--entitlements', str(ROOT / 'packaging/microphone.entitlements.plist')]), str(binary)], check=True)
        subprocess.run(['codesign', '--verify', '--strict', str(binary)], check=True)
        dependencies = subprocess.check_output(['otool', '-L', str(binary)], text=True)
        for line in dependencies.splitlines()[1:]:
            if not line.strip().split(' ')[0].startswith(('/usr/lib/', '/System/Library/')):
                raise RuntimeError('Unexpected capture fixture dependency: ' + line)
    elif target == 'windows-amd64':
        dependencies = subprocess.check_output(['dumpbin', '/DEPENDENTS', str(binary)], text=True)
        libraries = re.findall(r'^\s+([\w.-]+\.dll)\s*$', dependencies, re.M | re.I)
        allowed = {'kernel32.dll', 'ole32.dll', 'avrt.dll', 'advapi32.dll', 'bcrypt.dll', 'user32.dll'}
        if not libraries or any(x.lower() not in allowed and not x.lower().startswith('api-ms-win-') for x in libraries):
            raise RuntimeError('Unexpected capture DLL imports: ' + dependencies)
    else:
        dependencies = subprocess.check_output(['ldd', str(binary)], text=True)
        if 'not found' in dependencies:
            raise RuntimeError('Missing capture dependencies: ' + dependencies)
        # PulseAudio itself has distribution-maintained transitive libraries.
        # Retain the complete inventory; clean-host qualification remains open.
    record = {'name': 'microphone-worker', 'sha256': file_digest(binary), 'runtime': 'microphone',
        'runtimeCommit': pin['runtimeCommit'] + ('-fixture' if args.fixture_only else ''),
        'architecture': TARGETS[target]['arch']}
    sidecar = {'schemaVersion': 1, 'platform': TARGETS[target]['os'], 'helper': record,
        'testOnly': args.fixture_only, 'deviceBackendCompiled': not args.fixture_only, 'startupStallFixture': args.fixture_startup_stall,
        'capabilities': ['record_synthetic', 'capture_controls_v1', 'input_devices_v1'] if args.fixture_only else ['record', 'capture_controls_v1', 'input_devices_v1'],
        'permissionState': 'not_requested', 'deviceState': 'not_probed', 'deviceQualification': 'Unqualified',
        'runtimeSystemLibraries': ['kernel32.dll', 'user32.dll', 'advapi32.dll', 'ole32.dll', 'avrt.dll', 'mmdevapi.dll'] if target == 'windows-amd64' else [],
        'sourcePin': pin, 'sourceSha256': {p.name: file_digest(p) for p in (
            cpp, ROOT / 'native/capture-buffer.hpp', ROOT / 'native/capture-startup.hpp', ROOT / 'native/worker-platform.hpp')},
        'macOSConsent': {'implementation': 'AVCaptureDevice status/requestAccess before CoreAudio initialization',
            'permissionWaitSeconds': 20, 'deviceQualification': 'Unqualified',
            'embeddedPlistSha256': file_digest(ROOT / 'packaging/microphone-info.plist'),
            'entitlementsSha256': file_digest(ROOT / 'packaging/microphone.entitlements.plist')} if target == 'darwin-arm64' and not args.fixture_only else None,
        'commands': [command], 'toolchain': toolchain, 'dependencies': dependencies, 'host': platform.platform(),
        'recordedAt': datetime.now(timezone.utc).isoformat()}
    (output / ('microphone-fixture.json' if args.fixture_only else 'microphone-worker.json')).write_text(json.dumps(sidecar, indent=2) + '\n')
    notices = output / 'notices'; notices.mkdir(exist_ok=True)
    text = header.read_text()
    (notices / 'miniaudio-LICENSE.txt').write_text(text[text.rfind('/*'):])
    shutil.copyfile(ROOT / 'docs/phase-3/licenses/nlohmann-json-3.12.0-LICENSE.MIT', notices / 'microphone-nlohmann-LICENSE.MIT')
    print(binary)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
