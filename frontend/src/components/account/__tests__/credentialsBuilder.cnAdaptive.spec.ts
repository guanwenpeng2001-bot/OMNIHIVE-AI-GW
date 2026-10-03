import { describe, expect, it } from 'vitest'

import { cnSupportsNativeResponses, defaultCNAdaptiveBaseUrls, CN_BASE_URL_PRESETS, showsAccountModeCard, showsCodingPlanMode, cnQuotaCellVisible } from '../credentialsBuilder'

describe('cnSupportsNativeResponses', () => {
  it('is true for DeepSeek, Kimi, and MiniMax', () => {
    expect(cnSupportsNativeResponses('deepseek')).toBe(true)
    expect(cnSupportsNativeResponses('kimi')).toBe(true)
    expect(cnSupportsNativeResponses('minimax')).toBe(true)
    expect(cnSupportsNativeResponses('zhipu')).toBe(true)
    expect(cnSupportsNativeResponses('openai')).toBe(false)
  })
})

describe('defaultCNAdaptiveBaseUrls', () => {
  it('resolves Kimi endpoints by account mode', () => {
    expect(defaultCNAdaptiveBaseUrls('kimi', 'payg')).toEqual({
      chat_completions: 'https://api.moonshot.cn/v1',
      anthropic: 'https://api.moonshot.cn/anthropic',
      responses: 'https://api.moonshot.cn/v1'
    })
    expect(defaultCNAdaptiveBaseUrls('kimi', 'coding')).toEqual({
      chat_completions: 'https://api.kimi.com/coding/v1',
      anthropic: 'https://api.kimi.com/coding',
      responses: 'https://api.kimi.com/coding/v1'
    })
  })

  it('resolves GLM endpoints by account mode', () => {
    expect(defaultCNAdaptiveBaseUrls('zhipu', 'payg')).toEqual({
      chat_completions: 'https://open.bigmodel.cn/api/paas/v4',
      anthropic: 'https://open.bigmodel.cn/api/anthropic',
      responses: 'https://open.bigmodel.cn/api/v1'
    })
    expect(defaultCNAdaptiveBaseUrls('zhipu', 'coding')).toEqual({
      chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
      anthropic: 'https://open.bigmodel.cn/api/anthropic',
      responses: 'https://open.bigmodel.cn/api/v1'
    })
  })

  it('includes all three native DeepSeek endpoints', () => {
    expect(defaultCNAdaptiveBaseUrls('deepseek', 'payg')).toEqual({
      chat_completions: 'https://api.deepseek.com',
      anthropic: 'https://api.deepseek.com/anthropic',
      responses: 'https://api.deepseek.com'
    })
  })

  it('uses the same MiniMax CN endpoints for payg and coding', () => {
    const expected = {
      chat_completions: 'https://api.minimaxi.com/v1',
      anthropic: 'https://api.minimaxi.com/anthropic',
      responses: 'https://api.minimaxi.com/v1'
    }
    expect(defaultCNAdaptiveBaseUrls('minimax', 'payg')).toEqual(expected)
    expect(defaultCNAdaptiveBaseUrls('minimax', 'coding')).toEqual(expected)
  })
})


describe('Qianwen Coding Plan protocol defaults', () => {
  it('exposes subscription access independently of quota API availability', () => {
    expect(showsAccountModeCard('qianwen')).toBe(true)
    expect(showsCodingPlanMode('qianwen')).toBe(true)
    expect(cnQuotaCellVisible('qianwen', 'coding')).toBe(false)
    expect(defaultCNAdaptiveBaseUrls('qianwen', 'coding')).toEqual({
      chat_completions: 'https://coding.dashscope.aliyuncs.com/v1',
      anthropic: 'https://coding.dashscope.aliyuncs.com/apps/anthropic',
      responses: ''
    })
    expect(CN_BASE_URL_PRESETS.qianwen.filter(preset => preset.mode === 'coding' && preset.url.startsWith('https://coding.dashscope.aliyuncs.com/')).map(preset => preset.protocol)).toEqual(['chat_completions', 'anthropic'])
    expect(defaultCNAdaptiveBaseUrls('qianwen', 'payg').chat_completions).toBe('https://dashscope.aliyuncs.com/compatible-mode/v1')
  })
  it('makes CommandCode native Responses selectable', () => {
    expect(cnSupportsNativeResponses('commandcode')).toBe(true)
    expect(defaultCNAdaptiveBaseUrls('commandcode', 'payg').responses).toBe('https://api.commandcode.ai/provider/v1')
  })
})
