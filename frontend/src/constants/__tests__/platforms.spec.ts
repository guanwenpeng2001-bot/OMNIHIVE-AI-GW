// @vitest-environment node
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { beforeAll, describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS, getPlatformLabel, platformFamily, platformSupportsOAuth, platformSupportsModelCatalog } from '@/constants/platforms'

import * as platformColors from '@/utils/platformColors'
import englishAccounts from '@/i18n/locales/en/admin/accounts'
import chineseAccounts from '@/i18n/locales/zh/admin/accounts'

interface BackendPlatform {
  id: string
  oauth: boolean
  activeProbe: boolean
  quotaMonitor: boolean
  passiveMonitor: boolean
  modelCatalog: boolean
}

let backend: BackendPlatform[]
function exportBackend(extraDescriptor = false): BackendPlatform[] {
  const output = execFileSync('go', ['test', '-p', '2', '-count=1', '-run', '^TestExportPlatformContract$', '-v', './internal/domain'], {
    cwd: fileURLToPath(new URL('../../../../backend/', import.meta.url)),
    encoding: 'utf8',
    env: { ...process.env, SUB2API_PLATFORM_CONTRACT_TEST_EXTRA: extraDescriptor ? '1' : '0' },
    timeout: 120_000
  })
  const payload = output.match(/^PLATFORM_CONTRACT=(.+)$/m)?.[1]
  if (!payload) throw new Error('Go platform descriptor export did not execute')
  return JSON.parse(payload) as BackendPlatform[]
}

beforeAll(() => { backend = exportBackend() }, 130_000)

function assertConcreteContract(descriptors: BackendPlatform[]) {
  expect(CONCRETE_PLATFORM_OPTIONS.map(p => p.value).sort()).toEqual(descriptors.map(p => p.id).sort())
  for (const p of descriptors) expect(platformSupportsOAuth(p.id), p.id).toBe(p.oauth)
  for (const platform of descriptors) expect(platformSupportsModelCatalog(platform.id), platform.id).toBe(platform.modelCatalog)
}

describe('platform option catalogs', () => {
  it('localizes known platforms and preserves unknown display labels', () => {
    const translate = (key: string) => key === 'admin.accounts.platforms.qianwen' ? 'Qianwen AI' : key
    expect(getPlatformLabel('qianwen', translate)).toBe('Qianwen AI')
    expect(getPlatformLabel('cursor', translate)).toBe('Cursor')
    expect(getPlatformLabel('future-provider', translate)).toBe('future-provider')
    expect(getPlatformLabel('', translate)).toBe('API')
  })

  it('matches actual backend descriptors and shared OAuth capability', () => {
    assertConcreteContract(backend)
    expect(new Set(backend.map(p => p.id)).size).toBe(backend.length)
  })

  it('adds Composite only for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map(p => p.value).sort()).toEqual([...backend.map(p => p.id), 'composite'].sort())
    expect(platformSupportsOAuth('composite')).toBe(true)
    expect(platformSupportsOAuth('unknown')).toBe(false)
    expect(platformSupportsModelCatalog('unknown')).toBe(false)
    expect(platformSupportsModelCatalog('composite')).toBe(false)
  })

  it('detects an unhandled descriptor added to the backend', () => {
    const extendedBackend = exportBackend(true)
    expect(extendedBackend).toHaveLength(backend.length + 1)
    expect(() => assertConcreteContract(extendedBackend)).toThrow()
  }, 130_000)

  it('keeps passive, probe and quota capabilities distinct', () => {
    expect(backend.find(p => p.id === 'cursor')).toMatchObject({ passiveMonitor: true, activeProbe: false, quotaMonitor: false })
    expect(backend.find(p => p.id === 'antigravity')).toMatchObject({ activeProbe: false, quotaMonitor: true })
    expect(backend.find(p => p.id === 'qianwen')).toMatchObject({ activeProbe: true, quotaMonitor: false })
  })
})


