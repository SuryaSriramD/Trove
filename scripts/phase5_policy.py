"""Explicit TRD evidence selectors. Missing, skipped or stale tests fail closed.

Fixtures qualify the stated local contract, never a vendor account, physical
device or an unobserved native UI interaction. Keep those limits in the report.
"""
MODULE = 'trove.local/phase0'
WORKSPACE = MODULE + '/internal/workspace.'
INTEGRATION = MODULE + '/internal/integration.'
OFFLINE_TESTS = [INTEGRATION + 'TestP1PublicServiceJourney'] + [WORKSPACE + name for name in [
    'TestPhase5RecoveryP1ObjectGraphAndOfflineResume',
    'TestPhase5RecoveryExternalAuthorityRequiresFreshConsent',
    'TestPhase5RecoveryLinkedViewsMoveReplayAndRestartIdentity',
    'TestPhase5ProvenanceSuppliedMetadataAndOpaqueEvidenceSurviveRestore',
    'TestPhase5EditorMergedTablesTabsRestoreCanonicalContent',
    'TestPhase5FormulaTypesBlanksClocksAndLimits',
    'TestPhase5FormulaRelationScopeAndEmptyAggregation',
    'TestPhase5AutomationHalfHourDSTUsesFirstOccurrence',
    'TestPhase5AutomationSkippedConditionNeverReplaysAfterRestart',
]]

FAMILIES = {
    'V01': ('Identity', [
        'TestPhase5RecoveryLinkedViewsMoveReplayAndRestartIdentity',
        'TestDatabaseFourViewsShareRecordsAndRestart',
        'TestPortableCanonicalDatabaseRoundTripRelationsFormulasAndLinkedViews',
        'TestReciprocalRelationsAndCardinalityAreAtomic'], ['packaged-p1-completeness']),
    'V02': ('Import reconciliation', [
        'TestPhase5V02APIAuthenticationNetworkFailuresAndOfflineRestart',
        'TestPhase5V02V03HTMLSourceEvidenceSurvivesRestart',
        'TestPortableImportResumesAndRequiresConflictReview',
        'TestHTMLImportRetainsInertEvidenceAndRewritesLinks',
        'TestNotionCaptureResumesPaginationDownloadsOfflineAndRequiresReview',
        'TestNotionDeniedRootRetainsCursorAndNeedsExplicitPartialFinish'], ['packaged-fixture-completeness']),
    'V03': ('Provenance', [
        'TestPhase5ProvenanceSuppliedMetadataAndOpaqueEvidenceSurviveRestore',
        'TestPhase4PortableMetadataRetainedWithoutActivation',
        'TestRestoreSecondInstallationPreservesCanonicalHistoryAndEvidence',
        'TestImportedFormulaTranslationRetainsSeparateEvidence'], ['release-inventory-completeness']),
    'V04': ('Editor', [
        'TestPhase5EditorMergedTablesTabsRestoreCanonicalContent',
        'TestAdvancedEditorCycleAndLayoutOwnershipAreAtomic',
        'TestAdvancedEditorTableGeometryRejectsOverlaps',
        'TestAdvancedEditorRecoveryPreservesContentAndButtonProposalsDoNotExecute'], ['frontend-unit', 'frontend-browser-test-completeness']),
    'V05': ('Formulas', [
        'TestFormulaIndependentReferenceFixtures',
        'TestAdvancedFormulaEvidenceRollupAndInvalidation',
        'TestPhase5FormulaTypesBlanksClocksAndLimits',
        'TestPhase5FormulaRelationScopeAndEmptyAggregation'], ['database-browser-test-completeness']),
    'V06': ('Recovery', [
        'TestPhase5RecoveryP1ObjectGraphAndOfflineResume',
        'TestPhase5RecoveryExternalAuthorityRequiresFreshConsent',
        'TestRestoreSubtreeIsAtomicAndPreservesUnrelatedValues',
        'TestReciprocalRelationsRecoverBothGuardedEndpoints',
        'TestRestoreRejectsCorruptUnsafeAndChangedBackups'], ['packaged-p1-completeness', 'packaged-fixture-completeness']),
    'V07': ('Automation', [
        'TestAutomationAtomicDedupRestartAndOrigin',
        'TestAutomationRollbackFailureRetryAndPause',
        'TestAutomationCalendarDSTAndMissedPolicies',
        'TestAutomationExcludedOriginsAndEventDedup',
        'TestPhase5AutomationHalfHourDSTUsesFirstOccurrence',
        'TestPhase5AutomationSkippedConditionNeverReplaysAfterRestart'], ['p1-offline-test-completeness', 'p1-offline-required-tests']),
    'V08': ('Offline operation', [
        'TestSearchTranscriptNavigationAndNoModelFallback',
        'TestAdvancedEditorTemplateEvidenceReferencesAndSearch'], [
        'os-network-denial-control', 'go-race-real-offline-test-completeness',
        'packaged-fixture-completeness', 'p1-offline-test-completeness', 'p1-offline-required-tests', 'packaged-p1-completeness']),
    'V09': ('AI evidence', [
        'TestAIComparisonMemoryAndLiveAuthority',
        'TestAIExactQuoteAttributionAndStaleConversationContext',
        'TestAdvancedAIDatabaseDraftReviewAndIdempotency',
        'TestAdvancedAICodeLiteralAndStaleReview'], ['go-race-real-offline-test-completeness']),
    'V10': ('Tool boundaries', [
        'TestPhase5V10PairedRelayProtocolConsentAndCancellation',
        'TestProviderScopedReadReviewRetryAndDisconnect',
        'TestProviderStaleRevisionAndRevocationBeforeCommit',
        'TestProviderDisconnectCancelsApprovalBeforeCommit',
        'TestProviderUnqualifiedVendorBlocked',
        'TestApprovedRepositoryIndexExcludesSecretsAndTracksMoves'], ['paired-relay-tests']),
}

