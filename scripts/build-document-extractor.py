#!/usr/bin/env python3
"""Build and hash the PDFKit helper used by packaged document extraction."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from devtools import cache_dir

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--output', type=Path, default=ROOT / 'build/native')
p.add_argument('--cache', type=Path, default=cache_dir())
args = p.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
out = args.output / 'document-extractor'
subprocess.run(['swiftc', '-module-cache-path', str(cache_dir(args.cache) / 'swift-module-cache'),
                '-swift-version', '5', '-O', '-target', 'arm64-apple-macos27.0',
                '-framework', 'PDFKit', str(ROOT / 'native/document-extractor.swift'), '-o', str(out)], check=True)
subprocess.run(['codesign', '--force', '--sign', '-', str(out)], check=True)
subprocess.run(['codesign', '--verify', '--strict', str(out)], check=True)
for line in subprocess.check_output(['otool', '-L', str(out)], text=True).splitlines()[1:]:
    library = line.strip().split(' ')[0]
    if not library.startswith(('/usr/lib/', '/System/Library/', '/Library/Apple/System/')):
        raise SystemExit('Unbundled document dependency: ' + library)
(args.output / 'document-extractor.json').write_text(json.dumps({
    'schemaVersion': 1, 'name': out.name, 'sha256': hashlib.sha256(out.read_bytes()).hexdigest(),
    'sourceSha256': hashlib.sha256((ROOT / 'native/document-extractor.swift').read_bytes()).hexdigest(),
    'runtime': 'PDFKit', 'architecture': 'arm64', 'minimumMacOS': '27.0',
}, indent=2) + '\n')