describe('platform presentation tokens', () => {
  it.each([
    ['qianfan', 'red', '#dc2626'], ['hunyuan', 'fuchsia', '#d946ef'],
    ['mistral', 'amber', '#f59e0b'], ['bedrock', 'yellow', '#eab308'],
    ['volcengine', 'sky', '#0ea5e9'],
  ])('uses the project %s color family without changing selection fallback', (platform, color, accent) => {
    for (const token of [
      platformColors.platformBadgeClass, platformColors.platformBadgeLightClass,
      platformColors.platformBorderClass, platformColors.platformBorderStrongClass,
      platformColors.platformAccentBarClass, platformColors.platformTextClass,
      platformColors.platformIconClass, platformColors.platformButtonClass,
      platformColors.platformDiscountClass, platformColors.platformGradientClass,
      platformColors.platformGradientTextClass, platformColors.platformGradientSubtextClass,
    ]) {
      expect(token(platform)).toContain(`${color}-`)
    }
    expect(platformColors.platformBadgeClass(platform)).toContain(`dark:text-${color}-300`)
    expect(platformColors.platformAccentColor(platform)).toBe(accent)
    expect(platformColors.platformSelectionClass(platform)).toBe(platformColors.platformSelectionClass('unknown'))
    expect(platformColors.platformSelectionIconClass(platform)).toBe(platformColors.platformSelectionIconClass('unknown'))
  })

  it('keeps the previously colliding platforms in distinct color families', () => {
    // hunyuan/qianfan 曾与 gemini 同为 blue，mistral 与 opencode_go 逐字节相同；
    // 这里同时断言色系名与整组 token 都不同，避免只差一档的“伪区分”。
    const colliding = ['gemini', 'qianfan', 'hunyuan', 'mistral', 'opencode_go']
    const tokenSet = (platform: string) => [
      platformColors.platformBadgeClass, platformColors.platformBadgeLightClass,
      platformColors.platformBorderClass, platformColors.platformBorderStrongClass,
      platformColors.platformAccentColor, platformColors.platformAccentBarClass,
      platformColors.platformTextClass, platformColors.platformIconClass,
      platformColors.platformButtonClass, platformColors.platformDiscountClass,
      platformColors.platformGradientClass, platformColors.platformGradientTextClass,
      platformColors.platformGradientSubtextClass,
    ].map(token => token(platform))
    const colorFamily = (platform: string) => platformColors.platformBadgeClass(platform).match(/text-([a-z]+)-/)?.[1]

    expect(new Set(colliding.map(colorFamily)).size).toBe(colliding.length)
    for (const left of colliding) {
      for (const right of colliding) {
        if (left === right) continue
        expect(tokenSet(left), `${left} vs ${right}`).not.toEqual(tokenSet(right))
      }
    }
    // opencode_go 的账号模式选中卡跟随平台色（曾为 amber，与 mistral 同色）。
    expect(platformColors.platformSelectionClass('opencode_go')).toBe('border-lime-500 bg-lime-50 dark:bg-lime-900/20')
    expect(platformColors.platformSelectionIconClass('opencode_go')).toBe('bg-lime-500 text-white')
  })

  it('resolves a localized name for every platform id in both locales', () => {
    const platformIds = [...CONCRETE_PLATFORM_OPTIONS.map(option => option.value), 'vertex', 'composite']
    for (const [locale, labels] of Object.entries({ en: englishAccounts.accounts.platforms, zh: chineseAccounts.accounts.platforms })) {
      for (const platform of platformIds) {
        const localized = (labels as Record<string, string>)[platform]
        expect(localized, `${locale} locale has no name for ${platform}`).toBeTruthy()
        const translate = (key: string) => (labels as Record<string, string>)[key.replace('admin.accounts.platforms.', '')] ?? key
        const label = getPlatformLabel(platform, translate)
        expect(label, `${locale} renders the raw i18n key for ${platform}`).not.toMatch(/^admin\.accounts\.platforms\./)
        expect(label).toBe(localized)
      }
    }
  })

  it('keeps platform names locale-neutral outside the i18n table', () => {
    // 英文界面出现中文就是这条断言要拦的：静态兜底与 en 词条都不得含中文。
    for (const option of CONCRETE_PLATFORM_OPTIONS) {
      expect(option.label, `${option.value} fallback label is not locale-neutral`).not.toMatch(/[\u4e00-\u9fff]/)
    }
    for (const [platform, label] of Object.entries(englishAccounts.accounts.platforms)) {
      expect(label, `${platform} leaks Chinese into the English locale`).not.toMatch(/[\u4e00-\u9fff]/)
    }
    expect(getPlatformLabel('qianwen')).toBe('Qianwen AI')
  })

  it('assigns every platform entry to exactly one declared selector family', () => {
    const platformIds = [...CONCRETE_PLATFORM_OPTIONS.map(option => option.value), 'vertex']
    const grouped: Record<string, string[]> = { anthropic: [], google: [], openaiCompatible: [] }
    for (const platform of platformIds) grouped[platformFamily(platform)].push(platform)

    expect(grouped.anthropic.sort()).toEqual(['anthropic', 'bedrock'])
    expect(grouped.google.sort()).toEqual(['antigravity', 'gemini', 'vertex'])
    expect(grouped.openaiCompatible).toContain('cursor')
    expect(grouped.anthropic.length + grouped.google.length + grouped.openaiCompatible.length).toBe(platformIds.length)
    for (const family of Object.keys(grouped)) {
      expect(englishAccounts.accounts.platformFamilies[family as keyof typeof englishAccounts.accounts.platformFamilies], `en family label missing for ${family}`).toBeTruthy()
      expect(chineseAccounts.accounts.platformFamilies[family as keyof typeof chineseAccounts.accounts.platformFamilies], `zh family label missing for ${family}`).toBeTruthy()
    }
  })

  it('keeps matching locale keys and scoped supplier display names', () => {
    const english = englishAccounts.accounts.platforms
    const chinese = chineseAccounts.accounts.platforms
    expect(Object.keys(english).sort()).toEqual(Object.keys(chinese).sort())
    expect(chinese).toMatchObject({ qianfan: '百度千帆', hunyuan: '腾讯混元', volcengine: '火山引擎' })
    expect(english).toMatchObject({ qianfan: 'Baidu Qianfan', hunyuan: 'Tencent Hunyuan', volcengine: 'Volcengine Ark' })
    for (const option of CONCRETE_PLATFORM_OPTIONS) {
      for (const labels of [english, chinese]) {
        const translate = (key: string) => labels[key.replace('admin.accounts.platforms.', '') as keyof typeof labels] ?? key
        expect(getPlatformLabel(option.value, translate)).toBe(labels[option.value])
      }
    }
  })
})
