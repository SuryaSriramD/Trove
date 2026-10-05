import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
spec=importlib.util.spec_from_file_location('ocr_build',ROOT/'scripts/build-ocr-portable.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class OCRBuild(unittest.TestCase):
    def test_declared_plans_pin_engine_model_and_remain_unqualified(self):
        for target in ('windows-amd64','linux-amd64','darwin-arm64'):
            result=subprocess.run([sys.executable,str(ROOT/'scripts/build-ocr-portable.py'),'--target',target,'--plan'],capture_output=True,text=True,check=True)
            body=json.loads(result.stdout)
            self.assertEqual('5.5.1',body['pin']['tesseract']['version'])
            self.assertEqual('1.85.0',body['pin']['leptonica']['version'])
            self.assertEqual('English printed text only',body['language'])
            self.assertIn('independently',body['qualification'])
            self.assertEqual('7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2',body['pin']['model']['sha256'])
    def test_existing_source_cannot_gain_or_change_compiled_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            cache=Path(directory);archive=cache/'fixture.tar.gz'
            with tarfile.open(archive,'w:gz') as tar:
                root=tarfile.TarInfo('source');root.type=tarfile.DIRTYPE;tar.addfile(root)
                item=tarfile.TarInfo('source/a.cpp');item.size=4;tar.addfile(item,io.BytesIO(b'good'))
            pin={'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'sourceDirectory':'source'}
            source=module.source(cache,'fixture',pin,False)
            (source/'extra.cpp').write_text('injected')
            with self.assertRaisesRegex(RuntimeError,'Unexpected file'):module.source(cache,'fixture',pin,False)
            (source/'extra.cpp').unlink();(source/'a.cpp').write_text('bad!')
            with self.assertRaisesRegex(RuntimeError,'Modified OCR source'):module.source(cache,'fixture',pin,False)
    def test_missing_or_unsafe_archive_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            cache=Path(directory)
            with self.assertRaisesRegex(RuntimeError,'Missing or changed'):module.source(cache,'absent',{'sha256':'0'*64,'sourceDirectory':'source'},False)
            archive=cache/'escape.tar.gz'
            with tarfile.open(archive,'w:gz') as tar:
                item=tarfile.TarInfo('source/../../escape');item.size=1;tar.addfile(item,io.BytesIO(b'x'))
            with self.assertRaisesRegex(RuntimeError,'Unsafe OCR source'):module.source(cache,'escape',{'sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'sourceDirectory':'source'},False)

if __name__=='__main__':unittest.main()
