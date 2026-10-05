"""Development setup must be portable, explicit about network use and non-destructive."""
import io
import os
from pathlib import Path
import sys
import runpy
import shutil
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1] / 'scripts'
sys.path.insert(0, str(SCRIPTS))
import devtools
import bootstrap


class DeveloperToolsTests(unittest.TestCase):
    def test_persistent_platform_defaults(self):
        self.assertEqual(devtools.cache_dir(environ={}, system='Darwin', home='/home/test'), Path('/home/test/Library/Caches/Trove'))
        self.assertEqual(devtools.cache_dir(environ={'XDG_CACHE_HOME': '/cache'}, system='Linux', home='/home/test'), Path('/cache/trove'))
        self.assertEqual(devtools.cache_dir(environ={'LOCALAPPDATA': '/local'}, system='Windows', home='/home/test'), Path('/local/Trove/Cache'))

    def test_explicit_cache_overrides_environment(self):
        self.assertEqual(devtools.cache_dir('/explicit', environ={'TROVE_CACHE_DIR': '/ambient'}), Path('/explicit'))
        self.assertEqual(devtools.cache_dir(environ={'TROVE_CACHE_DIR': '/ambient'}), Path('/ambient'))

    def test_models_follow_custom_cache_and_explicit_model_override(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(devtools.models_dir(cache='/custom'), Path('/custom/models'))
        with patch.dict(os.environ, {'TROVE_MODELS_DIR': '/packs'}, clear=True):
            self.assertEqual(devtools.models_dir(cache='/custom'), Path('/packs'))
            self.assertEqual(devtools.models_dir('/explicit', '/custom'), Path('/explicit'))

    def test_config_survives_runtime_environment_sanitization(self):
        env = devtools.build_env('/selected', source={'PATH': '/bin'})
        self.assertEqual(env['TROVE_CACHE_DIR'], '/selected')
        self.assertEqual(env['GOMODCACHE'], '/selected/go-mod')
        self.assertEqual(env['PLAYWRIGHT_BROWSERS_PATH'], '/selected/playwright')
        self.assertEqual(env['GOPROXY'], 'off')
        self.assertEqual(env['GOTOOLCHAIN'], 'local')

    def test_explicit_go_cache_overrides_are_preserved(self):
        source = {'GOCACHE': '/build-cache', 'GOMODCACHE': '/module-cache'}
        env = devtools.build_env('/selected', source=source)
        self.assertEqual(env['GOCACHE'], '/build-cache')
        self.assertEqual(env['GOMODCACHE'], '/module-cache')
        self.assertEqual(source, {'GOCACHE': '/build-cache', 'GOMODCACHE': '/module-cache'})

    def test_online_setup_does_not_force_network_policy(self):
        env = devtools.build_env('/selected', source={}, offline=False)
        self.assertNotIn('GOPROXY', env)
        self.assertNotIn('GOSUMDB', env)

    def test_worktrees_have_distinct_native_build_caches(self):
        self.assertNotEqual(devtools.workspace_cache('/cache', '/worktree/a'), devtools.workspace_cache('/cache', '/worktree/b'))
        self.assertEqual(devtools.workspace_cache('/cache', '/worktree/a'), devtools.workspace_cache('/cache', '/worktree/a'))

    def test_missing_go_reports_bootstrap_action(self):
        with tempfile.TemporaryDirectory() as temporary, patch('devtools.shutil.which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'bootstrap.py --fetch'):
                devtools.go_binary(temporary)

    def test_incomplete_cached_go_is_rejected_instead_of_falling_back(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = root / 'go/bin/go'
            binary.parent.mkdir(parents=True)
            binary.touch()
            with patch('devtools.shutil.which', return_value='/unrelated/go'), patch('devtools.subprocess.check_output', side_effect=[
                'go version go' + devtools.baseline()['go']['version'] + ' darwin/arm64\n', str(root / 'go') + '\n']):
                with self.assertRaisesRegex(RuntimeError, 'standard-library'):
                    devtools.go_binary(root)

    def test_wrong_go_version_is_rejected(self):
        with patch('devtools.shutil.which', return_value='/go'), patch('devtools.subprocess.check_output', return_value='go version go0.0.0 darwin/arm64'):
            with self.assertRaisesRegex(RuntimeError, 'differs from baseline'):
                devtools.go_binary('/missing')

    def test_offline_bootstrap_never_fetches_missing_archive(self):
        with tempfile.TemporaryDirectory() as temporary, patch('bootstrap.urlopen') as network:
            with self.assertRaisesRegex(RuntimeError, '--fetch'):
                bootstrap.archive(Path(temporary), 'tool', {'sha256': 'bad', 'url': 'https://invalid/tool.tar.gz'}, False)
            network.assert_not_called()

    def test_seed_archive_must_match_pin(self):
        with tempfile.TemporaryDirectory() as temporary, patch('bootstrap.urlopen') as network:
            root = Path(temporary)
            cache, seed = root / 'cache', root / 'seed'
            cache.mkdir(); seed.mkdir()
            source = seed / 'tool.tar.gz'
            source.write_bytes(b'verified fixture')
            pin = {'sha256': devtools.file_digest(source), 'url': 'https://invalid/tool.tar.gz'}
            result = bootstrap.archive(cache, 'tool', pin, False, [seed])
            self.assertEqual(result.read_bytes(), source.read_bytes())
            network.assert_not_called()

    def test_download_hash_failure_preserves_existing_archive(self):
        with tempfile.TemporaryDirectory() as temporary, patch('bootstrap.urlopen', return_value=io.BytesIO(b'untrusted')):
            cache = Path(temporary)
            target = cache / 'tool.tar.gz'
            target.write_bytes(b'previous')
            with self.assertRaisesRegex(RuntimeError, 'SHA256'):
                bootstrap.archive(cache, 'tool', {'sha256': 'different', 'url': 'https://invalid'}, True)
            self.assertEqual(target.read_bytes(), b'previous')
            self.assertEqual(list(cache.glob('*.download')), [])

    def test_archive_cannot_escape_cache(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary)
            source = cache / 'bad.tar.gz'
            with tarfile.open(source, 'w:gz') as bundle:
                member = tarfile.TarInfo('../escape')
                member.size = 1
                bundle.addfile(member, io.BytesIO(b'x'))
            with self.assertRaisesRegex(RuntimeError, 'Unsafe archive'):
                bootstrap.extract(source, cache, 'tool')
            self.assertFalse((cache / 'tool').exists())

    def test_archive_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary)
            source = cache / 'bad.tar.gz'
            with tarfile.open(source, 'w:gz') as bundle:
                member = tarfile.TarInfo('tool/link')
                member.type = tarfile.SYMTYPE
                member.linkname = '/outside'
                bundle.addfile(member)
            with self.assertRaisesRegex(RuntimeError, 'Unsafe archive'):
                bootstrap.extract(source, cache, 'tool')

    def test_existing_sources_are_never_overwritten_by_setup(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary)
            target = cache / 'tool'
            target.mkdir()
            local = target / 'local-change'
            local.write_text('keep')
            bootstrap.extract(cache / 'not-needed.tar.gz', cache, 'tool')
            self.assertEqual(local.read_text(), 'keep')

    def test_print_environment_path_has_no_filesystem_side_effects(self):
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary) / 'not-created'
            with patch.object(sys, 'argv', ['bootstrap.py', '--cache', str(cache), '--print-venv']), patch('sys.stdout', new_callable=io.StringIO) as output:
                self.assertEqual(bootstrap.main(), 0)
                self.assertIn(str(cache), output.getvalue())
                self.assertFalse(cache.exists())

    def test_desktop_passes_selected_cache_and_python_to_native_children(self):
        with tempfile.TemporaryDirectory(prefix='trove tooling ') as temporary:
            root = Path(temporary).resolve()
            native = root / 'native'
            native.mkdir()
            (native / 'manifest.json').write_text('{"helpers": []}')
            commands = []
            copytree = shutil.copytree

            def copy_tree(source, destination, *args, **kwargs):
                if Path(source) == SCRIPTS.parent / 'frontend/dist':
                    Path(destination).mkdir(parents=True, exist_ok=True)
                    return str(destination)
                return copytree(source, destination, *args, **kwargs)

            def command(argv, **kwargs):
                commands.append((argv, kwargs.get('env', {})))
                if argv[0] == '/pinned/go' and '-o' in argv:
                    Path(argv[argv.index('-o') + 1]).write_bytes(b'fixture binary')

            with patch.object(sys, 'argv', ['build-desktop.py', '--skip-frontend', '--phase6',
                     '--cache', str(root / 'custom cache'), '--native-bundle', str(native), '--output', str(root / 'Trove.app')]), \
                    patch('devtools.go_binary', return_value='/pinned/go'), \
                    patch('shutil.copytree', side_effect=copy_tree), \
                    patch('subprocess.run', side_effect=command), \
                    patch('subprocess.check_output', return_value='fixture:\n'), \
                    patch('sys.stdout', new_callable=io.StringIO):
                runpy.run_path(str(SCRIPTS / 'build-desktop.py'), run_name='__main__')
            children = [argv for argv, _ in commands if argv[0] == sys.executable]
            self.assertEqual({Path(argv[1]).name for argv in children}, {
                'build-document-extractor.py', 'build-audio-decoder.py', 'build-pdf-renderer.py',
                'build-vision.py', 'build-ocr-worker.py'})
            for argv in children:
                self.assertEqual(argv[argv.index('--cache') + 1], str(root / 'custom cache'))
                self.assertEqual(argv[argv.index('--output') + 1], str(native))
            for _, env in commands:
                self.assertEqual(env['TROVE_CACHE_DIR'], str(root / 'custom cache'))


if __name__ == '__main__':
    unittest.main()
