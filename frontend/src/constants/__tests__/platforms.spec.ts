// @vitest-environment node
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { beforeAll, describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS, getPlatformLabel, platformSupportsOAuth } from '@/constants/platforms'

import * as platformColors from '@/utils/platformColors'
import englishAccounts from '@/i18n/locales/en/admin/accounts'
import chineseAccounts from '@/i18n/locales/zh/admin/accounts'

interface BackendPlatform {
  id: string
  oauth: boolean
  activeProbe: boolean
  quotaMonitor: boolean
  passiveMonitor: boolean
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
    ['qianfan', 'blue', '#3b82f6'], ['hunyuan', 'blue', '#3b82f6'],
    ['mistral', 'amber', '#f59e0b'], ['bedrock', 'orange', '#f97316'],
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
