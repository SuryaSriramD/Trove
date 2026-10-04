#!/usr/bin/env python3
"""Link Phase 1 implementation to inspected, passing product evidence.

Manual distribution/input/capture qualifications remain blocked independently.
This never promotes the remaining P0 import/search/AI requirements.
"""
import json
import hashlib
import argparse
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
path = ROOT / 'docs/phase-0/requirements-ledger.json'
ledger = json.loads(path.read_text())
parser = argparse.ArgumentParser()
parser.add_argument('--checkpoint', type=Path, help='Use an explicitly preserved historical Phase 1 package report, without attesting the current source tree.')
args = parser.parse_args()
report = json.loads((args.checkpoint or ROOT / 'evidence/phase1/verification.json').read_text())
if args.checkpoint and (report.get('qualificationScope') != 'preserved-phase1-package' or report.get('currentWorkingTreeVerified') is not False):
    raise SystemExit('Checkpoint must explicitly distinguish the preserved package from the current working tree')
if report['result'] != 'passed':
    raise SystemExit('Refusing to verify requirements from a failed run')
for name, digest in report['inputSha256'].items():
    source = ROOT / name
    if not source.is_file() or hashlib.sha256(source.read_bytes()).hexdigest() != digest:
        raise SystemExit('Source changed since verification: ' + name)
report_path = report['runEvidencePath']
if json.loads((ROOT / report_path).read_text()) != report:
    raise SystemExit('Immutable run report does not match the passing report')
checks = {c['name'] for c in report['checks'] if c['result'] == 'passed'}
required = {'contracts','go-race','frontend-types','frontend-codec','frontend-browser','database-browser','desktop-build','packaged-restart-native','source-stability'}
if args.checkpoint:
    required.remove('source-stability')
if not required <= checks:
    raise SystemExit('Missing final product checks: ' + ', '.join(required - checks))
evidence_id = 'PH1-PRODUCT'
catalog = {
 'PH1.KERNEL': ('Production SQLite, policy, job recovery and race acceptance', 'internal/workspace/kernel_test.go'),
 'PH1.EDITOR': ('Renderer codec, clipboard, rendering and keyboard scenarios', 'frontend/tests/browser.spec.ts'),
 'PH1.DATABASE': ('Canonical records, property values, views and task invariants', 'internal/workspace/database_test.go'),
 'PH1.RECOVERY': ('Files, revisions, restore, backup and retention acceptance', 'internal/workspace/recovery_test.go'),
 'PH1.NATIVE': ('Real models, native failure recovery and durable transcription', 'internal/native/native_test.go'),
 'PH1.PACKAGE': ('Real Wails create/restart/reopen and bundled model launches', 'scripts/verify-packaged.py'),
 'PH1.MANUAL': ('Native IME/VoiceOver, real microphone and distribution qualification', 'docs/phase-1/integration.md'),
}
for key,(description,source) in catalog.items():
    ledger['testCatalog'][key] = dict(id=key,description=description,path=source,availability='implemented' if key != 'PH1.MANUAL' else 'planned')
ledger['evidenceCatalog'][evidence_id] = dict(path=report_path,result='passed',command='python3 scripts/verify-phase1.py',recordedAt=report['recordedAt'],scope='product',tests=[x for x in catalog if x != 'PH1.MANUAL'],meaning='Automated product evidence with source fingerprints; manual release gates remain explicit and separate.')
if args.checkpoint:
    ledger['evidenceCatalog'][evidence_id]['meaning'] = report['qualificationNotes']
    ledger['evidenceCatalog'][evidence_id]['verificationTarget'] = report['app']

