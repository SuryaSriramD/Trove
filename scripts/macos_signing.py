"""Explicit Developer ID signing and resumable notarization of a new Mac bundle.

Never reads/exports credentials: notarytool owns its named Keychain profile.
Only a frozen, explicitly submitted candidate can obtain distribution evidence.
"""
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import plistlib
import re
import stat
import struct
import subprocess
import tempfile
import zipfile
import uuid

BUNDLE_ID = 'local.trove.desktop'
REMINDER_ID = BUNDLE_ID + '.reminders'
ROOT = Path(__file__).resolve().parents[1]


class SigningBlocked(RuntimeError):
    pass


def command(argv, timeout=90):
    """Do not echo vendor output, environment values or account/profile metadata."""
    try:
        result = subprocess.run([str(v) for v in argv], capture_output=True, text=True,
                                stdin=subprocess.DEVNULL, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise SigningBlocked('Signing/notarization command unavailable or timed out; retained evidence must be inspected before retry.') from error
    if result.returncode:
        raise SigningBlocked('Signing/notarization command failed. Check the selected Developer ID, unlocked signing key and configured notarytool Keychain profile; no bypass was used.')
    if len(result.stdout) + len(result.stderr) > 1 << 20:
        raise SigningBlocked('Signing/notarization command returned oversized output.')
    return result.stdout if result.stdout.strip() else result.stderr


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(1 << 20), b''):
            value.update(chunk)
    return value.hexdigest()


def plain_file(path):
    if path.is_symlink() or not path.is_file():
        raise SigningBlocked('Candidate contains a missing, linked or non-regular required file: ' + path.name)
    return path


def read_json(path):
    plain_file(path)
    if path.stat().st_size > 1 << 20:
        raise SigningBlocked('Candidate metadata exceeds its bound: ' + path.name)
    try:
        return json.loads(path.read_text())
    except (ValueError, UnicodeError) as error:
        raise SigningBlocked('Candidate metadata is invalid: ' + path.name) from error


def inventory(app):
    if app.is_symlink() or not app.is_dir():
        raise SigningBlocked('Select a regular application directory.')
    result = {}
    total = 0
    for path in sorted(app.rglob('*')):
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            continue
        plain_file(path)
        total += info.st_size
        if len(result) >= 10000 or total > 4 << 30:
            raise SigningBlocked('Candidate inventory exceeds the fixed release bounds.')
        result[path.relative_to(app).as_posix()] = {'sha256': digest(path), 'sizeBytes': info.st_size,
                                                  'mode': stat.S_IMODE(info.st_mode)}
    return result


def write_record(path, value):
    with path.open('x') as output:
        json.dump(value, output, indent=2)
        output.write('\n')


def validate_profile(profile):
    if not isinstance(profile, str) or not re.fullmatch(r'[A-Za-z0-9_.-]{1,128}', profile):
        raise SigningBlocked('Notarization needs an explicit configured notarytool Keychain profile name (letters, digits, dot, underscore or hyphen). No password/token argument is accepted.')


def requirement(team, identifier):
    return ('anchor apple generic and identifier "' + identifier + '" and '
            'certificate 1[field.1.2.840.113635.100.6.2.6] exists and '
            'certificate leaf[field.1.2.840.113635.100.6.1.13] exists and '
            'certificate leaf[subject.OU] = "' + team + '"')


def verify_code(path, team, identifier, run, deep=False):
    run(['/usr/bin/codesign', '--verify', '--strict', *(['--deep'] if deep else []),
         '-R=' + requirement(team, identifier), path])
    details = run(['/usr/bin/codesign', '--display', '--verbose=4', path])
    if (not re.search(r'^TeamIdentifier=' + re.escape(team) + r'$', details, re.M)
            or not re.search(r'^Timestamp=\S.+$', details, re.M)
            or not re.search(r'flags=0x[0-9a-fA-F]+\([^\n]*\bruntime\b', details)):
        raise SigningBlocked('Developer ID verification requires the selected team, secure timestamp and hardened runtime.')


def verify_entitlements():
    expected = {'com.apple.security.device.audio-input': True}
    for name in ['desktop.entitlements.plist', 'microphone.entitlements.plist']:
        try:
            actual = plistlib.loads(plain_file(ROOT / 'packaging' / name).read_bytes())
        except (ValueError, plistlib.InvalidFileException) as error:
            raise SigningBlocked('Production entitlements are invalid.') from error
        if actual != expected:
            raise SigningBlocked('Production signing requires the reviewed audio-input-only entitlement policy; debug or runtime exceptions need a separate review.')


