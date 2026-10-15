"""Independent consumer counterexamples for the declared workflow runners."""
import copy
import importlib.util
import json
import pathlib
import unittest

spec = importlib.util.spec_from_file_location('evaluation', pathlib.Path(__file__).with_name('run-expanded-evaluation.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class WorkflowAccounting(unittest.TestCase):
    def context(self):
        contract = {'family': 'context', 'modelPackId': 'chosen', 'corpusSHA256': 'frozen'}
        fact = 'Project Marigold launches on 14 April 2031.'
        report = dict(contract, engineMode='native', releaseQualified=False, generationAttempts=1,
            assertions={key: True for key in m.CONTEXT_ASSERTIONS['short-source']},
            promptTokens=1000, outputCap=512, contextTokens=8192, retainedFact=True,
            request={'pageId': 'source'}, sources=[{'text': fact}],
            rawDraft=json.dumps({'title': '', 'claims': [{'text': fact, 'quote': fact}]}),
            citations=[{'location': {'pageId': 'source'}, 'state': 'current'}])
        return report, contract

    def related(self):
        contract = {'family': 'related', 'modelPackId': 'chosen', 'corpusSHA256': 'frozen'}
        report = dict(contract, engineMode='native', releaseQualified=False,
            assertions={key: True for key in m.RELATED_ASSERTIONS}, embeddingIdentity='a'*64,
            embeddingCalls=3, sourceBindings={'current': 'self', 'relevant': 'yes', 'irrelevant': 'no', 'restricted': 'private'},
            semanticResults={'mode': 'semantic', 'hits': [{'location': {'pageId': 'yes', 'sourceRevision': '2'}, 'excerpt': 'current'}]},
            keywordFallback={'mode': 'keyword', 'warning': 'Semantic disabled'})
        return report, contract

    def test_case_inventory_is_complete_and_frozen(self):
        root = m.ROOT / 'internal/workspace/testdata/expanded-evaluation'
        for family in ('context', 'related'):
            filename, _, count = m.FAMILIES[family]
            cases = json.loads((root/filename).read_text())['cases']
            self.assertEqual(count, 12)
            self.assertEqual(len({c['id'] for c in cases}), count)
        self.assertEqual(set(m.CONTEXT_ASSERTIONS), {c['id'] for c in json.loads((root/'context.json').read_text())['cases']})

    def test_context_positive_and_absent_fact(self):
        r, c = self.context()
        self.assertEqual(m.validate_workflow_report(r, 'short-source', c), [])
        r['assertions'] = {key: True for key in m.CONTEXT_ASSERTIONS['ending-fact']}
        r.update(sources=[{'text': 'Unrelated evidence'}], retainedFact=False, citations=[],
            rawDraft=json.dumps({'title': '', 'claims': [{'text': 'The supplied evidence does not answer this question.', 'quote': ''}]}))
        self.assertEqual(m.validate_workflow_report(r, 'ending-fact', c), [])

    def test_context_false_success_cannot_pass(self):
        r, c = self.context()
        changes = [
            {'engineMode': 'deterministic'}, {'assertions': {}}, {'generationAttempts': 0},
            {'generationAttempts': True}, {'modelPackId': 'substitute'}, {'corpusSHA256': 'changed'},
            {'retainedFact': False}, {'sources': []}, {'promptTokens': 8192}, {'citations': []},
            {'releaseQualified': True}, {'rawDraft': '{}'},
            {'rawDraft': r['rawDraft'].replace('2031', '2032')},
            {'rawDraft': r['rawDraft'].replace('2031.', '2031. The launch is guaranteed to succeed.', 1)},
        ]
        for change in changes:
            with self.subTest(change=change):
                bad = dict(r, **change)
                self.assertTrue(m.validate_workflow_report(bad, 'short-source', c))

    def test_boundary_requires_no_generation_and_its_own_checks(self):
        _, c = self.context()
        r = dict(c, engineMode='native', releaseQualified=False, generationAttempts=0,
            assertions={key: True for key in m.CONTEXT_ASSERTIONS['framing-cost']},
            unframedTokens=200, framedTokens=220, contextTokens=728)
        self.assertEqual(m.validate_workflow_report(r, 'framing-cost', c), [])
        for change in ({'generationAttempts': 1}, {'framedTokens': 200}, {'contextTokens': 750}):
            self.assertTrue(m.validate_workflow_report(dict(r, **change), 'framing-cost', c))

    def test_related_positive(self):
        r, c = self.related()
        self.assertEqual(m.validate_workflow_report(r, 'renewal', c), [])

    def test_related_rejects_mock_scope_staleness_and_fallback_conflation(self):
        r, c = self.related()
        changes = [
            {'embeddingIdentity': 'injected-provider:chosen'}, {'embeddingCalls': 0},
            {'engineMode': 'deterministic'}, {'assertions': {}},
            {'keywordFallback': {'mode': 'semantic', 'warning': ''}},
            {'semanticResults': {'mode': 'keyword', 'hits': []}},
        ]
        for change in changes:
            self.assertTrue(m.validate_workflow_report(dict(r, **change), 'renewal', c))
        for page in ('private', 'self', 'no'):
            bad = copy.deepcopy(r)
            bad['semanticResults']['hits'].insert(0, {'location': {'pageId': page, 'sourceRevision': '2'}, 'excerpt': 'secret'})
            self.assertTrue(m.validate_workflow_report(bad, 'renewal', c))
        bad = copy.deepcopy(r)
        bad['semanticResults']['hits'][0]['location']['sourceRevision'] = '1'
        self.assertTrue(m.validate_workflow_report(bad, 'renewal', c))

if __name__ == '__main__':
    unittest.main()
