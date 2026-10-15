import json
from pathlib import Path
import sys
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from devtools import file_digest
from microphone_bundle import microphone_record, install_microphone_record


class MicrophoneBundleTests(unittest.TestCase):
    def setup_bundle(self, directory):
        root=Path(directory)
        (root/'microphone-worker').write_bytes(b'helper fixture; never executed')
        pin=json.loads((ROOT/'native/microphone.lock.json').read_text())
        helper={'name':'microphone-worker','runtime':'microphone','runtimeCommit':pin['runtimeCommit'],'architecture':'arm64','sha256':file_digest(root/'microphone-worker')}
        sidecar={'schemaVersion':1,'platform':'darwin','helper':helper,'testOnly':False,'deviceBackendCompiled':True,'capabilities':['record','capture_controls_v1','input_devices_v1']}
        (root/'microphone-worker.json').write_text(json.dumps(sidecar))
        return root,helper,sidecar

    def test_rebinds_legacy_manifest_without_changing_other_helpers(self):
        with tempfile.TemporaryDirectory() as directory:
            root,helper,sidecar=self.setup_bundle(directory)
            other={'name':'llama-worker','sha256':'unchanged'}
            (root/'manifest.json').write_text(json.dumps({'schemaVersion':1,'helpers':[other,{'name':'microphone-worker','runtimeCommit':'trove-microphone-v1'}]}))
            install_microphone_record(root,'darwin-arm64')
            manifest=json.loads((root/'manifest.json').read_text())
            self.assertEqual(manifest['schemaVersion'],2)
            self.assertEqual((manifest['platform'],manifest['architecture']),('darwin','arm64'))
            self.assertEqual(manifest['helpers'],[other,helper])
            self.assertTrue((root/'manifest.json').read_bytes().endswith(bytes([10])))

    def test_refuses_fixture_foreign_identity_and_tampered_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            root,helper,sidecar=self.setup_bundle(directory)
            for key,value in [('testOnly',True),('deviceBackendCompiled',False),('platform','windows')]:
                (root/'microphone-worker.json').write_text(json.dumps({**sidecar,key:value}))
                with self.assertRaises(RuntimeError):microphone_record(root,'darwin-arm64')
            (root/'microphone-worker.json').write_text(json.dumps(sidecar))
            (root/'microphone-worker').write_bytes(b'tampered')
            with self.assertRaises(RuntimeError):microphone_record(root,'darwin-arm64')

    def test_refuses_obsolete_runtime_and_incomplete_capabilities(self):
        with tempfile.TemporaryDirectory() as directory:
            root,helper,sidecar=self.setup_bundle(directory)
            variants=[{k:v for k,v in sidecar.items() if k!='capabilities'}]
            variants += [{**sidecar,'capabilities':caps} for caps in [[],['record'],['record','capture_controls_v1'],['record','input_devices_v1']]]
            variants.append({**sidecar,'helper':{**helper,'runtimeCommit':'miniaudio-0.11.25-trove-microphone-v1'}})
            for index,variant in enumerate(variants):
                with self.subTest(variant=index):
                    (root/'microphone-worker.json').write_text(json.dumps(variant))
                    with self.assertRaises(RuntimeError):microphone_record(root,'darwin-arm64')
            (root/'microphone-worker.json').write_text(json.dumps(sidecar))
            self.assertEqual(microphone_record(root,'darwin-arm64'),helper)

    def test_startup_stall_cannot_build_production(self):
        import subprocess
        result=subprocess.run([sys.executable,str(ROOT/'scripts/build-microphone.py'),'--fixture-startup-stall','--plan','--output','unused'],capture_output=True,text=True)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('requires --fixture-only',result.stderr)
