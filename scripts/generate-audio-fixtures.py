#!/usr/bin/env python3
"""Explicitly regenerate synthetic codec fixtures; ffmpeg is a test authoring tool only."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import shutil
import struct
import subprocess
import wave

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--output', type=Path, default=ROOT / 'internal/native/testdata/audio')
args = parser.parse_args()
encoder = shutil.which('ffmpeg')
if not encoder:
    raise SystemExit('ffmpeg is needed only to regenerate these fixtures, never by Trove at runtime')
args.output.mkdir(parents=True, exist_ok=True)
source = args.output / 'tones.wav'
with wave.open(str(source), 'wb') as output:
    output.setnchannels(2)
    output.setsampwidth(2)
    output.setframerate(48000)
    samples = bytearray()
    for frame in range(48000):
        for frequency in (330, 660):
            samples += struct.pack('<h', round(0.3 * 32767 * math.sin(2 * math.pi * frequency * frame / 48000)))
    output.writeframes(samples)
commands = []
for extension, codec in [('mp3', 'libmp3lame'), ('aac', 'aac'), ('m4a', 'aac')]:
    command = [encoder, '-nostdin', '-hide_banner', '-loglevel', 'error', '-y', '-i', str(source),
               '-map_metadata', '-1', '-fflags', '+bitexact', '-flags:a', '+bitexact', '-c:a', codec,
               '-b:a', '96k', str(args.output / ('tones.' + extension))]
    subprocess.run(command, check=True, timeout=30)
    local_paths = {str(source), str(args.output / ('tones.' + extension))}
    commands.append(['ffmpeg', *[Path(value).name if value in local_paths else value for value in command[1:]]])
records = []
for path in sorted(args.output.glob('tones.*')):
    records.append({'name': path.name, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'bytes': path.stat().st_size})
(args.output / 'manifest.json').write_text(json.dumps({
    'schemaVersion': 1, 'source': 'Generated one-second 48 kHz stereo sine tones (330 and 660 Hz), no human audio',
    'authoringTool': subprocess.check_output([encoder, '-version'], text=True).splitlines()[0],
    'recipe': 'scripts/generate-audio-fixtures.py', 'commands': commands, 'files': records,
    'scope': 'Codec and normalization behavior; does not establish speech recognition quality',
}, indent=2) + '\n')
