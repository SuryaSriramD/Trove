#!/usr/bin/env python3
"""Run one predeclared local-model corpus; never counts skipped/missing cases as pass."""
import argparse, hashlib, json, os, pathlib, platform, re, shutil, subprocess, sys, tempfile, time

ROOT = pathlib.Path(__file__).resolve().parent.parent
FAMILIES = {
    'p0': ('p0-qa-memory.json', 'TestRealLocalExpandedP0QAMemory', 24),
    'p0-holdouts': ('p0-closure-holdouts.json', 'TestRealLocalP0ClosureHoldouts', 12),
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
    terminals, reports, errors, buffers = {}, {}, [], {}
    for event in events:
        name = event.get('Test', '')
        prefix = parent + '/'
        if name.startswith(prefix) and event.get('Action') in ('pass', 'fail', 'skip'):
            case = name[len(prefix):]
            if case in terminals:
                errors.append('duplicate terminal: ' + case)
            terminals[case] = event['Action']
        if not name.startswith(prefix) or event.get('Action') != 'output':
            continue
        # test2json splits long log lines across Output events (including inside
        # JSON strings). Reassemble per test before parsing; never join tests.
        buffers[name] = buffers.get(name, '') + event.get('Output', '')
        while '\n' in buffers[name]:
            output, buffers[name] = buffers[name].split('\n', 1)
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
    # Unterminated output never counts as a complete evidence record.
    for name, output in buffers.items():
        if output.strip():
            errors.append('unterminated output line: ' + name)
    missing = sorted(set(expected) - set(terminals))
    unexpected = sorted(set(terminals) - set(expected))
    for case in expected:
        r = reports.get(case, {})
        passed = r.get('passed', r.get('automatedTaskPassed')) is True
        if terminals.get(case) != 'pass' or not passed or r.get('canonicalUnchanged') is not True:
            errors.append('required case did not pass: ' + case)
    if set(reports) != set(expected):
        errors.append('task-report inventory differs from locked corpus')
    return {'reportedTaskPasses': sorted(k for k, v in reports.items() if v.get('passed', v.get('automatedTaskPassed')) is True),
            'reportedTaskFailures': sorted(k for k, v in reports.items() if v.get('passed', v.get('automatedTaskPassed')) is False),
            'expectedCases': len(expected), 'passedCases': sum(terminals.get(k) == 'pass' for k in expected),
            'missingCases': missing, 'unexpectedCases': unexpected, 'errors': errors,
            'automatedCorpusPassed': not missing and not unexpected and not errors,
            'manualSemanticReviewPending': sorted(k for k, v in reports.items() if v.get('manualSemanticReviewRequired')),
            'releaseQualified': False}

def atomic_json(path, value):
    """A full disk or failed rename must not destroy the previous manifest."""
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, prefix='.manifest-', delete=False) as stream:
            temporary = pathlib.Path(stream.name)
            stream.write(json.dumps(value, indent=2) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def require_space(paths, minimum=4 * 1024**3):
    free = {str(path): shutil.disk_usage(path).free for path in paths}
    if not free or min(free.values()) < minimum:
        raise ValueError('need at least 4 GiB free on evidence and temporary volumes; measured ' + json.dumps(free))
    return free


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ['go', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'pack', 'output']:
        p.add_argument('--' + name, required=True)
    p.add_argument('--family', choices=FAMILIES, required=True)
    p.add_argument('--case', action='append', dest='cases', help='explicit focused case; full-corpus pass remains false')
    args = p.parse_args()
    if platform.system() != 'Darwin':
        p.error('this runner requires macOS sandbox-exec; qualify other platform network denial separately')
    corpus_file, test, count = FAMILIES[args.family]
    corpus_dir = ROOT / 'internal/workspace/testdata/expanded-evaluation'
    corpus = json.loads((corpus_dir / corpus_file).read_text())
    expected = [c['id'] for c in corpus['cases']]
    if len(expected) != count or len(set(expected)) != count:
        p.error('locked corpus count/identity mismatch')
    if args.cases:
        if len(set(args.cases)) != len(args.cases) or not set(args.cases).issubset(expected):
            p.error('unknown or duplicate focused case')
        expected = [case for case in expected if case in args.cases]
    out = pathlib.Path(args.output).resolve()
    # Model runs need headroom for temporary workspaces and host memory pressure.
    # This is a conservative harness precondition, not a product storage target.
    try:
        free_space = require_space([out.parent, pathlib.Path(tempfile.gettempdir())])
    except (OSError, ValueError) as error:
        p.error(str(error))
    # Fresh directory only: a failed run is never replaced by a later attempt.
    out.mkdir(mode=0o700)
    reserve = out / '.evidence-reserve'
    reserve.write_bytes(bytes(1024 * 1024))
    bundle, models = pathlib.Path(args.bundle).resolve(), pathlib.Path(args.models).resolve()
    paths = [corpus_dir / corpus_file, ROOT / 'internal/workspace/ai_expanded_evaluation_test.go',
             ROOT / 'internal/workspace/ai_p0_closure_holdouts_test.go',
             ROOT / 'internal/workspace/ai_expanded_modes_test.go', pathlib.Path(__file__),
             bundle / 'manifest.json', bundle / 'llama-worker', models / (args.pack + '.json'),
             models / (args.pack + '.model')]
    # Bind all local production Go/SQL/schema inputs, not only the oracle files.
    paths += list((ROOT / 'internal').rglob('*.go')) + list((ROOT / 'contracts').rglob('*.go'))
    paths += list((ROOT / 'contracts').rglob('*.json')) + list((ROOT / 'internal').rglob('*.sql'))
    paths += [ROOT / 'go.mod', ROOT / 'go.sum']
    paths = sorted(set(paths))
    started = time.time()
    before = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    status = subprocess.check_output(['git', 'status', '--short'], cwd=ROOT, text=True)
    manifest = {'schemaVersion': 1, 'sourceCommit': source, 'workingTreeStatus': status,
                'family': args.family, 'inputs': before, 'hardware': platform.platform(),
                'networkPolicy': '(version 1)(allow default)(deny network*)',
                'performanceQualified': False, 'releaseQualified': False, 'state': 'started', 'initialFreeBytes': free_space}
    def write(): atomic_json(out / 'manifest.json', manifest)
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
    selector = '^' + test + '$'
    if args.cases:
        selector += '/^(' + '|'.join(re.escape(case) for case in expected) + ')$'
    command = [str(pathlib.Path(args.go).resolve()), 'tool', 'test2json', '-t', '-p',
               'trove.local/phase0/internal/workspace', str(binary), '-test.run', selector,
               '-test.v', '-test.timeout=150m']
    manifest['runCommand'] = command; write()
    with open(out / 'results.jsonl', 'w') as log:
        run = subprocess.run(sandbox + command, cwd=ROOT / 'internal/workspace', env=env,
                             stdout=log, stderr=subprocess.STDOUT)
    reserve.unlink(missing_ok=True)
    events, malformed = [], []
    for number, line in enumerate((out / 'results.jsonl').read_text().splitlines(), 1):
        try: events.append(json.loads(line))
        except json.JSONDecodeError: malformed.append(number)
    summary = summarize(events, expected, test)
    after = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    summary['inputsUnchanged'] = before == after
    summary['malformedLogLines'] = malformed
    summary['automatedCorpusPassed'] &= run.returncode == 0 and before == after and not malformed
    selection_passed = summary['automatedCorpusPassed']
    summary['automatedSelectionPassed'] = selection_passed
    summary['fullFamilyRun'] = len(expected) == count
    summary['automatedCorpusPassed'] &= summary['fullFamilyRun']
    manifest.update(state='finished', exitCode=run.returncode, elapsedSeconds=time.time()-started,
                    summary=summary, outputs={n: digest(out/n) for n in ['build.log', 'results.jsonl']})
    write()
    print(json.dumps(summary, indent=2))
    return 0 if selection_passed else 1

if __name__ == '__main__':
    raise SystemExit(main())
