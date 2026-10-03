/** Shared by the manual, its contents and the router's hash scrolling. */
export const helpGroups = [
  { id: 'start', sections: ['quick', 'keys', 'models', 'suppliers'] },
  { id: 'api', sections: ['addresses', 'chat', 'messages', 'responses', 'gemini', 'retrieval', 'resources'] },
  { id: 'calls', sections: ['streaming', 'tools', 'reasoning', 'cache'] },
  { id: 'media', sections: ['images', 'async-images', 'media-jobs', 'audio'] },
  { id: 'apps', sections: ['clients', 'cc-switch'] },
  { id: 'account', sections: ['usage', 'billing', 'faq'] }
] as const

export const helpSectionIds = helpGroups.flatMap(group => [...group.sections])
export const helpScrollOffset = 112

export const helpClientReferences = [
  { label: 'Claude Code', url: 'https://code.claude.com/docs/en/llm-gateway-connect' },
  { label: 'Codex', url: 'https://developers.openai.com/codex/config-reference' },
  { label: 'OpenCode', url: 'https://opencode.ai/docs/providers/' },
  { label: 'DeepSeek Harness', url: 'https://deepseek-harness.github.io/deepseek-harness/en/guide/providers' },
  { label: 'Pi', url: 'https://pi.dev/docs/latest/models' },
  { label: 'OpenRouter API reference', url: 'https://openrouter.ai/docs/api_reference/overview' },
  { label: 'CC Switch · pinned configuration source', url: 'https://github.com/farion1231/cc-switch/tree/793e67d9b3210eb527e7d36f2c866a56fae9dec7/src/config' }
]
