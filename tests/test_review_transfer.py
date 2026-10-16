"""Accounting counterexamples only; synthetic reports never establish inference."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('expanded', ROOT/'scripts/run-expanded-evaluation.py')
RUNNER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNNER)
CORPUS = json.loads((ROOT/'internal/workspace/testdata/expanded-evaluation/personal-review-transfer-v1.json').read_text())


class ReviewTransferAccounting(unittest.TestCase):
    def fixture(self, case):
        bindings = {s['key']:'page-'+s['key'] for s in case['sources']}
        source_ids = list(bindings.values())
        report = dict(generationAttempts=1, generatorInvocations=1, nativeCallbackCompleted=True,
                      nativeCallbackError='<nil>', generationError='<nil>', generatorOutputCap=1024,
                      outputCap=1024, contextTokens=8192, promptTokens=100, peakWorkerRSSBytes=2048,
                      generationVersion='personal-review-claim-guidance-v2', finalStatus='ready',
                      manualSemanticReviewRequired=True, generatorGrammar='grammar',
                      generatorGrammarSHA256=hashlib.sha256(b'grammar').hexdigest(),
                      generatorPrompt=case['focus'], generatorSystem='system', sourceBindings=bindings,
                      rawDraft=json.dumps(case['golden']), actions=[],
                      displayed='\n'.join(c['text'] for c in case['golden']['claims']),
                      plan=dict(query=case['focus'], sourcePageIds=source_ids,
                                generative=dict(categories=case['categories'], allowedActions=case['allowedActions'],modelPackId='pack')),
                      sources=[dict(title=s['title'],text=s['body'],location=dict(pageId=bindings[s['key']])) for s in case['sources']],
                      citations=[dict(state='current',location=dict(pageId=p)) for p in source_ids])
        report['sourceLocations']={}
        report['sources']=[]
        report['citations']=[]
        for i,src in enumerate(case['sources']):
            for offset,field in enumerate(('title','body')):
                location=dict(pageId=bindings[src['key']],sourceRevision='2')
                if field=='body':location['blockId']='block-'+src['key']
                report['sourceLocations'][src['key']+':'+field]=location
                report['sources'].append(dict(number=i*2+offset+1,title=src['title'],text=src[field],location=location,state='current'))
        for support in case['expectedSupport']:
            if support=='absent':continue
            source=next(s for s in report['sources'] if s['location']==report['sourceLocations'][support])
            report['citations'].append(copy.deepcopy({k:source[k] for k in ('number','title','location','state')}))
        contract = dict(modelPackId='pack',reviewCases={case['id']:case})
        return report,contract

    def test_sealed_goldens_and_all_sixteen_semantic_poisons(self):
        for case in CORPUS['cases']:
            report,contract = self.fixture(case)
            self.assertEqual(RUNNER.validate_review_transfer_report(report,case['id'],contract),[])
            for poison in case['poisons']:
                with self.subTest(case=case['id'],poison=poison['kind']):
                    changed={**report,'rawDraft':json.dumps(poison['draft'])}
                    self.assertTrue(RUNNER.validate_review_transfer_report(changed,case['id'],contract))

    def test_actual_native_completion_and_budget_are_required(self):
        case=CORPUS['cases'][0];report,contract=self.fixture(case)
        for key,value in [('generationAttempts',0),('generationAttempts',2),('generatorInvocations',2),
                          ('nativeCallbackCompleted',False),('nativeCallbackError','failed'),
                          ('peakWorkerRSSBytes',0),('promptTokens',8000),('generatorOutputCap',2048),
                          ('generatorGrammarSHA256','unbound'),('finalStatus','applied'),
                          ('generationVersion','unknown')]:
            with self.subTest(field=key,value=value):
                self.assertTrue(RUNNER.validate_review_transfer_report({**report,key:value},case['id'],contract))

    def test_title_body_scope_citations_and_effects_are_checked(self):
        for case in CORPUS['cases']:
            report,contract=self.fixture(case)
            mutations=[]
            for field in ('title','text'):
                altered=copy.deepcopy(report)
                for source in altered['sources']:source[field]='wrong field'
                # Each fixture asks for either body or title; test its expected field.
                if field=='text':mutations.append(altered)
            for field,value in [('citations',[]),('sources',[]),('actions',[{'kind':'create_task'}]),('displayed','')]:
                mutations.append({**report,field:value})
            altered=copy.deepcopy(report);altered['sources'][0]['location']['pageId']='private';mutations.append(altered)
            for value in ('', '999'):
                altered=copy.deepcopy(report);altered['citations'][0]['location']['sourceRevision']=value;mutations.append(altered)
            altered=copy.deepcopy(report);altered['citations'][0]['location']['blockId']='wrong-block';mutations.append(altered)
            altered=copy.deepcopy(report);altered['citations'][0]['number']=999;mutations.append(altered)
            if any(s.endswith(':title') for s in case['expectedSupport']):
                altered=copy.deepcopy(report);altered['sources']=[s for s in altered['sources'] if s['location'].get('blockId')];mutations.append(altered)
            for altered in mutations:
                self.assertTrue(RUNNER.validate_review_transfer_report(altered,case['id'],contract))

    def test_duplicate_and_trailing_json_are_rejected(self):
        case=CORPUS['cases'][0];report,contract=self.fixture(case)
        for raw in ['{"claims":[],"claims":[],"actions":[]}',report['rawDraft']+'{}']:
            with self.assertRaises(ValueError):RUNNER.validate_review_transfer_report({**report,'rawDraft':raw},case['id'],contract)


if __name__=='__main__':unittest.main()
