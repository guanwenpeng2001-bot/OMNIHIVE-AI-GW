import { describe, expect, it } from 'vitest'
import { COMPATIBLE_PROVIDERS } from '@/constants/compatibleProviders'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import * as platformColors from '@/utils/platformColors'
import { platformSolidBadgeClass } from '@/utils/platformColors'

/**
 * 账号列表（/admin/accounts）的实心徽章色表。
 *
 * 背景：PlatformTypeBadge.vue 曾自带一份手写色表，只硬编码了 8 家，
 * 其余 11 家全部落进 `bg-blue-100 text-blue-700` 兜底，视觉上分不出平台。
 * 这份用例保留原有品牌色约束，并允许注册的兼容供应商有意共用灰色色系。
 */

// Original brand sharing is pair-specific; registered compatible providers share one category palette.
const INTENTIONALLY_SHARED_COLOR_PAIRS: ReadonlyArray<readonly [string, string, string]> = [
  ['commandcode', 'composite', 'composite 是组过滤用的合流标识，权威表里与 commandcode 同取 cyan'],
]

// 上一批只改了 BADGE / BADGE_LIGHT 等四张表，账号列表用的实心徽章当时仍走手写表：
// 下面这 11 家在列表里全部塌成同一个蓝色兜底，现在必须两两可分。
const PREVIOUSLY_FALLBACK_BLUE_PLATFORMS = [
  'gemini', 'cursor', 'qianwen', 'commandcode', 'volcengine', 'bedrock',
  'mistral', 'hunyuan', 'qianfan', 'opencode_go', 'composite',
]

// 手写表原有条目的 8 家，用于断言上面 11 家不再与它们撞色。
const HANDWRITTEN_TABLE_PLATFORMS = [
  'anthropic', 'openai', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax',
]

const platformIds = GROUP_PLATFORM_OPTIONS.map(option => option.value)
// Freeze original brand coverage independently of registry-derived coverage below.
const originalPlatformIds = [...HANDWRITTEN_TABLE_PLATFORMS, ...PREVIOUSLY_FALLBACK_BLUE_PLATFORMS, 'openrouter']
const compatibleIds = new Set(COMPATIBLE_PROVIDERS.map(provider => provider.id))
const sharedPairKeys = new Set(
  INTENTIONALLY_SHARED_COLOR_PAIRS.flatMap(([left, right]) => [`${left}|${right}`, `${right}|${left}`]),
)
const isIntentionallyShared = (left: string, right: string) => sharedPairKeys.has(`${left}|${right}`) || (compatibleIds.has(left) && compatibleIds.has(right))

const colorFamily = (platform: string) => platformSolidBadgeClass(platform).match(/\bbg-([a-z]+)-100\b/)?.[1] ?? ''

