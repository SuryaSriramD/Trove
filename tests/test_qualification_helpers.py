"""Complete-copy identity checks use small private fixtures, never native code."""
import copy
from pathlib import Path
import os
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import qualification_helpers as helpers


class QualificationHelperTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        (self.source / 'worker').write_bytes(b'original worker')
        (self.source / 'empty').mkdir()
        self.snapshot = self.root / 'snapshot'

    def test_destination_is_exclusive_and_prior_bytes_are_preserved(self):
        self.snapshot.mkdir()
        (self.snapshot / 'prior').write_bytes(b'retained failure')
        with self.assertRaises(FileExistsError):
            helpers.copy_helper_bundle(self.source, self.snapshot)
        self.assertEqual((self.snapshot / 'prior').read_bytes(), b'retained failure')
        self.assertEqual(list(self.snapshot.iterdir()), [self.snapshot / 'prior'])

    def test_source_root_file_link_and_directory_link_are_rejected_before_copy(self):
        for kind in ('root', 'file', 'directory'):
            with self.subTest(kind=kind):
                link = self.root / ('linked-' + kind)
                link.symlink_to(self.source if kind != 'file' else self.source / 'worker')
                if kind == 'root':
                    source = link
                else:
                    source = self.source
                    (source / ('unsafe-' + kind)).symlink_to(link)
                with self.assertRaises(ValueError):
                    helpers.copy_helper_bundle(source, self.snapshot)
                self.assertFalse(self.snapshot.exists())
                if kind != 'root':
                    (source / ('unsafe-' + kind)).unlink()

    @unittest.skipUnless(hasattr(os, 'mkfifo'), 'FIFO fixture requires POSIX')
    def test_special_source_file_is_rejected_without_opening_it(self):
        os.mkfifo(self.source / 'fifo')
        with self.assertRaises(ValueError):
            helpers.copy_helper_bundle(self.source, self.snapshot)
        self.assertFalse(self.snapshot.exists())

    def test_source_change_during_copy_rejects_and_retains_snapshot(self):
        actual_copy = helpers.shutil.copytree
        def mutate(source, destination, **kwargs):
            result = actual_copy(source, destination, **kwargs)
            (self.source / 'worker').write_bytes(b'changed during copy')
            return result
        with patch.object(helpers.shutil, 'copytree', side_effect=mutate):
            # Avoid recursion through shutil.copytree's own directory calls.
            (self.source / 'empty').rmdir()
            with self.assertRaisesRegex(ValueError, 'inventory differs'):
                helpers.copy_helper_bundle(self.source, self.snapshot)
        self.assertEqual((self.snapshot / 'worker').read_bytes(), b'original worker')

    def test_empty_directory_changes_are_part_of_the_contract(self):
        copied = helpers.copy_helper_bundle(self.source, self.snapshot)
        (self.snapshot / 'empty').rmdir()
        record = helpers.check_helper_bundle(self.source, self.snapshot, copied)
        self.assertEqual(record['snapshotChanges'], ['directory:empty'])
        with self.assertRaises(ValueError):
            helpers.validate_helper_copy(record, copied['sourceInventory'])

    def test_verifier_rejects_omitted_metadata_names_and_malformed_identity(self):
        (self.source / '._worker').write_bytes(b'executable, not an allowed omission')
        copied = helpers.copy_helper_bundle(self.source, self.snapshot)
        original = helpers.check_helper_bundle(self.source, self.snapshot, copied)
        for tamper in ('omit', 'change', 'bool_size', 'contract', 'changes'):
            with self.subTest(tamper=tamper):
                record = copy.deepcopy(original)
                if tamper == 'omit':
                    del record['snapshotInventory']['files']['._worker']
                elif tamper == 'change':
                    record['snapshotInventory']['files']['worker']['sha256'] = '0' * 64
                elif tamper == 'bool_size':
                    record['snapshotInventory']['files']['worker']['size'] = False
                elif tamper == 'contract':
                    record['contract'] = 'ignore-dotfiles'
                else:
                    record['sourceChanges'] = ['worker']
                with self.assertRaises(ValueError):
                    helpers.validate_helper_copy(record, copied['sourceInventory'])


if __name__ == '__main__':
    unittest.main()
