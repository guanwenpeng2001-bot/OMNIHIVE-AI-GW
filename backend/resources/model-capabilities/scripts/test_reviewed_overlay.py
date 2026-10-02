import copy
import unittest

import catalog


class ReviewedOverlayTests(unittest.TestCase):
    def setUp(self):
        self.inputs = catalog.read(catalog.INPUTS)
        self.review = catalog.read(catalog.REVIEWED)
        self.review['overlays'] = [{
            'profile': 'deepseek', 'model_id': 'deepseek-v4-flash',
            'source': 'https://api-docs.deepseek.com/quick_start/pricing',
            'source_content_hash': 'a' * 64, 'verified_at': '2026-10-02',
            'proof_scope': 'Exact DeepSeek API ID; ambiguous 384K output; vision explicitly supported',
            'suppress_fields': ['max_output_tokens'],
            'fields': {'supports_vision': True}}]

    def test_overlay_suppresses_only_named_field_and_keeps_sibling_provenance(self):
        base, _, _ = catalog.generate(self.inputs, {**self.review, 'overlays': []})
        result, report, _ = catalog.generate(self.inputs, self.review)
        before = next(r for r in base['records'] if r['profile'] == 'deepseek' and r['model_id'] == 'deepseek-v4-flash')
        after = next(r for r in result['records'] if r['profile'] == 'deepseek' and r['model_id'] == 'deepseek-v4-flash')
        self.assertNotIn('max_output_tokens', after)
        self.assertEqual(['max_output_tokens'], after['suppressed_fields'])
        self.assertTrue(after['supports_vision'])
        self.assertEqual(before['verified_at'], after['verified_at'])
        self.assertEqual(before['field_sources']['supports_reasoning'], after['field_sources']['supports_reasoning'])
        self.assertEqual('2026-10-02', after['field_sources']['supports_vision']['observed_at'])
        self.assertEqual('a' * 64, after['field_sources']['supports_vision']['content_hash'])
        row = next(r for r in report['models'] if r['profile'] == 'deepseek' and r['original_id'] == 'deepseek-v4-flash')
        self.assertIn('max_output_tokens', row['conflicts'])
        self.assertTrue(any(e.get('value') == 393216 for e in row['field_evidence']['max_output_tokens']))

    def test_reject_unknown_suppression_and_fields(self):
        for field in ['*', 'pricing', 'max_tokens', 'supports_vision']:
            review = copy.deepcopy(self.review)
            review['overlays'][0]['suppress_fields'] = [field]
            with self.assertRaises(ValueError):
                catalog.generate(self.inputs, review)
        review = copy.deepcopy(self.review)
        review['overlays'][0]['fields']['price'] = 1
        with self.assertRaises(ValueError):
            catalog.generate(self.inputs, review)

    def test_admitted_conflict_set_and_region_separation(self):
        result, coverage, _ = catalog.generate(self.inputs)
        records = {(r['profile'], r['model_id']): r for r in result['records']}
        suppressed = {key: record['suppressed_fields'] for key, record in records.items() if record.get('suppressed_fields')}
        expected = {('deepseek', model): ['max_output_tokens'] for model in
                    ('deepseek-flash', 'deepseek-v4-flash', 'deepseek-v4-flash-vision-exp', 'deepseek-v4-pro')}
        expected['moonshotai-cn', 'kimi-k2.6'] = ['context_limit_kind', 'context_window', 'max_input_tokens', 'max_output_tokens']
        self.assertEqual(expected, suppressed)
        self.assertEqual(1000000, records['moonshot', 'kimi-k2.6']['max_input_tokens'])
        self.assertEqual(384000, records['moonshot', 'kimi-k2.6']['max_output_tokens'])
        self.assertEqual('2026-09-17', records['moonshot', 'kimi-k2.6']['verified_at'])
        self.assertNotIn('max_input_tokens', records['openai', 'gpt-5-pro'])
        self.assertNotIn('suppressed_fields', records['openai', 'gpt-5-pro'])
        cn = next(r for r in coverage['models'] if (r['profile'], r['original_id']) == ('moonshotai-cn', 'kimi-k2.6'))
        self.assertTrue(any(e['value'] == 262144 for e in cn['field_evidence']['context_window']))
        self.assertEqual(len(records), len(coverage['models']))

    def test_exact_hosted_ranges_and_embedding_dimensions(self):
        result, _, _ = catalog.generate(self.inputs)
        records = {(r['profile'], r['model_id']): r for r in result['records']}
        self.assertEqual(393216, records['qianfan', 'deepseek-v4-pro']['max_output_tokens'])
        self.assertEqual(1024, records['qianfan', 'bge-large-zh']['embedding_dimensions'])
        self.assertEqual('embedding', records['qianfan', 'bge-large-zh']['mode'])


if __name__ == '__main__':
    unittest.main()