# id, package, workstream, requirement, implementation artifacts, tests, verification selectors
rows = [
 ('A-STORAGE','1A','WP02','Workspace creation/opening, one active owner, atomic migrations, foreign keys, WAL, serialized writes and revision conflicts.', ['internal/workspace/kernel.go','internal/workspace/mutations.go'], ['PH1.KERNEL'], ['TestWorkspaceRestartAndExclusiveOwnership','TestAtomicBatchRollbackAndConflict','TestConcurrentStaleWriters','TestMigrationFailureRollsBackWholeSchema']),
 ('A-POLICY','1A','WP02','All canonical writes use workspace services with authenticated scope, actor, origin, approval, idempotency and committed mutation records.', ['internal/workspace/permissions.go','internal/workspace/mutations.go','internal/workspace/events.go'], ['PH1.KERNEL'], ['TestScopeAndForgedContext','TestExactApprovalRequiredConsumedAndReplay','TestIdempotencyAndCommittedEvents','TestProposalHashPreservesExplicitNull']),
 ('A-JOBS','1A','WP07','Persistent jobs have progress, bounded retry, cancellation, restart recovery, source checks and atomic result-unit receipts.', ['internal/workspace/jobs.go','internal/workspace/job_commits.go','internal/workers/runner.go'], ['PH1.KERNEL','PH1.NATIVE'], ['TestJobUnitReceiptAtomicFenceAndSourceConflict','TestJobCannotBypassApprovalAndDelegationRevocation','TestCancellationCommitLinearizationAndReplay','TestTransientRetriesAreBoundedAndDurable','TestRunJobsShutdownFencesUnfinishedAttempt','TestRealDurableTranscriptionJob']),
 ('B-NAV','1B','WP03','Desktop Home, Inbox, Pages, Tasks, Meetings, Search, Import and Settings, with tree navigation, favorites, recents, tabs, breadcrumbs, history and quick switching.', ['frontend/src/App.tsx','app.go'], ['PH1.EDITOR','PH1.PACKAGE'], ['frontend-browser','packaged-restart-native']),
 ('B-EDITOR','1B','WP03','Canonical rich text and core blocks, media, slash/Markdown editing, transforms, duplication, find, undo and redo persist through the workspace service.', ['frontend/src/editor/Editor.tsx','frontend/src/editor/codec.ts'], ['PH1.EDITOR','PH1.PACKAGE'], ['frontend-codec','frontend-browser','packaged-restart-native']),
 ('B-PRESERVATION','1B','WP03','Imported H4, merged cells, content tabs, unsupported payloads, parent identities and mixed clipboard content remain recoverable.', ['frontend/src/editor/codec.ts','frontend/tests/codec.test.ts'], ['PH1.EDITOR'], ['frontend-codec','frontend-browser']),
 ('B-NAV-SCENARIOS','1B','WP03','The renderer opens and renames a page, saves favorites, moves keyboard focus and navigates the quick switcher.', ['frontend/tests/browser.spec.ts'], ['PH1.EDITOR'], ['frontend-browser']),
 ('B-EDITOR-SCENARIOS','1B','WP03','Renderer scenarios preserve Unicode, mixed clipboard identities and imported structures; render code, Mermaid and KaTeX; support Markdown heading, duplication, undo/redo and save/reopen through the fixture service.', ['frontend/tests/browser.spec.ts','frontend/tests/codec.test.ts'], ['PH1.EDITOR'], ['frontend-codec','frontend-browser']),
 ('C-ENGINE','1C','WP04','Canonical records with page bodies, typed editable properties and preserved imported identities, labels, timestamps and places.', ['internal/workspace/database.go','frontend/src/database/DatabaseView.tsx'], ['PH1.DATABASE'], ['TestDatabaseTypedPropertiesPreserveImportMetadata','TestDatabaseClearValuePreservesTaskAndTitleInvariants','TestDatabaseFileValuesGrantScopedAccessAfterRestart']),
 ('C-VIEWS','1C','WP04','Saved table/list/board/calendar views share records; recursive filters, sorting, grouping/subgrouping, visible properties, board moves and calendar rescheduling persist.', ['internal/workspace/database.go','frontend/src/database/DatabaseView.tsx'], ['PH1.DATABASE'], ['TestDatabaseFourViewsShareRecordsAndRestart','TestDatabaseFiltersDecimalSortCursorAndAtomicity','TestDatabaseContainersDoNotOwnRows','database-browser']),
 ('C-TASKS','1C','WP04','One task facet per canonical record and one completion status shared by to-do occurrences; a unique system collection owns standalone tasks.', ['internal/workspace/tasks.go','frontend/src/database/service.ts'], ['PH1.DATABASE'], ['TestTasksCanonicalStatusOccurrencesPromotionAndRestart','TestTasksRejectPromotionWithoutCompletionMappings','TestTaskStaleMutationAndMissingMappings']),
 ('D-ASSETS','1D','WP09','Immutable hashed assets deduplicate, expose only scoped file IDs and retain valid attachment references after restart.', ['internal/workspace/files.go','app.go'], ['PH1.RECOVERY'], ['TestAssetDeduplicationScopedReadsAndRestart','TestFileSelectionsRejectSymlinksAndInvalidHandles','TestAttachmentReferencesFollowCoverPropertiesAndBlockChanges']),
 ('D-HISTORY','1D','WP09','Timestamped page revisions, trash, restoration previews and recovery points restore page/record/task content with revision guards.', ['internal/workspace/history.go','frontend/src/recovery/RecoveryPanel.tsx'], ['PH1.RECOVERY'], ['TestTrashHistoryRestoreAndStalePreview','TestRestoreDeletedBlockKeepsMonotonicRevision','TestRecordTaskHistoryRestoresValuesWithoutCopyingIdentity']),
 ('D-BACKUP','1D','WP09','Consistent SQLite snapshots and verified asset manifests retain content/history/active-backup assets and recover interrupted backup state.', ['internal/workspace/backup.go','internal/workspace/job_backup.go'], ['PH1.RECOVERY'], ['TestBackupSnapshotRetentionAndRecoveryPoint','TestAbandonedAssetsBackupPinsAndTamperDetection','TestCancelledBackupAndRestartRecoverAbandonedPins','TestBackupDestinationJobAndLostAcknowledgement']),
 ('E-WORKERS','1E','WP07','Bundled llama.cpp, whisper.cpp and microphone workers use versioned bounded messages, streaming, cancellation, failure cleanup and resource scheduling.', ['internal/native/supervisor.go','internal/native/microphone.go','internal/native/scheduler.go','native/llama-worker.cpp','native/whisper-worker.cpp','native/microphone-worker.swift'], ['PH1.NATIVE'], ['TestFramesRejectMalformed','TestSchedulerRecordingPreemptionAndEditing','TestRealQualification','TestRealWorkerCancellationCrashAndMetal']),
 ('E-MODELS','1E','WP01','Separate chat, embedding and speech packs support verified download/import, compatibility checks, leased removal protection and interrupted acquisition recovery.', ['internal/native/packs.go','native/models/catalog.json','app_models.go'], ['PH1.NATIVE'], ['TestModelImportVerifyLeaseRemove','TestAcquisitionResumesAfterManagerRestart']),
 ('E-LOCAL-PACKAGE','1E','WP01','Development-signed arm64 Wails bundle launches actual verified native workers with system-only dynamic dependencies.', ['scripts/build-native.py','scripts/build-desktop.py','main.go'], ['PH1.PACKAGE'], ['desktop-build','packaged-restart-native']),
 ('INTEGRATION','Integration','WP09','Create a page and database record, attach a file, restart the packaged application and reopen the same identities, Unicode content and attachment bytes.', ['internal/integration/scenario.go','scripts/verify-packaged.py'], ['PH1.PACKAGE'], ['packaged-restart-native']),
]
known_tests = {x['test'].rsplit('.',1)[-1]: x['test'] for x in report['tests'] if x['result']=='passed'}
new_ids = {'PH1-'+r[0] for r in rows} | {'PH1-B-NATIVE-INPUT','PH1-E-DISTRIBUTION','PH1-E-REAL-MIC'}
ledger['requirements'] = [r for r in ledger['requirements'] if r['id'] not in new_ids]
for ident,phase,wp,requirement,artifacts,tests,selectors in rows:
    missing = set(selectors) - checks - known_tests.keys()
    if missing: raise SystemExit(ident + ' missing concrete passing checks: ' + ','.join(missing))
    for artifact in artifacts:
        if not (ROOT/artifact).is_file(): raise SystemExit('Missing artifact '+artifact)
    row = dict(id='PH1-'+ident,phase='Phase1',subphase=phase,requirement=requirement,release='P0',scope='product',workPackage=wp,owner=ledger['workPackages'][wp]['owner'],decisionState='accepted',sourceReferences=[dict(document='USER',section='Phase 1 request',anchor=requirement)],amendmentFamily=None,auditIds=[],acceptanceCriteria=[requirement,'Retain concrete automated evidence; distinguish remaining manual qualification.'],tests=tests,evidence=[evidence_id],implemented=True,verified=True,implementationStatus='implemented',verificationStatus='passed',artifacts=artifacts,verificationChecks=[dict(evidence=evidence_id,kind='check' if s in checks else 'test',name=s) for s in selectors])
    for selector in row['verificationChecks']:
        if selector['kind'] == 'test':
            selector['name'] = known_tests[selector['name']]
    row['verificationTarget'] = report.get('app', 'source fingerprint set in the immutable run report')
    if ident in {'B-NAV','B-EDITOR'}:
        row.update(verified=False, verificationStatus='blocked')
        row['verificationNotes'] = 'Implementation exists; automated scenarios establish a subset of interactions. Nine expanded renderer scenarios pass separately, but exhaustive native/editor acceptance remains unverified. See docs/phase-1/1b.md for the precise coverage table. Do not infer every interaction from one passing browser suite.'
    ledger['requirements'].append(row)
