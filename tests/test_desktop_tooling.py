"""Source-level host/prerequisite tests; simulated targets are not qualification."""
import contextlib
import io
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))

import bootstrap
import devtools
import doctor
import desktop_prerequisites as prereq

class DesktopToolingTests(unittest.TestCase):
    def test_go_pins_select_each_target_and_reject_unknown(self):
        pins = [devtools.go_archive_pin(t) for t in ('darwin-arm64','windows-amd64','linux-amd64')]
        self.assertEqual(len({p['sha256'] for p in pins}), 3)
        self.assertTrue(pins[1]['url'].endswith('.windows-amd64.zip'))
        self.assertTrue(pins[2]['url'].endswith('.linux-amd64.tar.gz'))
        with self.assertRaises(RuntimeError): devtools.go_archive_pin('windows-arm64')
        self.assertEqual(pins[0]['sha256'], devtools.baseline()['go']['sha256'])

    def test_matching_go_version_for_wrong_target_is_rejected(self):
        with patch('devtools.shutil.which',return_value='/fixture/go'), patch('devtools.host_target',return_value='windows-amd64'), patch('devtools.subprocess.check_output',return_value='go version go1.27.1 darwin/arm64'):
            with self.assertRaisesRegex(RuntimeError,'differs'): devtools.go_binary('/absent')

    def test_bootstrap_target_mismatch_precedes_mutation_or_network(self):
        with tempfile.TemporaryDirectory() as temporary, patch('native_targets.host_target',return_value='darwin-arm64'), patch('bootstrap.urlopen') as network:
            destination=Path(temporary)/'not-created'
            with patch.object(sys,'argv',['bootstrap.py','--target','windows-amd64','--fetch','--cache',str(destination)]):
                with self.assertRaisesRegex(ValueError,'selected OS'): bootstrap.main()
            network.assert_not_called(); self.assertFalse(destination.exists())

    def test_zip_has_expected_root_and_cannot_alias_escape_or_link(self):
        cases=['../outside','go/../outside','go/./alias','go//alias','go/A','go/CON','go/trailing.','go/trailing ','go/back\\slash','go/link']
        for name in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                root=Path(temporary); source=root/'tool.zip'
                with zipfile.ZipFile(source,'w') as z:
                    if name=='go/A': z.writestr('go/a','one')
                    item=zipfile.ZipInfo(name)
                    if name=='go/link': item.external_attr=(stat.S_IFLNK | 0o777) << 16
                    z.writestr(item,'two')
                with self.assertRaisesRegex(RuntimeError,'Unsafe'): bootstrap.extract(source,root,'go')
                self.assertFalse((root/'go').exists())
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); source=root/'tool.zip'
            with zipfile.ZipFile(source,'w') as z: z.writestr('go/bin/go.exe',b'fixture')
            bootstrap.extract(source,root,'go')
            self.assertEqual((root/'go/bin/go.exe').read_bytes(),b'fixture')

    def test_pnpm_windows_shim_uses_node_without_cmd_shell(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); shim=root/'pnpm.cmd';shim.touch()
            entry=root/'node_modules/pnpm/bin/pnpm.cjs';entry.parent.mkdir(parents=True);entry.touch()
            with patch('devtools.platform.system',return_value='Windows'),patch('devtools.shutil.which',side_effect=lambda name: str(shim) if name=='pnpm' else '/fixture/node'):
                result=devtools.tool_command('pnpm','install','--store-dir','spaces & untrusted')
                self.assertEqual(result,['/fixture/node',str(entry),'install','--store-dir','spaces & untrusted'])
                entry.unlink()
                with self.assertRaisesRegex(RuntimeError,'known Node'):devtools.tool_command('pnpm','install')
        self.assertEqual(devtools.build_env('/cache',source={})['COREPACK_ENABLE_NETWORK'],'0')

    def test_windows_and_linux_doctor_do_not_probe_mac_sdk(self):
        for target in ('windows-amd64','linux-amd64'):
            with self.subTest(target=target),patch('doctor.host_target',return_value=target),patch('doctor.prerequisites',return_value={'fixture':True}),patch('doctor.go_binary',return_value='/fixture/go'),patch('doctor.shutil.which',return_value=None),patch('doctor.subprocess.check_output') as subprocess:
                report=doctor.inspect(Path('/missing'),target)
                names={c['name'] for c in report['checks']}
                self.assertNotIn('macOS SDK',names);self.assertNotIn('clang',names)
                self.assertFalse(report['platformQualified']);subprocess.assert_not_called()

    def test_webview_registry_absence_zero_old_wrong_type_and_valid(self):
        for values,expected in [([],None),(['0.0.0.0'],None),(['93.0.1.0'],None),(['invalid'],None),(['94.0.992.31'],'94.0.992.31'),(['100.1.2.3','110.4.5.6'],'110.4.5.6')]:
            paths=[]
            def opened(hive,path,*args):
                paths.append(path)
                if hive>=len(values):raise FileNotFoundError()
                return contextlib.nullcontext(hive)
            reg=SimpleNamespace(HKEY_LOCAL_MACHINE=0,HKEY_CURRENT_USER=1,KEY_READ=1,KEY_WOW64_64KEY=256,REG_SZ=1,OpenKey=opened,QueryValueEx=lambda index,name:(values[index],1))
            with self.subTest(values=values):
                if expected:self.assertEqual(prereq.windows_webview2('94.0.992.31',reg),expected)
                else:
                    with self.assertRaisesRegex(ValueError,'offline'):prereq.windows_webview2('94.0.992.31',reg)
                self.assertEqual(len(paths),2);self.assertTrue(all(p.endswith('{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}') for p in paths))
        reg.QueryValueEx=lambda index,name:('110.4.5.6',2)
        with self.assertRaises(ValueError):prereq.windows_webview2('94.0.992.31',reg)

if __name__=='__main__':unittest.main()
