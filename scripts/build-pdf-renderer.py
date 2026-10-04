#!/usr/bin/env python3
"""Build the self-contained macOS WebKit/AppKit rich PDF helper and hash manifest."""
import argparse, hashlib, json, pathlib, subprocess
from devtools import cache_dir
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('--cache',type=pathlib.Path,default=cache_dir());p.add_argument('--output',type=pathlib.Path,default=ROOT/'build/native');a=p.parse_args();a.output.mkdir(parents=True,exist_ok=True)
out=a.output/'pdf-renderer';source=ROOT/'native/pdf-renderer.swift'
subprocess.run(['swiftc','-module-cache-path',str(a.cache/'swift-module-cache'),'-swift-version','5','-O','-target','arm64-apple-macos27.0','-framework','AppKit','-framework','WebKit','-framework','PDFKit',str(source),'-o',str(out)],check=True)
subprocess.run(['codesign','--force','--sign','-',str(out)],check=True)
subprocess.run(['codesign','--verify','--strict',str(out)],check=True)
for line in subprocess.check_output(['otool','-L',str(out)],text=True).splitlines()[1:]:
    library=line.strip().split(' ')[0]
    if not library.startswith(('/usr/lib/','/System/Library/','/Library/Apple/System/')):raise SystemExit('Unbundled PDF dependency: '+library)
(out.with_suffix('.json')).write_text(json.dumps(dict(schemaVersion=1,name=out.name,sha256=hashlib.sha256(out.read_bytes()).hexdigest(),sourceSha256=hashlib.sha256(source.read_bytes()).hexdigest(),runtime='WebKit/AppKit/PDFKit',architecture='arm64',minimumMacOS='27.0'),indent=2)+'\n')