for ident,phase,wp,requirement,reason,implemented in [
 ('B-NATIVE-INPUT','1B','WP03','Support native Indic/CJK candidate selection and VoiceOver navigation in the packaged editor.','Unicode/composition events and accessible semantics passed; native candidate selection and actual VoiceOver speech/navigation have not been established.',True),
 ('E-DISTRIBUTION','1E','WP01','Developer ID sign/notarize the application and verify installation on a separate clean target Mac.','No valid Developer ID identity or second clean Mac is available; local ad-hoc package qualification is recorded separately.',False),
 ('E-REAL-MIC','1E','WP07','Support macOS microphone authorization and private-audio recording through the native capture worker.','Synthetic capture passed; no private microphone audio was captured during development qualification.',True),
]:
    ledger['requirements'].append(dict(id='PH1-'+ident,phase='Phase1',subphase=phase,requirement=requirement,release='P0',scope='product',workPackage=wp,owner=ledger['workPackages'][wp]['owner'],decisionState='accepted',sourceReferences=[dict(document='USER',section='Phase 1 request',anchor=requirement)],amendmentFamily=None,auditIds=[],acceptanceCriteria=[requirement],tests=['PH1.MANUAL'],evidence=[],implemented=implemented,verified=False,implementationStatus='implemented' if implemented else 'partial',verificationStatus='blocked',artifacts=['docs/phase-1/integration.md'],verificationNotes=reason))
