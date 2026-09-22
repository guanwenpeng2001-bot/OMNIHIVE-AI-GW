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
  defaultCNBaseUrl,
  hasDeclaredNativeResponses,
  isCNProviderPlatform,
  showsAccountModeCard,
  showsCodingPlanMode,
  type CnBaseUrlPresetPlatform,
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

/** 账号类型卡的标题词条：国产多协议供应商与后加四家共用同一条 accountMode.title。 */
const MODE_TITLE_KEYS: Record<string, string> = {
  payg: 'admin.accounts.cnProviders.accountMode.payg',
  coding: 'admin.accounts.cnProviders.accountMode.coding'
}

function accountModeGroup(wrapper: ReturnType<typeof mountModal>) {
  return wrapper.findAll('[role="radiogroup"]').find(
    candidate => candidate.attributes('aria-label') === 'admin.accounts.cnProviders.accountMode.title'
  )
}

/** 按渲染顺序列出账号类型卡取值；未知卡片直接失败（避免少卡/多卡时静默通过）。 */
function accountModeCardValues(wrapper: ReturnType<typeof mountModal>): string[] {
  const group = accountModeGroup(wrapper)
  expect(group, 'account mode card missing').toBeDefined()
  return group!.findAll('button[role="radio"]').map(card => {
    const text = card.text()
    const match = Object.entries(MODE_TITLE_KEYS).find(([, titleKey]) => text.startsWith(titleKey))
    expect(match, `unexpected account mode card: ${text}`).toBeDefined()
    return match![0]
  })
}

async function selectAccountMode(wrapper: ReturnType<typeof mountModal>, mode: 'payg' | 'coding') {
  const group = accountModeGroup(wrapper)
  expect(group, 'account mode card missing').toBeDefined()
  const card = group!.findAll('button[role="radio"]').find(
    candidate => candidate.text().startsWith(MODE_TITLE_KEYS[mode]!)
  )
  expect(card, `${mode} account mode card missing`).toBeDefined()
  await card!.trigger('click')
}

function baseUrlValue(wrapper: ReturnType<typeof mountModal>): string {
  return (wrapper.get('[data-account-field="apiKeyBaseUrl"]').element as HTMLInputElement).value
}

/** 预设按钮文案形如 "Ark (ark.cn-beijing.volces.com/api/v3)"。 */
function baseUrlPresetTexts(wrapper: ReturnType<typeof mountModal>): string[] {
  return wrapper.findAll('[data-testid="cn-base-url-preset"]').map(preset => preset.text())
}

/** 指定平台 × 账号类型 × 协议下的预设地址（预设表是唯一事实源）。 */
function presetUrls(platform: CnBaseUrlPresetPlatform, mode: 'payg' | 'coding', protocol: string): string[] {
  return CN_BASE_URL_PRESETS[platform]
    .filter(preset => preset.mode === mode && preset.protocol === protocol)
    .map(preset => preset.url)
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

    // 按量付费档：白名单里的 5 个站点逐条对应到每个已声明协议。
    const paygPresets = CN_BASE_URL_PRESETS.hunyuan.filter(preset => preset.mode === 'payg')
    for (const protocol of ['chat_completions', 'anthropic', 'responses']) {
      const presetUrls = paygPresets
        .filter(preset => preset.protocol === protocol)
        .map(preset => preset.url)
      expect([...new Set(presetUrls)].sort(), `${protocol} presets`).toEqual([...whitelist].sort())
      expect(presetUrls, `${protocol} preset count`).toHaveLength(whitelist.length)
    }
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

  // 建号表单的占位符曾对后加平台回落成 Anthropic 的 sk-ant-... / https://api.anthropic.com/。
  // 现在按平台给：官方文档有前缀的按文档（千帆 bce-v3/ALTAK-、TokenHub sk-），
  // 没有固定前缀的用中性占位。
  const API_KEY_PLACEHOLDER: Record<string, string> = {
    qianfan: 'bce-v3/ALTAK-...',
    hunyuan: 'sk-...',
    mistral: '<api-key>',
    volcengine: '<api-key>'
  }

  it.each(['qianfan', 'hunyuan', 'mistral', 'volcengine'])('%s keeps its own credential placeholders', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)

    const apiKeyField = wrapper.get('[data-account-field="apiKeyValue"]')
    const baseUrlField = wrapper.get('[data-account-field="apiKeyBaseUrl"]')

    expect(apiKeyField.attributes('placeholder')).not.toBe('sk-ant-...')
    expect(apiKeyField.attributes('placeholder')).toBe(API_KEY_PLACEHOLDER[platform])
    expect(baseUrlField.attributes('placeholder')).not.toContain('anthropic.com')
    expect(baseUrlField.attributes('placeholder')).toBe(CHAT_PLATFORM_BASE_URLS[platform])
    expect((baseUrlField.element as HTMLInputElement).value).toBe(CHAT_PLATFORM_BASE_URLS[platform])

    wrapper.unmount()
  })
})

