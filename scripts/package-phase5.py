#!/usr/bin/env python3
"""Archive a verified Phase 5 personal-use candidate without granting release approval.

Uses local evidence, ditto and codesign only. The source app is never signed or
modified. A successful archive remains subject to every recorded release gate.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


def load_helper(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


p0 = load_helper('phase3_packaging_helpers', 'verify-phase3.py')
policy = load_helper('phase5_packaging_policy', 'phase5_policy.py')

MANDATORY_CHECKS = {
    'prerequisites', 'contracts', 'go-race-non-native', 'go-race-non-native-test-completeness',
    'frontend-types', 'frontend-unit', 'isolated-browser-server', 'frontend-browser',
    'frontend-browser-test-completeness', 'database-browser', 'database-browser-test-completeness',
    'browser-server-cleanup', 'release-inventory', 'release-inventory-completeness',
    'desktop-build', 'candidate-bundle-created', 'os-network-denial-control',
    'go-race-real-offline', 'go-race-real-offline-test-completeness',
    'packaged-p0-restart-native-offline', 'packaged-fixture-completeness',
    'p1-offline', 'p1-offline-test-completeness', 'p1-offline-required-tests',
    'packaged-p1-restart-offline', 'packaged-p1-completeness', 'packaged-paired-relay',
    'packaged-paired-relay-test-completeness', 'paired-relay-tests', 'candidate-signature',
    'source-stability', 'candidate-bundle-stability',
} | {'family-' + identity for identity in policy.FAMILIES}
MANDATORY_ARTIFACTS = {
    'source-before.json', 'source-after.json', 'bundle-before.json', 'bundle-after.json',
    'contracts.log', 'go-race-non-native.log', 'frontend-types.log', 'frontend-unit.log',
    'vite.log', 'frontend-browser.log', 'frontend-browser.json', 'database-browser.log',
    'database-browser.json', 'release-inventory.log', 'release/provenance.json',
    'desktop-build.log', 'os-network-denial-control.log', 'go-race-real-offline.log',
    'packaged-p0-restart-native-offline.log', 'packaged-integration.json',
    'p1-offline.log', 'packaged-p1-restart-offline.log', 'packaged-p1-integration.json',
    'packaged-paired-relay.log', 'candidate-signature.log',
    'native-phase1-native-smoke.json', 'native-phase1-native-failure.json',
    'native-meeting-native.json', 'native-one-hour-native.json',
}


class ValidationError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise ValidationError(message)


def read_json(path):
    try:
        return json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise ValidationError('Cannot read JSON evidence: ' + str(path)) from exc


def rooted_path(value):
    path = Path(value)
    return (path if path.is_absolute() else p0.ROOT / path).resolve()


def fingerprint_map(value, label):
    require(isinstance(value, dict) and bool(value), label + ' is empty or invalid')
    for name, sha in value.items():
        require(isinstance(name, str) and name and not Path(name).is_absolute()
                and '..' not in Path(name).parts, label + ' contains an unsafe path')
        require(isinstance(sha, str) and re.fullmatch('[0-9a-f]{64}', sha) is not None,
                label + ' contains an invalid SHA256')
    return value


def verify_signature(candidate):
    subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(candidate)], check=True)


def verify_executables(candidate):
    native = candidate / 'Contents/Resources/native'
    manifest = read_json(native / 'manifest.json')
    helpers = manifest.get('helpers', []) if isinstance(manifest, dict) else []
    names = {helper.get('name') for helper in helpers if isinstance(helper, dict)}
    require({'llama-worker', 'whisper-worker', 'microphone-worker'} <= names,
            'Required native helper declarations are missing')
    paths = [candidate / 'Contents/MacOS/Trove', candidate / 'Contents/MacOS/trove-mcp',
             native / 'document-extractor']
    for name in names:
        require(isinstance(name, str) and name and Path(name).name == name and name not in ('.', '..'),
                'Invalid native helper name')
        paths.append(native / name)
    for path in paths:
        require(path.is_file() and not path.is_symlink() and bool(path.stat().st_mode & 0o111)
                and os.access(path, os.X_OK), 'Packaged component is not executable: ' + str(path))


def validate_candidate(verification):
    """Fail closed before invoking an archiver; callable without CLI side effects."""
    verification = Path(verification).absolute()
    require(not verification.is_symlink(), 'Verification must be an original run report, not a symlink')
    verification = verification.resolve()
    report = read_json(verification)
    require(isinstance(report, dict), 'Verification report must be an object')
    require(verification.name == 'verification.json'
            and verification.parent.parent == (p0.ROOT / 'evidence/phase5/runs').resolve(),
            'Use the immutable evidence/phase5/runs/<run>/verification.json report')
    require(rooted_path(report.get('runEvidencePath', '')) == verification,
            'Report does not identify this immutable run')
    require(report.get('schemaVersion') == 1 and report.get('scope') == 'phase5-p1-qualification'
            and report.get('releaseVersion') == '0.5.0', 'Not a supported Phase 5 candidate report')
    require(report.get('status') == 'automated_passed' and report.get('interrupted') is False,
            'Qualification did not complete successfully')
    require(report.get('releaseApproved') is False, 'This packager only archives unapproved release candidates')
    require(bool(report.get('limitations')) and bool(report.get('releaseGates'))
            and bool(report.get('optionalIntegrations')), 'Candidate qualification limits are missing')
    checks = report.get('checks', [])
    require(isinstance(checks, list) and all(isinstance(c, dict) for c in checks), 'Invalid check records')
    names = [c.get('name') for c in checks]
    require(all(isinstance(name, str) for name in names) and len(names) == len(set(names)), 'Duplicate or invalid check names')
    require(MANDATORY_CHECKS <= set(names), 'Missing mandatory checks: ' + ', '.join(sorted(MANDATORY_CHECKS - set(names))))
    require(all(c.get('result') == 'passed' for c in checks), 'One or more mandatory/recorded checks did not pass')
    tests = report.get('tests', [])
    require(isinstance(tests, list) and bool(tests)
            and all(isinstance(t, dict) and isinstance(t.get('test'), str) and t.get('result') == 'passed' for t in tests),
            'Missing, skipped or unsuccessful test evidence')
    families = policy.evaluate_families(tests, checks)
    require(all(f['result'] == 'passed' for f in families), 'Current V01–V10 policy does not pass')
    require(report.get('verificationFamilies') == families, 'Recorded V01–V10 evidence differs from the current policy')
    expected_packages = {key: {**package, 'automatedResult': 'passed'} for key, package in policy.PACKAGES.items()}
    require(report.get('packages') == expected_packages, 'P1 package qualification or limitations are incomplete')
    for phase, required_tests in [
        ('p1-offline', policy.OFFLINE_TESTS),
        ('packaged-paired-relay', [policy.WORKSPACE + 'TestPhase5V10PairedRelayProtocolConsentAndCancellation']),
    ]:
        found = {test['test'] for test in tests if test.get('phase') == phase}
        require(set(required_tests) <= found, 'Required tests missing from ' + phase)
    source = fingerprint_map(report.get('inputSha256'), 'Source fingerprint')
    bundle = fingerprint_map(report.get('bundleFileSha256'), 'Bundle fingerprint')
    artifacts = fingerprint_map(report.get('artifactSha256'), 'Artifact fingerprint')
    require(MANDATORY_ARTIFACTS <= set(artifacts), 'Missing required artifact hashes: ' + ', '.join(sorted(MANDATORY_ARTIFACTS - set(artifacts))))
    for relative, sha in artifacts.items():
        artifact = verification.parent / relative
        require(artifact.resolve().is_relative_to(verification.parent) and not artifact.is_symlink()
                and artifact.is_file(), 'Artifact missing or escapes immutable run: ' + relative)
        require(p0.digest(artifact) == sha, 'Artifact hash mismatch: ' + relative)
    for check in checks:
        for field in ('log', 'report'):
            if field in check:
                artifact = rooted_path(check[field])
                require(artifact.is_relative_to(verification.parent), 'Check evidence is outside its immutable run')
                relative = str(artifact.relative_to(verification.parent))
                require(relative in artifacts, 'Check evidence has no artifact hash: ' + relative)
                if field + 'Sha256' in check:
                    require(check[field + 'Sha256'] == artifacts[relative], 'Check evidence hash mismatch: ' + relative)
    require(read_json(verification.parent / 'source-before.json') == source
            and read_json(verification.parent / 'source-after.json') == source, 'Source changed during qualification')
    require(read_json(verification.parent / 'bundle-before.json') == bundle
            and read_json(verification.parent / 'bundle-after.json') == bundle, 'App changed during qualification')
    require(p0.source_fingerprint() == source, 'Current source differs from the qualified source')
    require(isinstance(report.get('candidate'), str) and Path(report['candidate']).is_absolute(), 'Invalid candidate path')
    candidate = Path(report['candidate'])
    require(not candidate.is_symlink() and candidate.is_dir() and candidate.suffix == '.app', 'Candidate app is unavailable')
    candidate = candidate.resolve()
    require(p0.tree_fingerprint(candidate) == bundle, 'Current app differs from the qualified bundle')
    for relative in ('Contents/MacOS/Trove', 'Contents/MacOS/trove-mcp', 'Contents/Info.plist'):
        require(relative in bundle, 'Required packaged component missing: ' + relative)
    verify_executables(candidate)
    verify_signature(candidate)
    return report, candidate, p0.digest(verification)


def package_candidate(verification, output):
    output = Path(output).absolute()
    manifest_path = output.with_suffix(output.suffix + '.manifest.json')
    require(output.suffix.lower() == '.zip', 'Output must be a new ZIP file')
    for target in (output, manifest_path):
        require(not target.exists() and not target.is_symlink(), 'Refusing to overwrite: ' + str(target))
    verification = Path(verification).absolute()
    report, candidate, report_sha = validate_candidate(verification)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.trove-phase5-package-', dir=output.parent) as directory:
        temporary = Path(directory)
        archive = temporary / output.name
        subprocess.run(['/usr/bin/ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', str(candidate), str(archive)], check=True)
        extracted = temporary / 'extracted'
        subprocess.run(['/usr/bin/ditto', '-x', '-k', str(archive), str(extracted)], check=True)
        restored = extracted / candidate.name
        require(restored.is_dir() and p0.tree_fingerprint(restored) == report['bundleFileSha256'],
                'Extracted archive differs from the qualified bundle')
        verify_executables(restored)
        verify_signature(restored)
        # Recheck source, original app, evidence and signature after copying to
        # reject changes made while the archive was being produced.
        final_report, _, final_sha = validate_candidate(verification)
        require(final_sha == report_sha and final_report == report, 'Verification report changed during packaging')
        manifest = {
            'schemaVersion': 1, 'scope': 'phase5-personal-use-candidate-archive',
            'createdAt': p0.stamp(), 'releaseVersion': report['releaseVersion'],
            'automatedQualification': report['status'], 'releaseApproved': report['releaseApproved'],
            'archive': output.name, 'archiveSha256': p0.digest(archive),
            'verification': str(verification), 'verificationSha256': report_sha,
            'candidate': str(candidate), 'bundleSha256': p0.fingerprint_digest(report['bundleFileSha256']),
            'sourceSha256': p0.fingerprint_digest(report['inputSha256']),
            'extractedBundleMatches': True, 'strictSignatureVerified': True, 'executablePermissionsVerified': True,
            'verificationFamilies': report['verificationFamilies'], 'packages': report['packages'],
            'optionalIntegrations': report['optionalIntegrations'],
            'releaseGates': report['releaseGates'], 'limitations': report['limitations'],
        }
        temporary_manifest = temporary / 'manifest.json'
        p0.write_json(temporary_manifest, manifest)
        # Exclusive creation also works on volumes without hard-link support.
        # The manifest is published last and marks the archive as complete.
        publish_exclusive(archive, output)
        try:
            publish_exclusive(temporary_manifest, manifest_path)
        except BaseException:
            output.unlink()
            raise
    return manifest_path


def publish_exclusive(source, target):
    created = False
    try:
        with target.open('xb') as destination:
            created = True
            with source.open('rb') as original:
                shutil.copyfileobj(original, destination, 1024 * 1024)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        if created:
            target.unlink()
        raise


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verification', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        manifest = package_candidate(args.verification, args.output)
    except (ValidationError, OSError, subprocess.CalledProcessError) as exc:
        parser.exit(1, 'Candidate archive rejected: ' + str(exc) + '\n')
    print('Candidate archived; release approval remains withheld. Manifest: ' + str(manifest))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
