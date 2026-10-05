#!/usr/bin/env python3
"""Reproduce the exact ledger-bound extracts without modifying supplied documents."""
import hashlib
import json
from pathlib import Path
import zipfile
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
ledger = json.loads((ROOT / 'docs/phase-0/requirements-ledger.json').read_text())
outputs = {}
ns = {'w': 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
for name in ('PRD', 'TRD'):
    source = ledger['sources'][name]
    document = ROOT / ('Trove_' + name + '.docx')
    if hashlib.sha256(document.read_bytes()).hexdigest() != source['sha256']:
        raise SystemExit('Document differs from the requirements ledger: ' + document.name)
    with zipfile.ZipFile(document) as archive:
        tree = ET.fromstring(archive.read('word/document.xml'))
    paragraphs = [''.join(n.text or '' for n in p.findall('.//w:t', ns)) for p in tree.findall('.//w:p', ns)]
    # Empty paragraphs bind ledger line anchors; the original extract had no added final newline.
    raw = '\n'.join(paragraphs).encode('utf-8')
    if hashlib.sha256(raw).hexdigest() != source['textSha256']:
        raise SystemExit('Extract differs from the requirements ledger: ' + name)
    target = ROOT / '.phase0-source' / ('Trove_' + name + '.txt')
    if target.exists() and target.read_bytes() != raw:
        raise SystemExit('Existing source extract differs; inspect before replacing: ' + str(target))
    outputs[target] = raw
for target, raw in outputs.items():
    if not target.exists():
        target.parent.mkdir(exist_ok=True)
        target.write_bytes(raw)