def native_bindings(app):
    """Validate every old helper digest before any signature rewrites its bytes."""
    native = app / 'Contents/Resources/native'
    manifest_path = native / 'manifest.json'
    manifest = read_json(manifest_path)
    if not isinstance(manifest, dict) or manifest.get('schemaVersion') != 2 or manifest.get('platform') != 'darwin' or manifest.get('architecture') != 'arm64':
        raise SigningBlocked('A production darwin/arm64 native manifest is required.')
    records = manifest.get('helpers')
    if not isinstance(records, list) or any(not isinstance(r, dict) for r in records):
        raise SigningBlocked('Native helper inventory is invalid.')
    names = [r.get('name') for r in records]
    if any(not isinstance(name, str) for name in names):
        raise SigningBlocked('Native helper names must be strings.')
    expected = {'llama-worker', 'whisper-worker', 'microphone-worker'}
    if len(set(names)) != len(names) or not expected <= set(names) or set(names) - expected - {'vision-worker'}:
        raise SigningBlocked('Unexpected, duplicate or missing native helper identity.')
    documents = {manifest_path: manifest}
    bindings = {r['name']: [r] for r in records}
    mic_path = native / 'microphone-worker.json'
    mic = read_json(mic_path)
    if (not isinstance(mic, dict) or mic.get('schemaVersion') != 1 or mic.get('platform') != 'darwin' or mic.get('testOnly') is not False
            or mic.get('deviceBackendCompiled') is not True or mic.get('capabilities') != ['record']
            or not isinstance(mic.get('helper'), dict) or mic['helper'].get('name') != 'microphone-worker'
            or (native / 'microphone-fixture.json').exists()):
        raise SigningBlocked('The microphone must be a production helper, never a synthetic fixture.')
    bindings['microphone-worker'].append(mic['helper'])
    documents[mic_path] = mic
    for name, runtime in [('document-extractor', 'PDFKit'), ('audio-decoder', 'FFmpeg'), ('pdf-renderer', 'WebKit/AppKit/PDFKit')]:
        path = native / (name + '.json')
        record = read_json(path)
        if not isinstance(record, dict) or record.get('schemaVersion') != 1 or record.get('name') != name or record.get('architecture') != 'arm64' or record.get('runtime') != runtime:
            raise SigningBlocked('Unexpected native sidecar identity: ' + name)
        documents[path] = record
        bindings[name] = [record]
    ocr = native / 'ocr-worker'
    ocr_hash = native / 'ocr-worker.sha256'
    if ocr.exists() or ocr_hash.exists() or ocr.is_symlink() or ocr_hash.is_symlink():
        if plain_file(ocr_hash).read_text().strip() != digest(plain_file(ocr)):
            raise SigningBlocked('OCR bytes differ from their integrity sidecar.')
        bindings['ocr-worker'] = []
    for name, refs in bindings.items():
        binary = plain_file(native / name)
        actual = digest(binary)
        if binary.stat().st_size > 128 << 20 or not binary.stat().st_mode & 0o111:
            raise SigningBlocked('Invalid helper size or executable mode: ' + name)
        if any(ref.get('sha256') != actual or ref.get('architecture') != 'arm64' for ref in refs):
            raise SigningBlocked('Native bytes differ from their manifest: ' + name)
    return native, documents, bindings


def reminder_bundle(app):
    bundle = app / 'Contents/Library/LoginItems/TroveReminders.app'
    if not bundle.exists() and not bundle.is_symlink():
        return None  # Historical candidates predate the reminder feature.
    if bundle.is_symlink() or not bundle.is_dir():
        raise SigningBlocked('Reminder login item must be a regular nested bundle.')
    info = plistlib.loads(plain_file(bundle / 'Contents/Info.plist').read_bytes())
    if (info.get('CFBundleIdentifier') != REMINDER_ID or info.get('CFBundleExecutable') != 'TroveReminders'
            or info.get('LSMinimumSystemVersion') != '27.0' or info.get('LSUIElement') is not True):
        raise SigningBlocked('Reminder login item identity or lifecycle policy is invalid.')
    executable = plain_file(bundle / 'Contents/MacOS/TroveReminders')
    if not executable.stat().st_mode & 0o111:
        raise SigningBlocked('Reminder login item is not executable.')
    return bundle


