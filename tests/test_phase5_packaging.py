import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('phase5_packaging', ROOT / 'scripts/package-phase5.py')
packaging = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packaging)


class P1CandidatePackagingTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.run = self.root / 'evidence/phase5/runs/fixture'
        self.run.mkdir(parents=True)
        self.path = self.run / 'verification.json'
        self.app = self.root / 'build/Trove.app'
        for relative in ('Contents/MacOS/Trove', 'Contents/MacOS/trove-mcp', 'Contents/Info.plist'):
            path = self.app / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(relative)
            if 'MacOS' in path.parts:
                path.chmod(0o700)
        native = self.app / 'Contents/Resources/native'
        native.mkdir(parents=True)
        helpers = ['llama-worker', 'whisper-worker', 'microphone-worker']
        (native / 'manifest.json').write_text(json.dumps({'helpers': [{'name': name} for name in helpers]}))
        for name in helpers + ['document-extractor', 'pdf-renderer']:
            (native / name).write_text(name)
            (native / name).chmod(0o700)
        clipper = self.app / 'Contents/Resources/browser-clipper'
        clipper.mkdir(parents=True)
        for name in ('manifest.json', 'popup.html', 'popup.js', 'popup.css', 'extract.js', 'README.md'):
            (clipper / name).write_text(name)
        self.source = {'source.go': 'a' * 64}
        self.bundle = packaging.p0.tree_fingerprint(self.app)
        checks = [{'name': name, 'result': 'passed'} for name in sorted(packaging.MANDATORY_CHECKS)]
        tests = [{'test': packaging.policy.WORKSPACE + test, 'phase': 'go-race-non-native', 'result': 'passed'}
                 for _, names, _ in packaging.policy.FAMILIES.values() for test in names]
        tests += [{'test': test, 'phase': 'p1-offline', 'result': 'passed'} for test in packaging.policy.OFFLINE_TESTS]
        tests.append({'test': packaging.policy.WORKSPACE + 'TestPhase5V10PairedRelayProtocolConsentAndCancellation',
                      'phase': 'packaged-paired-relay', 'result': 'passed'})
        for relative in packaging.MANDATORY_ARTIFACTS:
            path = self.run / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('{}\n')
        for name in ('source-before.json', 'source-after.json'):
            (self.run / name).write_text(json.dumps(self.source))
        for name in ('bundle-before.json', 'bundle-after.json'):
            (self.run / name).write_text(json.dumps(self.bundle))
        self.report = {
            'schemaVersion': 1, 'scope': 'phase5-p1-qualification', 'releaseVersion': '0.5.0',
            'status': 'automated_passed', 'interrupted': False, 'releaseApproved': False,
            'runEvidencePath': str(self.path), 'candidate': str(self.app),
            'checks': checks, 'tests': tests,
            'verificationFamilies': packaging.policy.evaluate_families(tests, checks),
            'packages': {key: {**value, 'automatedResult': 'passed'} for key, value in packaging.policy.PACKAGES.items()},
            'inputSha256': self.source, 'bundleFileSha256': self.bundle,
            'artifactSha256': {name: packaging.p0.digest(self.run / name) for name in packaging.MANDATORY_ARTIFACTS},
            'limitations': ['Fixture evidence is not full approval'],
            'releaseGates': [{'gate': 'Interactive acceptance', 'status': 'pending'}],
            'optionalIntegrations': [{'name': 'Managed vendors', 'availability': 'disabled'}],
        }
        self.addCleanup(patch.stopall)
        patch.object(packaging.p0, 'ROOT', self.root).start()
        patch.object(packaging.p0, 'source_fingerprint', return_value=self.source).start()
        self.signature = patch.object(packaging, 'verify_signature').start()
        self.save()

    def save(self):
        self.path.write_text(json.dumps(self.report))

    def test_complete_candidate_validates_without_promoting_approval(self):
        report, candidate, sha = packaging.validate_candidate(self.path)
        self.assertIs(report['releaseApproved'], False)
        self.assertEqual(candidate, self.app.resolve())
        self.assertEqual(sha, packaging.p0.digest(self.path))
        self.signature.assert_called_once_with(self.app.resolve())

    def test_missing_failed_or_falsely_approved_reports_are_rejected(self):
        original = copy.deepcopy(self.report)
        changes = [
            lambda r: r.update(status='failed'),
            lambda r: r.update(releaseApproved=True),
            lambda r: r.update(interrupted=True),
            lambda r: r.update(checks=[c for c in r['checks'] if c['name'] != 'p1-offline-required-tests']),
            lambda r: r['checks'][0].update(result='skipped'),
            lambda r: r['verificationFamilies'][0].update(result='failed'),
            lambda r: r.update(tests=[t for t in r['tests'] if t['phase'] != 'packaged-paired-relay']),
            lambda r: r['artifactSha256'].pop('native-one-hour-native.json'),
        ]
        for change in changes:
            self.report = copy.deepcopy(original)
            change(self.report)
            self.save()
            with self.assertRaises(packaging.ValidationError):
                packaging.validate_candidate(self.path)
        self.signature.assert_not_called()

    def test_artifact_source_and_bundle_changes_are_rejected(self):
        target = self.run / 'contracts.log'
        original = target.read_bytes()
        target.write_text('changed evidence')
        with self.assertRaisesRegex(packaging.ValidationError, 'Artifact hash mismatch'):
            packaging.validate_candidate(self.path)
        target.write_bytes(original)
        with patch.object(packaging.p0, 'source_fingerprint', return_value={'source.go': 'b' * 64}):
            with self.assertRaisesRegex(packaging.ValidationError, 'Current source differs'):
                packaging.validate_candidate(self.path)
        (self.app / 'Contents/MacOS/trove-mcp').write_text('different relay')
        with self.assertRaisesRegex(packaging.ValidationError, 'Current app differs'):
            packaging.validate_candidate(self.path)
        self.signature.assert_not_called()

    def test_mutable_report_alias_and_escaping_artifacts_are_rejected(self):
        alias = self.root / 'evidence/phase5/latest-run.json'
        alias.write_text(self.path.read_text())
        with self.assertRaisesRegex(packaging.ValidationError, 'immutable'):
            packaging.validate_candidate(alias)
        self.report['artifactSha256']['../outside.log'] = 'a' * 64
        self.save()
        with self.assertRaisesRegex(packaging.ValidationError, 'unsafe path'):
            packaging.validate_candidate(self.path)

    def test_lost_executable_permissions_are_rejected_even_when_content_matches(self):
        for relative in ('Contents/MacOS/Trove', 'Contents/MacOS/trove-mcp',
                         'Contents/Resources/native/document-extractor', 'Contents/Resources/native/pdf-renderer', 'Contents/Resources/native/llama-worker'):
            path = self.app / relative
            path.chmod(0o600)
            self.assertEqual(packaging.p0.tree_fingerprint(self.app), self.bundle)
            with self.assertRaisesRegex(packaging.ValidationError, 'not executable'):
                packaging.validate_candidate(self.path)
            path.chmod(0o700)
        self.signature.assert_not_called()

    def test_existing_zip_or_manifest_is_never_overwritten(self):
        output = self.root / 'Trove.zip'
        for target in (output, output.with_suffix('.zip.manifest.json')):
            target.write_text('preserve')
            with self.assertRaisesRegex(packaging.ValidationError, 'Refusing to overwrite'):
                packaging.package_candidate(self.path, output)
            self.assertEqual(target.read_text(), 'preserve')
            target.unlink()
        self.signature.assert_not_called()


if __name__ == '__main__':
    unittest.main()
