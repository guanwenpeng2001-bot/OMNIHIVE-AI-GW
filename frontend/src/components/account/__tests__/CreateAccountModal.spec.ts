import { CHAT_PLATFORM_BASE_URLS, CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  probeUpstreamBillingMock,
  syncUpstreamModelsMock,
  showWarningMock,
  showInfoMock,
  importCodexSessionMock,
  createOpenAICodexPATMock,
  authIsSimpleMode,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
  syncUpstreamModelsMock: vi.fn(),
  showWarningMock: vi.fn(),
  showInfoMock: vi.fn(),
  importCodexSessionMock: vi.fn(),
  createOpenAICodexPATMock: vi.fn(),
  authIsSimpleMode: { value: true },
}))

vi.mock('@/api/client', () => ({ apiClient: {
  post: vi.fn(async (url: string) => ({ data: { credential_draft: 'synthetic-draft', revision: url.endsWith('/review') ? 'reviewed' : 'initial', platform: 'cursor', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } })),
  delete: vi.fn().mockResolvedValue({ data: { cancelled: true } })
} }))

vi.mock('@/stores/app' , () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: showWarningMock,
    showInfo: showInfoMock,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isSimpleMode() {
      return authIsSimpleMode.value
    },
  }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      batchCreate: async (payloads: Record<string, unknown>[]) => {
        const results = []
        for (const payload of payloads) { const account = await createAccountMock(payload); results.push({ name: payload.name, success: true, id: account.id }) }
        return { success: results.length, failed: 0, results }
      },
      probeUpstreamBilling: probeUpstreamBillingMock,
      syncUpstreamModels: syncUpstreamModelsMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
      importCodexSession: importCodexSessionMock,
      createOpenAICodexPAT: createOpenAICodexPATMock,
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({}),
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: { syncUpstreamModels: syncUpstreamModelsMock, getAvailableModels: vi.fn().mockResolvedValue([]), getModelCatalog: vi.fn().mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '1' } }), syncUpstreamModelsPreview: vi.fn().mockResolvedValue({ candidate_models: ['public-glm'], models: ['public-glm'], discovery_complete: true, complete: true }) },
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'
import { apiClient } from '@/api/client'
import { accountsAPI } from '@/api/admin/accounts'
import zhAdminAccounts from '@/i18n/locales/zh/admin/accounts'
import type { CreateAccountRequest } from '@/types'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import CursorKeyFields from '../CursorKeyFields.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import CursorBrowserAuthorization from '../CursorBrowserAuthorization.vue'
vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn().mockResolvedValue([]) }))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String,
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['import-codex-session', 'import-codex-pat'],
  template: `
    <div>
      <button data-testid="import-codex-session" @click="$emit('import-codex-session', 'session-json')">session</button>
      <button data-testid="import-codex-pat" @click="$emit('import-codex-pat', 'pat-token')">pat</button>
    </div>
  `,
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  template: `
    <button
      type="button"
      data-testid="select-pricing-groups"
      @click="$emit('update:modelValue', [1, 2])"
    >
      groups
    </button>
  `,
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
    platform: String,
    syncCredentials: Object,
  },
  emits: ['update:modelValue', 'upstream-synced'],
  template: `<button
    type="button"
    data-testid="model-whitelist-selector"
    @click="$emit('update:modelValue', ['public-glm']); $emit('upstream-synced')"
  >models</button>`,
})

function mountModal(groups: any[] = [], realCatalog = false, attachTo?: HTMLElement) {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups },
    attachTo,
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: false,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: realCatalog ? false : ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
      },
    },
  })
}

async function confirmReview(wrapper: ReturnType<typeof mountModal>) {
  await flushPromises()
  const confirm = wrapper.find('[data-testid="confirm-account-review"]')
  if (confirm.exists()) { await confirm.trigger('click'); await flushPromises() }
}

