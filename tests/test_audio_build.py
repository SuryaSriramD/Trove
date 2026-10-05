import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from audio_build import audio_pin, configure_options, verify_config, verify_source


class RestrictedAudioBuild(unittest.TestCase):
    def test_build_policy_excludes_ambient_libraries_and_network(self):
        pin = audio_pin()
        for target in ('darwin-arm64', 'windows-amd64', 'linux-amd64'):
            options = configure_options(pin, target)
            for option in ('--disable-everything', '--disable-autodetect', '--disable-network',
                           '--disable-protocols', '--disable-gpl', '--disable-nonfree', '--disable-shared',
                           '--disable-programs', '--disable-hwaccels'):
                self.assertIn(option, options)
            self.assertNotIn('--enable-gpl', options)
        self.assertEqual(pin['demuxers'], ['wav', 'mp3', 'aac', 'mov'])
        self.assertEqual(pin['sha256'], '84960df915059e8754fef2cd7c9afeb614062b1b5458ec471eecee619ee04e98')

    def test_unapproved_configuration_is_rejected(self):
        pin = audio_pin()
        flags = ['NETWORK', 'GPL', 'NONFREE', 'VERSION3', 'SHARED', 'AVDEVICE', 'AVFILTER', 'SWSCALE']
        config = ''.join('#define CONFIG_' + key + ' 0\n' for key in flags)
        for kind in ('demuxer', 'parser', 'decoder'):
            config += ''.join('#define CONFIG_' + name.upper() + '_' + kind.upper() + ' 1\n' for name in pin[kind + 's'])
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)
            (path / 'config.h').write_text(config)
            (path / 'config_components.h').write_text('')
            verify_config(path, pin)
            for injected in ('HTTP_PROTOCOL', 'FILE_PROTOCOL', 'H264_DECODER', 'MP4_MUXER'):
                (path / 'config_components.h').write_text('#define CONFIG_' + injected + ' 1\n')
                with self.assertRaisesRegex(RuntimeError, 'Unexpected enabled'):
                    verify_config(path, pin)

    def test_missing_verified_source_fails_offline(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(RuntimeError, 'Missing pinned'):
                verify_source(Path(temporary), 'ffmpeg-audio', audio_pin())


if __name__ == '__main__':
    unittest.main()
