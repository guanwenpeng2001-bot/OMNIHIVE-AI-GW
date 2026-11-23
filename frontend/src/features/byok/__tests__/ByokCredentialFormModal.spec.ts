import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import ByokCredentialFormModal from '../components/ByokCredentialFormModal.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('zh-CN'),
    t: (k: string) => k
  })
}))

const baseProps = {
  open: true,
  mode: 'create' as const,
  scopeLevel: 'workspace' as const,
  workspaceId: '1',
  projectId: '10',
  actorUserId: '1',
  availableProjects: [{ id: '10', name: 'Production', state: 'active' as const }],
  projectKeys: [
    {
      id: '100',
      project_id: '10',
      name: 'Prod Key',
      prefix: 'oh_live_100',
      state: 'active' as const,
      created_at: '2026-11-20T00:00:00Z',
      version: '1'
    }
  ],
  saving: false
}

const ModalStub = {
  template: '<div><slot /><slot name="footer" /></div>'
}

describe('ByokCredentialFormModal provider-class policy guard', () => {
  it('shows guidance card and disables submit when providerClasses is empty', () => {
    const wrapper = mount(ByokCredentialFormModal, {
      props: {
        ...baseProps,
        providerClasses: []
      },
      global: {
        stubs: { Modal: ModalStub }
      }
    })

    expect(wrapper.get('[data-testid="byok-policy-disabled-card"]').text()).toContain(
      '当前工作空间尚未启用自带上游凭据 (BYOK) 供应商类别'
    )
    const submitBtn = wrapper.get('[data-testid="byok-submit-btn"]')
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows guidance card and disables submit when all providerClasses have enabled=false', () => {
    const wrapper = mount(ByokCredentialFormModal, {
      props: {
        ...baseProps,
        providerClasses: [{ provider: 'openai', auth_type: 'api_key', supported: true, enabled: false }]
      },
      global: {
        stubs: { Modal: ModalStub }
      }
    })

    const card = wrapper.get('[data-testid="byok-policy-disabled-card"]')
    expect(card.text()).toContain('openai · api_key')
    const submitBtn = wrapper.get('[data-testid="byok-submit-btn"]')
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('enables submit when at least one providerClass is supported and enabled', () => {
    const wrapper = mount(ByokCredentialFormModal, {
      props: {
        ...baseProps,
        providerClasses: [{ provider: 'openai', auth_type: 'api_key', supported: true, enabled: true }]
      },
      global: {
        stubs: { Modal: ModalStub }
      }
    })

    expect(wrapper.find('[data-testid="byok-policy-disabled-card"]').exists()).toBe(false)
    const submitBtn = wrapper.get('[data-testid="byok-submit-btn"]')
    expect((submitBtn.element as HTMLButtonElement).disabled).toBe(false)
  })
})
