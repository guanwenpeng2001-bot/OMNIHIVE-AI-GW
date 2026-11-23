import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import type { Account } from '@/types'

const {
  updateAccountMock,
  getAccountByIdMock,
  checkMixedChannelRiskMock,
  getModelCatalogMock,
  getAvailableModelsMock,
  updateModelAllowlistMock,
  syncUpstreamModelsMock,
  apiPostMock,
  apiDeleteMock,
  batchCreateMock,
  showErrorMock,
  showSuccessMock,
  showInfoMock,
  showWarningMock,
  authIsSimpleMode
} = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  getAccountByIdMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  getModelCatalogMock: vi.fn(),
  getAvailableModelsMock: vi.fn(),
  updateModelAllowlistMock: vi.fn(),
  syncUpstreamModelsMock: vi.fn(),
  apiPostMock: vi.fn(),
  apiDeleteMock: vi.fn(),
  batchCreateMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showInfoMock: vi.fn(),
  showWarningMock: vi.fn(),
  authIsSimpleMode: { value: true }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}${JSON.stringify(params)}` : key
  })
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock,
    showInfo: showInfoMock,
    showWarning: showWarningMock
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isSimpleMode() {
      return authIsSimpleMode.value
    }
  })
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    post: apiPostMock,
    delete: apiDeleteMock
  }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getSettings: vi.fn().mockResolvedValue({ account_quota_notify_enabled: false }),
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false })
    },
    accounts: {
      update: updateAccountMock,
      getById: getAccountByIdMock,
      batchCreate: batchCreateMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    getById: getAccountByIdMock,
    getModelCatalog: getModelCatalogMock,
    getAvailableModels: getAvailableModelsMock,
    updateModelAllowlist: updateModelAllowlistMock,
    syncUpstreamModels: syncUpstreamModelsMock,
    syncUpstreamModelsPreview: vi.fn()
  }
}))

vi.mock('@/api/admin/platformModelPresets', () => ({
  getPlatformModelPresets: vi.fn().mockResolvedValue([])
}))

vi.mock('@/api/modelAccess', () => ({
  modelAccess: {
    groups: vi.fn().mockResolvedValue([]),
    rule: vi.fn().mockResolvedValue({ access_group_id: null, models: null, exclude: [], revision: 0 }),
    saveRule: vi.fn(),
    preview: vi.fn()
  }
}))

vi.mock('@/api/admin/modelAvailability', () => ({
  modelAvailabilityAPI: { get: vi.fn(), declare: vi.fn(), revoke: vi.fn() }
}))

vi.mock('@/api/admin/credentialSecrets', () => ({
  getCredentialSecret: vi.fn().mockRejectedValue({ reason: 'CREDENTIAL_SECRET_NOT_FOUND' }),
  convertCredentialSecret: vi.fn(),
  rotateCredentialSecret: vi.fn(),
  revokeCredentialSecret: vi.fn()
}))

import EditAccountModal from '../EditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: { type: Boolean, default: false }
  },
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: { modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<button type="button" data-testid="set-groups" @click="$emit(\'update:modelValue\', [9])">groups</button>'
})

function makeAccount(overrides: Partial<Account> & Record<string, unknown> = {}): Account {
  return {
    id: 101,
    name: 'Test Account',
    notes: '',
    platform: 'workbuddy',
    type: 'oauth',
    credentials: {
      site: 'cn',
      domain: 'www.workbuddy.cn',
      uid: 'wb-uid-1',
      model_mapping: {
        'hunyuan-t1': 'hunyuan-t1',
        'wb-alias': 'hunyuan-t1',
        'claude-*': 'hunyuan-t1'
      }
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false,
    ...overrides
  } as unknown as Account
}

function mountEditModal(account: Account, echoUpdated = true) {
  const wrapper = mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: [],
      ...(echoUpdated
        ? {
            onUpdated: (updated: Account) => {
              void wrapper.setProps({ account: { ...updated } })
            }
          }
        : {})
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: true,
        Icon: true,
        ModelIcon: true,
        ProxySelector: true,
        AccountProxySelector: true,
        GroupSelector: GroupSelectorStub,
        ModelAccessRuleEditor: true,
        AccountCredentialSecretSection: true,
        TempUnschedStatusModal: true
      }
    }
  })
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  authIsSimpleMode.value = true
  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
  getAvailableModelsMock.mockResolvedValue([])
  getModelCatalogMock.mockResolvedValue({
    synced: true,
    warnings: [],
    state: { rules_revision: '9007199254740993' }
  })
  updateModelAllowlistMock.mockResolvedValue({ rules_revision: '9007199254740994' })
})

describe('Account model selection on open and reopen', () => {
  it.each([
    {
      label: 'WorkBuddy OAuth CN',
      account: makeAccount({
        id: 101,
        platform: 'workbuddy',
        type: 'oauth',
        credentials: {
          site: 'cn',
          domain: 'www.workbuddy.cn',
          uid: 'uid-cn',
          model_mapping: {
            'hunyuan-t1': 'hunyuan-t1',
            'wb-cn-alias': 'hunyuan-t1',
            'claude-*': 'hunyuan-t1'
          }
        }
      }),
      expectedDirect: ['hunyuan-t1'],
      expectedMappings: [
        { from: 'wb-cn-alias', to: 'hunyuan-t1' },
        { from: 'claude-*', to: 'hunyuan-t1' }
      ]
    },
    {
      label: 'WorkBuddy OAuth International',
      account: makeAccount({
        id: 102,
        platform: 'workbuddy',
        type: 'oauth',
        credentials: {
          site: 'global',
          domain: 'www.workbuddy.ai',
          uid: 'uid-global',
          model_mapping: {
            'gpt-5.2': 'gpt-5.2',
            'wb-global-alias': 'gpt-5.2',
            'gemini-*': 'gpt-5.2'
          }
        }
      }),
      expectedDirect: ['gpt-5.2'],
      expectedMappings: [
        { from: 'wb-global-alias', to: 'gpt-5.2' },
        { from: 'gemini-*', to: 'gpt-5.2' }
      ]
    },
    {
      label: 'WorkBuddy API-key',
      account: makeAccount({
        id: 103,
        platform: 'workbuddy',
        type: 'apikey',
        credentials_status: { has_api_key: true },
        credentials: {
          site: 'cn',
          model_mapping: { 'hunyuan-turbos': 'hunyuan-turbos', alias: 'hunyuan-turbos' }
        }
      }),
      expectedDirect: ['hunyuan-turbos'],
      expectedMappings: [{ from: 'alias', to: 'hunyuan-turbos' }]
    },
    {
      label: 'Anthropic OAuth',
      account: makeAccount({
        id: 104,
        platform: 'anthropic',
        type: 'oauth',
        credentials: {
          model_mapping: { 'claude-sonnet-4-5': 'claude-sonnet-4-5', 'claude-fast': 'claude-haiku-4-5' }
        }
      }),
      expectedDirect: ['claude-sonnet-4-5'],
      expectedMappings: [{ from: 'claude-fast', to: 'claude-haiku-4-5' }]
    },
    {
      label: 'Anthropic setup-token',
      account: makeAccount({
        id: 105,
        platform: 'anthropic',
        type: 'setup-token',
        credentials: {
          model_mapping: { 'claude-opus-4-1': 'claude-opus-4-1', 'opus-*': 'claude-opus-4-1' }
        }
      }),
      expectedDirect: ['claude-opus-4-1'],
      expectedMappings: [{ from: 'opus-*', to: 'claude-opus-4-1' }]
    },
    {
      label: 'Gemini OAuth',
      account: makeAccount({
        id: 106,
        platform: 'gemini',
        type: 'oauth',
        credentials: {
          oauth_type: 'code_assist',
          tier_id: 'gcp_standard',
          model_mapping: { 'gemini-2.5-pro': 'gemini-2.5-pro', 'gem-*': 'gemini-2.5-flash' }
        }
      }),
      expectedDirect: ['gemini-2.5-pro'],
      expectedMappings: [{ from: 'gem-*', to: 'gemini-2.5-flash' }]
    },
    {
      label: 'Gemini setup-token',
      account: makeAccount({
        id: 107,
        platform: 'gemini',
        type: 'setup-token',
        credentials: {
          tier_id: 'gcp_standard',
          model_mapping: { 'gemini-2.5-flash': 'gemini-2.5-flash' }
        }
      }),
      expectedDirect: ['gemini-2.5-flash'],
      expectedMappings: []
    }
  ])('hydrates and echoes saved model rules on open and reopen for $label', async ({ account, expectedDirect, expectedMappings }) => {
    const wrapper = mountEditModal(account)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('modelValue')).toEqual(expectedDirect)
    expect(selector.props('modelMappings')).toEqual(expectedMappings)
    expect(wrapper.find('[data-testid="unsaved-model-selection-banner"]').exists()).toBe(false)

    await wrapper.setProps({ show: false })
    await flushPromises()
    await wrapper.setProps({ show: true, account: { ...account, credentials: { ...account.credentials } } })
    await flushPromises()
    expect(selector.props('modelValue')).toEqual(expectedDirect)
    expect(selector.props('modelMappings')).toEqual(expectedMappings)
    wrapper.unmount()
  })

  it('switches cleanly between WorkBuddy CN and International accounts without leaking rules or synthesizing discovery', async () => {
    getAvailableModelsMock.mockResolvedValue([{ id: 'discovered-only-model', display_name: 'Discovered Only' }])
    const cnAccount = makeAccount({
      id: 201,
      name: 'WB CN',
      credentials: { site: 'cn', domain: 'www.workbuddy.cn', model_mapping: { 'cn-model': 'cn-model' } }
    })
    const globalUnmappedAccount = makeAccount({
      id: 202,
      name: 'WB Global Unmapped',
      credentials: { site: 'global', domain: 'www.workbuddy.ai' }
    })
    const wrapper = mountEditModal(cnAccount)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('modelValue')).toEqual(['cn-model'])

    await wrapper.setProps({ account: globalUnmappedAccount })
    await flushPromises()
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.props('modelMappings')).toEqual([])
    wrapper.unmount()
  })
})

describe('Preserving model selection on rename', () => {
  it('sends preserve_model_selection=true and omits protected fields on WorkBuddy OAuth rename while echoing concurrent server updates', async () => {
    const initial = makeAccount({
      id: 301,
      name: 'WB Original',
      credentials: {
        site: 'cn',
        domain: 'www.workbuddy.cn',
        uid: 'uid-301',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'wb-alias': 'hunyuan-t1' }
      },
      extra: {
        upstream_model_allowlist: { enabled: true, models: ['hunyuan-t1'] }
      }
    })
    const serverUpdated = makeAccount({
      ...initial,
      name: 'WB Renamed',
      credentials: {
        ...initial.credentials,
        model_mapping: {
          'hunyuan-t1': 'hunyuan-t1',
          'wb-alias': 'hunyuan-t1',
          'concurrent-tab-model': 'concurrent-tab-model'
        }
      }
    })
    updateAccountMock.mockResolvedValue(serverUpdated)

    const wrapper = mountEditModal(initial)
    await flushPromises()
    await wrapper.get('input').setValue('WB Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload.name).toBe('WB Renamed')
    expect(payload.preserve_model_selection).toBe(true)
    expect(payload.credentials).toEqual({
      site: 'cn',
      domain: 'www.workbuddy.cn',
      uid: 'uid-301'
    })
    expect(payload.credentials).not.toHaveProperty('model_mapping')
    expect(payload.credentials).not.toHaveProperty('model_whitelist')
    expect(payload.extra ?? {}).not.toHaveProperty('upstream_model_allowlist')

    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('modelValue')).toEqual(['hunyuan-t1', 'concurrent-tab-model'])
    expect(selector.props('modelMappings')).toEqual([{ from: 'wb-alias', to: 'hunyuan-t1' }])
    expect(selector.get('[data-testid="model-save-status"]').text()).toBe('admin.accounts.modelSync.unchangedSelection')
    wrapper.unmount()
  })

  it('sends preserve_model_selection=true and updated site/domain when switching WorkBuddy site and renaming', async () => {
    const initial = makeAccount({
      id: 302,
      name: 'WB Site Switch',
      credentials: {
        site: 'cn',
        domain: 'www.workbuddy.cn',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1' }
      }
    })
    const serverUpdated = makeAccount({
      ...initial,
      name: 'WB Global',
      credentials: {
        site: 'global',
        domain: 'www.workbuddy.ai',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1' }
      }
    })
    updateAccountMock.mockResolvedValue(serverUpdated)

    const wrapper = mountEditModal(initial)
    await flushPromises()
    await wrapper.get('input').setValue('WB Global')
    await wrapper.get('[data-testid="workbuddy-site-global"]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent(ModelWhitelistSelector).props('configurationDirty')).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload.preserve_model_selection).toBe(true)
    expect(payload.credentials).toEqual({
      site: 'global',
      domain: 'www.workbuddy.ai'
    })
    expect(payload.credentials).not.toHaveProperty('model_mapping')
    expect(wrapper.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['hunyuan-t1'])
    wrapper.unmount()
  })

  it('does not silently discard an unsaved model selection draft during metadata save and lets the user save it via allowlist endpoint', async () => {
    const initial = makeAccount({
      id: 303,
      name: 'WB Draft',
      credentials: {
        site: 'cn',
        domain: 'www.workbuddy.cn',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1' }
      }
    })
    const renamedAccount = makeAccount({
      ...initial,
      name: 'WB Draft Renamed'
    })
    const selectionSavedAccount = makeAccount({
      ...renamedAccount,
      credentials: {
        ...renamedAccount.credentials,
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'new-draft-model': 'new-draft-model' }
      }
    })
    updateAccountMock.mockResolvedValue(renamedAccount)
    getAccountByIdMock.mockResolvedValue(selectionSavedAccount)

    const wrapper = mountEditModal(initial)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)

    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('new-draft-model')
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="unsaved-model-selection-banner"]').exists()).toBe(true)

    await wrapper.get('input').setValue('WB Draft Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledWith(
      303,
      expect.objectContaining({ name: 'WB Draft Renamed', preserve_model_selection: true })
    )
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
    expect(showInfoMock).toHaveBeenCalledWith('admin.accounts.modelSync.accountSavedSelectionStillUnsaved')
    // Unsaved draft must still be preserved after general save + parent echo
    expect(selector.props('modelValue')).toEqual(['hunyuan-t1', 'new-draft-model'])
    expect(wrapper.find('[data-testid="unsaved-model-selection-banner"]').exists()).toBe(true)

    await wrapper.get('[data-testid="footer-save-model-selection"]').trigger('click')
    await flushPromises()
    expect(updateModelAllowlistMock).toHaveBeenCalledWith(
      303,
      expect.objectContaining({
        expected_rules_revision: '9007199254740993',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'new-draft-model': 'new-draft-model' }
      })
    )
    expect(updateModelAllowlistMock.mock.calls[0]?.[1]).not.toHaveProperty('preserve_model_selection')
    expect(wrapper.find('[data-testid="unsaved-model-selection-banner"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('guards closing when model selection edits are unsaved (cancel, save-and-close, CAS failure, discard-and-close)', async () => {
    const initial = makeAccount({
      id: 304,
      credentials: {
        site: 'cn',
        domain: 'www.workbuddy.cn',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1' }
      }
    })
    const savedAccount = makeAccount({
      ...initial,
      credentials: {
        ...initial.credentials,
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'extra-model': 'extra-model' }
      }
    })
    getAccountByIdMock.mockResolvedValue(savedAccount)

    const wrapper = mountEditModal(initial)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('extra-model')
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    await flushPromises()

    const closeBtn = wrapper.findAll('button').find(button => button.text().trim() === 'common.close')!
    await closeBtn.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-testid="unsaved-model-selection-dialog"]').exists()).toBe(true)

    // Cancel keeps modal open and draft intact
    await wrapper.get('[data-testid="unsaved-selection-cancel-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="unsaved-model-selection-dialog"]').exists()).toBe(false)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(selector.props('modelValue')).toEqual(['hunyuan-t1', 'extra-model'])

    // Stale CAS failure on Save-and-Close keeps modal open with draft intact
    updateModelAllowlistMock.mockRejectedValueOnce({ response: { status: 409, data: { reason: 'CONFLICT' } } })
    await closeBtn.trigger('click')
    await wrapper.get('[data-testid="unsaved-selection-save-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(selector.props('modelValue')).toEqual(['hunyuan-t1', 'extra-model'])

    // Successful Save-and-Close saves via allowlist endpoint then emits close
    updateModelAllowlistMock.mockResolvedValueOnce({ rules_revision: '9007199254740995' })
    await closeBtn.trigger('click')
    await wrapper.get('[data-testid="unsaved-selection-save-btn"]').trigger('click')
    await flushPromises()
    expect(updateModelAllowlistMock).toHaveBeenLastCalledWith(
      304,
      expect.objectContaining({
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'extra-model': 'extra-model' }
      })
    )
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()

    // Discard & close emits close without saving selection
    const wrapper2 = mountEditModal(initial)
    await flushPromises()
    const selector2 = wrapper2.getComponent(ModelWhitelistSelector)
    await selector2.get('input[aria-label="admin.accounts.customModelName"]').setValue('discarded-model')
    await selector2.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    await flushPromises()
    const callsBefore = updateModelAllowlistMock.mock.calls.length
    await wrapper2.findAll('button').find(button => button.text().trim() === 'common.close')!.trigger('click')
    await wrapper2.get('[data-testid="unsaved-selection-discard-btn"]').trigger('click')
    await flushPromises()
    expect(updateModelAllowlistMock.mock.calls.length).toBe(callsBefore)
    expect(wrapper2.emitted('close')).toHaveLength(1)
    wrapper2.unmount()
  })
})

describe('Clearing model selection and preserving deny-all', () => {
  it('persists explicit clear as deny-all on WorkBuddy OAuth and preserves deny-all across subsequent rename', async () => {
    const initial = makeAccount({
      id: 401,
      name: 'WB Clear Test',
      credentials: {
        site: 'cn',
        domain: 'www.workbuddy.cn',
        model_mapping: { 'hunyuan-t1': 'hunyuan-t1', 'wb-alias': 'hunyuan-t1' }
      }
    })
    const cleared = makeAccount({
      ...initial,
      credentials: { site: 'cn', domain: 'www.workbuddy.cn', model_mapping: {} },
      extra: { upstream_model_allowlist: { enabled: true, models: [] } }
    })
    const renamedCleared = makeAccount({
      ...cleared,
      name: 'WB Cleared Renamed'
    })
    const reselected = makeAccount({
      ...renamedCleared,
      credentials: { site: 'cn', domain: 'www.workbuddy.cn', model_mapping: { 'hunyuan-turbos': 'hunyuan-turbos' } },
      extra: { upstream_model_allowlist: { enabled: false, models: [] } }
    })
    getAccountByIdMock.mockResolvedValueOnce(cleared).mockResolvedValueOnce(reselected)
    updateAccountMock.mockResolvedValue(renamedCleared)

    const wrapper = mountEditModal(initial)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)

    await selector.get('[data-testid="clear-model-selection"]').trigger('click')
    selector.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()

    expect(updateModelAllowlistMock).toHaveBeenLastCalledWith(
      401,
      expect.objectContaining({
        enabled: true,
        models: [],
        model_mapping: {},
        expected_rules_revision: '9007199254740993'
      })
    )
    expect(updateModelAllowlistMock.mock.calls[0]?.[1]).not.toHaveProperty('preserve_model_selection')
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.props('modelMappings')).toEqual([])
    expect(selector.find('[data-testid="deny-all"]').exists()).toBe(true)

    await wrapper.get('input').setValue('WB Cleared Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const renamePayload = updateAccountMock.mock.calls[0]?.[1]
    expect(renamePayload.preserve_model_selection).toBe(true)
    expect(renamePayload.credentials).not.toHaveProperty('model_mapping')
    expect(renamePayload.extra ?? {}).not.toHaveProperty('upstream_model_allowlist')

    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('hunyuan-turbos')
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    await selector.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(updateModelAllowlistMock).toHaveBeenLastCalledWith(
      401,
      expect.objectContaining({
        enabled: false,
        models: [],
        model_mapping: { 'hunyuan-turbos': 'hunyuan-turbos' }
      })
    )
    expect(selector.find('[data-testid="deny-all"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('Model selection across provider platforms', () => {
  it('handles OpenAI compact_model_mapping untouched omission, explicit edit, and explicit clear', async () => {
    const account = makeAccount({
      id: 501,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      credentials: {
        model_mapping: { 'gpt-5.2': 'gpt-5.2' },
        compact_model_mapping: { 'gpt-5.4': 'gpt-5.4-compact' }
      },
      extra: {
        openai_oauth_passthrough: true,
        openai_compact_mode: 'force_on'
      }
    })
    updateAccountMock.mockImplementation(async (_id, payload) => ({
      ...account,
      ...payload,
      credentials: { ...account.credentials, ...(payload.credentials ?? {}) }
    }))

    const wrapper = mountEditModal(account)
    await flushPromises()

    // 1. Untouched general save omits both model_mapping and compact_model_mapping
    await wrapper.get('input').setValue('OpenAI Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const firstPayload = updateAccountMock.mock.calls[0]?.[1]
    expect(firstPayload.preserve_model_selection).toBe(true)
    expect(firstPayload.credentials ?? {}).not.toHaveProperty('model_mapping')
    expect(firstPayload.credentials ?? {}).not.toHaveProperty('compact_model_mapping')
    expect(firstPayload.extra?.openai_passthrough).toBe(true)

    // 2. Explicit edit of compact_model_mapping sends compact_model_mapping while preserving primary model_mapping
    const compactTarget = wrapper
      .findAll('input')
      .find(input => (input.element as HTMLInputElement).value === 'gpt-5.4-compact')!
    await compactTarget.setValue('gpt-5.4-compact-v2')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const secondPayload = updateAccountMock.mock.calls[1]?.[1]
    expect(secondPayload.preserve_model_selection).toBe(true)
    expect(secondPayload.credentials).not.toHaveProperty('model_mapping')
    expect(secondPayload.credentials?.compact_model_mapping).toEqual({ 'gpt-5.4': 'gpt-5.4-compact-v2' })

    // 3. Explicit clear of compact_model_mapping sends empty object {} so backend clears it
    const compactLabel = wrapper.findAll('label').find(label => label.text() === 'admin.accounts.openai.compactModelMapping')!
    const compactContainer = wrapper.findAll('div').find(div => div.element === compactLabel.element.parentElement)!
    const removeCompactButton = compactContainer.findAll('button').find(button => button.attributes('title') === 'common.delete' || button.classes().some(cls => cls.includes('red')))
      ?? compactContainer.findAll('button')[0]!
    await removeCompactButton.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const thirdPayload = updateAccountMock.mock.calls[2]?.[1]
    expect(thirdPayload.preserve_model_selection).toBe(true)
    expect(thirdPayload.credentials?.compact_model_mapping).toEqual({})
    wrapper.unmount()
  })

  it('preserves Spark shadow restrictions on group-only edit and allows explicit compact mapping edit without parent tokens', async () => {
    authIsSimpleMode.value = false
    const shadow = makeAccount({
      id: 502,
      name: 'Spark Shadow',
      platform: 'openai',
      type: 'oauth',
      parent_account_id: 1,
      credentials: {
        access_token: 'parent-secret',
        model_mapping: { 'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark' },
        compact_model_mapping: { 'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact' }
      }
    })
    updateAccountMock.mockResolvedValue(shadow)
    const wrapper = mountEditModal(shadow)
    await flushPromises()

    await wrapper.get('[data-testid="set-groups"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]).toMatchObject({
      group_ids: [9],
      preserve_model_selection: true
    })
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toBeUndefined()

    const compactTarget = wrapper
      .findAll('input')
      .find(input => (input.element as HTMLInputElement).value === 'gpt-5.3-codex-spark-compact')!
    await compactTarget.setValue('gpt-5.3-codex-spark-compact-v2')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[1]?.[1]?.preserve_model_selection).toBe(true)
    expect(updateAccountMock.mock.calls[1]?.[1]?.credentials).toEqual({
      compact_model_mapping: { 'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact-v2' }
    })
    wrapper.unmount()
  })

  it.each([
    {
      label: 'Grok OAuth',
      account: makeAccount({
        id: 503,
        platform: 'grok',
        type: 'oauth',
        credentials: { refresh_token: 'rt', base_url: 'https://api.x.ai/v1', model_mapping: { 'grok-4': 'grok-4' } }
      }),
      expectedDirect: ['grok-4']
    },
    {
      label: 'Cursor Relay',
      account: makeAccount({
        id: 504,
        platform: 'cursor',
        type: 'upstream',
        credentials_status: { has_api_key: true },
        credentials: { base_url: 'https://relay.example/v1', api_protocol: 'chat_completions', model_mapping: { 'cursor-fast': 'cursor-fast' } }
      }),
      expectedDirect: ['cursor-fast']
    },
    {
      label: 'Antigravity legacy model_whitelist',
      account: makeAccount({
        id: 505,
        platform: 'antigravity',
        type: 'oauth',
        credentials: { antigravity_project_id: 'proj-1', model_whitelist: ['gemini-2.5-pro'] }
      }),
      expectedDirect: ['gemini-2.5-pro']
    },
    {
      label: 'Bedrock',
      account: makeAccount({
        id: 506,
        platform: 'anthropic',
        type: 'bedrock',
        credentials: {
          auth_mode: 'sigv4',
          aws_region: 'us-east-1',
          aws_access_key_id: 'AKIAEXAMPLE',
          model_mapping: { 'claude-sonnet': 'anthropic.claude-sonnet' }
        }
      }),
      expectedDirect: []
    },
    {
      label: 'Vertex service_account',
      account: makeAccount({
        id: 507,
        platform: 'gemini',
        type: 'service_account',
        credentials_status: { has_service_account_json: true },
        credentials: {
          project_id: 'gcp-proj',
          client_email: 'sa@gcp-proj.iam.gserviceaccount.com',
          location: 'us-central1',
          tier_id: 'vertex',
          model_mapping: { 'gemini-2.5-flash': 'gemini-2.5-flash' }
        }
      }),
      expectedDirect: ['gemini-2.5-flash']
    },
    {
      label: 'Upstream account',
      account: makeAccount({
        id: 508,
        platform: 'openai',
        type: 'upstream',
        credentials: {
          base_url: 'https://upstream.example/v1',
          model_mapping: { 'upstream-model': 'upstream-model' }
        }
      }),
      expectedDirect: ['upstream-model']
    }
  ])('preserves model selection across rename on control platform: $label', async ({ account, expectedDirect }) => {
    updateAccountMock.mockResolvedValue({ ...account, name: 'Control Renamed' })
    const wrapper = mountEditModal(account)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('modelValue')).toEqual(expectedDirect)

    await wrapper.get('input').setValue('Control Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload.preserve_model_selection).toBe(true)
    expect(payload.credentials ?? {}).not.toHaveProperty('model_mapping')
    expect(payload.credentials ?? {}).not.toHaveProperty('model_whitelist')
    expect(payload.extra ?? {}).not.toHaveProperty('upstream_model_allowlist')
    wrapper.unmount()
  })
})
