import type { CompatibleProvider } from '@/constants/compatibleProviders'
export const compatibleFixture: CompatibleProvider = {
  id: 'example-cloud', label: 'Example Cloud', label_zh: '示例云', base_source: 'preset', default_mode: 'payg',
  evidence_url: 'https://docs.example.com', evidence_observed: '2026-10-03',
  modes: [
    { mode: 'payg', default_protocol: 'chat_completions', endpoints: [
      { protocol: 'chat_completions', base_url: 'https://api.example.com/v1', auth: 'bearer' },
      { protocol: 'anthropic', base_url: 'https://api.example.com/anthropic', auth: 'x_api_key' }
    ], bridges: ['messages_to_chat', 'responses_to_chat'], discovery: 'openai_models', discovery_protocol: 'chat_completions', capability_profile: 'example-cloud' },
    { mode: 'coding', default_protocol: 'anthropic', endpoints: [
      { protocol: 'anthropic', base_url: 'https://coding.example.com', auth: 'x_api_key' }
    ], bridges: [], discovery: 'none', capability_profile: 'example-cloud-coding' }
  ]
}
export const resourceFixture: CompatibleProvider = {
  ...compatibleFixture, id: 'example-resource', label: 'Example Resource', label_zh: '示例资源', base_source: 'account',
  modes: [{ ...compatibleFixture.modes[0], endpoints: [
    { protocol: 'chat_completions', path: '/openai/v1', auth: 'api_key_header' },
    { protocol: 'responses', path: '/openai/v1', auth: 'api_key_header' }
  ], bridges: [], discovery: 'none' }]
}
