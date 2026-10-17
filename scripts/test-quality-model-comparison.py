#!/usr/bin/env python3
"""Synthetic counterexamples only; never substitute for native acceptance."""
import copy
import hashlib
import importlib.util
import json
import pathlib
import os
import signal
import tempfile
from types import SimpleNamespace
from unittest import mock
import sys
import unittest
sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]

def module(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    result = importlib.util.module_from_spec(spec); spec.loader.exec_module(result)
    return result

runner = module('comparison', 'scripts/run-quality-model-comparison.py')
modes = module('mode_controls', 'scripts/run-independent-mode-controls.py')
validator = modes.load_repository_validator(ROOT)
CASES = json.loads((ROOT / runner.REVIEW_CORPUS).read_text())['cases']


def synthetic(family='modes'):
    cap = 512 if family == 'modes' else 1024
    return dict(generationAttempts=1, nativeCallbackCompleted=True, nativeCallbackError='<nil>',
                generatorSystem='system', generatorPrompt='unchanged request', generatorGrammar='grammar',
                generatorGrammarSHA256=hashlib.sha256(b'grammar').hexdigest(), generatorOutputCap=cap,
                preparedSystem='system', preparedPrompt='unchanged request', rawDraft='wrong answer',
                promptTokens=100, outputCap=cap, contextTokens=8192, peakWorkerRSSBytes=2048,
                prepareError='<nil>', sourceScopeError='<nil>', finishError='<nil>',
                generationError='<nil>', generatorInvocations=1,
                nativeCompletionContract='GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos',
                assertions=dict(sourceScope=True, reservedBudget=True, canonicalPreserved=True))


class ModelComparisonGuards(unittest.TestCase):
    def test_only_exact_pinned_manifests_and_model_bytes(self):
        for pack, pin in runner.PINS.items():
            manifest = dict(id=pack,sha256=pin['sha256'],sizeBytes=pin['bytes'],kind='chat',format='gguf',contextTokens=8192,runtimeCommit=runner.RUNTIME,estimatedMemoryBytes=pin['estimatedMemoryBytes'])
            mb = dict(sha256=pin['manifestSHA256'],bytes=pin['manifestBytes'])
            model = dict(sha256=pin['sha256'],bytes=pin['bytes'])
            runner.validate_pack(pack, manifest, mb, model)
            for key,value in [('estimatedMemoryBytes',1),('runtimeCommit','other'),('contextTokens',16384),('id','unknown')]:
                with self.assertRaises(ValueError): runner.validate_pack(pack,{**manifest,key:value},mb,model)
            for changed in ({**mb,'sha256':'0'*64},{**mb,'bytes':1}):
                with self.assertRaises(ValueError): runner.validate_pack(pack,manifest,changed,model)
            for changed in ({**model,'sha256':'0'*64},{**model,'bytes':1}):
                with self.assertRaises(ValueError): runner.validate_pack(pack,manifest,mb,changed)
        for pack in ('',modes.PACK,'arbitrary','01990000-0000-7000-8000-000000000107'):
            with self.assertRaises(ValueError): runner.validate_pack(pack,{}, {}, {})

    def test_exact_harness_delta_cannot_authorize_production_or_arbitrary_source(self):
        before = {p:dict(sha256='a'*64,bytes=1) for p in runner.ALLOWED}
        current = {p:dict(sha256='b'*64,bytes=2) for p in runner.ALLOWED}
        review = dict(schemaVersion=1,baselineCommit=runner.BASELINE,sourceDeltas={p:dict(before=before[p],after=current[p]) for p in before})
        self.assertEqual(set(runner.reviewed_delta(before,current,review)),runner.ALLOWED)
        for changed in ({**current,'internal/workspace/ai.go':dict(sha256='x',bytes=1)}, {k:v for k,v in current.items() if k!=sorted(current)[0]}):
            with self.assertRaises(ValueError): runner.reviewed_delta(before,changed,review)
        changed=copy.deepcopy(current);changed[sorted(current)[0]]['sha256']='c'*64
        with self.assertRaises(ValueError): runner.reviewed_delta(before,changed,review)
        changed_review=copy.deepcopy(review);changed_review['sourceDeltas']['internal/workspace/ai.go']=dict(before=None,after=dict(sha256='x',bytes=1))
        with self.assertRaises(ValueError): runner.reviewed_delta(before,{**current,'internal/workspace/ai.go':dict(sha256='x',bytes=1)},changed_review)
        with self.assertRaises(ValueError): runner.reviewed_delta(before,current,{**review,'baselineCommit':'other'})

    def test_actual_callback_completion_budget_and_preparation_are_required(self):
        for family in ('modes','reviews-transfer'):
            base=synthetic(family);self.assertEqual(runner.execution_errors(base,family,base),[])
            for key,value in [('generationAttempts',0),('generationAttempts',2),('generationAttempts',True),('nativeCallbackCompleted',False),('nativeCallbackError','OUTPUT_LIMIT'),('promptTokens',8192),('peakWorkerRSSBytes',0),('peakWorkerRSSBytes',runner.LIMIT+1),('generatorOutputCap',32),('generatorGrammarSHA256','unbound'),('generatorSystem',''),('rawDraft','')]:
                self.assertTrue(runner.execution_errors({**base,key:value},family,base),(family,key))
        self.assertTrue(runner.execution_errors({**synthetic(),'preparedPrompt':'other'},'modes'))
        self.assertTrue(runner.execution_errors({**synthetic('reviews-transfer'),'generatorInvocations':2},'reviews-transfer'))
        for key in ('sourceScope','reservedBudget','canonicalPreserved'):
            changed=synthetic('reviews-transfer');changed['assertions'][key]=False
            self.assertTrue(runner.execution_errors(changed,'reviews-transfer'))

    def test_quality_failure_is_retained_without_becoming_execution_or_semantic_pass(self):
        report=synthetic();report.update(generationError='decode failed',structuralOracleError='wrong fact',automatedTaskPassed=False)
        self.assertEqual(runner.execution_errors(report,'modes',synthetic()),[])
        self.assertFalse(report['automatedTaskPassed'])

    def test_any_model_specific_prompt_or_grammar_change_breaks_parity(self):
        for family in ('modes','reviews-transfer'):
            base=synthetic(family)
            for key in ('generatorSystem','generatorPrompt','generatorGrammar','generatorGrammarSHA256','generatorOutputCap','outputCap','contextTokens'):
                changed=copy.deepcopy(base);changed[key]=str(changed[key])+' '
                self.assertEqual(runner.input_parity(changed,base),[key])
                self.assertTrue(runner.execution_errors(changed,family,base))

    def test_historical_helpers_still_default_to_locked106_and_original_entrypoint(self):
        case=json.loads((ROOT/modes.CORPUS).read_text())['cases'][0]
        report=synthetic();report.update(fixture=case['id'],mode=case['mode'],useMemory=False,corpusSHA256=modes.CORPUS_SHA256,modelPackId=next(iter(runner.PINS)),sourceTitle=case['title'],source=case['source'],prompt=case['prompt'],selection=case['selection'],request=dict(mode=case['mode'],prompt=case['prompt'],useMemory=False,modelPackId=next(iter(runner.PINS))))
        old=modes.validate_mode(report,case,validator)
        new=modes.validate_mode(report,case,validator,report['modelPackId'])
        self.assertIn('report binding differs: modelPackId',old)
        self.assertIn('actual request differs: modelPackId',old)
        self.assertNotIn('report binding differs: modelPackId',new)
        self.assertNotIn('actual request differs: modelPackId',new)
        test=runner.TESTS['reviews-transfer'];ids=[c['id'] for c in CASES]
        rows=[dict(Test=test,Action='pass')]+[dict(Test=test+'/'+case,Action='pass') for case in ids]
        self.assertFalse(modes.native_terminals(rows,test,ids)[1])
        self.assertTrue(modes.native_terminals(rows)[1])
        self.assertTrue(modes.native_terminals(rows+rows[-1:],test,ids)[1])
        rows[-1]['Action']='skip';self.assertTrue(modes.native_terminals(rows,test,ids)[1])

    def test_generic_report_reader_rejects_duplicate_wrong_or_missing_cases(self):
        test=runner.TESTS['reviews-transfer'];ids=[c['id'] for c in CASES]
        events=[dict(Test=test+'/'+case,Action='output',Output='test.go:1: '+json.dumps(dict(fixture=case))+'\n') for case in ids]
        self.assertFalse(modes.extract_reports(events,validator,test,ids)[1])
        self.assertTrue(modes.extract_reports(events[:-1],validator,test,ids)[1])
        self.assertTrue(modes.extract_reports(events+events[-1:],validator,test,ids)[1])
        events[-1]['Test']=test+'/unknown';self.assertTrue(modes.extract_reports(events,validator,test,ids)[1])

    def test_all54_review_golden_poison_scope_terminals_required(self):
        a,b='TestReviewTransferSealedGoldensAndPoisons','TestReviewTransferCapturedScopeAndDrafts'
        names={a,b}
        for case in CASES:
            names.update((a+'/'+case['id'],b+'/'+case['id']))
            names.update(a+'/'+case['id']+'/'+name for name in {'duplicate-key','unknown-field','trailing-object','missing-claims','extra-claim','unapproved-action','unselected-quote'} | {p['kind'] for p in case['poisons']})
        self.assertEqual(len(names),54)
        events=[dict(Test=n,Action='pass') for n in sorted(names)]
        self.assertFalse(runner.review_gate_errors(events,CASES))
        self.assertTrue(runner.review_gate_errors(events[:-1],CASES))
        self.assertTrue(runner.review_gate_errors(events+events[-1:],CASES))
        events[-1]['Action']='skip';self.assertTrue(runner.review_gate_errors(events,CASES))


class ControlledComparisonProcess(unittest.TestCase):
    """All children/groups/disks are fakes; these cases invoke no compiler/model."""
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='trove-comparison-guard-')
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.out = self.root / 'output'; self.out.mkdir(mode=0o700)
        self.cache = self.root / 'go-cache'; self.cache.mkdir()
        self.modules = self.root / 'go-mod'; self.modules.mkdir()
        disk = mock.patch.object(runner.shutil, 'disk_usage', return_value=SimpleNamespace(free=8*1024**3))
        self.disk = disk.start(); self.addCleanup(disk.stop)
        self.manifest = dict(state='preflight', family='modes', sourceCommit=runner.BASELINE)
        self.guard = runner.ControlledRun(self.out, self.root, runner.BASELINE, self.cache, self.modules, self.manifest)
        self.command = ['/usr/bin/true']
        self.cleanup = dict(pid=987654, processGroup=987654, signals=[], groupGone=True, processReaped=True, errors=[])

    def receipts(self):
        return [json.loads(p.read_text()) for p in sorted(self.guard.receipts.glob('*.json'))]

    def fake_child(self, poll=None):
        child = mock.Mock(pid=987654)
        child.poll.return_value = poll
        child.wait.return_value = 0 if poll is None else poll
        return child

    def bind_source(self):
        path = self.root / 'input'; path.write_text('fixed input')
        self.guard.paths = [path]
        self.guard.inputs = {str(path): dict(sha256=hashlib.sha256(path.read_bytes()).hexdigest(), bytes=path.stat().st_size)}
        self.guard.inputs_complete = True
        baseline=self.root/'baseline'; baseline.write_text('retained original evidence')
        self.guard.baseline_paths=[baseline]
        self.guard.baseline_inputs={str(baseline):dict(sha256=hashlib.sha256(baseline.read_bytes()).hexdigest(),bytes=baseline.stat().st_size)}
        self.guard.baseline_complete=True
        return path

    def test_baseline_manifests_outputs_oracle_and_canonical_bytes_are_separately_bound(self):
        base=self.root/'baseline-evidence'; base.mkdir()
        canonical=base/'canonical'; canonical.mkdir()
        manifest_path=base/'manifest.json'; manifest_path.write_text('{}')
        log=base/'results.jsonl'; log.write_text('retained raw output')
        oracle=base/'oracle-gates.log'; oracle.write_text('retained oracle')
        before=canonical/'before.json'; before.write_text('before canonical bytes')
        after=canonical/'after.json'; after.write_text('after canonical bytes')
        hash_file=lambda p:dict(sha256=hashlib.sha256(p.read_bytes()).hexdigest(),bytes=p.stat().st_size)
        manifest=dict(outputs={'results.jsonl':hash_file(log)['sha256']},sealedOracleScopeGates={'sha256':hash_file(oracle)['sha256']})
        report={'canonicalInventory':{phase:dict(path=str(p),sha256=hash_file(p)['sha256'],size=p.stat().st_size) for phase,p in [('before',before),('after',after)]}}
        expected={str(p) for p in (manifest_path,log,oracle,before,after)}
        args=(manifest_path,manifest,hash_file(manifest_path),{'one':report},'reviews-transfer',SimpleNamespace(digest=lambda p:hash_file(p)['sha256']))
        self.assertEqual(set(runner.retained_evidence_bindings(*args)),expected)
        for path in (manifest_path,log,oracle,before,after):
            raw=path.read_bytes(); path.write_bytes(raw+b' changed')
            with self.assertRaises(ValueError): runner.retained_evidence_bindings(*args)
            path.write_bytes(raw)

    def test_baseline_only_mutation_fails_final_binding_without_changing_source_inventory(self):
        self.bind_source(); original=copy.deepcopy(self.guard.inputs)
        self.guard.baseline_paths[0].write_text('changed retained output')
        with mock.patch.object(self.guard,'git',side_effect=[runner.BASELINE,'']):
            with self.guard: self.manifest['state']='consumer_failed'
        final=json.loads((self.out/'final-input-bindings.json').read_text())
        self.assertEqual(final['inputs'],original)
        self.assertFalse(final['baselineInputsUnchanged']); self.assertFalse(final['inputsUnchanged'])
        self.assertFalse(json.loads((self.out/'terminal-accounting.json').read_text())['executionComplete'])

    def test_private_allowlist_rejects_ambient_state_and_declares_only_shared_build_caches(self):
        with mock.patch.dict(os.environ, {'HOME':'/ambient-home', 'TMPDIR':'/ambient-tmp', 'PATH':'/ambient-bin',
                                         'OPENAI_API_KEY':'test-only-secret', 'TROVE_NATIVE_INTEGRATION':'1',
                                         'PYTHONPATH':'/ambient-python', 'HTTP_PROXY':'http://ambient'}, clear=False):
            extra = self.root / 'extra'; extra.mkdir(mode=0o700)
            env, receipt = runner.private_child_environment(extra, self.cache, self.modules)
        self.assertEqual(env['PATH'], runner.FIXED_PATH)
        self.assertEqual(receipt['sharedBuildCacheExceptions'], dict(GOCACHE=str(self.cache), GOMODCACHE=str(self.modules)))
        self.assertFalse(receipt['parentEnvironmentInherited'])
        self.assertEqual(env['CC'], '/usr/bin/clang')
        self.assertEqual(env['GIT_CONFIG_GLOBAL'], '/dev/null')
        for name in ('OPENAI_API_KEY', 'TROVE_NATIVE_INTEGRATION', 'PYTHONPATH', 'HTTP_PROXY'): self.assertNotIn(name, env)
        for name in ('HOME','TMPDIR','XDG_CONFIG_HOME','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_STATE_HOME','TROVE_WORKSPACE'):
            self.assertTrue(pathlib.Path(env[name]).is_relative_to(extra))
        runner.validate_child_environment(env, receipt)
        with self.assertRaises(FileExistsError): runner.private_child_environment(extra, self.cache, self.modules)
        for mutation in (dict(env, HOME='/ambient-home'), dict(env, PATH='/ambient-bin'), dict(env, OPENAI_API_KEY='forbidden')):
            with self.assertRaises(ValueError): runner.validate_child_environment(mutation, receipt)

    def test_runtime_directory_permissions_and_symlinks_are_rejected(self):
        home = pathlib.Path(self.guard.env['HOME'])
        home.chmod(0o755)
        with self.assertRaises(ValueError): runner.validate_child_environment(self.guard.env, self.guard.env_receipt)
        home.chmod(0o700)
        config = pathlib.Path(self.guard.env['XDG_CONFIG_HOME'])
        config.rmdir(); config.symlink_to(home, target_is_directory=True)
        with self.assertRaises(ValueError): runner.validate_child_environment(self.guard.env, self.guard.env_receipt)

    def test_absolute_executable_and_prelaunch_reserve_failure_never_start_a_child(self):
        with mock.patch.object(runner.subprocess, 'Popen') as popen:
            with self.assertRaises(ValueError): self.guard.run(['true'], self.root, self.guard.env, self.out/'relative.log', 1)
            self.disk.return_value.free = runner.MINIMUM_FREE - 1
            with self.assertRaises(runner.ComparisonStopped) as error:
                self.guard.run(self.command, self.root, self.guard.env, self.out/'reserve.log', 1)
            self.assertEqual(error.exception.reason, 'low_space')
            popen.assert_not_called()
        rows = self.receipts()
        self.assertEqual([r['state'] for r in rows], ['exception','low_space'])
        self.assertTrue(all(r['cleanup'] is None and r['retryCount']==0 for r in rows))
        self.assertTrue(all(value==runner.MINIMUM_FREE-1 for value in rows[-1]['minimumObservedFreeBytes'].values()))

    def test_during_run_reserve_failure_stops_only_owned_group_and_retains_log(self):
        child = self.fake_child()
        values = iter([SimpleNamespace(free=8*1024**3)]*len(self.guard.monitored) +
                      [SimpleNamespace(free=runner.MINIMUM_FREE-1)]*len(self.guard.monitored))
        self.disk.side_effect = lambda _: next(values)
        with mock.patch.object(runner.subprocess, 'Popen', return_value=child) as popen, mock.patch.object(self.guard, 'cleanup_group', return_value=self.cleanup) as clean:
            with self.assertRaises(runner.ComparisonStopped) as error:
                self.guard.run(self.command, self.root, self.guard.env, self.out/'reserve.log', 5)
            self.assertEqual(error.exception.reason, 'low_space')
            self.assertEqual(popen.call_count,1); clean.assert_called_once_with(child)
            self.assertTrue(popen.call_args.kwargs['start_new_session'])
            self.assertEqual(popen.call_args.kwargs['env'],self.guard.env)
        row=self.receipts()[0]
        self.assertEqual(row['state'],'low_space'); self.assertEqual(row['cleanup'],self.cleanup)
        self.assertEqual(row['logSHA256'],hashlib.sha256(b'').hexdigest())

    def test_timeout_has_one_launch_and_owned_cleanup_receipt(self):
        child=self.fake_child()
        with mock.patch.object(runner.subprocess,'Popen',return_value=child) as popen, mock.patch.object(self.guard,'cleanup_group',return_value=self.cleanup) as clean:
            with self.assertRaises(runner.ComparisonStopped) as error:
                self.guard.run(self.command,self.root,self.guard.env,self.out/'timeout.log',0)
            self.assertEqual(error.exception.reason,'timeout'); self.assertEqual(popen.call_count,1)
            clean.assert_called_once_with(child)
        self.assertEqual(self.receipts()[0]['state'],'timeout')

    def test_signal_interrupts_launch_and_restores_handlers_after_final_accounting(self):
        self.bind_source()
        for number in (signal.SIGINT,signal.SIGTERM):
            with self.subTest(signal=number):
                self.guard.stop_signal=None; self.guard.finalizing=False
                child=self.fake_child()
                child.poll.side_effect=lambda: self.guard.handle_signal(number,None)
                old={sig:signal.getsignal(sig) for sig in (signal.SIGINT,signal.SIGTERM)}
                log=self.out/('signal-'+str(number)+'.log')
                with mock.patch.object(runner.subprocess,'Popen',return_value=child), mock.patch.object(self.guard,'cleanup_group',return_value=self.cleanup) as clean, mock.patch.object(self.guard,'git',side_effect=[runner.BASELINE,'']):
                    with self.assertRaises(runner.ComparisonStopped):
                        with self.guard: self.guard.run(self.command,self.root,self.guard.env,log,1)
                    clean.assert_called_once_with(child)
                self.assertEqual({sig:signal.getsignal(sig) for sig in old},old)
                terminal=json.loads((self.out/'terminal-accounting.json').read_text())
                self.assertFalse(terminal['executionComplete'])
                self.assertEqual(self.manifest['state'],'interrupted')
                self.assertEqual(self.manifest['receivedSignal'],number)
                self.assertTrue(self.manifest['finalized'])

    def test_cleanup_escalates_only_recorded_process_group_and_reaps_child(self):
        child=self.fake_child(); child.wait.return_value=-signal.SIGKILL
        alive=[True]; calls=[]; ticks=iter(range(100))
        def killpg(pid,number):
            calls.append((pid,number))
            if number==signal.SIGKILL: alive[0]=False
        with mock.patch.object(self.guard,'group_exists',side_effect=lambda _:alive[0]), mock.patch.object(runner.os,'killpg',side_effect=killpg), mock.patch.object(runner.time,'monotonic',side_effect=lambda:next(ticks)), mock.patch.object(runner.time,'sleep'):
            result=self.guard.cleanup_group(child)
        self.assertEqual(calls,[(child.pid,signal.SIGTERM),(child.pid,signal.SIGKILL)])
        self.assertTrue(result['groupGone']); self.assertTrue(result['processReaped']); self.assertEqual(result['errors'],[])

    def test_cleanup_error_is_recorded_and_cannot_qualify_success(self):
        child=self.fake_child(poll=0)
        with mock.patch.object(self.guard,'group_exists',side_effect=PermissionError('synthetic group refusal')):
            cleanup=self.guard.cleanup_group(child)
        self.assertFalse(cleanup['groupGone']); self.assertTrue(cleanup['errors'])
        with mock.patch.object(runner.subprocess,'Popen',return_value=child), mock.patch.object(self.guard,'cleanup_group',return_value=cleanup):
            with self.assertRaises(runner.ComparisonStopped) as error:
                self.guard.run(self.command,self.root,self.guard.env,self.out/'cleanup.log',1)
        self.assertEqual(error.exception.reason,'cleanup_incomplete')
        self.assertEqual(self.receipts()[0]['cleanup'],cleanup)

    def test_nonzero_gate_finally_retains_source_identity_without_native_pass(self):
        self.bind_source(); child=self.fake_child(poll=7)
        with mock.patch.object(runner.subprocess,'Popen',return_value=child), mock.patch.object(self.guard,'cleanup_group',return_value=self.cleanup), mock.patch.object(self.guard,'git',side_effect=[runner.BASELINE,'']):
            with self.guard:
                code, timed_out=self.guard.run(self.command,self.root,self.guard.env,self.out/'failed-gate.log',1)
                self.manifest.update(state='consumer_failed',exitCode=code,timedOut=timed_out)
        self.assertEqual(code,7); self.assertFalse(timed_out)
        self.assertTrue(self.manifest['finalInputsUnchanged']); self.assertTrue(self.manifest['finalized'])
        terminal=json.loads((self.out/'terminal-accounting.json').read_text())
        self.assertFalse(terminal['executionComplete']); self.assertEqual(terminal['observedGenerationAttempts'],{})
        self.assertTrue(terminal['errors']); self.assertIn('failed-gate.log',self.manifest['outputs'])

    def test_changed_source_exception_and_missing_partial_reports_never_become_completed(self):
        path=self.bind_source(); self.guard.ids=['one','two']; self.guard.test='SyntheticComparison'
        self.guard.modes=modes
        self.guard.validator=SimpleNamespace(_unique_json=validator._unique_json, validate_canonical_inventory=lambda *args:None)
        report=synthetic(); report['fixture']='one'
        events=[dict(Action='output',Test=self.guard.test+'/one',Output='fixture.go:1: '+json.dumps(report)+'\n'),dict(Action='pass',Test=self.guard.test+'/one')]
        (self.out/'results.jsonl').write_text(''.join(json.dumps(event)+'\n' for event in events)+'{broken\n')
        self.manifest['summary']=dict(executionComplete=True,automatedChecksPassed=True)
        with mock.patch.object(self.guard,'git',side_effect=[runner.BASELINE,'']):
            with self.assertRaises(ValueError):
                with self.guard:
                    path.write_text('changed input')
                    raise ValueError('synthetic failed gate')
        terminal=json.loads((self.out/'terminal-accounting.json').read_text())
        self.assertEqual(terminal['reportedCases'],['one']); self.assertEqual(terminal['missingCaseReports'],['two'])
        self.assertEqual(terminal['observedGenerationAttempts'],{'one':1})
        self.assertFalse(terminal['executionComplete']); self.assertTrue(terminal['errors'])
        self.assertFalse(self.manifest['finalInputsUnchanged']); self.assertFalse(self.manifest['summary']['automatedChecksPassed'])
        self.assertEqual(self.manifest['state'],'failed'); self.assertTrue(self.manifest['finalized'])

    def test_callback_quality_failure_can_be_execution_complete_but_never_automated_pass(self):
        self.bind_source(); self.guard.ids=['one']; self.guard.test='SyntheticComparison'
        self.guard.modes=modes
        self.guard.validator=SimpleNamespace(_unique_json=validator._unique_json, validate_canonical_inventory=lambda *args:None)
        report=synthetic(); report.update(fixture='one',generationError='malformed answer')
        events=[dict(Action='output',Test=self.guard.test+'/one',Output='fixture.go:1: '+json.dumps(report)+'\n'),dict(Action='fail',Test=self.guard.test+'/one'),dict(Action='fail',Test=self.guard.test)]
        (self.out/'results.jsonl').write_text(''.join(json.dumps(event)+'\n' for event in events))
        self.manifest['summary']=dict(executionComplete=True,automatedChecksPassed=False)
        with mock.patch.object(self.guard,'git',side_effect=[runner.BASELINE,'']):
            with self.guard: self.manifest['state']='finished'
        terminal=json.loads((self.out/'terminal-accounting.json').read_text())
        self.assertTrue(terminal['executionComplete']); self.assertFalse(self.manifest['summary']['automatedChecksPassed'])
        self.assertEqual(terminal['observedGenerationAttempts'],{'one':1})

    def test_final_audit_failure_downgrades_prior_success_and_keeps_raw_evidence(self):
        raw=self.out/'results.jsonl'; raw.write_text('retained incomplete output\n')
        self.manifest['summary']=dict(executionComplete=True,automatedChecksPassed=True)
        with mock.patch.object(self.guard,'finalize',side_effect=OSError('synthetic final audit failure')):
            with self.guard: self.manifest['state']='finished'
        self.assertFalse(self.manifest['finalized']); self.assertFalse(self.manifest['summary']['executionComplete'])
        self.assertEqual(self.manifest['state'],'finalization_failed')
        self.assertEqual(raw.read_text(),'retained incomplete output\n')
        self.assertFalse(json.loads((self.out/'terminal-accounting.json').read_text())['executionComplete'])
        self.assertFalse(json.loads((self.out/'final-input-bindings.json').read_text())['inputsUnchanged'])


if __name__=='__main__': unittest.main()
