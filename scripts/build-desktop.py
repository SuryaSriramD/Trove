#!/usr/bin/env python3
"""Build the pinned frontend and a self-contained arm64 .app.

Default signing is ad-hoc. Explicit Developer ID signing rebinds helper hashes
before sealing the app; notarization and product qualification remain separate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys

from devtools import build_env, cache_dir, go_binary
from microphone_bundle import install_microphone_record
from macos_signing import SigningBlocked, SigningSession, notarize

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
parser.add_argument('--version', help='Signed three-component release version; phase default if omitted')
parser.add_argument('--build-number', type=int, help='Signed monotonically increasing build number; phase default if omitted')
parser.add_argument('--signing-identity', help='Exact 40-hex Developer ID Application certificate fingerprint; absent keeps ad-hoc development signing')
parser.add_argument('--team-id', help='Expected ten-character Apple Developer Team ID')
parser.add_argument('--signing-evidence', type=Path, help='New external evidence directory for signing and notarization records')
parser.add_argument('--notarize', action='store_true', help='Explicitly submit the signed candidate to Apple, staple acceptance and freeze the distribution archive')
parser.add_argument('--notary-profile', help='Configured notarytool Keychain profile name; credentials are never accepted here')
args = parser.parse_args()
if args.version is not None and not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', args.version):
    parser.error('--version must contain three numeric components')
if args.build_number is not None and not 0 < args.build_number < 2**64:
    parser.error('--build-number must be a positive 64-bit integer')
if sum([args.phase4, args.phase5, args.phase6]) > 1:
    parser.error('Choose one release phase')
if (args.phase5 or args.phase6) and args.output.exists():
    parser.error('P1 candidates are immutable; choose an unused output path')
if (args.phase5 or args.signing_identity) and not args.release_notices:
    parser.error('Production-signed and P1 candidates require --release-notices from the provenance inventory')
if not args.signing_identity and any([args.team_id, args.signing_evidence, args.notarize, args.notary_profile]):
    parser.error('Production signing/notarization options require --signing-identity; ad-hoc is never a fallback')
if args.signing_identity and (not args.team_id or not args.signing_evidence or not args.version or not args.build_number):
    parser.error('Production signing requires --team-id, --signing-evidence, --version and --build-number')
if args.notary_profile and not args.notarize:
    parser.error('--notary-profile requires explicit --notarize; otherwise use notarize-macos.py later')
signing = None
if args.signing_identity:
    try:
        signing = SigningSession(args.signing_identity, args.team_id, args.signing_evidence)
        signing.preflight(args.output, args.notary_profile, args.notarize)
    except SigningBlocked as error:
        parser.exit(1, 'BLOCKED: ' + str(error) + '\n')
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
# New candidates always contain the pinned miniaudio production helper. A
# caller-provided legacy Swift bundle is rebuilt and rebound explicitly.
run([sys.executable, ROOT / 'scripts/build-microphone.py', '--cache', args.cache, '--output', native])
install_microphone_record(native, 'darwin-arm64')
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
compatibility = json.loads(subprocess.check_output(
    [str(go), 'run', '-mod=readonly', '-tags', 'fts5 sqlite_omit_load_extension', './cmd/trove-format'],
    cwd=ROOT, env=env, text=True))
if set(compatibility) != {'userVersion', 'storageGeneration'} or any(type(value) is not int or value < 1 for value in compatibility.values()):
    raise SystemExit('Workspace compatibility metadata is invalid')
phase = 6 if args.phase6 else 5 if args.phase5 else 4 if args.phase4 else 3
info = {
    'CFBundleName': 'Trove', 'CFBundleDisplayName': 'Trove',
    'CFBundleExecutable': 'Trove', 'CFBundleIdentifier': 'local.trove.desktop',
    'CFBundlePackageType': 'APPL', 'CFBundleShortVersionString': args.version or f'0.{phase}.0',
    'CFBundleVersion': str(args.build_number or phase), 'LSMinimumSystemVersion': '27.0',
    'TroveWorkspaceUserVersion': compatibility['userVersion'],
    'TroveRollbackHandshakeVersion': 1,
    'TroveWorkspaceStorageGeneration': compatibility['storageGeneration'],
    'TroveArchitecture': env['GOARCH'],
    'TroveReleaseChannel': 'Developer ID signed candidate' if signing else 'P2 development preview' if args.phase6 else 'P1 personal-use candidate' if args.phase5 else ('P1 development preview' if args.phase4 else 'P0 release candidate'),
    'NSHighResolutionCapable': True,
    'NSMicrophoneUsageDescription': 'Trove records meetings locally when you start a recording.',
    'CFBundleURLTypes': [{'CFBundleURLName': 'local.trove.workspace', 'CFBundleURLSchemes': ['trove']}],
    'NSHumanReadableCopyright': 'Trove contributors',
}
with (contents / 'Info.plist').open('wb') as f:
    plistlib.dump(info, f)
(contents / 'PkgInfo').write_bytes(b'APPL????')
reminder_destination = contents / 'Library/LoginItems/TroveReminders.app'
if reminder_destination.exists() or reminder_destination.is_symlink():
    # Ad-hoc development outputs may be rebuilt; candidate preflight above
    # requires a new destination. Refuse an unrelated or linked nested payload.
    if reminder_destination.is_symlink() or not reminder_destination.is_dir():
        raise SystemExit('Invalid existing reminder build destination')
    previous_info = plistlib.loads((reminder_destination / 'Contents/Info.plist').read_bytes())
    if previous_info.get('CFBundleIdentifier') != 'local.trove.desktop.reminders':
        raise SystemExit('Refusing to replace an unrelated nested helper')
    shutil.rmtree(reminder_destination)
run([sys.executable, ROOT / 'scripts/build-reminder-helper.py', '--cache', args.cache,
     '--output', reminder_destination,
     '--version', info['CFBundleShortVersionString'], '--build-number', info['CFBundleVersion']])
# Source volume metadata must not become extra resources in the signed bundle.
for sidecar in args.output.rglob('._*'):
    sidecar.unlink()
run(['/usr/bin/xattr', '-cr', args.output])
manifest = json.loads((native / 'manifest.json').read_text())
for source in native.rglob('*'):
    if source.is_file() and not source.name.startswith('._'):
        copied = destination / source.relative_to(native)
        if hashlib.sha256(source.read_bytes()).digest() != hashlib.sha256(copied.read_bytes()).digest():
            raise SystemExit(f'Native resource changed during packaging: {source}')
if signing:
    try:
        signing.sign(args.output)
        if args.notarize:
            notarize(args.output, args.signing_evidence, args.notary_profile)
    except SigningBlocked as error:
        parser.exit(1, 'BLOCKED: ' + str(error) + '\n')
else:
    run(['/usr/bin/codesign', '--force', '--sign', '-',
         '--entitlements', ROOT / 'packaging' / 'desktop.entitlements.plist', args.output])
    run(['/usr/bin/codesign', '--verify', '--deep', '--strict', '--verbose=2', args.output])
print(args.output)
