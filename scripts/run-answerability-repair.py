#!/usr/bin/env python3
"""Explicit, serialized BEFORE/AFTER pilot for the sealed 12 answerability cases.

Review this artifact before use. It never applies patches, changes source, retries
models, selects a subset, or qualifies release/performance/manual semantics.
"""
import argparse
import hashlib
import importlib.util
import json
import os
import pathlib
import platform
import re
import signal
import subprocess
import sys
import tempfile
import time

sys.dont_write_bytecode = True
CORPUS = 'internal/workspace/testdata/expanded-evaluation/answerability-transfer-v1.json'
CORPUS_SHA256 = 'a3a0a837c042955037b2e6449ff332e65cfb3bb934c43f4b45476b087a5c6d2d'
ORIGINAL_CORPUS = 'internal/workspace/testdata/expanded-evaluation/transfer-20261009.json'
ORIGINAL_SHA256 = '1eb8cb07b0047fde8e4f349be53b17613c33200d6fa44bfdbfe8d4d488f886bd'
TEST = 'TestRealLocalAnswerabilityTransferCases'
PACK = '01990000-0000-7000-8000-000000000106'
TAGS = 'fts5 sqlite_omit_load_extension'
NETWORK_POLICY = '(version 1)(allow default)(deny network*)'
GATES = ('TestAITransferIndependentCorpusAndOracle', 'TestAITransferCanonicalSeedAndScope',
         'TestAnswerabilityTransferIndependentCorpusAndOracle', 'TestAnswerabilityTransferCanonicalSeedAndScope')
HARNESS = (CORPUS, ORIGINAL_CORPUS, 'internal/workspace/ai_transfer_evaluation_test.go',
           'internal/workspace/ai_answerability_transfer_evaluation_test.go',
           'internal/workspace/ai_canonical_inventory_test.go',
           'internal/workspace/ai_expanded_evaluation_test.go',
           'internal/workspace/ai_p0_closure_holdouts_test.go',
           'scripts/run-expanded-evaluation.py', 'scripts/run-answerability-repair.py', 'scripts/test-answerability-repair.py')
SOURCE_ROOTS = ('internal', 'contracts', 'schemas')
DOMAINS = ('museum-audio', 'bicycle-clamp', 'bakery-ingredient', 'greenhouse-medium')
VARIANTS = ('absent', 'explicit-negative', 'supported-positive')
IDS = ['answerability-' + domain + '-' + variant for domain in DOMAINS for variant in VARIANTS]


def fail(message):
    raise ValueError(message)


def git(root, *arguments):
    return subprocess.check_output(['git', *arguments], cwd=root, text=True).strip()


def load_repository_validator(root):
    path = root / 'scripts/run-expanded-evaluation.py'
    spec = importlib.util.spec_from_file_location('trove_repository_evaluation', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def input_paths(root, bundle, models, go):
    paths = {root / name for name in HARNESS}
    for directory in SOURCE_ROOTS:
        for suffix in ('*.go', '*.sql', '*.json', '*.c', '*.h', '*.m', '*.mm', '*.metal'):
            paths.update((root / directory).rglob(suffix))
    paths.update((root / 'go.mod', root / 'go.sum', pathlib.Path(__file__).resolve(), go,
                  bundle / 'manifest.json', bundle / 'llama-worker',
                  models / (PACK + '.json'), models / (PACK + '.model')))
    # Bundle validation can consult other declared helpers even though only llama
    # is invoked by this pilot; bind the actual helper bytes, never an app root.
    manifest = json.loads((bundle / 'manifest.json').read_text())
    for helper in manifest['helpers']:
        name = helper.get('name')
        if not isinstance(name, str) or pathlib.Path(name).name != name or name in ('', '.', '..'):
            fail('unsafe helper filename in bundle manifest')
        paths.add(bundle / name)
    return sorted(paths)


def inventory(paths, validator):
    result = {}
    for path in paths:
        if not path.is_file() or path.is_symlink():
            fail('input is missing, non-regular, or a link: ' + str(path))
        result[str(path)] = {'sha256': validator.digest(path), 'bytes': path.stat().st_size}
    return result


def stable_identity(root, bundle, models, go, inputs):
    bindings = {'harness:' + name: inputs[str(root / name)] for name in HARNESS}
    for label, path in [('pilot', pathlib.Path(__file__).resolve()), ('go', go),
                        ('bundleManifest', bundle / 'manifest.json'), ('llama', bundle / 'llama-worker'),
                        ('modelManifest', models / (PACK + '.json')), ('model', models / (PACK + '.model'))]:
        bindings[label] = inputs[str(path)]
    return bindings


def ensure_source(root, source):
    if git(root, 'rev-parse', 'HEAD') != source:
        fail('checkout commit differs from explicit --source-commit')
    # PROGRESS-only changes are recorded but do not invalidate the committed
    # execution source. Untracked Go/SQL/contract files cannot silently join it.
    status = git(root, 'status', '--porcelain', '--untracked-files=all', '--',
                 *SOURCE_ROOTS, 'go.mod', 'go.sum', 'scripts/run-expanded-evaluation.py', 'scripts/run-answerability-repair.py', 'scripts/test-answerability-repair.py')
    if status:
        fail('commit relevant source/harness inputs before running: ' + status)


def run_process(command, cwd, env, log_path, timeout):
    with log_path.open('w') as log:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=log,
                                   stderr=subprocess.STDOUT, start_new_session=True)
        try:
            return process.wait(timeout=timeout), False
        except (subprocess.TimeoutExpired, KeyboardInterrupt):
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            return process.returncode, True