async function openReview(wrapper: ReturnType<typeof mountModal>) {
  const name = wrapper.get('form#create-account-form input[type="text"]')
  if (!(name.element as HTMLInputElement).value) await name.setValue('Review fixture')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  expect(wrapper.find('[data-testid="account-review"]').exists()).toBe(true)
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(
  platform: 'openai' | 'anthropic',
  enableLongContextBilling = false,
  disableUpstreamBillingProbe = false
) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, platform === 'openai' ? 'OpenAI' : 'admin.accounts.claudeConsole')
  if (platform === 'openai') {
    await selectButtonByText(wrapper, 'API Key')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} account`)
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  if (disableUpstreamBillingProbe) {
    await wrapper.get('[data-testid="upstream-billing-auto-probe"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
  await flushPromises()
  return wrapper
}

async function openCodexImportStep(toggleClicks = 0) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, 'OpenAI')
  for (let click = 0; click < toggleClicks; click += 1) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
  return wrapper
}

describe('CreateAccountModal OpenAI long-context billing', () => {
  it.each(['current', 'legacy'])('opens mixed-channel confirmation for %s conflicts and retries the captured payload', async (format) => {
    const metadata = { group_id: 27, group_name: 'Mixed group', current_platform: 'Anthropic', other_platform: 'Antigravity' }
    createAccountMock.mockRejectedValueOnce(format === 'current'
      ? { status: 409, code: 409, reason: 'mixed_channel_warning', message: 'Confirm mixed group', metadata }
      : { response: { status: 409, data: { error: 'mixed_channel_warning', message: 'Confirm mixed group', details: metadata } } })
    const wrapper = await submitApiKeyAccount('anthropic')
    const dialog = wrapper.findAllComponents(ConfirmDialog).find(component => component.props('title') === 'admin.accounts.mixedChannelWarningTitle')!
    expect(dialog.props('show')).toBe(true)
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0][0]
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledTimes(2)
    expect(createAccountMock.mock.calls[1][0]).toEqual({ ...payload, confirm_mixed_channel_risk: true })
    wrapper.unmount()
  })

  it('cancels a mixed-channel conflict without another write', async () => {
    createAccountMock.mockRejectedValueOnce({ status: 409, code: 409, reason: 'mixed_channel_warning', message: 'Confirm mixed group', metadata: {} })
    const wrapper = await submitApiKeyAccount('anthropic')
    const dialog = wrapper.findAllComponents(ConfirmDialog).find(component => component.props('title') === 'admin.accounts.mixedChannelWarningTitle')!
    expect(dialog.props('show')).toBe(true)
    dialog.vm.$emit('cancel')
    await flushPromises()
    expect(dialog.props('show')).toBe(false)
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('created')).toBeUndefined()
    wrapper.unmount()
  })

  it('retains the Cursor browser authorization receipt for saved model configuration without applying', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Cursor')
    await wrapper.get('[data-testid="cursor-key-mode-auto"]').trigger('click')
    await flushPromises()
    createAccountMock.mockResolvedValueOnce({ id: 93, platform: 'cursor' })
    wrapper.getComponent(CursorBrowserAuthorization).vm.$emit('draft', { credential_draft: 'browser-draft', revision: 'initial', platform: 'cursor', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' })
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="account-review"]').exists()).toBe(true)
    await confirmReview(wrapper)
    await flushPromises()
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#93')
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('configureModels')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.find('[data-testid="created-catalog-accounts"]').exists()).toBe(false)
    wrapper.unmount()
  })
  beforeEach(() => {
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    syncUpstreamModelsMock.mockReset().mockResolvedValue({ candidate_models: [], models: [], metadata: {} })
    showWarningMock.mockReset()
    showInfoMock.mockReset()
    importCodexSessionMock.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      errors: [],
      warnings: [],
    })
    createOpenAICodexPATMock.mockReset().mockResolvedValue({ credential_draft: 'pat-draft', revision: 'initial', platform: 'openai', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' })
  })

  afterEach(() => vi.useRealTimers())

  it('mounts all catalog platforms as a named radio group with real decorative icons', () => {
    const wrapper = mountModal()
    const group = wrapper.get('[role="radiogroup"]')
    expect(group.attributes('aria-label')).toBe('admin.accounts.platform')
    expect(group.attributes('data-tour')).toBe('account-form-platform')
    expect(group.classes()).toEqual(expect.arrayContaining(['flex', 'flex-wrap', 'rounded-lg', 'p-1', 'bg-gray-100']))
    const radios = group.findAll('[role="radio"]')
    expect(radios).toHaveLength(CONCRETE_PLATFORM_OPTIONS.length + 1)
    expect(group.find('[data-testid="vertex-platform"]').exists()).toBe(true)
    CONCRETE_PLATFORM_OPTIONS.forEach((option, index) => {
      const radio = radios[index]!
      expect(radio.attributes('data-testid')).toBe(`${option.value}-platform`)
      expect(radio.attributes('type')).toBe('button')
      expect(radio.attributes('aria-checked')).toBe(String(index === 0))
      expect(radio.attributes('tabindex')).toBe(index === 0 ? '0' : '-1')
      expect(radio.getComponent(PlatformIcon).props('platform')).toBe(option.value)
      expect(radio.get('svg').attributes('aria-hidden')).toBe('true')
      expect(radio.classes()).toContain('focus-visible:ring-2')
      expect(radio.text()).toBe(option.value === 'commandcode' ? `CC${option.label}` : option.label)
    })
    expect(radios[0]!.classes()).toEqual(expect.arrayContaining(['bg-white', 'text-primary-600', 'shadow-sm']))
    wrapper.unmount()
  })

  it('moves selection and focus with arrows, Home and End, wrapping at both ends', async () => {
    const host = document.createElement('div')
    document.body.append(host)
    const wrapper = mountModal([], false, host)
    try {
      const entrypoints = [...CONCRETE_PLATFORM_OPTIONS, { value: 'vertex' }]
      const radio = (index: number) => wrapper.get(`[data-testid="${entrypoints[index]!.value}-platform"]`)
      const last = entrypoints.length - 1
      for (const [from, key, to] of [
        [0, 'ArrowRight', 1], [1, 'ArrowDown', 2], [2, 'ArrowLeft', 1],
        [1, 'ArrowUp', 0], [0, 'ArrowLeft', last], [last, 'ArrowRight', 0],
        [0, 'End', last], [last, 'Home', 0],
      ] as const) {
        await radio(from).trigger('keydown', { key })
        expect(radio(to).attributes('aria-checked')).toBe('true')
        expect(radio(to).attributes('tabindex')).toBe('0')
        expect(document.activeElement).toBe(radio(to).element)
        expect(wrapper.get('[data-tour="account-form-platform"]').findAll('[role="radio"][tabindex="0"]')).toHaveLength(1)
      }
      const ignored = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
      radio(0).element.dispatchEvent(ignored)
      expect(ignored.defaultPrevented).toBe(false)
      expect(createAccountMock).not.toHaveBeenCalled()
      expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
      expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

  it.each([' ', 'Enter'])('uses native %s activation once through click and retains the platform reset flow', async key => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="qianwen-platform"]').trigger('click')
    const endpoint = '[data-testid="cn-adaptive-base-url-chat_completions"]'
    await wrapper.get(endpoint).setValue('https://edited.example/v1')
    const selected = wrapper.get('[data-testid="qianwen-platform"]')
    const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
    selected.element.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    await flushPromises()
    // JSDOM does not synthesize the native button click for keyboard activation.
    // No reset on keydown; the browser-generated click runs selectPlatform exactly once.
    expect((wrapper.get(endpoint).element as HTMLInputElement).value).toBe('https://edited.example/v1')
    await selected.trigger('click')
    expect((wrapper.get(endpoint).element as HTMLInputElement).value).toBe('https://dashscope.aliyuncs.com/compatible-mode/v1')
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('applies the same protocol and endpoint reset for arrow selection and mouse selection', async () => {
    const keyboard = mountModal()
    const mouse = mountModal()
    await keyboard.get('[data-testid="opencode_go-platform"]').trigger('click')
    await keyboard.get('[data-testid="opencode_go-platform"]').trigger('keydown', { key: 'ArrowRight' })
    await mouse.get('[data-testid="qianwen-platform"]').trigger('click')
    expect((keyboard.get('[data-testid="cn-adaptive-base-url-chat_completions"]').element as HTMLInputElement).value).toBe((mouse.get('[data-testid="cn-adaptive-base-url-chat_completions"]').element as HTMLInputElement).value)
    expect(keyboard.get('[data-testid="qianwen-platform"]').attributes('aria-checked')).toBe('true')
    keyboard.unmount()
    mouse.unmount()
  })

  it('persists a create-time scheduling override and image-tool policy on API keys', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Configured account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="create-scheduling-threshold"]').setValue(72)
    const vm = wrapper.vm as unknown as { createImageTool: string }
    vm.createImageTool = 'block'
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ credentials: { account_scheduling_threshold: 72 }, extra: { codex_image_generation_explicit_tool_policy: 'strip' } })
    wrapper.unmount()
  })

  it('shows only one AI Studio tier selector and saves its chosen value', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Gemini')
    await selectButtonByText(wrapper, 'admin.accounts.gemini.accountType.apiKeyTitle')
    const tierSelects = wrapper.findAll('select').filter(select => select.find('option[value="aistudio_free"]').exists())
    expect(tierSelects).toHaveLength(1)
    await tierSelects[0]!.setValue('aistudio_paid')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Gemini paid')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0].credentials.tier_id).toBe('aistudio_paid')
    wrapper.unmount()
  })

  it('saves draft selections and previews saved candidates without recreating or requiring adoption', async () => {
    const wrapper = mountModal([], true)
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Reviewed account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-key')
    await openReview(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.clearAllModels')!.trigger('click')
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="add-preview-models"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledOnce()
    expect(createAccountMock.mock.calls[0][0].credentials.model_mapping).toEqual({ 'public-glm': 'public-glm' })
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toBeUndefined()
    const panel = wrapper.get('[data-testid="created-catalog-accounts"]')
    syncUpstreamModelsMock.mockResolvedValueOnce({ candidate_models: ['saved-model'], discovered_models: ['saved-model'], discovery_complete: true })
    await panel.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(syncUpstreamModelsMock).toHaveBeenLastCalledWith(42, false)
    expect(panel.find('[data-testid="apply-model-snapshot"]').exists()).toBe(false)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    expect(createAccountMock).toHaveBeenCalledOnce()
    expect(wrapper.findAllComponents(ModelWhitelistSelector).some(selector => selector.props('accountId') === 42)).toBe(true)
    wrapper.unmount()
  })

  it('creates Cursor auto with tokens and required machine IDs', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="account-concurrency"]').setValue(7)
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Cursor auto')
    await wrapper.get('[data-testid="cursor-key-mode-auto"]').trigger('click')
    for (const [field, value] of Object.entries({ access_token: 'synthetic-access', refresh_token: 'synthetic-refresh', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64) })) {
      await wrapper.get(`[data-testid="cursor-${field}"]`).setValue(value)
    }
    await openReview(wrapper)
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({ platform: 'cursor', type: 'oauth', credential_draft: 'synthetic-draft', credential_draft_revision: 'initial' })
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'cursor', type: 'oauth', concurrency: 7, credential_draft: 'synthetic-draft', credential_draft_revision: 'reviewed' })
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).not.toHaveProperty('api_key')
    wrapper.unmount()
  })

  it.each(['official', 'relay'] as const)('creates Cursor %s keys with the correct credentials and concurrency', async mode => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="account-concurrency"]').setValue(7)
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Cursor account')
    await wrapper.get(`[data-testid="cursor-key-mode-${mode}"]`).trigger('click')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('crsr_test')
    if (mode === 'relay') await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    await openReview(wrapper)
    const selector = wrapper.findComponent(ModelWhitelistSelectorStub)
    expect(selector.exists()).toBe(true)
    selector.vm.$emit('update:modelValue', ['composer-2.5'])
    await flushPromises()
    expect(wrapper.find('[data-testid="cursor-native-settings-hint"]').exists()).toBe(mode === 'official')
    if (mode === 'relay') await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({ platform: 'cursor', type: mode === 'official' ? 'apikey' : 'upstream', concurrency: 7 })
    expect(payload.credentials.api_key).toBe('crsr_test')
    expect(payload.credentials.model_mapping).toEqual({ 'composer-2.5': 'composer-2.5' })
    if (mode === 'official') expect(payload.credentials).not.toHaveProperty('base_url')
    else expect(payload.credentials).toMatchObject({ base_url: 'https://relay.example/v1', api_protocol: 'adaptive' })
    expect(payload.upstream_billing_probe_enabled).toBe(mode === 'relay' ? true : undefined)
    wrapper.unmount()
  })


  it('sets month and year expiry presets without submitting the account form', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-01-31T12:34:00'))
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('expiry account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    const input = wrapper.get<HTMLInputElement>('input[type="datetime-local"]')

    for (const [label, expected] of [
      ['payment.oneMonth', '2026-02-28T12:34'],
      ['payment.oneYear', '2027-01-31T12:34'],
    ]) {
      const button = wrapper.findAll('button').find((candidate) => candidate.text() === label)!
      expect(button.attributes('type')).toBe('button')
      await button.trigger('click')
      expect(input.element.value).toBe(expected)
      expect(createAccountMock).not.toHaveBeenCalled()
    }

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]?.expires_at).toBe(new Date('2027-01-31T12:34:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('allows a manually entered expiry to override a preset before account creation', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('custom expiry account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await selectButtonByText(wrapper, 'payment.oneMonth')
    await wrapper.get('input[type="datetime-local"]').setValue('2030-04-15T09:20')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]?.expires_at).toBe(new Date('2030-04-15T09:20:00').getTime() / 1000)
    wrapper.unmount()
  })

  it('hides only the redundant account toggle when every selected group enables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: true },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('keeps the account toggle when any selected group disables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: false },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('sends false explicitly for normal OpenAI account creation by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('omits the upstream request id header from extra when left empty', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra).not.toHaveProperty('upstream_request_id_header')
  })

  it('sends the trimmed upstream request id header in extra when filled', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('openai account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="upstream-request-id-header"]').setValue('  X-Oneapi-Request-Id  ')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.upstream_request_id_header).toBe('X-Oneapi-Request-Id')
  })

  it('omits images_url_to_b64_json from extra by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra).not.toHaveProperty('images_url_to_b64_json')
  })

  it('sends images_url_to_b64_json in extra when the toggle is enabled', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('openai account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="openai-images-url-to-b64-json-toggle"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.images_url_to_b64_json).toBe(true)
  })

  it('requires saved-identity confirmation after creating an account from preview', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenCode account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await openReview(wrapper)
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledOnce()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#42')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('includes the current concrete model mapping in preview credentials', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await openReview(wrapper)
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await flushPromises()

    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({
      model_mapping: { 'public-glm': 'public-glm' }
    })
  })

  it('retains the created account ID for confirmation with explicit mappings', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Mapped account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await openReview(wrapper)
    await selectButtonByText(wrapper, 'admin.accounts.addMapping')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('public-glm')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('glm-5.3')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials?.model_mapping).toEqual({
      'public-glm': 'glm-5.3'
    })
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#42')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not automatically adopt incomplete post-create metadata', async () => {
    syncUpstreamModelsMock.mockResolvedValue({
      candidate_models: ['x-preview-f-free'], models: ['x-preview-f-free'],
      warnings: [{ code: 'upstream_model_metadata_incomplete', message: 'metadata incomplete' }],
    })
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenCode account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await openReview(wrapper)
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#42')
  })

  it('requires confirmation instead of automatically adopting partial metadata', async () => {
    syncUpstreamModelsMock.mockResolvedValue({
      candidate_models: ['x-preview-f-free'], models: ['x-preview-f-free'],
      warnings: [{ code: 'upstream_model_metadata_partial', message: 'metadata incomplete' }],
    })
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenCode account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await openReview(wrapper)
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#42')
    expect(showWarningMock).not.toHaveBeenCalled()
  })

  // namespace 摊平是仅 OAuth 的兼容开关：API Key 走 chat completions 回退桥时由桥自行摊平
  it('shows the Codex namespace flatten toggle only for OpenAI OAuth accounts', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')

    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      true
    )

    await selectButtonByText(wrapper, 'API Key')
    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      false
    )
  })

  it('enables upstream billing probes by default for new OpenAI API key accounts', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('waits for the initial upstream billing probe before refreshing the account list', async () => {
    let resolveProbe: (() => void) | undefined
    probeUpstreamBillingMock.mockImplementationOnce(
      () => new Promise<void>((resolve) => {
        resolveProbe = resolve
      })
    )

    const wrapper = await submitApiKeyAccount('openai')

    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('created')).toBeUndefined()

    resolveProbe?.()
    await flushPromises()

    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it('sends an explicit disabled state when the create toggle is turned off', async () => {
    await submitApiKeyAccount('openai', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
    expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
  })

  it('creates independent Bedrock with the existing regional credential form', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="bedrock-platform"]').trigger('click')
    expect(wrapper.get('[data-testid="bedrock-platform"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Bedrock fixture')
    await wrapper.get('input[type="radio"][value="apikey"]').setValue()
    await wrapper.get('form#create-account-form input[type="password"]').setValue('fixture-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'anthropic', type: 'bedrock', credentials: { auth_mode: 'apikey', api_key: 'fixture-key', aws_region: 'us-east-1' } })
  })

  it.each(Object.entries(CHAT_PLATFORM_BASE_URLS))('creates %s with exact chat credentials', async (platform, baseURL) => {
    const wrapper = mountModal()
    await wrapper.get(`[data-testid="${platform}-platform"]`).trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('runtime fixture')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('fixture-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform, type: 'apikey', credentials: { api_key: 'fixture-key', base_url: baseURL } })
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials.api_base_urls).toBeUndefined()
  })

  it.each(['qianwen', 'commandcode'] as const)('creates %s as API-key-only with protocol defaults', async (platform) => {
    const wrapper = mountModal()
    await wrapper.get(`[data-testid="${platform}-platform"]`).trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue(platform)
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-sp-test-only')
    if (platform === 'qianwen') expect(wrapper.find('[data-testid="qianwen-coding-plan-scope"]').exists()).toBe(false)
    if (platform === 'commandcode') {
      expect(wrapper.find('[data-testid="cn-adaptive-base-url-responses"]').exists()).toBe(true)
      expect(wrapper.find('option[value="responses"]').exists()).toBe(true)
    }
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0][0]
    expect(payload.upstream_billing_probe_enabled).toBe(true)
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    expect(wrapper.find('[data-testid="upstream-billing-auto-probe"]').exists()).toBe(true)
    expect(payload).toMatchObject({ platform, type: 'apikey', credentials: { account_mode: 'payg', api_protocol: 'adaptive', api_key: 'sk-sp-test-only' } })
    if (platform === 'qianwen') {
      expect(payload.credentials.api_base_urls).toEqual({ chat_completions: 'https://dashscope.aliyuncs.com/compatible-mode/v1', responses: 'https://dashscope.aliyuncs.com/compatible-mode/v1', anthropic: 'https://dashscope.aliyuncs.com/apps/anthropic' })
    } else {
      expect(payload.credentials.protocol_rules).toEqual([{ pattern: 'claude-*', protocol: 'anthropic' }, { pattern: 'anthropic/claude-*', protocol: 'anthropic' }])
      expect(payload.credentials.api_base_urls).toEqual({ chat_completions: 'https://api.commandcode.ai/provider/v1', anthropic: 'https://api.commandcode.ai/provider/v1', responses: 'https://api.commandcode.ai/provider/v1' })
    }
  })

  it('submits OpenCode Zen default protocol rules with adaptive endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenCode')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('oc')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-opencode-zen')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
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

  it('submits OpenCode GO endpoints after switching account type', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenCode')
    await selectButtonByText(wrapper, 'admin.accounts.opencodeGo.accountMode.go')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('oc-go')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-opencode-go')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'go',
      api_protocol: 'adaptive',
      base_url: 'https://opencode.ai/zen/go/v1',
      api_base_urls: {
        chat_completions: 'https://opencode.ai/zen/go/v1',
        anthropic: 'https://opencode.ai/zen/go',
        responses: 'https://opencode.ai/zen/go/v1'
      },
      protocol_rules: [
        { pattern: 'grok-*', protocol: 'responses' },
        { pattern: 'gpt-*', protocol: 'responses' },
        { pattern: 'muse-spark-*', protocol: 'responses' },
        { pattern: 'minimax-*', protocol: 'anthropic' },
        { pattern: 'qwen*', protocol: 'anthropic' }
      ]
    })
  })

  it('submits adaptive Kimi protocol endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Kimi adaptive')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-kimi')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
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

  it('submits adaptive Kimi Coding Plan Responses endpoint', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.coding')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Kimi coding')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-kimi-coding')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'coding',
      api_protocol: 'adaptive',
      base_url: 'https://api.kimi.com/coding/v1',
      api_base_urls: {
        chat_completions: 'https://api.kimi.com/coding/v1',
        anthropic: 'https://api.kimi.com/coding',
        responses: 'https://api.kimi.com/coding/v1'
      }
    })
  })

  it('submits adaptive MiniMax protocol endpoints', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'MiniMax')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('MiniMax adaptive')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-minimax')

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: 'https://api.minimaxi.com/v1',
      api_base_urls: {
        chat_completions: 'https://api.minimaxi.com/v1',
        anthropic: 'https://api.minimaxi.com/anthropic',
        responses: 'https://api.minimaxi.com/v1'
      }
    })
  })

  it('uses the edited adaptive Chat endpoint when previewing upstream models', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Kimi')
    await wrapper
      .get('[data-testid="cn-adaptive-base-url-chat_completions"]')
      .setValue('https://relay.example.com/v1')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-relay')
    await openReview(wrapper)

    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({
      platform: 'kimi',
      type: 'apikey',
      base_url: 'https://relay.example.com/v1',
      api_key: 'sk-relay',
      credentials: { api_protocol: 'adaptive', account_mode: 'payg', api_base_urls: { chat_completions: 'https://relay.example.com/v1' } }
    })
  })

  it('exposes Agent Identity in the OpenAI authorization methods', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)

    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    expect(flow.props('showManualOption')).toBe(true)
    expect(flow.props('showCodexSessionImportOption')).toBe(true)
    expect(flow.props('showAgentIdentityOption')).toBe(true)
    expect(flow.props('showCodexPatOption')).toBe(true)
    expect(flow.props('initialInputMethod')).toBe('manual')
  })

  it.each([
    ['camelCase', { authMode: 'agentIdentity', agentIdentity: { agentRuntimeId: 'runtime' } }],
    ['nested identity without auth_mode', { agent_identity: { agent_runtime_id: 'runtime' } }],
  ])('accepts backend-compatible %s Agent Identity imports', async (_name, content) => {
    const wrapper = await openCodexImportStep()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.inputMethod = 'agent_identity'

    flow.vm.$emit('import-codex-session', JSON.stringify(content))
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
  })

  it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
    await submitApiKeyAccount('openai', true)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('omits the OpenAI setting for non-OpenAI account creation', async () => {
    await submitApiKeyAccount('anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
    // 上游倍率探测已放宽到全部 API-key 平台：非 OpenAI 平台与 OpenAI 一致，默认开启。
    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('sends an explicit disabled state when the non-OpenAI create toggle is turned off', async () => {
    await submitApiKeyAccount('anthropic', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
  })

  it('antigravity upstream 创建默认携带上游倍率探测开关', async () => {
    // antigravity upstream 走独立创建 helper，
    // 也必须与其余 API-key 平台一样默认开启探测并传递开关。
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Antigravity')
    await selectButtonByText(wrapper, 'admin.accounts.types.antigravityApikey')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('antigravity relay')
    const baseInput = wrapper
      .findAll('input')
      .find((candidate) => candidate.attributes('placeholder') === 'https://cloudcode-pa.googleapis.com')
    expect(baseInput).toBeDefined()
    await baseInput?.setValue('https://relay.example')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-upstream')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload?.platform).toBe('antigravity')
    expect(payload?.type).toBe('apikey')
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    // 创建成功后前端立即发起一次首探（与其他 apikey 平台一致）。
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
  })

  it('leaves Codex session import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('retains imported Codex account IDs for explicit catalog confirmation', async () => {
    createAccountMock.mockResolvedValueOnce({ id: 81, platform: 'openai' })
    importCodexSessionMock.mockResolvedValueOnce({ total: 1, created: 0, updated: 0, skipped: 0, failed: 0, ready: [{ account: { name: 'Codex', platform: 'openai', type: 'oauth', credentials: {}, credential_draft: 'codex-draft', credential_draft_revision: 'initial' } }] })
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()
    expect(importCodexSessionMock.mock.calls[0][0].extra?.model_capability_defaults).toBeUndefined()
    expect(createAccountMock).not.toHaveBeenCalled()
    await confirmReview(wrapper)
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#81')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('retains a created Codex PAT account ID for explicit catalog confirmation', async () => {
    createAccountMock.mockResolvedValueOnce({ id: 82, platform: 'openai' })
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    await confirmReview(wrapper)
    expect(wrapper.get('[data-testid="created-catalog-accounts"]').text()).toContain('#82')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(syncUpstreamModelsMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('leaves Codex PAT import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('sends explicit true for Codex session import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex session import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('sends explicit true for Codex PAT import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex PAT import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })
  it('creates a Cursor relay with an explicit probe preference and initial probe', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Cursor')
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('cursor relay')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('relay-key')
    await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    expect(wrapper.get('[data-testid="upstream-billing-auto-probe"]').exists()).toBe(true)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'cursor', type: 'upstream', upstream_billing_probe_enabled: true })
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
  })

  it('renders one account type entry before protocol and credentials', async () => {
    const wrapper = mountModal()
    // Cursor：三张一级卡先于协议与凭据，字段组件不再持有类型选择
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    const cursorEntry = wrapper.get('[data-tour="account-form-type"]')
    expect(cursorEntry.attributes('role')).toBe('radiogroup')
    const cursorCards = cursorEntry.findAll('button[role="radio"]')
    expect(cursorCards.map(card => card.attributes('data-testid'))).toEqual(['cursor-key-mode-auto', 'cursor-key-mode-official', 'cursor-key-mode-relay'])
    expect(cursorCards.map(card => card.attributes('aria-checked'))).toEqual(['false', 'true', 'false'])
    expect(wrapper.findAll('[data-testid^="cursor-key-mode-"]')).toHaveLength(3)
    expect(wrapper.getComponent(CursorKeyFields).findAll('[data-testid^="cursor-key-mode-"]')).toHaveLength(0)
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    expect(wrapper.get('[data-testid="account-protocol-choices"]').findAll('button[role="radio"]').map(card => card.attributes('data-testid')))
      .toEqual(['cursor-relay-protocol-adaptive', 'cursor-relay-protocol-chat_completions'])
    const orderedTargets = [...wrapper.get('form#create-account-form').element.querySelectorAll('[data-tour="account-form-type"], [data-testid="account-protocol-choices"], [data-testid="cursor-api-key"]')]
    expect(orderedTargets.map(element => element.getAttribute('data-tour') ?? element.getAttribute('data-testid')))
      .toEqual(['account-form-type', 'account-protocol-choices', 'cursor-api-key'])
    // 四个双模式平台：模式卡就是唯一一组一级选择，没有重复的模式入口或只读摘要卡
    for (const platform of ['kimi', 'zhipu', 'minimax', 'opencode_go']) {
      await wrapper.get(`[data-testid="${platform}-platform"]`).trigger('click')
      await flushPromises()
      const entries = wrapper.findAll('[data-tour="account-form-type"]')
      expect(entries).toHaveLength(1)
      expect(entries[0]!.findAll('button[role="radio"]')).toHaveLength(2)
      expect(wrapper.find('[data-tour="account-form-mode"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="account-type-summary"]').exists()).toBe(false)
    }
    // DeepSeek 保留单项 PayG 展示与旧模式锚点，不制造 Coding
    await wrapper.get('[data-testid="deepseek-platform"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-tour="account-form-mode"]').findAll('button[role="radio"]')).toHaveLength(1)
    expect(wrapper.find('[data-tour="account-form-type"]').exists()).toBe(false)
    // Command Code：一张只读说明卡，无可操作的假 radio
    await wrapper.get('[data-testid="commandcode-platform"]').trigger('click')
    await flushPromises()
    const commandCodeEntry = wrapper.get('[data-tour="account-form-type"]')
    expect(commandCodeEntry.findAll('button')).toHaveLength(0)
    expect(commandCodeEntry.findAll('[role="radio"]')).toHaveLength(0)
    expect(commandCodeEntry.get('[data-testid="commandcode-account-type-card"]').exists()).toBe(true)
    // 千问：维持没有账号类型选择区的现状，不为引导造卡
    await wrapper.get('[data-testid="qianwen-platform"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-tour="account-form-type"]').exists()).toBe(true)
    expect(wrapper.find('[data-tour="account-form-mode"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="commandcode-account-type-card"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['auto', 'oauth'],
    ['official', 'apikey'],
    ['relay', 'upstream'],
  ] as const)('aligns Cursor category preview and submitted type (%s)', async (mode, expectedType) => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get(`[data-testid="cursor-key-mode-${mode}"]`).trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue(`Cursor ${mode}`)
    if (mode === 'auto') {
      for (const [field, value] of Object.entries({ access_token: 'synthetic-access', refresh_token: 'synthetic-refresh', machine_id: 'a'.repeat(64), mac_machine_id: 'b'.repeat(64) })) {
        await wrapper.get(`[data-testid="cursor-${field}"]`).setValue(value)
      }
    } else {
      await wrapper.get('[data-testid="cursor-api-key"]').setValue('crsr_test')
    }
    if (mode === 'relay') await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    // 原生模式没有 Relay 协议控件；Relay 首次进入为 Adaptive
    expect(wrapper.find('[data-testid="cursor-relay-protocol-adaptive"]').exists()).toBe(mode === 'relay')
    expect(wrapper.find('[data-testid="cursor-relay-protocol-chat_completions"]').exists()).toBe(mode === 'relay')
    if (mode === 'relay') expect(wrapper.get('[data-testid="cursor-relay-protocol-adaptive"]').attributes('aria-checked')).toBe('true')
    await openReview(wrapper)
    await flushPromises()
    // 预览连接输入与最终创建读同一个实际类型
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({ platform: 'cursor', type: expectedType })
    await confirmReview(wrapper)
    await flushPromises()
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({ platform: 'cursor', type: expectedType })
    if (expectedType === 'upstream') {
      expect(payload.credentials).toMatchObject({ api_protocol: 'adaptive', base_url: 'https://relay.example/v1' })
    } else {
      expect(payload.credentials).not.toHaveProperty('api_protocol')
      expect(payload.credentials).not.toHaveProperty('base_url')
      expect(payload.credentials).not.toHaveProperty('api_base_urls')
    }
    // 切换后等待渲染，派生类型仍与预览/创建一致
    await flushPromises()
    expect((wrapper.vm as unknown as { form: { type: string } }).form.type).toBe(expectedType)
    wrapper.unmount()
  })

  it.each(['adaptive', 'chat_completions'] as const)('creates and previews Cursor relays with the selected protocol (%s)', async (protocol) => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Cursor relay')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('relay-key')
    await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    await wrapper.get(`[data-testid="cursor-relay-protocol-${protocol}"]`).trigger('click')
    await flushPromises()
    // 同一 Relay 内切协议保留 URL/Key
    expect((wrapper.get('[data-testid="cursor-api-key"]').element as HTMLInputElement).value).toBe('relay-key')
    expect((wrapper.get('[data-testid="cursor-base-url"]').element as HTMLInputElement).value).toBe('https://relay.example/v1')
    await openReview(wrapper)
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({
      platform: 'cursor', type: 'upstream', base_url: 'https://relay.example/v1', api_key: 'relay-key',
      credentials: { api_protocol: protocol, base_url: 'https://relay.example/v1' }
    })
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'cursor', type: 'upstream',
      credentials: { api_protocol: protocol, base_url: 'https://relay.example/v1', api_key: 'relay-key' }
    })
    wrapper.unmount()
  })

  it('ignores relay preview results from an earlier protocol', async () => {
    const previewMock = vi.mocked(accountsAPI.syncUpstreamModelsPreview)
    previewMock.mockClear()
    const wrapper = mountModal([], true)
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Cursor relay preview')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('relay-key')
    await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    await openReview(wrapper)

    let resolvePreview!: (value: unknown) => void
    previewMock.mockImplementationOnce(() => new Promise(resolve => { resolvePreview = resolve }))
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(previewMock).toHaveBeenCalledTimes(1)
    expect(previewMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'cursor', type: 'upstream', credentials: { api_protocol: 'adaptive' } })

    // 切协议产生新的连接输入：在途结果不得被展示成当前连接验证成功
    await wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').trigger('click')
    await flushPromises()
    resolvePreview({ candidate_models: ['late-adaptive-model'], models: ['late-adaptive-model'], discovery_complete: true, complete: true })
    await flushPromises()
    expect(wrapper.text()).not.toContain('late-adaptive-model')
    expect(wrapper.find('[data-testid="add-preview-models"]').exists()).toBe(false)
    expect((wrapper.get('[data-testid="cursor-api-key"]').element as HTMLInputElement).value).toBe('relay-key')
    expect((wrapper.get('[data-testid="cursor-base-url"]').element as HTMLInputElement).value).toBe('https://relay.example/v1')

    // 返回表单重新提交后，预览与创建都使用改选后的协议
    await wrapper.findAll('button').find(button => button.text() === 'common.back')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="account-review"]').exists()).toBe(false)
    await openReview(wrapper)
    await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
    await flushPromises()
    expect(previewMock.mock.calls.at(-1)?.[0]).toMatchObject({ platform: 'cursor', type: 'upstream', credentials: { api_protocol: 'chat_completions' } })
    await confirmReview(wrapper)
    await flushPromises()
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'cursor', type: 'upstream', credentials: { api_protocol: 'chat_completions' } })
    wrapper.unmount()
  })

  it.each([
    ['kimi', 'admin.accounts.cnProviders.accountMode.payg', 'payg'],
    ['kimi', 'admin.accounts.cnProviders.accountMode.coding', 'coding'],
    ['zhipu', 'admin.accounts.cnProviders.accountMode.coding', 'coding'],
    ['minimax', 'admin.accounts.cnProviders.accountMode.coding', 'coding'],
    ['zhipu', 'admin.accounts.cnProviders.accountMode.payg', 'payg'],
    ['opencode_go', 'admin.accounts.opencodeGo.accountMode.zen', 'zen'],
    ['opencode_go', 'admin.accounts.opencodeGo.accountMode.go', 'go'],
  ] as const)('creates package and metered accounts with existing mode values (%s %s)', async (platform, modeLabel, expectedMode) => {
    const wrapper = mountModal()
    await wrapper.get(`[data-testid="${platform}-platform"]`).trigger('click')
    await selectButtonByText(wrapper, modeLabel)
    // 智谱团队版字段只走原有 Coding 分支
    if (platform === 'zhipu' && expectedMode === 'coding') {
      await wrapper.get('input[placeholder="admin.accounts.cnProviders.zhipuTeam.organizationPlaceholder"]').setValue('team-org')
      await wrapper.get('input[placeholder="admin.accounts.cnProviders.zhipuTeam.projectPlaceholder"]').setValue('team-project')
    }
    if (platform === 'zhipu' && expectedMode === 'payg') {
      await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.coding')
      await wrapper.get('input[placeholder="admin.accounts.cnProviders.zhipuTeam.organizationPlaceholder"]').setValue('team-org')
      await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.payg')
    }
    await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} ${expectedMode}`)
    await wrapper.get('form#create-account-form input[type="password"]').setValue('fixture-key')
    await openReview(wrapper)
    await confirmReview(wrapper)
    await flushPromises()
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({ platform, type: 'apikey', credentials: { account_mode: expectedMode, api_protocol: 'adaptive', api_key: 'fixture-key' } })
    expect(Object.keys(payload.credentials.api_base_urls ?? {}).length).toBeGreaterThan(0)
    if (platform === 'zhipu' && expectedMode === 'coding') {
      expect(payload.credentials).toMatchObject({ zhipu_organization: 'team-org', zhipu_project: 'team-project' })
    } else {
      expect(payload.credentials).not.toHaveProperty('zhipu_organization')
      expect(payload.credentials).not.toHaveProperty('zhipu_project')
    }
    // 智谱与 CommandCode 同样提供原生 Responses；Kimi/MiniMax/OpenCode 保持四项
    const protocolCount = wrapper.findAll('[data-testid="account-protocol-choices"] > button[role="radio"]').length
    expect(protocolCount).toBe(4)
    wrapper.unmount()
  })

  it('preserves intentional defaults while changing platforms', async () => {
    const wrapper = mountModal()
    const state = wrapper.vm as unknown as { accountMode: string; openCodeAccountMode: string; form: { type: string } }
    // Kimi Coding 在智谱继续有效
    await wrapper.get('[data-testid="kimi-platform"]').trigger('click')
    await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.coding')
    await wrapper.get('[data-testid="zhipu-platform"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-tour="account-form-type"]').get('button[aria-checked="true"]').text()).toContain('admin.accounts.cnProviders.accountMode.coding')
    // 千问 / DeepSeek 固定 PayG
    await wrapper.get('[data-testid="qianwen-platform"]').trigger('click')
    await flushPromises()
    expect(state.accountMode).toBe('payg')
    await wrapper.get('[data-testid="deepseek-platform"]').trigger('click')
    await flushPromises()
    expect(state.accountMode).toBe('payg')
    // OpenCode 进入即默认 Zen
    await wrapper.get('[data-testid="opencode_go-platform"]').trigger('click')
    await flushPromises()
    expect(state.openCodeAccountMode).toBe('zen')
    // 回到 Kimi 选定 Coding，准备验证 Relay 草稿不会跨平台泄漏
    await wrapper.get('[data-testid="kimi-platform"]').trigger('click')
    await selectButtonByText(wrapper, 'admin.accounts.cnProviders.accountMode.coding')
    await flushPromises()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await wrapper.get('[data-testid="cursor-relay-protocol-chat_completions"]').trigger('click')
    await wrapper.get('[data-testid="cursor-api-key"]').setValue('relay-key')
    await wrapper.get('[data-testid="cursor-base-url"]').setValue('https://relay.example/v1')
    // 重复点击已选项不清空输入
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-testid="cursor-api-key"]').element as HTMLInputElement).value).toBe('relay-key')
    expect((wrapper.get('[data-testid="cursor-base-url"]').element as HTMLInputElement).value).toBe('https://relay.example/v1')
    // Relay → 其他平台：upstream 分类被清除，目标平台端点不被晚到的 Cursor 清理覆盖
    await wrapper.get('[data-testid="kimi-platform"]').trigger('click')
    await flushPromises()
    expect(state.form.type).toBe('apikey')
    expect((wrapper.get('[data-testid="cn-adaptive-base-url-chat_completions"]').element as HTMLInputElement).value).toBe('https://api.kimi.com/coding/v1')
    // 回到 Cursor 为官方 Key，不泄漏 upstream 分类或 Relay 协议
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="cursor-key-mode-official"]').attributes('aria-checked')).toBe('true')
    expect(state.form.type).toBe('apikey')
    expect(wrapper.find('[data-testid="cursor-relay-protocol-chat_completions"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cursor-base-url"]').exists()).toBe(false)
    // 重新进入 Relay 回到 Adaptive，不跨模式保存 Relay 草稿
    await wrapper.get('[data-testid="cursor-key-mode-relay"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="cursor-relay-protocol-adaptive"]').attributes('aria-checked')).toBe('true')
    expect((wrapper.get('[data-testid="cursor-api-key"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('[data-testid="cursor-base-url"]').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })

  it('finishes Cursor browser authorization without a second create', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="cursor-platform"]').trigger('click')
    await wrapper.get('[data-testid="cursor-key-mode-auto"]').trigger('click')
    await flushPromises()
    // 手动 Auto 与浏览器授权并存，手动 Token 输入不依赖浏览器
    expect(wrapper.find('[data-testid="cursor-access_token"]').exists()).toBe(true)
    const authorization = wrapper.getComponent(CursorBrowserAuthorization)
    createAccountMock.mockResolvedValueOnce({ id: 93, platform: 'cursor' })
    authorization.vm.$emit('draft', { credential_draft: 'browser-draft', revision: 'initial', platform: 'cursor', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' })
    await flushPromises()
    // 授权成功只登记一次复核，不额外调用创建接口
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="account-model-configuration"]')).toHaveLength(1)
    expect(wrapper.getComponent(ModelWhitelistSelectorStub).props('syncCredentials')).toMatchObject({ type: 'oauth', credential_draft: 'browser-draft', credential_draft_revision: 'initial' })
    // 已保存账号 ID 事件不触发第二次创建；切换模式真实卸载浏览器授权且旧结果不污染新表单
    authorization.vm.$emit('success', 93)
    await wrapper.get('[data-testid="cursor-key-mode-official"]').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(CursorBrowserAuthorization).exists()).toBe(false)
    await wrapper.get('[data-testid="cursor-key-mode-auto"]').trigger('click')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="account-model-configuration"]')).toHaveLength(1)
    await confirmReview(wrapper)
    await flushPromises()
    // 整个授权流程只创建一次，已保存账号 ID 只登记一次
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ platform: 'cursor', type: 'oauth', credential_draft: 'browser-draft', credential_draft_revision: 'reviewed' })
    const saved = wrapper.get('[data-testid="created-catalog-accounts"]')
    expect(saved.findAll('h3')).toHaveLength(1)
    expect(saved.text()).toContain('#93')
    wrapper.unmount()
  })

  it('describes CommandCode windows without changing its credential contract', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="commandcode-platform"]').trigger('click')
    const entry = wrapper.get('[data-tour="account-form-type"]')
    const card = entry.get('[data-testid="commandcode-account-type-card"]')
    expect(card.attributes('role')).toBe('note')
    expect(card.text()).toContain('admin.accounts.platformDetails.commandcodeAccountTypeTitle')
    expect(card.text()).toContain('admin.accounts.platformDetails.commandcodeAccountTypeDescription')
    expect(card.findAll('button')).toHaveLength(0)
    expect(entry.findAll('[role="radio"]')).toHaveLength(0)
    expect(wrapper.find('[data-tour="account-form-mode"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="account-type-summary"]').exists()).toBe(false)
    // 说明含上游条件、5 小时/周窗口与本地回落，不承诺固定额度
    const description = zhAdminAccounts.accounts.platformDetails.commandcodeAccountTypeDescription
    for (const phrase of ['上游返回有效数据', '5 小时', '本地统计', '不代表剩余额度']) expect(description).toContain(phrase)
    expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
    // 兼容载荷与探测行为不变
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Command Code')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-commandcode')
    await openReview(wrapper)
    await confirmReview(wrapper)
    await flushPromises()
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload).toMatchObject({ platform: 'commandcode', type: 'apikey', upstream_billing_probe_enabled: true, credentials: { account_mode: 'payg', api_protocol: 'adaptive', api_key: 'sk-commandcode' } })
    expect(payload.credentials.protocol_rules).toEqual([{ pattern: 'claude-*', protocol: 'anthropic' }, { pattern: 'anthropic/claude-*', protocol: 'anthropic' }])
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    wrapper.unmount()
  })

})


