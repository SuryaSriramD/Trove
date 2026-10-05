"""Restricted FFmpeg build policy. No network access or ambient codec discovery."""
import hashlib
import json
from pathlib import Path
import re
import tarfile
from devtools import ROOT, file_digest


def audio_pin():
    return json.loads((ROOT / 'native/audio-decoder.lock.json').read_text())


def configure_options(pin, target):
    options = ['--disable-everything', '--disable-autodetect', '--disable-network', '--disable-protocols',
        '--disable-programs', '--disable-doc', '--disable-gpl', '--disable-nonfree', '--disable-version3',
        '--disable-avdevice', '--disable-avfilter', '--disable-swscale', '--disable-hwaccels',
        '--enable-avformat', '--enable-avcodec', '--enable-swresample', '--enable-static', '--disable-shared',
        '--disable-asm', '--disable-debug', '--enable-pic']
    for name in ('demuxer', 'parser', 'decoder'):
        options.append('--enable-' + name + '=' + ','.join(pin[name + 's']))
    if target == 'windows-amd64':
        options += ['--toolchain=msvc', '--arch=x86_64', '--target-os=win64', '--extra-cflags=-MT']
    elif target == 'darwin-arm64':
        options += ['--arch=aarch64', '--target-os=darwin', '--cc=clang', '--cxx=clang++']
    else:
        options += ['--arch=x86_64', '--target-os=linux', '--cc=gcc', '--cxx=g++']
    return options


def verify_source(cache, name, pin):
    archive = cache / (name + '.tar.gz')
    if not archive.is_file() or file_digest(archive) != pin['sha256']:
        raise RuntimeError('Missing pinned ' + name + ' archive; run bootstrap.py --fetch --components native.')
    with tarfile.open(archive) as source:
        for member in source.getmembers():
            relative = Path(member.name)
            if relative.parts[0] != pin['sourceDirectory'] or member.issym() or member.islnk():
                raise RuntimeError('Unsafe source archive member: ' + member.name)
            path = cache / relative
            if not path.resolve().is_relative_to(cache.resolve()):
                raise RuntimeError('Source archive escapes cache')
            if member.isfile() and (path.is_symlink() or not path.is_file() or
                file_digest(path) != hashlib.sha256(source.extractfile(member).read()).hexdigest()):
                raise RuntimeError('Pinned audio build source modified: ' + member.name)
    return cache / pin['sourceDirectory']


def verify_config(build, pin):
    config = (build / 'config.h').read_text() + (build / 'config_components.h').read_text()
    for name in ('NETWORK', 'GPL', 'NONFREE', 'VERSION3', 'SHARED', 'AVDEVICE', 'AVFILTER', 'SWSCALE'):
        if not re.search(r'^#define CONFIG_' + name + r' 0$', config, re.M):
            raise RuntimeError('FFmpeg restriction missing: ' + name)
    for kind in ('PROTOCOL', 'DEMUXER', 'PARSER', 'DECODER', 'ENCODER', 'MUXER', 'FILTER', 'HWACCEL', 'INDEV', 'OUTDEV'):
        enabled = {name.lower() for name in re.findall(r'^#define CONFIG_(\w+)_' + kind + r' 1$', config, re.M)}
        expected = set(pin[kind.lower() + 's']) if kind in ('DEMUXER', 'PARSER', 'DECODER') else set()
        if enabled != expected:
            raise RuntimeError('Unexpected enabled FFmpeg ' + kind + ': ' + repr(enabled))
    return config
