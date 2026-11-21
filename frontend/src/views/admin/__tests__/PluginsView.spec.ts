import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'

const {
  showError,
  showSuccess,
  listPlugins,
  listExtensions,
  uploadPlugin,
  enablePlugin,
  savePluginConfig,
  createUISession,
  stepUpRun,
} = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn(),
  listPlugins: vi.fn(),
  listExtensions: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  savePluginConfig: vi.fn(),
  createUISession: vi.fn(),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      listExtensions,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: vi.fn().mockResolvedValue({}),
      saveConfig: savePluginConfig,
      test: vi.fn().mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 }),
      createUISession,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      rollout_percent: 100,
    },
  ],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Icon: true,
        CopyButton: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    listPlugins.mockResolvedValue([plugin])
    listExtensions.mockResolvedValue([])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })

  it('shows persistent key failure and rejects the plugin bridge save without success', async () => {
    savePluginConfig.mockRejectedValue({ reason: 'encryption_key_required', message: 'server fallback' })
    const wrapper = mountView()
    document.body.appendChild(wrapper.element)
    await flushPromises()
    await wrapper.findAll('button').find(item => item.text().includes('admin.plugins.configure'))!.trigger('click')
    await flushPromises()
    const frame = wrapper.get('iframe').element as HTMLIFrameElement
    const postMessage = vi.spyOn(frame.contentWindow!, 'postMessage')
    window.dispatchEvent(new MessageEvent('message', {
      origin: 'null', source: frame.contentWindow,
      data: { source: 'sub2api-plugin-ui', bridge_token: 'bridge', type: 'config.save', request_id: 'save-test', config: {} },
    }))
    await flushPromises()
    expect(savePluginConfig).toHaveBeenCalledWith(7, {})
    expect(showError).toHaveBeenCalledWith('common.encryptionKeyRequired')
    expect(showSuccess).not.toHaveBeenCalled()
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({ ok: false, error: 'common.encryptionKeyRequired' }), '*')
    wrapper.unmount()
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, 100, false)
  })

  it.each(['.s2plugin', '.OHPLUGIN'])('上传 %s 插件通过 step-up 控制器执行', async (extension) => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], `transport${extension}`, { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })

  it('扩展能力清单为空时展示真实空状态，不回退硬编码假模块或本地已启用', async () => {
    listExtensions.mockResolvedValue([])
    const wrapper = mountView()
    await flushPromises()

    expect(listExtensions).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="extensions-empty-state"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('prompt-guard')
    expect(wrapper.text()).not.toContain('本地已启用')
  })

  it('扩展能力清单返回真实签名能力包时准确渲染 ID、SDK 版本与授权动作', async () => {
    listExtensions.mockResolvedValue([
      {
        id: 'omnihive.responses.adapter',
        sdk_version: 2,
        publisher_id: 'omnihive-core',
        state: 'enabled',
        digest: 'f'.repeat(64),
        authorized_actions: ['provider.responses.v1'],
        capabilities: [
          {
            id: 'provider.responses.v1',
            platform: 'openai',
            account_type: 'oauth',
          },
        ],
      },
    ])
    const wrapper = mountView()
    await flushPromises()

    const card = wrapper.get('[data-testid="extension-inventory-card"]')
    expect(card.text()).toContain('omnihive.responses.adapter')
    expect(card.text()).toContain('SDK v2')
    expect(card.text()).toContain('provider.responses.v1')
    expect(card.text()).toContain('omnihive-core')
  })

  it('扩展能力清单接口报错时展示真实错误提示，不静默伪造模块', async () => {
    listExtensions.mockRejectedValue(new Error('extension inventory unreachable'))
    const wrapper = mountView()
    await flushPromises()

    const alert = wrapper.get('[data-testid="extensions-error-state"]')
    expect(alert.text()).toContain('extension inventory unreachable')
    expect(wrapper.findAll('[data-testid="extension-inventory-card"]')).toHaveLength(0)
  })
})