def read_events(path):
    if path.stat().st_size > 128 * 1024 * 1024:
        fail('event log exceeds bounded consumer size')
    events, errors = [], []
    with path.open() as stream:
        for number, line in enumerate(stream, 1):
            try:
                value = json.loads(line)
                if not isinstance(value, dict):
                    raise ValueError('event is not an object')
                events.append(value)
            except (ValueError, TypeError):
                errors.append('malformed event line ' + str(number))
    return events, errors


def gate_errors(events, original_ids):
    expected = set(GATES)
    for test in GATES:
        expected.update(test + '/' + case for case in (original_ids if test.startswith('TestAITransfer') else IDS))
    terminals, errors = {}, []
    for event in events:
        name, action = event.get('Test'), event.get('Action')
        if name and action in ('pass', 'fail', 'skip'):
            if name in terminals:
                errors.append('duplicate gate terminal: ' + name)
            terminals[name] = action
    if set(terminals) != expected:
        errors.append('golden/poison/scope gate inventory differs')
    errors.extend('gate did not pass: ' + name for name in expected if terminals.get(name) != 'pass')
    return errors


def extract_reports(events, validator):
    reports, buffers, errors = {}, {}, []
    for event in events:
        name = event.get('Test', '')
        if not name.startswith(TEST + '/') or event.get('Action') != 'output':
            continue
        buffers[name] = buffers.get(name, '') + event.get('Output', '')
        while '\n' in buffers[name]:
            line, buffers[name] = buffers[name].split('\n', 1)
            offset = line.find('{')
            if offset < 0:
                continue
            try:
                report = json.loads(line[offset:], object_pairs_hook=validator._unique_json)
            except (ValueError, TypeError):
                continue
            if not isinstance(report, dict) or 'fixture' not in report:
                continue
            case = report['fixture']
            if not isinstance(case, str) or case in reports or name != TEST + '/' + case:
                errors.append('duplicate or mismatched native task report')
                continue
            reports[case] = report
    if any(value.strip() for value in buffers.values()):
        errors.append('unterminated native report output')
    if set(reports) != set(IDS):
        errors.append('native report inventory differs from sealed twelve cases')
    return reports, errors


def native_terminals(events):
    expected = {TEST, *(TEST + '/' + case for case in IDS)}
    terminals, errors = {}, []
    for event in events:
        name, action = event.get('Test'), event.get('Action')
        if name and action in ('pass', 'fail', 'skip'):
            if name in terminals:
                errors.append('duplicate native terminal: ' + name)
            terminals[name] = action
    if set(terminals) != expected:
        errors.append('native terminal inventory differs from exact parent and twelve cases')
    if any(action == 'skip' for action in terminals.values()):
        errors.append('native skipped case cannot qualify as completed execution')
    return terminals, errors


