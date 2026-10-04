import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('clipper_packaging', ROOT / 'scripts/package-clipper.py')
packaging = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packaging)


class ClipperPackagingTests(unittest.TestCase):
    def test_deterministic_self_contained_package_and_exclusive_destination(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = packaging.package(root / 'first.zip')
            second = packaging.package(root / 'second.zip')
            self.assertEqual(first, second)
            with zipfile.ZipFile(root / 'first.zip') as archive:
                self.assertEqual(set(archive.namelist()), {'Trove-Web-Clipper/' + name for name in packaging.FILES})
                manifest = json.loads(archive.read('Trove-Web-Clipper/manifest.json'))
                self.assertEqual(manifest['host_permissions'], ['http://127.0.0.1/*'])
                self.assertEqual(set(manifest['permissions']), {'activeTab', 'storage', 'scripting'})
            with self.assertRaises(FileExistsError):
                packaging.package(root / 'first.zip')
