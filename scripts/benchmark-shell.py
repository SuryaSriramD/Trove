#!/usr/bin/env python3
"""Measure an actual packaged Wails shell on disposable cloned workspaces."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import statistics
import subprocess
import time

from qualification_fixtures import clone_closed_workspace, private_directory, retain_closed_workspace, sync_directory, sync_tree
from native_fixture_environment import OS_GLOBAL_LIMITATION, native_fixture_environment, validate_native_fixture_environment

ROOT = Path(__file__).resolve().parents[1]


def capture_stalled_process(process, path, log):
    """Sample only our disposable app, before terminating it; never block cleanup."""
    sampler = Path('/usr/bin/sample')
    if not sampler.is_file():
        return
    try:
        subprocess.run([str(sampler), str(process.pid), '2', '-file', str(path)],
                       stdout=log, stderr=subprocess.STDOUT, timeout=8, check=False)
    except (OSError, subprocess.TimeoutExpired):
        pass


def read_sample(path):
    try:
        sample = json.loads(path.read_text())
        value = sample['usableShellMs']
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
            raise ValueError('usableShellMs must be a finite non-negative number')
        return sample, None
    except (OSError, ValueError, KeyError, TypeError) as error:
        return None, 'Invalid or missing shell report: ' + str(error)


def measure(app, profiles, iterations, fixture, diagnostics, *, timeout=60, diagnostic_slow_ms=None):
    if iterations < 1 or timeout <= 0:
        raise ValueError('Iterations and timeout must be positive')
    if diagnostic_slow_ms is not None and (not math.isfinite(diagnostic_slow_ms) or not 0 < diagnostic_slow_ms < timeout * 1000):
        raise ValueError('Diagnostic trigger must be positive and precede the process deadline')
    if not profiles or len({p['pages'] for p in profiles}) != len(profiles):
        raise ValueError('Workspace profiles must be nonempty and distinct')
    binary = app.resolve() / 'Contents/MacOS/Trove'
    before = hashlib.sha256(binary.read_bytes()).hexdigest()
    results, failures = [], []
    diagnostics.mkdir(parents=True, exist_ok=True)
    # Native traces use exclusive creation. Refuse a reused evidence set before
    # launching anything, otherwise a prior trace could be attributed to this app.
    for profile in profiles:
        for i in range(iterations):
            stem = f"{profile['pages']}-{i}"
            paths = [fixture / (stem + '.json')]
            paths += [diagnostics / (stem + suffix) for suffix in ('.log', '.trace', '.sample')]
            if any(os.path.lexists(path) for path in paths):
                raise ValueError('Startup evidence already exists; choose a fresh output directory')
    for profile in profiles:
        n = profile['pages']
        workspace = fixture / f'pages-{n}'
        source_identity = None
        if profile['workspacePath']:
            source_identity = clone_closed_workspace(Path(profile['workspacePath']), workspace)
        env, isolation = native_fixture_environment(fixture / f'environment-{n}', workspace)
        samples = []
        attempted = 0
        for i in range(iterations):
            attempted += 1
            report = fixture / f'{n}-{i}.json'
            # Keep failure evidence beside the report, not only in a temporary
            # directory which may vanish when the development host restarts.
            log_path = diagnostics / f'{n}-{i}.log'
            trace_path = diagnostics / f'{n}-{i}.trace'
            native_sample = diagnostics / f'{n}-{i}.sample'
            validate_native_fixture_environment(env, isolation)
            env.update(TROVE_SHELL_REPORT=str(report), TROVE_SHELL_QUIT='1',
                       TROVE_STARTUP_TRACE=str(trace_path))
            started = time.monotonic()
            timed_out = False
            sampling_attempted = False
            with log_path.open('x') as log:
                process = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)', str(binary)],
                                           env=env, cwd=isolation['temporary'], stdout=log, stderr=subprocess.STDOUT)
                try:
                    if diagnostic_slow_ms is not None:
                        try:
                            process.wait(timeout=diagnostic_slow_ms / 1000)
                        except subprocess.TimeoutExpired:
                            # Parent exec time is a trigger, not the Go-shell metric.
                            # Sampling may perturb this launch and its shutdown.
                            sampling_attempted = True
                            capture_stalled_process(process, native_sample, log)
                            process.wait(timeout=max(0, timeout - (time.monotonic() - started)))
                    else:
                        process.wait(timeout=timeout)
                except subprocess.TimeoutExpired:
                    timed_out = True
                    if not sampling_attempted:
                        sampling_attempted = True
                        capture_stalled_process(process, native_sample, log)
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
            outer = (time.monotonic() - started) * 1000
            sample, invalid = read_sample(report)
            if timed_out or process.returncode != 0 or invalid:
                reason = 'App exceeded the startup/shutdown deadline' if timed_out else (
                    'App exited unsuccessfully' if process.returncode != 0 else invalid)
                failure = {'pages': n, 'iteration': i, 'exitCode': process.returncode,
                           'timedOut': timed_out, 'launchThroughShutdownMs': outer,
                           'reason': reason, 'log': str(log_path),
                           'diagnosticSamplingAttempted': sampling_attempted}
                if trace_path.exists():
                    failure['startupTrace'] = str(trace_path)
                if native_sample.exists():
                    failure['processSample'] = str(native_sample)
                # A readiness report followed by a shutdown timeout is still a
                # failed launch. Preserve the report without counting it as pass.
                if sample:
                    failure['shellReport'] = sample
                failures.append(failure)
                break
            sample['launchThroughShutdownMs'] = outer
            sample['diagnosticSamplingAttempted'] = sampling_attempted
            if native_sample.exists():
                sample['processSample'] = str(native_sample)
            samples.append(sample)
        values = [s['usableShellMs'] for s in samples]
        samples_complete = len(samples) == iterations
        retained = None
        try:
            retained = retain_closed_workspace(workspace)
        except (OSError, ValueError) as error:
            failures.append({'pages': n, 'reason': 'Workspace retention failed: ' + str(error)})
        complete = samples_complete and retained is not None
        results.append({'pages': n, 'paragraphBlocks': profile['paragraphBlocks'], 'samples': samples,
                        'sourceFixtureInventory': source_identity, 'retainedWorkspace': retained, 'isolation': isolation,
                        'requestedIterations': iterations, 'attemptedIterations': attempted,
                        'completedIterations': len(samples), 'sampleCollectionComplete': samples_complete, 'complete': complete,
                        'medianMs': statistics.median(values) if values else None,
                        'maximumMs': max(values) if values else None,
                        'approximatelyTwoSeconds': complete and max(values) <= 2500})
        print(f'pages={n}: completed={len(samples)}/{iterations}, maximum shell ms={max(values) if values else "FAILED"}', flush=True)
    after = hashlib.sha256(binary.read_bytes()).hexdigest()
    measurement_passed = bool(results) and not failures and before == after
    observed_target = measurement_passed and all(p['approximatelyTwoSeconds'] for p in results)
    return {'recordedAt': datetime.now(timezone.utc).isoformat(), 'app': str(app),
            'appBinarySha256': after, 'appUnchanged': before == after,
            'networkDeniedByOS': True, 'modelPacks': [], 'fixture': str(fixture),
            'profiles': results, 'failures': failures,
            'measurementPassed': measurement_passed,
            'observedShellThresholdPassed': observed_target,
            'shellTargetPassed': observed_target and diagnostic_slow_ms is None,
            'instrumented': diagnostic_slow_ms is not None,
            'diagnosticTriggerMs': diagnostic_slow_ms,
            'diagnosticTriggerMetric': 'parent executable launch through shutdown; not Go-shell elapsed time',
            'releaseQualified': False,
            'definition': 'Go process initialization to canonical workspace snapshot rendered and two animation frames. Startup includes native validation and Wails initialization. Each sample uses a new app process; OS disk caches are not flushed. The approximate two-second threshold is explicitly interpreted as at most 2.5 seconds, requires every requested iteration to finish, and exact values are retained.',
            'limitations': [OS_GLOBAL_LIMITATION, 'Developer Mac only; hardware and synthetic workspace definitions are in benchmark-environment.json.',
                           'Diagnostic mode is ineligible for performance promotion, even if no sampling was needed; sampling may perturb timing.',
                           'Does not measure installer/Gatekeeper time or guarantee editor responsiveness during background work.',
                           'A missing readiness report is a failed measurement; it does not by itself distinguish a workspace, bridge, renderer, or visibility problem.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', type=Path, required=True)
    parser.add_argument('--profiles', type=Path, default=ROOT / 'evidence/phase3/benchmark-service.json')
    parser.add_argument('--iterations', type=int, default=5)
    parser.add_argument('--diagnostic-slow-ms', type=float, help='Diagnostic only: sample a still-running owned app after this parent elapsed time; never qualifies timing')
    parser.add_argument('--output', type=Path, default=ROOT / 'evidence/phase3/shell-benchmark.json')
    args = parser.parse_args()
    if args.iterations < 1:
        parser.error('--iterations must be positive')
    if os.path.lexists(args.output):
        parser.error('Output already exists; choose a fresh candidate evidence path')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    fixture = private_directory(args.output.parent / (args.output.stem + '-fixtures'))
    diagnostics = private_directory(args.output.parent / (args.output.stem + '-diagnostics'))
    profiles = [{'pages': 0, 'paragraphBlocks': 0, 'workspacePath': None}]
    profiles += json.loads(args.profiles.read_text())['profiles']
    result = measure(args.app, profiles, args.iterations, fixture, diagnostics,
                     diagnostic_slow_ms=args.diagnostic_slow_ms)
    sync_tree(fixture)
    sync_tree(diagnostics)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open('x') as output:
        output.write(json.dumps(result, indent=2) + '\n')
        output.flush()
        os.fsync(output.fileno())
    sync_directory(args.output.parent)
    if args.diagnostic_slow_ms is not None:
        return 0 if result['measurementPassed'] else 1
    return 0 if result['shellTargetPassed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
