#!/usr/bin/env python3
"""Run one predeclared local-model corpus; never counts skipped/missing cases as pass."""
import argparse, hashlib, json, os, pathlib, platform, re, shutil, subprocess, sys, tempfile, time

ROOT = pathlib.Path(__file__).resolve().parent.parent
FAMILIES = {
    'p0': ('p0-qa-memory.json', 'TestRealLocalExpandedP0QAMemory', 24),
    'p0-holdouts': ('p0-closure-holdouts.json', 'TestRealLocalP0ClosureHoldouts', 12),
    'modes': ('heldout-modes.json', 'TestRealLocalExpandedModes', 42),
    'context': ('context.json', 'TestRealLocalWorkflowContext', 12),
    'related': ('related.json', 'TestRealLocalWorkflowRelated', 12),
    'memory-lifecycle': ('memory-lifecycle.json', 'TestRealLocalWorkflowMemoryLifecycle', 12),
    'images': ('images-execution-v2.json', 'TestRealLocalWorkflowImages', 24),
    'reviews': ('reviews-execution-v2.json', 'TestRealLocalWorkflowReviewsV2', 12),
}

def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

# Kept independent of the Go audit: validate retained bytes and recompute the
# equality predicate rather than trusting a report's boolean or row counts.
CANONICAL_SNAPSHOT_LIMIT = 32 * 1024 * 1024
CANONICAL_REQUIRED_TABLES = frozenset('''workspaces pages blocks collections properties property_options
records property_values task_facets todo_references containers container_collections views files attachments
meetings transcript_segments project_scopes project_pages memory_entries memory_sources import_mappings
import_observations mutation_history committed_events idempotency_receipts approvals page_metadata
page_links entity_tombstones page_snapshots file_references recovery_points recovery_point_pages'''.split())
CANONICAL_EXCLUDED_TABLES = frozenset('''ai_runs ai_proposals search_chunks search_dirty search_meta
search_fts search_fts_data search_fts_idx search_fts_docsize search_fts_config search_embeddings
search_vector_models semantic_auto_attempts document_extractions document_sections sqlite_sequence'''.split())


