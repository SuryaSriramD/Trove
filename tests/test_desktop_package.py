import json
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))

import desktop_package as d

class DesktopPackageTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.root=Path(self.temp.name)
        self.addCleanup(self.temp.cleanup)
        self.lock={'workers':{'llama':{'commit':'llama-pin'},'whisper':{'commit':'whisper-pin'}}}
    def binary(self,path,target):
        data=bytearray(256)
        if target=='linux-amd64':data[:6]=b'\x7fELF\x02\x01';struct.pack_into('<H',data,18,62)
        else:data[:2]=b'MZ';struct.pack_into('<I',data,60,128);data[128:132]=b'PE\0\0';struct.pack_into('<H',data,132,0x8664)
        path.write_bytes(data);path.chmod(0o755)
    def bundle(self,target):
        root=self.root/target;root.mkdir();helpers=[]
        suffix='.exe' if target.startswith('windows') else ''
        for name in ['llama-worker','whisper-worker','vision-worker']:
            path=root/(name+suffix);self.binary(path,target);runtime='whisper' if name.startswith('whisper') else 'llama'
            helpers.append(dict(name=name,sha256=d.digest(path),architecture='amd64',runtime=runtime+'.cpp',runtimeCommit=runtime+'-pin'))
        (root/'manifest.json').write_text(json.dumps(dict(schemaVersion=2,platform=target.split('-')[0],architecture='amd64',helpers=helpers)))
        audio=root/('audio-decoder'+suffix);self.binary(audio,target)
        (root/'audio-decoder.json').write_text(json.dumps(dict(schemaVersion=1,name='audio-decoder',sha256=d.digest(audio),platform=target.split('-')[0],architecture='amd64',runtime='FFmpeg',runtimeCommit='ffmpeg-9.0.2-trove-restricted-v1')))
        return root
    def test_explicit_target_and_all_required_helper_hashes(self):
        for target in d.TARGETS:
            root=self.bundle(target);self.assertEqual(d.validate_native(root,target,self.lock)['architecture'],'amd64')
            (root/('llama-worker'+('.exe' if target.startswith('windows') else ''))).write_bytes(b'tampered')
            with self.assertRaisesRegex(ValueError,'hash/target'):d.validate_native(root,target,self.lock)
    def test_fixture_and_foreign_platform_refused(self):
        root=self.bundle('linux-amd64')
        with self.assertRaisesRegex(ValueError,'target'):d.validate_native(root,'windows-amd64',self.lock)
        (root/'microphone-fixture.json').write_text('{}')
        with self.assertRaisesRegex(ValueError,'fixture'):d.validate_native(root,'linux-amd64',self.lock)
    def test_missing_decoder_and_runtime_drift_refused(self):
        root=self.bundle('linux-amd64');(root/'audio-decoder.json').unlink()
        with self.assertRaises(FileNotFoundError):d.validate_native(root,'linux-amd64',self.lock)
        self.lock['workers']['llama']['commit']='changed'
        with self.assertRaisesRegex(ValueError,'baseline'):d.validate_native(root,'linux-amd64',self.lock)
    def test_microphone_sidecar_must_match_production_record(self):
        root=self.bundle('linux-amd64');path=root/'microphone-worker';self.binary(path,'linux-amd64')
        helper=dict(name='microphone-worker',sha256=d.digest(path),architecture='amd64',runtime='microphone',runtimeCommit='miniaudio-0.11.25-trove-microphone-v1')
        manifest=json.loads((root/'manifest.json').read_text());manifest['helpers'].append(helper);(root/'manifest.json').write_text(json.dumps(manifest))
        sidecar=dict(schemaVersion=1,platform='linux',helper=helper,testOnly=False,deviceBackendCompiled=True)
        (root/'microphone-worker.json').write_text(json.dumps(sidecar));d.validate_native(root,'linux-amd64',self.lock)
        sidecar['helper']=dict(helper,sha256='different');(root/'microphone-worker.json').write_text(json.dumps(sidecar))
        with self.assertRaisesRegex(ValueError,'provenance'):d.validate_native(root,'linux-amd64',self.lock)

    def test_path_escape_reserved_names_and_checksum_injection(self):
        for path in ['../workspace.db','/tmp/x','a/../b','a/./b','a//b','C:/x','a\\b','a\nother','CON','a/LPT1.log','a.','a/ name ','.']:
            with self.subTest(path=path),self.assertRaises(ValueError):d.safe_relative(path)
        self.assertEqual(d.safe_relative('Resources/native/notices/name @1.0-LICENSE.txt'),'Resources/native/notices/name @1.0-LICENSE.txt')
    def test_symlinks_and_case_collisions_refused(self):
        root=self.root/'source';root.mkdir();(root/'file').write_text('safe');(root/'link').symlink_to(root/'file')
        with self.assertRaisesRegex(ValueError,'Symlink'):d.inventory(root)
        (root/'link').unlink()
        # macOS may be case insensitive; validate the inventory with explicit paths.
        (root/'other').write_text('other')
        with patch.object(Path,'relative_to',side_effect=[Path('FILE'),Path('file')]):
            with self.assertRaisesRegex(ValueError,'Case-colliding'):d.inventory(root)
    def test_copy_detects_changed_bytes_before_publication(self):
        root=self.root/'source';root.mkdir();(root/'file').write_text('reviewed')
        def changed(source,dest):Path(dest).write_text('replaced')
        with patch('desktop_package.shutil.copyfile',side_effect=changed):
            with self.assertRaisesRegex(ValueError,'changed'):d.copy_verified(root,self.root/'stage')
    def test_plan_is_explicitly_unqualified_and_has_no_output_side_effect(self):
        for target in d.TARGETS:
            result=subprocess.run([sys.executable,str(d.ROOT/'scripts/build-desktop-portable.py'),'--target',target,'--plan','--output',str(self.root/'never')],capture_output=True,text=True,check=True)
            plan=json.loads(result.stdout);self.assertFalse(plan['platformQualified']);self.assertTrue(plan['sourceOnly']);self.assertFalse((self.root/'never').exists())
    def test_actual_cross_host_build_refuses_before_mutation(self):
        import platform
        if platform.system()!='Darwin':self.skipTest('this test specifically verifies the Mac cannot claim a Windows/Linux build')
        for target in d.TARGETS:
            result=subprocess.run([sys.executable,str(d.ROOT/'scripts/build-desktop-portable.py'),'--target',target,'--output',str(self.root/'never')],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertIn('Native worker builds must run',result.stderr);self.assertFalse((self.root/'never').exists())

if __name__=='__main__':unittest.main()
