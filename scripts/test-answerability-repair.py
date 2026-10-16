#!/usr/bin/env python3
"""Counterexamples for the paired answerability runner; no models are run."""
import copy
import hashlib
import importlib.util
import json
import pathlib
import sys
import unittest
from unittest import mock
sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('answerability_repair', ROOT/'scripts/run-answerability-repair.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def completed():
    grammar = 'root ::= "test"\n'
    return {'generationAttempts': 1, 'nativeCallbackCompleted': True,
            'nativeCallbackError': '<nil>',
            'nativeCompletionContract': 'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos',
            'prepareError': '<nil>', 'sourceScopeError': '<nil>', 'finishError': '<nil>',
            'generatorGrammar': grammar, 'generatorGrammarSHA256': hashlib.sha256(grammar.encode()).hexdigest()}


class ComparisonGuards(unittest.TestCase):
    def test_successful_callback_is_comparable(self):
        self.assertEqual(runner.comparison_execution_errors(completed()), [])

    def test_failed_missing_or_repeated_callback_is_not_comparable(self):
        for key, value in [('generationAttempts', 0), ('generationAttempts', 2),
                           ('generationAttempts', True), ('nativeCallbackCompleted', False),
                           ('nativeCallbackError', 'RESOURCE_EXHAUSTED'),
                           ('nativeCompletionContract', 'unchecked'),
                           ('prepareError', 'failed'), ('sourceScopeError', 'foreign source'),
                           ('finishError', 'database unavailable')]:
            with self.subTest(field=key, value=value):
                self.assertTrue(runner.comparison_execution_errors({**completed(), key: value}))
        for key in completed():
            bad = completed(); del bad[key]
            with self.subTest(missing=key):
                self.assertTrue(runner.comparison_execution_errors(bad))

    def test_grammar_must_be_exact_and_nonempty(self):
        for key, value in [('generatorGrammar', ''), ('generatorGrammar', 'different'),
                           ('generatorGrammarSHA256', '0'*64)]:
            self.assertTrue(runner.comparison_execution_errors({**completed(), key: value}))

    def test_semantic_and_downstream_protocol_errors_remain_comparable(self):
        bad_answer = {**completed(), 'rawDraft': 'unsupported claim',
                      'generationError': 'quote invalid', 'rubricError': 'wrong attribute',
                      'executionPassed': False, 'passed': False}
        self.assertEqual(runner.comparison_execution_errors(bad_answer), [])

    def test_all_twelve_terminals_required_without_skips_or_duplicates(self):
        rows = [{'Test': runner.TEST, 'Action': 'fail'}]
        rows += [{'Test': runner.TEST+'/'+case, 'Action': 'fail'} for case in runner.IDS]
        self.assertEqual(runner.native_terminals(rows)[1], [])
        self.assertTrue(runner.native_terminals(rows[:-1])[1])
        self.assertTrue(runner.native_terminals(rows+[rows[-1]])[1])
        rows[-1]['Action'] = 'skip'
        self.assertTrue(runner.native_terminals(rows)[1])

    def test_original_and_independent_golden_scope_gates_are_all_required(self):
        original = [case['id'] for case in json.loads((ROOT/runner.ORIGINAL_CORPUS).read_text())['cases']]
        rows = []
        for test in runner.GATES:
            rows.append({'Test': test, 'Action': 'pass'})
            rows += [{'Test': test+'/'+case, 'Action': 'pass'} for case in (original if test.startswith('TestAITransfer') else runner.IDS)]
        self.assertEqual(runner.gate_errors(rows, original), [])
        self.assertTrue(runner.gate_errors(rows[:-1], original))
        rows[-1]['Action'] = 'skip'
        self.assertTrue(runner.gate_errors(rows, original))


class ModelComparisonGuards(unittest.TestCase):
    def bindings(self):
        stable = {'go': {'sha256': 'go'}, 'llama': {'sha256': 'llama'},
                  'model': {'sha256': 'baseline'}, 'modelManifest': {'sha256': 'baseline-manifest'},
                  'harness:corpus': {'sha256': 'frozen-corpus'}, 'pilot': {'sha256': 'old-recorder'}}
        source = {'internal/workspace/ai_generation.go': {'sha256': 'production'},
                  runner.CORPUS: {'sha256': runner.CORPUS_SHA256}}
        for name, digest in runner.COMPARISON_BASE_HARNESS.items():
            stable['harness:' + name] = {'sha256': digest}
            source[name] = {'sha256': digest}
        prior = {'sourceCommit': runner.COMPARISON_BASE_SOURCE, 'stableBindings': stable, 'sourceInputs': source}
        current = copy.deepcopy(stable); current['model'] = {'sha256': 'challenger'}
        current['modelManifest'] = {'sha256': 'challenger-manifest'}
        current['pilot'] = {'sha256': 'new-recorder'}
        changed_source = copy.deepcopy(source)
        for name in runner.COMPARISON_HARNESS:
            current['harness:' + name] = {'sha256': 'new-recorder'}
            changed_source[name] = {'sha256': 'new-recorder'}
        for name, digest in runner.COMPARISON_TEST_INPUTS.items():
            current['harness:' + name] = {'sha256': digest}
            changed_source[name] = {'sha256': digest}
        return prior, current, changed_source

    def test_only_selected_artifact_and_recorder_changes_are_allowed(self):
        prior, current, source = self.bindings()
        self.assertEqual(runner.model_comparison_binding_errors(prior, current, source), [])
        for key in ('go', 'llama', 'harness:corpus'):
            mutated = copy.deepcopy(current); mutated[key] = {'sha256': 'changed'}
            self.assertTrue(runner.model_comparison_binding_errors(prior, mutated, source))
        for key in ('internal/workspace/ai_generation.go', runner.CORPUS, 'internal/workspace/new_prompt.go'):
            mutated = copy.deepcopy(source); mutated[key] = {'sha256': 'changed'}
            self.assertTrue(runner.model_comparison_binding_errors(prior, current, mutated))
        for name in runner.COMPARISON_TEST_INPUTS:
            mutated = copy.deepcopy(source); mutated[name] = {'sha256': 'unreviewed-test-change'}
            self.assertTrue(runner.model_comparison_binding_errors(prior, current, mutated))
        wrong = copy.deepcopy(prior); wrong['sourceCommit'] = 'other'
        self.assertTrue(runner.model_comparison_binding_errors(wrong, current, source))
        wrong = copy.deepcopy(prior); wrong['stableBindings']['harness:scripts/run-answerability-repair.py']['sha256'] = 'unknown'
        self.assertTrue(runner.model_comparison_binding_errors(wrong, current, source))

    def report(self):
        return {**completed(), 'preparedSystem': 'system', 'generatorSystem': 'system',
                'preparedPrompt': 'evidence and question', 'generatorPrompt': 'evidence and question',
                'generatorOutputCap': 512, 'outputCap': 512, 'contextTokens': 8192,
                'source': 'fact', 'sourceTitle': 'title', 'prompt': 'question', 'selection': '',
                'mode': 'answer', 'useMemory': False, 'corpusSHA256': runner.CORPUS_SHA256,
                'sources': [{'number': 1, 'title': 'title', 'text': 'fact'}],
                'peakWorkerRSSBytes': 2 << 30}

    def test_actual_generation_inputs_must_be_identical(self):
        prior = self.report(); current = copy.deepcopy(prior)
        current.update(promptTokens=999, rawDraft='different model answer', modelPackId=runner.COMPARISON_PACK)
        self.assertEqual(runner.model_comparison_report_errors(prior, current, 10 << 30), [])
        for key in ('preparedSystem', 'preparedPrompt', 'generatorSystem', 'generatorPrompt',
                    'generatorGrammar', 'generatorGrammarSHA256', 'generatorOutputCap', 'outputCap',
                    'contextTokens', 'source', 'sourceTitle', 'prompt', 'selection', 'mode', 'useMemory', 'corpusSHA256'):
            changed = copy.deepcopy(current); changed[key] = 'changed'
            with self.subTest(field=key):
                self.assertTrue(runner.model_comparison_report_errors(prior, changed, 10 << 30))
        for sources in (None, [], ['malformed'], [{'number': 1, 'title': 'title', 'text': 'different fact'}]):
            self.assertTrue(runner.model_comparison_report_errors(prior, {**current, 'sources': sources}, 10 << 30))

    def test_sampled_memory_must_be_positive_and_within_unchanged_budget(self):
        report = self.report()
        for rss in (None, True, 0, -1, (10 << 30) + 1):
            self.assertTrue(runner.model_comparison_report_errors(report, {**report, 'peakWorkerRSSBytes': rss}, 10 << 30))


    def test_comparison_entrypoint_has_separate_complete_accounting(self):
        rows = [{'Test': runner.COMPARISON_TEST, 'Action': 'fail'}]
        rows += [{'Test': runner.COMPARISON_TEST+'/'+case, 'Action': 'fail'} for case in runner.IDS]
        self.assertEqual(runner.native_terminals(rows, runner.COMPARISON_TEST)[1], [])
        self.assertTrue(runner.native_terminals(rows)[1])
        self.assertTrue(runner.native_terminals(rows[:-1], runner.COMPARISON_TEST)[1])
        validator = runner.load_repository_validator(ROOT)
        events = [{'Test': runner.COMPARISON_TEST+'/'+case, 'Action': 'output',
                   'Output': 'report: '+json.dumps({'fixture': case})+'\n'} for case in runner.IDS]
        reports, errors = runner.extract_reports(events, validator, runner.COMPARISON_TEST)
        self.assertEqual(errors, [])
        self.assertEqual(set(reports), set(runner.IDS))
        self.assertTrue(runner.extract_reports(events, validator)[1])

    def test_exact_quality_pack_and_unknown_pack_are_distinct(self):
        quality = {**runner.COMPARISON_MODELS[runner.COMPARISON_QUALITY_PACK], 'architecture': 'portable'}
        with mock.patch.object(runner.subprocess, 'check_output', return_value=str(24 << 30)):
            self.assertEqual(runner.comparison_resources(quality)['declaredModelMemoryBytes'], 39 << 28)
            with self.assertRaises(ValueError):
                runner.comparison_resources({**quality, 'sha256': '0'*64})
            with self.assertRaises(ValueError):
                runner.comparison_resources({**quality, 'id': runner.PACK})

    def test_pack_pinning_and_native_admission_are_required(self):
        model = {**runner.COMPARISON_MODEL, 'architecture': 'arm64'}
        with mock.patch.object(runner.subprocess, 'check_output', return_value=str(24 << 30)):
            policy = runner.comparison_resources(model)
            self.assertEqual(policy['nativeBudgetBytes'], 10 << 30)
            self.assertEqual(policy['minimumFreeBytes'], 4 << 30)
            for field in runner.COMPARISON_MODEL:
                changed = {**model, field: 'unreviewed'}
                with self.subTest(field=field), self.assertRaises(ValueError):
                    runner.comparison_resources(changed)
            for field, value in [('architecture', 'x86'), ('qualificationOnly', True)]:
                with self.assertRaises(ValueError):
                    runner.comparison_resources({**model, field: value})
        with mock.patch.object(runner.subprocess, 'check_output', return_value=str(8 << 30)):
            with self.assertRaisesRegex(ValueError, 'admission'):
                runner.comparison_resources(model)


if __name__ == '__main__':
    unittest.main(verbosity=2)
