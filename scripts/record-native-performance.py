#!/usr/bin/env python3
"""Observe an explicitly operated packaged macOS app; never infer release approval.

Launches a private clone of a closed qualification fixture under network denial.
Operate the visible app, then quit normally before the deadline. No UI actions,
accounts or recording permission are activated by this script.
"""
import argparse
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import platform
import statistics
import stat
import subprocess
import sys
import time

from qualification_fixtures import clone_closed_workspace, inventory, private_directory, retain_closed_workspace, sync_directory

METRICS = {
    'database_dispatch', 'snapshot_dispatch', 'mutation_dispatch', 'search_dispatch',
    'database_bridge', 'snapshot_bridge', 'mutation_bridge', 'search_bridge',
    'database_load_to_frames', 'database_response_to_frames', 'input_to_frames', 'scroll_to_frames',
}
SCENARIOS = {
    'idle': {'snapshot_dispatch'},
    'table': {'database_dispatch', 'database_bridge', 'database_load_to_frames', 'database_response_to_frames', 'scroll_to_frames'},
    'edit': {'mutation_dispatch', 'mutation_bridge', 'input_to_frames'},
    'concurrent': {'mutation_dispatch', 'mutation_bridge', 'input_to_frames'},
}
WORKERS = {'llama-worker', 'whisper-worker', 'microphone-worker', 'audio-decoder',
           'document-extractor', 'pdf-renderer', 'vision-worker', 'ocr-worker'}
DIAGNOSTIC_COUNTERS = {
    'probes_requested', 'probes_scheduled', 'probes_completed', 'probes_cancelled',
    'probes_hidden', 'probes_limited', 'probes_missing_start', 'records_attempted',
    'records_accepted', 'records_rejected', 'records_failed', 'records_capped',
    'input_trusted', 'input_ignored', 'scroll_trusted', 'scroll_ignored',
}


def unique_json_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate_json_key')
        result[key] = value
    return result


class RendererDiagnostics:
    """Incremental bounded sidecar reader; partial writes are never readiness."""
    def __init__(self, path):
        self.path, self.offset, self.partial = path, 0, b''
        self.identity, self.last = None, None

    def poll(self, final=False):
        try:
            info = self.path.lstat()
        except FileNotFoundError:
            if final:
                raise ValueError('missing_renderer_diagnostics')
            return None
        identity = (info.st_dev, info.st_ino)
        if (not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_nlink != 1
                or info.st_uid != os.getuid() or info.st_size < self.offset or info.st_size > 4097 * 4097
                or (self.identity is not None and identity != self.identity)):
            raise ValueError('unsafe_renderer_diagnostics')
        self.identity = identity
        with self.path.open('rb') as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino) != identity:
                raise ValueError('changed_renderer_diagnostics')
            stream.seek(self.offset)
            new = stream.read(4097 * 4097 + 1)
            self.offset += len(new)
        lines = (self.partial + new).split(b'\n')
        self.partial = lines.pop()
        if len(self.partial) > 4096:
            raise ValueError('oversized_renderer_diagnostics')
        for line in lines:
            if len(line) > 4096:
                raise ValueError('oversized_renderer_diagnostics')
            value = json.loads(line, object_pairs_hook=unique_json_pairs)
            required = {'schemaVersion', 'sequence', 'finalized', 'capped', 'ready', 'visible',
                        'focused', 'stopped', 'pendingFrames', 'pendingTransports', 'counters'}
            if (not isinstance(value, dict) or set(value) != required
                    or type(value['schemaVersion']) is not int or value['schemaVersion'] != 1
                    or any(type(value[key]) is not bool for key in ('finalized', 'capped', 'ready', 'visible', 'focused', 'stopped'))
                    or type(value['sequence']) is not int or value['sequence'] != (self.last['sequence'] + 1 if self.last else 1)
                    or value['sequence'] > 4097 or (self.last and self.last['finalized'])
                    or type(value['pendingFrames']) is not int or not 0 <= value['pendingFrames'] <= 16
                    or type(value['pendingTransports']) is not int or not 0 <= value['pendingTransports'] <= 4104
                    or not isinstance(value['counters'], dict) or set(value['counters']) != DIAGNOSTIC_COUNTERS
                    or any(type(n) is not int or not 0 <= n <= 65535 for n in value['counters'].values())
                    or (self.last and any(value['counters'][key] < self.last['counters'][key] for key in DIAGNOSTIC_COUNTERS))):
                raise ValueError('invalid_renderer_diagnostics')
            self.last = value
        if final and (self.partial or self.last is None or not self.last['finalized']):
            raise ValueError('unfinished_renderer_diagnostics')
        return self.last


