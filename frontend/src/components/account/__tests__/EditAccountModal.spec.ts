vi.mock('@/api/admin/proxyGroups', () => ({ proxyGroupsAPI: { list: vi.fn().mockResolvedValue({ items: [], total: 0 }), get: vi.fn().mockRejectedValue(new Error('unavailable')) } }))
import createAccountSource from '../CreateAccountModal.vue?raw'
import editAccountSource from '../EditAccountModal.vue?raw'
import englishAccounts from '@/i18n/locales/en/admin/accounts'
import chineseAccounts from '@/i18n/locales/zh/admin/accounts'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const {
  catalogRead,
  allowlistWrite,
  credentialAccountRead,
  updateAccountMock,
  checkMixedChannelRiskMock,
  authIsSimpleMode,
  syncUpstreamModelsMock,
  showInfoMock,
  showWarningMock,
  showSuccessMock,
  showErrorMock
} = vi.hoisted(() => ({
  catalogRead: vi.fn().mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '1' } }),
  allowlistWrite: vi.fn(),
  credentialAccountRead: vi.fn(),
  updateAccountMock: vi.fn(),
  syncUpstreamModelsMock: vi.fn(),
  showInfoMock: vi.fn(),
  showWarningMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
  authIsSimpleMode: { value: true }
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

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getById: credentialAccountRead,
      update: updateAccountMock,
      syncUpstreamModels: syncUpstreamModelsMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: { getById: vi.fn().mockResolvedValue({ id: 1, credentials: {}, extra: {} }), syncUpstreamModels: syncUpstreamModelsMock, getAvailableModels: vi.fn().mockResolvedValue([]), getModelCatalog: catalogRead, updateModelAllowlist: allowlistWrite, syncUpstreamModelsPreview: vi.fn().mockResolvedValue({ candidate_models: ['public-glm'], models: ['public-glm'], discovery_complete: true, complete: true }) },
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      // 保留插值参数，便于断言带 {protocol} 的提示确实带上了账号原值；无参数时与裸键相等。
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key)
    })
  }
})

import { getPlatformModelPresets } from '@/api/admin/platformModelPresets'
import { accountsAPI } from '@/api/admin/accounts'
import EditAccountModal from '../EditAccountModal.vue'
import AccountProxySelector from '@/components/common/AccountProxySelector.vue'
import CursorKeyFields from '../CursorKeyFields.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { modelAccess } from '@/api/modelAccess'
import { modelAvailabilityAPI } from '@/api/admin/modelAvailability'
import { convertCredentialSecret, rotateCredentialSecret, revokeCredentialSecret } from '@/api/admin/credentialSecrets'
vi.mock('@/api/modelAccess', () => ({ modelAccess: {
  groups: vi.fn().mockResolvedValue([]), rule: vi.fn().mockResolvedValue({ access_group_id: null, models: null, exclude: [], revision: 0 }),
  saveRule: vi.fn(), preview: vi.fn()
} }))
vi.mock('@/api/admin/modelAvailability', () => ({ modelAvailabilityAPI: { get: vi.fn(), declare: vi.fn(), revoke: vi.fn() } }))
vi.mock('@/api/admin/credentialSecrets', () => ({
  getCredentialSecret: vi.fn().mockRejectedValue({ reason: 'CREDENTIAL_SECRET_NOT_FOUND' }),
  convertCredentialSecret: vi.fn(), rotateCredentialSecret: vi.fn(), revokeCredentialSecret: vi.fn()
}))
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn().mockResolvedValue([]) }))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div>
      <button
        type="button"
        data-testid="rewrite-to-snapshot"
        @click="$emit('update:modelValue', ['gpt-5.2-2025-12-11'])"
      >
        rewrite
      </button>
      <span data-testid="model-whitelist-value">
        {{ Array.isArray(modelValue) ? modelValue.join(',') : '' }}
      </span>
    </div>
  `
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: ''
    },
    options: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <select
      v-bind="$attrs"
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="group-selector">
      <button
        type="button"
        data-testid="set-shadow-group"
        @click="$emit('update:modelValue', [7])"
      >
        group
      </button>
    </div>
  `
})

function buildAccount() {
  return {
    id: 1,
    name: 'OpenAI Key',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: {
      api_key: 'sk-test',
      base_url: 'https://api.openai.com',
      model_mapping: {
        'gpt-5.2': 'gpt-5.2'
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
    auto_pause_on_expired: false
  } as any
}

it('preserves saved retired mappings during preset reload and edit submission', async () => {
  const savedMapping = { 'retired-model': 'retired-model', 'saved-alias': 'retired-model' }
  const account = buildAccount()
  account.credentials.model_mapping = savedMapping
  updateAccountMock.mockReset().mockResolvedValue(account)
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  vi.mocked(getPlatformModelPresets).mockResolvedValue([{ platform: 'openai', provider_profile: 'native', models: [
    { id: 'retired-model', lifecycle: 'retired' }, { id: 'current-model', lifecycle: 'active' },
  ] }] as Awaited<ReturnType<typeof getPlatformModelPresets>>)
  const wrapper = mountModal(account, false, true)
  await flushPromises()
  await (wrapper.vm as unknown as { reloadPresets: () => Promise<void> }).reloadPresets()
  await flushPromises()
  const selector = wrapper.getComponent(ModelWhitelistSelector)
  expect(selector.props('modelValue')).toEqual(['retired-model'])
  expect(selector.props('modelMappings')).toEqual([{ from: 'saved-alias', to: 'retired-model' }])
  expect((wrapper.vm as unknown as { presetMappings: { from: string }[] }).presetMappings.map(preset => preset.from)).toEqual(['current-model'])
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock.mock.calls.at(-1)?.[1].credentials.model_mapping).toEqual(savedMapping)
  expect(account.credentials.model_mapping).toEqual(savedMapping)
  wrapper.unmount()
  vi.mocked(getPlatformModelPresets).mockResolvedValue([])
})

function buildOpenAISparkShadowAccount() {
  const account = buildAccount()
  return {
    ...account,
    id: 4,
    name: 'OpenAI Spark Shadow',
    type: 'oauth',
    parent_account_id: 1,
    credentials: {
      access_token: 'parent-access-token',
      refresh_token: 'parent-refresh-token',
      api_key: 'sk-parent',
      base_url: 'https://api.openai.com',
      model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark'
      },
      compact_model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact'
      }
    },
    group_ids: []
  } as any
}

function buildVertexAccount() {
  return {
    id: 2,
    name: 'Vertex SA',
    notes: '',
    platform: 'gemini',
    type: 'service_account',
    credentials: {
      service_account_json: '{"type":"service_account","client_email":"sa@example.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\\nMIIE\\n-----END PRIVATE KEY-----\\n"}',
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function buildAntigravityAccount(projectId = 'configured-project') {
  return {
    id: 3,
    name: 'Antigravity OAuth',
    notes: '',
    platform: 'antigravity',
    type: 'oauth',
    credentials: {
      antigravity_project_id: projectId,
      model_mapping: {
        'gemini-2.5-flash': 'gemini-2.5-flash'
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
    auto_pause_on_expired: false
  } as any
}

function buildGrokOAuthAccount() {
  return {
    id: 5,
    name: 'Grok OAuth',
    notes: '',
    platform: 'grok',
    type: 'oauth',
    credentials: {
      refresh_token: 'grok-rt',
      base_url: 'https://api.x.ai/v1',
      model_mapping: {
        'grok-latest': 'grok-4.3'
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
    auto_pause_on_expired: false
  } as any
}

function buildGrokAPIKeyAccount() {
  return {
    ...buildAccount(),
    id: 6,
    name: 'Grok API Key',
    platform: 'grok',
    credentials: {},
    credentials_status: { has_api_key: true },
    concurrency: 2
  } as any
}

function buildOpenAISetupTokenAccount() {
  return {
    ...buildAccount(),
    type: 'setup-token',
    extra: {
      openai_oauth_responses_websockets_v2_mode: 'ctx_pool',
      openai_oauth_responses_websockets_v2_enabled: true
    }
  } as any
}

function buildOpenAIOAuthParentAccount() {
  return {
    ...buildAccount(),
    id: 7,
    name: 'OpenAI OAuth Parent',
    type: 'oauth',
    parent_account_id: null,
    credentials: { access_token: 'oauth-token' },
    extra: {}
  } as any
}

function mountModal(account = buildAccount(), renderGroupSelector = false, realCatalog = false) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: true,
        GroupSelector: renderGroupSelector ? false : GroupSelectorStub,
        ModelWhitelistSelector: realCatalog ? false : ModelWhitelistSelectorStub
      }
    }
  })
}

describe('IP group account configuration', () => {
  it.each([null, 41])('omits unchanged proxy fields when editing an account with group %s', async groupId => {
    const account = { ...buildAccount(), proxy_group_id: groupId }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const payload = updateAccountMock.mock.calls[0][1]
    expect(payload).not.toHaveProperty('proxy_id')
    expect(payload).not.toHaveProperty('proxy_group_id')
    wrapper.unmount()
  })
  it.each([{ proxy_id: 18, proxy_group_id: null }, { proxy_id: null, proxy_group_id: 41 }])('explicitly clears both fields when selecting no proxy from %j', async selection => {
    const account = { ...buildAccount(), ...selection }
    updateAccountMock.mockReset().mockResolvedValue({ ...account, proxy_id: null, proxy_group_id: null })
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.getComponent(AccountProxySelector).get('select').setValue('direct')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0][1]).toMatchObject({ proxy_id: 0, proxy_group_id: 0 })
    wrapper.unmount()
  })
  it('does not mutate the account while changing the draft and closing', async () => {
    const account = { ...buildAccount(), proxy_group_id: 41 }
    updateAccountMock.mockReset()
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.getComponent(AccountProxySelector).get('select').setValue('direct')
    await wrapper.setProps({ show: false })
    expect(account.proxy_group_id).toBe(41)
    expect(updateAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('model selection saves preserve unrelated account drafts', () => {
  it('retains the Antigravity legacy whitelist fallback in shared state', async () => {
    const account = { ...buildAntigravityAccount(), credentials: { model_whitelist: [' LegacyExact '] } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account, false, true)
    await flushPromises()
    expect(wrapper.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['LegacyExact'])
    expect(wrapper.getComponent(ModelWhitelistSelector).props('modelMappings')).toEqual([])
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping).toEqual({ LegacyExact: 'LegacyExact' })
    expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('model_whitelist')
    wrapper.unmount()
  })

  it.each(['oauth', 'apikey'])('hydrates Antigravity %s identities and aliases through save and reopen', async type => {
    const mapping = { Exact: 'Exact', Alias: 'PrivateTarget', 'family-*': 'FamilyTarget' }
    const account = { ...buildAntigravityAccount(), type, credentials: { model_mapping: mapping }, credentials_status: { has_api_key: true } }
    catalogRead.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '7' } })
    allowlistWrite.mockReset().mockResolvedValue({ rules_revision: '8' })
    vi.mocked(accountsAPI.getById).mockResolvedValue(account)
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account, false, true)
    await wrapper.setProps({ onUpdated: updated => { void wrapper.setProps({ account: { ...updated } }) } })
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('modelValue')).toEqual(['Exact'])
    expect(selector.props('modelMappings')).toEqual([{ from: 'Alias', to: 'PrivateTarget' }, { from: 'family-*', to: 'FamilyTarget' }])
    expect(wrapper.get('[data-testid="selected-models"]').text()).toContain('PrivateTarget')
    const expiryLabel = wrapper.findAll('label').find(label => label.text() === 'admin.accounts.autoPauseOnExpired')!
    const expiryToggle = wrapper.findAll('button').find(button => button.element === expiryLabel.element.parentElement!.parentElement!.querySelector('button'))!
    await expiryToggle.trigger('click')
    expect(selector.props('configurationDirty')).toBe(false)
    expect(wrapper.get('[data-testid="save-model-selection"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(allowlistWrite).toHaveBeenCalledWith(account.id, expect.objectContaining({ model_mapping: mapping }))
    expect(expiryToggle.classes()).toContain('bg-primary-600')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0][1].auto_pause_on_expired).toBe(true)
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping).toEqual(mapping)
    wrapper.unmount()
    const reopened = mountModal({ ...account, credentials: updateAccountMock.mock.calls[0][1].credentials }, false, true)
    await flushPromises()
    expect(reopened.getComponent(ModelWhitelistSelector).props('modelValue')).toEqual(['Exact'])
    expect(reopened.getComponent(ModelWhitelistSelector).props('modelMappings')).toEqual([{ from: 'Alias', to: 'PrivateTarget' }, { from: 'family-*', to: 'FamilyTarget' }])
    reopened.unmount()
  })

  it('preserves unsaved account fields after the parent echoes an independently saved credential change', async () => {
    const account = buildAccount()
    credentialAccountRead.mockResolvedValue({ ...account, updated_at: '2026-10-01T00:00:00Z' })
    const wrapper = mountModal(account)
    await wrapper.setProps({ onUpdated: updated => { void wrapper.setProps({ account: { ...updated } }) } })
    await flushPromises()
    const name = wrapper.get<HTMLInputElement>('[data-tour="edit-account-form-name"]')
    await name.setValue('Unsaved name')
    wrapper.getComponent({ name: 'AccountCredentialSecretSection' }).vm.$emit('changed')
    await flushPromises()
    expect(credentialAccountRead).toHaveBeenCalledWith(account.id)
    expect(name.element.value).toBe('Unsaved name')
    wrapper.unmount()
  })

  it('ignores a credential account refresh after switching accounts', async () => {
    const account = buildAccount()
    let finish!: (account: Account) => void
    credentialAccountRead.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountModal(account)
    await flushPromises()
    wrapper.getComponent({ name: 'AccountCredentialSecretSection' }).vm.$emit('changed')
    await wrapper.setProps({ account: { ...account, id: 99, name: 'Other account' } })
    finish(account)
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeUndefined()
    wrapper.unmount()
  })
  it.each([false, true])('preserves compact and switch drafts with parent echo=%s', async echoToParent => {
    const account = buildAccount()
    const savedAccount = {
      ...account,
      credentials: { ...account.credentials, model_mapping: { 'public-alias': 'upstream-selected' } },
      extra: { upstream_model_allowlist: { enabled: false, models: [] } }
    }
    catalogRead.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '7' } })
    allowlistWrite.mockReset().mockResolvedValue({ rules_revision: '8' })
    vi.mocked(accountsAPI.getById).mockResolvedValue(savedAccount)
    updateAccountMock.mockReset().mockResolvedValue(savedAccount)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account, false, true)
    if (echoToParent) {
      await wrapper.setProps({ onUpdated: updated => {
        // AccountsView merges runtime fields into a new object before syncing edAcc.
        void wrapper.setProps({ account: { ...updated, current_concurrency: 2 } })
      } })
    }
    await flushPromises()
    const compactSection = wrapper.findAll('label').find(label => label.text() === 'admin.accounts.openai.compactModelMapping')!.element.parentElement!
    const compact = wrapper.findAll('div').find(div => div.element === compactSection)!
    await compact.get('button').trigger('click')
    await compact.get('input[placeholder="admin.accounts.fromModel"]').setValue('compact-source')
    await compact.get('input[placeholder="admin.accounts.toModel"]').setValue('compact-draft-target')
    const expiryLabel = wrapper.findAll('label').find(label => label.text() === 'admin.accounts.autoPauseOnExpired')!
    const expiryToggle = wrapper.findAll('button').find(button => button.element === expiryLabel.element.parentElement!.parentElement!.querySelector('button'))!
    await expiryToggle.trigger('click')
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    expect(selector.props('configurationDirty')).toBe(false)
    await selector.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(allowlistWrite).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('updated')?.[0]).toEqual([savedAccount])
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.props('modelMappings')).toEqual([{ from: 'public-alias', to: 'upstream-selected' }])
    expect(compact.get<HTMLInputElement>('input[placeholder="admin.accounts.toModel"]').element.value).toBe('compact-draft-target')
    expect(expiryToggle.classes()).toContain('bg-primary-600')
    if (echoToParent) {
      expect(wrapper.props('account')).not.toBe(savedAccount)
      expect(selector.props('modelAllowlist')).toEqual(savedAccount.extra.upstream_model_allowlist)
    }
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledWith(account.id, expect.objectContaining({
      auto_pause_on_expired: true,
      credentials: expect.objectContaining({
        compact_model_mapping: { 'compact-source': 'compact-draft-target' },
        model_mapping: { 'public-alias': 'upstream-selected' }
      })
    }))
    wrapper.unmount()
  })

  it('keeps clear and reselect scope in sync through parent echoes and ordinary saves', async () => {
    const account = buildAccount()
    const cleared = { ...account, credentials: { ...account.credentials, model_mapping: {} }, extra: { upstream_model_allowlist: { enabled: true, models: [] } } }
    const reselected = { ...account, credentials: { ...account.credentials, model_mapping: { 'manual-model': 'manual-model' } }, extra: { upstream_model_allowlist: { enabled: false, models: [] } } }
    catalogRead.mockResolvedValue({ synced: true, warnings: [], state: { rules_revision: '9007199254740993' } })
    allowlistWrite.mockReset().mockResolvedValue({ rules_revision: '9007199254740994' })
    vi.mocked(accountsAPI.getById).mockResolvedValueOnce(cleared).mockResolvedValueOnce(reselected)
    updateAccountMock.mockReset().mockResolvedValue(cleared)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account, false, true)
    await wrapper.setProps({ onUpdated: updated => { void wrapper.setProps({ account: { ...updated } }) } })
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    await selector.get('[data-testid="clear-model-selection"]').trigger('click')
    selector.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(allowlistWrite).toHaveBeenLastCalledWith(account.id, expect.objectContaining({ enabled: true, models: [], model_mapping: {}, expected_rules_revision: '9007199254740993' }))
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.props('modelMappings')).toEqual([])
    expect(selector.find('[data-testid="deny-all"]').exists()).toBe(true)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials.model_mapping ?? {}).toEqual({})
    expect(updateAccountMock.mock.calls[0][1].extra.upstream_model_allowlist).toEqual({ enabled: true, models: [] })

    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('manual-model')
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    await selector.get('[data-testid="save-model-selection"]').trigger('click')
    await flushPromises()
    expect(allowlistWrite).toHaveBeenLastCalledWith(account.id, expect.objectContaining({ enabled: false, models: [], model_mapping: { 'manual-model': 'manual-model' } }))
    expect(selector.props('modelValue')).toEqual(['manual-model'])
    expect(selector.find('[data-testid="deny-all"]').exists()).toBe(false)
    updateAccountMock.mockResolvedValue(reselected)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls.at(-1)![1].credentials.model_mapping).toEqual({ 'manual-model': 'manual-model' })
    expect(updateAccountMock.mock.calls.at(-1)![1].extra.upstream_model_allowlist).toEqual({ enabled: false, models: [] })
    await wrapper.get('[data-account-field="editBaseUrl"]').setValue('https://changed.example')
    expect(selector.props('configurationDirty')).toBe(true)
    expect(selector.get('[data-testid="save-model-selection"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it.each(['switch', 'reopen', 'later refresh'] as const)('fully initializes account settings on %s after a selection save', async action => {
    const account = buildAccount()
    const wrapper = mountModal(account, false, true)
    await flushPromises()
    const expiryLabel = wrapper.findAll('label').find(label => label.text() === 'admin.accounts.autoPauseOnExpired')!
    await wrapper.findAll('button').find(button => button.element === expiryLabel.element.parentElement!.parentElement!.querySelector('button'))!.trigger('click')
    wrapper.getComponent(ModelWhitelistSelector).vm.$emit('selection-saved', account)
    await flushPromises()
    const nextAccount = { ...buildAccount(), id: action === 'switch' ? 22 : account.id, name: 'Fresh account' }
    if (action === 'reopen') await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, account: nextAccount })
    await flushPromises()
    updateAccountMock.mockReset().mockResolvedValue(nextAccount)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledWith(nextAccount.id, expect.objectContaining({ name: 'Fresh account', auto_pause_on_expired: false }))
    wrapper.unmount()
  })
})