def sign_reminder_bundle(app, identity, team, run):
    bundle = reminder_bundle(app)
    if bundle is not None:
        run(['/usr/bin/codesign', '--force', '--sign', identity, '--timestamp', '--options', 'runtime', '--identifier', REMINDER_ID, bundle])
        verify_code(bundle, team, REMINDER_ID, run, deep=True)


def executable_items(app, bindings):
    allowed = {app / 'Contents/Resources/native' / name for name in bindings}
    allowed.add(plain_file(app / 'Contents/MacOS/Trove'))
    reminder = reminder_bundle(app)
    if reminder is not None:
        allowed.add(reminder / 'Contents/MacOS/TroveReminders')
    for name in ['trove-mcp', 'trove-host']:
        path = app / 'Contents/MacOS' / name
        if path.exists() or path.is_symlink():
            allowed.add(plain_file(path))
    # Relinking objects/archives are data, not executable code. Refuse unknown
    # Mach-O executables, libraries, bundles or fat code rather than deep-signing.
    for relative in inventory(app):
        path = app / relative
        with path.open('rb') as source:
            header = source.read(16)
        magic = header[:4]
        mach_code = magic in [b'\xca\xfe\xba\xbe', b'\xbe\xba\xfe\xca', b'\xca\xfe\xba\xbf', b'\xbf\xba\xfe\xca']
        if len(header) == 16 and magic in [b'\xce\xfa\xed\xfe', b'\xcf\xfa\xed\xfe', b'\xfe\xed\xfa\xce', b'\xfe\xed\xfa\xcf']:
            byte_order = '<' if magic[0] in (0xce, 0xcf) else '>'
            mach_code = struct.unpack(byte_order + 'I', header[12:16])[0] in (2, 6, 8)
        if mach_code and path not in allowed:
            raise SigningBlocked('Unexpected nested executable requires explicit signing policy: ' + relative)
    return sorted(allowed - {app / 'Contents/MacOS/Trove'})


class SigningSession:
    def __init__(self, identity, team, evidence, run=command):
        if not re.fullmatch(r'[0-9A-Fa-f]{40}', identity or '') or not re.fullmatch(r'[A-Z0-9]{10}', team or ''):
            raise SigningBlocked('Production signing needs an explicit 40-hex Developer ID Application identity fingerprint and ten-character Team ID.')
        self.identity, self.team, self.evidence, self.run = identity.upper(), team, Path(evidence), run

    def preflight(self, app, notary_profile=None, notarize=False):
        app = Path(app).absolute()
        evidence = self.evidence.absolute()
        if evidence == app or app in evidence.parents:
            raise SigningBlocked('Signing evidence must be outside the application bundle.')
        if os.path.lexists(app) or os.path.lexists(evidence):
            raise SigningBlocked('Production app and signing-evidence destinations must both be new.')
        if notarize:
            validate_profile(notary_profile)
        identities = self.run(['/usr/bin/security', 'find-identity', '-v', '-p', 'codesigning'])
        matches = re.findall(r'\b' + self.identity + r'\s+"Developer ID Application: [^"\n]+ \(' + self.team + r'\)"', identities, re.I)
        if len(matches) != 1:
            raise SigningBlocked('Production signing is blocked: selected valid Developer ID Application identity/team is unavailable. Configure it through Apple/Xcode; ad-hoc signing is not a fallback.')
        if notarize:
            self.run(['/usr/bin/xcrun', '--find', 'notarytool'])
            self.run(['/usr/bin/xcrun', '--find', 'stapler'])
        self.evidence.mkdir(parents=True, mode=0o700, exist_ok=False)
        write_record(self.evidence / '01-preflight.json', {'schemaVersion': 1, 'identityFingerprint': self.identity,
                     'teamID': self.team, 'notarizationRequested': notarize, 'releaseQualified': False})

    def sign(self, app):
        app = Path(app).absolute()
        verify_entitlements()
        info = plistlib.loads(plain_file(app / 'Contents/Info.plist').read_bytes())
        if (not isinstance(info, dict) or info.get('CFBundleIdentifier') != BUNDLE_ID
                or info.get('CFBundleExecutable') != 'Trove' or info.get('TroveArchitecture') != 'arm64'
                or info.get('LSMinimumSystemVersion') != '27.0'
                or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', str(info.get('CFBundleShortVersionString', '')))
                or not re.fullmatch(r'[1-9][0-9]{0,19}', str(info.get('CFBundleVersion', '')))
                or int(info['CFBundleVersion']) >= 2**64):
            raise SigningBlocked('Unexpected application identity, architecture, minimum OS or release version/build.')
        native, documents, bindings = native_bindings(app)
        nested = executable_items(app, bindings)
        before = inventory(app)
        for path in nested + [app / 'Contents/MacOS/Trove']:
            self.run(['/usr/bin/lipo', '-verify_arch', 'arm64', path])
        for path in nested:
            identifier = REMINDER_ID if path.name == 'TroveReminders' else BUNDLE_ID + '.' + path.name
            args = ['/usr/bin/codesign', '--force', '--sign', self.identity, '--timestamp', '--options', 'runtime', '--identifier', identifier]
            if path.name == 'microphone-worker':
                args += ['--entitlements', ROOT / 'packaging/microphone.entitlements.plist']
            self.run(args + [path])
            verify_code(path, self.team, identifier, self.run)
        sign_reminder_bundle(app, self.identity, self.team, self.run)
        # Commit helper digests only after every nested code signature verified.
        for name, refs in bindings.items():
            for ref in refs:
                ref['sha256'] = digest(native / name)
        for path, body in documents.items():
            path.write_text(json.dumps(body, indent=2) + '\n')
        if 'ocr-worker' in bindings:
            (native / 'ocr-worker.sha256').write_text(digest(native / 'ocr-worker') + '\n')
        self.run(['/usr/bin/codesign', '--force', '--sign', self.identity, '--timestamp', '--options', 'runtime',
                  '--entitlements', ROOT / 'packaging/desktop.entitlements.plist', app])
        verify_code(app, self.team, BUNDLE_ID, self.run, deep=True)
        native_bindings(app)
        signed = {'schemaVersion': 1, 'appPath': str(app), 'identityFingerprint': self.identity, 'teamID': self.team,
                  'recordedAt': datetime.now(timezone.utc).isoformat(), 'beforeSigning': before, 'signedInventory': inventory(app),
                  'notarization': 'not_yet_submitted', 'releaseQualified': False}
        write_record(self.evidence / '02-signed.json', signed)
        return signed


