"""Offline desktop package validation; plan output never qualifies a target."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import struct

APP_ID = 'local.trove.desktop'
TARGETS = {'windows-amd64': 'Windows 11 x64', 'linux-amd64': 'Ubuntu 24.04 LTS x64'}
ROOT = Path(__file__).resolve().parents[1]

def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()

def no_link(path):
    p = Path(path)
    s = p.lstat()
    if p.is_symlink() or getattr(s, 'st_file_attributes', 0) & 0x400:
        raise ValueError('Symlink/reparse entry is not a package input: ' + str(p))
    return s

def safe_relative(value):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9_@+.,() /=-]+', value):
        raise ValueError('Unsafe package path')
    p = PurePosixPath(value)
    if not p.parts or p.is_absolute() or str(p) != value or any(x in ('', '.', '..') or x.endswith((' ', '.')) for x in p.parts):
        raise ValueError('Noncanonical package path')
    for x in p.parts:
        if x.split('.')[0].upper() in {'CON','PRN','AUX','NUL',*[f'COM{i}' for i in range(1,10)],*[f'LPT{i}' for i in range(1,10)]}:
            raise ValueError('Windows reserved package path')
    return value

def inventory(root):
    root = Path(root)
    if not stat.S_ISDIR(no_link(root).st_mode):
        raise ValueError('Package input must be a directory')
    result, seen = [], set()
    for path in sorted(root.rglob('*')):
        info = no_link(path)
        if stat.S_ISDIR(info.st_mode):
            continue
        if not stat.S_ISREG(info.st_mode):
            raise ValueError('Nonregular package input: ' + str(path))
        if path.name.startswith('._') or path.name == '.DS_Store':
            continue
        relative = safe_relative(path.relative_to(root).as_posix())
        if relative.lower() in seen:
            raise ValueError('Case-colliding package entries')
        seen.add(relative.lower())
        result.append({'path': relative, 'sizeBytes': info.st_size, 'sha256': digest(path)})
    return result

def copy_verified(source, destination):
    entries = inventory(source)
    Path(destination).mkdir(parents=True, exist_ok=False)
    for entry in entries:
        src, dst = Path(source)/entry['path'], Path(destination)/entry['path']
        no_link(src)
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(src, dst)
        if digest(dst) != entry['sha256'] or dst.stat().st_size != entry['sizeBytes']:
            raise ValueError('Package input changed during copy')
        dst.chmod(src.stat().st_mode & 0o777)
    return entries

def binary_target(path, target):
    with Path(path).open('rb') as f:
        data = f.read(64)
        if target == 'linux-amd64':
            return len(data)>=20 and data[:6] == b'\x7fELF\x02\x01' and struct.unpack_from('<H',data,18)[0] == 62
        if len(data)<64 or data[:2]!=b'MZ':
            return False
        offset=struct.unpack_from('<I',data,60)[0]
        if offset > 16<<20:
            return False
        f.seek(offset)
        header=f.read(6)
        return len(header)==6 and header[:4]==b'PE\0\0' and struct.unpack_from('<H',header,4)[0]==0x8664

def validate_native(root, target, lock):
    root=Path(root)
    inventory(root)
    m=json.loads((root/'manifest.json').read_text())
    if m.get('schemaVersion')!=2 or m.get('platform')!=target.split('-')[0] or m.get('architecture')!='amd64' or m.get('testOnly'):
        raise ValueError('Native bundle does not match the production desktop target')
    if (root/'microphone-fixture.json').exists():
        raise ValueError('A capture fixture cannot be distributed as a production helper')
    suffix='.exe' if target.startswith('windows') else ''
    seen=set()
    for helper in m['helpers']:
        name=helper.get('name')
        if name not in ('llama-worker','vision-worker','whisper-worker','microphone-worker') or name in seen or helper.get('architecture')!='amd64':
            raise ValueError('Invalid native helper identity')
        seen.add(name)
        path=root/(name+suffix)
        if digest(path)!=helper['sha256'] or not binary_target(path,target) or (target=='linux-amd64' and not path.stat().st_mode & 0o111):
            raise ValueError('Native helper hash/target mismatch: '+name)
        if name != 'microphone-worker':
            runtime='whisper' if name=='whisper-worker' else 'llama'
            if helper.get('runtime')!=runtime+'.cpp' or helper['runtimeCommit']!=lock['workers'][runtime]['commit']:
                raise ValueError('Native runtime differs from baseline')
        else:
            sidecar=json.loads((root/'microphone-worker.json').read_text())
            if sidecar.get('schemaVersion')!=1 or sidecar.get('platform')!=target.split('-')[0] or sidecar.get('helper')!=helper or helper.get('runtime')!='microphone' or helper.get('runtimeCommit')!='miniaudio-0.11.25-trove-microphone-v2' or sidecar.get('testOnly') is not False or sidecar.get('deviceBackendCompiled') is not True:
                raise ValueError('Production microphone provenance is missing or test-only')
    if not {'llama-worker','whisper-worker','vision-worker'}.issubset(seen):
        raise ValueError('Core local workers are missing')
    audio=json.loads((root/'audio-decoder.json').read_text())
    name=audio.get('name')
    audio_pin=json.loads((ROOT/'native/audio-decoder.lock.json').read_text())
    if audio.get('schemaVersion')!=1 or audio.get('platform')!=target.split('-')[0] or audio.get('architecture')!='amd64' or audio.get('runtime')!='FFmpeg' or audio.get('runtimeCommit')!=audio_pin['runtimeCommit'] or name!='audio-decoder' or audio.get('sha256')!=digest(root/(name+suffix)) or not binary_target(root/(name+suffix),target) or (target=='linux-amd64' and not (root/(name+suffix)).stat().st_mode & 0o111):
        raise ValueError('Audio decoder hash/target mismatch')
    capabilities=m.get('capabilities',[])
    if 'ocr_english' in capabilities or any((root/x).exists() for x in ['ocr-worker'+suffix,'ocr-worker.json','eng.traineddata']):
        ocr=json.loads((root/'ocr-worker.json').read_text());pin=json.loads((ROOT/'native/ocr.lock.json').read_text());helper=root/('ocr-worker'+suffix);model=root/'eng.traineddata'
        if ocr.get('schemaVersion')!=1 or ocr.get('name')!='ocr-worker' or ocr.get('os')!=target.split('-')[0] or ocr.get('architecture')!='amd64' or ocr.get('runtime')!=pin['runtime'] or ocr.get('testOnly') is not False or ocr.get('sha256')!=digest(helper) or helper.stat().st_size>128<<20 or not binary_target(helper,target) or (target=='linux-amd64' and not helper.stat().st_mode&0o111):raise ValueError('OCR production helper identity/target differs')
        if ocr.get('modelSha256')!=pin['model']['sha256'] or model.stat().st_size>32<<20 or digest(model)!=pin['model']['sha256']:raise ValueError('OCR model identity differs')
    if 'document_pdf' in capabilities or any((root/x).exists() for x in ['document-worker'+suffix,'document-worker.json','document-assets','document-web-extension.so']):
        from document_bundle import validate, LINUX_UNAVAILABLE
        if target.startswith("linux"):
            raise ValueError(LINUX_UNAVAILABLE)
        validate(root,target,json.loads((root/'document-worker.json').read_text()))
    return m


def copy_verified_native(source, destination, target, lock):
    # Input validation can precede a slow frontend build. Bind the actual copied
    # candidate to its sidecars again so changed input cannot become a package.
    validate_native(source,target,lock)
    entries=copy_verified(source,destination)
    validate_native(destination,target,lock)
    return entries
