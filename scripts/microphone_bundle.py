"""Validate the production capture sidecar before adding it to a native bundle."""
import json
from pathlib import Path
from devtools import ROOT, file_digest
from native_targets import TARGETS


def microphone_record(output, target):
    output = Path(output)
    sidecar = json.loads((output / 'microphone-worker.json').read_text())
    pin = json.loads((ROOT / 'native/microphone.lock.json').read_text())
    expected = {'name': 'microphone-worker', 'runtime': 'microphone',
                'runtimeCommit': pin['runtimeCommit'], 'architecture': TARGETS[target]['arch']}
    helper = sidecar.get('helper')
    if sidecar.get('schemaVersion') != 1 or sidecar.get('platform') != TARGETS[target]['os'] or sidecar.get('testOnly') is not False or sidecar.get('deviceBackendCompiled') is not True or sidecar.get('capabilities') != ['record', 'capture_controls_v1', 'input_devices_v1'] or not isinstance(helper, dict):
        raise RuntimeError('A matching production microphone sidecar is required.')
    if set(helper) != set(expected) | {'sha256'} or any(helper.get(k) != v for k, v in expected.items()):
        raise RuntimeError('Microphone runtime identity differs from the production pin.')
    filename = 'microphone-worker' + ('.exe' if target == 'windows-amd64' else '')
    binary = output / filename
    if binary.is_symlink() or not binary.is_file() or file_digest(binary) != helper['sha256']:
        raise RuntimeError('Microphone bytes differ from their sidecar.')
    if (output / 'microphone-fixture.json').exists():
        raise RuntimeError('A device-free fixture cannot enter a production bundle.')
    return helper


def install_microphone_record(output, target):
    output = Path(output)
    helper = microphone_record(output, target)
    path = output / 'manifest.json'
    manifest = json.loads(path.read_text())
    if manifest.get('schemaVersion') == 1:
        if target != 'darwin-arm64':
            raise RuntimeError('Legacy bundles are Mac-only.')
    elif manifest.get('schemaVersion') != 2 or manifest.get('platform') != TARGETS[target]['os'] or manifest.get('architecture') != TARGETS[target]['arch']:
        raise RuntimeError('Native bundle target differs from the capture target.')
    if not isinstance(manifest.get('helpers'), list) or sum(h.get('name') == 'microphone-worker' for h in manifest['helpers']) != 1:
        raise RuntimeError('Native bundle must contain exactly one microphone record.')
    manifest['helpers'] = [helper if h.get('name') == 'microphone-worker' else h for h in manifest['helpers']]
    manifest.update(schemaVersion=2, platform=TARGETS[target]['os'], architecture=TARGETS[target]['arch'])
    from codegen_output import publish
    publish({path: (json.dumps(manifest, indent=2) + chr(10)).encode()})
    return helper
