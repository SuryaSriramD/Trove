#!/usr/bin/env python3
"""Build and ad-hoc seal the reminder-only macOS login item; never register it."""
import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import re
import subprocess
from devtools import cache_dir

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--cache', type=Path, default=cache_dir())
p.add_argument('--output', type=Path, required=True, help='New TroveReminders.app bundle destination')
p.add_argument('--version', default='0.1.0')
p.add_argument('--build-number', default='1')
a = p.parse_args()
if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', a.version) or not re.fullmatch(r'[1-9][0-9]{0,19}', a.build_number):
    raise SystemExit('Invalid helper bundle version')
if a.output.exists() or a.output.is_symlink():
    raise SystemExit('Reminder helper destination must be new')
contents = a.output / 'Contents'
(contents / 'MacOS').mkdir(parents=True)
(contents / 'Resources').mkdir()
sources = [ROOT / 'native/reminder-schedule.swift', ROOT / 'native/reminder-agent.swift']
binary = contents / 'MacOS/TroveReminders'
subprocess.run(['swiftc', '-module-cache-path', str(a.cache / 'swift-module-cache'), '-swift-version', '5', '-O', '-target', 'arm64-apple-macos27.0', '-framework', 'AppKit', '-framework', 'UserNotifications', *map(str, sources), '-o', str(binary)], check=True)
with (contents / 'Info.plist').open('wb') as target:
    plistlib.dump({'CFBundleIdentifier':'local.trove.desktop.reminders','CFBundleExecutable':'TroveReminders','CFBundleName':'Trove Reminders','CFBundleDisplayName':'Trove Reminders','CFBundlePackageType':'APPL','CFBundleShortVersionString':a.version,'CFBundleVersion':a.build_number,'LSMinimumSystemVersion':'27.0','LSUIElement':True,'NSHighResolutionCapable':True}, target)
(contents / 'Resources/source.json').write_text(json.dumps({'version':1,'scope':'Derived reminder schedules and delivery receipts only; no workspace database access','sources':{source.name:hashlib.sha256(source.read_bytes()).hexdigest() for source in sources}}, indent=2) + '\n')
for line in subprocess.check_output(['otool','-L',str(binary)],text=True).splitlines()[1:]:
    dependency=line.strip().split(' ')[0]
    if not dependency.startswith(('/usr/lib/','/System/Library/','/Library/Apple/System/')):
        raise SystemExit('Unbundled reminder helper dependency: ' + dependency)
subprocess.run(['codesign','--force','--sign','-',str(a.output)],check=True)
subprocess.run(['codesign','--verify','--strict',str(a.output)],check=True)
print(a.output)
