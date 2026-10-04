import copy
import importlib.util
import json
from pathlib import Path
import sqlite3
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('reference_domain', ROOT / 'contracts/reference_domain.py')
domain = importlib.util.module_from_spec(spec)
spec.loader.exec_module(domain)


def fixture():
    return json.loads((ROOT / 'contracts/examples/domain-workspace.json').read_text())


def uid(n):
    return '0199a000-0000-7000-8000-' + format(n, '012x')


class DomainContractTests(unittest.TestCase):
    def test_complete_identity_fixture(self):
        self.assertTrue(domain.validate_snapshot(fixture()))

    def test_registry_schemas_and_all_examples(self):
        from jsonschema import Draft202012Validator
        schema = json.loads((ROOT / 'contracts/domain.schema.json').read_text())
        Draft202012Validator.check_schema(schema)
        registry = json.loads((ROOT / 'contracts/registries.json').read_text())
        domain.schema_validator('Registry').validate(registry)
        examples = json.loads((ROOT / 'contracts/examples/domain-registry-values.json').read_text())
        expected = set(domain.registry_entries())
        actual = {(e['family'], e['type'], e['schemaVersion']) for e in examples}
        self.assertEqual(actual, expected)
        self.assertEqual(len(actual), len(examples))
        self.assertFalse(domain.registry_entries()[('property', 'title', 1)]['ai']['writable'])
        for e in examples:
            with self.subTest(family=e['family'], type=e['type']):
                domain.validate_registry_value(e['family'], e['type'], e['schemaVersion'], e['data'])
                if 'config' in e:
                    domain.validate_registry_value(e['family'], e['type'], e['schemaVersion'], e['config'], config=True)
        for e in registry['entries']:
            self.assertIn(e['schemaRef'].split('/')[-1], schema['$defs'])
            self.assertEqual(e['migration']['fromVersions'], [])
            self.assertTrue(e['fallback']['rawRequired'])
            self.assertTrue(e['ai']['untrusted'])

    def test_revision_precision_and_identity(self):
        domain.validate_revision('9007199254740993')
        domain.validate_revision('9223372036854775807')
        for invalid in ('0', '01', '-1', '1.0', '9223372036854775808', 1, '１'):
            with self.subTest(value=invalid), self.assertRaises(ValueError):
                domain.validate_revision(invalid)
        for invalid in ('0199a000-0000-4000-8000-000000000001', 'not-a-uuid'):
            self.assertFalse(domain.schema_validator('Id').is_valid(invalid))

    def test_tasks_share_record_and_completion(self):
        snapshot = fixture()
        refs = [b for b in snapshot['blocks'] if b['type'] == 'todo' and b['data']['taskRecordId'] == uid(60)]
        self.assertEqual(len(refs), 2)
        self.assertEqual(domain.task_projection(snapshot, uid(60))['completed'], False)
        snapshot['records'][0]['values'][0]['value'] = uid(51)
        snapshot['records'][0]['revision'] = '2'
        next(p for p in snapshot['pages'] if p['id'] == uid(12))['revision'] = '2'
        domain.validate_snapshot(snapshot)
        self.assertTrue(all(domain.task_projection(snapshot, b['data']['taskRecordId'])['completed'] for b in refs))
        self.assertEqual(len(snapshot['records']), 2)
        self.assertEqual(domain.extracted_text(refs[0], snapshot), 'Ship phase zero')
        # Promoted record stays in its original collection, with the same body.
        self.assertEqual(snapshot['records'][1]['collectionId'], uid(31))
        self.assertTrue(domain.task_projection(snapshot, uid(61))['completed'])

    def test_views_reference_single_record_collection(self):
        snapshot = fixture()
        self.assertEqual(snapshot['views'][0]['collectionId'], snapshot['views'][1]['collectionId'])
        self.assertEqual(len(snapshot['containerCollections']), 2)
        snapshot['views'][1]['query']['visiblePropertyIds'] = [uid(43)]
        with self.assertRaisesRegex(ValueError, 'view_property_collection_mismatch'):
            domain.validate_snapshot(snapshot)

    def test_reject_dangling_and_duplicate_identity(self):
        changes = [
            lambda s: s['records'][1].update(pageId=s['records'][0]['pageId']),
            lambda s: s['blocks'][0]['data'].update(taskRecordId=uid(999)),
            lambda s: s['pages'][0].update(parentPageId=s['pages'][0]['id']),
            lambda s: s['records'][0].update(revision='2'),
        ]
        for change in changes:
            snapshot = fixture()
            change(snapshot)
            with self.assertRaises(ValueError):
                domain.validate_snapshot(snapshot)

    def test_unknown_and_future_content_preserved_inert(self):
        raw = {'html': '<script>neverExecute()</script>', 'nested': ['é', '🙂', {'x': None}]}
        fallback = domain.preserve_unknown('future.tabs', 9, raw, 'A preserved tab', 'future_version')
        roundtrip = json.loads(json.dumps(fallback, ensure_ascii=False))
        self.assertEqual(roundtrip['raw'], raw)
        self.assertEqual(roundtrip['readableText'], 'A preserved tab')
        with self.assertRaisesRegex(ValueError, 'requires_fallback'):
            domain.validate_registry_value('block', 'future.tabs', 9, raw)
        domain.validate_registry_value('block', 'unsupported_external', 1, fallback)

    def test_table_spans_coverage(self):
        table = {'rowCount': 1, 'columnCount': 2, 'headerRows': 0, 'headerColumns': 0,
                 'columnWidths': [100, 100], 'cells': [
                     {'id': uid(300), 'row': 0, 'column': 0, 'rowSpan': 1, 'colSpan': 2,
                      'richText': [{'text': 'Merged'}], 'preMergeCells': [{'original': 'Left'}, {'original': 'Right'}]}]}
        domain.validate_registry_value('block', 'table', 1, table)
        domain.validate_table(table)
        table['cells'].append(copy.deepcopy(table['cells'][0]))
        with self.assertRaisesRegex(ValueError, 'table_span_overlap'):
            domain.validate_table(table)

    def test_reimport_three_way_field_matrix(self):
        cases = json.loads((ROOT / 'contracts/examples/domain-reimport.json').read_text())
        for case in cases:
            with self.subTest(case=case['name']):
                base = copy.deepcopy(case['base'])
                merged, outcomes = domain.merge_fields(case['base'], case['local'], case['incoming'])
                self.assertEqual(outcomes, case['expected'])
                self.assertEqual(case['base'], base)
                for key, outcome in outcomes.items():
                    if outcome == 'conflict' and key in case['local']:
                        self.assertEqual(merged[key], case['local'][key])
        domain.schema_validator('ReimportPlan').validate(json.loads((ROOT / 'contracts/examples/domain-reimport-plan.json').read_text()))

    def test_import_resolution_binds_exact_plan_and_all_conflicts(self):
        plan = json.loads((ROOT / 'contracts/examples/domain-reimport-plan.json').read_text())
        resolution = json.loads((ROOT / 'contracts/examples/domain-import-resolution.json').read_text())
        domain.validate_import_resolution(plan, resolution)
        for change in ({'expectedLocalRevision': '2'}, {'fields': []}, {'planId': uid(999)}):
            with self.assertRaises(ValueError):
                domain.validate_import_resolution(plan, {**resolution, **change})
        bad = copy.deepcopy(resolution)
        bad['fields'][0]['choice'] = 'keep_local'
        self.assertFalse(domain.schema_validator('ImportResolution').is_valid(bad))
        bad['fields'][0].pop('value')
        domain.validate_import_resolution(plan, bad)
        plan['identityStatus'] = 'ambiguous'
        with self.assertRaisesRegex(ValueError, 'identity_resolution_required'):
            domain.validate_import_resolution(plan, resolution)
        resolved = {**resolution, 'identityChoice': {'choice': 'link_existing', 'localKind': 'page', 'localId': uid(14)}}
        domain.validate_import_resolution(plan, resolved)
        domain.validate_import_resolution(plan, {**resolved, 'identityChoice': {'choice': 'skip'}, 'fields': []})
        plan['identityStatus'] = 'matched'; plan['sourceDeleted'] = True
        with self.assertRaisesRegex(ValueError, 'deletion_resolution_required'):
            domain.validate_import_resolution(plan, resolution)
        deletion = {**resolution, 'deletionChoice': 'accept_source_delete'}
        domain.validate_import_resolution(plan, deletion)
        plan['sourceCompleteness'] = 'unknown'
        with self.assertRaisesRegex(ValueError, 'source_deletion_unproven'):
            domain.validate_import_resolution(plan, deletion)

    def test_reimport_deletion_ambiguity_and_omissions(self):
        base = dict(identity='matched', completeness='complete', source_deleted=False,
                    local_deleted=False, local_changed=False, incoming_changed=False)
        cases = [({'identity': 'ambiguous'}, 'needs_review'),
                 ({'source_deleted': True}, 'tombstone'),
                 ({'source_deleted': True, 'local_changed': True}, 'needs_review'),
                 ({'source_deleted': True, 'completeness': 'unknown'}, 'no_change'),
                 ({'source_deleted': True, 'completeness': 'partial'}, 'no_change'),
                 ({'local_deleted': True, 'incoming_changed': True}, 'needs_review'),
                 ({'local_deleted': True}, 'no_change')]
        for override, expected in cases:
            self.assertEqual(domain.reimport_action(**{**base, **override}), expected)

    def test_memory_scope_approval_forgetting_and_source_invalidation(self):
        snapshot = fixture(); memory = snapshot['memories'][0]
        self.assertTrue(domain.memory_eligible(memory, snapshot, uid(80)))
        for status in ('proposed', 'disabled', 'stale', 'superseded', 'forgotten'):
            candidate = {**memory, 'status': status}
            self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))
        candidate = {**memory, 'approvedAt': None}
        self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))
        snapshot['projectScopes'][0]['excludedPageIds'] = [uid(16)]
        self.assertFalse(domain.memory_eligible(memory, snapshot, uid(80)))
        snapshot['projectScopes'][0]['excludedPageIds'] = []
        source = next(p for p in snapshot['pages'] if p['id'] == uid(16))
        source['revision'] = '2'
        self.assertFalse(domain.memory_eligible(memory, snapshot, uid(80)))
        source['revision'] = '1'; source['deletedAt'] = '2026-09-30T01:00:00Z'
        self.assertFalse(domain.memory_eligible(memory, snapshot, uid(80)))

    def test_memory_content_or_context_reference_is_unambiguous(self):
        snapshot = fixture(); decision = snapshot['memories'][0]
        context = {k: v for k, v in decision.items() if k != 'content'}
        context.update(kind='project_context', pageRef=uid(16))
        domain.schema_validator('Memory').validate(context)
        self.assertTrue(domain.memory_eligible(context, snapshot, uid(80)))
        self.assertFalse(domain.schema_validator('Memory').is_valid({**context, 'content': 'Duplicated source'}))
        no_ref = {k: v for k, v in context.items() if k != 'pageRef'}
        self.assertFalse(domain.schema_validator('Memory').is_valid(no_ref))
        self.assertFalse(domain.schema_validator('Memory').is_valid({**no_ref, 'content': 'Missing original page'}))
        self.assertFalse(domain.schema_validator('Memory').is_valid({**decision, 'pageRef': uid(16)}))

    def test_memory_inheritance_is_opt_in_and_preferences_only(self):
        snapshot = fixture(); memory = snapshot['memories'][0]
        candidate = {**memory, 'scope': {'kind': 'workspace', 'workspaceId': uid(1)},
                     'kind': 'preference', 'sources': []}
        self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))
        snapshot['projectScopes'][0]['inheritWorkspacePreferences'] = True
        self.assertTrue(domain.memory_eligible(candidate, snapshot, uid(80)))
        candidate['kind'] = 'decision'
        self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))
        parent = {**snapshot['containers'][0], 'id': uid(82)}
        snapshot['containers'].append(parent)
        snapshot['containers'][0]['parentProjectId'] = uid(82)
        candidate.update(kind='preference', scope={'kind': 'project', 'workspaceId': uid(1), 'projectId': uid(82)})
        self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))
        snapshot['projectScopes'][0]['inheritAncestorPreferences'] = True
        self.assertTrue(domain.memory_eligible(candidate, snapshot, uid(80)))
        candidate['scope']['projectId'] = uid(999)
        self.assertFalse(domain.memory_eligible(candidate, snapshot, uid(80)))

    def test_citations_use_stable_identity_revision_and_timestamp(self):
        snapshot = fixture(); location = snapshot['memories'][0]['sources'][0]
        domain.validate_location(location, snapshot)
        bad = copy.deepcopy(location); bad['transcript']['endMs'] = 5000
        with self.assertRaisesRegex(ValueError, 'transcript_range_invalid'):
            domain.validate_location(bad, snapshot)
        bad = {**location, 'sourceRevision': '2'}
        with self.assertRaisesRegex(ValueError, 'source_revision_stale'):
            domain.validate_location(bad, snapshot)
        snapshot['blocks'] = [b for b in snapshot['blocks'] if b['id'] != location['blockId']]
        with self.assertRaisesRegex(ValueError, 'source_block_missing'):
            domain.validate_location(location, snapshot)


