#!/usr/bin/env python3
"""Build the optional local Apple Vision helper with an integrity sidecar."""
import argparse
import hashlib
import pathlib
import subprocess

from devtools import cache_dir

ROOT = pathlib.Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--output', type=pathlib.Path, default=ROOT / 'build/native')
p.add_argument('--cache', type=pathlib.Path, default=cache_dir())
a = p.parse_args()
a.output.mkdir(parents=True, exist_ok=True)
out = a.output / 'ocr-worker'
subprocess.run(['swiftc', '-module-cache-path', str(cache_dir(a.cache) / 'swift-module-cache'), '-swift-version', '5', '-O', '-framework', 'Vision', '-framework', 'ImageIO', str(ROOT / 'native/ocr-worker.swift'), '-o', str(out)], check=True)
subprocess.run(['codesign', '--force', '--sign', '-', str(out)], check=True)
(a.output / 'ocr-worker.sha256').write_text(hashlib.sha256(out.read_bytes()).hexdigest() + '\n')
print(out)