def comparison_execution_errors(report):
    """Transport completion is separate from semantic/protocol quality failures."""
    errors = []
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != 1:
        errors.append('not exactly one native generation')
    if report.get('nativeCallbackCompleted') is not True or report.get('nativeCallbackError') != '<nil>':
        errors.append('native callback did not complete successfully')
    if report.get('nativeCompletionContract') != 'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos':
        errors.append('native completion contract missing')
    for field in ('prepareError', 'sourceScopeError', 'finishError'):
        if report.get(field) != '<nil>':
            errors.append('comparison execution failure: ' + field)
    grammar = report.get('generatorGrammar')
    if not isinstance(grammar, str) or not grammar or hashlib.sha256(grammar.encode()).hexdigest() != report.get('generatorGrammarSHA256'):
        errors.append('actual grammar bytes/hash differ')
    return errors


def validate_answer(report, case, validator):
    errors = comparison_execution_errors(report)
    request = report.get('request', {})
    for field, value in [('fixture', case['id']), ('mode', 'answer'), ('useMemory', False),
                         ('corpusSHA256', CORPUS_SHA256), ('modelPackId', PACK),
                         ('sourceTitle', case['title']), ('source', case['source']),
                         ('prompt', case['prompt']), ('selection', '')]:
        if type(report.get(field)) is not type(value) or report[field] != value:
            errors.append('report binding differs: ' + field)
    if not isinstance(request, dict):
        return errors + ['request is missing']
    for field, value in [('mode', 'answer'), ('prompt', case['prompt']), ('useMemory', False), ('modelPackId', PACK)]:
        if type(request.get(field)) is not type(value) or request[field] != value:
            errors.append('actual request differs: ' + field)
    if set(request) - {'mode', 'prompt', 'pageId', 'blockId', 'modelPackId', 'useMemory', 'selection', 'conversationId', 'projectId', 'connectedSources', 'skill'} or any(request.get(field) for field in ('selection', 'conversationId', 'projectId', 'connectedSources', 'skill')):
        errors.append('request expanded beyond selected canonical block')
    page, block = request.get('pageId'), request.get('blockId')
    if not isinstance(page, str) or not page or not isinstance(block, str) or not block:
        errors.append('selected canonical identities missing')
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != 1:
        errors.append('not exactly one native generation')
    if not all(type(report.get(field)) is int for field in ('promptTokens', 'outputCap', 'contextTokens', 'generatorOutputCap')) or report.get('promptTokens', -1) < 0 or report.get('outputCap') != 512 or report.get('contextTokens') != 8192 or report.get('generatorOutputCap') != 512 or report.get('promptTokens', 8192) + 512 + 16 > 8192:
        errors.append('actual token budget differs or is not bounded')
    if not report.get('preparedSystem') or not report.get('preparedPrompt') or report.get('generatorSystem') != report.get('preparedSystem') or report.get('generatorPrompt') != report.get('preparedPrompt') or not re.fullmatch('[0-9a-f]{64}', report.get('generatorGrammarSHA256', '')):
        errors.append('actual generation prompt/grammar binding missing')
    sources, primary = report.get('sources'), []
    if not isinstance(sources, list) or not sources:
        return errors + ['prepared evidence missing']
    numbers = set()
    for source in sources:
        location = source.get('location', {})
        number = source.get('number')
        if type(number) is not int or number <= 0 or number in numbers:
            errors.append('source citation identity is invalid or duplicated')
        numbers.add(number)
        if source.get('memoryId') or source.get('connected') or location.get('pageId') != page or source.get('title') != case['title'] or not location.get('sourceRevision'):
            errors.append('foreign/stale/unselected evidence identity')
        if location.get('blockId'):
            if location['blockId'] != block or source.get('text') != case['source']:
                errors.append('selected source content differs')
            primary.append(source)
        elif source.get('text') != case['title']:
            errors.append('page title source contains unselected body')
    if len(primary) != 1:
        errors.append('exactly one full selected source required')
    for field in ('prepareError', 'sourceScopeError', 'generationError', 'finishError', 'rubricError'):
        if report.get(field) != '<nil>':
            errors.append('native execution/oracle failure: ' + field)
    if report.get('executionPassed') is not True or report.get('boundedAnswerOracleApplied') is not True or report.get('passed') is not True or report.get('manualSemanticReviewPending') is not True or report.get('releaseQualified') is not False:
        errors.append('execution, bounded-oracle or manual-review boundary differs')
    try:
        draft = json.loads(report['rawDraft'], object_pairs_hook=validator._unique_json)
        if not isinstance(draft, dict) or set(draft) != {'title', 'claims'} or draft['title'] != case['answerOracle']['heading'] or not isinstance(draft['claims'], list):
            raise ValueError('strict answer shape/heading differs')
        claims = draft['claims']
        if any(not isinstance(claim, dict) or set(claim) != {'text', 'quote'} or not all(isinstance(v, str) for v in claim.values()) for claim in claims):
            raise ValueError('claim shape differs')
        if case['answerOracle']['abstain']:
            if claims != [{'text': 'The supplied evidence does not answer this question.', 'quote': ''}]:
                raise ValueError('absent answer is not exclusive abstention')
        else:
            facts = case['answerOracle']['facts']
            if len(claims) != len(facts):
                raise ValueError('supported answer claim count differs')
            for claim, fact in zip(claims, facts):
                if fact['support'] != 'source' or not re.search(fact['claimPattern'], claim['text']) or not claim['quote'] or claim['quote'] not in case['source'] or not re.search(fact['quotePattern'], claim['quote']):
                    raise ValueError('answer differs from unchanged independent claim/support oracle')
                citations = report.get('citations', [])
                if not any(citation.get('number') == source['number'] and citation.get('location') == source['location'] and citation.get('state') == 'current' for citation in citations for source in primary if claim['quote'] in source['text']):
                    raise ValueError('supported answer lacks current exact-source citation')
        if not isinstance(report.get('decodedBody'), str) or not report['decodedBody'] or not isinstance(report.get('displayed'), str) or not report['displayed']:
            raise ValueError('generated or displayed result missing')
    except (ValueError, TypeError, KeyError) as error:
        errors.append(str(error))
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('root', 'source-commit', 'go', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'output'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--phase', choices=('before', 'after'), required=True)
    parser.add_argument('--before-manifest')
    args = parser.parse_args()
    if platform.system() != 'Darwin' or not pathlib.Path('/usr/bin/sandbox-exec').is_file():
        parser.error('macOS sandbox-exec network denial is required')
    if (args.phase == 'after') != bool(args.before_manifest):
        parser.error('AFTER requires --before-manifest; BEFORE must not supply one')
    root, bundle, models, go = [pathlib.Path(value).resolve() for value in (args.root, args.bundle, args.models, args.go)]
    out = pathlib.Path(args.output).resolve()
    if out.is_relative_to(root):
        parser.error('evidence output must be outside the source checkout')
    ensure_source(root, args.source_commit)
    validator = load_repository_validator(root)
    if validator.digest(root / CORPUS) != CORPUS_SHA256 or validator.digest(root / ORIGINAL_CORPUS) != ORIGINAL_SHA256:
        parser.error('new or original sealed corpus identity differs')
    corpus = json.loads((root / CORPUS).read_text(), object_pairs_hook=validator._unique_json)
    cases = corpus['cases']
    if [case['id'] for case in cases] != IDS or corpus.get('family') != 'answerability_transfer_v1':
        parser.error('sealed case identity/order differs')
    original_ids = [case['id'] for case in json.loads((root / ORIGINAL_CORPUS).read_text())['cases']]
    free = validator.require_space([out.parent, pathlib.Path(tempfile.gettempdir())])
    paths = input_paths(root, bundle, models, go)
    before = inventory(paths, validator)
    bindings = stable_identity(root, bundle, models, go, before)
    model = json.loads((models / (PACK + '.json')).read_text())
    if model.get('id') != PACK or model.get('kind') != 'chat' or model.get('sha256') != bindings['model']['sha256'] or model.get('sizeBytes') != bindings['model']['bytes'] or model.get('contextTokens') != 8192:
        parser.error('actual model bytes differ from installed pack106 manifest')
    helpers = json.loads((bundle / 'manifest.json').read_text())['helpers']
    if len([h for h in helpers if h.get('name') == 'llama-worker' and h.get('sha256') == bindings['llama']['sha256'] and h.get('runtimeCommit') == model.get('runtimeCommit')]) != 1:
        parser.error('actual llama helper differs from declared runtime/model binding')
    comparison = None
    source_inputs = {str(path.relative_to(root)): before[str(path)] for path in paths if path.is_relative_to(root)}
    if args.before_manifest:
        prior_path = pathlib.Path(args.before_manifest).resolve()
        prior = json.loads(prior_path.read_text(), object_pairs_hook=validator._unique_json)
        if prior.get('schemaVersion') != 1 or prior.get('family') != 'answerability-transfer-v1' or prior.get('phase') != 'before' or prior.get('state') != 'finished' or prior.get('stableBindings') != bindings or prior.get('sourceCommit') == args.source_commit or prior.get('summary', {}).get('executionComplete') is not True or prior.get('summary', {}).get('inputsUnchanged') is not True or prior.get('goldenPoisonScopePassed') is not True:
            parser.error('BEFORE manifest lacks complete unchanged comparable execution')
        for name, sha in prior.get('outputs', {}).items():
            if pathlib.Path(name).name != name or validator.digest(prior_path.parent / name) != sha:
                parser.error('retained BEFORE output bytes differ')
        if set(prior.get('outputs', {})) != {'build.log', 'gates.jsonl', 'results.jsonl'}:
            parser.error('BEFORE output inventory differs')
        if prior.get('test') != TEST or prior.get('caseIds') != IDS or prior.get('corpusSHA256') != CORPUS_SHA256 or prior.get('modelPackId') != PACK:
            parser.error('BEFORE test/request identities differ')
        prior_events, prior_errors = read_events(prior_path.parent / 'results.jsonl')
        prior_reports, extraction_errors = extract_reports(prior_events, validator)
        prior_terminals, terminal_errors = native_terminals(prior_events)
        if prior_errors or extraction_errors or terminal_errors or prior_terminals.get(TEST) not in ('pass', 'fail') or any(comparison_execution_errors(prior_reports[case]) for case in IDS):
            parser.error('retained BEFORE raw reports do not prove twelve unskipped single generations')
        for case in IDS:
            error = validator.validate_canonical_inventory(prior_reports[case], prior_path.parent / 'canonical', TEST + '/' + case)
            if error:
                parser.error('retained BEFORE canonical evidence differs: ' + error)
        prior_source = prior.get('sourceInputs')
        if not isinstance(prior_source, dict) or not prior_source:
            parser.error('BEFORE source inventory missing')
        source_changes = {name: {'before': prior_source.get(name), 'after': source_inputs.get(name)} for name in sorted(set(prior_source) | set(source_inputs)) if prior_source.get(name) != source_inputs.get(name)}
        comparison = {'manifest': str(prior_path), 'sha256': validator.digest(prior_path), 'sourceCommit': prior['sourceCommit'], 'stableBindingsIdentical': True, 'retainedCanonicalRevalidated': True, 'sourceInputChanges': source_changes}
    out.mkdir(mode=0o700)
    reserve = out / '.evidence-reserve'
    reserve.write_bytes(bytes(1024 * 1024))
    manifest = {'schemaVersion': 1, 'family': 'answerability-transfer-v1', 'phase': args.phase,
                'sourceCommit': args.source_commit, 'workingTreeStatus': git(root, 'status', '--short'),
                'test': TEST, 'caseIds': IDS, 'caseCount': 12, 'generationAttemptsPerCase': 1,
                'corpusSHA256': CORPUS_SHA256, 'modelPackId': PACK, 'inputs': before,
                'stableBindings': bindings, 'sourceInputs': source_inputs, 'beforeComparison': comparison,
                'networkPolicy': NETWORK_POLICY, 'hardware': platform.platform(), 'initialFreeBytes': free,
                'state': 'started', 'startedAtUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'canonicalEvidenceDirectory': str(out / 'canonical'),
                'canonicalEvidenceScope': 'Actual repository typed canonical rows and exact asset bytes, excluding only reviewed derived operational tables.',
                'manualSemanticReviewPending': IDS, 'performanceQualified': False, 'releaseQualified': False}
    started = time.monotonic()
    write = lambda: validator.atomic_json(out / 'manifest.json', manifest)
    write()
    env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'SDKROOT', 'DEVELOPER_DIR') if key in os.environ}
    env.update(GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOMODCACHE=str(pathlib.Path(args.go_mod_cache).resolve()), GOCACHE=str(pathlib.Path(args.go_build_cache).resolve()))
    sandbox = ['/usr/bin/sandbox-exec', '-p', NETWORK_POLICY]
    binary = out / 'evaluation.test'
    build = [str(go), 'test', '-mod=readonly', '-tags', TAGS, '-c', '-o', str(binary), './internal/workspace']
    manifest['buildCommand'] = build
    write()
    code, timeout = run_process(sandbox + build, root, env, out / 'build.log', 900)
    if code or timeout:
        manifest.update(state='build_failed', exitCode=code, timedOut=timeout)
        write()
        return 1
    manifest['testBinarySHA256'] = validator.digest(binary)
    gate_env = dict(env, TROVE_AI_CANONICAL_EVIDENCE_DIR=str(out / 'canonical-gates'))
    gate = [str(go), 'tool', 'test2json', '-t', '-p', 'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^(' + '|'.join(GATES) + ')$', '-test.v', '-test.timeout=10m']
    manifest['gateCommand'] = gate
    write()
    code, timeout = run_process(sandbox + gate, root / 'internal/workspace', gate_env, out / 'gates.jsonl', 630)
    gate_events, gate_problems = read_events(out / 'gates.jsonl')
    gate_problems += gate_errors(gate_events, original_ids)
    manifest.update(goldenPoisonScopePassed=not code and not timeout and not gate_problems, gateErrors=gate_problems)
    if not manifest['goldenPoisonScopePassed']:
        manifest.update(state='gates_failed', exitCode=code, timedOut=timeout)
        write()
        return 1
    # Recheck source and all inputs after build/gates, before model admission.
    ensure_source(root, args.source_commit)
    if inventory(input_paths(root, bundle, models, go), validator) != before:
        fail('inputs changed before native admission')
    env.update(TROVE_NATIVE_INTEGRATION='1', TROVE_CHAT_EVAL_PACK=PACK, TROVE_MODEL_ROOT=str(models), TROVE_NATIVE_BUNDLE=str(bundle), TROVE_AI_CANONICAL_EVIDENCE_DIR=manifest['canonicalEvidenceDirectory'])
    command = [str(go), 'tool', 'test2json', '-t', '-p', 'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^' + TEST + '$', '-test.v', '-test.timeout=45m']
    manifest.update(state='native_running', runCommand=command)
    write()
    code, timeout = run_process(sandbox + command, root / 'internal/workspace', env, out / 'results.jsonl', 2730)
    reserve.unlink(missing_ok=True)
    events, malformed = read_events(out / 'results.jsonl')
    requests = {case: {'useMemory': False, 'modelPackId': PACK, 'corpusSHA256': CORPUS_SHA256} for case in IDS}
    summary = validator.summarize(events, IDS, TEST, out / 'canonical', requests)
    reports, report_errors = extract_reports(events, validator)
    checks = {}
    for case in cases:
        try:
            checks[case['id']] = validate_answer(reports.get(case['id'], {}), case, validator)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            checks[case['id']] = ['malformed native task report: ' + str(error)]
    summary['errors'] += malformed + report_errors
    summary['errors'] += [case + ': ' + problem for case, problems in checks.items() for problem in problems]
    terminal, terminal_errors = native_terminals(events)
    summary['errors'] += terminal_errors
    summary['executionComplete'] = not timeout and not malformed and not report_errors and not terminal_errors and set(reports) == set(IDS) and terminal.get(TEST) in ('pass', 'fail') and all(terminal.get(TEST + '/' + case) in ('pass', 'fail') and not comparison_execution_errors(reports[case]) for case in IDS)
    summary['inputsUnchanged'] = inventory(input_paths(root, bundle, models, go), validator) == before
    try:
        ensure_source(root, args.source_commit)
    except ValueError as error:
        summary['inputsUnchanged'] = False
        summary['errors'].append(str(error))
    summary['automatedCorpusPassed'] = summary['automatedCorpusPassed'] and terminal.get(TEST) == 'pass' and not code and not timeout and not summary['errors'] and summary['inputsUnchanged'] and summary['executionComplete']
    summary['manualSemanticReviewPending'] = IDS
    summary['boundedOracleOnly'] = True
    summary['releaseQualified'] = False
    manifest.update(state='finished', exitCode=code, timedOut=timeout, elapsedSeconds=time.monotonic() - started, summary=summary,
                    outputs={name: validator.digest(out / name) for name in ('build.log', 'gates.jsonl', 'results.jsonl')})
    write()
    print(json.dumps(summary, indent=2))
    return 0 if summary['automatedCorpusPassed'] else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, TypeError) as error:
        print('Pilot stopped without success: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
