// 后加平台（混元 / 火山方舟 / 千帆 / Mistral）的账号类型、协议卡与站点预设对齐。
//
// 断言依据是后端 domain/platform_capabilities.go 的 MessagesPOST / ResponsesPOST 声明与
// service/hunyuan_adapter.go 的 TokenHub 站点白名单：协议集合与站点清单都从后端来，
// 前端不得自拼地址，也不得给出后端没有声明的协议（mistral 上游只有 chat）。
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { CHAT_PLATFORM_BASE_URLS } from '@/constants/platforms'
import {
  CN_BASE_URL_PRESETS,
  declaredPlatformApiProtocols,
  hasDeclaredNativeResponses,
  type DeclaredProtocolPlatform
} from '../credentialsBuilder'

const { createAccountMock } = vi.hoisted(() => ({ createAccountMock: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: {
    post: vi.fn(async () => ({ data: {} })),
    delete: vi.fn().mockResolvedValue({ data: { cancelled: true } })
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get isSimpleMode() { return true } })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      batchCreate: async (payloads: Record<string, unknown>[]) => {
        const results = []
        for (const payload of payloads) {
          const account = await createAccountMock(payload)
          results.push({ name: payload.name, success: true, id: account.id })
        }
        return { success: results.length, failed: 0, results }
      },
      probeUpstreamBilling: vi.fn().mockResolvedValue({}),
      syncUpstreamModels: vi.fn().mockResolvedValue({ candidate_models: [], models: [], metadata: {} }),
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
      importCodexSession: vi.fn(),
      createOpenAICodexPAT: vi.fn()
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels: vi.fn().mockResolvedValue({ candidate_models: [], models: [], metadata: {} }),
    getAvailableModels: vi.fn().mockResolvedValue([]),
    getModelCatalog: vi.fn().mockResolvedValue({ synced: false, warnings: [], state: { rules_revision: '1' } }),
    syncUpstreamModelsPreview: vi.fn().mockResolvedValue({ candidate_models: [], models: [], discovery_complete: true, complete: true })
  },
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('@/api/admin/platformModelPresets', () => ({ getPlatformModelPresets: vi.fn().mockResolvedValue([]) }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const OAuthAuthorizationFlowStub = defineComponent({ name: 'OAuthAuthorizationFlow', template: '<div />' })
const GroupSelectorStub = defineComponent({ name: 'GroupSelector', props: { modelValue: Array }, template: '<div />' })
const ModelWhitelistSelectorStub = defineComponent({ name: 'ModelWhitelistSelector', props: { modelValue: Array, platform: String, syncCredentials: Object }, template: '<div />' })

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
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
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true
      }
    }
  })
}

/** 协议卡的标题词条：与国产多协议平台共用同一套既有词条，因此用词条反推卡片取值。 */
const PROTOCOL_TITLE_KEYS: Record<string, string> = {
  adaptive: 'admin.accounts.cnProviders.apiProtocol.adaptive',
  chat_completions: 'admin.accounts.cnProviders.apiProtocol.chatCompletions',
  anthropic: 'admin.accounts.cnProviders.apiProtocol.anthropic',
  responses: 'admin.accounts.cnProviders.apiProtocol.responses'
}

function protocolCards(wrapper: ReturnType<typeof mountModal>) {
  return wrapper.get('[data-testid="account-protocol-choices"]').findAll('button[role="radio"]')
}

/** 按渲染顺序列出协议卡取值，未知卡片直接失败（避免新增未声明协议时静默通过）。 */
function protocolCardValues(wrapper: ReturnType<typeof mountModal>): string[] {
  return protocolCards(wrapper).map(card => {
    const text = card.text()
    const match = Object.entries(PROTOCOL_TITLE_KEYS).find(([, titleKey]) => text.startsWith(titleKey))
    expect(match, `unexpected protocol card: ${text}`).toBeDefined()
    return match![0]
  })
}

function ariaCheckedValues(wrapper: ReturnType<typeof mountModal>): string[] {
  return protocolCards(wrapper).map(card => card.attributes('aria-checked') ?? '')
}

