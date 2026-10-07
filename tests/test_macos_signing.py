"""Independent signing boundary fixtures; never use a certificate or network."""
import copy
import json
import io
import runpy
from pathlib import Path
import plistlib
import shutil
import struct
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import macos_signing as signing

IDENTITY = 'A' * 40
TEAM = 'ABCDE12345'
SUBMISSION = 'a42f4d3d-2f72-4a69-b3a7-0a0fe26f721f'


class SigningTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.app = self.root / 'Trove.app'
        self.evidence = self.root / 'evidence'
        self.commands = []
        self.available_identity = True
        self.signature_team = TEAM
        self.timestamp = True
        self.notary_status = 'Accepted'
        self.response_id = SUBMISSION
        self.outer_signed = False
        self.corrupt_extraction = False
        self.session = signing.SigningSession(IDENTITY, TEAM, self.evidence, self.run_command)

    def run_command(self, args, timeout=90):
        args = [str(v) for v in args]
        self.commands.append(args)
        if args[:2] == ['/usr/bin/security', 'find-identity']:
            return f'  1) {IDENTITY} "Developer ID Application: Fixture ({TEAM})"\n1 valid identities found' if self.available_identity else '0 valid identities found'
        if args[:2] == ['/usr/bin/lipo', '-verify_arch']:
            return ''
        if args[:2] == ['/usr/bin/codesign', '--force']:
            self.assertNotIn('--deep', args)
            self.assertIn('--timestamp', args)
            self.assertIn('runtime', args)
            self.assertEqual(args[args.index('--sign') + 1], IDENTITY)
            target = Path(args[-1])
            if target == self.app:
                # The complete independent inventory must already match signed
                # bytes when the outer seal is created, not afterwards.
                self.assert_native_hashes()
                self.assertEqual(set(self.signed_helpers), set(self.helper_names + ['trove-mcp']))
                self.outer_signed = True
                target = self.app / 'Contents/MacOS/Trove'
            else:
                self.assertFalse(self.outer_signed)
                self.signed_helpers.append(target.name)
            target.write_bytes(target.read_bytes() + b' fixture signed bytes')
            return ''
        if args[:2] == ['/usr/bin/codesign', '--display']:
            return f'TeamIdentifier={self.signature_team}\nflags=0x10000(runtime)\n' + ('Timestamp=7 Oct 2026\n' if self.timestamp else '')
        if args[:2] == ['/usr/bin/codesign', '--verify']:
            self.assertTrue(any('certificate leaf[subject.OU] = "' + TEAM + '"' in v for v in args))
            return ''
        if args[:2] == ['/usr/bin/ditto', '-c']:
            with zipfile.ZipFile(args[-1], 'w') as archive:
                for path in self.app.rglob('*'):
                    if path.is_file():
                        archive.write(path, self.app.name + '/' + path.relative_to(self.app).as_posix())
            return ''
        if args[:2] == ['/usr/bin/ditto', '-x']:
            destination = Path(args[-1]) / self.app.name
            shutil.copytree(self.app, destination)
            if self.corrupt_extraction:
                (destination / 'Contents/MacOS/Trove').chmod(0o644)
            return ''
        if args[:3] == ['/usr/bin/xcrun', 'notarytool', 'submit']:
            self.assertTrue(self.outer_signed)
            self.assert_native_hashes()
            return json.dumps({'id': SUBMISSION})
        if args[:3] == ['/usr/bin/xcrun', 'notarytool', 'wait']:
            return json.dumps({'id': self.response_id, 'status': self.notary_status})
        if args[:3] == ['/usr/bin/xcrun', 'stapler', 'staple']:
            self.assertTrue((self.evidence / '04-accepted.json').is_file())
            (self.app / 'Contents/ticket-fixture').write_text('accepted ticket fixture')
            return ''
        if args[:3] == ['/usr/bin/xcrun', 'stapler', 'validate']:
            self.assertTrue((Path(args[-1]) / 'Contents/ticket-fixture').is_file())
            return ''
        if args[:2] == ['/usr/sbin/spctl', '--assess']:
            return ''
        if args[:2] == ['/usr/bin/xcrun', '--find']:
            return '/fixture/' + args[-1]
        self.fail('Unexpected command: ' + repr(args))

    def prepare(self):
        self.session.preflight(self.app)
        native = self.app / 'Contents/Resources/native'
        native.mkdir(parents=True)
        (self.app / 'Contents/MacOS').mkdir()
        self.helper_names = ['llama-worker', 'whisper-worker', 'microphone-worker', 'vision-worker', 'document-extractor', 'audio-decoder', 'pdf-renderer', 'ocr-worker']
        self.signed_helpers = []
        header = b'\xcf\xfa\xed\xfe' + struct.pack('<III', 0x100000c, 0, 2)
        for name in self.helper_names:
            path = native / name
            path.write_bytes(header + name.encode())
            path.chmod(0o755)
        for name in ['Trove', 'trove-mcp']:
            path = self.app / 'Contents/MacOS' / name
            path.write_bytes(header + name.encode())
            path.chmod(0o755)
        info = {'CFBundleIdentifier': signing.BUNDLE_ID, 'CFBundleExecutable': 'Trove', 'TroveArchitecture': 'arm64', 'LSMinimumSystemVersion': '27.0', 'CFBundleShortVersionString': '1.0.0', 'CFBundleVersion': '1'}
        (self.app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        self.manifest = {'schemaVersion': 2, 'platform': 'darwin', 'architecture': 'arm64', 'helpers': [
            {'name': name, 'sha256': signing.digest(native / name), 'architecture': 'arm64'} for name in self.helper_names[:4]]}
        (native / 'manifest.json').write_text(json.dumps(self.manifest))
        (native / 'microphone-worker.json').write_text(json.dumps({'schemaVersion': 1, 'platform': 'darwin', 'testOnly': False,
            'deviceBackendCompiled': True, 'capabilities': ['record'], 'helper': copy.deepcopy(self.manifest['helpers'][2])}))
        for name, runtime in [('document-extractor', 'PDFKit'), ('audio-decoder', 'FFmpeg'), ('pdf-renderer', 'WebKit/AppKit/PDFKit')]:
            (native / (name + '.json')).write_text(json.dumps({'schemaVersion': 1, 'name': name, 'architecture': 'arm64', 'runtime': runtime, 'sha256': signing.digest(native / name)}))
        (native / 'ocr-worker.sha256').write_text(signing.digest(native / 'ocr-worker') + '\n')
        return native

    def assert_native_hashes(self):
        native = self.app / 'Contents/Resources/native'
        for item in json.loads((native / 'manifest.json').read_text())['helpers']:
            self.assertEqual(item['sha256'], signing.digest(native / item['name']))
        mic = json.loads((native / 'microphone-worker.json').read_text())
        self.assertEqual(mic['helper']['sha256'], signing.digest(native / 'microphone-worker'))
        for name in ['document-extractor', 'audio-decoder', 'pdf-renderer']:
            self.assertEqual(json.loads((native / (name + '.json')).read_text())['sha256'], signing.digest(native / name))
        self.assertEqual((native / 'ocr-worker.sha256').read_text().strip(), signing.digest(native / 'ocr-worker'))

    def test_incomplete_production_flags_fail_before_build_or_account_commands(self):
        script = signing.ROOT / 'scripts/build-desktop.py'
        for flags in [['--notarize'], ['--team-id', TEAM], ['--signing-identity', IDENTITY, '--release-notices', str(self.root)]]:
            with self.subTest(flags=flags), patch.object(sys, 'argv', [str(script), *flags]), patch('subprocess.run') as run, patch('sys.stderr', new_callable=io.StringIO):
                with self.assertRaises(SystemExit) as error:
                    runpy.run_path(str(script), run_name='__main__')
                self.assertNotEqual(error.exception.code, 0)
                run.assert_not_called()

    def test_debug_entitlements_cannot_be_signed(self):
        self.prepare()
        original = plistlib.loads
        def changed_policy(data):
            result = original(data)
            if 'com.apple.security.device.audio-input' in result:
                result['com.apple.security.get-task-allow'] = True
            return result
        with patch.object(plistlib, 'loads', side_effect=changed_policy):
            with self.assertRaisesRegex(signing.SigningBlocked, 'entitlement policy'):
                self.session.sign(self.app)
        self.assertFalse(any('--force' in c for c in self.commands))

    def test_missing_or_wrong_identity_cannot_create_a_candidate(self):
        self.available_identity = False
        with self.assertRaisesRegex(signing.SigningBlocked, 'unavailable'):
            self.session.preflight(self.app)
        self.assertFalse(self.app.exists())
        self.assertFalse(self.evidence.exists())
        self.assertFalse(any('--force' in c for c in self.commands))
        with self.assertRaises(signing.SigningBlocked):
            signing.SigningSession('-', TEAM, self.evidence)

    def test_missing_notary_profile_precedes_identity_access_and_submission(self):
        with self.assertRaisesRegex(signing.SigningBlocked, 'Keychain profile'):
            self.session.preflight(self.app, notarize=True)
        self.assertEqual(self.commands, [])

    def test_modified_helper_fails_before_any_signing(self):
        native = self.prepare()
        (native / 'llama-worker').write_bytes(b'changed after manifest')
        before = signing.inventory(self.app)
        with self.assertRaisesRegex(signing.SigningBlocked, 'differ'):
            self.session.sign(self.app)
        self.assertFalse(any('--force' in c for c in self.commands))
        self.assertEqual(signing.inventory(self.app), before)

    def test_nested_signatures_rebind_every_digest_before_outer_seal(self):
        self.prepare()
        report = self.session.sign(self.app)
        self.assertTrue(self.outer_signed)
        self.assert_native_hashes()
        self.assertNotEqual(report['beforeSigning'], report['signedInventory'])
        self.assertFalse(report['releaseQualified'])
        self.assertFalse(any('notarytool' in c for c in self.commands))

    def test_unexpected_nested_code_and_links_are_rejected(self):
        native = self.prepare()
        extra = native / 'unexpected-plugin'
        shutil.copyfile(native / 'llama-worker', extra)
        with self.assertRaisesRegex(signing.SigningBlocked, 'Unexpected nested executable'):
            self.session.sign(self.app)
        extra.unlink()
        extra.symlink_to(native / 'llama-worker')
        with self.assertRaises(signing.SigningBlocked):
            self.session.sign(self.app)
        self.assertFalse(any('--force' in c for c in self.commands))

    def test_wrong_signed_team_or_missing_timestamp_blocks_outer_seal(self):
        self.prepare()
        for attribute, value in [('signature_team', 'WRONG12345'), ('timestamp', False)]:
            with self.subTest(attribute=attribute):
                original = getattr(self, attribute)
                setattr(self, attribute, value)
                with self.assertRaisesRegex(signing.SigningBlocked, 'secure timestamp'):
                    self.session.sign(self.app)
                setattr(self, attribute, original)
                # Restore only the fixture's helper bytes after its deliberately
                # failed nested signing; a real candidate requires a new build.
                path = self.app / 'Contents/Resources/native/audio-decoder'
                if path.read_bytes().endswith(b' fixture signed bytes'):
                    path.write_bytes(path.read_bytes()[:-len(b' fixture signed bytes')])
        self.assertFalse(self.outer_signed)
        self.assertFalse((self.evidence / '02-signed.json').exists())

    def test_only_accepted_exact_submission_can_staple_and_freeze(self):
        self.prepare()
        self.session.sign(self.app)
        self.response_id = 'e1000000-1111-4222-8333-123456789abc'
        with self.assertRaisesRegex(signing.SigningBlocked, 'identity or status'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertFalse(any(c[:3] == ['/usr/bin/xcrun', 'stapler', 'staple'] for c in self.commands))
        self.response_id = SUBMISSION
        report = signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertEqual(sum(c[:3] == ['/usr/bin/xcrun', 'notarytool', 'submit'] for c in self.commands), 1)
        self.assertEqual(report['notarization'], 'accepted_and_stapled')
        self.assertFalse(report['releaseQualified'])
        self.assertEqual(report['appInventory'], signing.inventory(self.app))
        with self.assertRaisesRegex(signing.SigningBlocked, 'already frozen'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)

    def test_pending_notarization_retains_submission_and_resumes_without_reupload(self):
        self.prepare()
        self.session.sign(self.app)
        self.notary_status = 'In Progress'
        with self.assertRaisesRegex(signing.SigningBlocked, 'In Progress'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertTrue((self.evidence / '03-submitted.json').exists())
        self.assertFalse((self.evidence / '06-frozen.json').exists())
        self.notary_status = 'Accepted'
        signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertEqual(sum(c[:3] == ['/usr/bin/xcrun', 'notarytool', 'submit'] for c in self.commands), 1)

    def test_extraction_that_drops_executable_mode_cannot_freeze(self):
        self.prepare()
        self.session.sign(self.app)
        self.corrupt_extraction = True
        with self.assertRaisesRegex(signing.SigningBlocked, 'executable modes'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertFalse((self.evidence / '06-frozen.json').exists())

    def test_unknown_upload_outcome_cannot_resubmit_or_overwrite_archive(self):
        self.prepare()
        self.session.sign(self.app)
        original = self.run_command
        def interrupted(args, timeout=90):
            if [str(v) for v in args[:3]] == ['/usr/bin/xcrun', 'notarytool', 'submit']:
                original(args, timeout)
                raise signing.SigningBlocked('interrupted upload fixture')
            return original(args, timeout)
        with self.assertRaisesRegex(signing.SigningBlocked, 'interrupted upload'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', interrupted)
        archive = self.evidence / 'notary-submission.zip'
        previous = archive.read_bytes()
        with self.assertRaisesRegex(signing.SigningBlocked, 'outcome is unknown'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertEqual(archive.read_bytes(), previous)
        self.assertEqual(sum(c[:3] == ['/usr/bin/xcrun', 'notarytool', 'submit'] for c in self.commands), 1)
        self.assertFalse((self.evidence / '06-frozen.json').exists())

    def test_rejected_notarization_never_staples_or_freezes(self):
        self.prepare()
        self.session.sign(self.app)
        self.notary_status = 'Invalid'
        with self.assertRaisesRegex(signing.SigningBlocked, 'Invalid'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertFalse(any(c[:3] == ['/usr/bin/xcrun', 'stapler', 'staple'] for c in self.commands))
        self.assertFalse((self.evidence / '06-frozen.json').exists())

    def test_changed_signed_candidate_is_never_uploaded(self):
        self.prepare()
        self.session.sign(self.app)
        (self.app / 'Contents/MacOS/Trove').write_bytes(b'changed')
        with self.assertRaisesRegex(signing.SigningBlocked, 'changed'):
            signing.notarize(self.app, self.evidence, 'fixture-profile', self.run_command)
        self.assertFalse(any('notarytool' in c for c in self.commands))


if __name__ == '__main__':
    unittest.main()
