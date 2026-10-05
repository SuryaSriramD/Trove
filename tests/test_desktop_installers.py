"""Runs POSIX install logic with disposable files and explicitly simulated OS tools.

This is not Linux desktop/installation qualification. The only source replacement
redirects /etc/os-release to a synthetic fixture; no production bypass is added.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))

import desktop_package as d

class InstallerOwnershipTests(unittest.TestCase):
    def setUp(self):
        if not Path('/bin/sh').is_file() or not shutil.which('sha256sum'):self.skipTest('GNU sha256sum is needed for shell fixture')
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve();self.source=self.root/'source';self.source.mkdir()
        self.data=self.root/'data with spaces';self.tools=self.root/'tools';self.tools.mkdir()
        self.env=dict(os.environ,XDG_DATA_HOME=str(self.data),PATH=str(self.tools)+os.pathsep+os.environ['PATH'])
        os_release=self.root/'os-release';os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')
        for tool,body in {'uname':'[ "$1" = -s ] && echo Linux || echo x86_64','id':'echo 501','xdg-mime':'exit 0','update-desktop-database':'exit 0','dpkg-query':'printf 2.52.6-0ubuntu0.24.04.1'}.items():
            p=self.tools/tool;p.write_text('#!/bin/sh\n'+body+'\n');p.chmod(0o755)
        for name in ['install.sh','uninstall.sh']:
            text=(d.ROOT/'packaging/portable'/name).read_text().replace('. /etc/os-release',". '"+str(os_release)+"'")
            (self.source/name).write_text(text)
        (self.source/'bin').mkdir();(self.source/'bin/Trove').write_bytes(b'synthetic non-executable app')
        (self.source/'desktop-manifest.json').write_text(json.dumps(dict(schemaVersion=1,appId=d.APP_ID,target='linux-amd64',requiredWebKitGTKPackage='2.52.6-0ubuntu0.24.04.1'),indent=2)+'\n')
        self.checksums()
        self.app=self.data/'Trove/app';self.desktop=self.data/'applications/local.trove.desktop.desktop'
        self.workspace=self.data/'Trove/workspaces/my-data';self.workspace.parent.mkdir(parents=True);self.workspace.write_text('private fixture retained')
    def checksums(self):
        (self.source/'files.sha256').unlink(missing_ok=True)
        (self.source/'files.sha256').write_text(''.join(f"{e['sha256']}  {e['path']}\n" for e in d.inventory(self.source)))
    def runscript(self,name):
        return subprocess.run(['/bin/sh',str(self.source/name)],env=self.env,text=True,capture_output=True)
    def install(self):
        result=self.runscript('install.sh');self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        self.assertTrue(self.app.is_dir());self.assertTrue(self.desktop.is_file())
    def test_install_and_remove_owned_files_keep_workspace(self):
        self.install();result=self.runscript('uninstall.sh')
        self.assertEqual(result.returncode,0,result.stderr+result.stdout)
        self.assertFalse(self.app.exists());self.assertFalse(self.desktop.exists())
        self.assertEqual(self.workspace.read_text(),'private fixture retained')
    def test_preexisting_app_or_registration_is_preserved(self):
        self.app.mkdir();sentinel=self.app/'user-file';sentinel.write_text('keep')
        self.assertNotEqual(self.runscript('install.sh').returncode,0);self.assertEqual(sentinel.read_text(),'keep')
        sentinel.unlink();self.app.rmdir();self.desktop.parent.mkdir();self.desktop.write_text('foreign registration')
        self.assertNotEqual(self.runscript('install.sh').returncode,0);self.assertEqual(self.desktop.read_text(),'foreign registration')
    def test_uninstall_rejects_extra_file_directory_and_foreign_registration(self):
        self.install()
        extra=self.app/'user-added';extra.write_text('keep')
        self.assertNotEqual(self.runscript('uninstall.sh').returncode,0);self.assertTrue(self.desktop.exists());self.assertEqual(extra.read_text(),'keep')
        extra.unlink();extra.mkdir()
        self.assertNotEqual(self.runscript('uninstall.sh').returncode,0);self.assertTrue(extra.exists())
        extra.rmdir();self.desktop.write_text('foreign registration')
        self.assertNotEqual(self.runscript('uninstall.sh').returncode,0);self.assertTrue(self.app.exists());self.assertEqual(self.desktop.read_text(),'foreign registration')
    def test_uninstall_rejects_forged_marker_and_symlink(self):
        self.install();marker=self.app/'.trove-install';original=marker.read_text();marker.write_text('wrong installation')
        self.assertNotEqual(self.runscript('uninstall.sh').returncode,0);self.assertTrue(self.app.exists())
        marker.write_text(original);(self.app/'link').symlink_to(self.workspace)
        self.assertNotEqual(self.runscript('uninstall.sh').returncode,0);self.assertEqual(self.workspace.read_text(),'private fixture retained')
    def test_checksum_path_escape_is_rejected_before_filesystem_use(self):
        (self.source/'files.sha256').write_text('0'*64+'  ../workspace\n')
        self.assertNotEqual(self.runscript('install.sh').returncode,0);self.assertFalse(self.app.exists())
    def test_changed_package_or_prerequisite_is_rejected(self):
        (self.source/'bin/Trove').write_text('tampered')
        self.assertNotEqual(self.runscript('install.sh').returncode,0);self.assertFalse(self.app.exists())
        self.checksums();(self.tools/'dpkg-query').write_text('#!/bin/sh\necho different\n')
        self.assertNotEqual(self.runscript('install.sh').returncode,0);self.assertFalse(self.app.exists())

if __name__=='__main__':unittest.main()