# Mark existing broad source requirements as having implementation progress,
# without replacing their independent release acceptance definitions or tests.
for row in ledger['requirements']:
    if row['release']!='P0' or row['scope']!='product' or row['id'].startswith('PH1-'): continue
    wp=row['workPackage']
    if wp in {'WP02','WP03','WP04','WP09'} and row['implementationStatus']=='not_started' and row['id']!='BASELINE-SQLITE-VEC-RELEASE':
        row['implementationStatus']='partial'
        row['phase1Evidence']=[evidence_id]
        row['verificationNotes']='Phase 1 foundation is implemented. Exact phase-specific acceptance is tracked by PH1 rows; the complete original release requirement retains its own acceptance gate.'
ledger['title']='Trove contracts and product requirements ledger'
ledger['implementationPhase']='Phase1'
if args.checkpoint:
    ledger['phase1Qualification'] = dict(target=report['app'], binarySha256=report['appBinarySha256'], currentWorkingTreeVerified=False, reason=report['qualificationNotes'])
else:
    ledger['phase1Qualification'] = dict(currentWorkingTreeVerified=True, evidence=report_path)
path.write_text(json.dumps(ledger,indent=2)+'\n')
print(f"Recorded {len(new_ids)} Phase 1 requirements with separate manual gates")
