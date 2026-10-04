"""Executable conformance oracle for Phase 0, not a production persistence service.

Production implementations must produce these outcomes through workspace services.
JSON Schemas validate shape; this module verifies cross-object domain invariants.
"""
from copy import deepcopy
from pathlib import Path
import json

ROOT = Path(__file__).resolve().parent
MISSING = object()
MAX_REVISION = 9223372036854775807


def require(condition, message):
    if not condition:
        raise ValueError(message)


def schema_validator(definition):
    from jsonschema import Draft202012Validator, FormatChecker
    schema = json.loads((ROOT / 'domain.schema.json').read_text())
    return Draft202012Validator({**schema, '$ref': '#/$defs/' + definition}, format_checker=FormatChecker())


def validate_revision(revision):
    require(isinstance(revision, str) and revision.isascii() and revision.isdecimal()
            and not revision.startswith('0') and int(revision) <= MAX_REVISION,
            'invalid_revision')


def registry_entries():
    return {(e['family'], e['type'], e['schemaVersion']): e
            for e in json.loads((ROOT / 'registries.json').read_text())['entries']}


def validate_registry_value(family, type_name, version, value, config=False):
    entry = registry_entries().get((family, type_name, version))
    require(entry is not None, 'unknown_registry_entry_requires_fallback')
    ref = entry['configSchemaRef' if config else 'schemaRef']
    schema_validator(ref.split('/')[-1]).validate(value)


def validate_location(location, snapshot):
    schema_validator('Location').validate(location)
    pages = {p['id']: p for p in snapshot['pages']}
    page = pages.get(location['pageId'])
    require(page is not None and page['deletedAt'] is None, 'source_deleted_or_missing')
    require(location['sourceRevision'] == page['revision'], 'source_revision_stale')
    if 'blockId' in location:
        block = next((b for b in snapshot['blocks'] if b['id'] == location['blockId']), None)
        require(block is not None and block['pageId'] == page['id'], 'source_block_missing')
    if 'transcript' in location:
        t = location['transcript']
        segment = next((s for s in snapshot['transcriptSegments'] if s['id'] == t['segmentId']), None)
        meeting = next((m for m in snapshot['meetings'] if m['id'] == t['meetingId']), None)
        require(segment is not None and meeting is not None and meeting['pageId'] == page['id']
                and segment['meetingId'] == meeting['id'] and segment['blockId'] == location['blockId'],
                'transcript_identity_mismatch')
        require(segment['startMs'] <= t['startMs'] <= t['endMs'] <= segment['endMs'], 'transcript_range_invalid')


