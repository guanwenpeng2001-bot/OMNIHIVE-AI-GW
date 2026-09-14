// @vitest-environment node
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { beforeAll, describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS, platformSupportsOAuth } from '@/constants/platforms'

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
