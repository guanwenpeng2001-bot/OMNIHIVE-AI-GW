import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelsCatalogView from '../ModelsCatalogView.vue'
import {
  buildAdminProvenanceMatrix,
  isAdminCatalogSnapshotExpired,
  normalizeAdminCatalogDetail,
  normalizeAdminCatalogModelPage
} from '@/api/admin/modelCatalog'

const {
  mockGet,
  mockPost,
  mockPut,
  mockLocaleHolder,
  mockRouteQuery,
  mockAppStore
} = vi.hoisted(() => ({
  mockGet: vi.fn(),
  mockPost: vi.fn(),
  mockPut: vi.fn(),
  mockLocaleHolder: {
    locale: { value: 'zh-CN' }
  },
  mockRouteQuery: {
    workspace_id: '7'
  } as Record<string, string>,
  mockAppStore: {
    opsMonitoringEnabled: false,
    activeScope: { mode: 'workspace', workspaceId: '7', projectId: null },
    scopes: [{ workspace_id: '7', name: 'Workspace 7', is_default: true }]
  }
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get: mockGet,
    post: mockPost,
    put: mockPut
  },
  isSilentRequestError: () => false,
  buildGatewayUrl: (path: string) => path,
  buildApiUrl: (path: string) => path
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: mockLocaleHolder.locale
    })
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({
    path: '/admin/models',
    query: mockRouteQuery
  }),
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => mockAppStore
}))

const SAMPLE_LIST_PAYLOAD = {
  code: 0,
  message: 'success',
  data: {
    catalog_version:
      'cat_1111111111111111111111111111111111111111111111111111111111111111',
    as_of: '2026-11-23T06:00:00Z',
    quick_filter_counts: { routable_but_unpriced: 6, discovered_not_enabled: 13, pi_only: 21, stale: 0 },
    scope_version: '17',
    items: [
      {
        id: 'm_f561620eb6d43d1738e5c6b71ae0e1f27',
        canonical_id: 'm_f561620eb6d43d1738e5c6b71ae0e1f27',
        model: 'gpt-4.1',
        display_name: 'GPT-4.1',
        provider: 'openai',
        source: {
          kinds: ['account_discovery', 'builtin_reviewed', 'pi_ai_reference'],
          candidate: true,
          discovered: true
        },
        availability: {
          state: 'live',
          publication: 'published',
          access: 'allowed'
        },
        billing: {
          state: 'unpriced',
          sale_price: null,
          reference_price: {
            input_rate: {
              currency: 'USD',
              amount: '0.000002',
              unit: 'input_token',
              per: '1'
            },
            output_rate: {
              currency: 'USD',
              amount: '0.000008',
              unit: 'output_token',
              per: '1'
            },
            additional_rates: [],
            time_bands: [],
            state: 'reference',
            source: 'pi-ai'
          }
        },
        aliases_count: 1,
        accounts_count: 2,
        discovered_accounts_count: 3,
        groups_count: 2,
        channels_count: 1
      },
      {
        id: 'm_aaaa620eb6d43d1738e5c6b71ae0e1f27',
        canonical_id: 'm_aaaa620eb6d43d1738e5c6b71ae0e1f27',
        model: 'claude-4-opus',
        display_name: 'Claude 4 Opus',
        provider: 'anthropic',
        source: {
          kinds: ['builtin_reviewed', 'builtin_supplement', 'pi_ai_reference', 'account_discovery'],
          candidate: true,
          discovered: true
        },
        availability: {
          state: 'live',
          publication: 'published',
          access: 'allowed'
        },
        billing: {
          state: 'conflict',
          sale_price: {
            input_rate: {
              currency: 'USD',
              amount: '15',
              unit: 'input_token',
              per: '1000000'
            },
            output_rate: {
              currency: 'USD',
              amount: '75',
              unit: 'output_token',
              per: '1000000'
            },
            additional_rates: [],
            time_bands: [],
            source: 'channel',
            scope: 'authorized',
            price_version:
              'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
            valid_from: null,
            valid_until: null
          },
          reference_price: {
            input_rate: {
              currency: 'USD',
              amount: '0.000015',
              unit: 'input_token',
              per: '1'
            },
            output_rate: {
              currency: 'USD',
              amount: '0.000075',
              unit: 'output_token',
              per: '1'
            },
            additional_rates: [],
            time_bands: [],
            state: 'reference',
            source: 'pi-ai'
          }
        },
        aliases_count: 2,
        accounts_count: 2,
        discovered_accounts_count: 2,
        groups_count: 3,
        channels_count: 2,
        has_conflict: true
      },
      {
        id: 'm_bbbb620eb6d43d1738e5c6b71ae0e1f27',
        canonical_id: 'm_bbbb620eb6d43d1738e5c6b71ae0e1f27',
        model: 'pi-community-only',
        display_name: 'Pi Community Reference',
        provider: 'community',
        source: {
          kinds: ['pi_ai_reference'],
          candidate: true,
          discovered: false
        },
        availability: {
          state: 'catalog_only',
          publication: 'unpublished',
          access: 'allowed'
        },
        billing: {
          state: 'unpriced',
          sale_price: null,
          reference_price: {
            input_rate: {
              currency: 'USD',
              amount: '1',
              unit: 'input_token',
              per: '1000000'
            },
            output_rate: {
              currency: 'USD',
              amount: '3',
              unit: 'output_token',
              per: '1000000'
            },
            additional_rates: [],
            time_bands: [],
            state: 'reference',
            source: 'pi-ai'
          }
        },
        aliases_count: 0,
        accounts_count: 0,
        discovered_accounts_count: 0,
        groups_count: 0,
        channels_count: 0
      }
    ],
    next_cursor: null
  }
}