async function selectProtocol(wrapper: ReturnType<typeof mountModal>, protocol: string) {
  const card = protocolCards(wrapper).find(candidate => candidate.text().startsWith(PROTOCOL_TITLE_KEYS[protocol]!))
  expect(card, `${protocol} card missing`).toBeDefined()
  await card!.trigger('click')
}

async function selectPlatform(wrapper: ReturnType<typeof mountModal>, platform: string) {
  await wrapper.get(`[data-testid="${platform}-platform"]`).trigger('click')
}

async function submitApiKey(wrapper: ReturnType<typeof mountModal>, name: string, apiKey = 'sk-fixture') {
  await wrapper.get('form#create-account-form input[type="text"]').setValue(name)
  await wrapper.get('form#create-account-form input[type="password"]').setValue(apiKey)
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  const confirm = wrapper.find('[data-testid="confirm-account-review"]')
  if (confirm.exists()) {
    await confirm.trigger('click')
    await flushPromises()
  }
}

/**
 * 混元 TokenHub 站点白名单：从后端源码读取（backend/internal/service/hunyuan_adapter.go 的
 * hunyuanActionBaseURL）：4 个 TokenHub 站点 + 1 个旧版兼容站点。前端预设必须与它逐条一致。
 */
function hunyuanBackendWhitelist(): string[] {
  const source = readFileSync(resolve(process.cwd(), '../backend/internal/service/hunyuan_adapter.go'), 'utf8')
  const constant = (name: string): string => {
    const value = source.match(new RegExp(`const ${name} = "([^"]+)"`))?.[1]
    expect(value, `${name} missing from hunyuan_adapter.go`).toBeTruthy()
    return value!
  }
  const allowedCase = source.match(/case hunyuanCompatibilityBaseURL,([\s\S]*?)hunyuanLegacyBaseURL:/)?.[1]
  expect(allowedCase, 'hunyuan base URL whitelist case missing').toBeTruthy()
  const quoted = [...allowedCase!.matchAll(/"([^"]+)"/g)].map(match => match[1])
  return [constant('hunyuanCompatibilityBaseURL'), ...quoted, constant('hunyuanLegacyBaseURL')]
}

describe('late platform protocol cards', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 77, platform: 'hunyuan', type: 'apikey' })
  })
  it('gives hunyuan the four protocols the backend declares and lets both native ones be selected', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'hunyuan')

    expect(protocolCardValues(wrapper)).toEqual(['adaptive', 'chat_completions', 'anthropic', 'responses'])
    expect(ariaCheckedValues(wrapper)).toEqual(['false', 'true', 'false', 'false'])

    await selectProtocol(wrapper, 'anthropic')
    expect(ariaCheckedValues(wrapper)).toEqual(['false', 'false', 'true', 'false'])

    await selectProtocol(wrapper, 'responses')
    expect(ariaCheckedValues(wrapper)).toEqual(['false', 'false', 'false', 'true'])

    wrapper.unmount()
  })

  it.each(['volcengine', 'qianfan'])('gives %s exactly adaptive, chat_completions and the declared responses', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)

    expect(protocolCardValues(wrapper)).toEqual(['adaptive', 'chat_completions', 'responses'])
    expect(protocolCardValues(wrapper)).not.toContain('anthropic')

    wrapper.unmount()
  })

  it('keeps mistral at the single chat_completions protocol its upstream publishes', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'mistral')

    expect(protocolCardValues(wrapper)).toEqual(['chat_completions'])
    expect(ariaCheckedValues(wrapper)).toEqual(['true'])
    expect(wrapper.find('[data-testid="cn-adaptive-base-url-chat_completions"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cn-adaptive-base-url-anthropic"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cn-adaptive-base-url-responses"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it.each(['hunyuan', 'volcengine', 'qianfan', 'mistral'])('renders %s account type as a selected radio card instead of a bare summary', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)

    const group = wrapper.get('[data-testid="late-platform-account-type"]')
    expect(group.get('[role="radiogroup"]').attributes('aria-label')).toBe('admin.accounts.accountType')
    const radios = group.findAll('[role="radio"]')
    expect(radios).toHaveLength(1)
    expect(radios[0]!.attributes('data-testid')).toBe('late-platform-account-type-apikey')
    expect(radios[0]!.attributes('aria-checked')).toBe('true')
    expect(radios[0]!.attributes('tabindex')).toBe('0')
    expect(radios[0]!.text()).toContain('admin.accounts.apiKey')
    expect(radios[0]!.text()).toContain('admin.accounts.platformDetails.apiKeyOnlyAccountTypeDescription')
    expect(wrapper.find('[data-testid="account-type-summary"]').exists()).toBe(false)

    wrapper.unmount()
  })
})

