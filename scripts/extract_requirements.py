#!/usr/bin/env python3
"""Deterministic read-only source extraction for ledger provenance tests."""
from pathlib import Path
import zipfile
import xml.etree.ElementTree as ET
ROOT=Path(__file__).resolve().parents[1]
out=ROOT/'.phase0-source';out.mkdir(exist_ok=True)
ns={'w':'http://schemas.openxmlformats.org/wordprocessingml/2006/main'}
for name in ('Trove_PRD','Trove_TRD'):
 with zipfile.ZipFile(ROOT/(name+'.docx')) as archive:tree=ET.fromstring(archive.read('word/document.xml'))
 paragraphs=[''.join(n.text or '' for n in p.findall('.//w:t',ns)) for p in tree.findall('.//w:p',ns)]
 # Match the original source extract's omitted empty paragraphs.
 text='\n'.join(p for p in paragraphs if p.strip())+'\n'
 existing=out/(name+'.txt')
 if not existing.exists():existing.write_text(text)
