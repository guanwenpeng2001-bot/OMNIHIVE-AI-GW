import { describe, expect, it } from 'vitest'
import {
  CONCRETE_PLATFORM_OPTIONS, PRODUCT_PLATFORM_OPTIONS, PRODUCT_GROUP_PLATFORM_OPTIONS,
  HOSTED_ACCOUNT_OPTIONS, canonicalPlatform, hostedAccountType, hostedQuotaKey
} from '@/constants/platforms'

describe('hosted account identity', () => {
  it('keeps hosting out of product filters without deleting legacy budget keys', () => {
    expect(PRODUCT_PLATFORM_OPTIONS.some(option => hostedAccountType(option.value))).toBe(false)
    expect(PRODUCT_GROUP_PLATFORM_OPTIONS.some(option => hostedAccountType(option.value))).toBe(false)
    expect(CONCRETE_PLATFORM_OPTIONS.some(option => option.value === 'bedrock')).toBe(true)
    expect(HOSTED_ACCOUNT_OPTIONS).toContainEqual({ platform: 'anthropic', type: 'bedrock', label: 'Amazon Bedrock' })
    expect(HOSTED_ACCOUNT_OPTIONS).toContainEqual({ platform: 'gemini', type: 'service_account', label: 'Vertex AI' })
  })
  it.each(['bedrock', 'vertex', 'anthropic', 'gemini', 'openai'])('round trips the %s legacy budget independently', platform => {
    expect(hostedQuotaKey(canonicalPlatform(platform), hostedAccountType(platform))).toBe(platform)
  })
})
