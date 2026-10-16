import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import vision_profile as profile


class VisionProfileTests(unittest.TestCase):
    def test_profile_and_generated_header_bind_exact_bytes(self):
        path = ROOT / 'internal/native/vision_profile_v1.json'
        p = profile.read_profile(path)
        header = profile.profile_header(p)
        for value in (p['id'], p['modelSHA256'], p['projectorSHA256'], hashlib.sha256(path.read_bytes()).hexdigest()):
            self.assertIn(value, header)
        self.assertEqual(p['transport']['maxEdge'], 1024)
        self.assertFalse(p['reference']['originalConversionProcessorProven'])
        with tempfile.TemporaryDirectory() as tmp:
            changed = Path(tmp) / 'changed.json'
            changed.write_bytes(path.read_bytes() + b' ')
            with self.assertRaisesRegex(ValueError, 'reviewed build binding'):
                profile.read_profile(changed)

    def test_unverified_runtime_rejected(self):
        for relative in profile.RUNTIME_HASHES:
            with self.subTest(relative=relative):
                with self.assertRaisesRegex(ValueError, 'Unverified'):
                    profile.adapt_runtime(relative, b'arbitrary upstream source')
        self.assertEqual(profile.adapt_runtime('unrelated.cpp', b'unchanged'), b'unchanged')

    def test_actual_pinned_runtime_copy_and_guards(self):
        source = os.environ.get('TROVE_VISION_RUNTIME_SOURCE')
        if not source:
            self.skipTest('explicit verified runtime source needed for private-copy fixture')
        source = Path(source)
        originals = {p:(source / p).read_bytes() for p in profile.RUNTIME_HASHES}
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            archive = tmp / 'source.tar'
            with tarfile.open(archive, 'w') as tar:
                for name, raw in originals.items():
                    info = tarfile.TarInfo('pinned/' + name)
                    info.size = len(raw)
                    tar.addfile(info, io.BytesIO(raw))
            pin = dict(commit=profile.PINNED_RUNTIME, sourceDirectory='pinned', sha256=hashlib.sha256(archive.read_bytes()).hexdigest())
            stage = profile.stage_profile_source(archive, pin, tmp / 'stage')
            self.assertEqual(profile.stage_profile_source(archive, pin, stage), stage)
            # macOS temp roots may include /var -> /private/var. An alias of
            # the explicitly selected parent is not a link inside the stage.
            base = tmp / 'parent'
            base.mkdir()
            alias = tmp / 'parent-alias'
            alias.symlink_to(base, target_is_directory=True)
            alias_stage = profile.stage_profile_source(archive, pin, alias / 'source')
            self.assertTrue((alias_stage/'tools/mtmd/clip.cpp').is_file())
            destination_link = tmp / 'destination-link'
            destination_link.symlink_to(stage, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, 'symlink'):
                profile.stage_profile_source(archive, pin, destination_link)
            inside = stage/'tools/mtmd/clip.h'
            original_inside = inside.read_bytes()
            inside.unlink()
            inside.symlink_to(source/'tools/mtmd/clip.h')
            with self.assertRaisesRegex(ValueError, 'symlink'):
                profile.stage_profile_source(archive, pin, stage)
            inside.unlink()
            inside.write_bytes(original_inside)

            for name, raw in originals.items():
                self.assertEqual((source / name).read_bytes(), raw)
                self.assertNotEqual((stage / name).read_bytes(), raw)
            self.assertIn('/* image_longest_edge */ 0,', (stage/'tools/mtmd/mtmd.cpp').read_text())
            self.assertIn('ctx_params.image_longest_edge', (stage/'tools/mtmd/mtmd.cpp').read_text())
            clip = (stage/'tools/mtmd/clip.cpp').read_text()
            for guard in ['ctx_params.image_longest_edge != 2048', 'PROJECTOR_TYPE_IDEFICS3', 'h.image_size != 512', 'h.patch_size != 16', 'h.n_merge != 4', 'h.image_longest_edge != 0', 'h.image_min_pixels > 0', 'h.image_max_pixels > 0', 'h.image_mean[c] != 0.5f', 'h.image_std[c] != 0.5f']:
                self.assertIn(guard, clip)
            self.assertLess(clip.index('incompatible bounded image preprocessing profile'), clip.index('loader.load_tensors(*ctx_vision)'))
            (stage/'untracked.cmake').write_text('unsafe')
            with self.assertRaisesRegex(ValueError, 'unexpected'):
                profile.stage_profile_source(archive, pin, stage)
            (stage/'untracked.cmake').unlink()
            (stage/'tools/mtmd/clip.cpp').write_text('changed')
            with self.assertRaisesRegex(ValueError, 'changed'):
                profile.stage_profile_source(archive, pin, stage)

    def test_native_request_profile_guard_without_model(self):
        source = os.environ.get('TROVE_VISION_RUNTIME_SOURCE')
        if not source:
            self.skipTest('explicit verified runtime header needed for tiny native guard fixture')
        p = profile.read_profile(ROOT/'internal/native/vision_profile_v1.json')
        code = r'''#include "vision-profile.hpp"
#include <iostream>
int main(){
 using nlohmann::json;
 json wire={{"inputs",json::array({{{"sha256",VISION_PROFILE_MODEL}},json::object(),json::object(),{{"sha256",VISION_PROFILE_PROJECTOR}}})}};
 json valid={{"schemaVersion",2},{"preprocessingProfileId",VISION_PROFILE_ID},{"preprocessingProfileHash",VISION_PROFILE_HASH}};
 if(!vision_profile_requested(valid,wire))return 1;
 int rejected=0;
 for(auto field:{"preprocessingProfileId","preprocessingProfileHash"}){auto bad=valid;bad.erase(field);try{vision_profile_requested(bad,wire);}catch(...){rejected++;}}
 auto old=valid;old["schemaVersion"]=1;try{vision_profile_requested(old,wire);}catch(...){rejected++;}
 auto bad=valid;bad["preprocessingProfileHash"]=std::string(64,'f');try{vision_profile_requested(bad,wire);}catch(...){rejected++;}
 wire["inputs"][3]["sha256"]="another-projector";try{vision_profile_requested(valid,wire);}catch(...){rejected++;}
 if(vision_profile_requested(json{{"schemaVersion",1}},wire))return 2;
 if(rejected!=5)return 3;
 std::cout<<"exact pair accepted; 5 invalid requests rejected; unrelated legacy preserved"<<std::endl;
}'''
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            (tmp/'vision-profile-generated.hpp').write_text(profile.profile_header(p))
            (tmp/'guard.cpp').write_text(code)
            subprocess.run(['clang++','-std=c++17','-I',str(ROOT/'native'),'-I',str(Path(source)/'vendor/nlohmann'),'-I',str(tmp),str(tmp/'guard.cpp'),'-o',str(tmp/'guard')], check=True, capture_output=True, text=True)
            result = subprocess.run([str(tmp/'guard')], check=True, capture_output=True, text=True)
            self.assertIn('5 invalid requests rejected', result.stdout)

if __name__ == '__main__':
    unittest.main()
