import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import AccountTableActions from '@/components/admin/account/AccountTableActions.vue'
import GeneralSettings from '@/views/admin/settings/GeneralSettings.vue'
import BackupView from '@/views/admin/BackupView.vue'
import AuditLogView from '@/views/admin/AuditLogView.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/api', () => ({
  adminAPI: {
    backup: {
      getS3Config: vi.fn().mockResolvedValue({}),
      getImageStorageConfig: vi.fn().mockResolvedValue({}),
      getSchedule: vi.fn().mockResolvedValue({}),
      listRecords: vi.fn().mockResolvedValue({ items: [] })
    }
  },
  totpAPI: {
    getStatus: vi.fn().mockResolvedValue({ enabled: false })
  }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    audit: {
      list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    }
  }
}))

vi.mock('@/views/admin/settings/settingsContext', () => ({
  useSettingsContext: () => ({
    t: (key: string) => key,
    activeTab: 'general',
    form: {
      backend_mode_enabled: false,
      site_name: '',
      site_subtitle: '',
      site_icp: '',
      api_base_url: '',
      table_default_page_size: 20,
      table_page_size_options: '10,20,50',
      custom_menu_items: [],
      compact_home_enabled: false,
      hide_ccs_import_button: false,
      doc_url: ''
    },
    addCustomMenuItem: vi.fn(),
    removeCustomMenuItem: vi.fn()
  })
}))

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({ template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' })

function hasAccessibleLabel(el: Element, root: Element): boolean {
  if (el.getAttribute('aria-label')?.trim()) return true
  if (el.getAttribute('aria-labelledby')?.trim()) return true
  if (el.closest('label')) return true
  const id = el.getAttribute('id')
  if (id && root.querySelector(`label[for="${id}"]`)) return true
  return false
}

describe('Admin accessibility (FL-06)', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('gives the AccountTableActions refresh button an accessible name', () => {
    const wrapper = mount(AccountTableActions, {
      props: { loading: false },
      global: { stubs: { Icon: true } }
    })
    const buttons = wrapper.findAll('button')
    for (const btn of buttons) {
      const name = btn.text().trim() || btn.attributes('aria-label')?.trim() || ''
      expect(name.length).toBeGreaterThan(0)
    }
  })

  it('gives GeneralSettings switches and inputs accessible names', () => {
    const wrapper = mount(GeneralSettings, {
      global: { stubs: { Icon: true, ImageUpload: true } }
    })

    for (const toggle of wrapper.findAll('button[role="switch"]')) {
      expect(toggle.attributes('aria-label')?.trim().length).toBeGreaterThan(0)
    }
    for (const input of wrapper.findAll('input')) {
      expect(hasAccessibleLabel(input.element, wrapper.element)).toBe(true)
    }
  })

  it('links every BackupView form input to a label', async () => {
    const wrapper = mount(BackupView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          ConfirmDialog: true,
          RestoreWorkflow: true,
          Icon: true
        }
      }
    })
    await flushPromises()
    const inputs = wrapper.findAll('input')
    expect(inputs.length).toBeGreaterThanOrEqual(10)
    for (const input of inputs) {
      expect(hasAccessibleLabel(input.element, wrapper.element)).toBe(true)
    }
  })

  it('links every AuditLogView filter input to a label or aria-label', async () => {
    const wrapper = mount(AuditLogView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          TablePageLayout: TablePageLayoutStub,
          DataTable: true,
          Pagination: true,
          BaseDialog: true,
          ConfirmDialog: true,
          Select: true,
          Icon: true
        }
      }
    })
    await flushPromises()
    const inputs = wrapper.findAll('input')
    expect(inputs.length).toBeGreaterThanOrEqual(4)
    for (const input of inputs) {
      expect(hasAccessibleLabel(input.element, wrapper.element)).toBe(true)
    }
  })
})