/**
 * 账号类型卡（按量付费 / Coding Plan）：后加四家与国产多协议供应商对齐。
 * 可见性由 credentialsBuilder 的 showsAccountModeCard / showsCodingPlanMode 单一决定：
 * 千问无卡（后端没有套餐额度端点），DeepSeek 与 Mistral 只有按量付费一项。
 */
describe('late platform account mode card', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 79, platform: 'hunyuan', type: 'apikey' })
  })

  it.each(['hunyuan', 'volcengine', 'qianfan'])('gives %s both the pay-as-you-go and the Coding Plan cards', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)

    expect(accountModeCardValues(wrapper)).toEqual(['payg', 'coding'])
    const group = accountModeGroup(wrapper)!
    expect(group.findAll('button[role="radio"]').map(card => card.attributes('aria-checked'))).toEqual(['true', 'false'])
    // 套餐与按量付费的 Base URL / Key 互不通用，卡上必须写清
    expect(group.text()).toContain('admin.accounts.cnProviders.accountMode.codingIsolatedDesc')
    expect(group.text()).not.toContain('admin.accounts.cnProviders.accountMode.codingDesc')
    expect(wrapper.find('[data-testid="account-mode-payg-only-note"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('leaves mistral with the single pay-as-you-go card and says why', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'mistral')

    expect(accountModeCardValues(wrapper)).toEqual(['payg'])
    expect(accountModeGroup(wrapper)!.text()).toContain('admin.accounts.cnProviders.accountMode.paygDesc')
    expect(wrapper.get('[data-testid="account-mode-payg-only-note"]').text()).toBe(
      'admin.accounts.cnProviders.accountMode.paygOnlyNote'
    )

    wrapper.unmount()
  })

  it.each(['kimi', 'zhipu'])('keeps the %s cards on the existing wording', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)

    expect(accountModeCardValues(wrapper)).toEqual(['payg', 'coding'])
    expect(accountModeGroup(wrapper)!.text()).toContain('admin.accounts.cnProviders.accountMode.codingDesc')
    expect(accountModeGroup(wrapper)!.text()).not.toContain('admin.accounts.cnProviders.accountMode.codingIsolatedDesc')
    expect(wrapper.find('[data-testid="account-mode-payg-only-note"]').exists()).toBe(false)

    wrapper.unmount()
  })

  it('keeps deepseek pay-as-you-go only and qianwen without any account mode card', async () => {
    const wrapper = mountModal()

    await selectPlatform(wrapper, 'deepseek')
    expect(accountModeCardValues(wrapper)).toEqual(['payg'])

    await selectPlatform(wrapper, 'qianwen')
    expect(accountModeGroup(wrapper)).toBeUndefined()

    wrapper.unmount()
  })

  it('covers the four later platforms without widening the CN provider predicate', () => {
    // 回归：模式卡的覆盖范围走独立谓词，后加平台仍不属于国产供应商集合，
    // 因此余额/额度单元格、协议选项与序列化路径不受影响。
    const laterPlatforms = ['hunyuan', 'volcengine', 'qianfan', 'mistral']
    expect(laterPlatforms.map(showsAccountModeCard)).toEqual([true, true, true, true])
    expect(laterPlatforms.map(isCNProviderPlatform)).toEqual([false, false, false, false])

    expect(showsAccountModeCard('qianwen')).toBe(false)
    expect(showsCodingPlanMode('deepseek')).toBe(false)
    expect(showsCodingPlanMode('mistral')).toBe(false)
    expect(showsCodingPlanMode('kimi')).toBe(true)
  })
})

/**
 * 模式必须真的进凭据：后端按 credentials.account_mode 选平台默认 base URL，
 * 界面选中的档位没写进 credentials 就只是一个假选项（用错地址会绕过套餐额度）。
 */
