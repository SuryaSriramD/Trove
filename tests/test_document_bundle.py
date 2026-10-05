import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scripts'))
from document_bundle import assets, validate, RUNTIMES, MIN_WEBVIEW2
import desktop_package as d
from test_desktop_package import DesktopPackageTests
class DocumentPackageTests(DesktopPackageTests):
    def document(self,root,target):
        os_name=target.split('-')[0];suffix='.exe' if os_name=='windows' else ''
        self.binary(root/('document-worker'+suffix),target)
        resource=root/'document-assets';resource.mkdir()
        for name in ['print.html','extract.html','validate.html']:(resource/name).write_text('<!doctype html>'+name)
        (resource/'assets').mkdir();(resource/'assets/parser.js').write_text('/* fixture */')
        meta=dict(schemaVersion=1,name='document-worker',os=os_name,architecture='amd64',runtime=RUNTIMES[os_name],sha256=d.digest(root/('document-worker'+suffix)),testOnly=False,assets=assets(resource))
        if os_name=='windows':meta['minimumWebView2']=MIN_WEBVIEW2
        else:self.binary(root/'document-web-extension.so',target);meta['webExtensionSha256']=d.digest(root/'document-web-extension.so')
        (root/'document-worker.json').write_text(json.dumps(meta));return meta
    def test_document_bundle_requires_target_policy_and_exact_asset_bytes(self):
        for target in d.TARGETS:
            root=self.bundle(target);meta=self.document(root,target)
            validate(root,target,meta) # Source-manifest validation remains useful while Linux execution is disabled.
            if target.startswith('linux'):
                with self.assertRaisesRegex(ValueError,'whole-process containment'):d.validate_native(root,target,self.lock)
            else:d.validate_native(root,target,self.lock)
            for key,value in [('testOnly',True),('runtime','floating'),('minimumWebView2','94') if target.startswith('windows') else ('webExtensionSha256','0'*64)]:
                with self.subTest(target=target,key=key),self.assertRaises(ValueError):validate(root,target,dict(meta,**{key:value}))
            (root/'document-assets/assets/parser.js').write_text('modified')
            with self.assertRaisesRegex(ValueError,'inventory'):validate(root,target,meta)
    def test_missing_declared_document_or_ocr_is_rejected(self):
        root=self.bundle('linux-amd64');m=json.loads((root/'manifest.json').read_text())
        for capability in ['document_pdf','ocr_english']:
            m['capabilities']=[capability];(root/'manifest.json').write_text(json.dumps(m))
            with self.assertRaises(ValueError if capability=='document_pdf' else FileNotFoundError):d.validate_native(root,'linux-amd64',self.lock)
    def test_ocr_target_model_and_production_manifest_are_all_bound(self):
        root=self.bundle('linux-amd64');self.binary(root/'ocr-worker','linux-amd64');(root/'eng.traineddata').write_bytes(b'fixture model')
        pin=json.loads((d.ROOT/'native/ocr.lock.json').read_text())
        meta=dict(schemaVersion=1,name='ocr-worker',os='linux',architecture='amd64',runtime=pin['runtime'],testOnly=False,sha256=d.digest(root/'ocr-worker'),modelSha256=pin['model']['sha256'])
        (root/'ocr-worker.json').write_text(json.dumps(meta))
        with self.assertRaisesRegex(ValueError,'model'):d.validate_native(root,'linux-amd64',self.lock)
        # Source pin remains production-pinned; only this read-only fixture hash adapter
        # supplies the matching model to exercise later checks without copying weights.
        original=d.digest
        with patch('desktop_package.digest',side_effect=lambda p:pin['model']['sha256'] if Path(p).name=='eng.traineddata' else original(p)):
            d.validate_native(root,'linux-amd64',self.lock)
            meta['testOnly']=True;(root/'ocr-worker.json').write_text(json.dumps(meta))
            with self.assertRaisesRegex(ValueError,'production'):d.validate_native(root,'linux-amd64',self.lock)
            meta['testOnly']=False;meta['os']='windows';(root/'ocr-worker.json').write_text(json.dumps(meta))
            with self.assertRaisesRegex(ValueError,'production'):d.validate_native(root,'linux-amd64',self.lock)
    def test_unlisted_and_unsafe_assets_are_refused(self):
        root=self.root/'assets';root.mkdir()
        for name in ['print.html','extract.html','validate.html']:(root/name).write_text('x')
        (root/'secrets.txt').write_text('never served')
        with self.assertRaisesRegex(ValueError,'asset'):assets(root)
    def test_copied_candidate_revalidates_sidecar_after_input_changes(self):
        root=self.bundle('windows-amd64');self.document(root,'windows-amd64');destination=self.root/'candidate'
        original=d.copy_verified
        def changed(source,dest):
            entries=original(source,dest)
            (Path(dest)/'document-assets/assets/parser.js').write_text('changed after review')
            return entries
        with patch('desktop_package.copy_verified',side_effect=changed):
            with self.assertRaisesRegex(ValueError,'inventory'):d.copy_verified_native(root,destination,'windows-amd64',self.lock)
    def test_common_builder_plan_has_no_side_effects(self):
        for target in d.TARGETS:
            output=self.root/'not-created'
            run=subprocess.run([sys.executable,d.ROOT/'scripts/build-document-portable.py','--target',target,'--output',output,'--plan'],capture_output=True,text=True,check=True)
            record=json.loads(run.stdout);self.assertFalse(record['executionQualified']);self.assertFalse(record['networkAcquisition']);self.assertFalse(output.exists());self.assertEqual(target.startswith('windows'),record['runtimeAvailable'])
    def test_linux_actual_document_build_refuses_before_toolchain_or_output(self):
        output=self.root/'never-created'
        run=subprocess.run([sys.executable,d.ROOT/'scripts/build-document-portable.py','--target','linux-amd64','--output',output],capture_output=True,text=True)
        self.assertNotEqual(run.returncode,0);self.assertIn('whole-process containment',run.stderr);self.assertFalse(output.exists())
if __name__=='__main__':unittest.main()
