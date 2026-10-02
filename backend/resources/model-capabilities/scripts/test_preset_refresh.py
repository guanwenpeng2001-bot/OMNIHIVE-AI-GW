"""Exact-profile regressions for the October official evidence refresh."""
import unittest

import catalog
import presets


class OfficialPresetRefreshTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.inputs = catalog.read(presets.INPUTS)
        cls.profiles = {(p['platform'], p['provider_profile']): p
                        for p in presets.generate(cls.inputs)}

    def models(self, platform, profile):
        return {m['id']: m for m in self.profiles[platform, profile]['models']}

    def test_kimi_coding_evidence_does_not_refresh_inherited_output_or_global(self):
        coding = self.models('kimi', 'kimi-for-coding')
        self.assertEqual('high', coding['k3']['reasoning_control']['default'])
        self.assertEqual('max', coding['kimi-for-coding']['reasoning_control']['default'])
        self.assertIn('www.kimi.com/code/docs/', coding['k3']['field_sources']['context_window'])
        self.assertIn('models.dev', coding['k3']['field_sources']['max_output_tokens'])
        self.assertTrue(coding['k3']['verified_at'].startswith('2026-09-15'))
        for model in self.models('kimi', 'moonshotai').values():
            self.assertNotIn('platform.kimi.com', model['field_sources']['id'])
        self.assertFalse(any('inventory remains unverified' in gap for gap in
                             self.profiles['kimi', 'kimi-for-coding']['gaps']))

    def test_minimax_preview_and_exact_context_are_scoped(self):
        for profile in ('minimax', 'minimax-cn'):
            models = self.models('minimax', profile)
            self.assertEqual(1000000, models['MiniMax-M3']['context_window'])
            self.assertIn('MiniMax-M2.1-highspeed', models)
            self.assertNotIn('MiniMax-M3.1-Flash-Preview', models)
        for profile in ('minimax-coding-plan', 'minimax-cn-coding-plan'):
            preview = self.models('minimax', profile)['MiniMax-M3.1-Flash-Preview']
            self.assertEqual(['low', 'medium', 'high', 'xhigh', 'max'],
                             preview['reasoning_control']['effort_values'])
            self.assertNotIn('max_output_tokens', preview)

    def test_qwen_total_and_input_remain_distinct_and_hosted_legacy_survives(self):
        for profile in ('alibaba', 'alibaba-cn'):
            model = self.models('qianwen', profile)['qwen3.7-plus']
            self.assertEqual(1000000, model['context_window'])
            self.assertEqual(991808, model['max_input_tokens'])
            self.assertEqual(131072, model['max_output_tokens'])
        for profile in ('alibaba-coding-plan', 'alibaba-coding-plan-cn'):
            models = self.models('qianwen', profile)
            self.assertIn('qwen3.6-flash', models)
            self.assertNotEqual('retired', models['kimi-k2.5']['lifecycle'])

    def test_ambiguous_deepseek_output_is_not_reasserted(self):
        for model in self.models('deepseek', 'deepseek').values():
            self.assertNotIn('max_output_tokens', model)
            self.assertEqual(['chat_completions', 'messages', 'responses'], model['methods'])

    def test_openrouter_has_no_static_candidates(self):
        profile = self.profiles['openrouter', 'openrouter']
        self.assertEqual('unconfigured', profile['coverage_status'])
        self.assertEqual([], profile['models'])

    def test_hosted_additions_need_their_own_methods_and_preserve_old_aliases(self):
        command = self.models('commandcode', 'native')
        self.assertEqual(['messages'], command['claude-sonnet-5-5']['methods'])
        self.assertEqual(1000000, command['claude-sonnet-5-5']['context_window'])
        self.assertNotIn('max_output_tokens', command['claude-sonnet-5-5'])
        self.assertIn('claude-haiku-4-5', command)
        go = self.models('opencode_go', 'opencode-go')
        self.assertIn('grok-4.7', go)
        self.assertIn('ox-alpha-free', go)
        self.assertNotIn('gpt-6.1-sol', go)  # Zen evidence is not Go evidence.

    def test_field_review_resolves_each_observation_to_its_capture(self):
        for profile in self.inputs['profiles']:
            models = {m['id']: m for m in profile['models']}
            records = profile['coverage_scope'].get('official_review', {}).get('records', [])
            for record in records:
                source = self.inputs['sources'][record['source']]
                self.assertEqual(source['url'], record['evidence_url'])
                self.assertEqual(source['content_sha256'], record['capture_sha256'])
                self.assertEqual(source['observed_at'], record['observed_at'])
                model = models[record['exact_id']]
                field = record['method_or_field']
                if record['decision'] == 'confirmed':
                    self.assertIn(field, model)
                    self.assertEqual(source['url'], model['field_sources'][field])
                else:
                    self.assertNotIn(field, model)

    def test_official_new_models_and_vertex_have_independent_evidence(self):
        anthropic = self.models('anthropic', 'anthropic')
        self.assertEqual('medium', anthropic['claude-opus-5-5']['reasoning_control']['default'])
        self.assertEqual('high', anthropic['claude-sonnet-5-5']['reasoning_control']['default'])
        self.assertEqual('deprecated', anthropic['claude-sonnet-4-5']['lifecycle'])
        openai = self.models('openai', 'openai')
        self.assertEqual(1050000, openai['gpt-6.1-sol']['context_window'])
        self.assertEqual(128000, openai['gpt-6.1-sol']['max_output_tokens'])
        self.assertNotEqual('retired', openai['gpt-5.4']['lifecycle'])
        vertex = self.models('gemini', 'vertex-gemini')
        self.assertIn('gemini-3.8-flash', vertex)
        for model in vertex.values():
            self.assertIn('cloud.google.com', model['field_sources']['id'])
            self.assertNotEqual(64, model.get('max_output_tokens'))
            self.assertNotEqual(128, model.get('context_window'))


if __name__ == '__main__':
    unittest.main()