describe('late platform protocol declarations', () => {
  // 后端描述符导出（internal/domain 的 TestExportPlatformContract）是协议事实的唯一权威，
  // 前端选项集合必须逐条对应；后端改了声明而前端没跟上时这里就会失败。
  interface BackendDescriptor {
    id: string
    chatOnly: boolean
    multiProtocol: boolean
    messagesPost: string
    responsesPost: string
  }

  let backendDescriptors: BackendDescriptor[] = []

  beforeAll(() => {
    const output = execFileSync('go', ['test', '-p', '2', '-count=1', '-run', '^TestExportPlatformContract$', '-v', './internal/domain'], {
      cwd: resolve(process.cwd(), '../backend'),
      encoding: 'utf8',
      timeout: 120_000
    })
    const payload = output.match(/^PLATFORM_CONTRACT=(.+)$/m)?.[1]
    if (!payload) throw new Error('Go platform descriptor export did not execute')
    backendDescriptors = JSON.parse(payload) as BackendDescriptor[]
  }, 130_000)

  it('matches the MessagesPOST / ResponsesPOST facts the backend exports', () => {
    const expected = {
      hunyuan: { messagesPost: 'native', responsesPost: 'native', protocols: ['adaptive', 'chat_completions', 'anthropic', 'responses'] },
      volcengine: { messagesPost: '', responsesPost: 'native', protocols: ['adaptive', 'chat_completions', 'responses'] },
      qianfan: { messagesPost: '', responsesPost: 'native', protocols: ['adaptive', 'chat_completions', 'responses'] },
      mistral: { messagesPost: '', responsesPost: '', protocols: ['chat_completions'] }
    } as const

    for (const [platform, facts] of Object.entries(expected)) {
      const descriptor = backendDescriptors.find(candidate => candidate.id === platform)
      expect(descriptor, `${platform} missing from the Go descriptor export`).toBeDefined()
      expect(descriptor).toMatchObject({ messagesPost: facts.messagesPost, responsesPost: facts.responsesPost })
      expect(declaredPlatformApiProtocols(platform as DeclaredProtocolPlatform)).toEqual([...facts.protocols])
      expect(hasDeclaredNativeResponses(platform)).toBe(facts.responsesPost === 'native')
    }
  }, 130_000)

  it('keeps every other chat-only entry free of native text protocols', () => {
    const chatOnlyIds = backendDescriptors.filter(descriptor => descriptor.chatOnly).map(descriptor => descriptor.id)
    expect(chatOnlyIds.sort()).toEqual(['hunyuan', 'mistral', 'qianfan', 'volcengine'])
    expect(backendDescriptors.find(descriptor => descriptor.id === 'mistral')).toMatchObject({ multiProtocol: false })
    expect(backendDescriptors.find(descriptor => descriptor.id === 'hunyuan')).toMatchObject({ multiProtocol: true })
  })
})

