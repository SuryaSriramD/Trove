#!/usr/bin/env python3
"""Non-model consumer counterexamples. Synthetic receipts are test data only."""
import copy
import importlib.util
import json
import pathlib
import sys
import unittest
sys.dont_write_bytecode=True
ROOT=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('mode_controls',ROOT/'scripts/run-independent-mode-controls.py')
runner=importlib.util.module_from_spec(spec); spec.loader.exec_module(runner)
CASES=json.loads((ROOT/runner.CORPUS).read_text())['cases']


def synthetic_receipt():
    receipt=runner.manual_template(CASES)
    for item in receipt['items']:
        if not item['manualReviewRequired']: continue
        accept=item['expectedAccept']
        item['review']={'state':'completed','reviewer':'SYNTHETIC VALIDATOR UNIT TEST, NOT AN ASSESSMENT','reviewedAtUTC':'2026-10-16T00:00:00Z','allClausesAssessed':True,'accept':accept,'clauseAssessments':[{'text':'synthetic shape','finding':'validator fixture','evidence':'no semantic assessment'}],'unsupportedOrMissingClauses':[] if accept else ['synthetic rejection'],'rationale':'Only validates receipt structure; never written as real evidence.'}
    return receipt


def synthetic_parser():
    rows=[]
    for case in CASES:
        if case['mode']!='mermaid': continue
        diagram=copy.deepcopy(case['diagram']); diagram['parserType']='sequence' if diagram['kind']=='sequenceDiagram' else 'flowchart-v2'
        rows.append({'fixture':case['id'],'label':'golden','expected':True,'accepted':True,'passed':True,'source':case['golden'],'projection':diagram,'error':None})
        for poison in case['counterexamples']:
            rows.append({'fixture':case['id'],'label':poison['kind'],'expected':False,'accepted':False,'passed':True,'source':poison['output'],'projection':None,'error':'synthetic rejection, not real parser output'})
    return {'schemaVersion':1,'corpusSHA256':runner.CORPUS_SHA256,'parserVersion':'12.0.0','parserEntrySHA256':'f'*64,'mode':'golden','passed':True,'releaseQualified':False,'results':rows}