describe('late platform account mode credentials', () => {
  const PAYG_BASE_URLS: Record<string, string> = {
    hunyuan: 'https://tokenhub.tencentmaas.com/v1',
    volcengine: 'https://ark.cn-beijing.volces.com/api/v3',
    qianfan: 'https://qianfan.baidubce.com/v2',
    mistral: 'https://api.mistral.ai/v1'
  }
  const CODING_BASE_URLS: Record<string, string> = {
    hunyuan: 'https://api.lkeap.cloud.tencent.com/coding/v3',
    volcengine: 'https://ark.cn-beijing.volces.com/api/coding/v3',
    qianfan: 'https://qianfan.baidubce.com/v2/tokenplan/personal'
  }

  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 80, platform: 'hunyuan', type: 'apikey' })
  })

  it.each(['hunyuan', 'volcengine', 'qianfan'])('submits %s with account_mode payg and the pay-as-you-go site', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)
    await submitApiKey(wrapper, `${platform} payg`)

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'payg',
      base_url: PAYG_BASE_URLS[platform]
    })

    wrapper.unmount()
  })

  it.each(['hunyuan', 'volcengine', 'qianfan'])('submits the selected coding %s mode and the plan site', async (platform) => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, platform)
    await selectAccountMode(wrapper, 'coding')

    // 切档先把输入框换到套餐站点，避免把按量地址交给套餐 Key。
    expect(baseUrlValue(wrapper)).toBe(CODING_BASE_URLS[platform])

    await submitApiKey(wrapper, `${platform} coding`)

    // 断言原文：credentials.account_mode 等于所选值，base_url 等于厂商公布的套餐站点。
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'coding',
      base_url: CODING_BASE_URLS[platform]
    })

    wrapper.unmount()
  })

  it('submits mistral pay-as-you-go, the only mode its API offers', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'mistral')
    await submitApiKey(wrapper, 'mistral payg')

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'payg',
      base_url: PAYG_BASE_URLS.mistral
    })
    expect(accountModeCardValues(wrapper)).toEqual(['payg'])

    wrapper.unmount()
  })

  it('replaces a selected pay-as-you-go TokenHub site when hunyuan switches to a plan', async () => {
    // 用户点过 TokenHub Intl（按量站点）再切 coding：官方站点被换成套餐站点。
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'hunyuan')
    await wrapper.get('[data-account-field="apiKeyBaseUrl"]').setValue('https://tokenhub-intl.tencentmaas.cn/v1')
    await selectAccountMode(wrapper, 'coding')

    expect(baseUrlValue(wrapper)).toBe(CODING_BASE_URLS.hunyuan)

    wrapper.unmount()
  })

  it('keeps a user-typed relay address across a mode switch', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'volcengine')
    await wrapper.get('[data-account-field="apiKeyBaseUrl"]').setValue('https://relay.example/v1')
    await selectAccountMode(wrapper, 'coding')

    expect(baseUrlValue(wrapper)).toBe('https://relay.example/v1')

    wrapper.unmount()
  })
})

/**
 * 三家套餐站点的逐字断言：错一个字符就会打到按量端点、白花套餐钱
 * （火山方舟官方："请勿使用 https://ark.cn-beijing.volces.com/api/v3 ：该 Base URL 不会消耗
 * 您的 Coding Plan 额度，而是会产生额外费用"）。改这一组字符串里的任何一个字符，本组用例即红。
 */