def validate_snapshot(snapshot):
    schema_validator('DomainSnapshot').validate(snapshot)
    indexes = {}
    identity_sets = ['pages', 'blocks', 'collections', 'properties', 'records', 'containers',
                     'views', 'files', 'meetings', 'transcriptSegments', 'memories', 'importMappings']
    all_ids = []
    for key in identity_sets:
        indexes[key] = {x['id']: x for x in snapshot[key]}
        require(len(indexes[key]) == len(snapshot[key]), 'duplicate_' + key)
        all_ids.extend(indexes[key])
    require(len(all_ids) == len(set(all_ids)), 'identity_reused_across_entity_kinds')
    for key in identity_sets:
        for item in snapshot[key]:
            if 'revision' in item:
                validate_revision(item['revision'])
            if 'workspaceId' in item:
                require(item['workspaceId'] == snapshot['workspaceId'], 'workspace_mismatch')
    pages, blocks, collections = indexes['pages'], indexes['blocks'], indexes['collections']
    properties, records, containers = indexes['properties'], indexes['records'], indexes['containers']
    def acyclic(items, field):
        for item in items.values():
            seen = {item['id']}
            parent = item.get(field)
            while parent is not None:
                require(parent in items, 'missing_parent')
                require(parent not in seen, 'parent_cycle')
                seen.add(parent)
                parent = items[parent].get(field)
    acyclic(pages, 'parentPageId')
    acyclic(blocks, 'parentBlockId')
    acyclic(containers, 'parentProjectId')
    for container in containers.values():
        require(container['owningPageId'] in pages, 'container_page_missing')
        if container['parentProjectId']:
            require(containers[container['parentProjectId']]['kind'] == 'project', 'parent_not_project')
    ordered = set()
    for b in blocks.values():
        require(b['pageId'] in pages, 'block_page_missing')
        if b['parentBlockId']:
            require(blocks[b['parentBlockId']]['pageId'] == b['pageId'], 'block_parent_page_mismatch')
        order = (b['pageId'], b['parentBlockId'], b['sortKey'])
        require(order not in ordered, 'duplicate_block_order')
        ordered.add(order)
        validate_registry_value('block', b['type'], b['schemaVersion'], b['data'])
        if b['type'] == 'table':
            validate_table(b['data'])
    require(sum(c['systemRole'] == 'tasks' for c in collections.values()) <= 1, 'duplicate_tasks_collection')
    for c in collections.values():
        require(c['owningPageId'] in pages, 'collection_page_missing')
        titles = [p['id'] for p in properties.values() if p['collectionId'] == c['id'] and p['type'] == 'title']
        require(titles == [c['titlePropertyId']], 'exactly_one_primary_title')
    for p in properties.values():
        require(p['collectionId'] in collections, 'property_collection_missing')
        validate_registry_value('property', p['type'], p['schemaVersion'], p['config'], config=True)
        if p['type'] in ('select', 'multi_select', 'status'):
            ids = [o['id'] for o in p['config']['options']]
            require(len(ids) == len(set(ids)), 'duplicate_option_id')
            for role in ('defaultOptionId', 'doneOptionId', 'reopenOptionId'):
                require(role not in p['config'] or p['config'][role] in ids, 'option_role_missing')
    body_ids = [r['pageId'] for r in records.values()]
    require(len(body_ids) == len(set(body_ids)), 'record_page_not_unique')
    for r in records.values():
        require(r['pageId'] in pages and r['collectionId'] in collections, 'record_body_or_collection_missing')
        require(r['revision'] == pages[r['pageId']]['revision'], 'record_page_revision_mismatch')
        vals = {v['propertyId']: v for v in r['values']}
        require(len(vals) == len(r['values']), 'duplicate_property_value')
        for v in vals.values():
            p = properties.get(v['propertyId'])
            require(p is not None and p['collectionId'] == r['collectionId'], 'value_property_collection_mismatch')
            require(p['type'] != 'title', 'title_is_page_projection')
            validate_registry_value('property', p['type'], v['schemaVersion'], v['value'])
            if p['type'] in ('select', 'multi_select', 'status'):
                ids = {o['id'] for o in p['config']['options']}
                selected = v['value'] if isinstance(v['value'], list) else [v['value']]
                require(all(x is None or x in ids for x in selected), 'option_missing')
    tasks = {t['recordId']: t for t in snapshot['taskFacets']}
    require(len(tasks) == len(snapshot['taskFacets']), 'duplicate_task_facet')
    for t in tasks.values():
        r, p = records.get(t['recordId']), properties.get(t['statusPropertyId'])
        require(r is not None and p is not None and p['type'] == 'status' and p['collectionId'] == r['collectionId'], 'task_status_property_invalid')
        require(any(v['propertyId'] == p['id'] for v in r['values']), 'task_status_value_missing')
        config = p['config']; opts = {o['id']: o for o in config['options']}
        require('doneOptionId' in config and opts[config['doneOptionId']]['group'] == 'done', 'task_done_mapping_invalid')
        require('reopenOptionId' in config and opts[config['reopenOptionId']]['group'] != 'done', 'task_reopen_mapping_invalid')
        if t['duePropertyId']:
            due = properties.get(t['duePropertyId'])
            require(due is not None and due['type'] == 'date' and due['collectionId'] == r['collectionId'], 'task_due_property_invalid')
    for b in blocks.values():
        if b['type'] == 'todo':
            require(b['data']['taskRecordId'] in tasks, 'todo_task_missing')
    memberships = {(x['containerId'], x['collectionId']) for x in snapshot['containerCollections']}
    require(len(memberships) == len(snapshot['containerCollections']), 'duplicate_container_membership')
    for container_id, collection_id in memberships:
        require(container_id in containers and collection_id in collections, 'container_collection_missing')
    for v in indexes['views'].values():
        require(v['collectionId'] in collections and v['owningPageId'] in pages, 'view_target_missing')
        if v['containerId']:
            require((v['containerId'], v['collectionId']) in memberships, 'view_collection_not_in_container')
        validate_registry_value('view', v['type'], v['schemaVersion'], v['config'])
        ids = set(v['query']['visiblePropertyIds']) | {s['propertyId'] for s in v['query']['sorts']}
        ids |= {v['query'][k] for k in ('groupByPropertyId', 'subgroupByPropertyId') if v['query'][k] is not None}
        for key in ('groupPropertyId', 'datePropertyId', 'coverPropertyId'):
            if v['config'].get(key):
                ids.add(v['config'][key])
        for p in ids:
            require(p in properties and properties[p]['collectionId'] == v['collectionId'], 'view_property_collection_mismatch')
    hashes = [f['contentHash'] for f in indexes['files'].values()]
    require(len(hashes) == len(set(hashes)), 'file_content_duplicate')
    for f in indexes['files'].values():
        require(f['storageKey'] == 'files/' + f['contentHash'][:2] + '/' + f['contentHash'], 'file_storage_key_invalid')
        require(int(f['sizeBytes']) <= MAX_REVISION, 'file_size_out_of_range')
    for m in indexes['meetings'].values():
        require(m['pageId'] in pages, 'meeting_page_missing')
        require(m['recordingFileId'] is None or m['recordingFileId'] in indexes['files'], 'recording_file_missing')
    for s in indexes['transcriptSegments'].values():
        m = indexes['meetings'].get(s['meetingId'])
        require(m is not None and s['blockId'] in blocks and blocks[s['blockId']]['pageId'] == m['pageId'], 'segment_page_mismatch')
        require(0 <= s['startMs'] <= s['endMs'] <= m['durationMs'], 'segment_time_invalid')
    for p in snapshot['projectScopes']:
        require(p['projectId'] in containers and containers[p['projectId']]['kind'] == 'project', 'scope_project_missing')
        for page in p['memberPageIds'] + p['pinnedPageIds'] + p['excludedPageIds']:
            require(page in pages, 'scope_page_missing')
    for m in indexes['memories'].values():
        require(m['pageId'] in pages, 'memory_page_missing')
        require(m['scope']['workspaceId'] == snapshot['workspaceId'], 'memory_workspace_mismatch')
        if m['kind'] in ('decision', 'project_context'):
            require(bool(m['sources']), 'evidence_required')
        if m['status'] == 'active':
            require(m['approvedAt'] is not None and m['approvedByActorId'] is not None, 'memory_approval_required')
            for source in m['sources']:
                validate_location(source, snapshot)
    return True