class PersistenceContractTests(unittest.TestCase):
    def setUp(self):
        self.db = sqlite3.connect(':memory:')
        self.db.executescript((ROOT / 'schemas/workspace.sql').read_text())
        self.db.execute('INSERT INTO workspaces VALUES (?,1)', (uid(1),))
        for p in fixture()['pages']:
            self.db.execute('INSERT INTO pages(id,workspace_id,parent_page_id,title,revision,created_at,updated_at,deleted_at) VALUES(?,?,?,?,?,?,?,?)',
                            tuple(p[k] for k in ('id','workspaceId','parentPageId','title','revision','createdAt','updatedAt','deletedAt')))
        for c in fixture()['collections']:
            self.db.execute('INSERT INTO collections(id,workspace_id,owning_page_id,title,title_property_id,system_role,revision) VALUES(?,?,?,?,?,?,?)',
                            tuple(c[k] for k in ('id','workspaceId','owningPageId','title','titlePropertyId','systemRole','revision')))
        for p in fixture()['properties']:
            self.db.execute('INSERT INTO properties VALUES(?,?,?,?,?,?,?)', (p['id'],p['collectionId'],p['name'],p['type'],p['schemaVersion'],json.dumps(p['config']),p['revision']))
            for o in p['config'].get('options', []):
                self.db.execute('INSERT INTO property_options VALUES(?,?,?,?)',(o['id'],p['id'],o['label'],o.get('group')))
        for r in fixture()['records']:
            self.db.execute('INSERT INTO records VALUES(?,?,?,?,?)',(r['id'],r['collectionId'],r['pageId'],r['sortKey'],r['revision']))
            for v in r['values']:
                p = next(p for p in fixture()['properties'] if p['id']==v['propertyId'])
                self.db.execute('INSERT INTO property_values(record_id,collection_id,property_id,property_type,schema_version,value_json) VALUES(?,?,?,?,?,?)',(r['id'],r['collectionId'],p['id'],p['type'],1,json.dumps(v['value'])))
        for t in fixture()['taskFacets']:
            r = next(r for r in fixture()['records'] if r['id']==t['recordId'])
            self.db.execute('INSERT INTO task_facets(record_id,collection_id,status_property_id,due_property_id) VALUES(?,?,?,?)',(r['id'],r['collectionId'],t['statusPropertyId'],t['duePropertyId']))
        for b in fixture()['blocks']:
            self.db.execute('INSERT INTO blocks VALUES(?,?,?,?,?,?,?,?)',(b['id'],b['pageId'],b['parentBlockId'],b['sortKey'],b['type'],b['schemaVersion'],json.dumps(b['data']),b['revision']))
        self.db.commit()

    def tearDown(self):
        self.db.close()

    def test_record_body_required_unique_and_foreign_keyed(self):
        for record_id, page_id in ((uid(600),None),(uid(601),uid(12)),(uid(602),uid(999))):
            with self.assertRaises(sqlite3.IntegrityError):
                self.db.execute('INSERT INTO records VALUES(?,?,?,?,?)',(record_id,uid(30),page_id,'000000000002','1'))
        self.assertEqual(self.db.execute('PRAGMA foreign_key_check').fetchall(), [])

    def test_sql_task_completion_has_one_value(self):
        self.assertEqual(self.db.execute('SELECT completed FROM task_state WHERE record_id=?',(uid(60),)).fetchone()[0],0)
        self.db.execute('UPDATE property_values SET value_json=? WHERE record_id=? AND property_id=?',(json.dumps(uid(51)),uid(60),uid(41)))
        self.assertEqual(self.db.execute('SELECT completed FROM task_state WHERE record_id=?',(uid(60),)).fetchone()[0],1)
        self.assertEqual(self.db.execute('SELECT count(*) FROM todo_references WHERE task_record_id=?',(uid(60),)).fetchone()[0],2)
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('UPDATE property_values SET value_json=? WHERE record_id=?',(json.dumps(uid(999)),uid(60)))
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('UPDATE blocks SET data_json=? WHERE id=?',(json.dumps({'taskRecordId':uid(60),'checked':True}),uid(70)))

    def test_sql_rejects_wrong_collection_and_duplicate_title(self):
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('UPDATE task_facets SET status_property_id=? WHERE record_id=?',(uid(44),uid(60)))
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('INSERT INTO properties VALUES(?,?,?,?,?,?,?)',(uid(700),uid(30),'Another title','title',1,'{}','1'))

    def test_sql_revision_limit_and_cycles(self):
        for invalid in ('0','01','9223372036854775808','1.1'):
            with self.assertRaises(sqlite3.IntegrityError):
                self.db.execute('UPDATE pages SET revision=? WHERE id=?',(invalid,uid(10)))
        self.db.execute('UPDATE pages SET parent_page_id=? WHERE id=?',(uid(10),uid(11)))
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('UPDATE pages SET parent_page_id=? WHERE id=?',(uid(11),uid(10)))
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('UPDATE blocks SET parent_block_id=? WHERE id=?',(uid(71),uid(70)))
            self.db.commit()
        self.db.rollback()

    def test_sql_view_membership_preserves_multiple_sources(self):
        self.db.execute('INSERT INTO containers VALUES(?,?,?,?,?,?,?)',(uid(81),uid(1),uid(18),'database','Two sources',None,'1'))
        self.db.executemany('INSERT INTO container_collections VALUES(?,?)',[(uid(81),uid(30)),(uid(81),uid(31))])
        for v in fixture()['views']:
            self.db.execute('INSERT INTO views VALUES(?,?,?,?,?,?,?,?,?,?)',(v['id'],v['collectionId'],v['containerId'],v['owningPageId'],v['name'],v['type'],v['schemaVersion'],json.dumps(v['query']),json.dumps(v['config']),v['revision']))
        self.assertEqual(self.db.execute('SELECT count(*) FROM records').fetchone()[0],2)
        self.assertEqual(self.db.execute('SELECT count(*) FROM container_collections').fetchone()[0],2)

    def test_sql_asset_hash_and_mapping_namespace(self):
        for f in fixture()['files']:
            self.db.execute('INSERT INTO files VALUES(?,?,?,?,?,?,?,?)',(f['id'],f['workspaceId'],f['contentHash'],f['originalName'],f['mediaType'],int(f['sizeBytes']),f['storageKey'],f['revision']))
        f=fixture()['files'][0]
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('INSERT INTO files VALUES(?,?,?,?,?,?,?,?)',(uid(900),f['workspaceId'],f['contentHash'],'different-name.wav',f['mediaType'],int(f['sizeBytes']),f['storageKey'],f['revision']))
        values=(uid(130),uid(1),'notion','source-workspace','page','external_id','same-upstream-id','page',uid(14),'r1','b'*64,uid(101),uid(131))
        self.db.execute('INSERT INTO import_mappings VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)',values)
        other=list(values);other[0]=uid(132);other[4]='collection';other[7]='collection';other[8]=uid(31)
        self.db.execute('INSERT INTO import_mappings VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)',other)
        with self.assertRaises(sqlite3.IntegrityError):
            self.db.execute('INSERT INTO import_mappings VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)',(uid(133),*values[1:]))


if __name__ == '__main__':
    unittest.main()
