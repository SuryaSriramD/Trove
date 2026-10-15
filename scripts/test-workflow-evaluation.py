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

    def memory(self, case='preference-on'):
        contract = {'family': 'memory-lifecycle', 'modelPackId': 'chosen', 'corpusSHA256': 'frozen'}
        included = case not in m.MEMORY_EXCLUDED_CASES
        content = 'Use the heading Launch note for answers about Project Marigold.'
        fact = 'Project Marigold launches on 14 April 2031.'
        sources = [{'text': fact, 'location': {'pageId': 'primary'}}]
        if included: sources.append({'text': content, 'memoryId': 'memory', 'location': {'pageId': 'support'}})
        report = dict(contract, engineMode='native', releaseQualified=False,
            generationAttempts=int(case in m.MEMORY_GENERATION_CASES),
            assertions={key: True for key in m.MEMORY_ASSERTIONS[case]},
            promptTokens=1000, outputCap=512, contextTokens=8192,
            request={'pageId': 'primary', 'useMemory': case != 'preference-off', 'modelPackId': 'chosen'},
            sources=sources, memoryId='memory', memoryPageId='backing', supportingPageId='support',
            memoryContent=content, memoryIncluded=included, memoryStatus='active', preparedPrompt=content if included else fact,
            rawDraft=json.dumps({'title': 'Launch note' if case == 'preference-on' else '', 'claims': [{'text': fact, 'quote': fact}]}),
            citations=[{'location': {'pageId': 'primary'}, 'state': 'current'}])
        return report, contract

    def review(self, case='daily-summary'):
        contract = {'family': 'reviews', 'modelPackId': 'chosen', 'corpusSHA256': 'frozen'}
        expected = m.review_expected_draft(case)
        bindings = {'source': 'source', 'destination': 'inbox', 'private': 'private', 'documentation': 'docs', 'openTask': 'open', 'completedTask': 'done'}
        selected = ['source'] + (['docs'] if case == 'weekly-summary' else ['open', 'done'] if case == 'unresolved-tasks' else [])
        category = 'risks' if case == 'risk-suggestion' else 'decisions' if case == 'categories' else 'summary'
        actions = ['append_review', 'append_page'] if case == 'risk-suggestion' else ['append_review']
        plan = {'enabled': True, 'sourcePageIds': selected, 'includeMemory': case in ('changed-decision', 'forgotten-memory'),
                'schedule': {'frequency': 'weekly' if case == 'weekly-summary' else 'daily', 'timezone': 'UTC'},
                'generative': {'modelPackId': 'chosen', 'categories': [category], 'allowedActions': actions, 'actionPageIds': ['source']}}
        sources, citations = [], []
        for index, claim in enumerate(expected['claims']):
            page = 'open' if case == 'unresolved-tasks' else 'docs' if index else 'source'
            loc = {'pageId': page, 'sourceRevision': '2'}
            sources.append({'number': index+1, 'text': claim['quote'], 'location': loc})
            citations.append({'number': index+1, 'state': 'current', 'location': loc})
        report = dict(contract, engineMode='native', releaseQualified=False,
            assertions={key: True for key in m.REVIEW_ASSERTIONS[case]},
            generationAttempts=int(case in m.REVIEW_GENERATION_CASES), serviceBoundaryOnly=case in m.REVIEW_BOUNDARY_CASES,
            generatorInvocations=0 if case == 'restart' else 2 if case == 'cancel-retry' else 1,
            sourceBindings=bindings, plan=plan, promptTokens=1200, outputCap=1024, contextTokens=8192,
            preparedPrompt='The security review is complete. Launch can proceed.' if plan['includeMemory'] else m.REVIEW_FACT,
            rawDraft=json.dumps(expected), actions=expected['actions'], sources=sources, citations=citations,
            finalStatus='ready', unapprovedApplyRejected=True, runId='run', repeatedOccurrenceRunId='run',
            reviewedEffect={'destinationPageId': 'inbox', 'status': 'accepted', 'beforeBlocks': 1, 'afterBlocks': 2, 'retryBlocks': 2,
                            'destinationText': '\n'.join(c['text'] for c in expected['claims']), 'sourceText': m.REVIEW_FACT + m.REVIEW_SUGGESTION})
        if case == 'weekly-summary': report.update(warning='Additional excerpts omitted', capturedWarning='Additional excerpts omitted')
        if case == 'stale-destination': report['staleApplyRejected'] = True
        if case == 'forgotten-memory': report.update(finishRejected=True, proposalRejected=True, finalStatus='failed')
        if case == 'restart': report.update(finalStatus='cancelled', nextRunId='')
        if case == 'cancel-retry': report.update(cancelStatus='cancelled')
        if case == 'dedup-import': report.update(portableExecutionCounts=dict.fromkeys(m.REVIEW_PORTABLE_TABLES, 0), portableNextRunId='')
        return report, contract

    def image(self):
        corpus=json.loads((m.ROOT/'internal/workspace/testdata/expanded-evaluation/images-execution-v2.json').read_text())
        cases={case['id']:case for case in corpus['cases']}
        contract=dict(family='images',modelPackId='chosen',corpusSHA256='locked',imageCases=cases,modelSHA256='a'*64,projectorSHA256='b'*64,helperSHA256='c'*64)
        case=cases['diagram-1']
        report=dict(family='images',modelPackId='chosen',corpusSHA256='locked',engineMode='native',releaseQualified=False,
            assertions=dict.fromkeys(m.IMAGE_ASSERTIONS,True),sourceSHA256=case['sourceSHA256'],sourceKind=case['sourceKind'],
            question=case['prompt'],expectedMethod=case['method'],productInvocations=1,ocrInvocations=1,
            result=dict(state='ready',analysisMethod='local_vision_model',text='red',modelHash='a'*64,projectorHash='b'*64,helperHash='c'*64,finishReason='eos',inputTokens=200,outputTokens=2,sampledPeakRSSBytes=500000),
            ocr={'lines':[]},ocrCapability={'engine':'Apple Vision (local)','extractorVersion':'trove-local-vision-v1'})
        return report,contract

    def test_image_native_identity_content_and_calls(self):
        report,contract=self.image()
        self.assertEqual(m.validate_workflow_report(report,'diagram-1',contract),[])
        for change in ({'productInvocations':2},{'productInvocations':True},{'ocrInvocations':0},{'engineMode':'deterministic'},{'sourceSHA256':'changed'},{'question':'different'},{'ocrError':'failed'},{'ocr':{'lines':[{'text':'invented','confidence':.2}]}},{'assertions':{}}):
            self.assertTrue(m.validate_workflow_report(dict(report,**change),'diagram-1',contract),change)
        for change in ({'text':'red and a cat'},{'text':'blue'},{'modelHash':'f'*64},{'helperHash':'d'*64},{'finishReason':'length'},{'sampledPeakRSSBytes':0},{'state':'partial'}):
            bad=copy.deepcopy(report);bad['result'].update(change)
            self.assertTrue(m.validate_workflow_report(bad,'diagram-1',contract),change)

    def test_image_missing_representative_source_cannot_pass(self):
        report,contract=self.image()
        self.assertTrue(m.validate_workflow_report(report,'photograph-1',contract))
        self.assertTrue(m.validate_workflow_report(report,'screenshot-1',contract))

    def test_image_measurement_does_not_claim_model_generation(self):
        report,contract=self.image();case=contract['imageCases']['boundary-1']
        report.update(sourceSHA256=case['sourceSHA256'],sourceKind=case['sourceKind'],question=case['prompt'],expectedMethod=case['method'])
        report['result']=dict(state='ready',analysisMethod='pixel_measurement',finishReason='measurement',text=f"Source pixel measurement ({case['width']} × {case['height']} decoded pixels, before resizing and alpha composition; encoded orientation): the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted.")
        self.assertEqual(m.validate_workflow_report(report,'boundary-1',contract),[])
        for change in ({'modelHash':'a'*64},{'outputTokens':1},{'sampledPeakRSSBytes':1},{'text':report['result']['text']+' There is a cat.'}):
            bad=copy.deepcopy(report);bad['result'].update(change)
            self.assertTrue(m.validate_workflow_report(bad,'boundary-1',contract),change)

    def test_review_native_and_service_boundaries_are_distinct(self):
        for case in m.REVIEW_ASSERTIONS:
            report, contract = self.review(case)
            self.assertEqual(m.validate_workflow_report(report, case, contract), [], case)
            for change in ({'engineMode': 'deterministic'}, {'serviceBoundaryOnly': not report['serviceBoundaryOnly']}, {'generationAttempts': 2}, {'generatorInvocations': 10}, {'assertions': {}}):
                self.assertTrue(m.validate_workflow_report(dict(report, **change), case, contract), (case, change))

    def test_review_semantics_selection_quotes_and_effect_counterexamples(self):
        for case in m.REVIEW_GENERATION_CASES:
            r, c = self.review(case)
            for change in ({'rawDraft': '{}'}, {'citations': []}, {'sources': []}, {'promptTokens': -1}, {'unapprovedApplyRejected': False}, {'repeatedOccurrenceRunId': 'other'}, {'preparedPrompt': r['preparedPrompt']+'PRIVATE-REVIEW-73'}):
                self.assertTrue(m.validate_workflow_report(dict(r, **change), case, c), (case, change))
            for key, value in (('categories', ['summary', 'risks', 'decisions']), ('allowedActions', ['create_task']), ('actionPageIds', ['private'])):
                bad=copy.deepcopy(r); bad['plan']['generative'][key]=value
                self.assertTrue(m.validate_workflow_report(bad, case, c))
            for change in ({'retryBlocks': 3}, {'destinationPageId': 'private'}, {'destinationText': ''}):
                bad=copy.deepcopy(r); bad['reviewedEffect'].update(change)
                self.assertTrue(m.validate_workflow_report(bad, case, c))
        r,c=self.review('unresolved-tasks');r['citations'][0]['location']['pageId']='done'
        self.assertTrue(m.validate_workflow_report(r, 'unresolved-tasks', c))

    def test_review_forget_restart_stale_and_portable_counterexamples(self):
        changes = {'forgotten-memory': {'proposalRejected': False}, 'restart': {'nextRunId': 'run'}, 'stale-destination': {'staleApplyRejected': False}, 'cancel-retry': {'cancelStatus': 'ready'}, 'dedup-import': {'portableExecutionCounts': dict.fromkeys(m.REVIEW_PORTABLE_TABLES, 1)}}
        for case,change in changes.items():
            r,c=self.review(case)
            self.assertTrue(m.validate_workflow_report(dict(r, **change), case, c))

    def test_memory_generation_and_request_counterexamples(self):
        for case in ('preference-on', 'preference-off'):
            r, c = self.memory(case)
            self.assertEqual(m.validate_workflow_report(r, case, c), [])
            changes = [{'engineMode': 'deterministic'}, {'memoryIncluded': not r['memoryIncluded']},
                {'memoryStatus': 'forgotten'}, {'sources': []}, {'generationAttempts': 0},
                {'rawDraft': '{}'}, {'rawDraft': r['rawDraft'].replace('2031', '2032')},
                {'citations': []}, {'promptTokens': -1}, {'outputCap': 0}, {'assertions': {}}]
            for change in changes:
                self.assertTrue(m.validate_workflow_report(dict(r, **change), case, c), change)
            bad = copy.deepcopy(r)
            bad['request']['useMemory'] = not bad['request']['useMemory']
            self.assertTrue(m.validate_workflow_report(bad, case, c))
        r, c = self.memory('preference-off')
        r['preparedPrompt'] += r['memoryContent']
        self.assertTrue(m.validate_workflow_report(r, 'preference-off', c))

    def test_memory_forget_and_restore_boundary_counterexamples(self):
        r, c = self.memory('forget-stream')
        r.update(finishState='failed', finishText='', finishRejected=False, proposalRejected=True)
        self.assertEqual(m.validate_workflow_report(r, 'forget-stream', c), [])
        for change in ({'finishText': 'old memory'}, {'finishState': 'completed'}, {'proposalRejected': False}, {'generationAttempts': 1}):
            self.assertTrue(m.validate_workflow_report(dict(r, **change), 'forget-stream', c))
        r, c = self.memory('restore-forgotten')
        r.update(memoryStatus='forgotten', relatedRejected=True, backupWorkspaceId='workspace', restoredWorkspaceId='workspace', restoredIndexRebuilt=True)
        self.assertEqual(m.validate_workflow_report(r, 'restore-forgotten', c), [])
        for change in ({'memoryStatus': 'active'}, {'relatedRejected': False}, {'restoredWorkspaceId': 'other'}, {'restoredIndexRebuilt': False}):
            self.assertTrue(m.validate_workflow_report(dict(r, **change), 'restore-forgotten', c))
        r, c = self.memory('older-backup')
        r.update(backupWorkspaceId='workspace', restoredWorkspaceId='workspace', restoredIndexRebuilt=True,
            sourceAfterBackupStatus='forgotten', restoreBoundary='A backup predating Forget retains the earlier approved memory state.',
            nativeRestoreWarningAcceptancePending=True, manualSemanticReviewRequired=True)
        self.assertEqual(m.validate_workflow_report(r, 'older-backup', c), [])
        self.assertTrue(m.validate_workflow_report(dict(r, nativeRestoreWarningAcceptancePending=False), 'older-backup', c))

    def test_case_inventory_is_complete_and_frozen(self):
        root = m.ROOT / 'internal/workspace/testdata/expanded-evaluation'
        for family in ('context', 'related', 'memory-lifecycle', 'reviews'):
            filename, _, count = m.FAMILIES[family]
            cases = json.loads((root/filename).read_text())['cases']
            self.assertEqual(count, 12)
            self.assertEqual(len({c['id'] for c in cases}), count)
        self.assertEqual(set(m.REVIEW_ASSERTIONS), {c['id'] for c in json.loads((root/'reviews.json').read_text())['cases']})
        self.assertEqual(set(m.MEMORY_ASSERTIONS), {c['id'] for c in json.loads((root/'memory-lifecycle.json').read_text())['cases']})
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
            {'promptTokens': -100, 'outputCap': -512, 'contextTokens': 0},
            {'outputCap': 1}, {'contextTokens': 9999}, {'promptTokens': -1},
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
        for change in ({'generationAttempts': 1}, {'framedTokens': 200}, {'contextTokens': 750}, {'unframedTokens': -600, 'framedTokens': -599, 'contextTokens': -72}):
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
