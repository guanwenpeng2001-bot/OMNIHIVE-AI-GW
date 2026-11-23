import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  getEffectiveConfig,
  resetConfigOverrides,
  sanitizeEffectiveConfigItem,
  type EffectiveConfigItem
} from '@/api/admin/config'
import { useStepUp } from '@/composables/useStepUp'
import {
  formatBaselinePreviewToken,
  formatDiagnosticValueToken,
  resolveEffectiveSeedSource,
  resolveResetPreviewItems,
  useConfigDiagnostics
} from '../useConfigDiagnostics'

const { mockApiGet, mockApiPost } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockApiGet,
    post: mockApiPost
  }
}))

function makeItem(overrides: Partial<EffectiveConfigItem> = {}): EffectiveConfigItem {
  return {
    key: 'model_plaza_enabled',
    group: 'plaza',
    value_type: 'bool',
    value: false,
    source: 'database',
    authority: 'database_only',
    fallback_chain: ['database', 'default'],
    restart_required: false,
    resettable: false,
    sensitive: false,
    state: 'effective',
    configured: null,
    ...overrides
  }
}

describe('config API & useConfigDiagnostics', () => {
  beforeEach(() => {
    mockApiGet.mockReset()
    mockApiPost.mockReset()
  })

  it('strips value and baseline from sensitive items as defense-in-depth while preserving explicit false/0/empty', () => {
    const leakedSecret = sanitizeEffectiveConfigItem(
      makeItem({
        key: 'turnstile_secret_key',
        group: 'security',
        value_type: 'string',
        value: 'SUPER_SECRET_PLAINTEXT',
        baseline: 'SUPER_SECRET_BASELINE',
        sensitive: true,
        configured: true
      })
    )
    expect(leakedSecret.value).toBeNull()
    expect(leakedSecret.baseline).toBeUndefined()
    expect(leakedSecret.configured).toBe(true)
    expect(formatDiagnosticValueToken(leakedSecret)).toEqual({
      kind: 'secret',
      configured: true
    })
    expect(formatBaselinePreviewToken(leakedSecret)).toEqual({
      kind: 'secret',
      configured: null
    })

    expect(formatDiagnosticValueToken(makeItem({ value: 0, value_type: 'int' }))).toEqual({
      kind: 'mono',
      text: '0'
    })
    expect(formatDiagnosticValueToken(makeItem({ value: false, value_type: 'bool' }))).toEqual({
      kind: 'bool',
      text: 'false'
    })
    expect(formatDiagnosticValueToken(makeItem({ value: '', value_type: 'string' }))).toEqual({
      kind: 'empty-string'
    })
  })

  it('passes next_cursor verbatim and clamps limit when querying GET /admin/config/effective', async () => {
    mockApiGet.mockResolvedValue({
      data: {
        config_file: '/etc/omnihive/config.yaml',
        generated_at: '2026-11-23T01:00:00Z',
        items: [makeItem()],
        next_cursor: 'ops.enabled'
      }
    })

    const res = await getEffectiveConfig({
      group: 'security',
      q: 'turnstile',
      cursor: '  exact_cursor_key  ',
      limit: 500,
      source: 'database',
      state: 'blocked_by_hard_gate'
    })

    expect(mockApiGet).toHaveBeenCalledWith('/admin/config/effective', {
      params: {
        group: 'security',
        q: 'turnstile',
        cursor: '  exact_cursor_key  ',
        limit: 200,
        source: 'database',
        state: 'blocked_by_hard_gate'
      }
    })
    expect(res.next_cursor).toBe('ops.enabled')
  })

  it('deduplicates keys on POST /admin/config/reset and sanitizes returned items', async () => {
    mockApiPost.mockResolvedValue({
      data: {
        settings: { site_name: 'OmniHive' },
        generated_at: '2026-11-23T01:05:00Z',
        items: [
          makeItem({
            key: 'linuxdo_connect_client_secret',
            group: 'linuxdo_connect',
            sensitive: true,
            configured: false,
            value: 'SHOULD_NEVER_SURVIVE',
            baseline: 'SHOULD_NEVER_SURVIVE'
          })
        ]
      }
    })

    const res = await resetConfigOverrides([
      ' linuxdo_connect_enabled ',
      'linuxdo_connect_enabled',
      ''
    ])
    expect(mockApiPost).toHaveBeenCalledWith('/admin/config/reset', {
      keys: ['linuxdo_connect_enabled']
    })
    expect(res.items[0].value).toBeNull()
    expect(res.items[0].baseline).toBeUndefined()
  })

  it('resolves seed provenance from companion *_install_seed_source rows and expands atomic reset groups', () => {
    const balance = makeItem({
      key: 'default_balance',
      group: 'onboarding',
      value_type: 'float',
      value: 0,
      source: 'database',
      install_seed_source: 'unknown'
    })
    const seedRow = makeItem({
      key: 'default_balance_install_seed_source',
      group: 'onboarding',
      value_type: 'string',
      value: null,
      source: 'database',
      install_seed_source: 'environment'
    })
    const byKey = new Map<string, EffectiveConfigItem>([
      [balance.key, balance],
      [seedRow.key, seedRow]
    ])
    expect(resolveEffectiveSeedSource(balance, byKey)).toBe('environment')
    expect(balance.source).toBe('database')

    const oauthEnabled = makeItem({
      key: 'linuxdo_connect_enabled',
      group: 'linuxdo_connect',
      authority: 'database_override',
      resettable: true,
      value: true,
      baseline: false
    })
    const oauthSecret = makeItem({
      key: 'linuxdo_connect_client_secret',
      group: 'linuxdo_connect',
      authority: 'database_override',
      resettable: true,
      sensitive: true,
      configured: true
    })
    const opsJson = makeItem({
      key: 'ops_advanced_settings',
      group: 'ops',
      authority: 'database_override',
      resettable: true,
      value_type: 'object',
      value: { retention_days: 30 }
    })

    const groupPreview = resolveResetPreviewItems(oauthEnabled, [oauthEnabled, oauthSecret, opsJson])
    expect(groupPreview.map((item) => item.key)).toEqual([
      'linuxdo_connect_client_secret',
      'linuxdo_connect_enabled'
    ])
    expect(resolveResetPreviewItems(opsJson, [oauthEnabled, oauthSecret, opsJson]).map((i) => i.key)).toEqual([
      'ops_advanced_settings'
    ])
  })

  it('never marks syncState as synced on initial load failure or refresh failure, and supports step-up reset retry', async () => {
    mockApiGet.mockRejectedValueOnce({
      status: 503,
      reason: 'SERVICE_UNAVAILABLE',
      message: 'Database unreachable',
      metadata: { request_id: 'req-init-503' }
    })

    const stepUp = useStepUp()
    const onSettingsUpdated = vi.fn()
    const diag = useConfigDiagnostics({ stepUp, onSettingsUpdated })

    await diag.loadFirstPage()
    expect(diag.syncState.value).toBe('error')
    expect(diag.hasLoadedOnce.value).toBe(false)
    expect(diag.loadError.value?.requestId).toBe('req-init-503')
    expect(diag.items.value).toEqual([])

    // Succeeds on retry
    const resettableItem = makeItem({
      key: 'linuxdo_connect_enabled',
      group: 'linuxdo_connect',
      authority: 'database_override',
      source: 'database',
      resettable: true,
      value: true,
      baseline: false
    })
    mockApiGet.mockResolvedValueOnce({
      data: {
        config_file: '/app/data/config.yaml',
        generated_at: '2026-11-23T02:00:00Z',
        items: [resettableItem],
        next_cursor: ''
      }
    })
    await diag.loadFirstPage()
    expect(diag.syncState.value).toBe('synced')
    expect(diag.items.value).toHaveLength(1)

    // Refresh fails -> transitions to stale rather than synced
    mockApiGet.mockRejectedValueOnce({
      status: 500,
      message: 'Transient failure'
    })
    await diag.loadFirstPage(true)
    expect(diag.syncState.value).toBe('stale')
    expect(diag.refreshError.value?.message).toBe('Transient failure')

    // Step-up required during reset -> prompts -> retries -> updates item and calls onSettingsUpdated
    diag.openResetConfirm(resettableItem)
    mockApiPost
      .mockRejectedValueOnce({ status: 403, reason: 'STEP_UP_REQUIRED', message: 'Step-up required' })
      .mockResolvedValueOnce({
        data: {
          settings: { site_name: 'OmniHive After Reset' },
          generated_at: '2026-11-23T02:10:00Z',
          items: [
            makeItem({
              key: 'linuxdo_connect_enabled',
              group: 'linuxdo_connect',
              authority: 'database_override',
              source: 'yaml',
              resettable: true,
              value: false,
              baseline: false
            })
          ]
        }
      })

    const resetPromise = diag.confirmReset()
    await vi.waitFor(() => expect(stepUp.visible.value).toBe(true))
    stepUp.onVerified()
    const resetRes = await resetPromise

    expect(resetRes).not.toBeNull()
    expect(mockApiPost).toHaveBeenCalledTimes(2)
    expect(diag.items.value[0].source).toBe('yaml')
    expect(diag.items.value[0].value).toBe(false)
    expect(diag.syncState.value).toBe('synced')
    expect(onSettingsUpdated).toHaveBeenCalledWith({ site_name: 'OmniHive After Reset' })
  })
})