describe('EditAccountModal', () => {
  it('edits the token limit without changing the concurrency setting', async () => {
    const account = { ...buildAccount(), tpm_limit: 31 }
    updateAccountMock.mockReset().mockResolvedValue({ ...account, tpm_limit: 47 })
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    const input = wrapper.get('[data-testid="tpm-limit-field"] input')
    expect((input.element as HTMLInputElement).value).toBe('31')
    await input.setValue('47')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalled()
    expect(updateAccountMock.mock.calls[0][1]).toMatchObject({ tpm_limit: 47, concurrency: account.concurrency })
    wrapper.unmount()
  })

  it.each(['partial', 'incomplete'] as const)('keeps Toasts quiet for %s capability metadata', async (status) => {
    showInfoMock.mockReset()
    showWarningMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    syncUpstreamModelsMock.mockResolvedValue({
      candidate_models: ['gemini-new-model'], models: ['gemini-new-model'],
      warnings: [{ code: `upstream_model_metadata_${status}`, message: 'metadata status' }],
    })
    const wrapper = mountModal(buildAntigravityAccount(), false, true)
    await flushPromises()
    const syncButton = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsMock).toHaveBeenCalledWith(3, false)
    if (status === 'partial') {
      expect(showInfoMock).not.toHaveBeenCalled()
      expect(showWarningMock).not.toHaveBeenCalled()
    } else {
      expect(showWarningMock).not.toHaveBeenCalled()
      expect(showSuccessMock).not.toHaveBeenCalled()
    }
    expect(showErrorMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  beforeEach(() => {
    authIsSimpleMode.value = true
  })

  afterEach(() => vi.useRealTimers())

  it.each(['editApiKey', 'editApiProtocol', 'editGeminiTier', 'editPlanType'])('invalidates preview on %s changes without fetching on each edit', async field => {
    catalogRead.mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '1' } })
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ candidate_models: ['Native'], models: ['Native'], discovery_complete: true, complete: true, rules_revision: '1', review: { account_id: 1, digest: 'original' } })
    const wrapper = mountModal(buildAccount(), false, true)
    await flushPromises()
    const panel = wrapper.findAllComponents(ModelWhitelistSelector).at(0)!
    await panel.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    const reads = catalogRead.mock.calls.length
    ;(wrapper.vm as unknown as Record<string, unknown>)[field] = 'changed'
    await flushPromises()
    expect(panel.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    expect(panel.get('[data-testid="preview-upstream-models"]').attributes('disabled')).toBeDefined()
    expect(catalogRead.mock.calls.length).toBe(reads)
    expect(syncUpstreamModelsMock).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('changes the saved allowlist with a reason and lossless revision, then reads it back', async () => {
    const account = buildAccount()
    const initial = { synced: true, warnings: [], state: { rules_revision: '9007199254740993' }, effective_snapshot: { discovered_models: ['Native'], eligible_models: [{ id: 'Native' }] } }
    catalogRead.mockResolvedValueOnce(initial).mockResolvedValueOnce({ ...initial, state: { rules_revision: '9007199254740994' } })
    allowlistWrite.mockReset().mockResolvedValue({ rules_revision: '9007199254740994' })
    const wrapper = mountModal(account, false, true)
    await flushPromises()
    const panel = wrapper.findAllComponents(ModelWhitelistSelector).at(0)!
    await panel.get('[data-testid="allowlist-models"]').setValue('NotDiscovered')
    await panel.get('[data-testid="allowlist-reason"]').setValue('restrict account scope')
    await panel.get('[data-testid="save-allowlist"]').trigger('click')
    await flushPromises()
    expect(allowlistWrite).toHaveBeenCalledWith(account.id, { enabled: false, models: ['NotDiscovered'], reason: 'restrict account scope', expected_rules_revision: '9007199254740993' })
    expect(catalogRead).toHaveBeenLastCalledWith(account.id)
    wrapper.unmount()
  })



  it.each([['apikey', 'ai_studio', 'aistudio_free', 'aistudio_paid'], ['oauth', 'google_one', 'google_one_free', 'google_ai_ultra'], ['oauth', 'code_assist', 'gcp_standard', 'gcp_enterprise']])('loads and saves Gemini %s %s tier', async (type, oauthType, initial, selected) => {
    const account = { ...buildAccount(), platform: 'gemini', type, credentials: { tier_id: initial, oauth_type: oauthType, api_key: 'key', access_token: 'token', model_mapping: { 'gemini-2.5-flash': 'gemini-2.5-flash' } } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const vm = wrapper.vm as unknown as { editGeminiTier: string; allowedModels: string[]; modelMappings: unknown[] }
    expect(vm.editGeminiTier).toBe(initial)
    vm.editGeminiTier = selected
    vm.allowedModels = []
    vm.modelMappings = []
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1].credentials.tier_id).toBe(selected)
    expect(updateAccountMock.mock.calls[0]?.[1].credentials.oauth_type).toBe(oauthType)
    if (type === 'apikey') expect(updateAccountMock.mock.calls[0]?.[1].credentials).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })

  it('edits Cursor auto without echoing stored tokens or changing machine IDs', async () => {
    const account = { ...buildAccount(), platform: 'cursor', type: 'oauth', credentials: { machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64), client_version: '' } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    expect(wrapper.get<HTMLInputElement>('[data-testid="cursor-access_token"]').element.value).toBe('')
    const selector = wrapper.findComponent(ModelWhitelistSelectorStub)
    expect(selector.exists()).toBe(true)
    selector.vm.$emit('update:modelValue', ['default', 'gpt-5.5'])
    await flushPromises()
    await wrapper.get('form').trigger('submit.prevent'); await flushPromises()
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload.credentials.machine_id).toBe('a'.repeat(64))
    expect(payload.credentials.mac_machine_id).toBe('b'.repeat(64))
    expect(payload.credentials).not.toHaveProperty('access_token')
    expect(payload.credentials).not.toHaveProperty('refresh_token')
    expect(payload.credentials).not.toHaveProperty('api_key')
    expect(payload.concurrency).toBe(1)
    expect(payload.credentials.model_mapping).toEqual({ default: 'default', 'gpt-5.5': 'gpt-5.5' })
    wrapper.unmount()
  })

  it.each([
    { type: 'oauth', mode: 'auto', credentials: { machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64), client_version: '3.24.12', access_token: 'stored-token' } },
    { type: 'apikey', mode: 'official', credentials: { machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64), client_version: '3.24.12' } },
    { type: 'upstream', mode: 'relay', credentials: { base_url: 'https://relay.example/v1', api_protocol: 'adaptive' } }
  ] as const)('keeps stored Cursor account types read only ($type)', async ({ type, mode, credentials }) => {
    const account = { ...buildAccount(), platform: 'cursor', type, credentials_status: { has_api_key: true }, credentials: { ...credentials } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()

    const modeButtons = wrapper.get('[data-testid="cursor-key-mode"]').findAll('button')
    expect(modeButtons).toHaveLength(3)
    for (const button of modeButtons) {
      expect(button.attributes('disabled')).toBeDefined()
      await button.trigger('click')
    }
    for (const option of ['auto', 'official', 'relay']) {
      expect(wrapper.get(`[data-testid="cursor-key-mode-${option}"]`).attributes('aria-checked')).toBe(option === mode ? 'true' : 'false')
    }
    expect(wrapper.findComponent(CursorKeyFields).props('mode')).toBe(mode)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const payload = updateAccountMock.mock.calls[0]?.[1]
    // 只读类型不借重排允许互转：保存请求不携带 type/platform 改写。
    expect(updateAccountMock.mock.calls[0]?.[0]).toBe(account.id)
    expect(payload).not.toHaveProperty('type')
    expect(payload).not.toHaveProperty('platform')
    if (mode === 'relay') {
      expect(payload.credentials).toEqual({ base_url: 'https://relay.example/v1', api_protocol: 'adaptive' })
    } else {
      expect(payload.credentials).toEqual({
        client_version: '3.24.12',
        machine_id: 'a'.repeat(64),
        mac_machine_id: 'b'.repeat(64)
      })
      expect(payload.credentials).not.toHaveProperty('api_key')
      expect(payload.credentials).not.toHaveProperty('base_url')
    }
    expect(payload.concurrency).toBe(1)
    wrapper.unmount()
  })


  it.each(['oauth', 'apikey'])('persists, updates and clears Cursor %s scheduling overrides', async (type) => {
    const account = { ...buildAccount(), platform: 'cursor', type, credentials_status: { has_api_key: true }, credentials: { machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64), account_scheduling_threshold: 80 } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    expect(wrapper.get<HTMLInputElement>('[data-testid="account-scheduling-threshold-override-value"]').element.value).toBe('80')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.lastCall?.[1].credentials.account_scheduling_threshold).toBe(80)
    await wrapper.get('[data-testid="account-scheduling-threshold-override-value"]').setValue('75')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.lastCall?.[1].credentials.account_scheduling_threshold).toBe(75)
    await wrapper.get('[data-testid="account-scheduling-threshold-override-enabled"]').setValue(false)
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.lastCall?.[1].credentials.account_scheduling_threshold).toBeNull()
    wrapper.unmount()
  })

  it.each([
    ['qianwen', 'chat_completions', 'https://dashscope.aliyuncs.com/compatible-mode/v1'],
    ['qianwen', 'anthropic', 'https://dashscope.aliyuncs.com/apps/anthropic'],
    ['commandcode', 'chat_completions', 'https://api.commandcode.ai/provider/v1'],
    ['commandcode', 'anthropic', 'https://api.commandcode.ai/provider/v1'],
    ['minimax', 'chat_completions', 'https://api.minimaxi.com/v1'],
    ['minimax', 'anthropic', 'https://api.minimaxi.com/anthropic'],
  ])('restores the %s %s endpoint when Base URL is cleared', async (platform, protocol, expected) => {
    const account = { ...buildAccount(), platform, credentials: { api_key: 'test-key', account_mode: 'payg', api_protocol: protocol, base_url: 'https://relay.example/v1' } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    const input = wrapper.findAll('input').find(input => input.element.value === 'https://relay.example/v1')!
    await input.setValue('')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.lastCall?.[1].credentials.base_url).toBe(expected)
    wrapper.unmount()
  })

  it('sets expiry presets from now instead of extending the saved expiry', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2028-02-29T12:34:00'))
    const account = buildAccount()
    account.expires_at = new Date('2030-06-15T09:00:00').getTime() / 1000
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')

    for (const [label, expected] of [
      ['payment.oneMonth', '2028-03-29T12:34'],
      ['payment.oneYear', '2029-02-28T12:34'],
    ]) {
      const button = wrapper.findAll('button').find((candidate) => candidate.text() === label)!
      expect(button.attributes('type')).toBe('button')
      await button.trigger('click')
      expect(input.element.value).toBe(expected)
      expect(updateAccountMock).not.toHaveBeenCalled()
    }

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateAccountMock.mock.calls[0]?.[1]?.expires_at).toBe(new Date('2029-02-28T12:34:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('can clear a selected expiry preset before saving the account', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    const button = wrapper.findAll('button').find((candidate) => candidate.text() === 'payment.oneYear')!
    await button.trigger('click')
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')
    expect(input.element.value).not.toBe('')
    await input.setValue('')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateAccountMock.mock.calls[0]?.[1]?.expires_at).toBe(0)
    wrapper.unmount()
  })

  it('allows removing assigned inactive groups and undoing the selection before saving', async () => {
    authIsSimpleMode.value = false
    const account = buildAccount()
    const activeGroup = {
      id: 1,
      name: 'Active group',
      platform: 'openai',
      status: 'active',
      subscription_type: 'standard',
      rate_multiplier: 1
    }
    const inactiveGroup = { ...activeGroup, id: 2, name: 'Paused group', status: 'inactive' }
    account.group_ids = [1, 2]
    account.groups = [
      { ...activeGroup, name: 'Outdated name' },
      inactiveGroup,
      inactiveGroup,
      { ...inactiveGroup, id: 3, name: 'Unassigned paused group' }
    ]
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, true)
    await wrapper.setProps({ groups: [activeGroup] as any })
    const selector = wrapper.get('[data-tour="account-form-groups"]')
    expect(selector.findAll('input[type="checkbox"]').map(input => input.attributes('value')))
      .toEqual(['1', '2'])
    expect(selector.text()).toContain('Active group')
    expect(selector.text()).not.toContain('Outdated name')
    const pausedCheckbox = selector.get<HTMLInputElement>('input[value="2"]')
    expect(pausedCheckbox.element.checked).toBe(true)

    await pausedCheckbox.setValue(false)
    expect(selector.get<HTMLInputElement>('input[value="2"]').element.checked).toBe(false)
    await pausedCheckbox.setValue(true)
    expect(pausedCheckbox.element.checked).toBe(true)
    await pausedCheckbox.setValue(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.group_ids).toEqual([1])
    expect(account.group_ids).toEqual([1, 2])
  })

  it('reopening the same account rehydrates the OpenAI whitelist from props', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2-2025-12-11')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      'gpt-5.2': 'gpt-5.2'
    })
  })

  it.each(['qianwen', 'commandcode'] as const)('round-trips %s custom endpoints, headers and rules', async (platform) => {
    const account = buildAccount()
    account.platform = platform
    const api_base_urls = { chat_completions: 'https://relay.example/chat', anthropic: 'https://relay.example/messages', ...(platform === 'qianwen' ? { responses: 'https://relay.example/responses' } : {}) }
    account.credentials = { api_key: 'sk-sp-test-only', account_mode: 'payg', api_protocol: 'adaptive', base_url: api_base_urls.chat_completions, api_base_urls, protocol_rules: [{ pattern: 'anthropic/claude-*', protocol: 'anthropic' }], header_override_enabled: true, header_overrides: { 'x-relay-id': 'm4' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    if (platform === 'qianwen') expect(wrapper.find('[data-testid="qianwen-coding-plan-scope"]').exists()).toBe(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject(account.credentials)
  })

  it('preserves OpenCode Zen account type and endpoints on submit', async () => {
    const account = buildAccount()
    account.platform = 'opencode_go'
    account.credentials = {
      api_key: 'sk-opencode',
      account_mode: 'zen',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/v1',
        anthropic: 'https://opencode.ai/zen',
        responses: 'https://opencode.ai/zen/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'claude-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'zen',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/v1',
        anthropic: 'https://opencode.ai/zen',
        responses: 'https://opencode.ai/zen/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'claude-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    })
  })

  it('keeps legacy OpenCode mode as Go', async () => {
    const account = buildAccount()
    account.platform = 'opencode_go'
    account.credentials = {
      api_key: 'sk-opencode',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/go/v1',
        anthropic: 'https://opencode.ai/zen/go',
        responses: 'https://opencode.ai/zen/go/v1'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'go',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1'
    })
  })

  it('preserves adaptive Kimi Responses endpoint on submit', async () => {
    const account = buildAccount()
    account.platform = 'kimi'
    account.credentials = {
      api_key: 'sk-kimi',
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.moonshot.cn/v1',
      api_base_urls: {
        chat_completions: 'https://api.moonshot.cn/v1',
        anthropic: 'https://api.moonshot.cn/anthropic',
        responses: 'https://api.moonshot.cn/v1'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.moonshot.cn/v1',
      api_base_urls: {
        chat_completions: 'https://api.moonshot.cn/v1',
        anthropic: 'https://api.moonshot.cn/anthropic',
        responses: 'https://api.moonshot.cn/v1'
      }
    })
  })

  it('preserves adaptive GLM endpoints on submit', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://open.bigmodel.cn/api/coding/paas/v4',
      api_base_urls: {
        chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
        anthropic: 'https://open.bigmodel.cn/api/anthropic'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://open.bigmodel.cn/api/coding/paas/v4',
      api_base_urls: {
        chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
        anthropic: 'https://open.bigmodel.cn/api/anthropic'
      }
    })
  })

  it.each([
    ['explicit Chat Completions', 'chat_completions'],
    ['legacy missing protocol', undefined]
  ])('preserves a custom CN relay for %s accounts', async (_name, storedProtocol) => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      base_url: 'https://relay.example.com/v1'
    }
    if (storedProtocol) {
      account.credentials.api_protocol = storedProtocol
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const submittedCredentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(submittedCredentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'chat_completions',
      base_url: 'https://relay.example.com/v1'
    })
    expect(submittedCredentials).not.toHaveProperty('api_base_urls')
  })

  it('preserves the global profile and invisible native overrides on an ordinary edit', async () => {
    const account = buildAccount()
    account.platform = 'kimi'
    account.credentials_status = { has_api_key: true }
    account.credentials = { api_key_ref: 'vault:global', account_mode: 'payg', api_protocol: 'anthropic', base_url: 'https://api.moonshot.ai/anthropic', api_base_urls: { anthropic: 'https://api.moonshot.ai/anthropic', responses: 'https://private.example/responses', extension: 'keep' }, model_mapping: { Exact: 'Exact' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0][1].credentials
    expect(credentials.api_key_ref).toBe('vault:global')
    expect(credentials.base_url).toBe('https://api.moonshot.ai/anthropic')
    expect(credentials.api_base_urls).toEqual(account.credentials.api_base_urls)
    expect(credentials.model_mapping).toEqual({ Exact: 'Exact' })
  })

  it('uses the legacy base_url when adaptive endpoints are missing', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: '   '
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: 'https://relay.example.com/v1'
      }
    })
  })

  it.each([
    ['opencode_go', 'adaptive'],
    ['qianwen', 'adaptive'],
    ['commandcode', 'adaptive'],
    ['kimi', 'chat_completions'],
    ['zhipu', 'chat_completions'],
    ['deepseek', 'chat_completions'],
    ['minimax', 'chat_completions']
  ] as const)('hydrates missing protocols using backend platform defaults (%s)', async (platform, expectedProtocol) => {
    const account = buildAccount()
    account.platform = platform
    account.credentials = { api_key: 'sk-cn', account_mode: 'payg', base_url: 'https://relay.example.com/v1' }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await flushPromises()

    // 回填后的选项与保存值一致，而不是新建默认的一套。
    const pressed = wrapper.get('[data-testid="edit-apiProtocol"]').findAll('button').filter(button => button.attributes('aria-checked') === 'true')
    expect(pressed).toHaveLength(1)
    expect(pressed[0]!.text()).toContain(`admin.accounts.cnProviders.apiProtocol.${expectedProtocol === 'adaptive' ? 'adaptive' : 'chatCompletions'}`)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials.api_protocol).toBe(expectedProtocol)
    // 缺失协议不改 legacy base_url 的含义：固定协议继续用它，Adaptive 落入 chat 槽位。
    expect(credentials.base_url).toBe('https://relay.example.com/v1')
    if (expectedProtocol === 'adaptive') {
      expect(credentials.api_base_urls.chat_completions).toBe('https://relay.example.com/v1')
    } else {
      expect(credentials).not.toHaveProperty('api_base_urls')
    }
  })

  it.each([
    ['opencode_go', true],
    ['qianwen', true],
    ['kimi', true],
    ['deepseek', true],
    ['minimax', true],
    ['zhipu', true],
    ['commandcode', true]
  ] as const)('keeps an explicit protocol and custom endpoints when editing %s', async (platform, supportsNativeResponses) => {
    const account = buildAccount()
    account.platform = platform
    account.credentials = {
      api_key: 'sk-cn',
      account_mode: 'payg',
      api_protocol: 'chat_completions',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: 'https://relay.example.com/v1',
        anthropic: 'https://relay.example.com/anthropic',
        responses: 'https://relay.example.com/responses'
      }
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await flushPromises()

    const labels = wrapper.get('[data-testid="edit-apiProtocol"]').findAll('button').map(button => button.text())
    expect(labels.filter(text => text.includes('admin.accounts.cnProviders.apiProtocol.responses'))).toHaveLength(supportsNativeResponses ? 1 : 0)
    const pressed = wrapper.get('[data-testid="edit-apiProtocol"]').findAll('button').filter(button => button.attributes('aria-checked') === 'true')
    expect(pressed).toHaveLength(1)
    expect(pressed[0]!.text()).toContain('admin.accounts.cnProviders.apiProtocol.chatCompletions')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials.api_protocol).toBe('chat_completions')
    expect(credentials.base_url).toBe('https://relay.example.com/v1')
    if (platform === 'opencode_go' || platform === 'commandcode') expect(credentials).not.toHaveProperty('api_base_urls')
    else expect(credentials.api_base_urls).toEqual(account.credentials.api_base_urls)
  })

  it.each([
    ['kimi', 'responses'],
    ['deepseek', 'responses'],
    ['minimax', 'responses'],
    ['opencode_go', 'responses'],
    ['qianwen', 'responses'],
    ['zhipu', 'responses'],
    ['commandcode', 'responses']
  ] as const)('resolves a stored Responses protocol by platform support (%s)', async (platform, expectedProtocol) => {
    const account = buildAccount()
    account.platform = platform
    account.credentials = { api_key: 'sk-cn', account_mode: 'payg', api_protocol: 'responses', base_url: 'https://relay.example.com/v1' }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    await flushPromises()

    const pressed = wrapper.get('[data-testid="edit-apiProtocol"]').findAll('button').filter(button => button.attributes('aria-checked') === 'true')
    expect(pressed).toHaveLength(1)
    expect(pressed[0]!.text()).toContain(`admin.accounts.cnProviders.apiProtocol.${expectedProtocol === 'adaptive' ? 'adaptive' : expectedProtocol === 'responses' ? 'responses' : 'chatCompletions'}`)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials.api_protocol).toBe(expectedProtocol)
    expect(credentials.base_url).toBe('https://relay.example.com/v1')
    if (expectedProtocol === 'adaptive') {
      expect(credentials.api_base_urls.chat_completions).toBe('https://relay.example.com/v1')
    } else {
      expect(credentials).not.toHaveProperty('api_base_urls')
    }
  })

  it('switches account mode through described cards and saves the selection', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = { api_key: 'sk-glm', account_mode: 'payg', api_protocol: 'chat_completions' }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    const cards = wrapper.get('[data-testid="edit-accountMode"]').findAll('button')
    expect(cards).toHaveLength(2)
    expect(cards[0].attributes('aria-checked')).toBe('true')
    expect(cards[1].text()).toContain('admin.accounts.cnProviders.accountMode.codingDesc')
    await cards[1].trigger('click')
    expect(cards[1].attributes('aria-checked')).toBe('true')
    expect(cards[0].attributes('aria-checked')).toBe('false')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials.account_mode).toBe('coding')
    wrapper.unmount()
  })

  it('carries a fixed Chat relay into Adaptive when the user switches protocols', async () => {
    const account = buildAccount()
    account.platform = 'zhipu'
    account.credentials = {
      api_key: 'sk-glm',
      account_mode: 'payg',
      api_protocol: 'chat_completions',
      base_url: 'https://relay.example.com/v1'
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    const adaptiveButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.accounts.cnProviders.apiProtocol.adaptive'))
    expect(adaptiveButton).toBeDefined()
    expect(adaptiveButton!.text()).toContain('admin.accounts.cnProviders.apiProtocol.adaptiveDesc')
    await adaptiveButton!.trigger('click')
    expect(adaptiveButton!.attributes('aria-checked')).toBe('true')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: 'https://relay.example.com/v1',
      api_base_urls: {
        chat_completions: 'https://relay.example.com/v1'
      }
    })
  })

  it.each([
    {
      name: 'Anthropic',
      platform: 'zhipu',
      protocol: 'anthropic',
      baseUrl: 'https://relay.example.com/anthropic',
      expectedBaseUrl: '',
      expectedProtocolUrls: {
        anthropic: 'https://relay.example.com/anthropic'
      }
    },
    {
      name: 'Responses',
      platform: 'deepseek',
      protocol: 'responses',
      baseUrl: 'https://relay.example.com/responses',
      expectedBaseUrl: '',
      expectedProtocolUrls: {
        responses: 'https://relay.example.com/responses'
      }
    }
  ])('keeps a fixed $name relay in its protocol slot when switching to Adaptive', async (testCase) => {
    const account = buildAccount()
    account.platform = testCase.platform
    account.credentials = {
      api_key: 'sk-cn',
      account_mode: 'payg',
      api_protocol: testCase.protocol,
      base_url: testCase.baseUrl
    }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)
    const adaptiveButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('admin.accounts.cnProviders.apiProtocol.adaptive'))
    expect(adaptiveButton).toBeDefined()
    await adaptiveButton!.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      api_protocol: 'adaptive',
      base_url: testCase.expectedBaseUrl,
      api_base_urls: testCase.expectedProtocolUrls
    })
  })

  it('preserves model mappings when editing the whitelist', async () => {
    const account = buildAccount()
    account.credentials.model_mapping = {
      'gpt-5.2': 'gpt-5.2',
      'gpt-latest': 'gpt-5.2'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      'gpt-5.2-2025-12-11': 'gpt-5.2-2025-12-11',
      'gpt-latest': 'gpt-5.2'
    })
  })

  it('submits OpenAI compact mode and compact-only model mapping', async () => {
    const account = buildAccount()
    account.extra = {
      openai_compact_mode: 'force_on'
    }
    account.credentials = {
      ...account.credentials,
      compact_model_mapping: {
        'gpt-5.4': 'gpt-5.4-openai-compact'
      }
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_compact_mode).toBe('force_on')
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.compact_model_mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-openai-compact'
    })
  })

  it('loads and submits the per-account OpenAI long-context billing toggle', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('true')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('loads and clears the OAuth-only Codex namespace flatten toggle', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.extra = {
      openai_responses_flatten_namespaces: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="edit-openai-flatten-namespaces-toggle"]')

    // 关闭后应从 extra 中删除该键，而不是写入 false
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'openai_responses_flatten_namespaces'
    )
  })

  it('submits the Codex namespace flatten toggle when switched on', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="edit-openai-flatten-namespaces-toggle"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_flatten_namespaces).toBe(
      true
    )
  })

  it('writes the upstream request id header into extra only when it changes', async () => {
    const account = buildAccount()
    account.extra = { openai_compact_mode: 'force_on' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const untouched = mountModal(account)
    await untouched.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.upstream_request_id_header).toBeUndefined()

    updateAccountMock.mockClear()
    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue(' X-Oneapi-Request-Id ')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toMatchObject({
      openai_compact_mode: 'force_on',
      upstream_request_id_header: 'X-Oneapi-Request-Id'
    })
  })

  it('removes the upstream request id header from extra when cleared', async () => {
    const account = buildAccount()
    account.extra = { upstream_request_id_header: 'X-Request-ID' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    expect((wrapper.get('[data-testid="upstream-request-id-header"]').element as HTMLInputElement).value).toBe('X-Request-ID')
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toBeDefined()
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('upstream_request_id_header')
  })

  it('writes images_url_to_b64_json into extra when toggled on', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.images_url_to_b64_json).toBe(true)
  })

  it('removes images_url_to_b64_json from extra when toggled off', async () => {
    const account = buildAccount()
    account.extra = { images_url_to_b64_json: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).toBeDefined()
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('images_url_to_b64_json')
  })

  it('hides the Codex namespace flatten toggle for non-OAuth OpenAI accounts', async () => {
    const account = buildAccount()
    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="edit-openai-flatten-namespaces-toggle"]').exists()).toBe(
      false
    )
  })

  it('defaults legacy OpenAI accounts to long-context billing disabled', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('does not render or submit the long-context billing toggle for Spark shadow accounts', async () => {
    const account = buildOpenAISparkShadowAccount()
    account.extra = {
      openai_long_context_billing_enabled: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'openai_long_context_billing_enabled'
    )
  })

  it('preserves an explicit OpenAI long-context billing opt-out', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="openai-long-context-billing-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('fails closed for malformed OpenAI long-context billing values', async () => {
    const account = buildAccount()
    account.extra = {
      openai_long_context_billing_enabled: 'false'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="openai-long-context-billing-toggle"]').attributes('aria-checked')).toBe('false')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('loads and submits Grok OAuth model mapping edits', async () => {
    vi.mocked(getPlatformModelPresets).mockResolvedValue([
      { platform: 'grok', provider_profile: 'xai', models: [
        { id: 'grok-imagine-image', display_name: 'Imagine Image' },
        { id: 'grok-imagine-video', display_name: 'Imagine Video' }
      ] }
    ] as Awaited<ReturnType<typeof getPlatformModelPresets>>)

    const account = buildGrokOAuthAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await flushPromises()
    expect(wrapper.text()).toContain('Imagine Image')
    expect(wrapper.text()).toContain('Imagine Video')
    vi.mocked(getPlatformModelPresets).mockResolvedValue([])

    const inputWithValue = (value: string) => {
      const input = wrapper
        .findAll('input')
        .find((input) => (input.element as HTMLInputElement).value === value)
      expect(input).toBeTruthy()
      return input!
    }

    await inputWithValue('grok-latest').setValue('grok')
    await inputWithValue('grok-4.3').setValue('grok-build-0.1')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      grok: 'grok-build-0.1'
    })
  })

  it('uses the official xAI base URL when a Grok API-key account omits base_url', async () => {
    const account = buildGrokAPIKeyAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect((wrapper.get('input[placeholder="https://api.x.ai/v1"]').element as HTMLInputElement).value)
      .toBe('https://api.x.ai/v1')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.base_url).toBe('https://api.x.ai/v1')
  })

  it('only submits model mapping credentials when saving an OpenAI spark shadow account', async () => {
    authIsSimpleMode.value = false
    const account = buildOpenAISparkShadowAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="set-shadow-group"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.group_ids).toEqual([7])
    expect(payload?.credentials).toEqual({
      model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark'
      },
      compact_model_mapping: {
        'gpt-5.3-codex-spark': 'gpt-5.3-codex-spark-compact'
      }
    })
  })

  it('submits OpenAI APIKey Responses support override mode', async () => {
    const account = buildAccount()
    account.extra = {
      openai_responses_mode: 'force_chat_completions',
      openai_responses_supported: false
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="openai-responses-mode-select"]').setValue('force_responses')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_mode).toBe('force_responses')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(false)
  })

  it('submits the account upstream billing auto-probe setting', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.upstream_billing_probe_enabled).toBe(true)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty(
      'upstream_billing_probe_enabled'
    )
  })

  it.each(['grok', 'qianwen', 'commandcode'] as const)('exposes the upstream billing auto-probe toggle for %s API-key accounts', async (platform) => {
    // 探测已放宽到全部 API-key 平台：grok 账号同样能开启并保存。
    const account = buildAccount()
    account.platform = platform
    account.name = `${platform}-relay`
    account.credentials = { api_key: 'sk-grok', base_url: 'https://relay.example/v1' }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const toggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    expect(toggle.attributes('aria-checked')).toBe('false')

    await toggle.trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('enabling rate sync also enables probing and stops submitting a manual rate', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const syncToggle = wrapper.get('[data-testid="upstream-billing-rate-sync"]')
    const probeToggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    const rateInput = wrapper.get<HTMLInputElement>('[data-testid="account-rate-multiplier"]')
    expect(syncToggle.attributes('aria-checked')).toBe('false')
    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(rateInput.element.disabled).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.billingRateMultiplierHint')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBilling.syncRateManagedHint')

    await syncToggle.trigger('click')
    expect(syncToggle.attributes('aria-checked')).toBe('true')
    expect(probeToggle.attributes('aria-checked')).toBe('true')
    expect(rateInput.element.disabled).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.syncRateManagedHint')
    expect(wrapper.text()).not.toContain('admin.accounts.billingRateMultiplierHint')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(true)
    expect(payload).not.toHaveProperty('rate_multiplier')
  })

  it('disabling probing also disables rate sync and restores manual rate editing', async () => {
    const account = buildAccount()
    account.extra = {
      upstream_billing_probe_enabled: true,
      upstream_billing_rate_sync_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const syncToggle = wrapper.get('[data-testid="upstream-billing-rate-sync"]')
    const probeToggle = wrapper.get('[data-testid="upstream-billing-auto-probe"]')
    const rateInput = wrapper.get<HTMLInputElement>('[data-testid="account-rate-multiplier"]')
    expect(syncToggle.attributes('aria-checked')).toBe('true')
    expect(rateInput.element.disabled).toBe(true)

    await probeToggle.trigger('click')
    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(syncToggle.attributes('aria-checked')).toBe('false')
    expect(rateInput.element.disabled).toBe(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(false)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(false)
    expect(payload?.rate_multiplier).toBe(1)
  })

  it('disabling only rate sync keeps automatic probing enabled', async () => {
    const account = buildAccount()
    account.extra = {
      upstream_billing_probe_enabled: true,
      upstream_billing_rate_sync_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="upstream-billing-rate-sync"]').trigger('click')
    expect(wrapper.get('[data-testid="upstream-billing-auto-probe"]').attributes('aria-checked')).toBe(
      'true'
    )
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    expect(payload?.upstream_billing_rate_sync_enabled).toBe(false)
    expect(payload?.rate_multiplier).toBe(1)
  })

  it('clears OpenAI APIKey Responses override when set back to auto', async () => {
    const account = buildAccount()
    account.extra = {
      openai_responses_mode: 'force_chat_completions',
      openai_responses_supported: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="openai-responses-mode-select"]').setValue('auto')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('openai_responses_mode')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(true)
  })

  it('submits OpenAI APIKey endpoint capabilities from credentials', async () => {
    const account = buildAccount()
    account.credentials.openai_capabilities = ['chat_completions']
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.findAll('input[type="checkbox"]').some((input) => (input.element as HTMLInputElement).checked)).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'chat_completions'
    ])
  })

	it('submits OpenAI quota auto-pause thresholds in extra', async () => {
	  const account = buildAccount()
	  account.extra = {
		auto_pause_5h_threshold: 0.9,
		auto_pause_7d_threshold: 0.8
	  }
	  updateAccountMock.mockReset()
	  checkMixedChannelRiskMock.mockReset()
	  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
	  updateAccountMock.mockResolvedValue(account)

	  const wrapper = mountModal(account)

	  await wrapper.get('[data-testid="auto-pause-5h-threshold"]').setValue('95')
	  await wrapper.get('[data-testid="auto-pause-7d-threshold"]').setValue('96')
	  await wrapper.get('form#edit-account-form').trigger('submit.prevent')

	  expect(updateAccountMock).toHaveBeenCalledTimes(1)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_5h_threshold).toBe(0.95)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_7d_threshold).toBe(0.96)
	})

	it('submits OpenAI quota auto-pause disable flag in extra', async () => {
	  // Toggling the per-account disable flag must persist as auto_pause_5h_disabled
	  // so an admin can exempt one account from auto-pause even when a global default
	  // threshold is configured (otherwise leaving the threshold blank would silently
	  // fall back to the global default).
	  const account = buildAccount()
	  updateAccountMock.mockReset()
	  checkMixedChannelRiskMock.mockReset()
	  checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
	  updateAccountMock.mockResolvedValue(account)

	  const wrapper = mountModal(account)

	  await wrapper.get('[data-testid="auto-pause-5h-disabled"]').trigger('click')
	  await wrapper.get('form#edit-account-form').trigger('submit.prevent')

	  expect(updateAccountMock).toHaveBeenCalledTimes(1)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_5h_disabled).toBe(true)
	  expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.auto_pause_7d_disabled).toBeUndefined()
	})

  it('keeps at least one OpenAI APIKey endpoint capability selected', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const chatCheckbox = wrapper.get<HTMLInputElement>(
      '[data-testid="openai-endpoint-capability-chat_completions"]'
    )
    const embeddingsCheckbox = wrapper.get<HTMLInputElement>(
      '[data-testid="openai-endpoint-capability-embeddings"]'
    )

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(true)

    await embeddingsCheckbox.setValue(false)

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(false)

    await chatCheckbox.setValue(false)

    expect(chatCheckbox.element.checked).toBe(true)
    expect(embeddingsCheckbox.element.checked).toBe(false)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'chat_completions'
    ])
  })

  it('disables text generation protocol when only embeddings requests are accepted', async () => {
    const account = buildAccount()
    account.credentials.openai_capabilities = ['embeddings']
    account.extra = {
      openai_responses_mode: 'force_responses',
      openai_responses_supported: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    const responsesModeSelect = wrapper.get<HTMLSelectElement>(
      '[data-testid="openai-responses-mode-select"]'
    )

    expect(responsesModeSelect.element.disabled).toBe(true)
    expect(wrapper.find('[data-testid="openai-responses-mode-not-applicable"]').exists()).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.openai_capabilities).toEqual([
      'embeddings'
    ])
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('openai_responses_mode')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_responses_supported).toBe(true)
  })

  it('submits Codex image tool force-inject mode as bridge override', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_bridge: false,
      codex_image_generation_bridge_enabled: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageTool')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolDesc')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolEnabledDesc')

    await wrapper.get('button[data-testid="codex-image-tool-enabled"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_bridge).toBe(true)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge_enabled')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
  })

  it('submits Codex image tool no-injection mode without strip policy', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('button[data-testid="codex-image-tool-disabled"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_bridge).toBe(false)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
  })

  it('submits Codex image tool block mode as strip policy and clears bridge override', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_bridge: true
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolBlock')
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolBlockDesc')

    await wrapper.get('button[data-testid="codex-image-tool-block"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.codex_image_generation_explicit_tool_policy).toBe('strip')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge')
  })

  it('loads strip policy as block mode and clears both keys when reset to inherit', async () => {
    const account = buildAccount()
    account.extra = {
      codex_image_generation_explicit_tool_policy: 'strip'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('button[data-testid="codex-image-tool-inherit"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_explicit_tool_policy')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra).not.toHaveProperty('codex_image_generation_bridge')
  })

  it('setup-token account can select and submit OAuth WS mode', async () => {
    const account = buildOpenAISetupTokenAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="edit-openai-ws-mode-select"]').setValue('http_bridge')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_oauth_responses_websockets_v2_mode).toBe('http_bridge')
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_oauth_responses_websockets_v2_enabled).toBe(true)
  })

  it('allows saving apikey account when backend redacted api_key but credentials_status reports it exists', async () => {
    // 新前端 + 新后端：响应已脱敏，credentials 里没有 api_key，credentials_status.has_api_key=true
    const account = buildAccount()
    account.credentials = {
      base_url: 'https://api.openai.com',
      model_mapping: { 'gpt-5.2': 'gpt-5.2' }
    }
    account.credentials_status = { has_api_key: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    // 用户未输入新 key 时，payload 不应带 api_key，由后端合并保留旧值
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('api_key')
  })

  it('allows saving apikey account against legacy backend without credentials_status', async () => {
    // 新前端 + 旧后端：credentials_status 缺失，但 credentials.api_key 仍是明文，应允许保存
    const account = buildAccount()
    // 显式确保没有 credentials_status
    expect(account.credentials_status).toBeUndefined()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    // 旧后端响应未脱敏，原 api_key 会随 currentCredentials 一起传回去（旧行为，等价于无操作）
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.api_key).toBe('sk-test')
  })

  it('blocks apikey save when neither credentials_status nor legacy api_key indicates existence', async () => {
    const account = buildAccount()
    account.credentials = {
      base_url: 'https://api.openai.com'
    }
    // 既没有 credentials_status 也没有旧的 api_key
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).not.toHaveBeenCalled()
  })

  it('allows saving Vertex SA account when backend redacted service_account_json but credentials_status reports it exists', async () => {
    // 新前端 + 新后端：响应已脱敏，credentials 里没有 service_account_json，credentials_status.has_service_account_json=true
    const account = buildVertexAccount()
    account.credentials = {
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    }
    account.credentials_status = { has_service_account_json: true }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.project_id).toBe('demo-project')
  })

  it('allows saving Vertex SA account against legacy backend without credentials_status', async () => {
    // 新前端 + 旧后端：credentials_status 缺失，但 credentials.service_account_json 仍是明文，应允许保存
    const account = buildVertexAccount()
    expect(account.credentials_status).toBeUndefined()
    expect(account.credentials.service_account_json).toBeTruthy()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
  })

  it('blocks Vertex SA save when neither credentials_status nor legacy json indicates existence', async () => {
    const account = buildVertexAccount()
    account.credentials = {
      project_id: 'demo-project',
      client_email: 'sa@example.iam.gserviceaccount.com',
      location: 'us-central1',
      tier_id: 'vertex'
    }
    // 既没有 credentials_status 也没有旧的 service_account_json
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).not.toHaveBeenCalled()
  })

  it('loads and submits Antigravity configured project fallback', async () => {
    const account = buildAntigravityAccount('configured-project')
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('[data-testid="antigravity-project-id-input"]')
    expect(input.element.value).toBe('configured-project')

    await input.setValue('  updated-project  ')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.antigravity_project_id).toBe(
      'updated-project'
    )
  })

  it('clears Antigravity configured project fallback when input is empty', async () => {
    const account = buildAntigravityAccount('configured-project')
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const input = wrapper.get<HTMLInputElement>('[data-testid="antigravity-project-id-input"]')

    await input.setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty(
      'antigravity_project_id'
    )
  })
})

