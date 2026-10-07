"""Candidate-bound qualification evidence. Never grants release approval.

Only named policy selectors can execute. Observations are explicit operator
attestations, not inferred manual/device/account outcomes. All paths are local.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[1]
POLICY = ROOT / 'docs/completion/qualification-policy.json'
STATES = {'passed', 'failed', 'skipped', 'pending', 'not_available', 'unqualified'}
HEX = re.compile(r'^[0-9a-f]{64}$')


def stamp():
    return datetime.now(timezone.utc).isoformat()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()


def digest_value(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON field: ' + key)
        result[key] = value
    return result


def read_json(path):
    if path.stat().st_size > 32 << 20:
        raise ValueError('JSON record exceeds 32 MiB')
    return json.loads(path.read_text(), object_pairs_hook=_pairs)


def write_new(path, value):
    with path.open('x', encoding='utf-8') as stream:
        json.dump(value, stream, indent=2, ensure_ascii=False)
        stream.write('\n')


def file_identity(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError('expected a regular non-symlink file: ' + str(path))
    before = path.stat()
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b''):
            h.update(chunk)
    after = path.stat()
    if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns):
        raise ValueError('input changed while hashing: ' + str(path))
    return {'sha256': h.hexdigest(), 'size': after.st_size}


def bind_path(path):
    path = Path(os.path.abspath(path))
    if path.is_symlink():
        raise ValueError('symlink input is unsupported: ' + str(path))
    if path.is_file():
        return {'path': str(path), 'kind': 'file', **file_identity(path)}
    if not path.is_dir():
        raise ValueError('missing input: ' + str(path))
    files = {}
    directories = ['.']
    for directory, children, names in os.walk(path, followlinks=False):
        for child in sorted(children):
            full = Path(directory) / child
            if full.is_symlink():
                raise ValueError('symlink in bound directory: ' + str(full))
            directories.append(full.relative_to(path).as_posix())
        for name in sorted(names):
            full = Path(directory) / name
            files[full.relative_to(path).as_posix()] = file_identity(full)
            if len(files) + len(directories) > 200000:
                raise ValueError('inventory exceeds 200000 entries')
    if not files:
        raise ValueError('empty input directory: ' + str(path))
    inventory = {'files': files, 'directories': sorted(directories)}
    return {'path': str(path), 'kind': 'directory', 'inventory': inventory,
            'sha256': digest_value(inventory)}


def verify_binding(binding):
    return bind_path(Path(binding['path'])) == binding


def load_policy(path=POLICY):
    policy = read_json(path)
    if policy.get('schemaVersion') != 1 or policy.get('tiers') != ['P0', 'P1', 'P2']:
        raise ValueError('unsupported qualification policy')
    if not policy.get('checks') or not isinstance(policy.get('acceptanceCases'), dict):
        raise ValueError('checks and explicit acceptance mapping registry required')
    seen = set()
    for check in policy['checks']:
        if not re.fullmatch(r'[a-z][a-z0-9-]+', check['id']) or check['id'] in seen:
            raise ValueError('invalid/duplicate check ID')
        seen.add(check['id'])
        if check['tier'] not in policy['tiers'] or check['scope'] not in ('software', 'release'):
            raise ValueError('invalid check scope/tier')
        if not check['targets'] or not set(check['targets']) <= set(policy['targets']):
            raise ValueError('invalid check targets')
        if not set(check['prerequisites']) <= set(policy['prerequisites']):
            raise ValueError('unknown prerequisite')
        if not set(check['assets']) <= {'helpers', 'models', 'fixtures'}:
            raise ValueError('unknown asset group')
        if check['kind'] == 'go':
            if check['package'] != './internal/workspace' or not check['tests'] or len(set(check['tests'])) != len(check['tests']):
                raise ValueError('invalid Go selector')
            if any(not re.fullmatch(r'Test[A-Za-z0-9_]+', test) or test.startswith('TestReal') for test in check['tests']):
                raise ValueError('only explicit device-free Go selectors may execute')
        elif check['kind'] == 'observation':
            ids = [s['id'] for s in check['scenarios']]
            if not ids or len(ids) != len(set(ids)) or any(not s['expected'] for s in check['scenarios']):
                raise ValueError('observation scenarios must be explicit and unique')
        else:
            raise ValueError('unknown check kind')
    return policy


def selected_checks(policy, target, tier):
    if target not in policy['targets'] or tier not in policy['tiers']:
        raise ValueError('unknown target/tier')
    tiers = policy['tiers'][:policy['tiers'].index(tier) + 1]
    return [c for c in policy['checks'] if target in c['targets'] and c['tier'] in tiers]


def acceptance_catalog(policy, target, tier):
    """Read original acceptance text, never its stale implementation/pass flags.

    Backlog scope is conservative at its earliest committed tier. Mixed-tier
    criteria need explicit reviewed decomposition before adding acceptance cases;
    a broad package link or status flag supplies no pass.
    """
    selected_checks(policy, target, tier)
    levels = policy['tiers'][:policy['tiers'].index(tier) + 1]
    ledger = read_json(ROOT / 'docs/phase-0/requirements-ledger.json')
    registry = read_json(ROOT / 'docs/completion/registry.json')
    backlog = (ROOT / 'docs/remaining-work.md').read_text()
    rows = []
    for requirement in ledger['requirements']:
        if requirement['scope'] != 'product':
            continue
        for number, expected in enumerate(requirement['acceptanceCriteria'], 1):
            rows.append({'id': 'requirement:' + requirement['id'] + ':' + str(number),
                         'sourceId': requirement['id'], 'release': requirement['release'],
                         'applicability': 'required' if requirement['release'] in levels else 'later_tier',
                         'expected': expected})
    sections = dict(re.findall(r'^### ((?:FIX|CORE|PARITY|AI|EXT|REL|GOV|OPT)-\d{3}) [^\n]+\n(.*?)(?=^### |\Z)', backlog, re.M | re.S))
    if len(registry['items']) != 51 or set(sections) != {i['id'] for i in registry['items']}:
        raise ValueError('acceptance inventory must account for all 51 backlog items')
    for item in registry['items']:
        metadata = sections[item['id']].split('**', 2)[1]
        tiers = re.findall(r'P[012]', metadata)
        first = min(tiers) if tiers else 'P0'
        scope = 'required' if first in levels else 'later_tier'
        if item['id'] == 'REL-009' and target == 'darwin-arm64':
            scope = 'other_platform'
        rows.append({'id': 'backlog:' + item['id'], 'sourceId': item['id'], 'release': first,
                     'applicability': scope, 'scopeText': metadata,
                     'expected': item['acceptance_scenario']})
    return {'target': target, 'tier': tier, 'rows': rows, 'backlogItems': 51,
            'meaning': 'Unmapped criteria stay pending. Multi-release wording requires reviewed tier decomposition; broad package associations are not acceptance.'}


def acceptance_coverage(policy, catalog, receipts=None):
    receipts = receipts or {}
    checks = {c['id']: c for c in selected_checks(policy, catalog['target'], catalog['tier'])}
    mappings = policy['acceptanceCases'].get(catalog['target'] + '/' + catalog['tier'], {})
    expected_ids = {r['id'] for r in catalog['rows'] if r['applicability'] == 'required'}
    errors = []
    if not isinstance(mappings, dict) or not set(mappings) <= expected_ids:
        errors.append('acceptance mappings reference unknown/inapplicable criteria')
        mappings = {}
    rows = []
    for criterion in catalog['rows']:
        if criterion['applicability'] != 'required':
            rows.append({'id': criterion['id'], 'result': criterion['applicability']})
            continue
        mapping = mappings.get(criterion['id'])
        result = 'pending'
        if mapping is not None:
            # Criteria are independently stated expectations, not a global
            # completed flag. Require dedicated exact scenario expectations.
            if not isinstance(mapping, dict) or set(mapping) != {'criterionSha256', 'cases'} or mapping.get('criterionSha256') != digest_value(criterion) or not isinstance(mapping.get('cases'), list) or not mapping['cases']:
                result = 'invalid'
            else:
                outcomes = []
                for case in mapping['cases']:
                    if not isinstance(case, dict) or set(case) != {'checkId', 'scenarioId'}:
                        outcomes.append('invalid'); continue
                    check = checks.get(case['checkId'])
                    scenario = next((s for s in check['scenarios'] if s['id'] == case['scenarioId']), None) if check else None
                    if not scenario or scenario['expected'] != criterion['expected']:
                        outcomes.append('invalid'); continue
                    outcomes.append(receipts.get(case['checkId'], 'pending'))
                result = 'passed' if all(v == 'passed' for v in outcomes) else ('invalid' if 'invalid' in outcomes else 'pending')
        rows.append({'id': criterion['id'], 'result': result})
    required = [r for r in rows if r['id'] in expected_ids]
    return {'result': 'complete' if required and all(r['result'] == 'passed' for r in required) and not errors else 'pending',
            'requiredCriteria': len(required), 'passedCriteria': sum(r['result'] == 'passed' for r in required),
            'accountedBacklogItems': catalog['backlogItems'], 'errors': errors, 'criteria': rows}


def pending_plan(policy, target, tier):
    checks = selected_checks(policy, target, tier)
    needed = sorted({p for c in checks for p in c['prerequisites']})
    return {'target': target, 'tier': tier, 'mode': 'dry_run', 'status': 'pending',
            'softwareChecks': 'pending', 'releaseEligibility': 'pending', 'releaseQualified': False, 'releaseApproved': False,
            'acceptanceCoverage': acceptance_coverage(policy, acceptance_catalog(policy, target, tier)),
            'prerequisites': {p: {'status': policy['prerequisites'][p].get(target, policy['prerequisites'][p]['default']),
                                  'reason': policy['prerequisites'][p]['reason']} for p in needed},
            'checks': [{'id': c['id'], 'kind': c['kind'], 'group': c['group'], 'scope': c['scope'],
                        'tier': c['tier'], 'status': 'pending', 'tests': c.get('tests', []),
                        'scenarios': c['scenarios']} for c in checks]}


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def clean_source():
    if git('status', '--porcelain', '--untracked-files=normal'):
        raise ValueError('qualification execution requires a clean committed source tree')
    return git('rev-parse', 'HEAD'), git('rev-parse', 'HEAD^{tree}')


def freeze(args, policy):
    commit, tree = clean_source()
    plan = pending_plan(policy, args.target, args.tier)
    assets = {name: [bind_path(p) for p in getattr(args, name)] for name in ('helpers', 'models', 'fixtures')}
    bundle = bind_path(args.bundle)
    if bundle['kind'] != 'directory':
        raise ValueError('application bundle must be a directory')
    output = args.output.absolute()
    if output == ROOT or ROOT in output.parents:
        raise ValueError('qualification output must be outside the source checkout')
    output.mkdir(parents=True, exist_ok=False)
    archive = output / 'source.tar'
    with archive.open('xb') as stream:
        subprocess.run(['git', 'archive', '--format=tar', commit], cwd=ROOT, stdout=stream, check=True)
    if clean_source() != (commit, tree):
        raise ValueError('source changed during candidate freeze')
    candidate = {'schemaVersion': 1, 'qualificationId': str(uuid.uuid4()), 'frozenAt': stamp(),
                 'sourceCommit': commit, 'sourceTree': tree, 'sourceArchive': bind_path(archive),
                 'target': args.target, 'tier': args.tier, 'environment': {'hardware': args.hardware, 'osVersion': args.os_version},
                 'policySha256': digest_value(policy), 'acceptanceCatalogSha256': digest_value(acceptance_catalog(policy, args.target, args.tier)), 'bundle': bundle, 'assets': assets,
                 'requiredChecks': [c['id'] for c in selected_checks(policy, args.target, args.tier)],
                 'prerequisites': plan['prerequisites'], 'releaseApproved': False}
    candidate['candidateId'] = digest_value(candidate)
    errors = validate_candidate(candidate, policy)
    if errors:
        raise ValueError('; '.join(errors))
    write_new(output / 'candidate.json', candidate)
    (output / 'checks').mkdir()
    return {'record': str(output / 'candidate.json'), 'candidateId': candidate['candidateId'], 'status': 'pending', 'releaseApproved': False}


def validate_candidate(candidate, policy, verify_files=True):
    errors = []
    required = {'schemaVersion', 'qualificationId', 'frozenAt', 'sourceCommit', 'sourceTree', 'sourceArchive', 'target', 'tier', 'environment', 'policySha256', 'acceptanceCatalogSha256', 'bundle', 'assets', 'requiredChecks', 'prerequisites', 'releaseApproved', 'candidateId'}
    if set(candidate) != required:
        return ['candidate fields do not match schema']
    copy = dict(candidate)
    key = copy.pop('candidateId')
    if key != digest_value(copy):
        errors.append('candidate identity changed')
    if candidate['schemaVersion'] != 1 or candidate['releaseApproved'] is not False:
        errors.append('candidate cannot grant release approval')
    if candidate['policySha256'] != digest_value(policy):
        errors.append('policy identity differs; old records cannot drop new gates')
    for field in ('sourceCommit', 'sourceTree'):
        if not isinstance(candidate[field], str) or not re.fullmatch(r'[0-9a-f]{40}', candidate[field]):
            errors.append('invalid ' + field)
    try:
        checks = selected_checks(policy, candidate['target'], candidate['tier'])
        if candidate['acceptanceCatalogSha256'] != digest_value(acceptance_catalog(policy, candidate['target'], candidate['tier'])):
            errors.append('acceptance criteria/scope inventory changed')
        if candidate['requiredChecks'] != [c['id'] for c in checks]:
            errors.append('required checks omitted, reordered or substituted')
        needed = {p for c in checks for p in c['prerequisites']}
        if set(candidate['prerequisites']) != needed:
            errors.append('prerequisite inventory differs')
    except (ValueError, KeyError):
        errors.append('invalid candidate target/tier')
    if set(candidate['assets']) != {'helpers', 'models', 'fixtures'}:
        errors.append('asset inventory groups differ')
    try:
        datetime.fromisoformat(candidate['frozenAt'])
        uuid.UUID(candidate['qualificationId'])
        if not all(isinstance(candidate['environment'].get(k), str) and candidate['environment'][k].strip() for k in ('hardware', 'osVersion')):
            errors.append('recorded hardware and OS version required')
        if verify_files:
            for binding in [candidate['bundle'], candidate['sourceArchive'], *[b for values in candidate['assets'].values() for b in values]]:
                if not verify_binding(binding):
                    errors.append('bound bytes/inventory changed: ' + binding['path'])
    except (OSError, ValueError, KeyError, TypeError) as exc:
        errors.append('invalid/missing bound input: ' + str(exc))
    return errors


def go_result(log, check, exit_code):
    terminal = {}
    package_passes = 0
    errors = []
    package = 'trove.local/phase0/' + check['package'].removeprefix('./')
    for line in log.splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except ValueError:
            errors.append('non-JSON Go output')
            continue
        action, test = event.get('Action'), event.get('Test')
        if action in ('pass', 'fail', 'skip'):
            if event.get('Package') != package:
                errors.append('unexpected package identity')
            if action != 'pass':
                errors.append('failed/skipped selector: ' + (test or package))
            if test is None and action == 'pass':
                package_passes += 1
            if test and '/' not in test:
                if test in terminal:
                    errors.append('duplicate terminal test event')
                terminal[test] = action
    if exit_code != 0:
        errors.append('nonzero command exit')
    if package_passes != 1:
        errors.append('exact package completion event missing/duplicated')
    if set(terminal) != set(check['tests']):
        errors.append('exact required tests did not execute')
    return ('passed' if not errors else 'failed'), errors


def validate_observation(observation, candidate, policy, check):
    errors = []
    required = {'candidateId', 'policySha256', 'checkId', 'observedAt', 'observer', 'environment', 'scenarios', 'prerequisites', 'artifacts'}
    if set(observation) != required:
        return ['observation fields do not match schema']
    if observation['candidateId'] != candidate['candidateId'] or observation['policySha256'] != digest_value(policy) or observation['checkId'] != check['id']:
        errors.append('observation identity mismatch')
    try:
        observed = datetime.fromisoformat(observation['observedAt'])
        frozen = datetime.fromisoformat(candidate['frozenAt'])
        if observed.tzinfo is None or observed < frozen or observed > datetime.now(timezone.utc):
            errors.append('stale/future observation')
    except (TypeError, ValueError):
        errors.append('invalid observation timestamp')
    if not isinstance(observation['observer'], str) or not observation['observer'].strip():
        errors.append('named observer required')
    if observation['environment'] != {'target': candidate['target'], **candidate['environment']}:
        errors.append('observation environment differs')
    rows = observation['scenarios']
    expected = {s['id']: s['expected'] for s in check['scenarios']}
    if not isinstance(rows, list) or any(not isinstance(r, dict) for r in rows):
        return errors + ['invalid scenarios']
    if len(rows) != len(expected) or {r.get('id') for r in rows} != set(expected):
        errors.append('required scenarios omitted/duplicated/substituted')
    for row in rows:
        if set(row) != {'id', 'expected', 'result', 'observed', 'artifactIds'} or row.get('expected') != expected.get(row.get('id')):
            errors.append('scenario definition differs from policy')
        if row.get('result') not in STATES or not isinstance(row.get('observed'), str) or not row['observed'].strip():
            errors.append('explicit result and actual observation required')
        if not row.get('artifactIds'):
            errors.append('each observation needs actual supporting artifact references')
    prerequisites = observation['prerequisites']
    if not isinstance(prerequisites, dict) or set(prerequisites) != set(check['prerequisites']):
        errors.append('required prerequisites omitted/substituted')
    else:
        for value in prerequisites.values():
            if not isinstance(value, dict) or set(value) != {'status', 'description', 'artifactIds'} or value.get('status') not in ('available', 'unknown', 'not_available') or not value.get('description') or not value.get('artifactIds'):
                errors.append('prerequisite needs explicit availability and evidence')
    artifacts = observation['artifacts']
    if not isinstance(artifacts, dict) or not artifacts:
        errors.append('no actual evidence artifacts')
    else:
        references = [r.get('artifactIds', []) for r in rows]
        if isinstance(prerequisites, dict):
            references.extend(v.get('artifactIds', []) for v in prerequisites.values() if isinstance(v, dict))
        if any(not isinstance(refs, list) or not set(refs) <= set(artifacts) for refs in references):
            errors.append('unknown artifact reference')
        try:
            for key, binding in artifacts.items():
                if not re.fullmatch(r'[a-zA-Z0-9_-]+', key) or binding.get('kind') != 'file' or not verify_binding(binding):
                    errors.append('evidence artifact hash/inventory differs: ' + key)
        except (OSError, ValueError, KeyError, TypeError) as exc:
            errors.append('missing/invalid evidence: ' + str(exc))
    for name in check['assets']:
        if not candidate['assets'].get(name):
            errors.append('candidate lacks required ' + name + ' identities')
    return errors


def observation_result(observation):
    states = [r['result'] for r in observation['scenarios']]
    if 'failed' in states:
        return 'failed'
    if all(s == 'passed' for s in states) and all(p['status'] == 'available' for p in observation['prerequisites'].values()):
        return 'passed'
    return 'unqualified'


def receipt(candidate, policy, check, result, evidence, **extra):
    return {'schemaVersion': 1, 'candidateId': candidate['candidateId'], 'policySha256': digest_value(policy),
            'checkId': check['id'], 'checkSpecSha256': digest_value(check), 'recordedAt': stamp(),
            'result': result, 'evidence': evidence, 'releaseApproved': False, **extra}


def validate_receipt(value, candidate, policy, check):
    errors = []
    if value.get('schemaVersion') != 1 or value.get('releaseApproved') is not False or value.get('result') not in STATES:
        errors.append('invalid receipt state')
    if (value.get('candidateId'), value.get('policySha256'), value.get('checkId'), value.get('checkSpecSha256')) != (candidate['candidateId'], digest_value(policy), check['id'], digest_value(check)):
        errors.append('receipt identity mismatch')
    try:
        recorded = datetime.fromisoformat(value['recordedAt'])
        if recorded.tzinfo is None or recorded < datetime.fromisoformat(candidate['frozenAt']) or recorded > datetime.now(timezone.utc):
            errors.append('stale/future receipt')
        if not value.get('evidence') or any(not verify_binding(b) for b in value['evidence']):
            errors.append('receipt evidence changed/missing')
        if check['kind'] == 'go':
            expected_command = [value['tool']['path'], 'test', '-json', '-count=1', '-race', '-timeout=13m', '-tags=fts5 sqlite_omit_load_extension', check['package'], '-run', '^(' + '|'.join(check['tests']) + ')$']
            if value.get('command') != expected_command or not verify_binding(value['tool']) or value.get('environment') != {'target': candidate['target'], **candidate['environment']}:
                errors.append('recorded command/tool/environment differs')
            result, detail = go_result(Path(value['evidence'][0]['path']).read_text(), check, value['exitCode'])
            if value['result'] != result:
                errors.append('claimed Go result differs from raw events')
            if detail and value['result'] == 'passed':
                errors.extend(detail)
        else:
            observation = read_json(Path(value['evidence'][0]['path']))
            errors.extend(validate_observation(observation, candidate, policy, check))
            if value['result'] != observation_result(observation):
                errors.append('claimed observation result differs')
    except (OSError, ValueError, KeyError, TypeError) as exc:
        errors.append('invalid/missing receipt evidence: ' + str(exc))
    return errors


def status(record, policy):
    candidate = read_json(record)
    errors = validate_candidate(candidate, policy)
    if errors:
        return {'status': 'invalid', 'errors': errors, 'softwareChecks': 'unqualified', 'releaseEligibility': 'unqualified', 'releaseQualified': False, 'releaseApproved': False}
    rows = []
    expected = selected_checks(policy, candidate['target'], candidate['tier'])
    checks_root = record.parent / 'checks'
    if checks_root.exists():
        extra = set(p.name for p in checks_root.iterdir()) - {c['id'] for c in expected}
        if extra:
            errors.append('unexpected check evidence directories: ' + ', '.join(sorted(extra)))
    for check in expected:
        path = checks_root / check['id'] / 'receipt.json'
        problems = []
        result = 'pending'
        if path.exists():
            try:
                value = read_json(path)
                problems = validate_receipt(value, candidate, policy, check)
                result = 'invalid' if problems else value['result']
            except (OSError, ValueError, KeyError, TypeError) as exc:
                problems = [str(exc)]
                result = 'invalid'
        rows.append({'id': check['id'], 'scope': check['scope'], 'group': check['group'], 'result': result, 'errors': problems})
    software = all(r['result'] == 'passed' for r in rows if r['scope'] == 'software') and not errors
    coverage = acceptance_coverage(policy, acceptance_catalog(policy, candidate['target'], candidate['tier']), {r['id']: r['result'] for r in rows})
    minimum = all(r['result'] == 'passed' for r in rows) and not errors
    eligible = minimum and coverage['result'] == 'complete'
    return {'candidateId': candidate['candidateId'], 'target': candidate['target'], 'tier': candidate['tier'],
            'status': 'evidence_complete' if eligible else 'unqualified', 'softwareChecks': 'passed' if software else 'pending_or_failed',
            'minimumGates': 'passed' if minimum else 'pending_or_failed', 'acceptanceCoverage': coverage,
            'releaseEligibility': 'evidence_complete' if eligible else 'unqualified', 'releaseQualified': eligible, 'releaseApproved': False,
            'approval': 'Separate human release decision required; this tool never approves distribution.',
            'checks': rows, 'errors': errors}


def load_run(record, policy, check_id):
    candidate = read_json(record)
    errors = validate_candidate(candidate, policy)
    if errors:
        raise ValueError('; '.join(errors))
    checks = {c['id']: c for c in selected_checks(policy, candidate['target'], candidate['tier'])}
    if check_id not in checks:
        raise ValueError('check is not required for this candidate')
    return candidate, checks[check_id]


def observation_template(args, policy):
    candidate, check = load_run(args.record, policy, args.check)
    if check['kind'] != 'observation':
        raise ValueError('automated checks do not accept manual substitutes')
    return {'candidateId': candidate['candidateId'], 'policySha256': digest_value(policy), 'checkId': check['id'],
            'observedAt': '', 'observer': '', 'environment': {'target': candidate['target'], **candidate['environment']},
            'scenarios': [{**s, 'result': 'pending', 'observed': '', 'artifactIds': []} for s in check['scenarios']],
            'prerequisites': {p: {'status': candidate['prerequisites'][p]['status'], 'description': candidate['prerequisites'][p]['reason'], 'artifactIds': []} for p in check['prerequisites']},
            'artifacts': {}}


def observe(args, policy):
    candidate, check = load_run(args.record, policy, args.check)
    if check['kind'] != 'observation':
        raise ValueError('this check requires the named automated runner')
    value = read_json(args.observation)
    errors = validate_observation(value, candidate, policy, check)
    if errors:
        raise ValueError('; '.join(errors))
    directory = args.record.parent / 'checks' / check['id']
    directory.mkdir(parents=True, exist_ok=False)
    write_new(directory / 'observation.json', value)
    result = receipt(candidate, policy, check, observation_result(value), [bind_path(directory / 'observation.json')])
    write_new(directory / 'receipt.json', result)
    return result


def run(args, policy):
    candidate, check = load_run(args.record, policy, args.check)
    if check['kind'] != 'go':
        raise ValueError('this gate requires explicit actual observations; it has no automatic executor')
    if clean_source() != (candidate['sourceCommit'], candidate['sourceTree']):
        raise ValueError('source checkout differs from candidate')
    from devtools import build_env, go_binary
    from native_targets import host_target
    if host_target() != candidate['target']:
        raise ValueError('target execution requires matching native host')
    env = {k: v for k, v in os.environ.items() if not k.startswith(('TROVE_', 'DYLD_', 'GOFLAGS', 'GOOS', 'GOARCH', 'CGO_'))}
    env = build_env(args.cache, source=env)
    env.update(CGO_ENABLED='1', GOENV='off', GOWORK='off')
    binary = go_binary(args.cache)
    selector = '^(' + '|'.join(check['tests']) + ')$'
    command = [binary, 'test', '-json', '-count=1', '-race', '-timeout=13m', '-tags=fts5 sqlite_omit_load_extension', check['package'], '-run', selector]
    directory = args.record.parent / 'checks' / check['id']
    directory.mkdir(parents=True, exist_ok=False)
    log = directory / 'go.jsonl'
    error = None
    code = None
    with log.open('xb') as output:
        try:
            process = subprocess.run(command, cwd=ROOT, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=check['timeoutSeconds'])
            code = process.returncode
        except (subprocess.TimeoutExpired, OSError) as exc:
            error = str(exc)
    result, details = go_result(log.read_text(errors='replace'), check, code)
    if clean_source() != (candidate['sourceCommit'], candidate['sourceTree']) or validate_candidate(candidate, policy):
        result = 'failed'
        details.append('source or bound inputs changed during execution')
    value = receipt(candidate, policy, check, result, [bind_path(log)], command=command, tool=bind_path(Path(binary)), exitCode=code,
                    errors=details + ([error] if error else []), environment={'target': host_target(), **candidate['environment']})
    write_new(directory / 'receipt.json', value)
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='operation', required=True)
    plan = sub.add_parser('plan', help='read-only dry run; all checks remain pending')
    plan.add_argument('--target', required=True)
    plan.add_argument('--tier', choices=['P0', 'P1', 'P2'], required=True)
    frozen = sub.add_parser('freeze', help='freeze exact local inputs into a new directory; does not qualify them')
    for name in ('bundle', 'output'):
        frozen.add_argument('--' + name, type=Path, required=True)
    for name in ('helpers', 'models', 'fixtures'):
        frozen.add_argument('--' + name, type=Path, action='append', default=[])
    frozen.add_argument('--target', required=True)
    frozen.add_argument('--tier', required=True, choices=['P0', 'P1', 'P2'])
    frozen.add_argument('--hardware', required=True)
    frozen.add_argument('--os-version', required=True)
    coverage_parser = sub.add_parser('coverage', help='read-only exact acceptance catalog; no completion inference')
    coverage_parser.add_argument('--target', required=True)
    coverage_parser.add_argument('--tier', required=True, choices=['P0', 'P1', 'P2'])
    stat = sub.add_parser('status', help='read-only identity/evidence check; never grants approval')
    stat.add_argument('--record', type=Path, required=True)
    execute = sub.add_parser('run', help='run one exact allowlisted software check on the native target')
    execute.add_argument('--record', type=Path, required=True)
    execute.add_argument('--check', required=True)
    execute.add_argument('--cache', type=Path)
    template = sub.add_parser('template', help='read-only unfilled observation template; never passing evidence')
    template.add_argument('--record', type=Path, required=True)
    template.add_argument('--check', required=True)
    hashed = sub.add_parser('hash', help='read-only exact local input identity; hash existence is not a passing observation')
    hashed.add_argument('--path', type=Path, required=True)
    observed = sub.add_parser('observe', help='record an actual operator observation with hashed artifacts')
    observed.add_argument('--record', type=Path, required=True)
    observed.add_argument('--check', required=True)
    observed.add_argument('--observation', type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        policy = load_policy()
        if args.operation == 'hash':
            result = bind_path(args.path)
        elif args.operation == 'template':
            result = observation_template(args, policy)
        elif args.operation == 'plan':
            result = pending_plan(policy, args.target, args.tier)
        elif args.operation == 'coverage':
            result = acceptance_catalog(policy, args.target, args.tier)
        elif args.operation == 'status':
            result = status(args.record, policy)
        else:
            result = globals()[args.operation](args, policy)
        print(json.dumps(result, indent=2))
        if args.operation == 'status':
            return 0 if result['releaseEligibility'] == 'evidence_complete' else 2
        return 1 if result.get('result') == 'failed' else 0
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as exc:
        print(json.dumps({'status': 'invalid', 'error': str(exc), 'releaseApproved': False}), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
