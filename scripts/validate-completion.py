#!/usr/bin/env python3
"""Read-only checks for scoped completion mappings; never qualifies product behavior."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys


def canonical_hash(value):
    raw = json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()
    return hashlib.sha256(raw).hexdigest()


def validate(root):
    errors = []

    def require(condition, message):
        if not condition:
            errors.append(message)

    def file(path):
        p = Path(path)
        require(not p.is_absolute() and '..' not in p.parts, 'unsafe relative path: ' + path)
        require((root / p).is_file(), 'missing file: ' + path)

    ledger = json.loads((root / 'docs/phase-0/requirements-ledger.json').read_text())
    registry = json.loads((root / 'docs/completion/registry.json').read_text())
    overlay = json.loads((root / 'docs/completion/requirements-overlay.json').read_text())
    require(ledger['currentImplementationOverlay']['path'] == registry['currentRequirementsOverlay'] == 'docs/completion/requirements-overlay.json', 'overlay references differ')
    historical = dict(ledger)
    historical.pop('currentImplementationOverlay')
    require(canonical_hash(historical) == overlay['historicalLedgerCanonicalSHA256'], 'historical ledger content changed')
    require(canonical_hash(ledger['requirements']) == overlay['historicalRequirementsCanonicalSHA256'], 'historical requirement rows changed')
    require(re.fullmatch(r'[0-9a-f]{40}', overlay['reviewedSourceCommit']) is not None, 'overlay source must be an exact commit')
    require(overlay.get('releaseQualified') is False, 'overlay cannot qualify a release')

    old = {row['id']: row for row in ledger['requirements']}
    product = {key for key, row in old.items() if row['scope'] == 'product'}
    mapped = {row['requirementId']: row for row in overlay['requirements']}
    excluded = {row['requirementId']: row for row in overlay['excludedRows']}
    require(len(mapped) == len(overlay['requirements']) and set(mapped) == product, 'product mappings are duplicated, missing or outside product scope')
    require(len(excluded) == len(overlay['excludedRows']) and set(excluded) == set(old) - product, 'contract/boundary exclusions are incomplete or duplicated')
    require(overlay['counts'] == {'allLedgerRows': len(old), 'mappedProductRows': len(product), 'explicitlyExcludedContractOrBoundaryRows': len(old) - len(product)}, 'overlay counts differ from actual ledger')
    for key, row in excluded.items():
        require(row['scope'] == old[key]['scope'] and bool(row['reason']), 'unexplained exclusion: ' + key)

    backlog = (root / 'docs/remaining-work.md').read_text()
    headings = re.findall(r'^### ((?:FIX|CORE|PARITY|AI|EXT|REL|GOV|OPT)-\d{3}) (.+)$', backlog, re.M)
    expected = {key: title for key, title in headings}
    items = {item['id']: item for item in registry['items']}
    require(len(items) == len(registry['items']) == len(expected) == 51 and set(items) == set(expected), 'registry must cover the actual 51 backlog IDs exactly')
    packages = overlay['packages']
    for key, package in packages.items():
        require(package['coverageStatus'] == 'scoped_source_and_evidence', 'unsupported package coverage claim: ' + key)
        require(package.get('releaseQualified') is False, 'package cannot imply release qualification: ' + key)
        require(bool(package['coveredScope']) and bool(package['remainingGates']), 'missing scope/limits: ' + key)
        require(bool(package['sourcePaths']) and bool(package['evidencePaths']), 'missing source/evidence: ' + key)
        require(set(package['backlogIds']) <= set(items), 'unknown package backlog reference: ' + key)
        for path in package['sourcePaths'] + package['evidencePaths']:
            file(path)
    for key, row in mapped.items():
        require(row['release'] == old[key]['release'], 'requirement release changed: ' + key)
        require(row['coverageStatus'] in {'mapped_scoped_coverage', 'partial_scope_unreconciled'} and row.get('releaseQualified') is False, 'mapping became a completion claim: ' + key)
        require(bool(row['packageIds']) and set(row['packageIds']) <= set(packages), 'unknown/missing package: ' + key)
        if row['coverageStatus'] == 'partial_scope_unreconciled':
            require(bool(row.get('unresolvedAcceptance')), 'partial requirement lacks explicit gap: ' + key)
        require(row['remainingGatesFromPackages'] == row['packageIds'], 'package limits dropped: ' + key)
        expected_backlog = {item for p in row['packageIds'] if p in packages for item in packages[p]['backlogIds']}
        require(set(row['backlogIds']) == expected_backlog, 'backlog coverage differs from package mapping: ' + key)
        require('implemented' not in row and 'verified' not in row, 'historical status copied into overlay: ' + key)
    for key, item in items.items():
        require(item['title'] == expected[key], 'backlog title changed: ' + key)
        slug = re.sub(r'[^a-z0-9 -]', '', (key + ' ' + expected[key]).lower()).replace(' ', '-')
        require(item['backlog_anchor'] == '../remaining-work.md#' + slug, 'backlog anchor differs: ' + key)
        require(bool(item['acceptance_scenario']) and bool(item['owners']) and bool(item['phases']), 'missing acceptance/owner/phase: ' + key)
        require(item.get('evidence_target_status') == 'planned_destination_not_passing_evidence', 'planned evidence path lacks explicit status: ' + key)
        require(item['verification']['candidate'] is None, 'item must reference exact candidate records without blanket qualification: ' + key)
        for platform in ['macOS', 'Windows', 'Linux']:
            require(item['verification'][platform] == 'unqualified', 'new platform qualification claim: ' + key)
        for field in ['implementation_evidence', 'development_candidate_evidence', 'status_evidence']:
            for path in item.get(field, []):
                file(path)
        expected_rows = {r for r, v in mapped.items() if key in v['backlogIds']}
        require(set(item['requirements_overlay_ids']) == expected_rows, 'reverse requirement mapping differs: ' + key)
        expected_packages = {p for p, v in packages.items() if key in v['backlogIds']}
        require(set(item['coverage_packages']) == expected_packages, 'reverse package mapping differs: ' + key)
    require(items['GOV-004']['implementation'] == 'awaiting_external_source', 'missing research incorrectly closed')
    require(any(s.get('id') == 'R00' and s['state'] == 'not_supplied' for s in ledger['unavailableSources']), 'missing research source status changed')

    seen = set()
    for candidate in registry['developmentCandidates']:
        commit = candidate['sourceCommit']
        require(re.fullmatch(r'[0-9a-f]{40}', commit) is not None and commit not in seen, 'invalid/duplicate candidate source')
        seen.add(commit)
        require(candidate.get('releaseApproved') is False, 'candidate release approval was inferred')
        file(candidate['manifest'])
        if candidate.get('assessment'):
            file(candidate['assessment'])
        path = root / candidate['manifest']
        if path.is_file():
            report = json.loads(path.read_text())
            require(report.get('sourceCommit') == commit, 'candidate manifest source differs: ' + commit)
            require(report.get('releaseApproved') is False, 'candidate manifest is not explicitly unapproved: ' + commit)
    for failure in overlay.get('knownCandidateFailures', []):
        require(failure['sourceCommit'] in seen, 'failure references an unknown candidate')
        require(set(failure['backlogIds']) <= set(items), 'failure references unknown backlog IDs')
        file(failure['evidencePath'])
    pending = registry.get('pendingCandidatePublication')
    if pending:
        require(pending['status'] == 'candidate_owner_publication_pending' and pending['releaseApproved'] is False, 'pending candidate implies evidence/approval')
        require(pending['sourceCommitPrefix'] not in {c['sourceCommit'][:7] for c in registry['developmentCandidates']}, 'candidate both pending and recorded')
    return errors, {'backlogItems': len(items), 'productMappings': len(mapped), 'explicitContractBoundaryExclusions': len(excluded), 'coveragePackages': len(packages), 'candidateRecords': len(seen), 'releaseQualified': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        errors, summary = validate(args.root)
    except (OSError, ValueError, KeyError, TypeError) as error:
        errors, summary = [str(error)], {}
    print(json.dumps({'mappingValidation': 'failed' if errors else 'passed', 'summary': summary, 'errors': errors}, indent=2))
    return 1 if errors else 0


if __name__ == '__main__':
    sys.exit(main())