describe('EditAccountModal OpenAI 自动使用重置卡', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('仅对 OpenAI OAuth 母账号显示，默认关闭且阈值为 100/100', () => {
    const parent = mountModal(buildOpenAIOAuthParentAccount())
    expect(parent.find('[data-testid="auto-reset-credit-settings"]').exists()).toBe(true)
    expect((parent.get('[data-testid="auto-reset-credit-5h-threshold"]').element as HTMLInputElement).value).toBe('100')
    expect((parent.get('[data-testid="auto-reset-credit-7d-threshold"]').element as HTMLInputElement).value).toBe('100')
    expect(parent.get('[data-testid="auto-reset-credit-5h-threshold"]').attributes('disabled')).toBeDefined()
    parent.unmount()

    for (const account of [buildAccount(), buildOpenAISetupTokenAccount(), buildOpenAISparkShadowAccount()]) {
      const wrapper = mountModal(account)
      expect(wrapper.find('[data-testid="auto-reset-credit-settings"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('独立保存两个阈值，并禁止把运行态回写到管理请求', async () => {
    const account = buildOpenAIOAuthParentAccount()
    account.extra = {
      codex_auto_reset_credit_state: {
        status: 'success',
        trigger_window: '5h',
        available_count: 1
      }
    }
    updateAccountMock.mockResolvedValue(account)
    const wrapper = mountModal(account)

    await wrapper.get('[data-testid="auto-reset-credit-enabled"]').trigger('click')
    await wrapper.get('[data-testid="auto-reset-credit-5h-threshold"]').setValue('75.5')
    await wrapper.get('[data-testid="auto-reset-credit-7d-threshold"]').setValue('92')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const extra = updateAccountMock.mock.calls[0]?.[1]?.extra
    expect(extra).toMatchObject({
      auto_reset_credit_enabled: true,
      auto_reset_credit_5h_threshold: 0.755,
      auto_reset_credit_7d_threshold: 0.92
    })
    expect(extra).not.toHaveProperty('codex_auto_reset_credit_state')
    wrapper.unmount()
  })

  it('开启后拒绝超出 0.1–100 范围的任一阈值', async () => {
    const wrapper = mountModal(buildOpenAIOAuthParentAccount())
    await wrapper.get('[data-testid="auto-reset-credit-enabled"]').trigger('click')
    await wrapper.get('[data-testid="auto-reset-credit-5h-threshold"]').setValue('0')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('enables billing rate synchronization for a Cursor relay', async () => {
    const account = buildAccount()
    account.platform = 'cursor'
    account.type = 'upstream'
    account.credentials = { api_key: 'relay-key', base_url: 'https://relay.example/v1', api_protocol: 'adaptive' }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="upstream-billing-rate-sync"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]).toMatchObject({ upstream_billing_probe_enabled: true, upstream_billing_rate_sync_enabled: true })
    expect(updateAccountMock.mock.calls[0]?.[1]?.rate_multiplier).toBeUndefined()
  })

})

it.each([
  ['configuration', 'Configuration'], ['credentials', 'Credentials'],
  ['unsupported', 'Unsupported'], ['upstream', 'Unavailable'], ['internal', 'Failed']
])('classifies Antigravity sync %s errors', async (reason, key) => {
  showErrorMock.mockReset()
  syncUpstreamModelsMock.mockRejectedValueOnce({ reason: `upstream_model_sync_${reason}`, message: 'normalized error' })
  const wrapper = mountModal(buildAntigravityAccount(), false, true)
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
  await flushPromises()
  expect(showErrorMock).toHaveBeenCalledWith(`admin.accounts.syncUpstreamModels${key}`)
  wrapper.unmount()
})


describe('Cursor lossless modal submissions', () => {
  beforeEach(() => {
    updateAccountMock.mockReset().mockResolvedValue({})
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    showErrorMock.mockReset()
  })
  it.each(['oauth', 'apikey'] as const)('preserves mixed mappings without relay fields when renaming %s', async (type) => {
    const account = buildAccount()
    account.platform = 'cursor'
    account.type = type
    account.credentials_status = { has_api_key: true }
    account.credentials = { model_mapping: { ExactModel: 'ExactModel', 'alias-*': 'TargetModel' } }
    const wrapper = mountModal(account)
    await wrapper.get('input').setValue('Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toMatchObject({ model_mapping: { ExactModel: 'ExactModel', 'alias-*': 'TargetModel' } })
    for (const relayField of ['api_protocol', 'base_url', 'api_base_urls']) {
      expect(credentials).not.toHaveProperty(relayField)
    }
    wrapper.unmount()
  })
  it.each(['oauth', 'apikey'] as const)('does not carry historical relay fields from a mixed native %s account', async (type) => {
    const account = buildAccount()
    account.platform = 'cursor'
    account.type = type
    account.credentials = { api_key: 'stale-key', access_token: 'stale-token', base_url: 'https://stale.example/v1', api_protocol: 'anthropic', api_base_urls: { anthropic: 'https://stale.example/anthropic' }, model_mapping: { ExactModel: 'ExactModel' } }
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials).toMatchObject({ model_mapping: { ExactModel: 'ExactModel' } })
    for (const relayField of ['api_protocol', 'base_url', 'api_base_urls', 'api_key', 'access_token']) {
      expect(credentials).not.toHaveProperty(relayField)
    }
    wrapper.unmount()
  })
  it('round trips Cursor Chat without replacing its key', async () => {
    const storedMapping = { ExactModel: 'ExactModel', 'alias-*': 'TargetModel' }
    const account = { ...buildAccount(), platform: 'cursor', type: 'upstream', credentials_status: { has_api_key: true }, credentials: { base_url: 'https://relay.example/v1', api_protocol: 'chat_completions', model_mapping: storedMapping } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()

    // 打开时回填 Chat：不回显秘密，key 留空也不要求重输。
    expect(wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="cursor-relay-protocol-adaptive"]').attributes('aria-checked')).toBe('false')
    const apiKeyInput = wrapper.get<HTMLInputElement>('[data-testid="cursor-api-key"]')
    expect(apiKeyInput.element.value).toBe('')
    expect(apiKeyInput.attributes('required')).toBeUndefined()

    await wrapper.get('input').setValue('Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toEqual({
      base_url: 'https://relay.example/v1',
      api_protocol: 'chat_completions',
      model_mapping: storedMapping
    })
    wrapper.unmount()

    // 重新载入仍为 Chat；切到 Adaptive 再保存/载入亦一致，且地址不被重置。
    const saved = { ...account, credentials: { ...account.credentials, ...updateAccountMock.mock.calls[0]?.[1]?.credentials } }
    const reopened = mountModal(saved)
    await flushPromises()
    expect(reopened.get('[data-testid="cursor-relay-protocol-chat_completions"]').attributes('aria-checked')).toBe('true')
    await reopened.get('[data-testid="cursor-relay-protocol-adaptive"]').trigger('click')
    expect(reopened.get('[data-testid="cursor-relay-protocol-adaptive"]').attributes('aria-checked')).toBe('true')
    await reopened.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(2)
    expect(updateAccountMock.mock.calls[1]?.[1]?.credentials).toEqual({ base_url: 'https://relay.example/v1', api_protocol: 'adaptive', model_mapping: storedMapping })
    reopened.unmount()

    // replace：显式输入新 Key 后提交新值（keep 语义只针对留空）。
    const adaptiveSaved = { ...saved, credentials: { ...saved.credentials, api_protocol: 'adaptive' } }
    const replaced = mountModal(adaptiveSaved)
    await flushPromises()
    expect(replaced.get('[data-testid="cursor-relay-protocol-adaptive"]').attributes('aria-checked')).toBe('true')
    await replaced.get('[data-testid="cursor-api-key"]').setValue('crsr_replaced')
    await replaced.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(3)
    expect(updateAccountMock.mock.calls[2]?.[1]?.credentials).toEqual({ api_key: 'crsr_replaced', base_url: 'https://relay.example/v1', api_protocol: 'adaptive', model_mapping: storedMapping })
    replaced.unmount()
  })
  it('requires an explicit supported protocol for a legacy relay', async () => {
    const account = { ...buildAccount(), platform: 'cursor', type: 'upstream', credentials_status: { has_api_key: true }, credentials: { base_url: 'https://relay.example/v1', api_protocol: 'anthropic', model_mapping: { ExactModel: 'ExactModel' } } } as any
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    showErrorMock.mockReset()
    const wrapper = mountModal(account)
    await flushPromises()

    // 不预选合法值：原值只读回显在提示里，网格无选中项。
    for (const button of wrapper.get('[data-testid="cursor-relay-protocol"]').findAll('button')) {
      expect(button.attributes('aria-checked')).toBe('false')
    }
    const unsupported = wrapper.get('[data-testid="cursor-relay-protocol-unsupported"]')
    expect(unsupported.text()).toContain('admin.accounts.cursor.unsupportedRelayProtocol')
    expect(unsupported.text()).toContain('"protocol":"anthropic"')

    // 只改名称直接保存：不发任何请求，也不静默换协议。
    await wrapper.get('input').setValue('Renamed')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(checkMixedChannelRiskMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith(expect.stringContaining('admin.accounts.cursor.unsupportedRelayProtocol'))
    expect(wrapper.get('[data-testid="cursor-relay-protocol-unsupported"]').exists()).toBe(true)

    // 明确改选合法协议后才提交。
    await wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').trigger('click')
    expect(wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-testid="cursor-relay-protocol-unsupported"]').exists()).toBe(false)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toEqual({ base_url: 'https://relay.example/v1', api_protocol: 'chat_completions', model_mapping: { ExactModel: 'ExactModel' } })
    wrapper.unmount()
  })
  it('does not carry protocol or credential intents across edited accounts', async () => {
    const first = { ...buildAccount(), platform: 'cursor', type: 'upstream', credentials_status: { has_api_key: true }, credentials: { base_url: 'https://first.example/v1', api_protocol: 'chat_completions' } } as any
    const second = { ...buildAccount(), id: 2, platform: 'zhipu', credentials_status: { has_api_key: true }, credentials: { account_mode: 'coding', api_protocol: 'adaptive', base_url: 'https://second.example/v1' } } as any
    updateAccountMock.mockReset().mockResolvedValue(second)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(first)
    await flushPromises()
    expect(wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('typed-for-first')
    await wrapper.get('[data-testid="cursor-relay-protocol-adaptive"]').trigger('click')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ account: second, show: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="cursor-key-mode"]').exists()).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-account-field="editApiKey"]').element.value).toBe('')
    expect(wrapper.findAll('[data-testid="edit-apiProtocol"] button').find(button => button.attributes('aria-checked') === 'true')?.text()).toContain('admin.accounts.cnProviders.apiProtocol.adaptive')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const payload = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(payload).toMatchObject({ account_mode: 'coding', api_protocol: 'adaptive', base_url: 'https://second.example/v1' })
    expect(payload).not.toHaveProperty('api_key')
    expect(payload.api_base_urls.chat_completions).toBe('https://second.example/v1')
    wrapper.unmount()
  })
  it('does not carry mappings across sequential relay edits', async () => {
    const first = { ...buildAccount(), platform: 'cursor', type: 'upstream' }
    const second = { ...first, id: 2, credentials: { api_key: 'second', base_url: 'https://second.example/v1' } }
    const wrapper = mountModal(first)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ account: second, show: true })
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).not.toHaveProperty('model_mapping')
    wrapper.unmount()
  })
  it('blocks an invalid restriction before sending a save request', async () => {
    const account = buildAccount()
    account.credentials.model_mapping = { 'bad*source': 'target' }
    const wrapper = mountModal(account)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith(expect.stringContaining('admin.accounts.modelValidation.invalidWildcard'))
    wrapper.unmount()
  })
})


