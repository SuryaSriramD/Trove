#!/usr/bin/env python3
"""Counterexamples for the paired answerability runner; no models are run."""
import hashlib
import importlib.util
import json
import pathlib
import sys
import unittest
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


if __name__ == '__main__':
    unittest.main(verbosity=2)