def validate_table(table):
    covered = set()
    require(table['headerRows'] <= table['rowCount'] and table['headerColumns'] <= table['columnCount'], 'table_header_bounds')
    require(len(table['columnWidths']) == table['columnCount'], 'table_width_count')
    for cell in table['cells']:
        for r in range(cell['row'], cell['row'] + cell['rowSpan']):
            for c in range(cell['column'], cell['column'] + cell['colSpan']):
                require(r < table['rowCount'] and c < table['columnCount'], 'table_span_outside')
                require((r, c) not in covered, 'table_span_overlap')
                covered.add((r, c))
    require(len(covered) == table['rowCount'] * table['columnCount'], 'table_span_gap')


def merge_fields(base, local, incoming):
    """Keys are atomic schema paths; missing differs from JSON null. No writes."""
    merged, outcomes = {}, {}
    for field in sorted(base.keys() | local.keys() | incoming.keys()):
        b, l, i = base.get(field, MISSING), local.get(field, MISSING), incoming.get(field, MISSING)
        if l == b and i == b:
            outcome, value = 'unchanged', l
        elif l == i:
            outcome, value = 'converged', l
        elif l == b:
            outcome, value = 'apply_incoming', i
        elif i == b:
            outcome, value = 'keep_local', l
        else:
            outcome, value = 'conflict', l
        outcomes[field] = outcome
        if value is not MISSING:
            merged[field] = deepcopy(value)
    return merged, outcomes


def reimport_action(*, identity, completeness, source_deleted, local_deleted, local_changed, incoming_changed):
    """Object deletion and ambiguous identity are deliberately never inferred."""
    if identity != 'matched':
        return 'needs_review'
    if source_deleted:
        if completeness != 'complete':
            return 'no_change'
        return 'needs_review' if local_changed and not local_deleted else 'tombstone'
    if local_deleted:
        return 'needs_review' if incoming_changed else 'no_change'
    return 'merge_fields'


def project_pages(snapshot, project_id):
    policy = next(p for p in snapshot['projectScopes'] if p['projectId'] == project_id)
    container = next(c for c in snapshot['containers'] if c['id'] == project_id)
    candidates = set(policy['memberPageIds']) | set(policy['pinnedPageIds']) | {container['owningPageId']}
    live = {p['id'] for p in snapshot['pages'] if p['deletedAt'] is None}
    return (candidates - set(policy['excludedPageIds'])) & live