it('does not save a late Antigravity catalog into a different account', async () => {
  let resolve!: (result: { models: string[] }) => void
  syncUpstreamModelsMock.mockImplementationOnce(() => new Promise(r => { resolve = r }))
  updateAccountMock.mockReset().mockResolvedValue({})
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  const wrapper = mountModal(buildAntigravityAccount(), false, true)
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
  const second = { ...buildAntigravityAccount(), id: 987, credentials: { ...buildAntigravityAccount().credentials, model_mapping: { ExistingExact: 'ExistingExact' } } }
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ account: second, show: true })
  resolve({ candidate_models: ['late-model'], models: ['late-model'] })
  await flushPromises()
  expect(updateAccountMock).not.toHaveBeenCalled()
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock).toHaveBeenCalledTimes(1)
  expect(updateAccountMock.mock.calls[0]?.[0]).toBe(987)
  expect(updateAccountMock.mock.calls[0]?.[1]?.credentials.model_mapping).toEqual({ ExistingExact: 'ExistingExact' })
  wrapper.unmount()
})

describe('Management error contract edit confirmation', () => {
  it('shows persistent encryption setup error without reporting account update', async () => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.credentials = { api_key: 'synthetic' }
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    updateAccountMock.mockReset().mockRejectedValue({ reason: 'encryption_key_required', message: 'fallback' })
    showErrorMock.mockClear()
    showSuccessMock.mockClear()
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(showErrorMock).toHaveBeenCalledWith('common.encryptionKeyRequired')
    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
    wrapper.unmount()
  })

  it.each(['confirm', 'cancel'])('handles normalized 409 and %s without unintended writes', async action => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.credentials = { api_key: 'review-key' }
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    updateAccountMock.mockReset().mockRejectedValueOnce({ status: 409, code: 409, reason: 'mixed_channel_warning', message: 'Confirm group', metadata: { group_id: 27, group_name: 'Review group', current_platform: 'Anthropic', other_platform: 'Antigravity' } }).mockResolvedValue(account)
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const dialog = wrapper.findAllComponents(ConfirmDialog).find(component => component.props('title') === 'admin.accounts.mixedChannelWarningTitle')!
    expect(dialog.props('show')).toBe(true)
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = JSON.parse(JSON.stringify(updateAccountMock.mock.calls[0][1]))
    dialog.vm.$emit(action)
    await flushPromises()
    expect(dialog.props('show')).toBe(false)
    expect(updateAccountMock).toHaveBeenCalledTimes(action === 'confirm' ? 2 : 1)
    if (action === 'confirm') {
      expect(updateAccountMock.mock.calls[1][1]).toEqual({ ...payload, confirm_mixed_channel_risk: true })
      expect(wrapper.emitted('updated')).toHaveLength(1)
    } else {
      expect(wrapper.emitted('updated')).toBeUndefined()
    }
    wrapper.unmount()
  })
})


