import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n, type LocaleMessages, type VueMessageType } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import enMessages from '@/i18n/locales/en'
import zhMessages from '@/i18n/locales/zh'
import AvailableChannelsView from '../AvailableChannelsView.vue'

function prepareMessages(raw: Record<string, unknown>): LocaleMessages<VueMessageType> {
  return Object.fromEntries(
    Object.entries(raw).map(([key, value]) => [
      key,
      typeof value === 'string'
        ? new Function(`return ${baseCompile(value).code}`)()
        : prepareMessages(value as Record<string, unknown>)
    ])
  )
}

const compiledMessages = {
  en: prepareMessages(enMessages as unknown as Record<string, unknown>),
  zh: prepareMessages(zhMessages as unknown as Record<string, unknown>),
}

const { getAvailableMock, getUserGroupRatesMock, showErrorMock } = vi.hoisted(() => ({
  getAvailableMock: vi.fn(),
  getUserGroupRatesMock: vi.fn(),
  showErrorMock: vi.fn(),
}))

vi.mock('@/api/channels', () => ({
  default: {
    getAvailable: (...args: unknown[]) => getAvailableMock(...args),
  },
}))

vi.mock('@/api/groups', () => ({
  default: {
    getUserGroupRates: (...args: unknown[]) => getUserGroupRatesMock(...args),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
  }),
}))

const wrappers: Array<ReturnType<typeof mount>> = []

beforeEach(() => {
  getAvailableMock.mockReset()
  getUserGroupRatesMock.mockReset()
  showErrorMock.mockReset()
  getAvailableMock.mockResolvedValue([
    {
      id: 1,
      name: 'Primary Channel',
      description: 'Main pool',
      platforms: [
        {
          platform: 'openai',
          groups: [],
          supported_models: [],
        },
      ],
    },
  ])
  getUserGroupRatesMock.mockResolvedValue({ 12: 0.8 })
})

afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
})

describe('AvailableChannelsView localization and shortcuts [P2-1, P3-2]', () => {
  it('renders localized EN stats pills, accessible labels, and focuses search on / keydown', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: compiledMessages,
    })

    const wrapper = mount(AvailableChannelsView, {
      attachTo: document.body,
      global: {
        plugins: [i18n],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
          AvailableChannelsTable: true,
          Icon: true,
        },
      },
    })
    wrappers.push(wrapper)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Available Channels & Rates')
    expect(text).toContain('Matched')
    expect(text).toContain('Platforms')
    expect(text).toContain('Exclusive rates: 1')
    expect(text).not.toMatch(/[\u4e00-\u9fff]/)

    const searchInput = wrapper.get('input[type="text"]')
    expect(searchInput.attributes('aria-label')).toBe(enMessages.availableChannels.searchPlaceholder)

    const refreshBtn = wrapper.findAll('button').find((btn) => btn.attributes('title') === 'Refresh')
    expect(refreshBtn?.attributes('aria-label')).toBe('Refresh')

    document.body.focus()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await flushPromises()

    expect(document.activeElement).toBe(searchInput.element)
  })
})
