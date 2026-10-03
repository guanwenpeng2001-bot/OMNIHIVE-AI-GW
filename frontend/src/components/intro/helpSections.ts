/** Shared by the manual, its contents and the router's hash scrolling. */
export const helpGroups = [
  { id: 'start', sections: ['quick', 'keys', 'models'] },
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
  { label: 'Pi', url: 'https://pi.dev/docs/latest/models' }
]
