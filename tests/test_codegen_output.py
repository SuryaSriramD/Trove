import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('codegen_output', Path(__file__).resolve().parents[1] / 'scripts/codegen_output.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class CodegenOutputTests(unittest.TestCase):
    def test_drift_check_never_rewrites_checked_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'artifact'
            path.write_bytes(b'original')
            with self.assertRaises(SystemExit):
                module.publish({path: b'changed'}, check=True)
            self.assertEqual(path.read_bytes(), b'original')
            missing = Path(directory) / 'missing' / 'artifact'
            with self.assertRaises(SystemExit):
                module.publish({missing: b'new'}, check=True)
            self.assertFalse(missing.parent.exists())
            module.publish({path: b'changed', missing: b'new'})
            self.assertEqual(path.read_bytes(), b'changed')
            self.assertEqual(missing.read_bytes(), b'new')
            module.publish({path: b'changed', missing: b'new'}, check=True)