def verify_distribution_archive(archive, app, expected, team, run):
    try:
        with zipfile.ZipFile(archive) as zipped:
            entries = zipped.infolist()
            if len(entries) > 20000 or sum(item.file_size for item in entries) > 5 << 30:
                raise SigningBlocked('Distribution archive exceeds the fixed extraction bounds.')
            for item in entries:
                name = PurePosixPath(item.filename)
                if (name.is_absolute() or '..' in name.parts or '\\' in item.filename or not name.parts
                        or name.parts[0] not in (app.name, '__MACOSX')
                        or stat.S_ISLNK(item.external_attr >> 16)):
                    raise SigningBlocked('Distribution archive contains an unsafe entry.')
        with tempfile.TemporaryDirectory(prefix='trove-signed-archive-') as temporary:
            run(['/usr/bin/ditto', '-x', '-k', archive, temporary])
            extracted = Path(temporary) / app.name
            if inventory(extracted) != expected:
                raise SigningBlocked('Extracted distribution bytes or executable modes differ from the frozen app.')
            verify_code(extracted, team, BUNDLE_ID, run, deep=True)
            run(['/usr/bin/xcrun', 'stapler', 'validate', extracted], timeout=180)
    except (OSError, zipfile.BadZipFile) as error:
        raise SigningBlocked('Distribution archive could not be independently inspected.') from error


