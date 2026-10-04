#!/usr/bin/env python3
"""Qualify an immutable P1 candidate plus affected P0 workflows by TRD V01–V10.

No skip/fast option may produce qualification. Uses only installed dependencies
and model packs; optional live accounts are never activated by this runner.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys
import traceback

from phase5_policy import evaluate_families, PACKAGES, MODULE, INTEGRATION, WORKSPACE, OFFLINE_TESTS

spec = importlib.util.spec_from_file_location('phase3_qualification', Path(__file__).with_name('verify-phase3.py'))
p0 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p0)


class Qualification(p0.Qualification):
    EVIDENCE_PHASE = 'phase5'
    BUILD_OPTIONS = ['--phase5']

    def execute(self):
        super().execute()
        # Never substitute an old or partially built app for this run's candidate.
        if not self.bundle_before or not any(c['name'] == 'os-network-denial-control' and c['result'] == 'passed' for c in self.checks):
            self.record('p1-candidate-ready', False, reason='P0 candidate build/offline prerequisites did not complete.')
            return
        self.command('p1-offline', p0.OFFLINE + [self.go, 'test', '-mod=readonly', '-race', '-count=1', '-json',
                     '-p', '1', '-timeout', '10m', '-tags', p0.TAGS, '-run',
                     '^Test(P1PublicServiceJourney|Phase5(Recovery|Provenance|Formula|Automation|Editor))',
                     './internal/integration', './internal/workspace'])
        self.parse_go('p1-offline', native=False)
        offline = [t for t in self.tests if t['phase'] == 'p1-offline']
        passed_offline = {t['test'] for t in offline if t['result'] == 'passed'}
        missing_offline = sorted(set(OFFLINE_TESTS) - passed_offline)
        self.record('p1-offline-required-tests', not missing_offline, requiredTests=OFFLINE_TESTS, missingRequiredTests=missing_offline)

        self.command('packaged-p1-restart-offline', [sys.executable, p0.ROOT / 'scripts/verify-packaged.py',
                     '--p1', '--offline', '--app', self.args.app, '--evidence', self.run_dir], timeout=1500)
        path = self.run_dir / 'packaged-p1-integration.json'
        if path.is_file():
            report = json.loads(path.read_text())
            steps = report.get('checks', [])
            self.record('packaged-p1-completeness', report.get('passed') is True and report.get('networkDeniedByOS') is True
                        and [s.get('step') for s in steps] == ['p1-create', 'p1-verify']
                        and all(s.get('passed') is True and s.get('installedWeightsAvailable') is False for s in steps),
                        report=self.path(path), reportSha256=p0.digest(path),
                        note='Two real desktop processes, empty model cache, minimal PATH, OS network denial.')
        else:
            self.record('packaged-p1-completeness', False, reason='No packaged P1 report was produced.')
        self.command('packaged-paired-relay', [self.go, 'test', '-mod=readonly', '-race', '-count=1', '-json',
                     '-timeout', '5m', '-tags', p0.TAGS, '-run', '^TestPhase5V10PairedRelayProtocolConsentAndCancellation$', './internal/workspace'],
                     env=dict(self.env, TROVE_MCP_QUALIFICATION_BINARY=str(self.args.app / 'Contents/MacOS/trove-mcp')))
        self.parse_go('packaged-paired-relay', native=False)
        relay_tests = [t for t in self.tests if t['test'].startswith(MODULE + '/cmd/trove-mcp.Test')]
        self.record('paired-relay-tests', bool(relay_tests) and all(t['result'] == 'passed' for t in relay_tests)
                    and any(t['phase'] == 'packaged-paired-relay' and t['test'] == WORKSPACE + 'TestPhase5V10PairedRelayProtocolConsentAndCancellation' and t['result'] == 'passed' for t in self.tests)
                    and (self.args.app / 'Contents/MacOS/trove-mcp').is_file(),
                    tests=[t['test'] for t in relay_tests],
                    note='Independent stdio subprocess and loopback protocol fixtures, not live vendor-account certification.')
        self.command('candidate-signature', ['/usr/bin/codesign', '--verify', '--deep', '--strict', self.args.app])
        self.record_families()

    def record_families(self):
        for family in evaluate_families(self.tests, self.checks):
            self.record('family-' + family['id'], family['result'] == 'passed', familyName=family['name'],
                        **{k:v for k,v in family.items() if k not in ('result', 'name')})

    def enrich_report(self, report):
        families = evaluate_families(self.tests, self.checks)
        report.update(scope='phase5-p1-qualification', releaseVersion='0.5.0',
                      verificationFamilies=families,
                      packages={key: {**package, 'automatedResult': 'passed' if all(next(f for f in families if f['id']==identity)['result']=='passed' for identity in package['families']) else 'failed'} for key,package in PACKAGES.items()},
                      optionalIntegrations=[
                          {'name':'Paired local MCP','availability':'opt-in','qualification':'local protocol, cancellation, scope and reviewed mutation fixtures; requires app running and renewed pairing after restart'},
                          {'name':'Notion API capture','availability':'explicit opt-in preview','qualification':'fixture-qualified page/block capture only; live account and full database API parity unverified'},
                          {'name':'Managed Claude Code','availability':'disabled','qualification':'authentication, entitlement, distribution and OS containment unqualified'},
                          {'name':'Managed Codex','availability':'disabled','qualification':'official authentication, entitlement, distribution and OS containment unqualified'},
                          {'name':'Remote MCP','availability':'disabled','qualification':'remote transport and authorization unqualified'},
                      ])
        report['releaseGates'][0]['gate'] = 'Complete P0 and P1 interactive journey on this exact packaged candidate'
        report['releaseGates'].append({'gate':'Full P1 scope and optional live integration qualification','status':'pending',
                                       'reason':'Package limits and optional integration states are explicit. Fixture results do not close missing product breadth or live account/device evidence.'})
        report['limitations'] = [
            'Automated success produces a personal-use release candidate; full P1 release approval remains withheld until open acceptance gates are resolved.',
            'Browser suites use service fixtures. Packaged journeys execute real services and native helpers, but do not substitute for interactive dialogs/clipboard/screenshot/audio observations.',
            'Native audio lifecycle uses synthetic PCM/public speech; general meeting and every new AI mode quality are not established.',
            'Managed providers and remote MCP stay disabled. No credentials, account sign-in or network fallback are introduced by qualification.',
            'Ad-hoc signed macOS arm64 candidate; separate clean-Mac installation and public notarization remain unverified.',
        ]
        return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--models', type=Path, default=Path('/private/tmp/trove-phase1-models'))
    parser.add_argument('--cache', type=Path, default=Path('/private/tmp/trove-phase0-tools'))
    parser.add_argument('--app', type=Path, default=p0.ROOT / 'build/phase5-candidate/Trove.app')
    parser.add_argument('--port', type=int, default=0)
    args = parser.parse_args()
    args.models, args.cache, args.app = args.models.resolve(), args.cache.resolve(), args.app.resolve()
    run = Qualification(args)
    try:
        run.execute()
    except KeyboardInterrupt:
        run.interrupted = True
        run.record('runner-interrupted', False, reason='No release evidence can be promoted from an interrupted run.')
    except Exception as exc:
        path = run.run_dir / 'runner-exception.log'
        path.write_text(traceback.format_exc())
        run.record('runner-exception', False, error=repr(exc), log=run.path(path), logSha256=p0.digest(path))
    return run.finish()


if __name__ == '__main__':
    raise SystemExit(main())