describe('editor candidate identity and additive discovery', () => {
  it.each([
    ['bedrock', 'bedrock', 'us-east-1'],
    ['anthropic', 'bedrock', 'eu-west-1'],
    ['gemini', 'service_account', 'us-central1']
  ])('keeps native presets out of %s/%s and permits manual IDs', async (platform, type, region) => {
    vi.mocked(getPlatformModelPresets).mockResolvedValueOnce([
      { platform: 'anthropic', provider_profile: 'native', models: [{ id: 'claude-native-fixture' }] },
      { platform: 'gemini', provider_profile: 'native', models: [{ id: 'gemini-native-fixture' }] }
    ] as any)
    const account = type === 'service_account' ? buildVertexAccount() : {
      ...buildAccount(), platform, type,
      credentials: { auth_mode: 'api_key', api_key: 'fixture', aws_region: region }
    }
    const wrapper = mountModal(account, false, true)
    await flushPromises()
    const selector = wrapper.findAllComponents(ModelWhitelistSelector).find(component => !component.props('catalogOnly'))!
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
    expect(selector.props('modelValue')).toEqual([])
    expect(selector.props('platform')).toBe(platform)
    expect(selector.props('candidateIdentity')).toMatchObject({ type, region, profile: '', mode: 'payg' })
    await selector.get('input[aria-label="admin.accounts.customModelName"]').setValue('hosted-custom-id')
    await selector.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(selector.props('modelValue')).toEqual(['hosted-custom-id'])
    wrapper.unmount()
  })

  it.each([false, true])('adds sparse discovery (complete=%s) while retaining selections and the saved snapshot', async discoveryComplete => {
    const snapshot = { discovered_models: ['saved-native'], eligible_models: [{ id: 'saved-native' }] }
    const summary = { synced: true, warnings: [], state: { rules_revision: '1' }, effective_snapshot: snapshot }
    catalogRead.mockResolvedValueOnce(summary)
    syncUpstreamModelsMock.mockReset().mockResolvedValue({
      discovered_models: ['new-native'], candidate_models: ['new-native'], eligible_models: [],
      models: [], discovery_complete: discoveryComplete, complete: false, capability_status: 'incomplete',
      review: { account_id: 1, digest: 'review' }
    })
    updateAccountMock.mockReset()
    const wrapper = mountModal(buildAccount(), false, true)
    await flushPromises()
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    const panel = wrapper.findAllComponents(ModelWhitelistSelector).at(0)!
    const add = panel.get('[data-testid="add-preview-models"]')
    expect(add.attributes('disabled')).toBeUndefined()
    expect(panel.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    await add.trigger('click')
    await flushPromises()
    expect(panel.props('modelValue')).toEqual(['gpt-5.2', 'new-native'])
    expect(panel.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    expect(panel.findComponent({ name: 'ModelCatalogDetails' }).props('result').discovered_models).toEqual(['new-native'])
    expect(panel.findComponent({ name: 'ModelCatalogDetails' }).props('summary').effective_snapshot).toEqual({
      discovered_models: ['saved-native'], eligible_models: [{ id: 'saved-native' }]
    })
    expect(syncUpstreamModelsMock.mock.calls).toEqual([[1, false]])
    expect(updateAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})


describe('late platform edit mode, protocol and endpoint persistence', () => {
  const platforms = [
    { platform: 'hunyuan', payg: 'https://tokenhub.tencentmaas.com/v1', coding: 'https://api.lkeap.cloud.tencent.com/coding/v3', anthropic: 'https://api.lkeap.cloud.tencent.com/coding/anthropic', protocols: 4 },
    { platform: 'volcengine', payg: 'https://ark.cn-beijing.volces.com/api/v3', coding: 'https://ark.cn-beijing.volces.com/api/coding/v3', anthropic: 'https://ark.cn-beijing.volces.com/api/coding', protocols: 4 },
    { platform: 'qianfan', payg: 'https://qianfan.baidubce.com/v2', coding: 'https://qianfan.baidubce.com/v2/tokenplan/personal', anthropic: 'https://qianfan.baidubce.com/anthropic/tokenplan/personal', protocols: 3 },
    { platform: 'mistral', payg: 'https://api.mistral.ai/v1', coding: null, anthropic: null, protocols: 1 }
  ]
  it.each(platforms)('normalizes legacy choices and saves the mode-aware fallback for $platform', async ({ platform, coding, payg }) => {
    const account = { ...buildAccount(), platform, credentials: { api_key: 'test-key', account_mode: 'coding', api_protocol: 'chat_completions', base_url: coding ?? payg } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('[data-account-field="editBaseUrl"]').setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ account_mode: coding ? 'coding' : 'payg', api_protocol: 'chat_completions', base_url: coding ?? payg })
    wrapper.unmount()
  })
  it.each(platforms.filter(entry => entry.coding))('saves adaptive coding endpoints for $platform', async ({ platform, payg, coding, anthropic }) => {
    const account = { ...buildAccount(), platform, credentials: { api_key: 'test-key', account_mode: 'payg', api_protocol: 'adaptive', base_url: payg } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('[data-testid="edit-accountMode"]').findAll('button')[1].trigger('click')
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const expectedEndpoints = { chat_completions: coding, ...(platform === 'volcengine' ? { responses: coding } : {}), ...(anthropic ? { anthropic } : {}) }
    expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ account_mode: 'coding', api_protocol: 'adaptive', base_url: coding })
    expect(updateAccountMock.mock.calls[0][1].credentials.api_base_urls).toEqual(expectedEndpoints)
    wrapper.unmount()
  })
  it.each(platforms)('persists supported mode and protocol edits with exact presets for $platform', async ({ platform, payg, coding, anthropic, protocols }) => {
    const account = { ...buildAccount(), platform, credentials: { api_key: 'test-key', account_mode: 'payg', api_protocol: 'chat_completions', base_url: payg } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    const modeCards = wrapper.get('[data-testid="edit-accountMode"]').findAll('[role="radio"]')
    expect(modeCards).toHaveLength(coding ? 2 : 1)
    expect(wrapper.get('[data-testid="edit-apiProtocol"]').findAll('[role="radio"]')).toHaveLength(protocols)
    expect(wrapper.findAll('[data-testid="cn-base-url-preset"]').map(preset => preset.text())).toEqual(expect.arrayContaining([expect.stringContaining(`(${payg.replace('https://', '')})`)]))
    if (coding) {
      await modeCards[1].trigger('click')
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('[data-account-field="editBaseUrl"]').element.value).toBe(coding)
      const presets = wrapper.findAll('[data-testid="cn-base-url-preset"]')
      expect(presets.map(preset => preset.text())).toEqual(expect.arrayContaining([expect.stringContaining(coding.replace('https://', ''))]))
      expect(presets.map(preset => preset.text())).not.toEqual(expect.arrayContaining([expect.stringContaining(`(${payg.replace('https://', '')})`)]))
      await presets[0].trigger('click')
    } else {
      await modeCards[0].trigger('click')
    }
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ account_mode: coding ? 'coding' : 'payg', api_protocol: 'chat_completions', base_url: coding ?? payg })
    if (coding) {
      await wrapper.setProps({ account: { ...account, credentials: { ...account.credentials, ...updateAccountMock.mock.calls[0][1].credentials } } })
      await flushPromises()
      const nextProtocol = anthropic ? 'anthropic' : 'responses'
      const nextBaseUrl = anthropic ?? coding
      updateAccountMock.mockClear()
      const protocolCards = wrapper.get('[data-testid="edit-apiProtocol"]').findAll('[role="radio"]')
      await protocolCards.find(card => card.text().includes(`apiProtocol.${nextProtocol}`))!.trigger('click')
      await flushPromises()
      expect(wrapper.get<HTMLInputElement>('[data-account-field="editBaseUrl"]').element.value).toBe(nextBaseUrl)
      expect(wrapper.findAll('[data-testid="cn-base-url-preset"]').map(preset => preset.text())).toEqual(expect.arrayContaining([expect.stringContaining(nextBaseUrl.replace('https://', ''))]))
      await wrapper.get('form#edit-account-form').trigger('submit.prevent')
      await flushPromises()
      expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ account_mode: 'coding', api_protocol: nextProtocol, base_url: nextBaseUrl })
    }
    for (const radio of wrapper.findAll('[role="radio"]')) {
      expect(radio.attributes('aria-checked')).toMatch(/^(true|false)$/)
      expect(radio.attributes()).not.toHaveProperty('aria-pressed')
    }
    wrapper.unmount()
  })
})


