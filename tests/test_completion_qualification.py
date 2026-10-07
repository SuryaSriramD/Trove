"""Evidence substitution and missing-gate tests; no product/model/device runs."""
import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import completion_qualification as q


class QualificationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.policy = q.load_policy()
        self.candidate = self.make_candidate()
        self.record = self.root / 'candidate.json'
        q.write_new(self.record, self.candidate)
        (self.root / 'checks').mkdir()

    def make_candidate(self):
        bundle = self.root / 'app'
        bundle.mkdir()
        (bundle / 'binary').write_bytes(b'fixture candidate bytes, not a product build')
        archive = self.root / 'source.tar'
        archive.write_bytes(b'fixture source identity')
        fixture = self.root / 'fixture.bin'
        fixture.write_bytes(b'independent fixture source')
        plan = q.pending_plan(self.policy, 'darwin-arm64', 'P0')
        value = dict(schemaVersion=1, qualificationId='10000000-0000-4000-8000-000000000001',
                     frozenAt=(datetime.now(timezone.utc)-timedelta(minutes=1)).isoformat(),
                     sourceCommit='a'*40, sourceTree='b'*40, sourceArchive=q.bind_path(archive),
                     target='darwin-arm64', tier='P0', environment={'hardware':'synthetic test fixture','osVersion':'fixture'},
                     policySha256=q.digest_value(self.policy), acceptanceCatalogSha256=q.digest_value(q.acceptance_catalog(self.policy,'darwin-arm64','P0')), bundle=q.bind_path(bundle),
                     assets={k:[q.bind_path(fixture)] for k in ('helpers','models','fixtures')},
                     requiredChecks=[c['id'] for c in q.selected_checks(self.policy,'darwin-arm64','P0')],
                     prerequisites=plan['prerequisites'], releaseApproved=False)
        value['candidateId'] = q.digest_value(value)
        return value

    def check(self, name):
        return next(c for c in self.policy['checks'] if c['id']==name)

    def observation(self, name='p0-native-editor'):
        check = self.check(name)
        raw = self.root / (name+'.txt')
        raw.write_text('Synthetic unit-test evidence; never a real observation.')
        return dict(candidateId=self.candidate['candidateId'],policySha256=q.digest_value(self.policy),
                    checkId=name,observedAt=q.stamp(),observer='unit test fixture',
                    environment={'target':self.candidate['target'],**self.candidate['environment']},
                    scenarios=[dict(**s,result='passed',observed='Synthetic independently expected fixture outcome.',artifactIds=['raw']) for s in check['scenarios']],
                    prerequisites={p:dict(status='available',description='Synthetic test availability.',artifactIds=['raw']) for p in check['prerequisites']},
                    artifacts={'raw':q.bind_path(raw)})

    def test_all_targets_tiers_are_cumulative_and_pending(self):
        for target in self.policy['targets']:
            previous=set()
            for tier in self.policy['tiers']:
                plan=q.pending_plan(self.policy,target,tier)
                ids={c['id'] for c in plan['checks']}
                self.assertTrue(previous <= ids)
                previous=ids
                self.assertEqual(plan['status'],'pending')
                self.assertFalse(plan['releaseApproved'])
                self.assertTrue(all(c['status']=='pending' for c in plan['checks']))
                self.assertEqual('windows-native' in ids,target=='windows-amd64')
                self.assertEqual('linux-native' in ids,target=='linux-amd64')
        self.assertEqual(q.pending_plan(self.policy,'darwin-arm64','P0')['prerequisites']['independent-clean-target']['status'],'not_available')

    def test_policy_go_selectors_exist_exactly_in_source(self):
        import re
        source='\n'.join(p.read_text() for p in (ROOT/'internal/workspace').glob('*_test.go'))
        names=set(re.findall(r'^func (Test\w+)\(',source,re.M))
        for c in self.policy['checks']:
            if c['kind']=='go':
                self.assertTrue(set(c['tests']) <= names)
                self.assertFalse(any(t.startswith('TestReal') for t in c['tests']))

    def test_schema_accepts_real_record_shapes(self):
        import jsonschema
        schema=json.loads((ROOT/'docs/completion/qualification.schema.json').read_text())
        validator=jsonschema.Draft202012Validator(schema)
        validator.check_schema(schema)
        observation=self.observation()
        check=self.check(observation['checkId'])
        path=self.root/'observation.json'
        q.write_new(path,observation)
        receipt=q.receipt(self.candidate,self.policy,check,'passed',[q.bind_path(path)])
        for value in [self.candidate,observation,receipt]:
            validator.validate(value)

    def test_acceptance_inventory_accounts_every_applicable_criterion_and_all_backlog(self):
        for target in self.policy['targets']:
            for tier in self.policy['tiers']:
                catalog=q.acceptance_catalog(self.policy,target,tier)
                self.assertEqual(sum(r['id'].startswith('backlog:') for r in catalog['rows']),51)
                self.assertEqual(len(catalog['rows']),len({r['id'] for r in catalog['rows']}))
                coverage=q.acceptance_coverage(self.policy,catalog,{c['id']:'passed' for c in self.policy['checks']})
                self.assertEqual(coverage['result'],'pending')
                self.assertEqual(coverage['passedCriteria'],0)
                self.assertGreater(coverage['requiredCriteria'],205)

    def test_acceptance_boolean_or_broad_scenario_cannot_bypass_unmapped_criteria(self):
        catalog=q.acceptance_catalog(self.policy,'darwin-arm64','P0')
        criterion=next(r for r in catalog['rows'] if r['applicability']=='required')
        policy=copy.deepcopy(self.policy)
        for mapping in [True,{'complete':True},{'criterionSha256':q.digest_value(criterion),'cases':[{'checkId':'p0-native-editor','scenarioId':'preservation'}]}]:
            policy['acceptanceCases']={'darwin-arm64/P0':{criterion['id']:mapping}}
            coverage=q.acceptance_coverage(policy,catalog,{'p0-native-editor':'passed'})
            self.assertEqual(coverage['result'],'pending')
            self.assertEqual(coverage['passedCriteria'],0)
            self.assertEqual(next(r for r in coverage['criteria'] if r['id']==criterion['id'])['result'],'invalid')

    def test_exact_dedicated_criterion_mapping_requires_passing_bound_evidence(self):
        catalog=q.acceptance_catalog(self.policy,'darwin-arm64','P0')
        criterion=next(r for r in catalog['rows'] if r['applicability']=='required')
        policy=copy.deepcopy(self.policy)
        check=next(c for c in policy['checks'] if c['id']=='p0-native-editor')
        check['scenarios'].append({'id':'exact-criterion','expected':criterion['expected']})
        policy['acceptanceCases']={'darwin-arm64/P0':{criterion['id']:{'criterionSha256':q.digest_value(criterion),'cases':[{'checkId':check['id'],'scenarioId':'exact-criterion'}]}}}
        for state in ['pending','skipped','failed','unqualified']:
            self.assertEqual(q.acceptance_coverage(policy,catalog,{check['id']:state})['passedCriteria'],0)
        result=q.acceptance_coverage(policy,catalog,{check['id']:'passed'})
        self.assertEqual(result['passedCriteria'],1)
        self.assertEqual(result['result'],'pending')

    def test_qualified_predicate_requires_full_coverage_and_every_exact_record(self):
        # Isolated synthetic policy/catalog, never persisted as product evidence.
        criterion={'id':'requirement:FIXTURE:1','sourceId':'FIXTURE','release':'P0','applicability':'required','expected':'Exact synthetic expected bytes.'}
        catalog={'target':'darwin-arm64','tier':'P0','rows':[criterion],'backlogItems':51}
        self.policy=copy.deepcopy(self.policy)
        base=copy.deepcopy(self.check('p0-native-editor'))
        base.update(id='synthetic-release',prerequisites=[],assets=[],scenarios=[{'id':'exact','expected':criterion['expected']}])
        software=copy.deepcopy(base);software.update(id='synthetic-software',scope='software')
        self.policy['checks']=[base,software]
        self.policy['acceptanceCases']={'darwin-arm64/P0':{criterion['id']:{'criterionSha256':q.digest_value(criterion),'cases':[{'checkId':base['id'],'scenarioId':'exact'}]}}}
        self.candidate.update(policySha256=q.digest_value(self.policy),acceptanceCatalogSha256=q.digest_value(catalog),requiredChecks=[base['id'],software['id']],prerequisites={})
        self.candidate.pop('candidateId');self.candidate['candidateId']=q.digest_value(self.candidate)
        self.record.write_text(json.dumps(self.candidate))
        for check in [base,software]:
            value=self.observation(check['id'])
            directory=self.root/'checks'/check['id'];directory.mkdir()
            raw=directory/'observation.json';q.write_new(raw,value)
            q.write_new(directory/'receipt.json',q.receipt(self.candidate,self.policy,check,'passed',[q.bind_path(raw)]))
        with patch.object(q,'acceptance_catalog',return_value=catalog):
            result=q.status(self.record,self.policy)
            self.assertTrue(result['releaseQualified'])
            self.assertFalse(result['releaseApproved'])
            self.assertEqual(result['acceptanceCoverage']['result'],'complete')
            (self.root/'synthetic-software.txt').write_text('substituted evidence')
            result=q.status(self.record,self.policy)
            self.assertFalse(result['releaseQualified'])
            self.assertFalse(result['releaseApproved'])

    def test_empty_status_never_passes_and_is_read_only(self):
        before=sorted(str(p) for p in self.root.rglob('*'))
        result=q.status(self.record,self.policy)
        self.assertEqual(result['releaseEligibility'],'unqualified')
        self.assertFalse(result['releaseApproved'])
        self.assertTrue(all(c['result']=='pending' for c in result['checks']))
        self.assertEqual(before,sorted(str(p) for p in self.root.rglob('*')))

    def test_changed_bundle_added_file_or_helper_rejects_candidate(self):
        (self.root/'app/binary').write_bytes(b'changed')
        self.assertTrue(q.validate_candidate(self.candidate,self.policy))
        (self.root/'app/binary').write_bytes(b'fixture candidate bytes, not a product build')
        (self.root/'app/extra').write_bytes(b'new helper')
        self.assertTrue(q.validate_candidate(self.candidate,self.policy))
        (self.root/'app/extra').unlink()
        (self.root/'fixture.bin').write_bytes(b'changed model/fixture')
        self.assertTrue(q.validate_candidate(self.candidate,self.policy))

    def test_source_archive_loss_cannot_pass(self):
        (self.root/'source.tar').unlink()
        self.assertEqual(q.status(self.record,self.policy)['status'],'invalid')

    def test_drop_required_check_rehash_still_rejected(self):
        value=copy.deepcopy(self.candidate)
        value['requiredChecks'].pop()
        value.pop('candidateId')
        value['candidateId']=q.digest_value(value)
        self.assertIn('required checks omitted, reordered or substituted',q.validate_candidate(value,self.policy))

    def test_policy_change_invalidates_existing_candidate(self):
        policy=copy.deepcopy(self.policy)
        policy['checks'][0]['tests'].pop()
        self.assertTrue(q.validate_candidate(self.candidate,policy))

    def test_candidate_cannot_approve_release(self):
        value=copy.deepcopy(self.candidate)
        value['releaseApproved']=True
        self.assertTrue(q.validate_candidate(value,self.policy))

    def test_observation_requires_identity_exact_scenarios_and_artifacts(self):
        base=self.observation()
        check=self.check(base['checkId'])
        self.assertEqual(q.validate_observation(base,self.candidate,self.policy,check),[])
        for mutate in [lambda v:v.update(candidateId='f'*64),lambda v:v.update(checkId='wrong'),
                       lambda v:v['scenarios'].pop(),lambda v:v['scenarios'].append(v['scenarios'][0]),
                       lambda v:v['scenarios'][0].update(expected='weakened expectation'),
                       lambda v:v['scenarios'][0].update(artifactIds=['missing']),
                       lambda v:v.update(artifacts={})]:
            value=copy.deepcopy(base);mutate(value)
            self.assertTrue(q.validate_observation(value,self.candidate,self.policy,check))

    def test_stale_or_future_manual_evidence_rejected(self):
        value=self.observation();check=self.check(value['checkId'])
        for time in [datetime.now(timezone.utc)-timedelta(days=1),datetime.now(timezone.utc)+timedelta(days=1)]:
            value['observedAt']=time.isoformat()
            self.assertTrue(q.validate_observation(value,self.candidate,self.policy,check))

    def test_fixture_evidence_substitution_after_observation_rejected(self):
        value=self.observation();check=self.check(value['checkId'])
        Path(value['artifacts']['raw']['path']).write_text('different observation')
        self.assertTrue(q.validate_observation(value,self.candidate,self.policy,check))

    def test_missing_prerequisite_and_not_available_do_not_pass(self):
        value=self.observation('clean-install');check=self.check('clean-install')
        value['prerequisites']['independent-clean-target']['status']='not_available'
        self.assertEqual(q.observation_result(value),'unqualified')
        value['prerequisites']={}
        self.assertTrue(q.validate_observation(value,self.candidate,self.policy,check))

    def test_all_unsuccessful_manual_states_remain_unqualified(self):
        for state in ['failed','skipped','pending','not_available','unqualified']:
            value=self.observation()
            value['scenarios'][0]['result']=state
            self.assertNotEqual(q.observation_result(value),'passed')

    def test_missing_assets_cannot_be_supplied_only_by_observation_claim(self):
        value=self.observation('p0-local-ai');check=self.check(value['checkId'])
        candidate=copy.deepcopy(self.candidate);candidate['assets']['models']=[]
        self.assertTrue(q.validate_observation(value,candidate,self.policy,check))

    def test_named_go_selectors_require_all_exact_events(self):
        check=self.check('p0-service-regressions')
        events=[dict(Package='trove.local/phase0/internal/workspace',Test=t,Action='pass') for t in check['tests']] + [dict(Package='trove.local/phase0/internal/workspace',Action='pass')]
        log=lambda values:'\n'.join(json.dumps(v) for v in values)
        self.assertEqual(q.go_result(log(events),check,0)[0],'passed')
        for values,code in [(events[:-1],0),(events+[events[0]],0),(events,1),([],0)]:
            self.assertEqual(q.go_result(log(values),check,code)[0],'failed')
        for action in ['skip','fail']:
            values=copy.deepcopy(events);values[0]['Action']=action
            self.assertEqual(q.go_result(log(values),check,0)[0],'failed')
        values=copy.deepcopy(events);values[0]['Test']+='Other'
        self.assertEqual(q.go_result(log(values),check,0)[0],'failed')
        values=copy.deepcopy(events);values[0]['Package']='other/package'
        self.assertEqual(q.go_result(log(values),check,0)[0],'failed')

    def test_automated_receipt_cannot_swap_command_tool_or_skip_output(self):
        check=self.check('p0-service-regressions')
        tool=q.bind_path(self.root/'fixture.bin')
        raw=self.root/'go.jsonl'
        events=[dict(Package='trove.local/phase0/internal/workspace',Test=t,Action='pass') for t in check['tests']]+[dict(Package='trove.local/phase0/internal/workspace',Action='pass')]
        raw.write_text('\n'.join(json.dumps(v) for v in events))
        command=[tool['path'],'test','-json','-count=1','-race','-timeout=13m','-tags=fts5 sqlite_omit_load_extension',check['package'],'-run','^('+'|'.join(check['tests'])+')$']
        value=q.receipt(self.candidate,self.policy,check,'passed',[q.bind_path(raw)],command=command,tool=tool,exitCode=0,errors=[],environment={'target':self.candidate['target'],**self.candidate['environment']})
        self.assertEqual(q.validate_receipt(value,self.candidate,self.policy,check),[])
        changed=copy.deepcopy(value);changed['command'][-1]='.*'
        self.assertTrue(q.validate_receipt(changed,self.candidate,self.policy,check))
        changed=copy.deepcopy(value);changed['tool']['sha256']='f'*64
        self.assertTrue(q.validate_receipt(changed,self.candidate,self.policy,check))
        events[0]['Action']='skip';raw.write_text('\n'.join(json.dumps(v) for v in events))
        value['evidence']=[q.bind_path(raw)]
        self.assertTrue(q.validate_receipt(value,self.candidate,self.policy,check))

    def test_unfilled_observation_template_is_not_evidence(self):
        from types import SimpleNamespace
        value=q.observation_template(SimpleNamespace(record=self.record,check='p0-native-editor'),self.policy)
        self.assertTrue(all(s['result']=='pending' for s in value['scenarios']))
        self.assertEqual(value['artifacts'],{})
        self.assertTrue(q.validate_observation(value,self.candidate,self.policy,self.check('p0-native-editor')))

    def test_receipt_reparse_prevents_result_or_candidate_substitution(self):
        observation=self.observation();check=self.check(observation['checkId'])
        path=self.root/'observation.json';q.write_new(path,observation)
        value=q.receipt(self.candidate,self.policy,check,'passed',[q.bind_path(path)])
        self.assertEqual(q.validate_receipt(value,self.candidate,self.policy,check),[])
        value['candidateId']='c'*64
        self.assertTrue(q.validate_receipt(value,self.candidate,self.policy,check))
        value['candidateId']=self.candidate['candidateId'];value['result']='unqualified'
        self.assertTrue(q.validate_receipt(value,self.candidate,self.policy,check))

    def test_dropped_receipt_becomes_pending_even_after_prior_pass(self):
        observation=self.observation();check=self.check(observation['checkId'])
        directory=self.root/'checks'/check['id'];directory.mkdir()
        raw=directory/'observation.json';q.write_new(raw,observation)
        q.write_new(directory/'receipt.json',q.receipt(self.candidate,self.policy,check,'passed',[q.bind_path(raw)]))
        rows=lambda:{c['id']:c for c in q.status(self.record,self.policy)['checks']}
        self.assertEqual(rows()[check['id']]['result'],'passed')
        (directory/'receipt.json').unlink()
        self.assertEqual(rows()[check['id']]['result'],'pending')

    def test_symlink_and_reused_output_refused(self):
        link=self.root/'link';link.symlink_to(self.root/'fixture.bin')
        with self.assertRaises(ValueError):q.bind_path(link)
        with self.assertRaises(FileExistsError):q.write_new(self.record,{})

    def test_duplicate_json_fields_refused(self):
        path=self.root/'duplicate.json';path.write_text('{"result":"failed","result":"passed"}')
        with self.assertRaises(ValueError):q.read_json(path)


if __name__=='__main__':
    unittest.main()
