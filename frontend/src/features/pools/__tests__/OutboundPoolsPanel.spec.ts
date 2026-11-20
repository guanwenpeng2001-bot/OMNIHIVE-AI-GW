import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import OutboundPoolsPanel from '../OutboundPoolsPanel.vue'
import PoolsWorkbenchView from '../PoolsWorkbenchView.vue'

let currentLocale: 'en' | 'zh' = 'en'

function resolveTranslation(key: string, params?: Record<string, unknown>): string {
  let value: unknown = currentLocale === 'zh' ? zh : en
  for (const part of key.split('.')) {
    if (value && typeof value === 'object' && part in (value as Record<string, unknown>)) {
      value = (value as Record<string, unknown>)[part]
    } else {
      return key
    }
  }
  if (typeof value !== 'string') return key
  if (!params) return value
  return value.replace(/\{(\w+)\}/g, (_, token: string) =>
    params[token] !== undefined ? String(params[token]) : `{${token}}`
  )
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: resolveTranslation,
    locale: { value: currentLocale },
  }),
}))

const mocks = vi.hoisted(() => ({
  listPools: vi.fn(),
  createPool: vi.fn(),
  updatePool: vi.fn(),
  deletePool: vi.fn(),
  routerPush: vi.fn(),
}))

vi.mock('@/features/connections/api', () => ({
  connectionsApi: {
    listPools: mocks.listPools,
    createPool: mocks.createPool,
    updatePool: mocks.updatePool,
    deletePool: mocks.deletePool,
  },
}))

vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRouter: () => ({ push: mocks.routerPush }),
    useRoute: () => ({ path: '/admin/pools', query: {} }),
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    activeScope: { workspaceId: '42', projectId: null },
  }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div data-testid="app-layout"><slot /></div>' },
}))

describe('OutboundPoolsPanel & PoolsWorkbenchView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    currentLocale = 'en'
    mocks.listPools.mockResolvedValue({
      items: [
        { id: 'pool_1', name: 'Primary Pool', max_concurrency: 100, connection_ids: ['c1', 'c2'] },
      ],
    })
    mocks.createPool.mockResolvedValue({ id: 'pool_2', name: 'Burst Pool', max_concurrency: 200 })
    mocks.deletePool.mockResolvedValue(undefined)
  })

  it('renders PoolsWorkbenchView with workspace scope from store and displays loaded pools', async () => {
    currentLocale = 'en'
    const wrapper = mount(PoolsWorkbenchView, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    })
    await flushPromises()

    expect(mocks.listPools).toHaveBeenCalledWith({ limit: 100 }, '42')
    expect(wrapper.get('[data-testid="v2-outbound-pools-panel"]').text()).toContain('Primary Pool')
    expect(wrapper.text()).toContain('Max 100')
    expect(wrapper.text()).toContain('2 connections')
  })

  it('creates a new outbound pool via OutboundPoolsPanel', async () => {
    currentLocale = 'zh'
    const wrapper = mount(OutboundPoolsPanel, {
      props: { workspaceId: '7' },
    })
    await flushPromises()

    const buttons = wrapper.findAll('button')
    const newPoolBtn = buttons.find((b) => b.text().includes('新建资源池'))
    expect(newPoolBtn).toBeDefined()
    await newPoolBtn!.trigger('click')

    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('Burst Pool')
    await inputs[1].setValue('200')

    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()

    expect(mocks.createPool).toHaveBeenCalledWith({ name: 'Burst Pool', max_concurrency: 200 }, '7')
  })
})
