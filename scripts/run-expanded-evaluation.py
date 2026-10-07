#!/usr/bin/env python3
"""Run one predeclared local-model corpus; never counts skipped/missing cases as pass."""
import argparse, hashlib, json, os, pathlib, platform, subprocess, sys, time

ROOT = pathlib.Path(__file__).resolve().parent.parent
FAMILIES = {
    'p0': ('p0-qa-memory.json', 'TestRealLocalExpandedP0QAMemory', 24),
    'modes': ('heldout-modes.json', 'TestRealLocalExpandedModes', 42),
}

def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def summarize(events, expected, parent):
    """Require exactly the independently declared cases and their task reports."""
    terminals, reports, errors = {}, {}, []
    for event in events:
        name = event.get('Test', '')
        prefix = parent + '/'
        if name.startswith(prefix) and event.get('Action') in ('pass', 'fail', 'skip'):
            case = name[len(prefix):]
            if case in terminals:
                errors.append('duplicate terminal: ' + case)
            terminals[case] = event['Action']
        if name.startswith(prefix) and event.get('Action') == 'output':
            output = event.get('Output', '')
            start = output.find('{')
            if start < 0:
                continue
            try:
                report = json.loads(output[start:])
            except json.JSONDecodeError:
                continue
            if 'fixture' not in report:
                continue
            case = report['fixture']
            if case in reports:
                errors.append('duplicate report: ' + case)
            if name != prefix + case:
                errors.append('report/test mismatch: ' + case)
            reports[case] = report
    missing = sorted(set(expected) - set(terminals))
    unexpected = sorted(set(terminals) - set(expected))
    for case in expected:
        r = reports.get(case, {})
        passed = r.get('passed', r.get('automatedTaskPassed')) is True
        if terminals.get(case) != 'pass' or not passed or r.get('canonicalUnchanged') is not True:
            errors.append('required case did not pass: ' + case)
    if set(reports) != set(expected):
        errors.append('task-report inventory differs from locked corpus')
    return {'expectedCases': len(expected), 'passedCases': sum(terminals.get(k) == 'pass' for k in expected),
            'missingCases': missing, 'unexpectedCases': unexpected, 'errors': errors,
            'automatedCorpusPassed': not missing and not unexpected and not errors,
            'manualSemanticReviewPending': sorted(k for k, v in reports.items() if v.get('manualSemanticReviewRequired')),
            'releaseQualified': False}

def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ['go', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'pack', 'output']:
        p.add_argument('--' + name, required=True)
    p.add_argument('--family', choices=FAMILIES, required=True)
    args = p.parse_args()
    if platform.system() != 'Darwin':
        p.error('this runner requires macOS sandbox-exec; qualify other platform network denial separately')
    corpus_file, test, count = FAMILIES[args.family]
    corpus_dir = ROOT / 'internal/workspace/testdata/expanded-evaluation'
    corpus = json.loads((corpus_dir / corpus_file).read_text())
    expected = [c['id'] for c in corpus['cases']]
    if len(expected) != count or len(set(expected)) != count:
        p.error('locked corpus count/identity mismatch')
    out = pathlib.Path(args.output).resolve()
    # Fresh directory only: a failed run is never replaced by a later attempt.
    out.mkdir(mode=0o700)
    bundle, models = pathlib.Path(args.bundle).resolve(), pathlib.Path(args.models).resolve()
    paths = [corpus_dir / corpus_file, ROOT / 'internal/workspace/ai_expanded_evaluation_test.go',
             ROOT / 'internal/workspace/ai_expanded_modes_test.go', pathlib.Path(__file__),
             bundle / 'manifest.json', bundle / 'llama-worker', models / (args.pack + '.json'),
             models / (args.pack + '.model')]
    started = time.time()
    before = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    status = subprocess.check_output(['git', 'status', '--short'], cwd=ROOT, text=True)
    manifest = {'schemaVersion': 1, 'sourceCommit': source, 'workingTreeStatus': status,
                'family': args.family, 'inputs': before, 'hardware': platform.platform(),
                'networkPolicy': '(version 1)(allow default)(deny network*)',
                'performanceQualified': False, 'releaseQualified': False, 'state': 'started'}
    def write(): (out / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    write()
    # Deliberately omit ambient provider/API credentials. No account is involved.
    env = {k: os.environ[k] for k in ['PATH', 'HOME', 'TMPDIR', 'SDKROOT', 'DEVELOPER_DIR'] if k in os.environ}
    env.update({'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off',
                'GOMODCACHE': str(pathlib.Path(args.go_mod_cache).resolve()),
                'GOCACHE': str(pathlib.Path(args.go_build_cache).resolve())})
    sandbox = ['/usr/bin/sandbox-exec', '-p', manifest['networkPolicy']]
    binary = out / 'evaluation.test'
    build = [str(pathlib.Path(args.go).resolve()), 'test', '-mod=readonly', '-tags',
             'fts5 sqlite_omit_load_extension', '-c', '-o', str(binary), './internal/workspace']
    manifest['buildCommand'] = build
    with open(out / 'build.log', 'w') as log:
        build_result = subprocess.run(sandbox + build, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    if build_result.returncode:
        manifest.update(state='build_failed', exitCode=build_result.returncode); write(); return 1
    manifest['testBinarySHA256'] = digest(binary)
    env.update(TROVE_NATIVE_INTEGRATION='1', TROVE_CHAT_EVAL_PACK=args.pack,
               TROVE_MODEL_ROOT=str(models), TROVE_NATIVE_BUNDLE=str(bundle))
    command = [str(pathlib.Path(args.go).resolve()), 'tool', 'test2json', '-t', '-p',
               'trove.local/phase0/internal/workspace', str(binary), '-test.run', '^' + test + '$',
               '-test.v', '-test.timeout=150m']
    manifest['runCommand'] = command; write()
    with open(out / 'results.jsonl', 'w') as log:
        run = subprocess.run(sandbox + command, cwd=ROOT / 'internal/workspace', env=env,
                             stdout=log, stderr=subprocess.STDOUT)
    events, malformed = [], []
    for number, line in enumerate((out / 'results.jsonl').read_text().splitlines(), 1):
        try: events.append(json.loads(line))
        except json.JSONDecodeError: malformed.append(number)
    summary = summarize(events, expected, test)
    after = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    summary['inputsUnchanged'] = before == after
    summary['malformedLogLines'] = malformed
    summary['automatedCorpusPassed'] &= run.returncode == 0 and before == after and not malformed
    manifest.update(state='finished', exitCode=run.returncode, elapsedSeconds=time.time()-started,
                    summary=summary, outputs={n: digest(out/n) for n in ['build.log', 'results.jsonl']})
    write()
    print(json.dumps(summary, indent=2))
    return 0 if summary['automatedCorpusPassed'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