describe('late platform plan base URL presets', () => {
  it('pins the three vendor plan endpoints character for character', () => {
    expect(presetUrls('hunyuan', 'coding', 'chat_completions')).toEqual([
      'https://api.lkeap.cloud.tencent.com/coding/v3'
    ])
    expect(presetUrls('hunyuan', 'coding', 'anthropic')).toEqual([
      'https://api.lkeap.cloud.tencent.com/coding/anthropic'
    ])
    expect(presetUrls('volcengine', 'coding', 'chat_completions')).toEqual([
      'https://ark.cn-beijing.volces.com/api/coding/v3'
    ])
    expect(presetUrls('volcengine', 'coding', 'anthropic')).toEqual([
      'https://ark.cn-beijing.volces.com/api/coding'
    ])
    expect(presetUrls('qianfan', 'coding', 'chat_completions')).toEqual([
      'https://qianfan.baidubce.com/v2/tokenplan/personal',
      'https://qianfan.baidubce.com/v2/tokenplan/team'
    ])
    expect(presetUrls('qianfan', 'coding', 'anthropic')).toEqual([
      'https://qianfan.baidubce.com/anthropic/tokenplan/personal',
      'https://qianfan.baidubce.com/anthropic/tokenplan/team'
    ])
  })

  it('never mixes plan endpoints into the pay-as-you-go bucket', () => {
    for (const platform of ['hunyuan', 'volcengine', 'qianfan'] as const) {
      const paygUrls = CN_BASE_URL_PRESETS[platform].filter(preset => preset.mode === 'payg').map(preset => preset.url)
      const codingUrls = CN_BASE_URL_PRESETS[platform].filter(preset => preset.mode === 'coding').map(preset => preset.url)
      expect(codingUrls.length).toBeGreaterThan(0)
      for (const url of codingUrls) expect(paygUrls).not.toContain(url)
      for (const url of paygUrls) expect(codingUrls).not.toContain(url)
    }
  })

  it('keeps mistral on the single pay-as-you-go site with no plan endpoint', () => {
    expect(CN_BASE_URL_PRESETS.mistral).toEqual([
      { mode: 'payg', protocol: 'chat_completions', label: 'Mistral', url: 'https://api.mistral.ai/v1' }
    ])
    expect(presetUrls('mistral', 'coding', 'chat_completions')).toEqual([])
    expect(showsCodingPlanMode('mistral')).toBe(false)
  })

  it('derives the default base URL from the mode, not from the pay-as-you-go site', () => {
    expect(defaultCNBaseUrl('hunyuan', 'coding', 'chat_completions')).toBe('https://api.lkeap.cloud.tencent.com/coding/v3')
    expect(defaultCNBaseUrl('hunyuan', 'coding', 'anthropic')).toBe('https://api.lkeap.cloud.tencent.com/coding/anthropic')
    expect(defaultCNBaseUrl('volcengine', 'coding', 'chat_completions')).toBe('https://ark.cn-beijing.volces.com/api/coding/v3')
    // OpenAI 协议族共用厂商公布的套餐根，responses 不会回落到按量站点。
    expect(defaultCNBaseUrl('volcengine', 'coding', 'responses')).toBe('https://ark.cn-beijing.volces.com/api/coding/v3')
    expect(defaultCNBaseUrl('volcengine', 'coding', 'anthropic')).toBe('https://ark.cn-beijing.volces.com/api/coding')
    expect(defaultCNBaseUrl('qianfan', 'coding', 'chat_completions')).toBe('https://qianfan.baidubce.com/v2/tokenplan/personal')
    expect(defaultCNBaseUrl('qianfan', 'coding', 'responses')).toBe('https://qianfan.baidubce.com/v2/tokenplan/personal')
    // 按量付费档逐条不变。
    expect(defaultCNBaseUrl('volcengine', 'payg', 'chat_completions')).toBe('https://ark.cn-beijing.volces.com/api/v3')
    expect(defaultCNBaseUrl('qianfan', 'payg', 'responses')).toBe('https://qianfan.baidubce.com/v2')
    expect(defaultCNBaseUrl('volcengine', 'payg', 'anthropic')).toBe('')
    expect(defaultCNBaseUrl('qianfan', 'payg', 'anthropic')).toBe('')
  })

  it('swaps the preset list with the selected mode and fills the plan site', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'volcengine')

    expect(baseUrlPresetTexts(wrapper)).toEqual(['Ark (ark.cn-beijing.volces.com/api/v3)'])

    await selectAccountMode(wrapper, 'coding')
    expect(baseUrlPresetTexts(wrapper)).toEqual(['Ark Coding Plan (ark.cn-beijing.volces.com/api/coding/v3)'])
    expect(baseUrlValue(wrapper)).toBe('https://ark.cn-beijing.volces.com/api/coding/v3')

    await selectAccountMode(wrapper, 'payg')
    expect(baseUrlPresetTexts(wrapper)).toEqual(['Ark (ark.cn-beijing.volces.com/api/v3)'])
    expect(baseUrlValue(wrapper)).toBe('https://ark.cn-beijing.volces.com/api/v3')

    wrapper.unmount()
  })

  it('offers the qianfan team plan site as a preset and keeps personal as the default', async () => {
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'qianfan')
    await selectAccountMode(wrapper, 'coding')

    const presets = wrapper.findAll('[data-testid="cn-base-url-preset"]')
    expect(presets.map(preset => preset.attributes('aria-pressed'))).toEqual(['true', 'false'])
    expect(presets[1]!.text()).toContain('admin.accounts.cnProviders.baseUrlPresets.qianfanTokenPlanEnterprise')
    expect(presets[1]!.text()).toContain('qianfan.baidubce.com/v2/tokenplan/team')

    await presets[1]!.trigger('click')
    expect(baseUrlValue(wrapper)).toBe('https://qianfan.baidubce.com/v2/tokenplan/team')

    await submitApiKey(wrapper, 'qianfan team')
    expect(createAccountMock.mock.calls[0]?.[0]?.credentials).toMatchObject({
      account_mode: 'coding',
      base_url: 'https://qianfan.baidubce.com/v2/tokenplan/team'
    })

    wrapper.unmount()
  })
})
