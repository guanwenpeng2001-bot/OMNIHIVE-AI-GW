import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account } from '@/types'
import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const requests = vi.hoisted(() => ({ review: vi.fn(), cancel: vi.fn(), apply: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post: requests.review, delete: requests.cancel } }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { applyOAuthCredentials: requests.apply } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))

function mountReauthorization(platform: 'anthropic' | 'gemini' | 'antigravity' | 'grok') {
  return mount(ReAuthAccountModal, {
    props: { show: true, account: { id: 71, name: 'Existing account', platform, type: 'oauth', credentials: { model_mapping: { public: 'native', selected: 'selected' } } } as Account },
    global: { stubs: {
      BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
      OAuthAuthorizationFlow: true, AccountModelConfigurationSection: true
    } }
  })
}

describe('existing account credential draft review', () => {
  beforeEach(() => {
    requests.review.mockReset().mockResolvedValue({ data: { revision: 'reviewed' } })
    requests.cancel.mockReset().mockResolvedValue({ data: { cancelled: true } })
    requests.apply.mockReset().mockResolvedValue({ id: 71 })
  })

  it.each(['anthropic', 'gemini', 'antigravity', 'grok'] as const)('reviews %s and retains mappings before the sole write', async platform => {
    const wrapper = mountReauthorization(platform)
    const workflow = wrapper.vm as unknown as {
      stageReauthorization: (id: number, payload: { type: 'oauth'; credentials: Record<string, unknown> }) => Promise<never>
      authorizationModels: string[]
    }
    await workflow.stageReauthorization(71, { type: 'oauth', credentials: { credential_draft: 'draft', revision: 'initial', platform, type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } }).catch(() => {})
    await flushPromises()
    expect(requests.apply).not.toHaveBeenCalled()
    workflow.authorizationModels = ['selected', 'additional']
    await wrapper.get('[data-testid="confirm-reauthorization"]').trigger('click')
    await flushPromises()
    expect(requests.review).toHaveBeenCalledWith('/admin/accounts/credential-drafts/draft/review', expect.objectContaining({ name: 'account:71', platform, credential_draft_revision: 'initial' }))
    expect(requests.apply).toHaveBeenCalledOnce()
    expect(requests.apply).toHaveBeenCalledWith(71, expect.objectContaining({ credential_draft: 'draft', credential_draft_revision: 'reviewed', credentials: { model_mapping: { public: 'native', selected: 'selected', additional: 'additional' } } }))
    expect(wrapper.emitted('reauthorized')).toEqual([[{ id: 71 }]])
    wrapper.unmount()
  })

  it('cancels without replacing the saved account credentials', async () => {
    const wrapper = mountReauthorization('anthropic')
    const workflow = wrapper.vm as unknown as { stageReauthorization: (id: number, payload: { type: 'oauth'; credentials: Record<string, unknown> }) => Promise<never>; handleClose: () => void }
    await workflow.stageReauthorization(71, { type: 'oauth', credentials: { credential_draft: 'cancel-me', revision: 'initial', platform: 'anthropic', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } }).catch(() => {})
    workflow.handleClose()
    await flushPromises()
    expect(requests.cancel).toHaveBeenCalledWith('/admin/accounts/credential-drafts/cancel-me')
    expect(requests.apply).not.toHaveBeenCalled()
    expect(wrapper.emitted('reauthorized')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps inherit mode without sending empty model_mapping: {} on re-authorization', async () => {
    const wrapper = mount(ReAuthAccountModal, {
      props: {
        show: true,
        account: {
          id: 72,
          name: 'Inherit account',
          platform: 'anthropic',
          type: 'oauth',
          credentials: {},
          extra: { upstream_model_allowlist: { enabled: false, models: [] } }
        } as Account
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          OAuthAuthorizationFlow: true,
          AccountModelConfigurationSection: true
        }
      }
    })
    const workflow = wrapper.vm as unknown as {
      stageReauthorization: (id: number, payload: { type: 'oauth'; credentials: Record<string, unknown> }) => Promise<never>
      authorizationMode: string
      authorizationModels: string[]
    }
    await workflow.stageReauthorization(72, {
      type: 'oauth',
      credentials: { credential_draft: 'inherit-draft', revision: 'initial', platform: 'anthropic', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' }
    }).catch(() => {})
    await flushPromises()
    expect(workflow.authorizationMode).toBe('inherit')
    expect(workflow.authorizationModels).toEqual([])
    await wrapper.get('[data-testid="confirm-reauthorization"]').trigger('click')
    await flushPromises()
    expect(requests.review).toHaveBeenCalledTimes(1)
    expect(requests.review.mock.calls[0]?.[1]?.credentials).toEqual({})
    expect(requests.review.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
    expect(requests.apply).toHaveBeenCalledWith(
      72,
      expect.objectContaining({
        credential_draft: 'inherit-draft',
        credential_draft_revision: 'reviewed',
        credentials: {}
      })
    )
    expect(requests.apply.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })

  it('hydrates restricted scope from upstream_model_allowlist when model_mapping is absent', async () => {
    const wrapper = mount(ReAuthAccountModal, {
      props: {
        show: true,
        account: {
          id: 73,
          name: 'Allowlist-only account',
          platform: 'anthropic',
          type: 'oauth',
          credentials: {},
          extra: { upstream_model_allowlist: { enabled: true, models: ['vendor/model-a'] } }
        } as Account
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          OAuthAuthorizationFlow: true,
          AccountModelConfigurationSection: true
        }
      }
    })
    const workflow = wrapper.vm as unknown as {
      stageReauthorization: (id: number, payload: { type: 'oauth'; credentials: Record<string, unknown> }) => Promise<never>
      authorizationMode: string
      authorizationModels: string[]
    }
    await workflow.stageReauthorization(73, {
      type: 'oauth',
      credentials: { credential_draft: 'scope-draft', revision: 'initial', platform: 'anthropic', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' }
    }).catch(() => {})
    await flushPromises()
    expect(workflow.authorizationMode).toBe('restricted')
    expect(workflow.authorizationModels).toEqual(['vendor/model-a'])
    // Unchanged restricted selection omits model_mapping so backend preserves existing allowlist.
    await wrapper.get('[data-testid="confirm-reauthorization"]').trigger('click')
    await flushPromises()
    expect(requests.apply.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })
})