describe('account form semantic and localized validation regressions', () => {
  it.each([['create', createAccountSource], ['edit', editAccountSource]])('%s uses localized required Base URL errors', (_name, source) => {
    expect(source).toContain("showError(t('admin.accounts.baseUrlRequired'))")
    expect(source).not.toContain("showError('Base URL is required.')")
    expect(englishAccounts.accounts.baseUrlRequired).toBe('Base URL is required.')
    expect(chineseAccounts.accounts.baseUrlRequired).toBe('请填写 Base URL。')
  })
  it('never assigns toggle button aria-pressed to editing radio cards', () => {
    expect(editAccountSource).not.toContain(':aria-pressed=')
  })
})


describe('account editor organization', () => {
  it('keeps drafts mounted through folds and saves only account settings', async () => {
    updateAccountMock.mockReset().mockResolvedValue(buildAccount())
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    allowlistWrite.mockClear()
    syncUpstreamModelsMock.mockClear()
    const wrapper = mountModal(buildAccount(), false, true)
    await flushPromises()
    const selector = wrapper.getComponent(ModelWhitelistSelector)
    const initialReads = catalogRead.mock.calls.length
    const originalSelector = selector.vm.$
    const selection = wrapper.get<HTMLDetailsElement>('[data-testid="model-selection"]')
    const customInput = selector.get('input[aria-label="admin.accounts.customModelName"]')
    await customInput.setValue('provider/draft-model')
    selection.element.open = true
    await selection.trigger('toggle')
    selection.element.open = false
    await selection.trigger('toggle')
    selection.element.open = true
    await selection.trigger('toggle')
    expect(wrapper.getComponent(ModelWhitelistSelector).vm.$).toBe(originalSelector)
    expect((customInput.element as HTMLInputElement).value).toBe('provider/draft-model')
    expect(catalogRead).toHaveBeenCalledTimes(initialReads)
    const access = wrapper.get<HTMLDetailsElement>('[data-testid="model-access-rules"]')
    await access.get('textarea').setValue('provider/excluded-draft')
    access.element.open = false
    await access.trigger('toggle')
    expect((access.get('textarea').element as HTMLTextAreaElement).value).toBe('provider/excluded-draft')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(allowlistWrite).not.toHaveBeenCalled()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(modelAccess.saveRule).not.toHaveBeenCalled()
    expect(modelAccess.preview).not.toHaveBeenCalled()
    expect(modelAvailabilityAPI.declare).not.toHaveBeenCalled()
    expect(modelAvailabilityAPI.revoke).not.toHaveBeenCalled()
    expect(convertCredentialSecret).not.toHaveBeenCalled()
    expect(rotateCredentialSecret).not.toHaveBeenCalled()
    expect(revokeCredentialSecret).not.toHaveBeenCalled()
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('type')).toBe('submit')
    expect(wrapper.get('[data-tour="account-form-submit"]').attributes('form')).toBe('edit-account-form')
    wrapper.unmount()
  })

  it('reveals native invalid fields without changing drafts or requests', async () => {
    const wrapper = mountModal()
    await flushPromises()
    const connection = wrapper.get<HTMLDetailsElement>('[data-testid="account-connection-settings"]')
    const input = connection.get<HTMLInputElement>('input')
    const originalValue = input.element.value
    connection.element.open = false
    await input.trigger('invalid')
    await flushPromises()
    expect(connection.element.open).toBe(true)
    expect(input.element.value).toBe(originalValue)
    wrapper.unmount()
  })


})


