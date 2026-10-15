#!/usr/bin/env python3
"""Explicit, serialized BEFORE/AFTER pilot for the sealed 16 mode controls.

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
CORPUS = 'internal/workspace/testdata/expanded-evaluation/independent-mode-controls-v1.json'
CORPUS_SHA256 = '9d23ba556d0cec8a7b54ab91fdf9039cc55d8931791d3023158ee6d23f15eed1'
ORIGINAL_CORPUS = 'internal/workspace/testdata/expanded-evaluation/heldout-modes.json'
ORIGINAL_SHA256 = '31f97f71107cc81e772a68d86d01acd6f5756ee68d2b9da46db6b936119ebf9a'
TEST = 'TestRealLocalIndependentModeControls'
PACK = '01990000-0000-7000-8000-000000000106'
TAGS = 'fts5 sqlite_omit_load_extension'
NETWORK_POLICY = '(version 1)(allow default)(deny network*)'
GATES = ('TestExpandedModeOracles', 'TestIndependentModeControlsGoldenAndPoisons', 'TestIndependentModeControlsScopeAndSelection')
HARNESS = (CORPUS, ORIGINAL_CORPUS, 'internal/workspace/ai_independent_mode_controls_test.go',
           'internal/workspace/ai_expanded_modes_test.go', 'internal/workspace/ai_canonical_inventory_test.go',
           'scripts/run-expanded-evaluation.py', 'scripts/evaluate-mode-control-mermaid.mjs',
           'scripts/review-independent-mode-controls.py', 'scripts/test-independent-mode-controls.py',
           'frontend/package.json', 'frontend/pnpm-lock.yaml')
SOURCE_ROOTS = ('internal', 'contracts', 'schemas')
IDS = ['mode-control-db-complete-title', 'mode-control-db-case-sensitive', 'mode-control-db-punctuation', 'mode-control-db-multiword-slash', 'mode-control-mermaid-linear', 'mode-control-mermaid-branch', 'mode-control-mermaid-case-distinct', 'mode-control-mermaid-sequence-type', 'mode-control-go-escaped-newline', 'mode-control-go-raw-newline', 'mode-control-go-partial-function', 'mode-control-go-literal-quotes-backslashes', 'mode-control-expand-supported-forbidden', 'mode-control-expand-supported-required-labeled', 'mode-control-expand-absent-forbidden', 'mode-control-expand-absent-required-labeled']
OUTPUTS = {'build.log', 'gates.jsonl', 'mermaid-gates.json', 'results.jsonl', 'native-reports.json', 'mermaid-results.json', 'manual-review-template.json', 'manual-preflight-template.json', 'manual-preflight-review.json', 'consumer-gates.txt'}


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


def parser_package_inputs(root):
    """Bind actual installed parser/dependency source, not only a package label."""
    first = (root / 'frontend/node_modules/mermaid').resolve()
    todo, seen, files = [first], set(), set()
    while todo:
        package = todo.pop()
        if package in seen: continue
        seen.add(package)
        if len(seen) > 1000: fail('parser dependency inventory exceeds bound')
        metadata = json.loads((package / 'package.json').read_text())
        if package == first and metadata.get('version') != '12.0.0': fail('actual Mermaid version differs from product pin')
        files.add(package / 'package.json')
        for suffix in ('*.js', '*.mjs', '*.cjs', '*.json', '*.wasm'):
            files.update(path for path in package.rglob(suffix) if 'node_modules' not in path.relative_to(package).parts)
        for dependency in metadata.get('dependencies', {}):
            candidates = [parent / 'node_modules' / dependency for parent in (package, *package.parents)]
            resolved = next((candidate.resolve() for candidate in candidates if (candidate / 'package.json').is_file()), None)
            if resolved is None: fail('installed parser dependency missing: ' + dependency)
            todo.append(resolved)
    return sorted(files)


def input_paths(root, bundle, models, go, node):
    paths = {root / name for name in HARNESS}
    for directory in SOURCE_ROOTS:
        for suffix in ('*.go', '*.sql', '*.json', '*.c', '*.h', '*.m', '*.mm', '*.metal'):
            paths.update((root / directory).rglob(suffix))
    paths.update((root / 'go.mod', root / 'go.sum', pathlib.Path(__file__).resolve(), go, node,
                  pathlib.Path(sys.executable).resolve(), bundle / 'manifest.json', bundle / 'llama-worker',
                  models / (PACK + '.json'), models / (PACK + '.model')))
    paths.update(parser_package_inputs(root))
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


def stable_identity(root, bundle, models, go, node, inputs):
    bindings = {'harness:' + name: inputs[str(root / name)] for name in HARNESS}
    for label, path in [('pilot', pathlib.Path(__file__).resolve()), ('go', go), ('node', node), ('python',pathlib.Path(sys.executable).resolve()),
                        ('bundleManifest', bundle / 'manifest.json'), ('llama', bundle / 'llama-worker'),
                        ('modelManifest', models / (PACK + '.json')), ('model', models / (PACK + '.model'))]:
        bindings[label] = inputs[str(path)]
    for parser_input in parser_package_inputs(root):
        bindings['parser:' + str(parser_input.relative_to(root / 'frontend/node_modules'))] = inputs[str(parser_input)]
    return bindings


def ensure_source(root, source):
    if git(root, 'rev-parse', 'HEAD') != source:
        fail('checkout commit differs from explicit --source-commit')
    # PROGRESS-only changes are recorded but do not invalidate the committed
    # execution source. Untracked Go/SQL/contract files cannot silently join it.
    status = git(root, 'status', '--porcelain', '--untracked-files=all', '--',
                 *SOURCE_ROOTS, 'go.mod', 'go.sum', 'scripts/run-expanded-evaluation.py', 'scripts/evaluate-mode-control-mermaid.mjs',
           'scripts/review-independent-mode-controls.py', 'scripts/test-independent-mode-controls.py', 'scripts/run-independent-mode-controls.py', 'scripts/review-independent-mode-controls.py', 'scripts/test-independent-mode-controls.py', 'frontend/package.json', 'frontend/pnpm-lock.yaml')
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
        expected.update(test + '/' + case for case in (original_ids if test == 'TestExpandedModeOracles' else IDS))
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
        errors.append('native report inventory differs from sealed sixteen cases')
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
        errors.append('native terminal inventory differs from exact parent and sixteen cases')
    if any(action == 'skip' for action in terminals.values()):
        errors.append('native skipped case cannot qualify as completed execution')
    return terminals, errors


def comparison_execution_errors(report):
    # A quality failure after actual generation remains comparable. A process,
    # installation, timeout or incomplete native callback is not a model baseline.
    errors=[]
    if type(report.get('generationAttempts')) is not int or report['generationAttempts']!=1: errors.append('exactly one native attempt required')
    if report.get('nativeCallbackCompleted') is not True or report.get('nativeCallbackError')!='<nil>': errors.append('native callback did not complete successfully')
    if report.get('nativeCompletionContract')!='GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos': errors.append('native completion contract missing')
    if not isinstance(report.get('rawDraft'),str): errors.append('native raw-output field missing')
    # Do not turn content quality or downstream decoding into transport success.
    # Conversely, do not reject a recorded successful callback merely by looking
    # for a preferred answer/nonempty substring; the native API owns completion.
    for key in ('prepareError','sourceScopeError','finishError'):
        if report.get(key)!='<nil>': errors.append('execution boundary failed: '+key)
    return errors


def validate_mode(report, case, validator):
    errors = comparison_execution_errors(report)
    request = report.get('request', {})
    bindings = {'fixture':case['id'], 'mode':case['mode'], 'useMemory':False, 'corpusSHA256':CORPUS_SHA256,
                'modelPackId':PACK, 'sourceTitle':case['title'], 'source':case['source'], 'prompt':case['prompt'], 'selection':case['selection']}
    for key, expected in bindings.items():
        if type(report.get(key)) is not type(expected) or report[key] != expected: errors.append('report binding differs: '+key)
    if not isinstance(request, dict): return errors+['actual request missing']
    if case['selection']:
        marker='\nSELECTED SOURCE (JSON string; replace only this source)\n'
        expected=json.dumps(case['selection'],ensure_ascii=False,separators=(',',':'))
        prepared=report.get('preparedPrompt','')
        if not isinstance(prepared,str) or prepared.count(marker)!=1 or not prepared.split(marker,1)[-1].startswith(expected): errors.append('exact JSON-encoded selected source differs')
    for key, expected in {'mode':case['mode'], 'prompt':case['prompt'], 'useMemory':False, 'modelPackId':PACK}.items():
        if type(request.get(key)) is not type(expected) or request[key] != expected: errors.append('actual request differs: '+key)
    if request.get('selection','') != case['selection'] or any(request.get(k) for k in ('conversationId','projectId','connectedSources','skill')):
        errors.append('request selection/scope differs')
    page, block = request.get('pageId'), request.get('blockId','')
    if not isinstance(page,str) or not page or bool(block) != bool(case['source']): errors.append('selected canonical IDs differ')
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != 1: errors.append('exactly one native generation required')
    if not all(type(report.get(k)) is int for k in ('promptTokens','outputCap','contextTokens','generatorOutputCap')) or report.get('promptTokens',-1)<0 or report.get('outputCap')!=512 or report.get('contextTokens')!=8192 or report.get('generatorOutputCap')!=512 or report.get('promptTokens',8192)+528>8192:
        errors.append('token budget not bound')
    if not report.get('preparedSystem') or not report.get('preparedPrompt') or report.get('generatorSystem')!=report.get('preparedSystem') or report.get('generatorPrompt')!=report.get('preparedPrompt') or not isinstance(report.get('generatorGrammar'),str) or report.get('generatorGrammarSHA256')!=hashlib.sha256(report.get('generatorGrammar','').encode()).hexdigest():
        errors.append('actual prompt/grammar binding missing')
    sources = report.get('sources')
    if not isinstance(sources,list): return errors+['source evidence missing']
    primary, numbers = [], set()
    for source in sources:
        location=source.get('location',{}); number=source.get('number')
        if type(number) is not int or number<=0 or number in numbers: errors.append('source citation identity differs')
        numbers.add(number)
        if source.get('memoryId') or source.get('connected') or location.get('pageId')!=page or source.get('title')!=case['title'] or not location.get('sourceRevision'): errors.append('unselected source identity')
        if location.get('blockId'):
            primary.append(source)
            if location['blockId']!=block or source.get('text')!=case['source']: errors.append('selected source bytes differ')
        elif source.get('text')!=case['title']: errors.append('title source contains other body')
    if len(primary)!=int(bool(case['source'])): errors.append('missing or extra selected block')
    for key in ('prepareError','sourceScopeError','generationError','finishError','structuralOracleError','proposalError'):
        if report.get(key)!='<nil>': errors.append('production/structural failure: '+key)
    if report.get('executionPassed') is not True or report.get('automatedTaskPassed') is not True or report.get('manualSemanticReviewRequired') is not True or report.get('semanticTaskPassed') is not None or report.get('releaseQualified') is not False:
        errors.append('execution or pending manual boundary differs')
    text=report.get('displayed')
    if not isinstance(text,str) or not text: return errors+['displayed output missing']
    if not isinstance(report.get('rawDraft'),str) or not report['rawDraft'] or not isinstance(report.get('decodedBody'),str) or not report['decodedBody']: errors.append('raw/decoded output missing')
    if case['mode']=='database':
        draft=json.loads(text,object_pairs_hook=validator._unique_json)
        if set(draft)!={'title','properties','view'} or draft['title']!=case['expectedTitle'] or draft['view']!='table' or sorted(draft['properties'],key=lambda p:p['name'])!=sorted(case['properties'],key=lambda p:p['name']): errors.append('exact database title/property oracle differs')
    if case['mode']=='code_comment':
        for literal in case['literalTokens']:
            if literal not in text: errors.append('original literal token missing')
    if case.get('proposal'):
        proposal=report.get('proposal')
        if not isinstance(proposal,dict) or proposal.get('state')!='draft' or proposal.get('kind')!=case['proposal'] or not proposal.get('id') or not proposal.get('commands'): errors.append('unapplied reviewed proposal missing')
        elif case['mode']=='code_comment':
            boundary=case.get('unselectedBoundary')
            expected=boundary['prefix']+text+boundary['suffix'] if boundary else text
            if proposal.get('before')!=case['source'] or proposal.get('after')!=expected or len(proposal['commands'])!=1: errors.append('selected proposal changed outside source')
            command=proposal['commands'][0]
            if command.get('kind')!='block.update' or command.get('blockId')!=block or command.get('type')!='code' or command.get('data',{}).get('source')!=expected or command.get('data',{}).get('language')!='go': errors.append('exact code candidate command differs')
    return errors


def manual_template(cases, reports=None):
    items=[]
    for case in cases:
        if reports is None:
            candidates=[{'label':'golden','output':case['golden'],'expectedAccept':True}]+[{'label':p['kind'],'output':p['output'],'expectedAccept':False,'reason':p['reason']} for p in case['counterexamples']]
        else:
            candidates=[{'label':'native','output':reports.get(case['id'],{}).get('displayed',''),'expectedAccept':None}]
        for candidate in candidates:
            required = reports is not None or case['mode']=='expand' or case['mode']=='code_comment' and candidate['label'] in ('golden','inaccurate_comment')
            items.append({'manualReviewRequired':required,'fixture':case['id'],'mode':case['mode'],'source':case['source'],'selection':case['selection'],'prompt':case['prompt'],'required':case['required'],'forbidden':case['forbidden'],'expectedFacts':case.get('expectedFacts'),'commentMeaning':case.get('commentMeaning'),**candidate,'outputSHA256':hashlib.sha256(candidate['output'].encode()).hexdigest(),'review':{'state':'pending','reviewer':None,'reviewedAtUTC':None,'allClausesAssessed':False,'accept':None,'clauseAssessments':[],'unsupportedOrMissingClauses':[],'rationale':None}})
    return {'schemaVersion':1,'corpusSHA256':CORPUS_SHA256,'automaticSemanticAcceptance':False,'instructions':'Only edit review for manualReviewRequired=true items (preflight: eight code/prose goldens and 24 semantic poisons; native: all sixteen outputs). Leave other reviews pending; actual Go and pinned-parser gates establish their bounded structural results separately. Identify agent review as agent review, keeping human acceptance pending. Assess every factual/comment clause against the exact source and request, including every modifier, absent fact and allowed/forbidden suggestion. AST, parsing, formatting and keyword success do not establish semantic fidelity. For each clause or unmet criterion record text, finding and supporting source/request evidence in clauseAssessments. A reviewer must attest complete coverage; software cannot independently infer that coverage. Keep expectedAccept unchanged. Review receipts bind exact output hashes and never authorize model retries or oracle changes.','items':items,'releaseQualified':False}


def validate_manual_review(receipt, template):
    # Only the per-item review may change. This binds all source, criteria,
    # expected decisions and exact output bytes as well as item order/inventory.
    errors=[]
    if not isinstance(receipt,dict) or set(receipt)!=set(template): return ['manual receipt envelope differs']
    for key in template:
        if key!='items' and receipt[key]!=template[key]: errors.append('manual receipt binding differs: '+key)
    items=receipt.get('items')
    if not isinstance(items,list) or len(items)!=len(template['items']): return errors+['manual receipt inventory differs']
    for actual, expected in zip(items,template['items']):
        name=expected['fixture']+'/'+expected['label']
        if not isinstance(actual,dict) or set(actual)!=set(expected) or {k:v for k,v in actual.items() if k!='review'}!={k:v for k,v in expected.items() if k!='review'}:
            errors.append(name+': exact review item differs'); continue
        review=actual.get('review')
        if expected['manualReviewRequired'] is False:
            if review!=expected['review']: errors.append(name+': automatic-only item review must remain pending')
            continue
        if not isinstance(review,dict) or set(review)!=set(expected['review']): errors.append(name+': review shape differs'); continue
        if review['state']!='completed' or review['allClausesAssessed'] is not True or type(review['accept']) is not bool or not isinstance(review['reviewer'],str) or not review['reviewer'].strip() or not isinstance(review['rationale'],str) or not review['rationale'].strip() or not isinstance(review['reviewedAtUTC'],str) or not re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ',review['reviewedAtUTC']):
            errors.append(name+': incomplete all-clause assessment')
        if expected['expectedAccept'] is not None and review['accept'] is not expected['expectedAccept']: errors.append(name+': sealed golden/poison decision differs')
        clauses=review['clauseAssessments']
        if not isinstance(clauses,list) or not clauses:
            errors.append(name+': clause assessments missing')
        else:
            for clause in clauses:
                if not isinstance(clause,dict) or set(clause)!={'text','finding','evidence'} or not all(isinstance(clause.get(k),str) and clause[k].strip() for k in ('text','finding','evidence')):
                    errors.append(name+': incomplete clause evidence')
        issues=review['unsupportedOrMissingClauses']
        if not isinstance(issues,list) or not all(isinstance(x,str) and x.strip() for x in issues) or (review['accept'] is True and issues) or (review['accept'] is False and not issues): errors.append(name+': acceptance/issues contradict')
    return errors


def parser_report_errors(report, cases, mode, entry_sha, reports=None):
    errors=[]
    if not isinstance(report,dict): return ['parser report missing']
    if report.get('schemaVersion')!=1 or report.get('corpusSHA256')!=CORPUS_SHA256 or report.get('parserVersion')!='12.0.0' or report.get('parserEntrySHA256')!=entry_sha or report.get('mode')!=mode or report.get('releaseQualified') is not False:
        errors.append('actual parser identity differs')
    expected=[]
    for case in cases:
        if case['mode']!='mermaid': continue
        if mode=='golden':
            expected.append((case,'golden',case['golden'],True))
            expected.extend((case,x['kind'],x['output'],False) for x in case['counterexamples'])
        else:
            expected.append((case,'native_displayed',(reports or {}).get(case['id'],{}).get('displayed'),True))
    actual=report.get('results')
    if not isinstance(actual,list) or len(actual)!=len(expected): return errors+['parser exact result inventory differs']
    for result,(case,label,source,accept) in zip(actual,expected):
        name=case['id']+'/'+label
        if not isinstance(result,dict) or result.get('fixture')!=case['id'] or result.get('label')!=label or result.get('source')!=source or result.get('expected') is not accept or type(result.get('accepted')) is not bool or result.get('passed') is not (result.get('accepted') is accept):
            errors.append(name+': parser input/outcome binding differs'); continue
        if result['accepted']:
            if result.get('error') is not None or not isinstance(result.get('projection'),dict): errors.append(name+': accepted parse lacks projection')
            else:
                projection=result['projection']; diagram=case['diagram']
                if diagram['kind']=='flowchart':
                    if projection.get('kind')!='flowchart' or projection.get('parserType') not in ('flowchart','flowchart-v2') or projection.get('direction','').replace('TB','TD')!=diagram['direction'] or sorted(projection.get('nodes',[]))!=sorted(diagram['nodes']) or sorted(json.dumps(x,sort_keys=True) for x in projection.get('edges',[]))!=sorted(json.dumps(x,sort_keys=True) for x in diagram['edges']): errors.append(name+': actual graph projection differs')
                elif projection.get('kind')!='sequenceDiagram' or projection.get('parserType')!='sequence' or projection.get('participants')!=diagram['participants'] or projection.get('messages')!=diagram['messages']: errors.append(name+': actual sequence projection differs')
        elif not isinstance(result.get('error'),str) or not result['error'] or result.get('projection') is not None: errors.append(name+': rejected parse lacks error')
        if result['passed'] is not True: errors.append(name+': actual parser structural check failed')
    expected_pass=all(isinstance(x,dict) and x.get('passed') is True for x in actual)
    if report.get('passed') is not expected_pass: errors.append('parser aggregate contradicts rows')
    return errors


def read_parser_report(path, validator):
    try:
        return json.loads(path.read_text(),object_pairs_hook=validator._unique_json),[]
    except (OSError,ValueError,TypeError) as error:
        return {},['parser environment/harness did not emit a complete JSON report: '+str(error)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('root', 'source-commit', 'go', 'node', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'output', 'preflight-review'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--phase', choices=('before', 'after'), required=True)
    parser.add_argument('--before-manifest')
    args = parser.parse_args()
    if platform.system() != 'Darwin' or not pathlib.Path('/usr/bin/sandbox-exec').is_file():
        parser.error('macOS sandbox-exec network denial is required')
    if (args.phase == 'after') != bool(args.before_manifest):
        parser.error('AFTER requires --before-manifest; BEFORE must not supply one')
    root, bundle, models, go, node = [pathlib.Path(value).resolve() for value in (args.root, args.bundle, args.models, args.go, args.node)]
    out = pathlib.Path(args.output).resolve()
    if out.is_relative_to(root):
        parser.error('evidence output must be outside the source checkout')
    if pathlib.Path(__file__).resolve()!=root/'scripts/run-independent-mode-controls.py': parser.error('run the committed harness from the explicit source checkout')
    ensure_source(root, args.source_commit)
    validator = load_repository_validator(root)
    if validator.digest(root / CORPUS) != CORPUS_SHA256 or validator.digest(root / ORIGINAL_CORPUS) != ORIGINAL_SHA256:
        parser.error('new or original sealed corpus identity differs')
    corpus = json.loads((root / CORPUS).read_text(), object_pairs_hook=validator._unique_json)
    cases = corpus['cases']
    if [case['id'] for case in cases] != IDS or corpus.get('family') != 'independent_mode_controls_v1':
        parser.error('sealed case identity/order differs')
    original_ids = [case['id'] for case in json.loads((root / ORIGINAL_CORPUS).read_text())['cases']]
    free = validator.require_space([out.parent, pathlib.Path(tempfile.gettempdir())])
    paths = input_paths(root, bundle, models, go, node)
    preflight = pathlib.Path(args.preflight_review).resolve()
    preflight_receipt = json.loads(preflight.read_text(),object_pairs_hook=validator._unique_json)
    preflight_errors = validate_manual_review(preflight_receipt,manual_template(cases))
    if preflight_errors: parser.error('Complete the sealed manual golden/poison review before native admission: '+str(preflight_errors))
    paths.append(preflight)
    before = inventory(paths, validator)
    bindings = stable_identity(root, bundle, models, go, node, before)
    bindings['manualPreflightReview'] = before[str(preflight)]
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
        if prior.get('schemaVersion') != 1 or prior.get('family') != 'independent-mode-controls-v1' or prior.get('phase') != 'before' or prior.get('state') != 'finished' or prior.get('stableBindings') != bindings or prior.get('sourceCommit') == args.source_commit or prior.get('summary', {}).get('executionComplete') is not True or prior.get('summary', {}).get('inputsUnchanged') is not True or prior.get('goldenPoisonScopePassed') is not True or prior.get('actualParserGoldenPoisonsPassed') is not True or prior.get('consumerChecksPassed') is not True or prior.get('manualPreflightPassed') is not True:
            parser.error('BEFORE manifest lacks complete unchanged comparable execution')
        for name, sha in prior.get('outputs', {}).items():
            if pathlib.Path(name).name != name or validator.digest(prior_path.parent / name) != sha:
                parser.error('retained BEFORE output bytes differ')
        if set(prior.get('outputs', {})) != OUTPUTS:
            parser.error('BEFORE output inventory differs')
        if prior.get('test') != TEST or prior.get('caseIds') != IDS or prior.get('corpusSHA256') != CORPUS_SHA256 or prior.get('modelPackId') != PACK:
            parser.error('BEFORE test/request identities differ')
        prior_events, prior_errors = read_events(prior_path.parent / 'results.jsonl')
        prior_reports, extraction_errors = extract_reports(prior_events, validator)
        prior_terminals, terminal_errors = native_terminals(prior_events)
        if prior_errors or extraction_errors or terminal_errors or prior_terminals.get(TEST) not in ('pass', 'fail') or any(comparison_execution_errors(prior_reports[case]) for case in IDS):
            parser.error('retained BEFORE raw reports do not prove sixteen unskipped successful native callbacks')
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
    manifest = {'schemaVersion': 1, 'family': 'independent-mode-controls-v1', 'phase': args.phase,
                'sourceCommit': args.source_commit, 'workingTreeStatus': git(root, 'status', '--short'),
                'test': TEST, 'caseIds': IDS, 'caseCount': 16, 'generationAttemptsPerCase': 1,
                'corpusSHA256': CORPUS_SHA256, 'modelPackId': PACK, 'inputs': before,
                'stableBindings': bindings, 'sourceInputs': source_inputs, 'beforeComparison': comparison,
                'networkPolicy': NETWORK_POLICY, 'hardware': platform.platform(), 'initialFreeBytes': free,
                'state': 'started', 'startedAtUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'canonicalEvidenceDirectory': str(out / 'canonical'),
                'canonicalEvidenceScope': 'Actual repository typed canonical rows and exact asset bytes, excluding only reviewed derived operational tables.',
                'manualPreflightPassed': True, 'manualPreflightReceipt': str(preflight), 'manualSemanticReviewPending': IDS, 'performanceQualified': False, 'releaseQualified': False}
    validator.atomic_json(out / 'manual-preflight-template.json', manual_template(cases))
    validator.atomic_json(out / 'manual-preflight-review.json', preflight_receipt)
    started = time.monotonic()
    write = lambda: validator.atomic_json(out / 'manifest.json', manifest)
    write()
    env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'SDKROOT', 'DEVELOPER_DIR') if key in os.environ}
    env.update(GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off', GOMODCACHE=str(pathlib.Path(args.go_mod_cache).resolve()), GOCACHE=str(pathlib.Path(args.go_build_cache).resolve()))
    sandbox = ['/usr/bin/sandbox-exec', '-p', NETWORK_POLICY]
    consumer_command = [sys.executable,str(root / 'scripts/test-independent-mode-controls.py')]
    manifest['consumerCheckCommand'] = consumer_command
    write()
    consumer_code, consumer_timeout = run_process(sandbox+consumer_command, root, env, out / 'consumer-gates.txt',120)
    manifest['consumerChecksPassed'] = consumer_code==0 and not consumer_timeout
    if not manifest['consumerChecksPassed']:
        manifest.update(state='consumer_gates_failed',exitCode=consumer_code,timedOut=consumer_timeout); write(); return 1
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
    parser_gate = [str(node), str(root / 'scripts/evaluate-mode-control-mermaid.mjs'), '--root', str(root), '--mode', 'golden']
    manifest['parserGateCommand'] = parser_gate
    write()
    parser_code, parser_timeout = run_process(sandbox + parser_gate, root, env, out / 'mermaid-gates.json', 120)
    parser_result, parser_problems = read_parser_report(out / 'mermaid-gates.json',validator)
    parser_entry = (root / 'frontend/node_modules/mermaid/dist/mermaid.core.mjs').resolve()
    parser_problems += parser_report_errors(parser_result,cases,'golden',before[str(parser_entry)]['sha256'])
    manifest['parserGateErrors'] = parser_problems
    manifest['actualParserGoldenPoisonsPassed'] = not parser_code and not parser_timeout and not parser_problems and parser_result.get('passed') is True
    if not manifest['actualParserGoldenPoisonsPassed']:
        manifest.update(state='parser_gates_failed', exitCode=parser_code, timedOut=parser_timeout)
        write()
        return 1
    # Recheck source and all inputs after build/gates, before model admission.
    ensure_source(root, args.source_commit)
    if inventory(input_paths(root, bundle, models, go, node)+[preflight], validator) != before:
        fail('inputs changed before native admission')
    env.update(TROVE_NATIVE_INTEGRATION='1', TROVE_CHAT_EVAL_PACK=PACK, TROVE_MODEL_ROOT=str(models), TROVE_NATIVE_BUNDLE=str(bundle), TROVE_AI_CANONICAL_EVIDENCE_DIR=manifest['canonicalEvidenceDirectory'])
    command = [str(go), 'tool', 'test2json', '-t', '-p', 'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^' + TEST + '$', '-test.v', '-test.timeout=55m']
    manifest.update(state='native_running', runCommand=command)
    write()
    code, timeout = run_process(sandbox + command, root / 'internal/workspace', env, out / 'results.jsonl', 3330)
    reserve.unlink(missing_ok=True)
    events, malformed = read_events(out / 'results.jsonl')
    requests = {case: {'useMemory': False, 'modelPackId': PACK, 'corpusSHA256': CORPUS_SHA256} for case in IDS}
    summary = validator.summarize(events, IDS, TEST, out / 'canonical', requests)
    reports, report_errors = extract_reports(events, validator)
    checks = {}
    for case in cases:
        try:
            checks[case['id']] = validate_mode(reports.get(case['id'], {}), case, validator)
        except (ValueError, TypeError, KeyError, AttributeError) as error:
            checks[case['id']] = ['malformed native task report: ' + str(error)]
    validator.atomic_json(out / 'native-reports.json', reports)
    validator.atomic_json(out / 'manual-review-template.json', manual_template(cases, reports))
    parser_command = [str(node), str(root / 'scripts/evaluate-mode-control-mermaid.mjs'), '--root', str(root), '--mode', 'outputs', '--input', str(out / 'native-reports.json')]
    parser_code, parser_timeout = run_process(sandbox + parser_command, root, env, out / 'mermaid-results.json', 120)
    parser_report, parser_problems = read_parser_report(out / 'mermaid-results.json',validator)
    parser_problems += parser_report_errors(parser_report,cases,'outputs',before[str(parser_entry)]['sha256'],reports)
    summary['errors'] += parser_problems
    summary['actualMermaidParserPassed'] = not parser_code and not parser_timeout and not parser_problems and parser_report.get('passed') is True
    if not summary['actualMermaidParserPassed']: summary['errors'].append('actual Mermaid semantic parser checks did not pass')
    summary['errors'] += malformed + report_errors
    summary['errors'] += [case + ': ' + problem for case, problems in checks.items() for problem in problems]
    terminal, terminal_errors = native_terminals(events)
    summary['errors'] += terminal_errors
    summary['nativeExecutionErrors'] = {case: comparison_execution_errors(reports.get(case,{})) for case in IDS}
    summary['canonicalExecutionErrors'] = {case: validator.validate_canonical_inventory(reports.get(case,{}),out/'canonical',TEST+'/'+case) for case in IDS}
    summary['executionComplete'] = not any(summary['canonicalExecutionErrors'].values()) and not any(summary['nativeExecutionErrors'].values()) and not timeout and not malformed and not report_errors and not terminal_errors and set(reports) == set(IDS) and terminal.get(TEST) in ('pass', 'fail') and all(terminal.get(TEST + '/' + case) in ('pass', 'fail') and type(reports[case].get('generationAttempts')) is int and reports[case]['generationAttempts'] == 1 for case in IDS)
    summary['inputsUnchanged'] = inventory(input_paths(root, bundle, models, go, node)+[preflight], validator) == before
    try:
        ensure_source(root, args.source_commit)
    except ValueError as error:
        summary['inputsUnchanged'] = False
        summary['errors'].append(str(error))
    summary['automatedCorpusPassed'] = summary['automatedCorpusPassed'] and terminal.get(TEST) == 'pass' and not code and not timeout and not summary['errors'] and summary['inputsUnchanged'] and summary['executionComplete']
    summary['manualSemanticReviewPending'] = IDS
    summary['automatedChecksPassed'] = summary['automatedCorpusPassed']
    summary['automatedCorpusPassed'] = False
    summary['semanticCorpusState'] = 'manual_review_pending'
    summary['boundedOracleOnly'] = True
    summary['releaseQualified'] = False
    manifest.update(state='finished', exitCode=code, timedOut=timeout, elapsedSeconds=time.monotonic() - started, summary=summary,
                    outputs={name: validator.digest(out / name) for name in sorted(OUTPUTS)})
    write()
    print(json.dumps(summary, indent=2))
    return 0 if summary['automatedChecksPassed'] else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError, TypeError) as error:
        print('Pilot stopped without success: ' + str(error), file=sys.stderr)
        raise SystemExit(1)
