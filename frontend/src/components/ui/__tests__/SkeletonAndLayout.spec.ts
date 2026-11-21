import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import Skeleton from '@/components/ui/Skeleton.vue'
import UserRedeemView from '@/views/user/RedeemView.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN' } })
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ query: {}, params: {} })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ user: { balance: 10 }, refreshUser: vi.fn() }),
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn(), cachedPublicSettings: null })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { balance: 10 }, refreshUser: vi.fn() })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn(), cachedPublicSettings: null })
}))

vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({ fetchActiveSubscriptions: vi.fn() })
}))

vi.mock('@/api', () => ({
  authAPI: {
    getPublicSettings: vi.fn().mockResolvedValue({ contact_info: '' })
  },
  redeemAPI: {
    getHistory: vi.fn().mockResolvedValue({
      items: [
        {
          id: 1,
          code: 'ABCD1234EFGH5678',
          type: 'balance',
          value: 25,
          used_at: '2026-11-21T01:00:00Z'
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    }),
    redeem: vi.fn()
  }
}))

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })

describe('Skeleton and layout fixes (E3-4)', () => {
  it('renders multiple rows for table-row skeleton when lines > 1', () => {
    const wrapper = mount(Skeleton, {
      props: { variant: 'table-row', lines: 4, columns: 4 }
    })
    expect(wrapper.findAll('[data-testid="skeleton-table-row"]').length).toBe(4)
  })

  it('applies explicit height and width on rect skeleton', () => {
    const wrapper = mount(Skeleton, {
      props: { variant: 'rect', height: '420px', width: '100%' }
    })
    const rect = wrapper.get('[data-testid="skeleton-rect"]')
    expect(rect.attributes('style')).toContain('height: 420px')
  })

  it('prevents flex overflow in user RedeemView history rows at narrow viewports', async () => {
    const wrapper = mount(UserRedeemView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          Icon: true
        }
      }
    })
    await flushPromises()
    const amountCol = wrapper.get('div.text-right')
    expect(amountCol.classes()).toContain('shrink-0')
    const row = amountCol.element.parentElement!
    const leftCol = row.firstElementChild!
    expect(leftCol.className).toContain('min-w-0')
    expect(leftCol.className).toContain('flex-1')
  })
})