it('reveals a folded connection section when an existing submit validator rejects credentials', async () => {
  const account = { ...buildAccount(), credentials: { base_url: 'https://synthetic.example' }, credentials_status: { has_api_key: false } }
  const wrapper = mountModal(account)
  await flushPromises()
  const connection = wrapper.get<HTMLDetailsElement>('[data-testid="account-connection-settings"]')
  connection.element.open = false
  updateAccountMock.mockClear()
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(connection.element.open).toBe(true)
  expect(showErrorMock).toHaveBeenLastCalledWith('admin.accounts.apiKeyIsRequired')
  expect(updateAccountMock).not.toHaveBeenCalled()
  wrapper.unmount()
})


it('ordinary account save preserves protected deny-all and existing capability declarations', async () => {
  const account = buildAccount()
  account.extra = { ...account.extra, upstream_model_allowlist: { enabled: true, models: [] }, model_capability_defaults: { schema_version: 1, reason: 'legacy', models: {} } }
  updateAccountMock.mockResolvedValue(account)
  allowlistWrite.mockClear()
  const wrapper = mountModal(account, false, true)
  await flushPromises()
  expect(wrapper.findComponent({ name: 'ModelCapabilityDefaultsEditor' }).exists()).toBe(false)
  expect(wrapper.findComponent({ name: 'ModelAvailabilityEditor' }).exists()).toBe(false)
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock).toHaveBeenCalled()
  const payload = updateAccountMock.mock.calls.at(-1)![1]
  if (payload.extra) {
    expect(payload.extra.upstream_model_allowlist).toEqual({ enabled: true, models: [] })
    expect(payload.extra.model_capability_defaults).toEqual(account.extra.model_capability_defaults)
  }
  expect(allowlistWrite).not.toHaveBeenCalled()
  wrapper.unmount()
})


it('shows the observed CommandCode plan separately from API key authentication', async () => {
  const wrapper = mountModal({ ...buildAccount(), id: 2401, platform: 'commandcode', extra: {
    commandcode_plan_level: 'individual-goat', commandcode_monthlyCredits: 0,
    commandcode_usage_updated_at: new Date().toISOString()
  } })
  await flushPromises()
  expect(wrapper.text()).toContain('admin.accounts.platformDetails.commandcodeAccountTypeTitle')
  const quota = wrapper.get('[data-test="cn-provider-quota"]')
  expect(quota.text()).toContain('GOAT')
  expect(quota.text()).toContain('0.00')
  expect(quota.find('select').exists()).toBe(false)
  wrapper.unmount()
})

it('round-trips Qianwen Coding Plan credentials without replacing its private endpoints', async () => {
  const account = buildAccount()
  account.platform = 'qianwen'
  account.credentials = { api_key: 'fixture-coding-key', account_mode: 'coding', api_protocol: 'adaptive', base_url: 'https://private.example/chat', api_base_urls: { chat_completions: 'https://private.example/chat', anthropic: 'https://private.example/messages', responses: 'https://private.example/responses' } }
  updateAccountMock.mockReset().mockResolvedValue(account)
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  const wrapper = mountModal(account)
  await flushPromises()
  expect(wrapper.get('[data-testid="qianwen-coding-plan-scope"] a').attributes('href')).toBe('https://help.aliyun.com/zh/model-studio/coding-plan')
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock).toHaveBeenCalledTimes(1)
  expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject(account.credentials)
  wrapper.unmount()
})

vi.mock('@/constants/compatibleProviders', async () => {
  const actual = await vi.importActual<typeof import('@/constants/compatibleProviders')>('@/constants/compatibleProviders')
  const { compatibleFixture, resourceFixture } = await import('./compatibleProviderFixtures')
  return { ...actual, compatibleProvider: (platform: string) => [compatibleFixture, resourceFixture].find(provider => provider.id === platform) ?? actual.compatibleProvider(platform) }
})


it.each([false, true])('edits a compatible account preserving other overrides and explicit protocol clear=%s', async (clearProtocol) => {
  const credentials = { account_mode: 'coding', api_protocol: 'anthropic', base_url: 'https://relay.example.com', api_base_urls: { anthropic: 'https://relay.example.com/messages', responses: 'https://retained.example.com' }, api_key_ref: 'vault:fixture', model_mapping: { public: 'deployment-name' } }
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  const account = { ...buildAccount(), platform: 'example-cloud', proxy_id: 8, credentials_status: { has_api_key: true }, credentials }
  showErrorMock.mockReset()
  updateAccountMock.mockReset().mockResolvedValue(account)
  const wrapper = mountModal(account)
  await flushPromises()
  expect(wrapper.find('[data-testid="upstream-billing-auto-probe"]').exists()).toBe(false)
  expect(wrapper.get('[data-testid="compatible-protocol"] [data-choice="anthropic"]').attributes('aria-checked')).toBe('true')
  if (clearProtocol) await wrapper.get('[data-testid="compatible-base-anthropic"]').setValue('')
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(showErrorMock.mock.calls).toEqual([])
  expect(updateAccountMock).toHaveBeenCalledOnce()
  const payload = updateAccountMock.mock.calls[0][1]
  expect(payload.credentials).toMatchObject({ ...credentials, api_base_urls: { ...credentials.api_base_urls, anthropic: clearProtocol ? '' : credentials.api_base_urls.anthropic } })
  expect(payload.credentials.api_key).toBeUndefined()
  expect(payload.proxy_id).toBeUndefined()
  expect(payload.upstream_billing_probe_enabled).toBe(false)
  expect(payload.upstream_billing_rate_sync_enabled).toBe(false)
  expect(account.credentials).toEqual(credentials)
  wrapper.unmount()
})

