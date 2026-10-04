#!/usr/bin/env python3
"""Record offline dependency/model provenance and retain third-party notices.

Uses only already-installed build inputs. It never downloads models or sources.
The inventory is evidence, not a legal opinion or a claim of release approval.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

from devtools import build_env, cache_dir, go_binary, models_dir

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def license_family(text):
    if 'Apache License' in text and 'Version 2.0' in text:
        return 'Apache-2.0'
    if 'Permission is hereby granted, free of charge' in text:
        return 'MIT'
    if 'Redistribution and use in source and binary forms' in text:
        return 'BSD (see retained text for exact terms)'
    if 'ISC License' in text or ('Permission to use, copy, modify' in text and 'ISC' in text):
        return 'ISC'
    if 'CC0' in text:
        return 'CC0 (see retained text)'
    return 'See retained upstream text'


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--cache', type=Path, default=cache_dir())
    p.add_argument('--models', type=Path, default=None)
    p.add_argument('--output', type=Path, default=ROOT / 'evidence/phase3')
    args = p.parse_args()
    args.cache = cache_dir(args.cache)
    args.models = models_dir(args.models, args.cache)
    args.output.mkdir(parents=True, exist_ok=True)
    notices = args.output / 'notices'
    notices.mkdir(exist_ok=True)
    env = build_env(args.cache)
    go = go_binary(args.cache)
    warnings = []

    def retain(name, directory, recursive=False):
        selected = []
        paths = directory.rglob('*') if recursive else directory.glob('*')
        for source in sorted(paths):
            if source.is_file() and not source.name.startswith('._') and re.match(r'^(licen[sc]e|notice|copying|authors|copyright|third.party.*licen[sc]e|ofl)([.\-]|$)', source.name, re.I):
                rel = source.relative_to(directory)
                dest = notices / re.sub(r'[^a-zA-Z0-9_.-]', '_', name) / rel
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, dest)
                selected.append({'sourceRelativePath': str(rel), 'retainedPath': str(dest.relative_to(args.output)),
                                 'sha256': digest(source), 'familyFromText': license_family(source.read_text(errors='replace'))})
        return selected

    sums = {}
    for line in (ROOT / 'go.sum').read_text().splitlines():
        module, version, checksum = line.split()
        sums[(module, version)] = checksum
    cmd = [go, 'list', '-mod=readonly', '-deps', '-tags', 'desktop production fts5 sqlite_omit_load_extension',
           '-f', '{{if .Module}}{{.Module.Path}} {{.Module.Version}} {{.Module.Dir}}{{end}}', '.']
    module_lines = subprocess.check_output(cmd, cwd=ROOT, env=env, text=True)
    modules = []
    for line in sorted(set(module_lines.splitlines())):
        fields = line.split()
        if len(fields) != 3:
            continue
        name, version, directory = fields
        licenses = retain(name + '@' + version, Path(directory), recursive=True)
        checksum = sums.get((name, version))
        modules.append({'module': name, 'version': version, 'goSum': checksum,
                        'goModSum': sums.get((name, version + '/go.mod')), 'notices': licenses})
        if not checksum or not licenses:
            warnings.append('Missing hash or license notice for linked Go module: ' + name)

    lock_text = (ROOT / 'frontend/pnpm-lock.yaml').read_text()
    package_section = lock_text.split('\npackages:\n', 1)[1].split('\nsnapshots:\n', 1)[0]
    integrities = {}
    for match in re.finditer(r"^  ['\"]?(.+?)['\"]?:\n(.*?)(?=^  \S|\Z)", package_section, re.M | re.S):
        key, body = match.groups()
        integrity = re.search(r'integrity:\s*([^\s},]+)', body)
        if integrity:
            integrities[key] = integrity.group(1)
    frontend = {}
    pnpm = ROOT / 'frontend/node_modules/.pnpm'
    for store in sorted(pnpm.iterdir()):
        nested = store / 'node_modules'
        if not nested.is_dir():
            continue
        manifests = list(nested.glob('*/package.json')) + list(nested.glob('@*/*/package.json'))
        for manifest in manifests:
            if manifest.parent.is_symlink():
                continue
            body = json.loads(manifest.read_text())
            name, version = body.get('name'), body.get('version')
            if not name or not version:
                continue
            key = name + '@' + version
            if key in frontend:
                continue
            declared = body.get('license', body.get('licenses'))
            found = retain(key, manifest.parent)
            # Some packages retain notice text only in a README or package manifest.
            if not found:
                for fallback in [manifest.parent / 'README.md', manifest]:
                    if fallback.is_file():
                        dest = notices / re.sub(r'[^a-zA-Z0-9_.-]', '_', key) / fallback.name
                        dest.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copyfile(fallback, dest)
                        found.append({'sourceRelativePath': fallback.name, 'retainedPath': str(dest.relative_to(args.output)),
                                      'sha256': digest(fallback), 'familyFromText': 'Manifest/README declaration; full license may require upstream review'})
            frontend[key] = {'name': name, 'version': version, 'declaredLicense': declared,
                             'lockIntegrity': integrities.get(key), 'manifestSha256': digest(manifest), 'notices': found}
            if (not declared and not any(n['familyFromText'] != 'See retained upstream text' for n in found)) or not integrities.get(key):
                warnings.append('Missing declared license or integrity for installed frontend package: ' + key)
    lock = json.loads((ROOT / 'baseline/baseline.lock.json').read_text())
    native = []
    for name, pin in lock['workers'].items():
        archive = args.cache / (name + '.tar.gz')
        native.append({'name': name, **pin, 'localArchiveSha256': digest(archive) if archive.exists() else None,
                       'archiveVerified': archive.exists() and digest(archive) == pin['sha256'],
                       'notices': retain(name + '.cpp', args.cache / pin['sourceDirectory'], recursive=True)})
        if not native[-1]['archiveVerified']:
            warnings.append('Native archive absent or invalid: ' + name)
    vector_root = ROOT / 'internal/sqlitevec'
    vector_pin = json.loads((vector_root / 'upstream.json').read_text())
    vector_hashes = {name: digest(vector_root / name) for name in vector_pin['sourceSha256']}
    native.append({'name': 'sqlite-vec', **vector_pin, 'localSourceSha256': vector_hashes,
                   'sourceVerified': vector_hashes == vector_pin['sourceSha256'],
                   'notices': retain('sqlite-vec', vector_root)})
    if not native[-1]['sourceVerified']:
        warnings.append('Vendored sqlite-vec source differs from its pinned manifest')
    toolchain = {'go': {**lock['go'], 'licenseNotices': retain('go-toolchain', args.cache / 'go')}}
    packs = []
    for pack in json.loads((ROOT / 'native/models/catalog.json').read_text()):
        path = args.models / (pack['id'] + '.model')
        actual = digest(path) if path.exists() else None
        packs.append({**pack, 'installed': path.exists(), 'installedSha256': actual,
                      'installedSizeBytes': path.stat().st_size if path.exists() else None,
                      'verified': path.exists() and actual == pack['sha256'] and path.stat().st_size == pack['sizeBytes']})
        if path.exists() and not packs[-1]['verified']:
            warnings.append('Installed model mismatch: ' + pack['id'])
    external_notices = []
    license_root = ROOT / 'docs/phase-3/licenses'
    for record in json.loads((license_root / 'sources.json').read_text()):
        source = license_root / record['path']
        dest = notices / 'model-and-runtime-licenses' / source.name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, dest)
        external_notices.append({**record, 'sha256': digest(source), 'retainedPath': str(dest.relative_to(args.output))})
    inputs = ['go.mod', 'go.sum', 'frontend/package.json', 'frontend/pnpm-lock.yaml', 'baseline/baseline.lock.json',
              'native/models/catalog.json', 'native/models/provenance.json', 'scripts/record-release-provenance.py',
              'internal/sqlitevec/register.go', 'internal/sqlitevec/upstream.json', 'docs/phase-3/licenses/sources.json']
    sources = [p for p in (ROOT / 'schemas').glob('*.sql')]
    sources += [p for p in (ROOT / 'internal/workspace').glob('*.go') if not p.name.endswith('_test.go') and not p.name.startswith('._')]
    report = {'schemaVersion': 1, 'recordedAt': datetime.now(timezone.utc).isoformat(),
              'scope': 'macOS arm64 production Go closure; all installed frontend/build packages (conservative superset); native source archives; optional installed model packs',
              'hashVerificationPassed': not warnings, 'warnings': warnings,
              'inputSha256': {s: digest(ROOT / s) for s in inputs},
              'migrationSourceSha256': {str(s.relative_to(ROOT)): digest(s) for s in sources},
              'goDependencyCommand': cmd, 'goModules': modules, 'frontendPackages': list(frontend.values()),
              'nativeRuntimes': native, 'toolchain': toolchain, 'modelPacks': packs,
              'pinnedLicenseDocuments': external_notices,
              'modelLicenseEvidence': json.loads((ROOT / 'native/models/provenance.json').read_text()),
              'limitations': ['Model licenses are recorded publisher declarations at pinned URLs, not a legal review. Model weights are not bundled.',
                              'Registry archive integrity is retained from pnpm lock; installed frontend file trees are not independently redownloaded.',
                              'Full frontend build dependency superset is retained because bundler tree shaking is not used as a license exclusion.',
                              'OS-provided frameworks are platform dependencies; their binaries are not redistributed.']}
    (args.output / 'provenance.json').write_text(json.dumps(report, indent=2) + '\n')
    summary = ['Trove third-party notices', '', 'See provenance.json for exact versions, integrity digests and upstream model license URLs.',
               'The notices directory conservatively includes build dependencies as well as runtime dependencies.', '']
    for kind, records in [('Go modules', modules), ('Frontend packages', list(frontend.values())), ('Native runtimes', native)]:
        summary.append(kind)
        for record in records:
            summary.append('  ' + record.get('module', record.get('name', '')) + ' ' + record.get('version', record.get('commit', '')))
            for notice in record['notices']:
                summary.append('    ' + notice['retainedPath'])
        summary.append('')
    (args.output / 'THIRD-PARTY-NOTICES.txt').write_text('\n'.join(summary))
    print(json.dumps({'goModules': len(modules), 'frontendPackages': len(frontend), 'modelPacks': len(packs),
                      'warnings': warnings, 'output': str(args.output / 'provenance.json')}, indent=2))
    return 1 if warnings else 0


if __name__ == '__main__':
    raise SystemExit(main())
