#!/usr/bin/env python3
"""Create a deterministic unpacked-extension ZIP and SHA256 receipt, locally."""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
FILES = ('README.md', 'manifest.json', 'popup.html', 'popup.css', 'popup.js', 'extract.js',
         'logo.png', 'icon-16.png', 'icon-32.png', 'icon-48.png', 'icon-128.png')
EXPECTED_ID = 'mmdbnaapeedpfjhafldjcacmanhjmjbb'


def extension_files(source):
    result = {}
    for name in FILES:
        file = source / name
        if not file.is_file() or file.is_symlink():
            raise ValueError('Missing or unsafe extension file: ' + name)
        result[name] = file.read_bytes()
    manifest = json.loads(result['manifest.json'])
    digest = hashlib.sha256(base64.b64decode(manifest['key'], validate=True)).digest()[:16].hex()
    identity = ''.join(chr(ord('a') + int(value, 16)) for value in digest)
    if manifest.get('manifest_version') != 3 or identity != EXPECTED_ID:
        raise ValueError('Unexpected extension manifest version or identity')
    if manifest.get('background') or manifest.get('content_scripts'):
        raise ValueError('Clipper must not gain an implicit background capture surface')
    return result


def package(destination, source=ROOT / 'browser-clipper'):
    files = extension_files(source)
    # Exclusive creation prevents replacing a previously reviewed package.
    with zipfile.ZipFile(destination, 'x', zipfile.ZIP_DEFLATED) as archive:
        for name, body in sorted(files.items()):
            info = zipfile.ZipInfo('Trove-Web-Clipper/' + name, (2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, body)
    return {'extensionId': EXPECTED_ID, 'sha256': hashlib.sha256(destination.read_bytes()).hexdigest(),
            'files': {name: hashlib.sha256(body).hexdigest() for name, body in sorted(files.items())}}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path, help='New ZIP path')
    args = parser.parse_args()
    print(json.dumps(package(args.output), indent=2))