def notarize(app, evidence, profile, run=command):
    """Submit once, or resume an existing submission without signing/re-uploading."""
    validate_profile(profile)
    app, evidence = Path(app).absolute(), Path(evidence)
    signed = read_json(evidence / '02-signed.json')
    if not isinstance(signed, dict):
        raise SigningBlocked('Signing evidence must be a metadata object.')
    team = signed.get('teamID')
    if signed.get('appPath') != str(app) or not re.fullmatch(r'[A-Z0-9]{10}', team or ''):
        raise SigningBlocked('Signing evidence is not bound to this application/team.')
    if (evidence / '06-frozen.json').exists():
        raise SigningBlocked('Candidate is already frozen; do not mutate or resubmit it.')
    expected = signed['signedInventory']
    stapled = evidence / '05-stapled.json'
    if stapled.exists():
        expected = read_json(stapled)['inventory']
    if inventory(app) != expected:
        raise SigningBlocked('Application bytes/modes changed since the recorded signing/stapling stage.')
    verify_code(app, team, BUNDLE_ID, run, deep=True)
    native_bindings(app)
    submission_path = evidence / '03-submitted.json'
    archive = evidence / 'notary-submission.zip'
    if submission_path.exists():
        submission = read_json(submission_path)
        if digest(plain_file(archive)) != submission.get('archiveSha256'):
            raise SigningBlocked('Notary submission archive changed.')
    else:
        # An archive without a receipt means a prior upload may have succeeded.
        # Never automatically submit again after that unknown outcome.
        if os.path.lexists(archive):
            raise SigningBlocked('Submission outcome is unknown; recover its Apple submission ID before retry. Automatic re-upload is refused.')
        run(['/usr/bin/ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', app, archive])
        output = run(['/usr/bin/xcrun', 'notarytool', 'submit', archive, '--keychain-profile', profile,
                      '--no-wait', '--output-format', 'json'], timeout=1200)
        try:
            body = json.loads(output)
            identity = str(uuid.UUID(body['id']))
        except (ValueError, KeyError, TypeError) as error:
            raise SigningBlocked('Notary submission outcome is unknown; no automatic resubmission is allowed.') from error
        submission = {'submissionID': identity, 'archiveSha256': digest(plain_file(archive))}
        write_record(submission_path, submission)
    identity = submission.get('submissionID')
    try:
        if str(uuid.UUID(identity)) != identity:
            raise ValueError()
    except (ValueError, TypeError, AttributeError) as error:
        raise SigningBlocked('Notary submission identity is invalid.') from error
    accepted_path = evidence / '04-accepted.json'
    if not accepted_path.exists():
        output = run(['/usr/bin/xcrun', 'notarytool', 'wait', identity, '--keychain-profile', profile,
                      '--timeout', '20m', '--output-format', 'json'], timeout=1230)
        try:
            body = json.loads(output)
        except ValueError as error:
            raise SigningBlocked('Notary wait returned invalid output; resume using the retained submission.') from error
        status = body.get('status') if isinstance(body, dict) else None
        if not isinstance(body, dict) or body.get('id') != identity or status not in ('Accepted', 'Invalid', 'In Progress', 'Rejected'):
            raise SigningBlocked('Notary response identity or status is invalid; no ticket was stapled.')
        outcome = {'submissionID': identity, 'status': status}
        write_record(evidence / ('notary-wait-' + uuid.uuid4().hex + '.json'), outcome)
        if status != 'Accepted':
            raise SigningBlocked('Notarization is ' + status + '; release remains blocked. Resume a pending submission instead of uploading again.')
        write_record(accepted_path, outcome)
    elif read_json(accepted_path) != {'submissionID': identity, 'status': 'Accepted'}:
        raise SigningBlocked('Accepted notarization evidence differs from the selected submission.')
    if not stapled.exists():
        run(['/usr/bin/xcrun', 'stapler', 'staple', app], timeout=180)
        write_record(stapled, {'inventory': inventory(app), 'submissionID': identity})
    run(['/usr/bin/xcrun', 'stapler', 'validate', app], timeout=180)
    verify_code(app, team, BUNDLE_ID, run, deep=True)
    run(['/usr/sbin/spctl', '--assess', '--type', 'execute', app], timeout=180)
    native_bindings(app)
    final_inventory = inventory(app)
    distribution = evidence / 'Trove-notarized.zip'
    if os.path.lexists(distribution):
        raise SigningBlocked('Distribution archive already exists without a final freeze; preserve and inspect it.')
    run(['/usr/bin/ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', app, distribution])
    if inventory(app) != final_inventory:
        raise SigningBlocked('Application changed while creating its distribution archive.')
    verify_distribution_archive(distribution, app, final_inventory, team, run)
    report = {'schemaVersion': 1, 'appPath': str(app), 'teamID': team, 'submissionID': identity,
              'notarization': 'accepted_and_stapled', 'appInventory': final_inventory, 'extractedArchiveVerified': True,
              'archiveSha256': digest(plain_file(distribution)), 'archivePath': distribution.name,
              'recordedAt': datetime.now(timezone.utc).isoformat(), 'releaseQualified': False,
              'limits': ['Distribution integrity only; product/device/independent-install acceptance remains separate.',
                         'Archive retains resource forks/extended metadata; byte inventory describes regular bundle files.']}
    write_record(evidence / '06-frozen.json', report)
    return report