it.each([false, true])('preserves Nous references for same-mode edits and clears old invocation secrets on proxy switch=%s', async (switchMode) => {
  const credentials = { auth_mode: 'invocation_token', account_mode: 'payg', credential_secret_reference: 'bound-ref', credential_secret_version: 3, model_mapping: { public: 'private' } }
  const account = { ...buildAccount(), platform: 'nous_research', credentials_status: { has_api_key: true }, credentials }
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  showErrorMock.mockReset()
  updateAccountMock.mockReset().mockResolvedValue(account)
  const wrapper = mountModal(account)
  await flushPromises()
  if (switchMode) {
    await wrapper.get('[data-account-field="editApiKey"]').setValue('discarded-fixture')
    await wrapper.get('[data-testid="special-auth-mode"]').setValue('official_proxy')
    expect((wrapper.get('[data-account-field="editApiKey"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('[data-testid="special-auth-base"]').setValue('https://operator.example/v1')
  }
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(showErrorMock.mock.calls).toEqual([])
  expect(updateAccountMock).toHaveBeenCalledOnce()
  const submitted = updateAccountMock.mock.calls[0][1].credentials
  expect(submitted.credential_secret_reference).toBe('bound-ref')
  expect(submitted.credential_secret_version).toBe(3)
  expect(submitted.api_key).toBe(switchMode ? '' : undefined)
  expect(submitted.auth_mode).toBe(switchMode ? 'official_proxy' : 'invocation_token')
  expect(account.credentials).toEqual(credentials)
  wrapper.unmount()
})

it.each([['aicodemirror', 'pro'], ['ccsub', 'subscription']])('restores and explicitly clears a saved %s plan in the real edit form', async (platform, plan) => {
  const credentials = { billing_plan: plan, account_mode: 'payg', api_protocol: 'adaptive', credential_secret_reference: 'plan-ref', credential_secret_version: 3, base_url: 'https://relay.example/v1', api_base_urls: { anthropic: 'https://private.example/messages' }, model_mapping: { public: 'private' } }
  const account = { ...buildAccount(), platform, proxy_id: 8, group_ids: [12], credentials_status: { has_api_key: true }, credentials }
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  showErrorMock.mockReset()
  updateAccountMock.mockReset().mockResolvedValue(account)
  const wrapper = mountModal(account)
  await flushPromises()
  expect(wrapper.get(`[data-testid="compatible-plan"] [data-choice="${plan}"]`).attributes('aria-checked')).toBe('true')
  await wrapper.get('[data-testid="compatible-plan-clear"]').trigger('click')
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(showErrorMock.mock.calls).toEqual([])
  expect(updateAccountMock).toHaveBeenCalledOnce()
  const payload = updateAccountMock.mock.calls[0][1]
  expect(payload.credentials).toMatchObject({ ...credentials, billing_plan: null })
  expect(JSON.parse(JSON.stringify(payload)).credentials).toHaveProperty('billing_plan', null)
  expect(payload.credentials.api_key).toBeUndefined()
  expect(payload.proxy_id).toBeUndefined()
  expect(payload.group_ids).toEqual([12])
  expect(account.credentials).toEqual(credentials)
  wrapper.unmount()
})


it('reconstructs and locks Step Plan while preserving masked references and overrides on save', async () => {
  const credentials = { account_mode: 'coding', api_protocol: 'adaptive', base_url: 'https://relay.example/v1', api_base_urls: { anthropic: 'https://relay.example/messages', responses: 'https://retained.example/responses' }, credential_secret_reference: 'bound-secret', credential_secret_version: 3, api_key_ref: 'vault:step-plan', model_mapping: { public: 'private-model' } }
  const account = { ...buildAccount(), platform: 'stepfun_plan', proxy_id: 8, group_ids: [12], credentials_status: { has_api_key: true }, credentials }
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  updateAccountMock.mockReset().mockResolvedValue(account)
  const wrapper = mountModal(account)
  await flushPromises()
  expect(wrapper.get('[data-testid="supplier-product"] [data-choice="stepPlan"]').attributes('aria-checked')).toBe('true')
  expect(wrapper.get('[data-testid="supplier-region"] [data-choice="china"]').attributes('aria-checked')).toBe('true')
  const sibling = wrapper.get('[data-testid="supplier-product"] [data-choice="api"]')
  expect(sibling.attributes('disabled')).toBeDefined()
  await sibling.trigger('click')
  expect(wrapper.find('[data-testid="compatible-mode"]').exists()).toBe(false)
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock).toHaveBeenCalledOnce()
  const payload = updateAccountMock.mock.calls[0][1]
  expect(payload.platform).toBeUndefined()
  expect(payload.credentials).toMatchObject(credentials)
  expect(payload.credentials.api_key).toBeUndefined()
  expect(payload.proxy_id).toBeUndefined()
  expect(payload.group_ids).toEqual([12])
  expect(account.credentials).toEqual(credentials)
  wrapper.unmount()
})

it('allows kat_coder same-ID coding mode edits without rebinding its masked secret', async () => {
  const credentials = { account_mode: 'payg', api_protocol: 'adaptive', credential_secret_reference: 'kat-secret', credential_secret_version: 2, base_url: 'https://custom.example/v1', model_mapping: { public: 'private-model' } }
  const account = { ...buildAccount(), platform: 'kat_coder', credentials_status: { has_api_key: true }, credentials }
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  updateAccountMock.mockReset().mockResolvedValue(account)
  const wrapper = mountModal(account)
  await flushPromises()
  expect(wrapper.find('[data-testid="supplier-variants"]').exists()).toBe(false)
  await wrapper.get('[data-testid="compatible-mode"] [data-choice="coding"]').trigger('click')
  await wrapper.get('form#edit-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(updateAccountMock).toHaveBeenCalledOnce()
  const payload = updateAccountMock.mock.calls[0][1]
  expect(payload.platform).toBeUndefined()
  expect(payload.credentials).toMatchObject({ ...credentials, account_mode: 'coding' })
  expect(payload.credentials.api_key).toBeUndefined()
  expect(account.platform).toBe('kat_coder')
  wrapper.unmount()
})

describe('Volcengine query credential editing', () => {
  const quotaAccount = () => ({ ...buildAccount(), platform: 'volcengine', credentials_status: {
    has_api_key: true, has_volcengine_access_key_id: true, has_volcengine_secret_access_key: true
  }, credentials: { account_mode: 'coding', api_protocol: 'chat_completions', base_url: 'https://ark.cn-beijing.volces.com/api/coding/v3', volcengine_plan_edition: 'team', volcengine_seat_id: 'synthetic-seat' } })
  it('restores edition and seat while leaving saved secrets blank, and preserves them on submit', async () => {
    const account = quotaAccount()
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    expect(wrapper.get('[data-test="accessKeyId"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-test="secretAccessKey"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-test="quota-edition"]').element).toHaveProperty('value', 'team')
    expect(wrapper.get('[data-test="quota-seat"]').element).toHaveProperty('value', 'synthetic-seat')
    expect(wrapper.get('[data-test="volcengine-quota-fields"]').text()).toContain('volcengineQuota.saved')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0][1].credentials
    expect(credentials).not.toHaveProperty('volcengine_access_key_id')
    expect(credentials).not.toHaveProperty('volcengine_secret_access_key')
    expect(credentials).not.toHaveProperty('api_key')
    expect(credentials).toMatchObject({ volcengine_plan_edition: 'team', volcengine_seat_id: 'synthetic-seat' })
    wrapper.unmount()
  })
  it.each(['replace', 'clear'])('supports explicit %s without changing inference credentials', async action => {
    const account = quotaAccount()
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account)
    await flushPromises()
    if (action === 'clear') {
      await wrapper.get('[data-test="clearAccessKeyId"]').setValue(true)
      await wrapper.get('[data-test="clearSecretAccessKey"]').setValue(true)
      expect(wrapper.get('[data-test="accessKeyId"]').attributes('disabled')).toBeDefined()
    } else {
      await wrapper.get('[data-test="accessKeyId"]').setValue('new-synthetic-access')
      await wrapper.get('[data-test="secretAccessKey"]').setValue('new-synthetic-secret')
    }
    await wrapper.get('[data-test="quota-edition"]').setValue('personal')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const credentials = updateAccountMock.mock.calls[0][1].credentials
    expect(credentials.volcengine_access_key_id).toBe(action === 'clear' ? null : 'new-synthetic-access')
    expect(credentials.volcengine_secret_access_key).toBe(action === 'clear' ? null : 'new-synthetic-secret')
    expect(credentials).not.toHaveProperty('api_key')
    expect(credentials).not.toHaveProperty('volcengine_seat_id')
    wrapper.unmount()
  })
  it('never echoes legacy secrets and resets draft secrets when switching accounts', async () => {
    const account = quotaAccount()
    const legacy = { ...account, credentials: { ...account.credentials, volcengine_access_key_id: 'legacy-synthetic-access', volcengine_secret_access_key: 'legacy-synthetic-secret' } }
    const wrapper = mountModal(legacy)
    await flushPromises()
    expect(wrapper.html()).not.toContain('legacy-synthetic-')
    await wrapper.get('[data-test="secretAccessKey"]').setValue('new-synthetic-secret')
    await wrapper.setProps({ account: { ...account, id: 5699 } })
    await flushPromises()
    expect(wrapper.get('[data-test="secretAccessKey"]').element).toHaveProperty('value', '')
    wrapper.unmount()
  })
})


describe('management query editing', () => {
  it.each(['keep', 'replace', 'clear'])('submits management %s without changing inference', async action => {
    const account = { ...buildAccount(), platform: 'dmxapi', credentials_status: { has_api_key: true, has_usage_query_token: true }, credentials: { usage_query_user_id: '42' } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account); await flushPromises()
    expect(wrapper.get('[data-test="usage-query-token"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-test="usage-query-user-id"]').element).toHaveProperty('value', '42')
    if (action === 'replace') await wrapper.get('[data-test="usage-query-token"]').setValue('replacement-management')
    if (action === 'clear') await wrapper.get('[data-test="usage-query-clear"]').setValue(true)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock).toHaveBeenCalledOnce()
    const credentials = updateAccountMock.mock.calls[0][1].credentials
    expect(credentials).not.toHaveProperty('api_key')
    expect(credentials.usage_query_user_id).toBe('42')
    expect(credentials.usage_query_token).toBe(action === 'keep' ? undefined : action === 'clear' ? null : 'replacement-management')
    wrapper.unmount()
  })
})


describe('integrated query credentials and model readback', () => {
  it.each(['azure_openai', 'github_copilot'])('keeps %s inference save independent of optional query configuration', async platform => {
    const account = { ...buildAccount(), platform, credentials: { account_mode: 'payg', base_url: platform === 'azure_openai' ? 'https://fixture.openai.azure.com/openai/v1' : '', api_key: 'inference-fixture', usage_query_subscription_id: 'subscription', usage_query_region: 'eastus', usage_query_username: 'octocat' }, credentials_status: { has_api_key: true, has_usage_query_token: true } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account); await flushPromises()
    expect(wrapper.get('[data-test="usage-query-token"]').element).toHaveProperty('value', '')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('usage_query_token')
    if (platform === 'azure_openai') expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ usage_query_subscription_id: 'subscription', usage_query_region: 'eastus' })
    else expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ usage_query_username: 'octocat' })
    await wrapper.get('[data-test="usage-query-clear"]').setValue(true)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[1][1].credentials.usage_query_token).toBeNull()
    wrapper.unmount()
  })
  it('restores Tencent region/team while independently preserving, replacing and clearing cloud secrets', async () => {
    const account = { ...buildAccount(), platform: 'hunyuan', credentials: { account_mode: 'coding', api_protocol: 'chat_completions', base_url: 'https://tokenhub.tencentmaas.com/plan/v3', tencent_query_region: 'ap-shanghai', tencent_query_team_id: 'team-fixture' }, credentials_status: { has_api_key: true, has_tencent_query_secret_id: true, has_tencent_query_secret_key: true } }
    updateAccountMock.mockReset().mockResolvedValue(account)
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account); await flushPromises()
    expect(wrapper.get('[data-test="secretId"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-test="region"]').element).toHaveProperty('value', 'ap-shanghai')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ tencent_query_region: 'ap-shanghai', tencent_query_team_id: 'team-fixture' })
    expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('tencent_query_secret_id')
    expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('api_key')
    await wrapper.get('[data-test="secretId"]').setValue('replacement-query-id')
    await wrapper.get('[data-test="clearSecretKey"]').setValue(true)
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[1][1].credentials).toMatchObject({ tencent_query_secret_id: 'replacement-query-id', tencent_query_secret_key: null })
    expect(updateAccountMock.mock.calls[1][1].credentials).not.toHaveProperty('api_key')
    wrapper.unmount()
  })
  it('reopens Antigravity mappings from full account readback without turning aliases into direct IDs', async () => {
    const mapping = Object.fromEntries(Array.from({ length: 41 }, (_, index) => [`public-alias-${index}`, `upstream-model-${index}`]))
    const account = { ...buildAntigravityAccount(), credentials: { antigravity_project_id: 'configured-project', model_mapping: mapping } }
    updateAccountMock.mockReset().mockImplementation(async (_id, payload) => ({ ...account, ...payload }))
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
    const wrapper = mountModal(account); await flushPromises()
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    const saved = updateAccountMock.mock.calls[0][1]
    expect(saved.credentials.model_mapping).toEqual(mapping)
    wrapper.unmount()
    const readback = { ...account, ...saved }
    credentialAccountRead.mockResolvedValue(readback)
    const reopened = mountModal(readback); await flushPromises()
    expect(reopened.get('[data-testid="model-whitelist-value"]').text()).toBe('')
    await reopened.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
    expect(updateAccountMock.mock.calls[1][1].credentials.model_mapping).toEqual(mapping)
    expect(Object.entries(updateAccountMock.mock.calls[1][1].credentials.model_mapping).filter(([source, target]) => source === target)).toHaveLength(0)
    reopened.unmount()
  })
})


it('edits BytePlus team query keys using the existing plan without another edition selector', async () => {
  const account = { ...buildAccount(), platform: 'byteplus', credentials: { account_mode: 'coding', billing_plan: 'team', byteplus_seat_id: 'saved-seat' }, credentials_status: { has_api_key: true, has_byteplus_access_key_id: true, has_byteplus_secret_access_key: true } }
  updateAccountMock.mockReset().mockResolvedValue(account)
  checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  const wrapper = mountModal(account); await flushPromises()
  const fields = wrapper.get('[data-test="byteplus-quota-fields"]')
  expect(fields.find('[data-test="quota-edition"]').exists()).toBe(false)
  expect(fields.get('[data-test="quota-seat"]').element).toHaveProperty('value', 'saved-seat')
  expect(fields.get('[data-test="accessKeyId"]').element).toHaveProperty('value', '')
  await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
  expect(updateAccountMock.mock.calls[0][1].credentials).toMatchObject({ billing_plan: 'team', byteplus_seat_id: 'saved-seat' })
  expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('byteplus_access_key_id')
  expect(updateAccountMock.mock.calls[0][1].credentials).not.toHaveProperty('api_key')
  await fields.get('[data-test="accessKeyId"]').setValue('replacement-byteplus-id')
  await fields.get('[data-test="clearSecretAccessKey"]').setValue(true)
  await wrapper.get('form#edit-account-form').trigger('submit.prevent'); await flushPromises()
  expect(updateAccountMock.mock.calls[1][1].credentials).toMatchObject({ billing_plan: 'team', byteplus_access_key_id: 'replacement-byteplus-id', byteplus_secret_access_key: null })
  expect(updateAccountMock.mock.calls[1][1].credentials).not.toHaveProperty('byteplus_plan_edition')
  wrapper.unmount()
})
