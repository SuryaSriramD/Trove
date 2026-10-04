#!/usr/bin/env python3
"""Build the bounded macOS descriptor-only CoreAudio normalization helper."""
import argparse
import json
from pathlib import Path
import subprocess

from devtools import ROOT, cache_dir, file_digest

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--cache', type=Path, default=cache_dir())
parser.add_argument('--output', type=Path, default=ROOT / 'build/native')
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
output = args.output / 'audio-decoder'
source = ROOT / 'native/audio-decoder.swift'
subprocess.run(['swiftc', '-module-cache-path', str(cache_dir(args.cache) / 'swift-module-cache'),
                '-swift-version', '5', '-O', '-target', 'arm64-apple-macos27.0',
                '-framework', 'AudioToolbox', str(source), '-o', str(output)], check=True)
subprocess.run(['codesign', '--force', '--sign', '-', str(output)], check=True)
subprocess.run(['codesign', '--verify', '--strict', str(output)], check=True)
for line in subprocess.check_output(['otool', '-L', str(output)], text=True).splitlines()[1:]:
    library = line.strip().split(' ')[0]
    if not library.startswith(('/usr/lib/', '/System/Library/', '/Library/Apple/System/')):
        raise SystemExit('Unbundled audio decoder dependency: ' + library)
(args.output / 'audio-decoder.json').write_text(json.dumps({
    'schemaVersion': 1, 'name': output.name, 'sha256': file_digest(output),
    'sourceSha256': file_digest(source), 'runtime': 'CoreAudio', 'runtimeCommit': 'trove-audio-decoder-v1',
    'architecture': 'arm64', 'platform': 'darwin', 'minimumMacOS': '27.0',
    'capabilities': {'containers': ['WAV', 'MP3', 'AAC', 'M4A'], 'inputChannels': [1, 2],
                     'minimumSampleRate': 8000, 'maximumSampleRate': 192000,
                     'outputSampleRate': 16000, 'outputChannels': 1, 'outputFormat': 'f32le',
                     'maximumDurationMs': 7200000, 'maximumInputBytes': 512 << 20,
                     'platformDependent': True, 'originalBytesRemainCanonical': True},
}, indent=2) + '\n')
print(output)