const SAMPLE_DETAIL_PAYLOAD = {
  code: 0,
  message: 'success',
  data: {
    catalog_version:
      'cat_1111111111111111111111111111111111111111111111111111111111111111',
    as_of: '2026-11-23T06:00:00Z',
    scope_version: '17',
    model: SAMPLE_LIST_PAYLOAD.data.items[1],
    aliases: ['claude-opus-latest'],
    common_facts: {
      context_tokens: '200000',
      context_kind: 'input',
      max_output_tokens: '32768',
      embedding_dimensions: null,
      tools: null,
      vision: false,
      reasoning: null,
      prompt_caching: null,
      pdf: null,
      modalities: ['text']
    },
    provenance: [
      {
        field: 'context_window',
        state: 'known',
        source_kind: 'builtin_reviewed',
        value: { tokens: '200000', basis: 'input' }
      },
      {
        field: 'max_output_tokens',
        state: 'known',
        source_kind: 'builtin_reviewed',
        value: '32768'
      },
      {
        field: 'max_output_tokens',
        state: 'conflict',
        source_kind: 'builtin_supplement',
        value: '64000'
      },
      {
        field: 'max_output_tokens',
        state: 'known',
        source_kind: 'pi_ai_reference',
        value: '16384'
      },
      {
        field: 'supports_vision',
        state: 'known',
        source_kind: 'builtin_reviewed',
        value: false
      },
      {
        field: 'reference_price',
        state: 'known',
        source_kind: 'pi_ai_reference'
      }
    ],
    accounts: [
      {
        account_id: '23',
        label: 'Observed account',
        account_type: 'apikey',
        discovery_state: 'current',
        membership_state: 'eligible',
        observed_at: '2026-11-23T05:45:00Z',
        identity_revision: '1',
        snapshot_revision: '4',
        rules_revision: '20'
      }
    ],
    offers: [
      {
        group_id: '8',
        channel_id: '3',
        account_id: '23',
        action: 'text.chat',
        protocol: 'openai_responses',
        eligible: true,
        price_context: 'group_default',
        billing: {
          state: 'priced',
          sale_price: {
            input_rate: {
              currency: 'USD',
              amount: '15',
              unit: 'input_token',
              per: '1000000'
            },
            output_rate: {
              currency: 'USD',
              amount: '75',
              unit: 'output_token',
              per: '1000000'
            },
            additional_rates: [],
            time_bands: [],
            source: 'channel',
            scope: 'authorized',
            price_version:
              'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
            valid_from: null,
            valid_until: null
          }
        }
      },
      {
        group_id: '9',
        channel_id: '0',
        account_id: null,
        action: 'text.chat',
        protocol: 'anthropic_messages',
        eligible: false,
        reason: 'no_eligible_binding',
        price_context: 'group_default',
        billing: {
          state: 'unpriced'
        }
      }
    ],
    publications: [
      {
        group_id: '8',
        channel_id: '3',
        state: 'published',
        basis: 'explicit',
        reason: 'published'
      },
      {
        group_id: '8',
        channel_id: '3',
        state: 'published',
        basis: 'explicit',
        reason: 'published'
      }
    ],
    next_accounts_cursor: 'acc_cur_2',
    next_offers_cursor: 'off_cur_2'
  }
}