it('opens and resets without inferred Claude selections and keeps foreign authentication hints empty', async () => {
  const wrapper = mountModal()
  const state = wrapper.vm as unknown as { allowedModels: string[]; apiKeyHint: string; modelRestrictionMode: string }
  expect(state.allowedModels).toEqual([])
  await wrapper.get('[data-testid="qianfan-platform"]').trigger('click')
  await flushPromises()
  expect(state.allowedModels).toEqual([])
  expect(state.apiKeyHint).toBe('')
  state.allowedModels = ['manually-chosen']
  state.modelRestrictionMode = 'mapping'
  await flushPromises()
  state.modelRestrictionMode = 'whitelist'
  await flushPromises()
  expect(state.allowedModels).toEqual(['manually-chosen'])
  await wrapper.setProps({ show: false })
  await wrapper.setProps({ show: true })
  await flushPromises()
  expect(state.allowedModels).toEqual([])
  wrapper.unmount()
})


it.each(['anthropic', 'gemini'] as const)('stages %s OAuth with no writes, then saves the selected models with the reviewed revision', async platform => {
  createAccountMock.mockReset().mockResolvedValue({ id: 142, platform })
  const wrapper = mountModal()
  const workflow = wrapper.vm as unknown as { createWithOptions: (payload: CreateAccountRequest) => Promise<never>; allowedModels: string[] }
  await workflow.createWithOptions({ name: 'OAuth review', platform, type: 'oauth', concurrency: 1, priority: 1,
    credentials: { credential_draft: `${platform}-draft`, revision: 'initial', platform, type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } }).catch(() => {})
  await flushPromises()
  expect(createAccountMock).not.toHaveBeenCalled()
  expect(wrapper.findAll('[data-testid="account-model-configuration"]')).toHaveLength(1)
  workflow.allowedModels = ['chosen-model']
  await confirmReview(wrapper)
  expect(createAccountMock).toHaveBeenCalledOnce()
  expect(createAccountMock.mock.calls[0][0]).toMatchObject({ credential_draft: `${platform}-draft`, credential_draft_revision: 'reviewed', credentials: { model_mapping: { 'chosen-model': 'chosen-model' } } })
  wrapper.unmount()
})