PACKAGES = {
    '4A': {'name': 'Advanced editing', 'families': ['V01','V03','V04','V06','V08'], 'limits': 'Rich nested layout controls and native clipboard/device interactions need separate observation.'},
    '4B': {'name': 'Advanced databases', 'families': ['V01','V03','V05','V06','V08'], 'limits': 'Only the published formula subset is supported; no proprietary formula parity claim.'},
    '4C': {'name': 'Migration and export', 'families': ['V01','V02','V03','V06','V08'], 'limits': 'Notion live-account capture and complete database/schema/view API reconstruction are not qualified. PDF is a readable rendition.'},
    '4D': {'name': 'Capture and external indexes', 'families': ['V02','V03','V06','V08','V10'], 'limits': 'Browser paste/share and approved-root indexing are local. Native screenshot permission/UI and AST parity remain unqualified.'},
    '4E': {'name': 'Local automation', 'families': ['V01','V06','V07','V08','V10'], 'limits': 'Runs while Trove is open. UI exposes a subset of service actions. No remote side effects.'},
    '4F': {'name': 'Expanded local AI', 'families': ['V03','V06','V08','V09','V10'], 'limits': 'Draft validation and evidence boundaries are qualified by fixtures; general output quality for every P1 mode is not established.'},
    '4G': {'name': 'Optional provider adapters', 'families': ['V03','V06','V09','V10'], 'limits': 'Paired loopback MCP only; managed Claude Code/Codex and remote MCP remain disabled.'},
}


def evaluate_families(tests, checks):
    """Require exact successful identities, not substring/log/pass-count guesses."""
    states = {}
    for test in tests:
        states.setdefault(test['test'], []).append(test['result'])
    check_states = {item['name']: item['result'] for item in checks}
    output = []
    for identity, (name, required_tests, required_checks) in FAMILIES.items():
        selected = [WORKSPACE + test for test in required_tests]
        missing = [test for test in selected if not states.get(test) or any(state != 'passed' for state in states[test])]
        failed_checks = [check for check in required_checks if check_states.get(check) != 'passed']
        output.append({'id': identity, 'name': name, 'result': 'passed' if not missing and not failed_checks else 'failed',
                       'tests': selected, 'checks': required_checks, 'missingOrUnsuccessfulTests': missing,
                       'missingOrUnsuccessfulChecks': failed_checks, 'scope': 'automated supported-contract qualification'})
    return output