class ConsumerGuards(unittest.TestCase):
    def test_manual_template_never_counts_as_completed(self):
        template=runner.manual_template(CASES)
        self.assertEqual(len(template['items']),84)
        self.assertTrue(runner.validate_manual_review(template,template))
    def test_manual_shape_positive_is_not_semantic_proof(self):
        self.assertFalse(runner.validate_manual_review(synthetic_receipt(),runner.manual_template(CASES)))
    def test_manual_missing_clause_assessment_and_coverage_reject(self):
        for field,value in [('clauseAssessments',[]),('allClausesAssessed',False),('reviewer',''),('reviewedAtUTC',None)]:
            receipt=synthetic_receipt(); receipt['items'][40]['review'][field]=value
            self.assertTrue(runner.validate_manual_review(receipt,runner.manual_template(CASES)),field)
    def test_manual_binding_or_decision_change_reject(self):
        template=runner.manual_template(CASES)
        for key,value in [('output','different bytes'),('source','foreign source'),('prompt','hint'),('expectedAccept',False),('outputSHA256','0'*64)]:
            receipt=synthetic_receipt(); receipt['items'][0][key]=value
            self.assertTrue(runner.validate_manual_review(receipt,template),key)
        receipt=synthetic_receipt(); receipt['items'][43]['review']['accept']=True
        self.assertTrue(runner.validate_manual_review(receipt,template))
        receipt=synthetic_receipt(); receipt['items'].pop()
        self.assertTrue(runner.validate_manual_review(receipt,template))
    def test_parser_positive_shape_only(self):
        self.assertFalse(runner.parser_report_errors(synthetic_parser(),CASES,'golden','f'*64))
    def test_parser_missing_or_duplicate_row_reject(self):
        report=synthetic_parser(); report['results'].pop()
        self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
        report=synthetic_parser(); report['results'][1]=copy.deepcopy(report['results'][0])
        self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
    def test_parser_wrong_input_or_identity_reject(self):
        for key,value in [('corpusSHA256','0'*64),('parserVersion','11.0.0'),('parserEntrySHA256','0'*64),('mode','outputs')]:
            report=synthetic_parser(); report[key]=value
            self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64),key)
        report=synthetic_parser(); report['results'][0]['source']='other source'
        self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
    def test_parser_claimed_success_with_wrong_graph_reject(self):
        for mutator in (lambda p:p['nodes'].append('Extra'),lambda p:p['edges'].pop(),lambda p:p.update(direction='RL')):
            report=synthetic_parser(); mutator(report['results'][0]['projection'])
            self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
    def test_parser_claimed_success_with_wrong_sequence_reject(self):
        report=synthetic_parser(); row=next(x for x in report['results'] if x['label']=='golden' and x['projection']['kind']=='sequenceDiagram')
        row['projection']['messages'][0]['kind']='-->>'
        self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
    def test_parser_outcome_aggregate_contradictions_reject(self):
        for change in ('accepted','passed','error'):
            report=synthetic_parser(); report['results'][0][change]={'accepted':False,'passed':False,'error':'failure'}[change]
            self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
        report=synthetic_parser(); report['passed']=False
        self.assertTrue(runner.parser_report_errors(report,CASES,'golden','f'*64))
    def test_gate_requires_all_42_original_and_32_new_subcases(self):
        original=[c['id'] for c in json.loads((ROOT/runner.ORIGINAL_CORPUS).read_text())['cases']]
        rows=[]
        for test in runner.GATES:
            rows.append({'Test':test,'Action':'pass'})
            rows.extend({'Test':test+'/'+case,'Action':'pass'} for case in (original if test=='TestExpandedModeOracles' else runner.IDS))
        self.assertFalse(runner.gate_errors(rows,original))
        self.assertEqual(len(rows),77)
        self.assertTrue(runner.gate_errors(rows[:-1],original))
        rows[-1]['Action']='skip'
        self.assertTrue(runner.gate_errors(rows,original))
    def test_native_terminal_parent_missing_duplicate_and_skip(self):
        rows=[{'Test':runner.TEST,'Action':'pass'}]+[{'Test':runner.TEST+'/'+case,'Action':'pass'} for case in runner.IDS]
        self.assertFalse(runner.native_terminals(rows)[1])
        self.assertTrue(runner.native_terminals(rows[1:])[1])
        self.assertTrue(runner.native_terminals(rows+[rows[0]])[1])
        rows[-1]['Action']='skip'
        self.assertTrue(runner.native_terminals(rows)[1])


    def test_comparison_requires_successful_native_callback(self):
        base={'generationAttempts':1,'nativeCallbackCompleted':True,'nativeCallbackError':'<nil>','nativeCompletionContract':'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos','rawDraft':'output','prepareError':'<nil>','sourceScopeError':'<nil>','finishError':'<nil>'}
        self.assertFalse(runner.comparison_execution_errors(base))
        for key,value in [('nativeCallbackCompleted',False),('nativeCallbackError','MODEL_NOT_INSTALLED'),('nativeCallbackError','OUTPUT_LIMIT'),('generationAttempts',0),('generationAttempts',True),('finishError','database unavailable')]:
            bad={**base,key:value}
            self.assertTrue(runner.comparison_execution_errors(bad),key)
    def test_comparison_preserves_downstream_quality_failures(self):
        report={'generationAttempts':1,'nativeCallbackCompleted':True,'nativeCallbackError':'<nil>','nativeCompletionContract':'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos','rawDraft':'wrong or malformed output','prepareError':'<nil>','sourceScopeError':'<nil>','finishError':'<nil>','generationError':'JSON decode failed','structuralOracleError':'wrong graph','proposalError':'unsupported proposal','automatedTaskPassed':False}
        self.assertFalse(runner.comparison_execution_errors(report))
    def test_comparison_does_not_use_answer_content_as_completion(self):
        report={'generationAttempts':1,'nativeCallbackCompleted':True,'nativeCallbackError':'<nil>','nativeCompletionContract':'GenerateTextWithGrammarMetrics: success validates generation kind, nonempty text and eos','rawDraft':'','prepareError':'<nil>','sourceScopeError':'<nil>','finishError':'<nil>'}
        self.assertFalse(runner.comparison_execution_errors(report))
        del report['nativeCallbackError']
        self.assertTrue(runner.comparison_execution_errors(report))


if __name__=='__main__': unittest.main(verbosity=2)