it('cancels an OAuth review with no account writes and preserves the credential form for retry', async () => {
  createAccountMock.mockReset()
  const wrapper = mountModal()
  const workflow = wrapper.vm as unknown as { createWithOptions: (payload: CreateAccountRequest) => Promise<never>; cancelAccountReview: () => Promise<void>; form: { name: string } }
  workflow.form.name = 'Keep this name'
  await workflow.createWithOptions({ name: workflow.form.name, platform: 'anthropic', type: 'oauth', concurrency: 1, priority: 1,
    credentials: { credential_draft: 'cancelled-draft', revision: 'initial', platform: 'anthropic', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } }).catch(() => {})
  await workflow.cancelAccountReview()
  await flushPromises()
  expect(apiClient.delete).toHaveBeenCalledWith('/admin/accounts/credential-drafts/cancelled-draft')
  expect(createAccountMock).not.toHaveBeenCalled()
  expect(workflow.form.name).toBe('Keep this name')
  expect(wrapper.find('[data-testid="account-review"]').exists()).toBe(false)
  wrapper.unmount()
})

it('refuses an old confirmation after the model payload changes while its review is pending', async () => {
  createAccountMock.mockReset()
  const wrapper = mountModal()
  const workflow = wrapper.vm as unknown as { createWithOptions: (payload: CreateAccountRequest) => Promise<never>; confirmAccountReview: () => Promise<void>; allowedModels: string[] }
  await workflow.createWithOptions({ name: 'Binding', platform: 'anthropic', type: 'oauth', concurrency: 1, priority: 1,
    credentials: { credential_draft: 'binding-draft', revision: 'initial', platform: 'anthropic', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' } }).catch(() => {})
  let finishReview!: (value: { data: { revision: string } }) => void
  vi.mocked(apiClient.post).mockImplementationOnce(() => new Promise(resolve => { finishReview = resolve }))
  const confirmation = workflow.confirmAccountReview()
  await flushPromises()
  workflow.allowedModels = ['changed-after-confirm']
  finishReview({ data: { revision: 'new-review' } })
  await confirmation
  expect(createAccountMock).not.toHaveBeenCalled()
  expect(wrapper.find('[data-testid="account-review"]').exists()).toBe(true)
  wrapper.unmount()
})


it('selects the DashScope Messages endpoint without rewriting later hand-entered URLs', async () => {
  const wrapper = mountModal()
  await wrapper.get('[data-testid="qianwen-platform"]').trigger('click')
  const state = wrapper.vm as unknown as { accountMode: 'payg' | 'coding'; apiProtocol: string; apiKeyBaseUrl: string; onCnPresetSelect: (preset: { mode: 'payg'; protocol: 'anthropic'; url: string }) => void }
  const endpoint = 'https://dashscope.aliyuncs.com/apps/anthropic'
  state.onCnPresetSelect({ mode: 'payg', protocol: 'anthropic', url: endpoint })
  await flushPromises()
  expect(state.apiKeyBaseUrl).toBe(endpoint)
  expect(state.accountMode).toBe('payg')
  expect(state.apiProtocol).toBe('anthropic')
  state.apiKeyBaseUrl = 'https://hand-entered.example/custom/v1'
  state.accountMode = 'payg'
  await flushPromises()
  expect(state.apiKeyBaseUrl).toBe('https://hand-entered.example/custom/v1')
  state.apiProtocol = 'chat_completions'
  await flushPromises()
  expect(state.apiKeyBaseUrl).toBe('https://hand-entered.example/custom/v1')
  wrapper.unmount()
})

it('focuses a missing API key without losing the rest of the form', async () => {
  createAccountMock.mockReset()
  const host = document.createElement('div')
  document.body.append(host)
  const wrapper = mountModal([], false, host)
  await selectButtonByText(wrapper, 'admin.accounts.claudeConsole')
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Preserve account name')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  const field = wrapper.get('[data-account-field="apiKeyValue"]')
  expect(field.attributes('aria-invalid')).toBe('true')
  expect(document.activeElement).toBe(field.element)
  expect((wrapper.get('form#create-account-form input[type="text"]').element as HTMLInputElement).value).toBe('Preserve account name')
  expect(createAccountMock).not.toHaveBeenCalled()
  wrapper.unmount()
  host.remove()
})


it('retains reviewed OAuth configuration and retries a lost confirmation response without reviewing a consumed draft', async () => {
  createAccountMock.mockReset().mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({ id: 143, platform: 'openai' })
  vi.mocked(apiClient.post).mockClear()
  const wrapper = mountModal()
  const workflow = wrapper.vm as unknown as {
    createWithOptions: (payload: CreateAccountRequest) => Promise<never>
    createPlanType: string
    createSchedulingThreshold: number
  }
  workflow.createPlanType = 'legacy-custom-plan'
  workflow.createSchedulingThreshold = 72
  await workflow.createWithOptions({ name: 'Configured OAuth', platform: 'openai', type: 'oauth', concurrency: 1, priority: 1,
    credentials: { credential_draft: 'configured-draft', revision: 'initial', platform: 'openai', type: 'oauth', expires_at: '2099-01-01T00:00:00Z', intercept_warmup_requests: true } }).catch(() => {})
  await confirmReview(wrapper)
  expect(wrapper.emitted('created')).toBeUndefined()
  expect(createAccountMock.mock.calls[0][0].credentials).toMatchObject({ plan_type: 'legacy-custom-plan', account_scheduling_threshold: 72, intercept_warmup_requests: true })
  const frozen = structuredClone(createAccountMock.mock.calls[0][0])
  await confirmReview(wrapper)
  expect(apiClient.post).toHaveBeenCalledTimes(1)
  expect(createAccountMock.mock.calls[1][0]).toEqual(frozen)
  expect(wrapper.emitted('created')).toHaveLength(1)
  wrapper.unmount()
})


it.each([null, 7])('sends proxy_id=%s in credential previews', async proxyId => {
  createAccountMock.mockClear()
  const wrapper = mountModal([], true)
  await selectButtonByText(wrapper, 'OpenAI')
  await selectButtonByText(wrapper, 'API Key')
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Proxy preview')
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-key')
  ;(wrapper.vm as unknown as { form: { proxy_id: number | null } }).form.proxy_id = proxyId
  await openReview(wrapper)
  const preview = vi.mocked(accountsAPI.syncUpstreamModelsPreview)
  preview.mockClear()
  await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
  await flushPromises()
  expect(preview).toHaveBeenCalledWith(expect.objectContaining({ proxy_id: proxyId, platform: 'openai' }))
  expect(createAccountMock).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('previews OAuth drafts through the selected proxy without exposing draft credentials', async () => {
  createAccountMock.mockClear()
  const wrapper = mountModal([], true)
  await selectButtonByText(wrapper, 'Cursor')
  await wrapper.get('[data-testid="cursor-key-mode-auto"]').trigger('click')
  ;(wrapper.vm as unknown as { form: { proxy_id: number } }).form.proxy_id = 9
  await flushPromises()
  wrapper.getComponent(CursorBrowserAuthorization).vm.$emit('draft', { credential_draft: 'proxy-draft', revision: 'initial', platform: 'cursor', type: 'oauth', expires_at: '2099-01-01T00:00:00Z' })
  await flushPromises()
  const preview = vi.mocked(accountsAPI.syncUpstreamModelsPreview)
  preview.mockClear()
  await wrapper.get('[data-testid="preview-upstream-models"]').trigger('click')
  await flushPromises()
  expect(preview).toHaveBeenCalledWith(expect.objectContaining({ proxy_id: 9, credential_draft: 'proxy-draft', credential_draft_revision: 'initial' }))
  expect(preview.mock.calls[0]?.[0].api_key).toBeUndefined()
  expect(createAccountMock).not.toHaveBeenCalled()
  wrapper.unmount()
})


it('creates Qianwen Coding Plan with isolated defaults and preserves a custom base on mode changes', async () => {
  const wrapper = mountModal()
  await wrapper.get('[data-testid="qianwen-platform"]').trigger('click')
  const state = wrapper.vm as unknown as { accountMode: 'payg' | 'coding'; apiProtocol: string; apiKeyBaseUrl: string }
  state.accountMode = 'coding'
  await flushPromises()
  const scope = wrapper.get('[data-testid="qianwen-coding-plan-scope"]')
  expect(scope.get('a').attributes('href')).toBe('https://help.aliyun.com/zh/model-studio/coding-plan')
  expect(wrapper.get('[data-testid="cn-adaptive-base-url-chat_completions"]').element).toHaveProperty('value', 'https://coding.dashscope.aliyuncs.com/v1')
  expect(wrapper.get('[data-testid="cn-adaptive-base-url-anthropic"]').element).toHaveProperty('value', 'https://coding.dashscope.aliyuncs.com/apps/anthropic')
  expect(wrapper.get('[data-testid="cn-adaptive-base-url-responses"]').element).toHaveProperty('value', '')
  state.apiProtocol = 'chat_completions'
  await flushPromises()
  state.apiKeyBaseUrl = 'https://private.example/custom/v1'
  state.accountMode = 'payg'
  await flushPromises()
  expect(state.apiKeyBaseUrl).toBe('https://private.example/custom/v1')
  wrapper.unmount()
})
