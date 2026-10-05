#!/usr/bin/env python3
"""Build the pinned restricted static FFmpeg helper on its native target host."""
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

from devtools import ROOT, baseline, cache_dir, file_digest, workspace_cache
from native_targets import TARGETS, host_target, require_native_host
from audio_build import audio_pin, configure_options, verify_source, verify_config


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', type=Path, default=cache_dir())
    parser.add_argument('--output', type=Path, default=ROOT / 'build/native')
    parser.add_argument('--target', choices=TARGETS)
    parser.add_argument('--plan', action='store_true')
    args = parser.parse_args()
    target = args.target or host_target()
    pin = audio_pin()
    options = configure_options(pin, target)
    if args.plan:
        print(json.dumps({'target': target, 'source': pin, 'configure': options,
            'requires': 'Native C++ toolchain, GNU make, bash. Windows: MSYS2 bash/make on PATH in x64 MSVC developer environment.',
            'qualification': 'Target execution must be recorded separately.'}, indent=2))
        return 0
    require_native_host(target)
    if target == 'linux-amd64':
        release = Path('/etc/os-release').read_text()
        if not re.search(r'^ID=ubuntu$', release, re.M) or not re.search(r'^VERSION_ID="24\.04"$', release, re.M):
            raise RuntimeError('Initial Linux decoder builds require Ubuntu 24.04 LTS.')
    cache = cache_dir(args.cache)
    source = verify_source(cache, 'ffmpeg-audio', pin)
    llama = verify_source(cache, 'llama', baseline()['workers']['llama'])
    build = workspace_cache(cache) / ('ffmpeg-audio-' + target)
    build.mkdir(parents=True, exist_ok=True)
    output = args.output.resolve(); output.mkdir(parents=True, exist_ok=True)
    commands = []
    env = dict(os.environ)
    # Avoid injected compilation/link flags or dynamic library lookup paths.
    for key in ('CFLAGS', 'CXXFLAGS', 'CPPFLAGS', 'LDFLAGS', 'LD_PRELOAD', 'LD_LIBRARY_PATH', 'DYLD_LIBRARY_PATH'):
        env.pop(key, None)
    env['LC_ALL'] = 'C'
    if target == 'windows-amd64':
        if not env.get('VCToolsVersion') or not env.get('WindowsSDKVersion'):
            raise RuntimeError('Use x64 MSVC developer environment with MSYS2 bash and make on PATH.')
        # Disable MSYS automatic rewriting of explicit native compiler arguments.
        env['MSYS2_ARG_CONV_EXCL'] = '*'
    def run(command, cwd=build):
        commands.append(list(map(str, command)))
        subprocess.run(commands[-1], cwd=cwd, env=env, check=True)
    configuration = ['bash', (source / 'configure').as_posix()] + options
    config_identity = json.dumps(configuration)
    marker = build / 'trove-configuration.json'
    if not marker.exists() or marker.read_text() != config_identity:
        if marker.exists():
            raise RuntimeError('Audio build options changed; choose a new cache or remove this build directory explicitly.')
        run(configuration)
        marker.write_text(config_identity)
    config = verify_config(build, pin)
    run(['make', '-j4'])
    public_headers = build / 'public-include'
    run(['make', 'install-headers', 'includedir=' + public_headers.as_posix()])
    source_cpp = ROOT / 'native/audio-decoder.cpp'
    object_file = build / ('audio-decoder.obj' if target == 'windows-amd64' else 'audio-decoder.o')
    libraries = [build / ('lib' + name) / ((name + '.lib') if target == 'windows-amd64' else ('lib' + name + '.a')) for name in ('avformat', 'avcodec', 'swresample', 'avutil')]
    filename = 'audio-decoder' + ('.exe' if target == 'windows-amd64' else '')
    binary = output / filename
    if target == 'windows-amd64':
        # FFmpeg's MSVC toolchain emits av*.lib static archives.
        compile_command = ['cl', '/nologo', '/std:c++17', '/EHsc', '/utf-8', '/O2', '/MT', '/c', source_cpp,
            '/I' + str(public_headers), '/I' + str(llama / 'vendor'), '/Fo' + str(object_file)]
        link_command = ['link', '/NOLOGO', '/OUT:' + str(binary), object_file, *libraries, 'bcrypt.lib']
    else:
        compiler = 'clang++' if target == 'darwin-arm64' else 'g++'
        compile_command = [compiler, '-std=c++17', '-O2', '-Wno-deprecated-declarations', '-I' + str(public_headers), '-I' + str(llama / 'vendor'), '-c', source_cpp, '-o', object_file]
        link_command = [compiler, object_file, *libraries, '-lm', '-lpthread', '-o', binary]
    run(compile_command); run(link_command)
    if target == 'darwin-arm64':
        run(['codesign', '--force', '--sign', '-', binary])
        run(['codesign', '--verify', '--strict', binary])
        dependencies = subprocess.check_output(['otool', '-L', str(binary)], text=True)
        for line in dependencies.splitlines()[1:]:
            library = line.strip().split(' ')[0]
            if not library.startswith(('/usr/lib/', '/System/Library/', '/Library/Apple/System/')):
                raise RuntimeError('Unbundled audio decoder dependency: ' + library)
    elif target == 'windows-amd64':
        dependencies = subprocess.check_output(['dumpbin', '/DEPENDENTS', str(binary)], text=True)
        libraries_used = re.findall(r'^\s+([\w.-]+\.dll)\s*$', dependencies, re.M | re.I)
        if not libraries_used or any(x.lower() not in {'kernel32.dll', 'bcrypt.dll', 'advapi32.dll'} and not x.lower().startswith('api-ms-win-') for x in libraries_used):
            raise RuntimeError('Unexpected audio decoder DLL dependencies: ' + dependencies)
    else:
        dependencies = subprocess.check_output(['ldd', str(binary)], text=True)
        libraries_used = re.findall(r'^\s*(\S+)\s+=>', dependencies, re.M)
        allowed = {'libstdc++.so.6', 'libm.so.6', 'libgcc_s.so.1', 'libc.so.6', 'libpthread.so.0', 'libdl.so.2', 'librt.so.1'}
        if 'not found' in dependencies or any(x not in allowed for x in libraries_used):
            raise RuntimeError('Unexpected audio decoder ELF dependencies: ' + dependencies)
    # Static LGPL linking: ship exact corresponding source, config, linkable
    # application object, libraries, headers and a relocation-independent recipe.
    kit = output / 'audio-relink'; kit.mkdir(exist_ok=True)
    shutil.copyfile(cache / 'ffmpeg-audio.tar.gz', kit / 'ffmpeg-source.tar.gz')
    for name in ('LICENSE.md', 'COPYING.LGPLv2.1'):
        shutil.copyfile(source / name, kit / name)
    shutil.copyfile(object_file, kit / object_file.name)
    for library in libraries:
        shutil.copyfile(library, kit / library.name)
    for name in ('audio-decoder.cpp', 'worker-platform.hpp', 'audio-decoder.lock.json'):
        shutil.copyfile(ROOT / 'native' / name, kit / name)
    shutil.copyfile(llama / 'vendor/nlohmann/json.hpp', kit / 'json.hpp')
    shutil.copyfile(ROOT / 'docs/phase-3/licenses/nlohmann-json-3.12.0-LICENSE.MIT', kit / 'nlohmann-LICENSE.MIT')
    (kit / 'ffmpeg-configuration.txt').write_text(config)
    relink = [str(x) for x in link_command]
    relink = [Path(x).name if x in [str(object_file), str(binary), *map(str, libraries)] else
        ('/OUT:' + filename if x.startswith('/OUT:') else x) for x in relink]
    (kit / 'relink.json').write_text(json.dumps(relink, indent=2) + '\n')
    (kit / 'relink.py').write_text('import json, pathlib, subprocess\nroot=pathlib.Path(__file__).resolve().parent\nsubprocess.run(json.loads((root/"relink.json").read_text()),cwd=root,check=True)\n')
    (kit / 'README.txt').write_text('Trove audio decoder uses FFmpeg under LGPL v2.1 or later. The FFmpeg source is unmodified.\n'
        'The included object and static libraries support relinking a modified compatible FFmpeg. Run python relink.py with the recorded native compiler installed.\n'
        'Use the recorded configure arguments to rebuild the included source; replace these libraries before relinking. No network protocols or additional codecs are required by Trove.\n'
        'A modified helper requires its audio-decoder.json SHA256 to be updated for a private development bundle. Public app signatures and release verification do not approve modified binaries.\n'
        'Release distribution must retain this kit and notices, permit LGPL-required reverse engineering for debugging modifications, and review codec/patent obligations for the distribution jurisdiction.\n')
    manifest = {'schemaVersion': 1, 'name': 'audio-decoder', 'sha256': file_digest(binary),
        'sourceSha256': file_digest(source_cpp), 'runtime': 'FFmpeg', 'runtimeCommit': pin['runtimeCommit'],
        'architecture': TARGETS[target]['arch'], 'platform': TARGETS[target]['os'],
        'capabilities': {'containers': ['WAV', 'MP3', 'AAC', 'M4A'], 'inputChannels': [1, 2],
            'minimumSampleRate': 8000, 'maximumSampleRate': 192000, 'outputSampleRate': 16000,
            'outputChannels': 1, 'outputFormat': 'f32le', 'maximumDurationMs': 7200000,
            'maximumInputBytes': 512 << 20, 'originalBytesRemainCanonical': True, 'networkProtocols': [],
            'decoders': pin['decoders']}}
    (output / 'audio-decoder.json').write_text(json.dumps(manifest, indent=2) + '\n')
    provenance = {'recordedAt': datetime.now(timezone.utc).isoformat(), 'target': target, 'host': platform.platform(),
        'sourcePin': pin, 'configureCommand': configuration, 'commands': commands, 'dependencies': dependencies, 'sourceSha256': {
            name: file_digest(ROOT / name) for name in ('native/audio-decoder.cpp', 'native/worker-platform.hpp',
            'scripts/build-audio-decoder.py', 'scripts/audio_build.py')},
        'compiler': subprocess.check_output(['clang++' if target == 'darwin-arm64' else 'g++', '--version'], text=True) if target != 'windows-amd64' else {
            'VCToolsVersion': env['VCToolsVersion'], 'WindowsSDKVersion': env['WindowsSDKVersion']},
        'qualification': 'Build evidence only. Fixture, cancellation and hostile-input results must be recorded separately.'}
    (kit / 'build-provenance.json').write_text(json.dumps(provenance, indent=2) + '\n')
    print(binary)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
