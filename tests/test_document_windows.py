"""Portable policy/recipe tests do not claim Windows compilation or COM execution."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scripts'))
from devtools import ROOT,baseline,cache_dir,file_digest
spec=importlib.util.spec_from_file_location('document_windows_builder',ROOT/'scripts/build-document-windows.py')
builder=importlib.util.module_from_spec(spec);spec.loader.exec_module(builder)

class DocumentWindowsTests(unittest.TestCase):
    def test_pinned_resource_and_status_policy_on_host(self):
        pin=json.loads((ROOT/'native/document-windows.lock.json').read_text())
        source=cache_dir()/baseline()['workers']['llama']['sourceDirectory']
        header=source/pin['jsonHeader']['path']
        if not header.is_file():self.skipTest('explicit verified native source cache required for C++ host policy checks')
        self.assertEqual(file_digest(header),pin['jsonHeader']['sha256'])
        compiler=shutil.which('clang++') or shutil.which('g++')
        if not compiler:self.skipTest('host C++ compiler required; not Windows target compilation')
        with tempfile.TemporaryDirectory() as temporary:
            binary=Path(temporary)/'document-policy-test'
            command=[compiler,'-std=c++17','-O1','-Wall','-Wextra','-Werror','-I'+str(ROOT/'native'),'-I'+str(header.parent),str(ROOT/'tests/native/document_policy_test.cpp'),'-o',str(binary)]
            subprocess.run(command,check=True,capture_output=True,text=True)
            result=subprocess.run([str(binary)],check=True,capture_output=True,text=True)
            self.assertIn('policy checks passed',result.stdout)
    def test_sdk_members_are_exact_and_archive_is_verified(self):
        with tempfile.TemporaryDirectory() as temporary:
            p=Path(temporary)/'sdk.nupkg';body=b'header fixture'
            with zipfile.ZipFile(p,'w') as z:z.writestr('build/native/include/WebView2.h',body)
            pin={'sizeBytes':p.stat().st_size,'sha256':file_digest(p),'files':{'build/native/include/WebView2.h':hashlib.sha256(body).hexdigest()}}
            self.assertEqual(builder.sdk_files(p,pin)['build/native/include/WebView2.h'],body)
            pin['files']['build/native/include/WebView2.h']='0'*64
            with self.assertRaisesRegex(RuntimeError,'member differs'):builder.sdk_files(p,pin)
            pin['sha256']='0'*64
            with self.assertRaisesRegex(RuntimeError,'does not download'):builder.sdk_files(p,pin)
    def test_duplicate_sdk_member_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            p=Path(temporary)/'sdk.nupkg'
            import warnings
            with warnings.catch_warnings():
                warnings.simplefilter('ignore',UserWarning)
                with zipfile.ZipFile(p,'w') as z:z.writestr('header',b'x');z.writestr('header',b'x')
            pin={'sizeBytes':p.stat().st_size,'sha256':file_digest(p),'files':{'header':hashlib.sha256(b'x').hexdigest()}}
            with self.assertRaisesRegex(RuntimeError,'duplicated'):builder.sdk_files(p,pin)
    def test_plan_is_unqualified_and_does_not_create_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            output=Path(temporary)/'not-created'
            result=subprocess.run([sys.executable,str(ROOT/'scripts/build-document-windows.py'),'--output',str(output),'--plan'],check=True,capture_output=True,text=True)
            report=json.loads(result.stdout);self.assertTrue(report['sourceOnly']);self.assertFalse(report['executionQualified']);self.assertFalse(report['networkAcquisition']);self.assertFalse(output.exists())
    def test_cross_host_build_refuses_before_output(self):
        if sys.platform=='win32':self.skipTest('cross-host refusal checked on non-Windows')
        with tempfile.TemporaryDirectory() as temporary:
            output=Path(temporary)/'not-created'
            result=subprocess.run([sys.executable,str(ROOT/'scripts/build-document-windows.py'),'--output',str(output)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertIn('selected OS/architecture',result.stderr);self.assertFalse(output.exists())

if __name__=='__main__':unittest.main()
