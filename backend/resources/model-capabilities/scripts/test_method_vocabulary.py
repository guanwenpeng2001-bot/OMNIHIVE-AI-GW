"""The reviewed projection uses existing method names, not capture label spellings."""
import unittest

import catalog
import presets


class MethodVocabularyTests(unittest.TestCase):
    def test_all_affected_identities_match_in_source_presets_and_catalog(self):
        expected = {}
        for model in ('chatgpt-image-latest', 'gpt-image-1', 'gpt-image-1-mini',
                      'gpt-image-1.5', 'gpt-image-2', 'gpt-image-2.5-flare',
                      'gpt-image-2.5-sunburst'):
            expected['openai', model] = ['images/edits', 'images/generations'] + (
                ['responses'] if model == 'gpt-image-1' else [])
        for model in ('gpt-4o-mini-transcribe', 'gpt-4o-transcribe',
                      'gpt-4o-transcribe-diarize', 'gpt-transcribe'):
            expected['openai', model] = ['transcriptions'] + (
                ['realtime'] if model in ('gpt-4o-mini-transcribe', 'gpt-4o-transcribe') else [])
        expected['openai', 'gpt-4o-mini-tts'] = ['speech']
        expected['mistral', 'voxtral-mini-tts-2603'] = ['speech']
        for model in ('voxtral-mini-2602', 'voxtral-mini-transcribe-realtime-2602'):
            expected['mistral', model] = ['transcriptions']
        source = catalog.read(presets.INPUTS)
        generated = presets.generate(source)
        capability, _, _ = catalog.generate(catalog.read(catalog.INPUTS))
        records = {(r['profile'], r['model_id']): r for r in capability['records']}
        for profiles in (source['profiles'], generated):
            models = {(p['platform'], m['id']): m for p in profiles
                      if (p['platform'], p['provider_profile']) in
                      {('openai', 'openai'), ('mistral', 'native')}
                      for m in p['models']}
            for identity, methods in expected.items():
                with self.subTest(identity=identity):
                    self.assertEqual(methods, models[identity]['methods'])
                    self.assertEqual(sorted(methods), sorted(records[identity]['supported_methods']))

    def test_no_capture_label_spellings_in_any_projected_method_field(self):
        forbidden = {'audio_speech', 'audio_transcriptions', 'images_generations', 'images_edits'}
        source = catalog.read(presets.INPUTS)
        review = catalog.read(catalog.REVIEWED)
        capability, _, _ = catalog.generate(catalog.read(catalog.INPUTS))
        fields = [m['methods'] for p in source['profiles'] for m in p['models']]
        fields += [m['methods'] for p in presets.generate(source) for m in p['models']]
        fields += [r.get('supported_methods', []) for r in review['records'] + capability['records']]
        fields += [r['fields'].get('supported_methods', []) for r in review['overlays']]
        for methods in fields:
            self.assertFalse(forbidden.intersection(methods), methods)

    def test_generators_preserve_unknown_and_unrelated_methods(self):
        # There is deliberately no broad ingestion rewrite or task-to-chat inference.
        source = catalog.read(presets.INPUTS)
        profile = next(p for p in source['profiles'] if p['platform'] == 'mistral')
        model = next(m for m in profile['models'] if m['id'] == 'voxtral-mini-tts-2603')
        methods = ['unknown', 'fim', 'realtime', 'live', 'ocr', 'future_method']
        model['methods'] = methods[:]
        generated = presets.generate(source)
        result = next(m for p in generated if p['platform'] == 'mistral'
                      for m in p['models'] if m['id'] == model['id'])
        self.assertEqual(methods, result['methods'])
        self.assertEqual(methods, model['methods'])


if __name__ == '__main__':
    unittest.main()
