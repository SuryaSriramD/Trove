"""Requirements provenance and evidence discipline for the Phase 0 handoff."""

import hashlib
import json
from pathlib import Path
import re
import unittest
import xml.etree.ElementTree as ET
import zipfile


ROOT = Path(__file__).resolve().parents[1]
LEDGER_PATH = ROOT / 'docs/phase-0/requirements-ledger.json'


class LedgerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.ledger = json.loads(LEDGER_PATH.read_text())
        cls.rows = cls.ledger['requirements']
        cls.by_id = {row['id']: row for row in cls.rows}

    def test_unique_ids_and_complete_ownership_chain(self):
        self.assertEqual(len(self.rows), len(self.by_id))
        allowed_releases = {'Phase0', 'P0', 'P1', 'P2', 'Excluded', 'Unscheduled'}
        for row in self.rows:
            with self.subTest(requirement=row['id']):
                self.assertIn(row['release'], allowed_releases)
                self.assertIn(row['scope'], {'contract', 'product', 'boundary'})
                self.assertIn(row['decisionState'], {'proposed', 'accepted', 'excluded'})
                self.assertIn(row['implementationStatus'], {'not_started', 'partial', 'implemented'})
                self.assertIn(row['verificationStatus'], {'not_run', 'failed', 'blocked', 'passed'})
                self.assertIn(row['owner'], self.ledger['ownerRoles'])
                self.assertIn(row['workPackage'], self.ledger['workPackages'])
                self.assertEqual(row['owner'], self.ledger['workPackages'][row['workPackage']]['owner'])
                self.assertTrue(row['requirement'])
                self.assertTrue(row['acceptanceCriteria'])
                self.assertTrue(row['sourceReferences'])
                self.assertTrue(row['tests'])
                for test_id in row['tests']:
                    self.assertIn(test_id, self.ledger['testCatalog'])
                    test = self.ledger['testCatalog'][test_id]
                    self.assertEqual(test['id'], test_id)
                    self.assertIn(test['availability'], {'planned', 'implemented'})
                    if test['availability'] == 'implemented':
                        self.assertTrue(test['path'])
                        self.assertTrue((ROOT / test['path']).is_file())

    def test_source_text_is_complete_and_matches_supplied_docx(self):
        namespaces = {'w': 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
        for name, source in self.ledger['sources'].items():
            with self.subTest(source=name):
                document = ROOT / source['path']
                text_path = ROOT / source['textPath']
                self.assertEqual(hashlib.sha256(document.read_bytes()).hexdigest(), source['sha256'])
                self.assertEqual(hashlib.sha256(text_path.read_bytes()).hexdigest(), source['textSha256'])
                with zipfile.ZipFile(document) as archive:
                    tree = ET.fromstring(archive.read('word/document.xml'))
                paragraphs = [''.join(node.text or '' for node in paragraph.findall('.//w:t', namespaces))
                              for paragraph in tree.findall('.//w:p', namespaces)]
                self.assertEqual('\n'.join(paragraphs).strip(), text_path.read_text().strip())

    def test_source_anchors_and_numbered_requirements_are_preserved(self):
        source_lines = {name: (ROOT / source['textPath']).read_text().splitlines()
                        for name, source in self.ledger['sources'].items()}
        for row in self.rows:
            for ref in row['sourceReferences']:
                if ref['document'] == 'USER':
                    continue
                self.assertEqual(source_lines[ref['document']][ref['line'] - 1], ref['anchor'])
        for text in source_lines.values():
            for line in text:
                match = re.fullmatch(r'(FR-\d{3}|ADR-\d{3}|R\d{2})', line)
                if match:
                    self.assertIn(match[1], self.by_id)
                match = re.fullmatch(r'(V\d{2}) — .+', line)
                if match:
                    self.assertIn(match[1], self.by_id)
        self.assertEqual({f'FR-{i:03}' for i in range(1, 22)}, {r for r in self.by_id if r.startswith('FR-')})
        self.assertEqual({f'V{i:02}' for i in range(1, 11)}, {r for r in self.by_id if re.fullmatch(r'V\d{2}', r)})

    def test_all_release_labelled_matrix_rows_are_mapped(self):
        for doc, start, end, prefix in [
            ('PRD', '16.1 Page editor and block system', '16.2 Editing interactions', 'EDITOR'),
            ('PRD', '16.2 Editing interactions', '16.2.1 Editor behavior and preservation (R04)', 'EDIT'),
            ('PRD', '16.3 Databases, properties and views', '16.3.1 Canonical sources and reusable views (R02)', 'DATABASE'),
            ('PRD', '16.4 Database property types', '16.4.1 Identity and location provenance (R03)', 'PROPERTY'),
            ('PRD', '16.5 Navigation, knowledge and history', '16.6 Import, export and portability', 'NAV'),
            ('PRD', '16.6 Import, export and portability', '16.7 Media, embeds and capture', 'PORTABLE'),
            ('PRD', '16.7 Media, embeds and capture', '16.8 Deterministic local automation (R07)', 'MEDIA'),
            ('PRD', '17. Local AI, transcription and intelligence capabilities', '18. External AI connectivity and intentional exclusions', 'AI'),
            ('TRD', '23. Block Type Registry', '24. Database Engine Registry', 'BLOCK'),
            ('TRD', '24.1 Property registry', '24.2 View registry', 'TYPE'),
            ('TRD', '24.2 View registry', '24.3 Query and view configuration', 'VIEW'),
            ('TRD', '27. Local AI Capability Matrix', '27.1 Model/runtime adapters', 'LOCALAI'),
        ]:
            lines = (ROOT / self.ledger['sources'][doc]['textPath']).read_text().splitlines()
            a = max(i for i, line in enumerate(lines) if line == start)
            b = next(i for i in range(a+1, len(lines)) if lines[i] == end)
            expected = []
            for i in range(a+1, b):
                if lines[i] in ('P0', 'P1', 'P2'):
                    expected.append((lines[i-1], lines[i]))
                elif lines[i] == 'P0 / P1':
                    expected.extend([(lines[i-1], 'P0'), (lines[i-1], 'P1')])
            actual = [(r['requirement'], r['release']) for r in self.rows if r['id'].startswith(f'{doc}-{prefix}-')]
            with self.subTest(section=start):
                self.assertCountEqual(expected, actual)

    def test_phase0_and_product_completion_are_independent(self):
        expected = {'BASELINE','IDENTITY','REGISTRIES','SERVICES','MUTATIONS','JOBS','LOCATORS','LEDGER','TASKS','REIMPORT','MEMORY','HANDOFF'}
        self.assertEqual({'PH0-' + x for x in expected}, {r['id'] for r in self.rows if r['release'] == 'Phase0'})
        for row in self.rows:
            if row['release'] == 'Phase0':
                self.assertEqual(row['scope'], 'contract')
            if row['scope'] == 'product' and row['verified']:
                # Product verification must never borrow a contract-only run.
                evidence = [self.ledger['evidenceCatalog'][e] for e in row['evidence']]
                self.assertTrue(any(e.get('scope') == 'product' for e in evidence))
        self.assertEqual(self.by_id['FR-018']['release'], 'P1')
        self.assertEqual(self.by_id['V05']['release'], 'P1')
        self.assertEqual(self.by_id['V07']['release'], 'P1')
        self.assertEqual(self.by_id['PH0-LOCATORS']['release'], 'Phase0')

    def test_verified_requires_implementation_and_covering_passing_evidence(self):
        reports = {}
        for evidence_id, evidence in self.ledger['evidenceCatalog'].items():
            report = json.loads((ROOT / evidence['path']).read_text())
            reports[evidence_id] = report
            self.assertTrue(report['recordedAt'])
            self.assertTrue(report['inputSha256'])
            self.assertTrue(all(re.fullmatch(r'[a-f0-9]{64}', digest)
                                for digest in report['inputSha256'].values()))
            if evidence_id == 'PH0-NATIVE-BASELINE':
                self.assertEqual(report['phase0Status'], 'verified')
                self.assertEqual(report['host']['system'], 'Darwin')
                self.assertEqual(report['host']['machine'], 'arm64')
                self.assertTrue(all(check['status'] == 'verified' for check in report['checks']))
                self.assertTrue(report['releaseGates'])
            else:
                self.assertEqual(report['result'], 'passed')
                self.assertTrue(report['tests'])
                self.assertTrue(all(result['result'] == 'passed' for result in report['tests']))
                self.assertTrue(all(check['result'] == 'passed' for check in report['checks']))
            for check in report['checks']:
                if 'log' in check:
                    log = ROOT / check['log']
                    self.assertEqual(hashlib.sha256(log.read_bytes()).hexdigest(), check['logSha256'])
                if 'exitCode' in check:
                    self.assertEqual(check['exitCode'], 0)

        # A blanket pass or one generic smoke test cannot verify all twelve gates.
        required_checks = {
            'PH0-BASELINE': {'host-architecture', 'manifest-consistency', 'storage', 'storage-linked-libraries',
                             'wails-build', 'wails-architecture', 'frontend-build', 'llama-build', 'llama-runtime',
                             'llama-architecture', 'whisper-build', 'whisper-runtime', 'whisper-architecture'},
            'PH0-IDENTITY': {'test_domain.DomainContractTests.test_complete_identity_fixture',
                             'test_domain.PersistenceContractTests.test_record_body_required_unique_and_foreign_keyed',
                             'test_domain.PersistenceContractTests.test_sql_view_membership_preserves_multiple_sources'},
            'PH0-REGISTRIES': {'test_domain.DomainContractTests.test_registry_schemas_and_all_examples',
                               'test_domain.DomainContractTests.test_unknown_and_future_content_preserved_inert',
                               'test_contracts.ContractTests.test_all_schema_definitions_are_valid'},
            'PH0-SERVICES': {'test_services.ServiceTests.test_stale_revision_and_missing_guard_never_overwrite',
                             'test_services.ServiceTests.test_atomic_batch_rolls_back_content_history_and_events',
                             'test_services.ServiceTests.test_cancellation_before_commit_and_revision_once_per_batch',
                             'test_services.ServiceTests.test_retry_after_lost_ack_replays_one_commit',
                             'go-contracts', 'typescript-contracts'},
            'PH0-MUTATIONS': {'test_services.ServiceTests.test_ai_approval_binds_exact_proposal_actor_policy_and_expiry',
                              'test_services.ServiceTests.test_scope_revoked_and_origin_spoof_rejected',
                              'test_services.ServiceTests.test_atomic_batch_rolls_back_content_history_and_events',
                              'test_services.ServiceTests.test_retry_after_lost_ack_replays_one_commit'},
            'PH0-JOBS': {'test_jobs.JobTests.test_jobs_survive_connection_restart',
                         'test_jobs.JobTests.test_claim_is_exclusive_and_old_worker_is_fenced',
                         'test_jobs.JobTests.test_cancel_crash_does_not_restart_and_terminal_is_final',
                         'test_jobs.JobTests.test_retry_budget_is_bounded',
                         'test_jobs.JobTests.test_frames_fragment_coalesce_and_reject_oversized_truncated_or_nonfinite',
                         'test_contracts.ContractTests.test_worker_limits_and_citation_identity_are_structural'},
            'PH0-LOCATORS': {'test_domain.DomainContractTests.test_citations_use_stable_identity_revision_and_timestamp',
                             'test_contracts.ContractTests.test_worker_limits_and_citation_identity_are_structural'},
            'PH0-LEDGER': {'test_ledger.LedgerTests.test_source_text_is_complete_and_matches_supplied_docx',
                           'test_ledger.LedgerTests.test_source_anchors_and_numbered_requirements_are_preserved',
                           'test_ledger.LedgerTests.test_all_release_labelled_matrix_rows_are_mapped',
                           'test_ledger.LedgerTests.test_unique_ids_and_complete_ownership_chain',
                           'test_ledger.LedgerTests.test_phase0_and_product_completion_are_independent',
                           'test_ledger.LedgerTests.test_verified_requires_implementation_and_covering_passing_evidence'},
            'PH0-TASKS': {'test_domain.DomainContractTests.test_tasks_share_record_and_completion',
                          'test_domain.PersistenceContractTests.test_sql_task_completion_has_one_value',
                          'test_contracts.ContractTests.test_tasks_and_memory_commands_share_domain_vocabulary'},
            'PH0-REIMPORT': {'test_domain.DomainContractTests.test_reimport_three_way_field_matrix',
                             'test_domain.DomainContractTests.test_reimport_deletion_ambiguity_and_omissions',
                             'test_domain.DomainContractTests.test_import_resolution_binds_exact_plan_and_all_conflicts'},
            'PH0-MEMORY': {'test_domain.DomainContractTests.test_memory_scope_approval_forgetting_and_source_invalidation',
                           'test_domain.DomainContractTests.test_memory_inheritance_is_opt_in_and_preferences_only',
                           'test_domain.DomainContractTests.test_memory_content_or_context_reference_is_unambiguous'},
            'PH0-HANDOFF': {'test_contracts.ContractTests.test_generator_has_no_drift',
                            'test_contracts.ContractTests.test_protocol_authoring_has_no_drift',
                            'test_contracts.ContractTests.test_protocol_examples_validate',
                            'test_domain.DomainContractTests.test_registry_schemas_and_all_examples',
                            'go-contracts', 'typescript-contracts', 'baseline-evidence'},
        }
        for row in self.rows:
            with self.subTest(requirement=row['id']):
                self.assertIsInstance(row['implemented'], bool)
                self.assertIsInstance(row['verified'], bool)
                self.assertEqual(row['implemented'], row['implementationStatus'] == 'implemented')
                self.assertEqual(row['verified'], row['verificationStatus'] == 'passed')
                for artifact in row['artifacts']:
                    self.assertTrue((ROOT / artifact).is_file(), artifact)
                if not row['verified']:
                    continue
                self.assertTrue(row['implemented'])
                self.assertTrue(row['artifacts'])
                self.assertTrue(row['evidence'])
                if row.get('phase') == 'Phase1':
                    self.assertTrue(row['verificationChecks'])
                    for check in row['verificationChecks']:
                        self.assertIn(check['evidence'], row['evidence'])
                        report = reports[check['evidence']]
                        self.assertIn(check['kind'], {'test', 'check'})
                        key = 'test' if check['kind'] == 'test' else 'name'
                        actual = {result[key]: result['result'] for result in report['tests' if check['kind'] == 'test' else 'checks']}
                        self.assertEqual(actual.get(check['name']), 'passed')
                if row['release'] == 'Phase0':
                    concrete = row['verificationChecks']
                    self.assertTrue(required_checks[row['id']] <= {check['name'] for check in concrete})
                    for check in concrete:
                        self.assertIn(check['evidence'], row['evidence'])
                        report = reports[check['evidence']]
                        if check['kind'] == 'test':
                            actual = {result['test']: result['result'] for result in report['tests']}
                            self.assertEqual(actual.get(check['name']), 'passed')
                        elif check['kind'] == 'check':
                            actual = {result['name']: result['result'] for result in report['checks']}
                            self.assertEqual(actual.get(check['name']), 'passed')
                        elif check['kind'] == 'baseline':
                            actual = {result['id']: result['status'] for result in report['checks']}
                            self.assertEqual(actual.get(check['name']), 'verified')
                        else:
                            self.fail('Unknown evidence selector kind: ' + check['kind'])
                    # The verifier fingerprints the final state after tests finish.
                    # Checking current ledger/test hashes here would make a rerun
                    # unable to replace the previous run after a legitimate edit.
                    fingerprinted = {path for report in reports.values() for path in report['inputSha256']}
                    self.assertTrue(set(row['artifacts']) <= fingerprinted)
                covered = set()
                for evidence_id in row['evidence']:
                    evidence = self.ledger['evidenceCatalog'][evidence_id]
                    self.assertEqual(evidence['result'], 'passed')
                    self.assertTrue(evidence['command'])
                    self.assertTrue(evidence['recordedAt'])
                    self.assertTrue((ROOT / evidence['path']).is_file())
                    covered.update(evidence['tests'])
                self.assertTrue(set(row['tests']) <= covered, 'Every required test mapping needs evidence')

    def test_cited_audit_ids_are_expanded_without_claiming_missing_report(self):
        audit_ids = set()
        for n in range(1, 9):
            row = self.by_id[f'R{n:02}']
            self.assertEqual(row['amendmentFamily'], row['id'])
            self.assertTrue(row['auditIds'])
            self.assertTrue(all(re.fullmatch(r'[A-Z]\d{2,3}', x) for x in row['auditIds']))
            audit_ids.update(row['auditIds'])
        self.assertTrue({'M001','M026','D044','E080','W065','A053','T01','T10'} <= audit_ids)
        unavailable = {x['id']: x for x in self.ledger['unavailableSources']}
        self.assertEqual(unavailable['R00']['state'], 'not_supplied')
        self.assertEqual(unavailable['R00']['referencedBehaviorCount'], 307)
        self.assertTrue(unavailable['R00']['consequence'])


if __name__ == '__main__':
    unittest.main()