describe('late platform base URL presets', () => {
  it('offers exactly the TokenHub sites the backend whitelists, on every declared protocol', () => {
    const whitelist = hunyuanBackendWhitelist()
    expect(whitelist).toHaveLength(5)
    expect(new Set(whitelist).size).toBe(5)

    for (const protocol of ['chat_completions', 'anthropic', 'responses']) {
      const presetUrls = CN_BASE_URL_PRESETS.hunyuan
        .filter(preset => preset.protocol === protocol)
        .map(preset => preset.url)
      expect([...new Set(presetUrls)].sort(), `${protocol} presets`).toEqual([...whitelist].sort())
      expect(presetUrls, `${protocol} preset count`).toHaveLength(whitelist.length)
    }
    // 火山方舟与千帆在后端只有单一站点常量，没有可核证的备选站点，因此不给预设。
    expect(CN_BASE_URL_PRESETS).not.toHaveProperty('volcengine')
    expect(CN_BASE_URL_PRESETS).not.toHaveProperty('qianfan')
  })

  it('renders the whitelisted sites as preset buttons and fills the base URL on selection', async () => {
    const whitelist = hunyuanBackendWhitelist()
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'hunyuan')
    await selectProtocol(wrapper, 'anthropic')

    const presets = wrapper.findAll('[data-testid="cn-base-url-preset"]')
    expect(presets).toHaveLength(whitelist.length)
    // 默认 base url 就是白名单主站，因此只有对应那条预设处于按下态。
    expect(presets.map(preset => preset.attributes('aria-pressed'))).toEqual(
      whitelist.map(url => String(url === CHAT_PLATFORM_BASE_URLS.hunyuan))
    )

    const intlSite = 'https://tokenhub-intl.tencentmaas.cn/v1'
    expect(whitelist).toContain(intlSite)
    const intlPreset = presets.find(preset => preset.text().includes('tokenhub-intl.tencentmaas.cn/v1'))
    expect(intlPreset).toBeDefined()
    await intlPreset!.trigger('click')
    expect((wrapper.get('[data-account-field="apiKeyBaseUrl"]').element as HTMLInputElement).value).toBe(intlSite)

    wrapper.unmount()
  })
})

describe('late platform credential payloads', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 78, platform: 'hunyuan', type: 'apikey' })
  })

  it('pins the hunyuan Anthropic protocol on the TokenHub site', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'hunyuan')
    await selectProtocol(wrapper, 'anthropic')
    await submitApiKey(wrapper, 'hunyuan anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'hunyuan',
      type: 'apikey',
      credentials: {
        api_key: 'sk-fixture',
        account_mode: 'payg',
        api_protocol: 'anthropic',
        base_url: 'https://tokenhub.tencentmaas.com/v1'
      }
    })
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials.api_base_urls).toBeUndefined()

    wrapper.unmount()
  })

  it('writes the chosen TokenHub site to every hunyuan protocol in adaptive mode', async () => {
    const site = 'https://tokenhub-intl.tencentmaas.com/v1'
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'hunyuan')
    await wrapper.get('[data-account-field="apiKeyBaseUrl"]').setValue(site)
    await selectProtocol(wrapper, 'adaptive')
    await submitApiKey(wrapper, 'hunyuan adaptive')

    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'hunyuan',
      credentials: {
        account_mode: 'payg',
        api_protocol: 'adaptive',
        base_url: site,
        api_base_urls: { chat_completions: site, anthropic: site, responses: site }
      }
    })

    wrapper.unmount()
  })

  it.each([
    ['volcengine', 'https://ark.cn-beijing.volces.com/api/v3'],
    ['qianfan', 'https://qianfan.baidubce.com/v2']
  ])('writes only the declared native protocols for %s in adaptive mode', async (platform, site) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)
    await selectProtocol(wrapper, 'adaptive')
    await submitApiKey(wrapper, `${platform} adaptive`)

    const credentials = createAccountMock.mock.calls[0]?.[0]?.credentials
    expect(credentials).toMatchObject({
      account_mode: 'payg',
      api_protocol: 'adaptive',
      base_url: site,
      api_base_urls: { chat_completions: site, responses: site }
    })
    expect(credentials.api_base_urls).not.toHaveProperty('anthropic')

    wrapper.unmount()
  })

  it('keeps mistral chat-only credentials without any protocol endpoint map', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'mistral')
    await submitApiKey(wrapper, 'mistral chat')

    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'mistral',
      type: 'apikey',
      credentials: {
        account_mode: 'payg',
        api_protocol: 'chat_completions',
        base_url: CHAT_PLATFORM_BASE_URLS.mistral
      }
    })
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials.api_base_urls).toBeUndefined()

    wrapper.unmount()
  })
})
