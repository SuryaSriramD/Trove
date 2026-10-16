"""Synthetic consumer counterexamples; no image inference or quality claims."""
import copy
import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('workflow_checks', pathlib.Path(__file__).with_name('test-workflow-evaluation.py'))
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)
m = checks.m

PROFILE_ID = 'smolvlm-500m-idefics3-bounded-v1'
PROFILE_HASH = 'd3288e6d3d3c197c5e31e6d836427a93562315d679519b73eda799fbbef9be2b'
MODEL_HASH = '9d4612de6a42214499e301494a3ecc2be0abdd9de44e663bda63f1152fad1bf4'
PROJECTOR_HASH = '4b2fca922e549e9b9608f4ba56f93565dd5220e935ba5570a2a864a0fcc988a6'
RUNTIME = 'd2e54583c7452353eb35d40431281f6ee984332f'
FIELDS = ('preprocessingProfileId', 'preprocessingProfileHash')


class ImagePreprocessingAccounting(unittest.TestCase):
    def image(self):
        report, contract = checks.WorkflowAccounting().image()
        contract.update(modelSHA256=MODEL_HASH, projectorSHA256=PROJECTOR_HASH,
                        expectedPreprocessingProfile={'id': PROFILE_ID, 'sha256': PROFILE_HASH})
        report['result'].update(modelHash=MODEL_HASH, projectorHash=PROJECTOR_HASH,
                               preprocessingProfileId=PROFILE_ID, preprocessingProfileHash=PROFILE_HASH)
        return report, contract

    def errors(self, report, contract, case='diagram-1'):
        return m.validate_workflow_report(report, case, contract)

    def test_exact_profiled_result_is_accepted(self):
        self.assertEqual(self.errors(*self.image()), [])

    def test_missing_partial_wrong_empty_or_null_profile_is_rejected(self):
        for changes, removed in [({}, FIELDS), ({}, FIELDS[:1]), ({}, FIELDS[1:]),
                                 ({FIELDS[0]: 'other'}, ()), ({FIELDS[1]: 'a'*64}, ()),
                                 ({FIELDS[0]: ''}, ()), ({FIELDS[1]: ''}, ()),
                                 ({FIELDS[0]: None}, ()), ({FIELDS[1]: None}, ())]:
            with self.subTest(changes=changes, removed=removed):
                report, contract = self.image()
                for key in removed: report['result'].pop(key)
                report['result'].update(changes)
                self.assertTrue(self.errors(report, contract))

    def test_explicit_legacy_absence_is_readable_without_invented_provenance(self):
        report, contract = self.image()
        contract['expectedPreprocessingProfile'] = None
        for key in FIELDS: report['result'].pop(key)
        self.assertEqual(self.errors(report, contract), [])
        for field in FIELDS:
            for value in ('', None, PROFILE_ID if field == FIELDS[0] else PROFILE_HASH):
                with self.subTest(field=field, value=value):
                    changed = copy.deepcopy(report); changed['result'][field] = value
                    self.assertTrue(self.errors(changed, contract))

    def test_unrelated_pair_has_no_profile(self):
        report, contract = checks.WorkflowAccounting().image()
        contract['expectedPreprocessingProfile'] = None
        self.assertEqual(self.errors(report, contract), [])
        report['result'].update(preprocessingProfileId=PROFILE_ID, preprocessingProfileHash=PROFILE_HASH)
        self.assertTrue(self.errors(report, contract))

    def test_missing_or_untrusted_contract_cannot_downgrade_provenance(self):
        report, contract = self.image()
        missing = copy.deepcopy(contract); missing.pop('expectedPreprocessingProfile')
        self.assertTrue(self.errors(report, missing))
        for expected in ({'id': 'other', 'sha256': PROFILE_HASH}, {'id': PROFILE_ID, 'sha256': 'a'*64},
                         {'id': PROFILE_ID}, {'sha256': PROFILE_HASH}, {}, '', [], False):
            with self.subTest(expected=expected):
                changed = dict(contract, expectedPreprocessingProfile=expected)
                self.assertTrue(self.errors(report, changed))
        contract['modelSHA256'] = report['result']['modelHash'] = 'a'*64
        self.assertTrue(self.errors(report, contract))

    def test_measurement_rejects_profile_fields_even_when_empty(self):
        report, contract = self.image(); case = contract['imageCases']['boundary-1']
        report.update(sourceSHA256=case['sourceSHA256'], sourceKind=case['sourceKind'], question=case['prompt'], expectedMethod=case['method'])
        report['result'] = dict(state='ready', analysisMethod='pixel_measurement', finishReason='measurement',
            text=f"Source pixel measurement ({case['width']} × {case['height']} decoded pixels, before resizing and alpha composition; encoded orientation): the image is exactly uniform white (blank at decoded-pixel level). No objects or text can be identified from spatial pixel variation. No model interpretation or OCR was attempted.")
        self.assertEqual(self.errors(report, contract, 'boundary-1'), [])
        for field in FIELDS:
            for value in ('', None, PROFILE_ID if field == FIELDS[0] else PROFILE_HASH):
                with self.subTest(field=field, value=value):
                    changed = copy.deepcopy(report); changed['result'][field] = value
                    self.assertTrue(self.errors(changed, contract, 'boundary-1'))

    def test_normalization_rejection_cannot_claim_preprocessing(self):
        report, contract = self.image(); case = copy.deepcopy(contract['imageCases']['diagram-1'])
        case['method'] = 'normalization_rejected'; contract['imageCases']['diagram-1'] = case
        report['expectedMethod'] = case['method']
        report['result'] = dict(state='failed', error='NORMALIZATION_DETAIL_LOST: source mark removed')
        self.assertEqual(self.errors(report, contract), [])
        report['result']['preprocessingProfileId'] = PROFILE_ID
        self.assertTrue(self.errors(report, contract))

    def bound_inputs(self, root):
        profile = root / 'internal/native/vision_profile_v1.json'
        profile.parent.mkdir(parents=True)
        profile.write_bytes((m.ROOT / 'internal/native/vision_profile_v1.json').read_bytes())
        model, projector = root/'chosen.model', root/'projector.model'
        before = {str(profile): {'sha256': hashlib.sha256(profile.read_bytes()).hexdigest(), 'bytes': profile.stat().st_size},
                  str(model): {'sha256': MODEL_HASH, 'bytes': 1}, str(projector): {'sha256': PROJECTOR_HASH, 'bytes': 1}}
        pack = dict(kind='vision', visionVersion=1, runtimeCommit=RUNTIME, sha256=MODEL_HASH,
                    projector=dict(id='projector', sha256=PROJECTOR_HASH))
        return profile, pack, before, model, projector

    def test_expected_profile_is_derived_from_locked_source_and_exact_pair(self):
        with tempfile.TemporaryDirectory() as directory:
            args = self.bound_inputs(pathlib.Path(directory))
            self.assertEqual(m.expected_image_profile(*args), {'id': PROFILE_ID, 'sha256': PROFILE_HASH})
            for key, value in (('kind', 'chat'), ('visionVersion', 2), ('visionVersion', True), ('runtimeCommit', 'b'*40)):
                changed = list(args); changed[1] = dict(args[1], **{key: value})
                self.assertIsNone(m.expected_image_profile(*changed))
            args[1]['sha256'] = args[2][str(args[3])]['sha256'] = 'b'*64
            self.assertIsNone(m.expected_image_profile(*args))

    def test_changed_profile_or_bytes_cannot_be_rebound(self):
        with tempfile.TemporaryDirectory() as directory:
            args = self.bound_inputs(pathlib.Path(directory))
            original = args[0].read_bytes()
            args[0].write_bytes(original + b' ')
            with self.assertRaisesRegex(ValueError, 'locked source bytes'): m.expected_image_profile(*args)
            args[2][str(args[0])] = {'sha256': hashlib.sha256(args[0].read_bytes()).hexdigest(), 'bytes': args[0].stat().st_size}
            with self.assertRaisesRegex(ValueError, 'locked source bytes'): m.expected_image_profile(*args)
            args[0].write_bytes(original)
            args[2][str(args[0])] = {'sha256': PROFILE_HASH, 'bytes': len(original)}
            args[2][str(args[3])]['sha256'] = 'e'*64
            with self.assertRaisesRegex(ValueError, 'selected pack'): m.expected_image_profile(*args)

    def test_actual_image_inventory_detects_profile_change_and_keeps_sources(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            profile, pack, _, _, _ = self.bound_inputs(root)
            corpus_dir, bundle, models = root/'corpus', root/'bundle', root/'models'
            for path in (corpus_dir, bundle, models): path.mkdir()
            for path in (corpus_dir/'images.json', corpus_dir/'source.png', bundle/'vision-worker', models/'projector.model'):
                path.write_bytes(b'fixture')
            paths = m.image_input_paths(root, corpus_dir, {'cases': [{'source': 'source.png'}, {'source': ''}]}, bundle, models, pack)
            self.assertEqual(set(paths), {profile, corpus_dir/'images.json', corpus_dir/'source.png', bundle/'vision-worker', models/'projector.model'})
            before = m.input_snapshot(paths)
            profile.write_bytes(profile.read_bytes() + b' ')
            after = m.input_snapshot(paths)
            self.assertNotEqual(before, after)
            self.assertEqual({key for key in before if before[key] != after[key]}, {str(profile)})


if __name__ == '__main__':
    unittest.main()
