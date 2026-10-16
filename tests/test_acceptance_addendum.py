"""Accepted-scope omission/substitution tests; never supplies product evidence."""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import completion_qualification as q


class AddendumTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.policy = q.load_policy()

    def test_all_addendum_ids_retain_exact_accepted_words_and_pending_state(self):
        for tier, count in [('P0', 411), ('P1', 659), ('P2', 697)]:
            catalog = q.acceptance_catalog(self.policy, 'darwin-arm64', tier)
            added = [r for r in catalog['rows'] if r['id'].startswith('addendum:')]
            self.assertEqual(len(added), 36)
            self.assertEqual(len({r['sourceId'] for r in added}), 36)
            coverage = q.acceptance_coverage(self.policy, catalog)
            self.assertEqual(coverage['requiredCriteria'], count)
            self.assertEqual(coverage['mappedCriteria'], count)
            self.assertEqual(coverage['unmappedCriteria'], [])
            self.assertEqual(coverage['errors'], [])
            self.assertEqual(coverage['passedCriteria'], 0)
            self.assertTrue(all(r['result'] != 'invalid' for r in coverage['criteria']))
            mobile = next(r for r in added if r['sourceId'] == 'G11-A1')
            self.assertIn('not a mobile implementation pass', mobile['expected'])

    def test_p1_p2_exact_procedures_and_resources_are_bound(self):
        for tier in ('P1', 'P2'):
            catalog = q.acceptance_catalog(self.policy, 'darwin-arm64', tier)
            required = {r['id']: r for r in catalog['rows'] if r['applicability'] == 'required'}
            checks = {c['id']: c for c in q.selected_checks(self.policy, 'darwin-arm64', tier)}
            for criterion, mapping in self.policy['acceptanceCases']['darwin-arm64/' + tier].items():
                for ref in mapping['cases']:
                    check = checks[ref['checkId']]
                    if check['kind'] != 'observation':
                        continue
                    scenario = next(s for s in check['scenarios'] if s['id'] == ref['scenarioId'])
                    procedure = q.observation_procedure(scenario, check)
                    self.assertEqual(procedure['criterionSha256'], q.digest_value(required[criterion]))
                    self.assertEqual(procedure['expected'], required[criterion]['expected'])
                    self.assertGreaterEqual(len(procedure['steps']), 4)
                    self.assertGreaterEqual(len(procedure['assertions']), 3)
                    self.assertNotIn('result', procedure)
            broad = {c['id']: 'passed' for c in checks.values() if '-acceptance-' not in c['id']}
            self.assertEqual(q.acceptance_coverage(self.policy, catalog, broad)['passedCriteria'], 2)

    def test_deleting_one_mapping_does_not_borrow_a_sibling_pass(self):
        policy = copy.deepcopy(self.policy)
        del policy['acceptanceCases']['darwin-arm64/P2']['addendum:G12-A3']
        coverage = q.acceptance_coverage(policy, q.acceptance_catalog(policy, 'darwin-arm64', 'P2'),
                                         {c['id']: 'passed' for c in policy['checks']})
        self.assertEqual(coverage['unmappedCriteria'], ['addendum:G12-A3'])
        self.assertEqual(coverage['result'], 'pending')
        self.assertEqual(coverage['passedCriteria'], 696)

    def test_unavailable_installation_and_live_prerequisites_are_separate(self):
        plan = q.pending_plan(self.policy, 'darwin-arm64', 'P2')
        self.assertEqual(plan['prerequisites']['independent-clean-target']['status'], 'not_available')
        for key in ('connector-github-test-resource', 'connector-gmail-client-registration',
                    'connector-gmail-policy-readiness', 'native-os-permissions'):
            self.assertEqual(plan['prerequisites'][key]['status'], 'unknown')
        checks = {c['id']: c for c in self.policy['checks']}
        self.assertEqual(checks['p1-acceptance-extended-layouts']['prerequisites'], [])
        self.assertIn('independent-clean-target', checks['p0-acceptance-extended-encrypted-install']['prerequisites'])
        self.assertIn('connector-github-test-resource', checks['p1-acceptance-extended-connector-github-writes']['prerequisites'])
        self.assertEqual(checks['p1-acceptance-extended-skill-import-safety']['prerequisites'], [])
        self.assertEqual(checks['p0-acceptance-extended-meeting-retention']['prerequisites'], [])
        self.assertEqual(checks['p1-acceptance-extended-imports-extended']['prerequisites'], [])
        self.assertIn('models', checks['p1-acceptance-extended-offline-model-continuity']['assets'])

    def test_every_connector_parent_needs_all_six_independent_adapter_receipts(self):
        checks = {c['id']: c for c in self.policy['checks']}
        adapters = ('google-calendar', 'google-drive', 'gmail', 'github', 'notion', 'outbound-mcp')
        catalog = q.acceptance_catalog(self.policy, 'darwin-arm64', 'P2')
        for number in range(1, 5):
            key = 'addendum:G12-A' + str(number)
            refs = self.policy['acceptanceCases']['darwin-arm64/P2'][key]['cases']
            self.assertEqual(len(refs), 6)
            for adapter in adapters:
                check = next(checks[r['checkId']] for r in refs
                             if checks[r['checkId']]['id'].startswith('p1-acceptance-extended-connector-' + adapter + '-'))
                self.assertIn('connector-' + adapter + '-policy-readiness', check['prerequisites'])
                self.assertIn('connector-' + adapter + '-test-resource', check['prerequisites'])
                self.assertNotIn('connector-test-resources', check['prerequisites'])
            receipts = {c: 'passed' for c in checks}
            del receipts[refs[-1]['checkId']]
            coverage = q.acceptance_coverage(self.policy, catalog, receipts)
            self.assertEqual(next(r['result'] for r in coverage['criteria'] if r['id'] == key), 'pending')

    def test_other_platforms_keep_added_obligations_visible_without_macos_passes(self):
        for target in ('windows-amd64', 'linux-amd64'):
            catalog = q.acceptance_catalog(self.policy, target, 'P2')
            added = [r for r in catalog['rows'] if r['id'].startswith('addendum:')]
            self.assertEqual(len(added), 36)
            self.assertTrue(all(r['applicability'] == 'other_platform' for r in added))
            self.assertFalse(q.pending_plan(self.policy, target, 'P2')['releaseQualified'])

    def temporary_manifest(self, root):
        policy = copy.deepcopy(self.policy)
        source = root / 'docs/PRD_SCOPE_ADDENDUM.md'
        source.parent.mkdir(parents=True)
        source.write_bytes((ROOT / 'docs/PRD_SCOPE_ADDENDUM.md').read_bytes())
        path = root / policy['acceptanceAddendum']['path']
        path.parent.mkdir(parents=True)
        path.write_bytes((ROOT / policy['acceptanceAddendum']['path']).read_bytes())
        return policy, path, source

    def test_source_manifest_hash_and_source_text_cannot_be_substituted(self):
        mutations = [
            lambda m: m['criteria'].pop(),
            lambda m: m['criteria'].__setitem__(1, copy.deepcopy(m['criteria'][0])),
            lambda m: m['criteria'][0].update(expected='A broad smoke test passed.'),
            lambda m: m['criteria'][0].update(context=''),
            lambda m: m['criteria'][0].update(targets=['unknown']),
            lambda m: m.update(source='../outside.md'),
        ]
        for mutate in mutations:
            with self.subTest(mutate=mutate), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp); policy, path, source = self.temporary_manifest(root)
                value = q.read_json(path); mutate(value); path.write_text(json.dumps(value))
                policy['acceptanceAddendum']['sha256'] = q.file_identity(path)['sha256']
                with patch.object(q, 'ROOT', root), self.assertRaises(ValueError):
                    q.addendum_acceptance(policy, 'darwin-arm64', ['P0', 'P1', 'P2'])
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); policy, path, source = self.temporary_manifest(root)
            source.write_text(source.read_text().replace('without losing either retained version', 'without checking retained versions'))
            with patch.object(q, 'ROOT', root), self.assertRaisesRegex(ValueError, 'source changed'):
                q.addendum_acceptance(policy, 'darwin-arm64', ['P0'])
            path.write_text(path.read_text() + ' ')
            with patch.object(q, 'ROOT', root), self.assertRaisesRegex(ValueError, 'manifest changed'):
                q.addendum_acceptance(policy, 'darwin-arm64', ['P0'])

    def test_full_policy_cannot_remove_addendum_reference(self):
        with tempfile.TemporaryDirectory() as tmp:
            policy = copy.deepcopy(self.policy); del policy['acceptanceAddendum']
            path = Path(tmp) / 'policy.json'; path.write_text(json.dumps(policy))
            with self.assertRaisesRegex(ValueError, 'requires the accepted addendum'):
                q.load_policy(path)
            with self.assertRaisesRegex(ValueError, 'requires the accepted addendum'):
                q.addendum_acceptance(policy, 'darwin-arm64', ['P0'])


if __name__ == '__main__':
    unittest.main()
