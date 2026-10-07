#!/usr/bin/env python3
"""Observe an explicitly operated packaged macOS app; never infer release approval.

Launches a private clone of a closed qualification fixture under network denial.
Operate the visible app, then quit normally before the deadline. No UI actions,
accounts or recording permission are activated by this script.
"""
import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import shutil
import statistics
import stat
import subprocess
import sys
import tempfile
import time

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


def private_json(path, value):
    with open(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as output:
        json.dump(value, output, indent=2, allow_nan=False)
        output.write('\n')


def inventory(root):
    """Bind an input without emitting its filenames, paths or content."""
    if root.is_symlink() or not root.is_dir():
        raise ValueError('input must be an existing unlinked directory')
    h = hashlib.sha256()
    count = size = 0
    for path in sorted(root.rglob('*')):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not (stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode)):
            raise ValueError('input contains a link or special object')
        name = path.relative_to(root).as_posix().encode()
        h.update(len(name).to_bytes(8, 'big') + name + (b'd' if path.is_dir() else b'f'))
        if path.is_file():
            file_hash = hashlib.sha256()
            with path.open('rb') as source:
                while chunk := source.read(1024 * 1024):
                    file_hash.update(chunk)
            h.update(info.st_size.to_bytes(8, 'big') + file_hash.digest())
            count += 1
            size += info.st_size
    return {'sha256': h.hexdigest(), 'files': count, 'bytes': size}


def clone_closed_workspace(source, destination):
    """Hold the existing product owner lock while copying a closed fixture."""
    fd = os.open(source / '.owner.lock', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as lock:
        info = os.fstat(lock.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid():
            raise ValueError('fixture owner lock is unsafe')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        before = inventory(source)
        shutil.copytree(source, destination)
        after = inventory(source)
        if before != after or inventory(destination) != before:
            raise ValueError('fixture changed during copy')
        return before


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
    scratch = Path(tempfile.mkdtemp(prefix='trove-native-performance-', dir='/private/tmp')).resolve()
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
           'TROVE_STARTUP_TRACE': str(output / 'startup.trace')}
    private_json(output / 'started.json', {'schemaVersion': 1, 'recordedAt': datetime.now(timezone.utc).isoformat(),
                 'scenario': args.scenario, 'minimumSamplesPerRequiredMetric': args.minimum_samples, 'appInventory': before, 'modelsInventory': model_before,
                 'sourceFixtureInventory': fixture, 'networkDeniedByOS': True,
                 'scratchDirectory': str(scratch), 'releaseQualified': False})
    print('Operate the visible disposable app for the selected scenario, then quit normally before the deadline.', flush=True)
    print('Private scratch retained at ' + str(scratch), flush=True)
    samples, timed_out, interrupted = [], False, False
    started = time.monotonic()
    process = subprocess.Popen(wrapper + [str(binary)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
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
    metrics = summarize(trace) if trace else {}
    missing = sorted(label for label in SCENARIOS[args.scenario] if metrics.get(label, {}).get('samples', 0) < args.minimum_samples)
    available = [row for row in samples if row['available']]
    workers_seen = any(row['workerCount'] > 0 for row in available)
    unchanged = inventory(app) == before and inventory(models) == model_before
    capped = any(entry['dropped'] > 0 for entry in metrics.values())
    memory_failed = any(not row['available'] and not row.get('processExitedDuringSample') for row in samples)
    complete = (process.returncode == 0 and not timed_out and not interrupted and trace is not None and unchanged
                and bool(available) and not memory_failed and not missing and not capped
                and (args.scenario != 'concurrent' or workers_seen))
    result = {'schemaVersion': 1, 'scenario': args.scenario, 'recordedAt': datetime.now(timezone.utc).isoformat(),
              'processExitCode': process.returncode, 'timedOut': timed_out, 'interrupted': interrupted,
              'appAndModelsUnchanged': unchanged, 'traceError': trace_error, 'metrics': metrics,
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
                  'Private cloned workspace is retained for independent canonical inspection. No account credentials are inherited and all app networking is denied.',
              ]}
    private_json(output / 'result.json', result)
    print(json.dumps({'localObservationRecorded': complete, 'belowMinimumScenarioMetrics': missing, 'traceError': trace_error, 'sampleAccountingComplete': False, 'releaseQualified': False}))
    return 0 if complete else 1


if __name__ == '__main__':
    raise SystemExit(main())