function mountCatalogView() {
  return mount(ModelsCatalogView, {
    global: {
      stubs: {
        AppLayout: { template: '<div data-testid="app-layout"><slot /></div>' }
      }
    }
  })
}

describe('ModelsCatalogView (/admin/models) & inspector contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockLocaleHolder.locale.value = 'zh-CN'
    mockRouteQuery.workspace_id = '7'
    mockAppStore.opsMonitoringEnabled = false
    mockGet.mockImplementation(async (url: string) => {
      if (url === '/api/v2/admin/catalog/models') {
        return { data: SAMPLE_LIST_PAYLOAD }
      }
      if (url.startsWith('/api/v2/admin/catalog/models/')) {
        return { data: SAMPLE_DETAIL_PAYLOAD }
      }
      return { data: {} }
    })
  })

  it('normalizes missing fields as unknown and never fabricates 0 or false', () => {
    const page = normalizeAdminCatalogModelPage({
      items: [{ id: 'm_sparse' }]
    })
    expect(page.catalog_version).toBe('unknown')
    expect(page.items[0].availability.state).toBe('unknown')
    expect(page.items[0].billing.state).toBe('unknown')
    expect(page.items[0].accounts_count).toBeNull()

    const detail = normalizeAdminCatalogDetail({
      model: { id: 'm_sparse' },
      common_facts: { vision: false, tools: null },
      provenance: []
    })
    expect(detail.common_facts.tools).toBeNull()
    expect(detail.common_facts.vision).toBe(false)
    expect(detail.common_facts.context_tokens).toBeNull()

    const matrix = buildAdminProvenanceMatrix(detail.common_facts, detail.provenance)
    const toolsRow = matrix.find((r) => r.field === 'tools')!
    const visionRow = matrix.find((r) => r.field === 'supports_vision')!
    expect(toolsRow.effectiveValue).toBe('—')
    expect(visionRow.effectiveValue).toBe('false')
  })

  it('renders 4-stage funnel and 4 price layers, isolates reference_price, and filters routable_but_unpriced / discovered_not_enabled / pi_only', async () => {
    const wrapper = mountCatalogView()
    await flushPromises()

    expect(mockGet).toHaveBeenCalledWith(
      '/api/v2/admin/catalog/models',
      expect.objectContaining({
        params: expect.objectContaining({
          workspace_id: '7',
          limit: 50
        })
      })
    )

    // Canonical 4-stage funnel & 4 price layers legends
    expect(wrapper.get('[data-testid="admin-models-funnel-legend"]').text()).toContain(
      '1. 平台候选 -> 2. 账号原生发现 -> 3. 账号有效模型 -> 4. 分组对外可见'
    )
    expect(wrapper.get('[data-testid="admin-models-price-layer-legend"]').text()).toContain(
      '1. 参考价（非结算价） / 2. 销售价 / 3. 内部成本 / 4. 实收结算价'
    )

    // All 3 rows visible initially
    const rows = wrapper.findAll('[data-testid="admin-model-row"]')
    expect(rows).toHaveLength(3)

    // P0 badge on gpt-4.1 (live + unpriced, even though reference_price exists)
    expect(rows[0].find('[data-testid="row-p0-unpriced-badge"]').exists()).toBe(true)
    expect(rows[0].get('[data-testid="row-reference-price"]').text()).toContain(
      '参考价（非结算价）：$2 / $8 · 社区目录（pi）'
    )
    // Zero raw "pi-ai" in user-facing UI
    expect(wrapper.text()).not.toContain('pi-ai')

    const { items, quick_filter_counts } = SAMPLE_LIST_PAYLOAD.data
    const respondWith = (rows: typeof items) =>
      mockGet.mockResolvedValueOnce({ data: { ...SAMPLE_LIST_PAYLOAD.data, items: rows } })
    expect(wrapper.get('[data-testid="quick-filter-pi_only"] span:last-child').text()).toBe(String(quick_filter_counts.pi_only))
    respondWith([items[0]])
    // Filter: routable_but_unpriced (P0)
    await wrapper.get('[data-testid="quick-filter-routable_but_unpriced"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenLastCalledWith(
      '/api/v2/admin/catalog/models',
      expect.objectContaining({
        params: expect.objectContaining({
          workspace_id: '7',
          quick_filter: 'routable_but_unpriced'
        })
      })
    )
    expect(wrapper.findAll('[data-testid="admin-model-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="admin-model-row"]').text()).toContain('openai/gpt-4.1')

    respondWith([items[0]])
    // Filter: discovered_not_enabled (discovered_accounts_count 3 > accounts_count 2)
    await wrapper.get('[data-testid="quick-filter-discovered_not_enabled"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="admin-model-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="admin-model-row"]').text()).toContain('openai/gpt-4.1')

    respondWith([items[2]])
    // Filter: pi_only
    await wrapper.get('[data-testid="quick-filter-pi_only"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="admin-model-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="admin-model-row"]').text()).toContain(
      'community/pi-community-only'
    )

    respondWith([])
    await wrapper.get('[data-testid="quick-filter-stale"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="admin-model-row"]')).toHaveLength(0)
    expect(wrapper.get('[data-testid="quick-filter-discovered_not_enabled"] span:last-child').text()).toBe('13')
    expect(wrapper.get('[data-testid="quick-filter-stale"] span:last-child').text()).toBe('0')
  })

  it('opens detail drawer with provenance conflict highlight in amber, 4-stage funnel, 4-layer price waterfall, and nested pagination', async () => {
    const wrapper = mountCatalogView()
    await flushPromises()

    // Click row 2 (claude-4-opus)
    const rows = wrapper.findAll('[data-testid="admin-model-row"]')
    await rows[1].trigger('click')
    await flushPromises()

    expect(mockGet).toHaveBeenCalledWith(
      '/api/v2/admin/catalog/models/m_aaaa620eb6d43d1738e5c6b71ae0e1f27',
      expect.objectContaining({
        params: expect.objectContaining({
          workspace_id: '7',
          catalog_version:
            'cat_1111111111111111111111111111111111111111111111111111111111111111'
        })
      })
    )

    const drawer = wrapper.get('[data-testid="admin-model-detail-drawer"]')
    expect(drawer.exists()).toBe(true)

    // Provenance conflict highlighted on max_output_tokens (32768 vs 64000 vs 16384)
    const maxOutRow = drawer.get('[data-provenance-field="max_output_tokens"]')
    expect(maxOutRow.attributes('data-provenance-conflict')).toBe('true')
    expect(maxOutRow.find('[data-testid="provenance-conflict-badge"]').exists()).toBe(true)
    expect(maxOutRow.text()).toContain('32768')
    expect(maxOutRow.text()).toContain('64000')
    expect(maxOutRow.text()).toContain('16384')

    // 4-Stage funnel stages & deduplicated publications + channel #0 / null account handling
    expect(drawer.get('[data-testid="funnel-stage-1-candidates"]').text()).toContain('1. 平台候选')
    expect(drawer.get('[data-testid="funnel-stage-2-discovered"]').text()).toContain('2. 账号原生发现')
    expect(drawer.get('[data-testid="funnel-stage-3-effective"]').text()).toContain('3. 账号有效模型')
    expect(drawer.get('[data-testid="funnel-stage-3-effective"]').text()).toContain(
      '仅分组证据 (channel #0)'
    )
    expect(drawer.get('[data-testid="funnel-stage-3-effective"]').text()).toContain(
      '无具体连接绑定 (group/channel 级)'
    )
    expect(drawer.get('[data-testid="funnel-stage-4-visible"]').text()).toContain('4. 分组对外可见')
    expect(drawer.findAll('[data-testid="funnel-publication-row"]')).toHaveLength(1)

    // 4-Layer Price Waterfall
    expect(drawer.get('[data-testid="price-layer-1-reference"]').text()).toContain(
      '1. 参考价（非结算价）'
    )
    expect(drawer.get('[data-testid="price-layer-2-sale"]').text()).toContain('2. 销售价')
    expect(drawer.get('[data-testid="price-layer-3-internal-cost"]').text()).toContain('3. 内部成本')
    expect(drawer.get('[data-testid="price-layer-4-settlement"]').text()).toContain('4. 实收结算价')

    // Nested pagination for accounts
    await drawer.get('[data-testid="drawer-load-more-accounts"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenLastCalledWith(
      '/api/v2/admin/catalog/models/m_aaaa620eb6d43d1738e5c6b71ae0e1f27',
      expect.objectContaining({
        params: expect.objectContaining({
          workspace_id: '7',
          accounts_cursor: 'acc_cur_2'
        })
      })
    )
  })

  it('offers a local CSV export and no batch mutations on the read-only inspector', async () => {
    const wrapper = mountCatalogView()
    await flushPromises()
    const links = wrapper.get('[data-testid="catalog-read-only-note"]').findAll('a')
    expect(links.map(link => link.attributes('href'))).toEqual(['/admin/accounts', '/admin/channels/pricing'])
    expect(wrapper.get('[data-testid="catalog-version-pill"]').classes()).toContain('break-all')
    expect(wrapper.get('[data-testid="admin-models-table"]').element.parentElement?.classList.contains('overflow-x-auto')).toBe(true)
    expect(wrapper.find('[data-testid="open-batch-preview-modal"]').exists()).toBe(false)
    await wrapper.get('[data-testid="export-unpriced-csv"]').trigger('click')
    expect(wrapper.get('[data-testid="exported-csv-preview"]').text()).toContain('gpt-4.1')
    expect(mockPut).not.toHaveBeenCalled()
    expect(mockPost).not.toHaveBeenCalled()
  })

  it('makes CatalogVerificationWorkbench reachable in the page drawer even when opsMonitoringEnabled is false', async () => {
    mockAppStore.opsMonitoringEnabled = false
    const wrapper = mountCatalogView()
    await flushPromises()

    const first = SAMPLE_LIST_PAYLOAD.data.items[0]
    mockGet.mockResolvedValueOnce({ data: { ...SAMPLE_DETAIL_PAYLOAD.data, model: first } })
    await wrapper.get('[data-testid="open-catalog-verification-workbench"]').trigger('click')
    await flushPromises()
    expect(mockGet).toHaveBeenLastCalledWith(`/api/v2/admin/catalog/models/${first.id}`, expect.anything())
    expect(wrapper.get('[data-testid="admin-model-detail-drawer"] header').text()).toContain(first.display_name)
    await flushPromises()

    const drawer = wrapper.get('[data-testid="admin-model-detail-drawer"]')
    const verification = drawer.get('[data-testid="catalog-verification-workbench"]')
    expect(verification.findAll('form')).toHaveLength(1)
    expect(verification.find('#catalog-target-model').exists()).toBe(false)
  })

  it('recovers from 410 SNAPSHOT_EXPIRED by prompting a fresh snapshot load', async () => {
    mockGet.mockRejectedValueOnce({
      status: 410,
      reason: 'SNAPSHOT_EXPIRED',
      message: 'Refresh the model catalog and try again.'
    })
    const wrapper = mountCatalogView()
    await flushPromises()

    expect(wrapper.find('[data-testid="list-snapshot-expired"]').exists()).toBe(true)
    await wrapper.get('[data-testid="list-expired-refresh-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="list-snapshot-expired"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="admin-model-row"]')).toHaveLength(3)
  })

  it('normalizes catalog API responses and tolerates null common_facts as unknown', () => {
    const be9DetailPayload = {
      code: 0,
      message: 'success',
      data: {
        catalog_version: 'cat_1111111111111111111111111111111111111111111111111111111111111111',
        as_of: '2026-11-23T06:00:00Z',
        scope_version: '17',
        model: {
          id: 'm_f561620eb6d43d1738e5c6b71ae0e1f27',
          canonical_id: 'm_f561620eb6d43d1738e5c6b71ae0e1f27',
          model: 'demo',
          display_name: 'Demo',
          provider: 'openai',
          source: {
            kinds: ['account_discovery', 'builtin_reviewed', 'pi_ai_reference'],
            candidate: true,
            discovered: true
          },
          availability: { state: 'live', publication: 'published', access: 'allowed' },
          billing: {
            state: 'priced',
            sale_price: {
              input_rate: { currency: 'USD', amount: '2', unit: 'input_token', per: '1000000' },
              output_rate: { currency: 'USD', amount: '8', unit: 'output_token', per: '1000000' },
              additional_rates: [],
              time_bands: [],
              source: 'channel',
              scope: 'authorized',
              price_version:
                'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              valid_from: null,
              valid_until: null
            },
            reference_price: {
              input_rate: { currency: 'USD', amount: '0.000003', unit: 'input_token', per: '1' },
              output_rate: { currency: 'USD', amount: '0.000012', unit: 'output_token', per: '1' },
              additional_rates: [],
              time_bands: [],
              state: 'reference',
              source: 'pi-ai'
            }
          },
          aliases_count: 1,
          accounts_count: 1,
          discovered_accounts_count: 1,
          groups_count: 1,
          channels_count: 1
        },
        aliases: ['public-alias'],
        common_facts: {
          context_tokens: '200000',
          context_kind: 'input',
          max_output_tokens: '32768',
          embedding_dimensions: null,
          tools: null,
          vision: false,
          reasoning: null,
          prompt_caching: null,
          pdf: null,
          modalities: ['text']
        },
        provenance: [
          {
            field: 'context_window',
            state: 'known',
            source_kind: 'builtin_reviewed',
            value: { tokens: '200000', basis: 'input' }
          },
          {
            field: 'max_output_tokens',
            state: 'known',
            source_kind: 'account_discovery',
            value: '32768'
          },
          {
            field: 'supports_vision',
            state: 'known',
            source_kind: 'builtin_reviewed',
            value: false
          },
          {
            field: 'reference_price',
            state: 'known',
            source_kind: 'pi_ai_reference'
          }
        ],
        accounts: [
          {
            account_id: '23',
            label: 'Observed account',
            account_type: 'apikey',
            discovery_state: 'current',
            membership_state: 'eligible',
            observed_at: '2026-11-23T05:45:00Z',
            identity_revision: '1',
            snapshot_revision: '4',
            rules_revision: '20'
          }
        ],
        offers: [
          {
            group_id: '8',
            channel_id: '3',
            account_id: '23',
            action: 'text.chat',
            protocol: 'openai_responses',
            eligible: true,
            price_context: 'group_default',
            billing: {
              state: 'priced',
              sale_price: {
                input_rate: { currency: 'USD', amount: '2', unit: 'input_token', per: '1000000' },
                output_rate: { currency: 'USD', amount: '8', unit: 'output_token', per: '1000000' },
                additional_rates: [],
                time_bands: [],
                source: 'channel',
                scope: 'authorized',
                price_version:
                  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
                valid_from: null,
                valid_until: null
              }
            }
          }
        ],
        publications: [
          {
            group_id: '8',
            channel_id: '3',
            state: 'published',
            basis: 'explicit',
            reason: 'published'
          }
        ],
        next_accounts_cursor: null,
        next_offers_cursor: null
      }
    }

    const normalized = normalizeAdminCatalogDetail(be9DetailPayload)
    expect(normalized.model.id).toBe('m_f561620eb6d43d1738e5c6b71ae0e1f27')
    expect(normalized.common_facts.vision).toBe(false)
    expect(normalized.common_facts.tools).toBeNull()

    const matrix = buildAdminProvenanceMatrix(
      normalized.common_facts,
      normalized.provenance,
      normalized.model.billing
    )
    const visionRow = matrix.find((r) => r.field === 'supports_vision')
    expect(visionRow?.approvedValue).toBe('false')
    expect(visionRow?.effectiveValue).toBe('false')
    const refPriceRow = matrix.find((r) => r.field === 'reference_price')
    expect(refPriceRow?.piValue).toBe('$3 / $12')
    expect(refPriceRow?.effectiveValue).toBe('$3 / $12')

    // Null common_facts tolerance
    const nullFactsDetail = normalizeAdminCatalogDetail({
      ...be9DetailPayload.data,
      common_facts: null
    })
    expect(nullFactsDetail.common_facts.context_tokens).toBeNull()

    // Expired cursor envelope
    expect(
      isAdminCatalogSnapshotExpired({
        code: 410,
        message: 'Refresh the model catalog and try again.',
        reason: 'SNAPSHOT_EXPIRED',
        data: null
      })
    ).toBe(true)
  })
})