def _unique_json(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate canonical evidence key: ' + key)
        result[key] = value
    return result


def _snapshot_binding(binding, root, test_name, phase, expected_excluded=None):
    import base64
    if not isinstance(binding, dict) or binding.get('retained') is not True:
        raise ValueError('canonical snapshot is not durably retained')
    path = pathlib.Path(binding.get('path', ''))
    root = pathlib.Path(root)
    if not path.is_absolute() or path.name != phase + '.json' or not path.is_relative_to(root):
        raise ValueError('canonical snapshot path is outside its declared evidence directory')
    # Reject path traversal and links before reading any report-selected file.
    if '..' in path.parts or any(part.is_symlink() for part in (path, *path.parents)):
        raise ValueError('canonical snapshot path contains a link or traversal')
    if not path.is_file() or path.stat().st_size > CANONICAL_SNAPSHOT_LIMIT:
        raise ValueError('canonical snapshot is unavailable or exceeds the byte limit')
    raw = path.read_bytes()
    if len(raw) > CANONICAL_SNAPSHOT_LIMIT or len(raw) != binding.get('size') or hashlib.sha256(raw).hexdigest() != binding.get('sha256'):
        raise ValueError('canonical snapshot byte identity differs')
    data = json.loads(raw, object_pairs_hook=_unique_json)
    if data.get('schemaVersion') != 1 or data.get('testName') != test_name or not data.get('scope'):
        raise ValueError('canonical snapshot scope/test identity differs')
    excluded, tables, assets = data.get('excludedTables'), data.get('tables'), data.get('assets')
    if expected_excluded is None: expected_excluded = CANONICAL_EXCLUDED_TABLES
    if not isinstance(excluded, dict) or set(excluded) != expected_excluded or not all(isinstance(v, str) and v for v in excluded.values()):
        raise ValueError('canonical snapshot exclusions differ from the reviewed scope')
    if not isinstance(tables, dict) or not CANONICAL_REQUIRED_TABLES.issubset(tables) or set(tables) & set(excluded):
        raise ValueError('canonical snapshot omits protected content tables')
    row_counts = {}
    for name, table in tables.items():
        if not isinstance(table, dict) or not isinstance(table.get('schema'), str) or not table['schema']:
            raise ValueError('canonical table schema missing: ' + name)
        columns, rows = table.get('columns'), table.get('rows')
        if not isinstance(columns, list) or not columns or not all(isinstance(c, str) for c in columns) or len(set(columns)) != len(columns) or not isinstance(rows, list):
            raise ValueError('canonical table shape invalid: ' + name)
        row_counts[name] = len(rows)
        for row in rows:
            if not isinstance(row, list) or len(row) != len(columns):
                raise ValueError('canonical row width differs: ' + name)
            for cell in row:
                if not isinstance(cell, dict) or set(cell) - {'type', 'value'} or not isinstance(cell.get('value', ''), str):
                    raise ValueError('canonical typed cell is malformed')
                kind, value = cell.get('type'), cell.get('value', '')
                if kind in ('text', 'blob'):
                    base64.b64decode(value, validate=True)
                elif kind == 'integer':
                    if not re.fullmatch(r'0|-?[1-9][0-9]*', value):
                        raise ValueError('canonical integer encoding is invalid')
                elif kind == 'real-ieee754':
                    if not re.fullmatch('[0-9a-f]{16}', value):
                        raise ValueError('canonical float encoding is invalid')
                elif kind != 'null' or value:
                    raise ValueError('canonical cell type is unreviewed')
    roots, directories = data.get('assetRoots'), data.get('assetDirectories')
    if not isinstance(roots, dict) or set(roots) != {'files', 'recordings'} or roots.get('files') is not True or type(roots.get('recordings')) is not bool:
        raise ValueError('canonical files/recordings root inventory missing')
    if not isinstance(directories, list) or 'files' not in directories or len(directories) != len(set(directories)) or ('recordings' in directories) != roots['recordings']:
        raise ValueError('canonical asset directory inventory missing')
    if not isinstance(assets, dict):
        raise ValueError('canonical asset inventory missing')
    asset_bytes = 0
    for name in [*directories, *assets]:
        if not isinstance(name, str) or not name or pathlib.PurePosixPath(name).is_absolute() or '..' in pathlib.PurePosixPath(name).parts or not roots.get(name.split('/')[0]):
            raise ValueError('canonical asset path invalid')
    for asset in assets.values():
        if not isinstance(asset, dict) or not isinstance(asset.get('bytes'), str):
            raise ValueError('canonical asset bytes missing')
        content = base64.b64decode(asset['bytes'], validate=True)
        if len(content) != asset.get('size') or hashlib.sha256(content).hexdigest() != asset.get('sha256'):
            raise ValueError('canonical asset content binding differs')
        asset_bytes += len(content)
    if row_counts != binding.get('tableRows') or len(assets) != binding.get('assetFiles') or asset_bytes != binding.get('assetBytes'):
        raise ValueError('canonical snapshot summary differs from exact content')
    if data.get('mutationHistoryCount') != len(tables['mutation_history']['rows']):
        raise ValueError('canonical history count differs from retained rows')
    return data


def validate_canonical_inventory(report, root, test_name, expected_excluded=None):
    """Return a validation error; old count-only reports never qualify new runs."""
    try:
        if root is None:
            raise ValueError('canonical evidence directory was not supplied')
        audit = report.get('canonicalInventory')
        if not isinstance(audit, dict) or audit.get('schemaVersion') != 1 or audit.get('error'):
            raise ValueError('canonical inventory report missing or failed')
        before = _snapshot_binding(audit.get('before'), root, test_name, 'before', expected_excluded)
        after = _snapshot_binding(audit.get('after'), root, test_name, 'after', expected_excluded)
        if pathlib.Path(audit['before']['path']).parent != pathlib.Path(audit['after']['path']).parent:
            raise ValueError('canonical snapshot pair belongs to different observations')
        checks = {
            'rowsUnchanged': before['tables'] == after['tables'],
            'assetsUnchanged': (before['assetRoots'], before['assetDirectories'], before['assets']) == (after['assetRoots'], after['assetDirectories'], after['assets']),
            'mutationHistoryCountUnchanged': before['mutationHistoryCount'] == after['mutationHistoryCount'],
        }
        if any(audit.get(key) is not value for key, value in checks.items()) or audit.get('unchanged') is not all(checks.values()):
            raise ValueError('canonical comparison report differs from retained snapshot content')
        if not all(checks.values()) or report.get('mutationHistoryCountUnchanged') is not True:
            raise ValueError('canonical content or existing mutation-count check changed')
        return None
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        return str(error)


def validate_request_report(report, expected):
    """Qualify only explicit controls captured from the actual Go request.

    A legacy missing field is neither false nor evidence of a single attempt.
    Keep its original report readable, but do not qualify it under this contract.
    """
    if type(expected.get('useMemory')) is not bool:
        return 'locked useMemory expectation is not a boolean'
    if type(report.get('useMemory')) is not bool:
        return 'useMemory is missing or not a boolean'
    if report['useMemory'] is not expected['useMemory']:
        return 'useMemory differs from the locked request'
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != 1:
        return 'generationAttempts must record exactly one actual attempt'
    for field in ('modelPackId', 'corpusSHA256'):
        if not isinstance(expected.get(field), str) or not expected[field]:
            return 'locked ' + field + ' expectation is missing'
        if report.get(field) != expected[field]:
            return field + ' differs from the locked input'
    return None


CONTEXT_GENERATION_CASES = frozenset(('short-source', 'near-budget', 'overflow', 'beginning-fact', 'middle-fact', 'ending-fact'))
CONTEXT_ASSERTIONS = {
    'short-source': {'reservedBudget', 'noOmission', 'requiredFactRetained', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'currentCitation', 'canonicalPreserved'},
    'near-budget': {'nearCapacityMeasured', 'reservedBudget', 'requiredFactRetained', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'currentCitation', 'canonicalPreserved'},
    'overflow': {'reservedBudget', 'omissionDisclosed', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'canonicalPreserved'},
    'beginning-fact': {'reservedBudget', 'requiredFactRetained', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'currentCitation', 'canonicalPreserved'},
    'middle-fact': {'reservedBudget', 'omissionDisclosed', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'canonicalPreserved'},
    'ending-fact': {'reservedBudget', 'omissionDisclosed', 'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'canonicalPreserved'},
    'exact-code': {'reservedBudget', 'selectionPreserved', 'unselectedCodePreserved', 'canonicalPreserved'},
    'oversized-code': {'preparationRejected', 'noRunnableDraft', 'canonicalPreserved'},
    'framing-cost': {'framingAddsTokens', 'framingRejected'},
    'during-tokenization': {'preparationRejected', 'noRunnableDraft', 'canonicalPreserved'},
    'during-generation': {'reservedBudget', 'draftInvalidated', 'proposalRejected', 'canonicalPreserved'},
    'truncated-output': {'reservedBudget', 'draftInvalidated', 'proposalRejected', 'canonicalPreserved'},
}
RELATED_ASSERTIONS = frozenset(('semanticDisclosed', 'relevantRetrieved', 'relevanceAboveDistractor', 'scopeAndSelfFiltered', 'currentRevisions', 'keywordFallbackDisclosed', 'canonicalPreserved'))


MEMORY_GENERATION_CASES = frozenset(('preference-on', 'preference-off', 'current-authority', 'constraint'))
MEMORY_EXCLUDED_CASES = frozenset(('preference-off', 'source-edit', 'source-delete', 'forgotten', 'restore-stale', 'restore-forgotten'))
MEMORY_ASSERTIONS = {case: {'reservedBudget', 'memoryEligibility', 'canonicalPreserved'} for case in (
    'preference-on', 'preference-off', 'current-authority', 'constraint', 'source-edit', 'source-delete',
    'forgotten', 'forget-stream', 'restore-stale', 'restore-forgotten', 'older-backup', 'scope-revoke')}
for _case in MEMORY_GENERATION_CASES:
    MEMORY_ASSERTIONS[_case] |= {'oneGeneration', 'generatedSuccessfully', 'independentAnswerOracle', 'currentCitation'}
for _case in MEMORY_EXCLUDED_CASES:
    MEMORY_ASSERTIONS[_case].add('memoryContentAbsent')
for _case in MEMORY_EXCLUDED_CASES - {'preference-off'}:
    MEMORY_ASSERTIONS[_case].add('relatedSourceExcluded')
for _case in ('forget-stream', 'scope-revoke'):
    MEMORY_ASSERTIONS[_case] |= {'publicationRejected', 'proposalRejected'}
for _case in ('restore-stale', 'restore-forgotten'):
    MEMORY_ASSERTIONS[_case] |= {'verifiedRestore', 'restoredStatePreserved'}
MEMORY_ASSERTIONS['older-backup'] |= {'verifiedRestore', 'laterForgetRecorded', 'olderStateRecovered'}


def validate_memory_report(report, case):
    errors = []
    attempts = 1 if case in MEMORY_GENERATION_CASES else 0
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != attempts:
        errors.append('memory generation count differs')
    if not all(type(report.get(k)) is int for k in ('promptTokens', 'outputCap', 'contextTokens')) or report['promptTokens'] < 0 or report['outputCap'] != 512 or report['contextTokens'] != 8192 or report['promptTokens'] + 512 + 16 > 8192:
        errors.append('memory token budget not proved')
    request, sources = report['request'], report['sources']
    if type(request.get('useMemory')) is not bool or request['useMemory'] is not (case != 'preference-off') or request.get('modelPackId') != report['modelPackId']:
        errors.append('memory request controls differ')
    memory, primary, supporting = report['memoryId'], request['pageId'], report['supportingPageId']
    if not all(isinstance(v, str) and v for v in (memory, primary, supporting, report['memoryPageId'])) or primary in (supporting, report['memoryPageId']):
        errors.append('memory source identities missing or conflated')
    expected = case not in MEMORY_EXCLUDED_CASES
    found = [source for source in sources if source.get('memoryId') == memory]
    if report.get('memoryIncluded') is not expected or bool(found) != expected:
        errors.append('memory evidence eligibility differs')
    content = 'Use the heading Launch note for answers about Project Marigold.'
    if case == 'current-authority': content = 'Project Marigold launches on 9 February 2030.'
    if case == 'constraint': content = 'Do not suggest weekend due dates.'
    if report.get('memoryContent') != content or (expected and not any(s.get('text') == content for s in found)) or (not expected and content in report['preparedPrompt']):
        errors.append('memory context content differs')
    expected_status = 'stale' if case in ('source-edit', 'source-delete', 'restore-stale') else 'forgotten' if case in ('forgotten', 'restore-forgotten') else 'active'
    if report.get('memoryStatus') != expected_status:
        errors.append('persisted memory status differs')
    if case in MEMORY_EXCLUDED_CASES - {'preference-off'} and report.get('relatedRejected') is not True:
        errors.append('ineligible memory remains a related-content source')
    if case in ('restore-stale', 'restore-forgotten', 'older-backup'):
        if not report.get('backupWorkspaceId') or report.get('restoredWorkspaceId') != report['backupWorkspaceId'] or report.get('restoredIndexRebuilt') is not True:
            errors.append('verified restored workspace binding absent')
    if case == 'older-backup':
        if report.get('sourceAfterBackupStatus') != 'forgotten' or report.get('restoreBoundary') != 'A backup predating Forget retains the earlier approved memory state.' or report.get('nativeRestoreWarningAcceptancePending') is not True or report.get('manualSemanticReviewRequired') is not True:
            errors.append('older-backup behavior or separate native warning acceptance is obscured')
    if case in ('forget-stream', 'scope-revoke'):
        if report.get('proposalRejected') is not True or report.get('finishText') != '':
            errors.append('invalidated memory draft can publish or apply')
        if case == 'forget-stream' and (report.get('finishState') != 'failed' or report.get('finishRejected') is not False):
            errors.append('forgotten memory draft was not invalidated')
        if case == 'scope-revoke' and report.get('finishRejected') is not True:
            errors.append('revoked authority did not reject publication')
    if case in MEMORY_GENERATION_CASES:
        fact = 'Monday 21 April 2031 is an available due date.' if case == 'constraint' else 'Project Marigold launches on 14 April 2031.'
        heading = 'Launch note' if case == 'preference-on' else ''
        answer = json.loads(report.get('rawDraft', ''))
        if answer != {'title': heading, 'claims': [{'text': fact, 'quote': fact}]}:
            errors.append('memory answer violates independent fact, heading or constraint oracle')
        if not any(fact in source.get('text', '') and source.get('location', {}).get('pageId') == primary and not source.get('memoryId') for source in sources):
            errors.append('current primary evidence missing')
        citations = report.get('citations', [])
        allowed = {primary, supporting} if case == 'preference-on' else {primary}
        if not any(c.get('location', {}).get('pageId') == primary for c in citations) or any(c.get('location', {}).get('pageId') not in allowed or c.get('state') != 'current' for c in citations):
            errors.append('current source citation missing or contaminated')
    return errors


REVIEW_FACT = 'The security review is incomplete. Launch cannot proceed until that review is approved.'
REVIEW_DOCUMENTATION = 'Documentation is ready.'
REVIEW_SUGGESTION = 'Suggestion: request security review approval.'
REVIEW_GENERATION_CASES = frozenset(('daily-summary', 'weekly-summary', 'unresolved-tasks', 'changed-decision', 'risk-suggestion', 'no-owner', 'categories'))
REVIEW_BOUNDARY_CASES = frozenset(('stale-destination', 'forgotten-memory', 'cancel-retry', 'restart', 'dedup-import'))
REVIEW_EXCLUDED_TABLES = CANONICAL_EXCLUDED_TABLES | {'personal_review_runs', 'personal_review_model_runs'}
REVIEW_ASSERTIONS = {case: {'canonicalPreserved'} for case in REVIEW_GENERATION_CASES | REVIEW_BOUNDARY_CASES}
for _case in REVIEW_ASSERTIONS:
    if _case != 'restart': REVIEW_ASSERTIONS[_case].add('reservedBudget')
    if _case not in ('restart', 'forgotten-memory'):
        REVIEW_ASSERTIONS[_case].update(('sourceScope', 'independentAnswerOracle', 'currentCitations', 'readyUnapplied', 'approvalRequired'))
        if _case != 'stale-destination': REVIEW_ASSERTIONS[_case].update(('reviewedOnce', 'reviewedContentPresent', 'occurrenceDedup'))
REVIEW_ASSERTIONS['weekly-summary'].add('omissionsDisclosed')
REVIEW_ASSERTIONS['changed-decision'].add('memoryIncluded')
REVIEW_ASSERTIONS['risk-suggestion'].add('suggestionAppliedExactly')
REVIEW_ASSERTIONS['stale-destination'].add('staleApplyRejected')
REVIEW_ASSERTIONS['forgotten-memory'].add('forgetInvalidates')
REVIEW_ASSERTIONS['cancel-retry'].update(('cancelledInert', 'boundedRetry'))
REVIEW_ASSERTIONS['restart'].add('restartInert')
REVIEW_PORTABLE_TABLES = ('personal_review_activation', 'personal_review_runs', 'personal_review_model_runs', 'ai_proposals')
REVIEW_ASSERTIONS['dedup-import'].update({'portableNoGeneration', *('portableInert-' + table for table in REVIEW_PORTABLE_TABLES)})


def review_expected_draft(case):
    category = 'risks' if case == 'risk-suggestion' else 'decisions' if case == 'categories' else 'summary'
    claims = [{'category': category, 'text': REVIEW_FACT, 'quote': REVIEW_FACT}]
    actions = []
    if case == 'weekly-summary': claims.append({'category': 'summary', 'text': REVIEW_DOCUMENTATION, 'quote': REVIEW_DOCUMENTATION})
    if case == 'unresolved-tasks': claims = [{'category': 'summary', 'text': 'Security review is open.', 'quote': 'Status: To do'}]
    if case == 'risk-suggestion': actions.append({'kind': 'update_page', 'target': 'Current security decision', 'body': REVIEW_SUGGESTION})
    return {'claims': claims, 'actions': actions}


def review_draft_matches_v2(actual, expected):
    # Only claim prose may omit the initial article; all remaining structure,
    # source quotations and proposed actions must match the independent oracle.
    if not isinstance(actual, dict): return False
    claims = actual.get('claims')
    if not isinstance(claims, list) or len(claims) != len(expected['claims']): return False
    normalized = []
    for claim, wanted in zip(claims, expected['claims']):
        if not isinstance(claim, dict): return False
        text, expected_text = claim.get('text'), wanted['text']
        alternate = expected_text[4].upper() + expected_text[5:] if len(expected_text) > 4 and expected_text.startswith('The ') and 'a' <= expected_text[4] <= 'z' else expected_text
        normalized.append(dict(claim, text=expected_text) if text in (expected_text, alternate) else claim)
    return dict(actual, claims=normalized) == expected


def validate_review_report(report, case):
    errors = []
    if report.get('oracleVersion') != 2: errors.append('review report lacks the explicit v2 oracle version')
    model_case = case in REVIEW_GENERATION_CASES
    if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != int(model_case): errors.append('review native generation count differs')
    if report.get('serviceBoundaryOnly') is not (not model_case): errors.append('service-only boundary is not explicitly distinguished')
    expected_invocations = 0 if case == 'restart' else 2 if case == 'cancel-retry' else 1
    if type(report.get('generatorInvocations')) is not int or report['generatorInvocations'] != expected_invocations: errors.append('review callback count differs')
    bindings, plan = report['sourceBindings'], report['plan']
    ids = [bindings[k] for k in ('source', 'destination', 'private')]
    if not all(isinstance(v, str) and v for v in ids) or len(set(ids)) != 3: errors.append('review source identities conflated')
    if plan['generative']['modelPackId'] != report['modelPackId'] or plan.get('enabled') is not True: errors.append('review model/activation config differs')
    category = 'risks' if case == 'risk-suggestion' else 'decisions' if case == 'categories' else 'summary'
    actions = ['append_review', 'append_page'] if case == 'risk-suggestion' else ['append_review']
    if plan['generative']['categories'] != [category] or plan['generative']['allowedActions'] != actions or plan['generative']['actionPageIds'] != [bindings['source']]: errors.append('selected categories/action destinations differ')
    selected = [bindings['source']]
    if case == 'weekly-summary': selected.append(bindings['documentation'])
    if case == 'unresolved-tasks': selected.extend((bindings['openTask'], bindings['completedTask']))
    if plan['sourcePageIds'] != selected or bindings['private'] in selected: errors.append('selected source set differs')
    if plan['schedule']['frequency'] != ('weekly' if case == 'weekly-summary' else 'daily') or plan['schedule']['timezone'] != 'UTC': errors.append('review recurrence identity differs')
    if plan['includeMemory'] is not (case in ('changed-decision', 'forgotten-memory')): errors.append('review memory control differs')
    if case == 'restart':
        if report.get('finalStatus') != 'cancelled' or report.get('nextRunId') != '': errors.append('restart resumed interrupted occurrence')
        return errors
    if not all(type(report.get(k)) is int for k in ('promptTokens', 'outputCap', 'contextTokens')) or report['promptTokens'] < 0 or report['outputCap'] != 1024 or report['contextTokens'] != 8192 or report['promptTokens'] + 1024 + 16 > 8192: errors.append('review token budget not proved')
    if 'PRIVATE-REVIEW-73' in report['preparedPrompt']: errors.append('unselected private source entered review context')
    if case in ('changed-decision', 'forgotten-memory') and 'The security review is complete. Launch can proceed.' not in report['preparedPrompt']: errors.append('historical memory counterexample absent')
    if case == 'forgotten-memory':
        if report.get('finishRejected') is not True or report.get('proposalRejected') is not True or report.get('finalStatus') not in ('failed', 'cancelled'): errors.append('Forget did not invalidate generated review')
        return errors
    expected = review_expected_draft(case)
    actual_draft = json.loads(report.get('rawDraft', ''))
    if not review_draft_matches_v2(actual_draft, expected) or report.get('actions') != expected['actions']: errors.append('review output differs from independent fact/category/action oracle')
    sources, citations = report['sources'], report['citations']
    if report.get('finalStatus') != 'ready' or not citations or report.get('unapprovedApplyRejected') is not True: errors.append('review escaped its unapplied approval boundary')
    if any(c.get('state') != 'current' or c.get('location', {}).get('pageId') not in selected for c in citations): errors.append('review citations are stale or outside selection')
    for claim in expected['claims']:
        supporting = [s for s in sources if not s.get('memoryId') and claim['quote'] in s.get('text', '')]
        if not any(c.get('number') == s.get('number') and c.get('location') == s.get('location') for s in supporting for c in citations): errors.append('review claim lacks a current canonical supporting citation')
    if case == 'unresolved-tasks' and any(c.get('location', {}).get('pageId') != bindings['openTask'] for c in citations): errors.append('completed task mislabeled as open')
    if case == 'weekly-summary' and ('omitted' not in report.get('warning', '') or 'omitted' not in report.get('capturedWarning', '')): errors.append('bounded review omissions not disclosed')
    if case == 'stale-destination':
        if report.get('staleApplyRejected') is not True: errors.append('stale destination allowed a reviewed write')
        return errors
    effect = report['reviewedEffect']
    if effect.get('destinationPageId') != bindings['destination'] or effect.get('status') != 'accepted' or not all(type(effect.get(k)) is int for k in ('beforeBlocks', 'afterBlocks', 'retryBlocks')) or effect['beforeBlocks'] < 1 or effect['afterBlocks'] <= effect['beforeBlocks'] or effect['retryBlocks'] != effect['afterBlocks']: errors.append('reviewed effect or duplicate-apply bound differs')
    actual_claims = actual_draft.get('claims', []) if isinstance(actual_draft, dict) else []
    if not actual_claims or any(not isinstance(claim, dict) or not isinstance(claim.get('text'), str) or claim['text'] not in effect.get('destinationText', '') for claim in actual_claims): errors.append('approved review content missing from canonical destination')
    if case == 'risk-suggestion' and REVIEW_SUGGESTION not in effect.get('sourceText', ''): errors.append('reviewed suggestion not applied to exact destination')
    if report.get('repeatedOccurrenceRunId') != report.get('runId') or not report.get('runId'): errors.append('repeated occurrence changed identity')
    if case == 'cancel-retry' and report.get('cancelStatus') != 'cancelled': errors.append('cancelled occurrence did not remain inert before retry')
    if case == 'dedup-import' and (report.get('portableExecutionCounts') != dict.fromkeys(REVIEW_PORTABLE_TABLES, 0) or report.get('portableNextRunId') != ''): errors.append('portable review import activated or replayed execution')
    return errors


IMAGE_EXCLUDED_TABLES = CANONICAL_EXCLUDED_TABLES | {'image_descriptions'}
IMAGE_ASSERTIONS = frozenset(('serviceCompleted', 'independentAnswerOracle', 'provenance', 'ocrOracle', 'originalPreserved', 'canonicalPreserved'))

def validate_image_report(report, case, contract):
    errors = []
    expected = contract['imageCases'][case]
    if not expected.get('source') or not expected.get('sourceSHA256'):
        return ['authorized source and locked oracle unavailable']
    for key in ('sourceSHA256', 'sourceKind'):
        if report.get(key) != expected[key]: errors.append('image ' + key + ' differs')
    if report.get('question') != expected['prompt'] or report.get('expectedMethod') != expected['method']:
        errors.append('image prompt or method differs')
    if type(report.get('productInvocations')) is not int or type(report.get('ocrInvocations')) is not int or report.get('productInvocations') != 1 or report.get('ocrInvocations') != 1:
        errors.append('one product and one OCR invocation required')
    result = report.get('result', {})
    method = expected['method']
    if method == 'normalization_rejected':
        if result.get('state') != 'failed' or result.get('text') or not result.get('error', '').startswith('NORMALIZATION_DETAIL_LOST:'):
            errors.append('lost source detail was not rejected')
    elif result.get('state') != 'ready' or result.get('analysisMethod') != method:
        errors.append('ready image analysis method differs')
    elif method == 'local_vision_model':
        answer = result.get('text', '').strip().lower()
        if case.startswith('diagram-'):
            if answer not in (expected['expected'], expected['expected']+'.'): errors.append('color answer differs or adds unsupported content')
        elif case in ('boundary-4', 'boundary-5'):
            if not re.fullmatch(r'(?:a |there is a )?(?:small |tiny )?(?:faint|light) (?:gray|grey) (?:square|mark|dot|patch)(?: (?:on|against) a (?:plain )?white background)?\.?', answer): errors.append('faint-mark answer differs or adds unsupported content')
        elif not re.fullmatch(r'(?:a |there is a )?(?:small|tiny) (?:black|dark) (?:square|mark|dot)(?: (?:on|against) a (?:plain |white )?white background)?\.?', answer):
            errors.append('small-mark answer includes unsupported content')
        for key, identity in (('modelHash','modelSHA256'), ('projectorHash','projectorSHA256'), ('helperHash','helperSHA256')):
            if result.get(key) != contract[identity] or not re.fullmatch('[0-9a-f]{64}', result.get(key,'')): errors.append('image artifact identity differs: '+key)
        if result.get('finishReason') != 'eos' or not 0 < result.get('inputTokens',0) <= 8192 or not 0 < result.get('outputTokens',0) <= 512 or result.get('sampledPeakRSSBytes',0) <= 0:
            errors.append('complete bounded native image metrics absent')
    elif method == 'pixel_measurement':
        suffix = {
            'boundary-1': 'the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted.',
            'boundary-2': 'every source pixel has the same 16-bit RGB value #000000000000 and alpha 65535/65535. This uniform color has no spatial pixel variation to identify objects or text. No model interpretation or OCR was attempted.',
            'boundary-3': 'every source pixel is fully transparent. Hidden RGB values are not visible content. No visible objects or text can be identified. No model interpretation or OCR was attempted.',
        }[case]
        if not re.fullmatch(re.escape(f"Source pixel measurement ({expected['width']} × {expected['height']} decoded pixels, before resizing and alpha composition; encoded orientation): "+suffix),result.get('text','')):
            errors.append('exact pixel measurement differs')
        if result.get('finishReason') != 'measurement': errors.append('pixel measurement was not disclosed')
    if method != 'local_vision_model' and any(result.get(key) for key in ('modelHash','projectorHash','helperHash','inputTokens','outputTokens','sampledPeakRSSBytes')):
        errors.append('non-model measurement claims model output')
    ocr = report.get('ocr', {})
    lines = '\n'.join(line['text'].strip() for line in (ocr.get('lines') or []) if line['text'].strip())
    if report.get('ocrError') or lines != expected['ocrExpected'] or not report.get('ocrCapability',{}).get('engine') or not report.get('ocrCapability',{}).get('extractorVersion'):
        errors.append('OCR output or provenance differs')
    return errors

def validate_workflow_report(report, case, contract):
    """Independently bind native mode, declared controls, semantic observations
    and the required assertions. A passing mock or an empty assertion map cannot
    qualify a native run. The Go tests perform service-only boundary checks too.
    """
    errors = []
    try:
        for key in ('family', 'modelPackId', 'corpusSHA256'):
            if report.get(key) != contract[key]:
                errors.append(key + ' differs from the declared run')
        if report.get('engineMode') != 'native' or report.get('releaseQualified') is not False:
            errors.append('native execution mode or release boundary absent')
        family = contract['family']
        required = IMAGE_ASSERTIONS if family == 'images' else CONTEXT_ASSERTIONS[case] if family == 'context' else MEMORY_ASSERTIONS[case] if family == 'memory-lifecycle' else REVIEW_ASSERTIONS[case] if family == 'reviews' else RELATED_ASSERTIONS
        assertions = report.get('assertions', {})
        if not isinstance(assertions, dict) or not required.issubset(assertions) or any(value is not True for value in assertions.values()):
            errors.append('required assertion missing or failed')
        if family == 'context':
            attempts = 1 if case in CONTEXT_GENERATION_CASES else 0
            if type(report.get('generationAttempts')) is not int or report['generationAttempts'] != attempts:
                errors.append('generation attempt count differs')
            if case not in ('oversized-code', 'during-tokenization', 'framing-cost'):
                if not all(type(report.get(k)) is int for k in ('promptTokens', 'outputCap', 'contextTokens')) or report['promptTokens'] < 0 or report['outputCap'] != 512 or report['contextTokens'] != 8192 or report['promptTokens'] + report['outputCap'] + 16 > report['contextTokens']:
                    errors.append('reserved token budget not proved')
            if case == 'near-budget' and report.get('promptTokens', 0) < (8192 - 512 - 16) * 9 // 10:
                errors.append('near-capacity workload was not measured near its declared limit')
            if case == 'framing-cost':
                raw, framed = report.get('unframedTokens'), report.get('framedTokens')
                if type(raw) is not int or type(framed) is not int or type(report.get('contextTokens')) is not int or raw < 0 or raw >= framed or raw + 512 + 16 != report.get('contextTokens'):
                    errors.append('framing boundary not proved')
            if case in CONTEXT_GENERATION_CASES:
                fact = 'Project Marigold launches on 14 April 2031.'
                sources = report.get('sources', [])
                retained = any(fact in source.get('text', '') for source in sources)
                if report.get('retainedFact') is not retained:
                    errors.append('retained fact differs from actual supplied sources')
                answer = json.loads(report.get('rawDraft', ''))
                claims = answer.get('claims', [])
                if answer.get('title') != '' or len(claims) != 1:
                    errors.append('one answer without heading required')
                elif retained:
                    claim = claims[0]
                    quote = claim.get('quote', '')
                    if not re.fullmatch(r'(?:Project Marigold (?:launches|will launch|is scheduled to launch) on 14 April 2031|The launch date for Project Marigold is 14 April 2031)\.', claim.get('text', '')) or quote != fact or not any(quote in source.get('text', '') for source in sources):
                        errors.append('answer lacks declared fact and exact supplied support')
                    page = report.get('request', {}).get('pageId')
                    citations = report.get('citations', [])
                    if len(citations) != 1 or citations[0].get('location', {}).get('pageId') != page or citations[0].get('state') != 'current':
                        errors.append('current source citation absent')
                elif claims[0] != {'text': 'The supplied evidence does not answer this question.', 'quote': ''}:
                    errors.append('omitted fact requires exclusive abstention')
        elif family == 'images':
            errors.extend(validate_image_report(report, case, contract))
        elif family == 'reviews':
            errors.extend(validate_review_report(report, case))
        elif family == 'memory-lifecycle':
            errors.extend(validate_memory_report(report, case))
        elif family == 'related':
            if not isinstance(report.get('embeddingIdentity'), str) or not re.fullmatch('[0-9a-f]{64}', report['embeddingIdentity']):
                errors.append('verified native embedding identity absent')
            if type(report.get('embeddingCalls')) is not int or report['embeddingCalls'] < 2:
                errors.append('native indexing/retrieval calls absent')
            bindings = report['sourceBindings']
            semantic = report['semanticResults']
            ids = [hit['location']['pageId'] for hit in semantic['hits']]
            if semantic.get('mode') != 'semantic' or semantic.get('warning') or len(ids) != len(set(ids)) or bindings['current'] in ids or bindings['restricted'] in ids:
                errors.append('scoped semantic result disclosure differs')
            if bindings['relevant'] not in ids or (bindings['irrelevant'] in ids and ids.index(bindings['relevant']) >= ids.index(bindings['irrelevant'])):
                errors.append('declared relevant source did not outrank distraction')
            if any(hit['location'].get('sourceRevision') != '2' or not hit.get('excerpt') for hit in semantic['hits']):
                errors.append('current excerpt provenance absent')
            fallback = report['keywordFallback']
            if fallback.get('mode') != 'keyword' or not fallback.get('warning'):
                errors.append('keyword fallback was not disclosed')
        else:
            errors.append('unknown workflow family')
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        errors.append('malformed workflow evidence: ' + str(error))
    return errors


def summarize(events, expected, parent, canonical_root=None, expected_requests=None, workflow_contract=None):
    """Require exactly the independently declared cases and their task reports."""
    terminals, reports, errors, buffers = {}, {}, [], {}
    if parent in (FAMILIES['p0'][1], FAMILIES['p0-holdouts'][1]) and expected_requests is None:
        errors.append('P0 qualification requires locked request controls')
    for event in events:
        name = event.get('Test', '')
        prefix = parent + '/'
        if name.startswith(prefix) and event.get('Action') in ('pass', 'fail', 'skip'):
            case = name[len(prefix):]
            if case in terminals:
                errors.append('duplicate terminal: ' + case)
            terminals[case] = event['Action']
        if not name.startswith(prefix) or event.get('Action') != 'output':
            continue
        # test2json splits long log lines across Output events (including inside
        # JSON strings). Reassemble per test before parsing; never join tests.
        buffers[name] = buffers.get(name, '') + event.get('Output', '')
        while '\n' in buffers[name]:
            output, buffers[name] = buffers[name].split('\n', 1)
            start = output.find('{')
            if start < 0:
                continue
            try:
                report = json.loads(output[start:])
            except json.JSONDecodeError:
                continue
            if 'fixture' not in report:
                continue
            case = report['fixture']
            if case in reports:
                errors.append('duplicate report: ' + case)
            if name != prefix + case:
                errors.append('report/test mismatch: ' + case)
            reports[case] = report
    # Unterminated output never counts as a complete evidence record.
    for name, output in buffers.items():
        if output.strip():
            errors.append('unterminated output line: ' + name)
    missing = sorted(set(expected) - set(terminals))
    unexpected = sorted(set(terminals) - set(expected))
    for case in expected:
        r = reports.get(case, {})
        if expected_requests is not None:
            if case not in expected_requests:
                errors.append('request contract missing for case: ' + case)
            else:
                request_error = validate_request_report(r, expected_requests[case])
                if request_error:
                    errors.append('request contract ' + case + ': ' + request_error)
        if workflow_contract is not None:
            errors.extend('workflow ' + case + ': ' + message for message in validate_workflow_report(r, case, workflow_contract))
        passed = r.get('passed', r.get('automatedTaskPassed')) is True
        excluded = IMAGE_EXCLUDED_TABLES if workflow_contract and workflow_contract.get('family') == 'images' else REVIEW_EXCLUDED_TABLES if workflow_contract and workflow_contract.get('family') == 'reviews' else None
        canonical_error = validate_canonical_inventory(r, canonical_root, parent + '/' + case, excluded)
        if excluded is not None:
            for name in (['beforeExplicitMutationInventory'] if case == 'forgotten-memory' else ['staleApplyInventory'] if case == 'stale-destination' else []):
                auxiliary = dict(r, canonicalInventory=r.get(name), mutationHistoryCountUnchanged=True)
                auxiliary_error = validate_canonical_inventory(auxiliary, canonical_root, parent + '/' + case, excluded)
                if auxiliary_error: errors.append('review phase ' + name + ': ' + auxiliary_error)
        if canonical_error:
            errors.append('canonical inventory ' + case + ': ' + canonical_error)
        if terminals.get(case) != 'pass' or not passed or r.get('canonicalUnchanged') is not True:
            errors.append('required case did not pass: ' + case)
    if set(reports) != set(expected):
        errors.append('task-report inventory differs from locked corpus')
    if expected_requests is not None and set(expected_requests) != set(expected):
        errors.append('request-contract inventory differs from locked case selection')
    return {'reportedTaskPasses': sorted(k for k, v in reports.items() if v.get('passed', v.get('automatedTaskPassed')) is True),
            'reportedTaskFailures': sorted(k for k, v in reports.items() if v.get('passed', v.get('automatedTaskPassed')) is False),
            'expectedCases': len(expected), 'passedCases': sum(terminals.get(k) == 'pass' for k in expected),
            'missingCases': missing, 'unexpectedCases': unexpected, 'errors': errors,
            'automatedCorpusPassed': not missing and not unexpected and not errors,
            'manualSemanticReviewPending': sorted(k for k, v in reports.items() if v.get('manualSemanticReviewRequired')),
            'canonicalEvidence': {k: v.get('canonicalInventory') for k, v in reports.items()},
            'releaseQualified': False}

def atomic_json(path, value):
    """A full disk or failed rename must not destroy the previous manifest."""
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, prefix='.manifest-', delete=False) as stream:
            temporary = pathlib.Path(stream.name)
            stream.write(json.dumps(value, indent=2) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def require_space(paths, minimum=4 * 1024**3):
    free = {str(path): shutil.disk_usage(path).free for path in paths}
    if not free or min(free.values()) < minimum:
        raise ValueError('need at least 4 GiB free on evidence and temporary volumes; measured ' + json.dumps(free))
    return free


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ['go', 'go-mod-cache', 'go-build-cache', 'bundle', 'models', 'pack', 'output']:
        p.add_argument('--' + name, required=True)
    p.add_argument('--family', choices=FAMILIES, required=True)
    p.add_argument('--case', action='append', dest='cases', help='explicit focused case; full-corpus pass remains false')
    args = p.parse_args()
    if platform.system() != 'Darwin':
        p.error('this runner requires macOS sandbox-exec; qualify other platform network denial separately')
    corpus_file, test, count = FAMILIES[args.family]
    corpus_dir = ROOT / 'internal/workspace/testdata/expanded-evaluation'
    corpus = json.loads((corpus_dir / corpus_file).read_text())
    expected = [c['id'] for c in corpus['cases']]
    if len(expected) != count or len(set(expected)) != count:
        p.error('locked corpus count/identity mismatch')
    if args.cases:
        if len(set(args.cases)) != len(args.cases) or not set(args.cases).issubset(expected):
            p.error('unknown or duplicate focused case')
        expected = [case for case in expected if case in args.cases]
    out = pathlib.Path(args.output).resolve()
    # Model runs need headroom for temporary workspaces and host memory pressure.
    # This is a conservative harness precondition, not a product storage target.
    try:
        free_space = require_space([out.parent, pathlib.Path(tempfile.gettempdir())])
    except (OSError, ValueError) as error:
        p.error(str(error))
    # Fresh directory only: a failed run is never replaced by a later attempt.
    out.mkdir(mode=0o700)
    reserve = out / '.evidence-reserve'
    reserve.write_bytes(bytes(1024 * 1024))
    bundle, models = pathlib.Path(args.bundle).resolve(), pathlib.Path(args.models).resolve()
    paths = [corpus_dir / corpus_file, ROOT / 'internal/workspace/ai_expanded_evaluation_test.go',
             ROOT / 'internal/workspace/ai_p0_closure_holdouts_test.go',
             ROOT / 'internal/workspace/ai_expanded_modes_test.go', pathlib.Path(__file__),
             bundle / 'manifest.json', bundle / 'llama-worker', models / (args.pack + '.json'),
             models / (args.pack + '.model')]
    # Bind all local production Go/SQL/schema inputs, not only the oracle files.
    paths += list((ROOT / 'internal').rglob('*.go')) + list((ROOT / 'contracts').rglob('*.go'))
    paths += list((ROOT / 'contracts').rglob('*.json')) + list((ROOT / 'internal').rglob('*.sql'))
    paths += list((ROOT / 'schemas').glob('*.go')) + list((ROOT / 'schemas').glob('*.sql'))
    paths += [ROOT / 'go.mod', ROOT / 'go.sum']
    if args.family == 'images':
        pack_manifest = json.loads((models / (args.pack + '.json')).read_text())
        paths += [corpus_dir / 'images.json', models / (pack_manifest['projector']['id'] + '.model')]
        paths += [corpus_dir / case['source'] for case in corpus['cases'] if case.get('source')]
        paths += [path for path in bundle.iterdir() if path.is_file()]
    paths = sorted(set(paths))
    started = time.time()
    before = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    status = subprocess.check_output(['git', 'status', '--short'], cwd=ROOT, text=True)
    manifest = {'schemaVersion': 1, 'sourceCommit': source, 'workingTreeStatus': status,
                'family': args.family, 'inputs': before, 'hardware': platform.platform(),
                'networkPolicy': '(version 1)(allow default)(deny network*)',
                'performanceQualified': False, 'releaseQualified': False, 'state': 'started', 'initialFreeBytes': free_space,
                'canonicalEvidenceDirectory': str(out / 'canonical'),
                'canonicalEvidenceScope': 'Retained typed canonical rows and exact asset bytes; runtime temporary workspace itself is not retained.'}
    def write(): atomic_json(out / 'manifest.json', manifest)
    write()
    # Deliberately omit ambient provider/API credentials. No account is involved.
    env = {k: os.environ[k] for k in ['PATH', 'HOME', 'TMPDIR', 'SDKROOT', 'DEVELOPER_DIR'] if k in os.environ}
    env.update({'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off',
                'GOMODCACHE': str(pathlib.Path(args.go_mod_cache).resolve()),
                'GOCACHE': str(pathlib.Path(args.go_build_cache).resolve())})
    sandbox = ['/usr/bin/sandbox-exec', '-p', manifest['networkPolicy']]
    binary = out / 'evaluation.test'
    build = [str(pathlib.Path(args.go).resolve()), 'test', '-mod=readonly', '-tags',
             'fts5 sqlite_omit_load_extension', '-c', '-o', str(binary), './internal/workspace']
    manifest['buildCommand'] = build
    with open(out / 'build.log', 'w') as log:
        build_result = subprocess.run(sandbox + build, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    if build_result.returncode:
        manifest.update(state='build_failed', exitCode=build_result.returncode); write(); return 1
    manifest['testBinarySHA256'] = digest(binary)
    env.update(TROVE_NATIVE_INTEGRATION='1', TROVE_CHAT_EVAL_PACK=args.pack, TROVE_EMBED_EVAL_PACK=args.pack, TROVE_VISION_EVAL_PACK=args.pack,
               TROVE_MODEL_ROOT=str(models), TROVE_NATIVE_BUNDLE=str(bundle),
               TROVE_AI_CANONICAL_EVIDENCE_DIR=manifest['canonicalEvidenceDirectory'])
    selector = '^' + test + '$'
    if args.cases:
        selector += '/^(' + '|'.join(re.escape(case) for case in expected) + ')$'
    command = [str(pathlib.Path(args.go).resolve()), 'tool', 'test2json', '-t', '-p',
               'trove.local/phase0/internal/workspace', str(binary), '-test.run', selector,
               '-test.v', '-test.timeout=150m']
    manifest['runCommand'] = command; write()
    with open(out / 'results.jsonl', 'w') as log:
        run = subprocess.run(sandbox + command, cwd=ROOT / 'internal/workspace', env=env,
                             stdout=log, stderr=subprocess.STDOUT)
    reserve.unlink(missing_ok=True)
    events, malformed = [], []
    for number, line in enumerate((out / 'results.jsonl').read_text().splitlines(), 1):
        try: events.append(json.loads(line))
        except json.JSONDecodeError: malformed.append(number)
    expected_requests = None
    if args.family in ('p0', 'p0-holdouts'):
        expected_requests = {case['id']: {'useMemory': case['useMemory'], 'modelPackId': args.pack,
                             'corpusSHA256': before[str(corpus_dir / corpus_file)]['sha256']}
                             for case in corpus['cases'] if case['id'] in expected}
    workflow_contract = None
    if args.family in ('context', 'related', 'memory-lifecycle', 'reviews', 'images'):
        workflow_contract = {'family': args.family, 'modelPackId': args.pack,
                             'corpusSHA256': before[str(corpus_dir / corpus_file)]['sha256']}
    if args.family == 'images':
        workflow_contract.update(imageCases={case['id']: case for case in corpus['cases']},
            modelSHA256=before[str(models / (args.pack + '.model'))]['sha256'],
            projectorSHA256=before[str(models / (pack_manifest['projector']['id'] + '.model'))]['sha256'],
            helperSHA256=before[str(bundle / 'vision-worker')]['sha256'])
    summary = summarize(events, expected, test, out / 'canonical', expected_requests, workflow_contract)
    after = {str(x): {'sha256': digest(x), 'bytes': x.stat().st_size} for x in paths}
    summary['inputsUnchanged'] = before == after
    summary['malformedLogLines'] = malformed
    summary['automatedCorpusPassed'] &= run.returncode == 0 and before == after and not malformed
    selection_passed = summary['automatedCorpusPassed']
    summary['automatedSelectionPassed'] = selection_passed
    summary['fullFamilyRun'] = len(expected) == count
    summary['automatedCorpusPassed'] &= summary['fullFamilyRun']
    manifest.update(state='finished', exitCode=run.returncode, elapsedSeconds=time.time()-started,
                    summary=summary, outputs={n: digest(out/n) for n in ['build.log', 'results.jsonl']})
    write()
    print(json.dumps(summary, indent=2))
    return 0 if selection_passed else 1

if __name__ == '__main__':
    raise SystemExit(main())
