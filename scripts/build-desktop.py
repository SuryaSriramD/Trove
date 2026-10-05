#!/usr/bin/env python3
"""Build the pinned frontend and a self-contained, ad-hoc-signed arm64 .app.

Developer ID signing/notarization is a separate release qualification step.
Native helpers are already signed before hashing; never re-sign them here.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys

from devtools import build_env, cache_dir, go_binary

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--cache', type=Path, default=cache_dir())
parser.add_argument('--native-bundle', type=Path, default=ROOT / 'build/native')
parser.add_argument('--skip-frontend', action='store_true')
parser.add_argument('--phase4', action='store_true', help='Build the P1 development preview and paired MCP relay')
parser.add_argument('--phase6', action='store_true', help='Build optional P2 packages, host and OCR helper')
parser.add_argument('--phase5', action='store_true', help='Build the P1 personal-use release candidate and paired MCP relay')
parser.add_argument('--output', type=Path, default=ROOT / 'build' / 'Trove.app')
parser.add_argument('--release-notices', type=Path, help='Directory with provenance.json and THIRD-PARTY-NOTICES.txt for distribution')
args = parser.parse_args()
if sum([args.phase4, args.phase5, args.phase6]) > 1:
    parser.error('Choose one release phase')
if (args.phase5 or args.phase6) and args.output.exists():
    parser.error('P1 candidates are immutable; choose an unused output path')
if args.phase5 and not args.release_notices:
    parser.error('P1 candidates require --release-notices from the provenance inventory')
args.cache = cache_dir(args.cache)
env = build_env(args.cache)
go = go_binary(args.cache)
env['CGO_ENABLED'] = '1'
env['GOOS'], env['GOARCH'] = 'darwin', 'arm64'
env['CGO_LDFLAGS'] = '-framework UniformTypeIdentifiers'
env['MACOSX_DEPLOYMENT_TARGET'] = '27.0'

def run(argv, cwd=ROOT):
    subprocess.run([str(v) for v in argv], cwd=cwd, env=env, check=True)

if not args.skip_frontend:
    # Invoke the installed pinned tools without a package-manager network step.
    # Install frontend/pnpm-lock.yaml first if these executables are absent.
    run([ROOT / 'frontend/node_modules/.bin/tsc', '--noEmit'], ROOT / 'frontend')
    run([ROOT / 'frontend/node_modules/.bin/vite', 'build'], ROOT / 'frontend')
native = args.native_bundle.resolve()
if not (native / 'manifest.json').exists():
    raise SystemExit('Build native helpers first: python scripts/build-native.py')
run([sys.executable, ROOT / 'scripts/build-document-extractor.py', '--cache', args.cache, '--output', native])
run([sys.executable, ROOT / 'scripts/build-audio-decoder.py', '--cache', args.cache, '--output', native])
run([sys.executable, ROOT / 'scripts/build-pdf-renderer.py', '--cache', args.cache, '--output', native])
if args.phase6:
    run([sys.executable, ROOT / 'scripts/build-vision.py', '--cache', args.cache, '--output', native])
    run([sys.executable, ROOT / 'scripts/build-ocr-worker.py', '--cache', args.cache, '--output', native])
shutil.copytree(ROOT / 'native/models', native / 'models', dirs_exist_ok=True)
contents = args.output / 'Contents'
(contents / 'MacOS').mkdir(parents=True, exist_ok=True)
(contents / 'Resources').mkdir(parents=True, exist_ok=True)
shutil.copytree(ROOT / 'browser-clipper', contents / 'Resources' / 'browser-clipper', dirs_exist_ok=True, ignore=shutil.ignore_patterns('._*', '.DS_Store'))
run([go, 'build', '-mod=readonly', '-trimpath', '-tags',
     'desktop production fts5 sqlite_omit_load_extension', '-o',
     contents / 'MacOS' / 'Trove', '.'])
if args.phase4 or args.phase5 or args.phase6:
    relay = contents / 'MacOS' / 'trove-mcp'
    run([go, 'build', '-mod=readonly', '-trimpath', '-o', relay, './cmd/trove-mcp'])
    run(['/usr/bin/codesign', '--force', '--sign', '-', relay])
if args.phase6:
    host = contents / 'MacOS' / 'trove-host'
    run([go, 'build', '-mod=readonly', '-trimpath', '-tags', 'fts5 sqlite_omit_load_extension', '-o', host, './cmd/trove-host'])
    run(['/usr/bin/codesign', '--force', '--sign', '-', host])
    shutil.copytree(ROOT / 'frontend/dist', contents / 'Resources' / 'web', dirs_exist_ok=True)
binary = contents / 'MacOS' / 'Trove'
run(['/usr/bin/lipo', '-verify_arch', 'arm64', binary])
dependencies = subprocess.check_output(['/usr/bin/otool', '-L', str(binary)], text=True)
for line in dependencies.splitlines()[1:]:
    dependency = line.strip().split(' ')[0]
    if not dependency.startswith(('/usr/lib/', '/System/Library/', '/Library/Apple/System/')):
        raise SystemExit('Unbundled desktop dependency: ' + dependency)
destination = contents / 'Resources' / 'native'
shutil.copytree(native, destination, dirs_exist_ok=True,
                ignore=shutil.ignore_patterns('._*', '.DS_Store'))
if args.release_notices:
    notices = contents / 'Resources' / 'release'
    notices.mkdir(parents=True, exist_ok=True)
    for name in ['provenance.json', 'THIRD-PARTY-NOTICES.txt']:
        shutil.copyfile(args.release_notices / name, notices / name)
    shutil.copytree(args.release_notices / 'notices', notices / 'notices', dirs_exist_ok=True,
                    ignore=shutil.ignore_patterns('._*', '.DS_Store'))
info = {
    'CFBundleName': 'Trove', 'CFBundleDisplayName': 'Trove',
    'CFBundleExecutable': 'Trove', 'CFBundleIdentifier': 'local.trove.desktop',
    'CFBundlePackageType': 'APPL', 'CFBundleShortVersionString': '0.6.0' if args.phase6 else '0.5.0' if args.phase5 else ('0.4.0' if args.phase4 else '0.3.0'),
    'CFBundleVersion': '6' if args.phase6 else '5' if args.phase5 else ('4' if args.phase4 else '3'), 'LSMinimumSystemVersion': '27.0',
    'TroveReleaseChannel': 'P2 development preview' if args.phase6 else 'P1 personal-use candidate' if args.phase5 else ('P1 development preview' if args.phase4 else 'P0 release candidate'),
    'NSHighResolutionCapable': True,
    'NSMicrophoneUsageDescription': 'Trove records meetings locally when you start a recording.',
    'CFBundleURLTypes': [{'CFBundleURLName': 'local.trove.workspace', 'CFBundleURLSchemes': ['trove']}],
    'NSHumanReadableCopyright': 'Trove contributors',
}
with (contents / 'Info.plist').open('wb') as f:
    plistlib.dump(info, f)
(contents / 'PkgInfo').write_bytes(b'APPL????')
# Source volume metadata must not become extra resources in the signed bundle.
for sidecar in args.output.rglob('._*'):
    sidecar.unlink()
run(['/usr/bin/xattr', '-cr', args.output])
run(['/usr/bin/codesign', '--force', '--sign', '-',
     '--entitlements', ROOT / 'packaging' / 'desktop.entitlements.plist', args.output])
run(['/usr/bin/codesign', '--verify', '--deep', '--strict', '--verbose=2', args.output])
manifest = json.loads((native / 'manifest.json').read_text())
for source in native.rglob('*'):
    if source.is_file() and not source.name.startswith('._'):
        copied = destination / source.relative_to(native)
        if hashlib.sha256(source.read_bytes()).digest() != hashlib.sha256(copied.read_bytes()).digest():
            raise SystemExit(f'Native resource changed during packaging: {source}')
print(args.output)