describe('solid platform badge palette', () => {
  it('covers every group platform id and never falls through to the neutral default', () => {
    const fallbackClass = platformSolidBadgeClass('not-a-real-platform')

    for (const platform of platformIds) {
      expect(platformSolidBadgeClass(platform), platform).not.toBe(fallbackClass)
    }
    // 兜底不得与任何一家同色：否则未知平台会被误认成某个已知平台。
    expect(platformIds.filter(platform => platformSolidBadgeClass(platform) === fallbackClass)).toEqual([])
    expect(platformIds).toEqual(expect.arrayContaining(PREVIOUSLY_FALLBACK_BLUE_PLATFORMS))
  })

  it('keeps the existing design language: opaque light tint, dark text, dark variant', () => {
    for (const platform of platformIds) {
      const classes = platformSolidBadgeClass(platform)
      expect(classes, platform).toMatch(/\bbg-[a-z]+-100\b/)
      expect(classes, platform).toMatch(/\btext-[a-z]+-700\b/)
      expect(classes, platform).toMatch(/\bdark:bg-[a-z]+-(?:900\/30|800)\b/)
      expect(classes, platform).toMatch(/\bdark:text-[a-z]+-(?:300|400)\b/)
    }
  })

  it('preserves brand colors while allowing the documented compatible-provider category palette', () => {
    const unregisteredCollisions: string[] = []
    for (const [index, left] of platformIds.entries()) {
      for (const right of platformIds.slice(index + 1)) {
        if (isIntentionallyShared(left, right)) continue
        if (platformSolidBadgeClass(left) === platformSolidBadgeClass(right)) {
          unregisteredCollisions.push(`${left}/${right}`)
        }
      }
    }
    expect(unregisteredCollisions).toEqual([])

    // 登记在册的共用必须真的共用，避免清单变成摆设。
    for (const [left, right] of INTENTIONALLY_SHARED_COLOR_PAIRS) {
      expect(platformSolidBadgeClass(left)).toBe(platformSolidBadgeClass(right))
    }
  })

  it('keeps the eleven previously blue platforms mutually distinguishable', () => {
    const collisions: string[] = []
    for (const [index, left] of PREVIOUSLY_FALLBACK_BLUE_PLATFORMS.entries()) {
      for (const right of PREVIOUSLY_FALLBACK_BLUE_PLATFORMS.slice(index + 1)) {
        if (isIntentionallyShared(left, right)) continue
        if (platformSolidBadgeClass(left) === platformSolidBadgeClass(right)) collisions.push(`${left}/${right}`)
      }
    }
    expect(collisions).toEqual([])

    const driftingPlatforms = PREVIOUSLY_FALLBACK_BLUE_PLATFORMS.filter(
      platform => platformSolidBadgeClass(platform) === 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
    )
    // 蓝色兜底本身是 gemini 的正式色系，只有 gemini 可以蓝。
    expect(driftingPlatforms).toEqual(['gemini'])
  })

  it('keeps the eleven off the eight colors the old handwritten table already owned', () => {
    const collisions: string[] = []
    for (const latePlatform of PREVIOUSLY_FALLBACK_BLUE_PLATFORMS) {
      for (const existingPlatform of HANDWRITTEN_TABLE_PLATFORMS) {
        if (isIntentionallyShared(latePlatform, existingPlatform)) continue
        if (platformSolidBadgeClass(latePlatform) === platformSolidBadgeClass(existingPlatform)) {
          collisions.push(`${latePlatform}/${existingPlatform}`)
        }
      }
    }
    expect(collisions).toEqual([])
  })

  it('preserves the original brand color families independently of registry growth', () => {
    const families = {
      anthropic: 'orange', openai: 'emerald', antigravity: 'purple', gemini: 'blue',
      grok: 'zinc', cursor: 'stone', kimi: 'pink', zhipu: 'indigo', deepseek: 'teal',
      minimax: 'rose', qianwen: 'violet', commandcode: 'cyan', volcengine: 'sky',
      bedrock: 'yellow', mistral: 'amber', openrouter: 'neutral', hunyuan: 'fuchsia',
      qianfan: 'red', opencode_go: 'lime', composite: 'cyan',
    }
    for (const [platform, family] of Object.entries(families)) expect(colorFamily(platform), platform).toBe(family)
  })

  it('separates Bedrock from Anthropic in the account list', () => {
    // 权威表里两家都是 orange；Bedrock 是另一条上游、另一套计费，列表里必须分得开。
    expect(platformSolidBadgeClass('bedrock')).not.toBe(platformSolidBadgeClass('anthropic'))
    expect(colorFamily('bedrock')).not.toBe('orange')
    expect(colorFamily('anthropic')).toBe('orange')
  })
})

// Every public palette is checked, including literal hex accents.
describe('all platform palettes', () => {
  const palettes = Object.entries(platformColors).filter(([name]) => !name.startsWith('platformSelection'))
  it.each(palettes)('%s distinguishes brands and the shared compatible category', (_name, palette) => {
    for (const [index, left] of platformIds.entries()) {
      for (const right of platformIds.slice(index + 1)) {
        if (isIntentionallyShared(left, right)) continue
        expect(palette(left), `${left}/${right}`).not.toBe(palette(right))
      }
    }
  })
})

// Gray is a category palette for registered compatible providers, not a supplier
// brand. Only members of this registry may share it; unknown values keep fallback.
describe('registered compatible palette policy', () => {
  const palettes = Object.entries(platformColors)
  it('covers the original nineteen brands plus composite independently', () => {
    expect(originalPlatformIds).toHaveLength(20)
    expect(platformIds).toEqual(expect.arrayContaining(originalPlatformIds))
    expect(COMPATIBLE_PROVIDERS.length).toBeGreaterThanOrEqual(70)
    expect(platformIds).toEqual(expect.arrayContaining([...compatibleIds]))
  })

  it.each(palettes)('%s resolves every registered ID including selected cards', (_name, palette) => {
    for (const platform of platformIds) {
      expect(typeof palette(platform), platform).toBe('string')
      expect(palette(platform).length, platform).toBeGreaterThan(0)
    }
    const shared = palette(COMPATIBLE_PROVIDERS[0]!.id)
    for (const platform of compatibleIds) expect(palette(platform), platform).toBe(shared)
    for (const platform of originalPlatformIds) expect(palette(platform), platform).not.toBe(shared)
    for (const unknown of ['', 'not-a-real-platform', 'compatible', 'toString', '__proto__']) {
      expect(palette(unknown), unknown).not.toBe(shared)
    }
  })

  it('automatically styles registry growth in all palettes without adding brand entries', () => {
    const providers = COMPATIBLE_PROVIDERS as Array<(typeof COMPATIBLE_PROVIDERS)[number]>
    const added = { ...providers[0]!, id: 'test-new-compatible-provider' }
    const unknown = palettes.map(([, palette]) => palette(added.id))
    providers.push(added)
    try {
      for (const [index, [, palette]] of palettes.entries()) {
        expect(palette(added.id)).toBe(palette(providers[0]!.id))
        expect(palette(added.id)).not.toBe(unknown[index])
      }
    } finally {
      providers.pop()
    }
  })
})