def memory_eligible(memory, snapshot, project_id):
    if memory['status'] != 'active' or memory['approvedAt'] is None or memory['approvedByActorId'] is None:
        return False
    policy = next(p for p in snapshot['projectScopes'] if p['projectId'] == project_id)
    scope = memory['scope']
    if scope['workspaceId'] != snapshot['workspaceId']:
        return False
    containers = {c['id']: c for c in snapshot['containers']}
    if containers[project_id]['owningPageId'] not in {p['id'] for p in snapshot['pages'] if p['deletedAt'] is None}:
        return False
    allowed = project_pages(snapshot, project_id)
    if memory['pageId'] in policy['excludedPageIds']:
        return False
    if scope['kind'] == 'workspace':
        if memory['kind'] != 'preference' or not policy['inheritWorkspacePreferences']:
            return False
    elif scope['kind'] == 'project' and scope['projectId'] != project_id:
        ancestors = set()
        current = containers[project_id]['parentProjectId']
        while current is not None:
            if current in ancestors:
                return False
            ancestors.add(current)
            current = containers[current]['parentProjectId']
        if memory['kind'] != 'preference' or not policy['inheritAncestorPreferences'] or scope['projectId'] not in ancestors:
            return False
    elif scope['kind'] == 'pages' and not set(scope['pageIds']).issubset(allowed):
        return False
    for source in memory['sources']:
        if source['pageId'] not in allowed:
            return False
        try:
            validate_location(source, snapshot)
        except (ValueError, KeyError):
            return False
    if memory.get('pageRef') is not None and memory['pageRef'] not in allowed:
        return False
    own_page = next((p for p in snapshot['pages'] if p['id'] == memory['pageId']), None)
    return own_page is not None and own_page['deletedAt'] is None


def task_projection(snapshot, record_id):
    facet = next(t for t in snapshot['taskFacets'] if t['recordId'] == record_id)
    record = next(r for r in snapshot['records'] if r['id'] == record_id)
    page = next(p for p in snapshot['pages'] if p['id'] == record['pageId'])
    definition = next(p for p in snapshot['properties'] if p['id'] == facet['statusPropertyId'])
    value = next(v['value'] for v in record['values'] if v['propertyId'] == facet['statusPropertyId'])
    option = next(o for o in definition['config']['options'] if o['id'] == value)
    return {'recordId': record_id, 'pageId': page['id'], 'title': page['title'],
            'statusOptionId': value, 'completed': option['group'] == 'done'}


def extracted_text(block, snapshot):
    """Golden P0 text representation; production extractors share these semantics."""
    data = block['data']
    if block['type'] == 'todo':
        return task_projection(snapshot, data['taskRecordId'])['title']
    if block['type'] == 'unsupported_external':
        return data['readableText']
    if 'richText' in data:
        return ''.join(span['text'] for span in data['richText'])
    if block['type'] == 'table':
        return '\n'.join(''.join(s['text'] for s in cell['richText'])
                         for cell in sorted(data['cells'], key=lambda c: (c['row'], c['column'])))
    if 'source' in data:
        return data['source']
    if 'latex' in data:
        return data['latex']
    return '\n'.join(filter(None, [data.get('altText', ''), data.get('displayName', ''),
                                  ''.join(s['text'] for s in data.get('caption', []))]))


def preserve_unknown(type_name, version, raw, readable_text, reason='unknown_type'):
    """Fallback conversion retains the original JSON; caller keeps original bytes in rawFileId when present."""
    fallback = {'sourceType': type_name, 'sourceVersion': version, 'raw': deepcopy(raw),
                'readableText': readable_text, 'reason': reason}
    schema_validator('Fallback').validate(fallback)
    return fallback


def validate_import_resolution(plan, resolution):
    """Check reviewer choices against the immutable per-object plan before apply."""
    schema_validator('ReimportPlan').validate(plan)
    schema_validator('ImportResolution').validate(resolution)
    require(resolution['planId'] == plan['id'] and resolution['mappingId'] == plan['mappingId'], 'resolution_plan_mismatch')
    require(resolution['expectedLocalRevision'] == plan['expectedLocalRevision']
            and resolution['incomingSourceRevision'] == plan['incomingSourceRevision'], 'resolution_revision_stale')
    if plan['identityStatus'] != 'matched':
        require('identityChoice' in resolution, 'identity_resolution_required')
    else:
        require('identityChoice' not in resolution, 'matched_identity_cannot_be_remapped')
    if resolution.get('identityChoice', {}).get('choice') == 'skip':
        require(not resolution['fields'] and 'deletionChoice' not in resolution, 'skip_has_no_content_writes')
        return True
    if plan['sourceDeleted'] or plan['localDeleted']:
        require('deletionChoice' in resolution, 'deletion_resolution_required')
        choice = resolution['deletionChoice']
        if choice == 'accept_source_delete':
            require(plan['sourceDeleted'] and plan['sourceCompleteness'] == 'complete', 'source_deletion_unproven')
        if choice == 'restore_source':
            require(plan['localDeleted'] and not plan['sourceDeleted'], 'restore_requires_live_source')
    else:
        require('deletionChoice' not in resolution, 'unexpected_deletion_choice')
    paths = {f['path']: f for f in plan['fields']}
    resolved = {f['path'] for f in resolution['fields']}
    require(len(resolved) == len(resolution['fields']), 'duplicate_field_resolution')
    require(resolved.issubset(paths), 'resolution_field_not_in_plan')
    conflicts = {f['path'] for f in plan['fields'] if f['outcome'] == 'conflict'}
    require(conflicts.issubset(resolved), 'unresolved_import_conflict')
    return True