def renderer_ready(value):
    return bool(value and not value['finalized'] and not value['capped'] and value['ready']
                and value['visible'] and value['focused'] and not value['stopped'])


def owned_executable_matches(pid, binary):
    """Only inspect the unreaped driver's child; never attach to another Trove."""
    try:
        result = subprocess.run(['/bin/ps', '-p', str(pid), '-o', 'comm='], capture_output=True, text=True, timeout=2)
        return result.returncode == 0 and result.stdout.strip() == str(binary)
    except (OSError, subprocess.TimeoutExpired):
        return False


def collection_complete(*, process_exit, timed_out, interrupted, trace_valid, unchanged,
                        memory_available, memory_failed, missing, capped, retained,
                        worker_required, worker_seen, readiness, diagnostics_valid):
    return (process_exit == 0 and not timed_out and not interrupted and trace_valid and unchanged
            and memory_available and not memory_failed and not missing and not capped and retained
            and (not worker_required or worker_seen) and readiness and diagnostics_valid)


def private_json(path, value):
    with open(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as output:
        json.dump(value, output, indent=2, allow_nan=False)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    sync_directory(path.parent)


def owned_memory(text, root_pid):
    """Process names/PIDs are transient ownership inputs, never output fields."""
    if len(text) > 4 * 1024 * 1024:
        raise ValueError('process_snapshot_too_large')
    rows = {}
    for line in text.splitlines():
        fields = line.split(None, 3)
        if len(fields) != 4:
            raise ValueError('invalid_process_snapshot')
        pid, parent, kib = (int(value) for value in fields[:3])
        if pid <= 0 or parent < 0 or kib < 0 or pid in rows:
            raise ValueError('invalid_process_snapshot')
        rows[pid] = (parent, kib * 1024, Path(fields[3]).name)
    if root_pid not in rows or rows[root_pid][1] <= 0 or rows[root_pid][2] != 'Trove':
        raise ValueError('owned_process_unavailable')
    owned = {root_pid}
    for _ in range(512):
        added = {pid for pid, (parent, _, _) in rows.items() if parent in owned and pid not in owned}
        if not added:
            break
        owned.update(added)
        if len(owned) > 512:
            raise ValueError('owned_process_limit')
    worker = [rows[pid][1] for pid in owned - {root_pid} if rows[pid][2] in WORKERS]
    other = [rows[pid][1] for pid in owned - {root_pid} if rows[pid][2] not in WORKERS]
    return {'available': True, 'appRssBytes': rows[root_pid][1], 'workerRssBytes': sum(worker),
            'otherDescendantRssBytes': sum(other), 'workerCount': len(worker), 'otherDescendantCount': len(other)}


def sample_memory(pid):
    try:
        result = subprocess.run(['/bin/ps', '-axo', 'pid=,ppid=,rss=,comm='], capture_output=True, text=True, timeout=2)
        if result.returncode:
            return {'available': False, 'reason': 'process_sampler_failed'}
        return owned_memory(result.stdout, pid)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        return {'available': False, 'reason': 'process_sample_unavailable'}


def read_trace(path):
    if path.is_symlink() or path.stat().st_size > 512 * 1024:
        raise ValueError('invalid_trace_file')
    def unique_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate_trace_key')
            result[key] = value
        return result
    trace = json.loads(path.read_text(), object_pairs_hook=unique_pairs)
    if not isinstance(trace, dict) or set(trace) != {'schemaVersion', 'finalized', 'sampleLimitPerMetric', 'metrics'} or type(trace['schemaVersion']) is not int or trace['schemaVersion'] != 1 or trace['finalized'] is not True or type(trace['sampleLimitPerMetric']) is not int or trace['sampleLimitPerMetric'] != 512 or not isinstance(trace['metrics'], dict):
        raise ValueError('invalid_trace_schema')
    for label, values in trace['metrics'].items():
        if label not in METRICS or not isinstance(values, dict) or set(values) != {'milliseconds', 'dropped'} or type(values['dropped']) is not int or values['dropped'] < 0 or not isinstance(values['milliseconds'], list) or len(values['milliseconds']) > 512:
            raise ValueError('invalid_trace_metric')
        if any(type(n) not in (int, float) or not math.isfinite(n) or not 0 <= n <= 3600000 for n in values['milliseconds']):
            raise ValueError('invalid_trace_duration')
    return trace


def summarize(trace):
    output = {}
    for name, entry in trace['metrics'].items():
        values = sorted(entry['milliseconds'])
        output[name] = {'samples': len(values), 'dropped': entry['dropped'],
                        'medianMs': statistics.median(values) if values else None,
                        'p95Ms': values[math.ceil(len(values) * .95) - 1] if values else None,
                        'maximumMs': values[-1] if values else None}
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app', required=True, type=Path)
    parser.add_argument('--workspace', type=Path, help='Closed disposable/authorized fixture to clone; omit for a new empty workspace')
    parser.add_argument('--models', type=Path, help='Already-installed selected local packs; never acquired by the driver')
    parser.add_argument('--scenario', choices=SCENARIOS, required=True)
    parser.add_argument('--duration-seconds', type=int, default=600, help='Quit normally before this deadline (30..1800 seconds)')
    parser.add_argument('--minimum-samples', type=int, default=5, help='Predeclared minimum per required scenario metric (1..100); does not establish complete event accounting')
    parser.add_argument('--output', required=True, type=Path, help='New private evidence directory; never reused')
    args = parser.parse_args()
    if platform.system() != 'Darwin' or not 30 <= args.duration_seconds <= 1800 or not 1 <= args.minimum_samples <= 100:
        parser.error('requires native macOS, a 30..1800 second deadline and 1..100 minimum samples')
    app = args.app.resolve(strict=True)
    binary = app / 'Contents/MacOS/Trove'
    if not binary.is_file():
        parser.error('packaged executable missing')
    output = args.output.absolute()
    output.parent.resolve(strict=True)
    # Resolve only the parent; preserve a dangling/existing final object refusal.
    output = output.parent.resolve() / output.name
    output.mkdir(mode=0o700, exist_ok=False)
    if output.stat().st_mode & 0o077:
        raise ValueError('evidence_directory_not_private')
    # Keep the actual observed bytes with the immutable evidence, rather than
    # relying on a system temporary directory surviving sleep or cleanup.
    scratch = private_directory(output / 'runtime')
    home, temporary, workspace = scratch / 'home', scratch / 'temporary', scratch / 'workspace'
    home.mkdir(mode=0o700); temporary.mkdir(mode=0o700)
    if args.workspace and args.workspace.is_symlink():
        raise ValueError('fixture_root_is_linked')
    fixture = clone_closed_workspace(args.workspace.resolve(strict=True), workspace) if args.workspace else None
    empty_models = scratch / 'empty-models'
    empty_models.mkdir(mode=0o700)
    models = args.models.resolve(strict=True) if args.models else empty_models
    before, model_before = inventory(app), inventory(models)
    wrapper = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)']
    probe = subprocess.run(wrapper + [sys.executable, '-c', 'import socket; socket.socket().connect(("127.0.0.1", 9))'], capture_output=True, text=True)
    if probe.returncode == 0 or 'Operation not permitted' not in probe.stderr or 'sandbox_apply' in probe.stderr:
        raise ValueError('network_denial_not_established')
    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'HOME': str(home), 'TMPDIR': str(temporary),
           'LANG': 'en_US.UTF-8', 'TROVE_WORKSPACE': str(workspace), 'TROVE_MODEL_ROOT': str(models),
           'TROVE_PERFORMANCE_REPORT': str(output / 'timings.json'),
           'TROVE_PERFORMANCE_DIAGNOSTICS': str(output / 'renderer-diagnostics.jsonl'),
           'TROVE_STARTUP_TRACE': str(output / 'startup.trace')}
    private_json(output / 'started.json', {'schemaVersion': 1, 'recordedAt': datetime.now(timezone.utc).isoformat(),
                 'scenario': args.scenario, 'minimumSamplesPerRequiredMetric': args.minimum_samples, 'appInventory': before, 'modelsInventory': model_before,
                 'sourceFixtureInventory': fixture, 'networkDeniedByOS': True,
                 'scratchDirectory': str(scratch), 'releaseQualified': False})
    print('Operate the visible disposable app for the selected scenario, then quit normally before the deadline.', flush=True)
    print('Private scratch retained at ' + str(scratch), flush=True)
    samples, timed_out, interrupted = [], False, False
    diagnostics = RendererDiagnostics(output / 'renderer-diagnostics.jsonl')
    diagnostics_error, readiness = None, None
    started = time.monotonic()
    process = subprocess.Popen(wrapper + [str(binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        private_json(output / 'process.json', {'schemaVersion': 1, 'ownedProcessId': process.pid,
                     'executable': str(binary), 'launchKind': 'driver-owned-child',
                     'rule': 'Do not select or reacquire the app after this owned process exits; reopen only through a new explicit driver invocation.'})
        while process.poll() is None:
            if time.monotonic() - started >= args.duration_seconds:
                timed_out = True
                break
            # NewApp reserves this sink only after sandbox-exec has replaced
            # itself with Trove. Exclude the launcher's earlier memory. poll()
            # is the only reap of our child; an exit during ps remains a zombie
            # until the next poll, preventing root-PID reuse during a sample.
            if not (output / 'timings.json').is_file():
                time.sleep(.05)
                continue
            if diagnostics_error is None:
                try:
                    previous_sequence = diagnostics.last['sequence'] if diagnostics.last else 0
                    state = diagnostics.poll()
                    if (readiness is None and renderer_ready(state) and state['sequence'] > previous_sequence
                            and process.poll() is None and owned_executable_matches(process.pid, binary)
                            and process.poll() is None):
                        readiness = {'schemaVersion': 1, 'elapsedMs': (time.monotonic() - started) * 1000,
                                     'rendererSequence': state['sequence'], 'ownedExecutableVerified': True,
                                     'ready': True, 'visible': True, 'focused': True,
                                     'actualForegroundWindowIndependentlyObserved': False,
                                     'limitation': 'Renderer and owned-process readiness only. Independently observe the actual candidate window before gestures; this is not a paint or input-device proof.'}
                        private_json(output / 'readiness.json', readiness)
                        print('Owned renderer is ready, visible and focused. Independently verify its actual window before qualification gestures.', flush=True)
                except (OSError, ValueError, TypeError, UnicodeError):
                    diagnostics_error = 'invalid_renderer_diagnostics'
            observed = time.monotonic()
            sample = sample_memory(process.pid)
            sample['elapsedMs'] = (observed - started) * 1000
            sample['acquisitionMs'] = (time.monotonic() - observed) * 1000
            if not sample['available'] and process.poll() is not None:
                sample['processExitedDuringSample'] = True
            samples.append(sample)
            time.sleep(max(0, .25 - (time.monotonic() - observed)))
    except KeyboardInterrupt:
        interrupted = True
    finally:
        if process.poll() is None:
            process.terminate()
            try: process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait(timeout=5)
    trace, trace_error = None, None
    try: trace = read_trace(output / 'timings.json')
    except (OSError, ValueError, TypeError): trace_error = 'missing_or_invalid_finalized_trace'
    if diagnostics_error is None:
        try: diagnostics.poll(final=True)
        except (OSError, ValueError, TypeError, UnicodeError): diagnostics_error = 'missing_or_invalid_finalized_renderer_diagnostics'
    diagnostics_valid = diagnostics_error is None and diagnostics.last is not None and not diagnostics.last['capped']
    metrics = summarize(trace) if trace else {}
    missing = sorted(label for label in SCENARIOS[args.scenario] if metrics.get(label, {}).get('samples', 0) < args.minimum_samples)
    available = [row for row in samples if row['available']]
    workers_seen = any(row['workerCount'] > 0 for row in available)
    unchanged = inventory(app) == before and inventory(models) == model_before
    retained, retention_error = None, None
    try:
        retained = retain_closed_workspace(workspace)
    except (OSError, ValueError) as error:
        retention_error = str(error)
    capped = any(entry['dropped'] > 0 for entry in metrics.values())
    memory_failed = any(not row['available'] and not row.get('processExitedDuringSample') for row in samples)
    complete = collection_complete(process_exit=process.returncode, timed_out=timed_out, interrupted=interrupted,
                trace_valid=trace is not None, unchanged=unchanged, memory_available=bool(available),
                memory_failed=memory_failed, missing=missing, capped=capped, retained=retained is not None,
                worker_required=args.scenario == 'concurrent', worker_seen=workers_seen,
                readiness=readiness is not None, diagnostics_valid=diagnostics_valid)
    result = {'schemaVersion': 1, 'scenario': args.scenario, 'recordedAt': datetime.now(timezone.utc).isoformat(),
              'processExitCode': process.returncode, 'timedOut': timed_out, 'interrupted': interrupted,
              'appAndModelsUnchanged': unchanged, 'traceError': trace_error, 'metrics': metrics,
              'rendererReadinessObserved': readiness is not None, 'rendererDiagnosticsError': diagnostics_error,
              'rendererDiagnosticsFinal': diagnostics.last,
              'actualForegroundWindowIndependentlyObserved': False,
              'retainedWorkspace': retained, 'retentionError': retention_error,
              'belowMinimumScenarioMetrics': missing, 'minimumSamplesPerRequiredMetric': args.minimum_samples,
              'observationMinimaMet': not missing, 'sampleCoverageCapped': capped, 'workerObserved': workers_seen,
              'memory': {'requestedIntervalMs': 250, 'samples': samples,
                         'sampledMaximumAppRssBytes': max((s['appRssBytes'] for s in available), default=None),
                         'sampledMaximumWorkerRssBytes': max((s['workerRssBytes'] for s in available), default=None),
                         'sampledMaximumOtherDescendantRssBytes': max((s['otherDescendantRssBytes'] for s in available), default=None)},
              'localObservationRecorded': complete, 'collectorFinalized': trace is not None,
              'sampleAccountingComplete': False, 'releaseQualified': False,
              'limitations': [
                  'Instrumented local observation. No latency SLA or product/criterion acceptance is inferred.',
                  'Dispatch includes desktop admission/locking/serialization; bridge includes serialization and transport. Independent aggregate samples are not paired for subtraction.',
                  'Two animation-frame callbacks are scheduling/render opportunities, not confirmed screen paint or input-device latency.',
                  'Frame probes are opportunistic: hidden documents, cancelled/stale renders and more than 16 pending probes omit events without complete event accounting. The first 512 samples per metric are retained; capped coverage is incomplete.',
                  'Renderer metric calls are asynchronous and may be in flight when the app quits. Finalized means the backend collector closed only. Minimum counts establish recorded observations, never complete workflow or input-event coverage.',
                  'App RSS and currently owned descendants are sampled; shared pages may be double-counted. Unowned WebKit/GPU/driver processes and unsampled worker peaks are not included. This is not whole-application memory qualification.',
                  'Memory collection begins after the native trace sink is reserved. Root process identity is checked each sample; descendant ownership is rebuilt from the current process snapshot, capped at 512 processes. An unavailable terminal sample after the owned process exited is retained but is not an active-process sampling failure.',
                  'Worker presence does not prove simultaneous recording/indexing/AI, useful results, correctness, cancellation or physical-device behavior. Record independent native observations.',
                  'Normal UI quit is required. Forced deadline/interrupt and missing memory/timing data cannot complete collection.',
                  'A live owned-executable and ready/visible/focused renderer witness is required. It does not prove the actual frontmost window: retain a separate native observation. Do not reacquire the app after exit, which can launch an uncontrolled default workspace.',
                  'Content-free renderer counters diagnose omitted probes and asynchronous transport acknowledgements. They do not provide complete event accounting, confirmed paint, or canonical persistence. A finalized sidecar closes only its collector.',
                  'Private cloned workspace is retained for independent canonical inspection. No account credentials are inherited and all app networking is denied.',
              ]}
    private_json(output / 'result.json', result)
    print(json.dumps({'localObservationRecorded': complete, 'belowMinimumScenarioMetrics': missing, 'traceError': trace_error, 'sampleAccountingComplete': False, 'releaseQualified': False}))
    return 0 if complete else 1


if __name__ == '__main__':
    raise SystemExit(main())
