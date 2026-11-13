<template>
  <AppLayout>
    <section class="plugins-page space-y-4">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold tracking-tight">插件管理</h1>
          <p class="mt-1 text-sm text-[var(--muted-foreground)]">管理宿主动态传输插件（.s2plugin）与网关核心流量扩展引擎</p>
        </div>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || actionBusy" @click="loadPlugins">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          刷新列表
        </button>
      </header>

      <div class="plugin-card space-y-2 p-4 text-sm">
        <p>现有传输能力：OpenAI OAuth 出站传输；API Key 请求路径保持隔离，不受传输插件影响。停用后新请求恢复宿主原有路径。</p>
        <p class="text-[var(--muted-foreground)]">插件菜单开关仅控制入口显示，不停止运行中的插件。安装、启停、卸载与配置保存等敏感操作受宿主二次验证机制保护。</p>
      </div>

      <section
        class="plugin-card install-card p-5"
        :class="{ 'is-dragging': dragDepth > 0 }"
        :aria-busy="uploading"
        @dragenter.prevent="dragDepth++"
        @dragover.prevent
        @dragleave.prevent="dragDepth = Math.max(0, dragDepth - 1)"
        @drop.prevent="handleDrop"
      >
        <h2 class="font-semibold">安装插件</h2>
        <p class="mt-2 text-sm text-[var(--muted-foreground)]">选择或拖入单个 .s2plugin 文件（ZIP 包），宿主自动校验包清单、协议版本、兼容性范围与可信签名。</p>
        <input ref="fileInput" class="hidden" type="file" accept=".s2plugin" aria-label="选择 .s2plugin 文件" @change="handleFileSelected" />
        <button type="button" class="btn btn-primary btn-sm mt-3" :disabled="actionBusy" @click="fileInput?.click()">
          <Icon name="upload" size="sm" />
          {{ uploading ? '正在安装…' : '选择 .s2plugin 文件' }}
        </button>
        <span v-if="dragDepth > 0" class="ml-3 text-sm text-[var(--brand-text)]" role="status">松开以安装插件</span>
      </section>

      <section aria-labelledby="installed-plugins-title" :aria-busy="loading">
        <h2 id="installed-plugins-title" class="mb-3 font-semibold">已安装传输插件</h2>
        <p v-if="pluginLoadError" class="plugin-card p-4 text-sm text-[var(--bad-text)]" role="alert">{{ pluginLoadError }} 请刷新列表重试。</p>
        <p v-else-if="loading && !plugins.length" class="plugin-card p-8 text-center text-sm text-[var(--muted-foreground)]" role="status">正在加载插件…</p>
        <div v-else-if="!plugins.length" class="plugin-card p-8 text-center">
          <p class="font-semibold">{{ t('admin.plugins.empty') }}</p>
          <p class="mt-2 text-sm text-[var(--muted-foreground)]">选择或拖入本机的 .s2plugin 文件进行安装。</p>
        </div>
        <div v-if="plugins.length" class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <article v-for="plugin in plugins" :key="plugin.id" class="plugin-card flex flex-col gap-4 p-5">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="break-words font-semibold">{{ plugin.name }}</h3>
                  <span class="plugin-status" :class="stateClass(plugin)">
                    <span aria-hidden="true">●</span>{{ pluginState(plugin) }}
                  </span>
                </div>
                <p class="mt-1 break-all font-mono text-xs text-[var(--muted-foreground)]">{{ plugin.plugin_key }} · 版本 {{ plugin.version }} · {{ plugin.author || '未知作者' }}</p>
                <p v-if="plugin.description" class="mt-2 break-words text-sm text-[var(--muted-foreground)]">{{ plugin.description }}</p>
              </div>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="actionBusy" @click="openConfiguration(plugin)">{{ t('admin.plugins.configure') }}</button>
            </div>
            <div class="grid gap-3 text-sm sm:grid-cols-2">
              <div>
                <h4 class="font-semibold">版本兼容性</h4>
                <p class="mt-1" :class="!plugin.compatibility.compatible ? 'text-[var(--bad-text)]' : !plugin.compatibility.tested ? 'text-[var(--warn-text)]' : 'text-[var(--ok-text)]'">{{ t(`admin.plugins.${plugin.compatibility.status}`) }}</p>
                <p class="mt-1 break-words text-xs text-[var(--muted-foreground)]">宿主 {{ plugin.compatibility.current_sub2api_version }} · 要求 {{ plugin.compatibility.required_sub2api_version }} · 建议 {{ plugin.compatibility.recommended_sub2api_version || '未指定' }}</p>
              </div>
              <div>
                <h4 class="font-semibold">运行状态</h4>
                <p class="mt-1">{{ plugin.runtime_healthy ? '运行正常' : '未运行' }} · 包签名：{{ plugin.signature_status === 'trusted' ? '已受信任' : '未签名' }}</p>
                <p v-if="plugin.last_error" class="mt-1 break-words text-xs text-[var(--bad-text)]">{{ plugin.last_error }}</p>
                <p v-else-if="pluginTests[plugin.id]" class="mt-1 break-words text-xs" :class="pluginTests[plugin.id].success ? 'text-[var(--ok-text)]' : 'text-[var(--bad-text)]'" role="status">{{ pluginTests[plugin.id].success ? '探活通过' : '探活失败' }} · {{ pluginTests[plugin.id].latency_ms }} ms · {{ pluginTests[plugin.id].message }}</p>
                <p v-else class="mt-1 break-words text-xs text-[var(--muted-foreground)]">{{ plugin.runtime_message || '尚未测试' }}</p>
              </div>
            </div>
            <div class="mt-auto">
              <label :for="`plugin-rollout-${plugin.id}`" class="block text-sm">OAuth 流量比例 <b class="font-mono tabular-nums">{{ rolloutValues[plugin.id] ?? currentRollout(plugin) }}%</b><span v-if="hasEnabledBinding(plugin)" class="text-[var(--muted-foreground)]"> · 停用后可调整</span></label>
              <input :id="`plugin-rollout-${plugin.id}`" :value="rolloutValues[plugin.id] ?? currentRollout(plugin)" :style="{ '--rollout': `${rolloutValues[plugin.id] ?? currentRollout(plugin)}%` }" class="plugin-range mt-3" type="range" min="1" max="100" step="1" :disabled="hasEnabledBinding(plugin) || actionBusy" @input="setRollout(plugin.id, $event)" />
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="testPlugin(plugin)">{{ t('admin.plugins.test') }}</button>
              <button v-if="hasEnabledBinding(plugin)" type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="disablePlugin(plugin)">{{ t('admin.plugins.disable') }}</button>
              <button v-else type="button" class="btn btn-primary btn-sm" :disabled="actionBusy || plugin.state === 'starting' || !plugin.compatibility.compatible" @click="enablePlugin(plugin)">{{ t('admin.plugins.enable') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy || hasEnabledBinding(plugin) || plugin.state === 'starting'" :title="hasEnabledBinding(plugin) ? '请先停用插件，再执行卸载' : '卸载插件及其配置'" @click="uninstallPlugin(plugin)">{{ t('admin.plugins.uninstall') }}</button>
              <span v-if="busyID === plugin.id" class="self-center text-xs text-[var(--muted-foreground)]" role="status">正在处理…</span>
            </div>
          </article>
        </div>
      </section>

      <section class="plugin-card p-4" aria-labelledby="gateway-extensions-title">
        <h2 id="gateway-extensions-title" class="font-semibold">网关内置扩展引擎 · {{ extensions.length || 4 }} 项</h2>
        <p class="mt-2 text-sm text-[var(--muted-foreground)]">独立管理网关流量策略、响应缓存与成本预算。</p>
        <p v-if="hasLocalExtensions" class="mt-2 text-sm text-[var(--warn-text)]" role="status">网关扩展接口暂未接通，当前配置仅保存在本次页面会话内；接通接口后由网关执行，自检仅验证本地配置。</p>
        <p v-if="extensionLoadError" class="mt-3 text-sm text-[var(--bad-text)]" role="alert">{{ extensionLoadError }} 请刷新列表重试。</p>
        <p v-else-if="loading && !extensions.length" class="py-6 text-sm text-[var(--muted-foreground)]" role="status">正在加载扩展引擎…</p>
        <p v-else-if="!extensions.length" class="py-6 text-sm text-[var(--muted-foreground)]">网关尚未提供可用扩展。</p>
        <div class="mt-3 grid grid-cols-1 gap-4 xl:grid-cols-2">
          <article v-for="extension in extensions" :key="extension.key" class="plugin-card flex flex-col gap-4 p-5">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="font-semibold">{{ extension.name }}</h3>
                  <span class="plugin-status" :class="extension.enabled ? isLocalExtension(extension.key) ? 'status-warn' : extension.runtime_healthy ? 'status-ok' : 'status-bad' : 'status-neutral'"><span aria-hidden="true">●</span>{{ extension.enabled ? isLocalExtension(extension.key) ? '本地已启用' : extension.runtime_healthy ? '运行正常' : '运行异常' : '已停用' }}</span>
                </div>
                <p class="mt-1 break-all font-mono text-xs text-[var(--muted-foreground)]">{{ extension.key }} · 版本 {{ extension.version }} · {{ extension.author }}</p>
                <p class="mt-2 text-sm text-[var(--muted-foreground)]">{{ extension.description }}</p>
              </div>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="actionBusy" @click="openExtensionConfiguration(extension)">配置</button>
            </div>
            <div class="grid gap-3 text-sm sm:grid-cols-2">
              <div><h4 class="font-semibold">扩展能力</h4><p class="mt-1">{{ extension.category }}</p></div>
              <div>
                <h4 class="font-semibold">运行状态</h4>
                <p class="mt-1">{{ isLocalExtension(extension.key) ? '等待连接网关' : extension.runtime_healthy ? '运行正常' : extension.enabled ? '运行异常' : '未运行' }}</p>
                <p v-if="extension.last_test" class="mt-1 break-words text-xs" :class="extension.last_test.success ? 'text-[var(--ok-text)]' : 'text-[var(--bad-text)]'" role="status">{{ extension.last_test.message }} · {{ extension.last_test.latency_ms }} ms</p>
                <p v-else class="mt-1 text-xs text-[var(--muted-foreground)]">尚未测试</p>
              </div>
            </div>
            <div class="mt-auto">
              <label :for="`extension-rollout-${extension.key}`" class="block text-sm">流量覆盖比例 <b class="font-mono tabular-nums">{{ extensionRollouts[extension.key] ?? extension.rollout_percent }}%</b></label>
              <input :id="`extension-rollout-${extension.key}`" :value="extensionRollouts[extension.key] ?? extension.rollout_percent" :style="{ '--rollout': `${extensionRollouts[extension.key] ?? extension.rollout_percent}%` }" class="plugin-range mt-3" type="range" min="1" max="100" step="1" :disabled="actionBusy" @input="extensionRollouts[extension.key] = rangeValue($event)" @change="saveExtensionRollout(extension)" />
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="testGatewayExtension(extension)">{{ isLocalExtension(extension.key) ? '配置自检' : '测试' }}</button>
              <button type="button" class="btn btn-sm" :class="extension.enabled ? 'btn-secondary' : 'btn-primary'" :disabled="actionBusy" @click="toggleExtension(extension)">{{ extension.enabled ? '停用' : '启用' }}</button>
              <span v-if="busyExtension === extension.key" class="self-center text-xs text-[var(--muted-foreground)]" role="status">正在处理…</span>
            </div>
          </article>
        </div>
      </section>

      <BaseDialog :show="configPlugin !== null || configExtension !== null" :title="`${configPlugin?.name || configExtension?.name || ''} · 配置`" :width="configMode === 'iframe' ? 'full' : 'wide'" :close-on-escape="!configSaving" :show-close-button="!configSaving" @close="closeConfiguration">
        <div v-if="configMode === 'iframe'" class="space-y-3">
          <div class="flex justify-end"><button type="button" class="btn btn-secondary btn-sm" :disabled="configSaving" @click="openJsonConfiguration">切换 JSON 编辑器</button></div>
          <div class="relative min-h-[520px] overflow-hidden bg-[var(--surface-2)]" :style="{ height: `${iframeHeight}px` }">
            <p v-if="uiLoading" class="absolute inset-0 flex items-center justify-center text-sm text-[var(--muted-foreground)]" role="status">{{ t('admin.plugins.loadingUI') }}</p>
            <iframe v-if="uiSession" ref="pluginFrame" :src="uiSession.url" sandbox="allow-scripts" referrerpolicy="no-referrer" class="h-full w-full border-0 bg-[var(--card)]" :title="`${configPlugin?.name || ''} 配置`" @load="handlePluginFrameLoad" @error="openJsonConfiguration" />
          </div>
        </div>
        <div v-else class="space-y-3">
          <p class="text-sm text-[var(--muted-foreground)]">编辑完整 JSON 对象并保存。参数由对应插件或网关扩展校验。</p>
          <p v-if="configExtension && isLocalExtension(configExtension.key)" class="text-sm text-[var(--warn-text)]">当前为会话内配置，尚未写入网关。</p>
          <p v-if="jsonLoading" class="text-sm" role="status">正在读取配置…</p>
          <label for="plugin-config-json" class="block text-sm font-medium">配置 JSON 对象</label>
          <textarea id="plugin-config-json" v-model="jsonDraft" class="input min-h-64 w-full font-mono text-sm" rows="14" spellcheck="false" :disabled="jsonLoading || configSaving || !jsonReady" :aria-invalid="!!jsonError" :aria-describedby="jsonError ? 'plugin-config-error' : undefined" />
          <p v-if="jsonError" id="plugin-config-error" class="break-words text-sm text-[var(--bad-text)]" role="alert">{{ jsonError }}</p>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-primary btn-sm" :disabled="jsonLoading || configSaving || !jsonReady" @click="saveJsonConfiguration">{{ configSaving ? '正在保存…' : '保存配置' }}</button>
            <button v-if="!jsonReady && !jsonLoading" type="button" class="btn btn-secondary btn-sm" @click="openJsonConfiguration">重新读取</button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="configSaving" @click="closeConfiguration">关闭</button>
          </div>
        </div>
      </BaseDialog>
      <TotpStepUpDialog :controller="pluginStepUp" />
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI, type PluginInstallation, type PluginUISession } from '@/api/admin'
import type { GatewayExtensionModule, GatewayExtensionUpdate, PluginTestResult } from '@/api/admin/plugins'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'

interface PluginBridgeMessage {
  source?: string
  bridge_token?: string
  type?: string
  request_id?: string
  config?: unknown
  height?: unknown
  level?: unknown
  message?: unknown
}

const { t } = useI18n()
const appStore = useAppStore()
const pluginStepUp = useStepUp()
const plugins = ref<PluginInstallation[]>([])
const extensions = ref<GatewayExtensionModule[]>([])
const loading = ref(false)
const uploading = ref(false)
const busyID = ref<number | null>(null)
const busyExtension = ref<string | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const dragDepth = ref(0)
const rolloutValues = ref<Record<number, number>>({})
const extensionRollouts = ref<Record<string, number>>({})
const pluginTests = ref<Record<number, PluginTestResult>>({})
const pluginLoadError = ref('')
const extensionLoadError = ref('')
const configPlugin = ref<PluginInstallation | null>(null)
const configExtension = ref<GatewayExtensionModule | null>(null)
const configMode = ref<'iframe' | 'json'>('json')
const jsonDraft = ref('')
const jsonOriginal = ref('')
const jsonError = ref('')
const jsonLoading = ref(false)
const jsonReady = ref(false)
const configSaving = ref(false)
const uiSession = ref<PluginUISession | null>(null)
const pluginFrame = ref<HTMLIFrameElement | null>(null)
const uiLoading = ref(false)
const iframeHeight = ref(640)
const pluginFrameLoaded = ref(false)
const pendingBridgeRequests = new Map<string, number>()
const localKeys = ref<string[]>([])
const hasLocalExtensions = computed(() => localKeys.value.length > 0)
const actionBusy = computed(() => loading.value || uploading.value || busyID.value !== null || busyExtension.value !== null || configSaving.value)
let configGeneration = 0
let frameTimeout: number | undefined
let disposed = false

function errorMessage(error: unknown): string {
  return extractApiErrorMessage(error, t('common.unknownError'), {
    encryption_key_required: t('common.encryptionKeyRequired')
  })
}

function reportSensitiveActionError(error: unknown): void {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    appStore.showError(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? t('stepUp.adminApiKeyForbidden') : t('stepUp.notEnabled'))
    return
  }
  appStore.showError(errorMessage(error))
}

function syncLocalKeys(): void {
  localKeys.value = extensions.value.filter(extension => adminAPI.plugins.isExtensionLocal(extension.key)).map(extension => extension.key)
}

function isLocalExtension(key: string): boolean {
  return localKeys.value.includes(key)
}

async function loadPlugins(): Promise<void> {
  if (loading.value || disposed) return
  loading.value = true
  const [pluginResult, extensionResult] = await Promise.allSettled([
    Promise.resolve().then(() => adminAPI.plugins.list()),
    Promise.resolve().then(() => adminAPI.plugins.listExtensions())
  ])
  if (disposed) return
  pluginLoadError.value = ''
  extensionLoadError.value = ''
  if (pluginResult.status === 'fulfilled') {
    plugins.value = pluginResult.value
    for (const plugin of plugins.value) rolloutValues.value[plugin.id] = currentRollout(plugin)
  } else {
    pluginLoadError.value = errorMessage(pluginResult.reason)
  }
  if (extensionResult.status === 'fulfilled') {
    extensions.value = extensionResult.value
    for (const extension of extensions.value) extensionRollouts.value[extension.key] = extension.rollout_percent
    syncLocalKeys()
  } else {
    extensionLoadError.value = errorMessage(extensionResult.reason)
  }
  loading.value = false
}

async function installFiles(files: FileList | File[] | null | undefined): Promise<void> {
  if (actionBusy.value || !files?.length) return
  if (files.length !== 1) {
    appStore.showError('一次只能安装一个 .s2plugin 文件。')
    return
  }
  const file = files[0]
  if (!file.name.toLowerCase().endsWith('.s2plugin') || file.name.length <= 9 || file.size <= 0) {
    appStore.showError('请选择非空的 .s2plugin 文件。')
    return
  }
  uploading.value = true
  try {
    await pluginStepUp.run(() => adminAPI.plugins.upload(file))
    appStore.showSuccess(t('admin.plugins.uploadSuccess'))
    await loadPlugins()
  } catch (error: unknown) {
    reportSensitiveActionError(error)
  } finally {
    uploading.value = false
  }
}

async function handleFileSelected(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement
  const files = Array.from(target.files || [])
  target.value = ''
  await installFiles(files)
}

async function handleDrop(event: DragEvent): Promise<void> {
  dragDepth.value = 0
  await installFiles(event.dataTransfer?.files)
}

function currentRollout(plugin: PluginInstallation): number {
  return (plugin.bindings.find(binding => binding.capability === 'openai.oauth.outbound_transport.v1') ?? plugin.bindings[0])?.rollout_percent || 100
}

function hasEnabledBinding(plugin: PluginInstallation): boolean {
  return plugin.state === 'enabled' || plugin.bindings.some(binding => binding.enabled)
}

function rangeValue(event: Event): number {
  return Math.min(100, Math.max(1, Math.round(Number((event.target as HTMLInputElement).value) || 1)))
}

function setRollout(id: number, event: Event): void {
  rolloutValues.value[id] = rangeValue(event)
}

async function runPluginAction(plugin: PluginInstallation, action: () => Promise<unknown>, message: string): Promise<void> {
  if (actionBusy.value) return
  busyID.value = plugin.id
  try {
    await pluginStepUp.run(action)
    appStore.showSuccess(message)
    await loadPlugins()
  } catch (error: unknown) {
    reportSensitiveActionError(error)
  } finally {
    busyID.value = null
  }
}

async function enablePlugin(plugin: PluginInstallation): Promise<void> {
  if (actionBusy.value || !plugin.compatibility.compatible || plugin.state === 'starting') return
  const acceptUntested = !plugin.compatibility.tested
  const risks = [
    acceptUntested ? '该插件尚未通过当前宿主版本测试。' : '',
    plugin.signature_status !== 'trusted' ? '该插件未签名，无法验证发布者身份与包完整性。' : ''
  ].filter(Boolean)
  if (risks.length && !window.confirm(`${risks.join('\n')}\n确认承担风险并启用 ${plugin.name}？`)) return
  await runPluginAction(plugin, () => adminAPI.plugins.enable(plugin.id, rolloutValues.value[plugin.id] || 100, acceptUntested), t('admin.plugins.enableSuccess'))
}

async function disablePlugin(plugin: PluginInstallation): Promise<void> {
  if (actionBusy.value || !window.confirm(t('admin.plugins.confirmDisable'))) return
  await runPluginAction(plugin, () => adminAPI.plugins.disable(plugin.id), t('admin.plugins.disableSuccess'))
}

async function uninstallPlugin(plugin: PluginInstallation): Promise<void> {
  if (actionBusy.value || hasEnabledBinding(plugin) || plugin.state === 'starting' || !window.confirm(t('admin.plugins.confirmUninstall'))) return
  await runPluginAction(plugin, () => adminAPI.plugins.remove(plugin.id), t('admin.plugins.uninstallSuccess'))
}

async function testPlugin(plugin: PluginInstallation): Promise<void> {
  if (actionBusy.value) return
  busyID.value = plugin.id
  try {
    const result = await pluginStepUp.run(() => adminAPI.plugins.test(plugin.id))
    pluginTests.value[plugin.id] = result
    if (result.success) appStore.showSuccess(result.message || t('admin.plugins.testSuccess'))
    else appStore.showError(result.message || t('common.error'))
  } catch (error: unknown) {
    reportSensitiveActionError(error)
  } finally {
    busyID.value = null
  }
}

function replaceExtension(extension: GatewayExtensionModule): void {
  extensions.value = extensions.value.map(current => current.key === extension.key ? extension : current)
  extensionRollouts.value[extension.key] = extension.rollout_percent
  syncLocalKeys()
}

async function changeExtension(extension: GatewayExtensionModule, payload: GatewayExtensionUpdate): Promise<void> {
  if (actionBusy.value) return
  busyExtension.value = extension.key
  try {
    const updated = await pluginStepUp.run(() => adminAPI.plugins.updateExtension(extension.key, payload))
    replaceExtension(updated)
    appStore.showSuccess(isLocalExtension(extension.key) ? '本次会话内的扩展配置已更新' : '扩展配置已更新')
  } catch (error: unknown) {
    extensionRollouts.value[extension.key] = extension.rollout_percent
    reportSensitiveActionError(error)
  } finally {
    busyExtension.value = null
  }
}

async function toggleExtension(extension: GatewayExtensionModule): Promise<void> {
  await changeExtension(extension, { enabled: !extension.enabled })
}

async function saveExtensionRollout(extension: GatewayExtensionModule): Promise<void> {
  const rollout = extensionRollouts.value[extension.key] ?? extension.rollout_percent
  if (rollout !== extension.rollout_percent) await changeExtension(extension, { rollout_percent: rollout })
}

async function testGatewayExtension(extension: GatewayExtensionModule): Promise<void> {
  if (actionBusy.value) return
  busyExtension.value = extension.key
  try {
    const result = await pluginStepUp.run(() => adminAPI.plugins.testExtension(extension.key))
    syncLocalKeys()
    replaceExtension({ ...extension, last_test: result, runtime_healthy: !isLocalExtension(extension.key) && extension.enabled && result.success })
    if (result.success) appStore.showSuccess(result.message)
    else appStore.showError(result.message)
  } catch (error: unknown) {
    reportSensitiveActionError(error)
  } finally {
    busyExtension.value = null
  }
}

function resetConfiguration(): void {
  configGeneration++
  window.clearTimeout(frameTimeout)
  clearPendingBridgeRequests()
  pluginFrameLoaded.value = false
  configPlugin.value = null
  configExtension.value = null
  uiSession.value = null
  uiLoading.value = false
  jsonDraft.value = ''
  jsonOriginal.value = ''
  jsonError.value = ''
  jsonReady.value = false
  jsonLoading.value = false
}

function canCloseConfiguration(): boolean {
  return !configSaving.value && (jsonDraft.value === jsonOriginal.value || window.confirm('配置尚未保存，确认放弃修改？'))
}

async function openConfiguration(plugin: PluginInstallation): Promise<void> {
  if (actionBusy.value || !canCloseConfiguration()) return
  resetConfiguration()
  configPlugin.value = plugin
  if (!plugin.manifest?.ui?.entrypoint) {
    await openJsonConfiguration()
    return
  }
  configMode.value = 'iframe'
  uiLoading.value = true
  iframeHeight.value = 640
  const generation = configGeneration
  frameTimeout = window.setTimeout(() => {
    if (generation === configGeneration && uiLoading.value) void openJsonConfiguration()
  }, 12_000)
  try {
    const session = await adminAPI.plugins.createUISession(plugin.id)
    if (generation !== configGeneration) return
    if (!session.url || !session.bridge_token) throw new Error('插件配置界面不可用')
    const url = new URL(session.url, window.location.href)
    if (!['http:', 'https:'].includes(url.protocol) || url.origin !== window.location.origin) throw new Error('插件配置地址不受信任')
    uiSession.value = session
  } catch {
    if (generation === configGeneration) await openJsonConfiguration()
  }
}

function openExtensionConfiguration(extension: GatewayExtensionModule): void {
  if (actionBusy.value || !canCloseConfiguration()) return
  resetConfiguration()
  configExtension.value = extension
  configMode.value = 'json'
  jsonDraft.value = JSON.stringify(extension.config, null, 2)
  jsonOriginal.value = jsonDraft.value
  jsonReady.value = true
}

async function openJsonConfiguration(): Promise<void> {
  if (configSaving.value || !configPlugin.value) return
  const pluginID = configPlugin.value.id
  const generation = ++configGeneration
  window.clearTimeout(frameTimeout)
  clearPendingBridgeRequests()
  uiSession.value = null
  uiLoading.value = false
  configMode.value = 'json'
  jsonLoading.value = true
  jsonReady.value = false
  jsonError.value = ''
  try {
    const config = await adminAPI.plugins.getJsonConfig(pluginID)
    if (generation !== configGeneration) return
    jsonDraft.value = JSON.stringify(config, null, 2)
    jsonOriginal.value = jsonDraft.value
    jsonReady.value = true
  } catch (error: unknown) {
    if (generation === configGeneration) jsonError.value = errorMessage(error)
  } finally {
    if (generation === configGeneration) jsonLoading.value = false
  }
}

async function saveJsonConfiguration(): Promise<void> {
  if (!jsonReady.value || configSaving.value || jsonLoading.value) return
  jsonError.value = ''
  let config: Record<string, unknown>
  try {
    const parsed: unknown = JSON.parse(jsonDraft.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('配置必须是 JSON 对象。')
    config = parsed as Record<string, unknown>
  } catch (error: unknown) {
    jsonError.value = error instanceof SyntaxError ? 'JSON 格式有误，请检查引号、逗号与括号。' : errorMessage(error)
    return
  }
  const plugin = configPlugin.value
  const extension = configExtension.value
  if (!plugin && !extension) return
  const generation = configGeneration
  configSaving.value = true
  try {
    let saved: Record<string, unknown>
    if (extension) {
      const updated = await pluginStepUp.run(() => adminAPI.plugins.updateExtension(extension.key, { config }))
      replaceExtension(updated)
      configExtension.value = updated
      saved = updated.config
    } else {
      saved = await pluginStepUp.run(() => adminAPI.plugins.saveJsonConfig(plugin!.id, config))
    }
    if (generation !== configGeneration) return
    jsonDraft.value = JSON.stringify(saved, null, 2)
    jsonOriginal.value = jsonDraft.value
    appStore.showSuccess(extension && isLocalExtension(extension.key) ? '配置已保存至本次会话' : '配置已保存')
  } catch (error: unknown) {
    if (generation === configGeneration && !isStepUpCancelled(error)) jsonError.value = errorMessage(error)
    reportSensitiveActionError(error)
  } finally {
    configSaving.value = false
  }
}

function closeConfiguration(): void {
  if (canCloseConfiguration()) resetConfiguration()
}

function clearPendingBridgeRequests(): void {
  for (const timeout of pendingBridgeRequests.values()) window.clearTimeout(timeout)
  pendingBridgeRequests.clear()
}

function handlePluginFrameLoad(): void {
  if (pluginFrameLoaded.value) {
    configGeneration++
    clearPendingBridgeRequests()
    window.clearTimeout(frameTimeout)
    uiLoading.value = true
    const generation = configGeneration
    frameTimeout = window.setTimeout(() => {
      if (generation === configGeneration && uiLoading.value) void openJsonConfiguration()
    }, 12_000)
  }
  pluginFrameLoaded.value = true
}

function postBridgeResult(request: PluginBridgeMessage, payload: Record<string, unknown>, generation: number): void {
  if (generation !== configGeneration || !pluginFrame.value?.contentWindow || !uiSession.value) return
  const requestID = typeof request.request_id === 'string' ? request.request_id.trim() : ''
  const timeout = pendingBridgeRequests.get(requestID)
  if (!requestID || timeout === undefined) return
  window.clearTimeout(timeout)
  pendingBridgeRequests.delete(requestID)
  pluginFrame.value.contentWindow.postMessage({
    source: 'sub2api-plugin-host',
    bridge_token: uiSession.value.bridge_token,
    type: `${request.type}.result`,
    request_id: requestID,
    ...payload
  }, '*')
}

async function handleBridgeMessage(event: MessageEvent): Promise<void> {
  if (!uiSession.value || !configPlugin.value || event.source !== pluginFrame.value?.contentWindow || event.origin !== 'null') return
  const message = event.data as PluginBridgeMessage
  if (!message || message.source !== 'sub2api-plugin-ui' || message.bridge_token !== uiSession.value.bridge_token) return
  const generation = configGeneration
  const pluginID = configPlugin.value.id
  const requestID = typeof message.request_id === 'string' ? message.request_id.trim() : ''
  const expectsResponse = ['config.load', 'config.save', 'config.test'].includes(message.type || '')
  if (expectsResponse) {
    if (!requestID || pendingBridgeRequests.has(requestID)) return
    pendingBridgeRequests.set(requestID, window.setTimeout(() => pendingBridgeRequests.delete(requestID), 30_000))
  }
  try {
    switch (message.type) {
      case 'sub2api.plugin.ready':
        uiLoading.value = false
        window.clearTimeout(frameTimeout)
        break
      case 'config.load': {
        const config = await adminAPI.plugins.getConfig(pluginID)
        postBridgeResult(message, { ok: true, config }, generation)
        break
      }
      case 'config.save': {
        if (!message.config || typeof message.config !== 'object' || Array.isArray(message.config)) throw new Error(t('admin.plugins.bridgeRejected'))
        if (actionBusy.value) throw new Error('正在处理其他操作，请稍后重试。')
        configSaving.value = true
        try {
          const config = await pluginStepUp.run(() => adminAPI.plugins.saveConfig(pluginID, message.config as Record<string, unknown>))
          postBridgeResult(message, { ok: true, config }, generation)
          appStore.showSuccess(t('common.saved'))
        } finally {
          configSaving.value = false
        }
        break
      }
      case 'config.test': {
        if (actionBusy.value) throw new Error('正在处理其他操作，请稍后重试。')
        busyID.value = pluginID
        try {
          const result = await pluginStepUp.run(() => adminAPI.plugins.test(pluginID))
          pluginTests.value[pluginID] = result
          postBridgeResult(message, { ok: result.success, result }, generation)
          if (result.success) appStore.showSuccess(result.message || t('admin.plugins.testSuccess'))
          else appStore.showError(result.message || t('common.error'))
        } finally {
          busyID.value = null
        }
        break
      }
      case 'ui.resize': {
        const height = Number(message.height)
        if (Number.isFinite(height)) iframeHeight.value = Math.min(960, Math.max(520, Math.round(height)))
        break
      }
      case 'ui.notify': {
        const text = typeof message.message === 'string' ? message.message.slice(0, 500) : ''
        if (!text) break
        if (message.level === 'error') appStore.showError(text)
        else if (message.level === 'success') appStore.showSuccess(text)
        else appStore.showInfo(text)
        break
      }
    }
  } catch (error: unknown) {
    if (isStepUpBlocked(error)) reportSensitiveActionError(error)
    else if (message.type === 'config.save' && !isStepUpCancelled(error)) appStore.showError(errorMessage(error))
    postBridgeResult(message, { ok: false, error: isStepUpCancelled(error) ? t('common.cancel') : errorMessage(error) }, generation)
  }
}

function pluginState(plugin: PluginInstallation): string {
  if (!plugin.compatibility.compatible || plugin.state === 'incompatible') return '不兼容'
  if (plugin.state === 'error' || plugin.last_error) return '异常'
  if (plugin.state === 'starting') return '启动中'
  return hasEnabledBinding(plugin) ? '已启用' : '已停用'
}

function stateClass(plugin: PluginInstallation): string {
  const state = pluginState(plugin)
  if (state === '不兼容' || state === '异常') return 'status-bad'
  if (state === '已启用') return 'status-ok'
  return state === '启动中' ? 'status-warn' : 'status-neutral'
}

onMounted(() => {
  window.addEventListener('message', handleBridgeMessage)
  void loadPlugins()
})

onBeforeUnmount(() => {
  disposed = true
  window.removeEventListener('message', handleBridgeMessage)
  resetConfiguration()
})
</script>

<style scoped>
.plugins-page { color: var(--foreground); }
.plugin-card { border: 1px solid var(--border); border-radius: var(--radius-md, 12px); background: var(--card); }
.install-card { border: 2px dashed var(--border); transition: border-color 150ms, background-color 150ms; }
.install-card.is-dragging { border-color: var(--brand); background: color-mix(in srgb, var(--brand) 8%, var(--card)); }
.plugin-status { display: inline-flex; align-items: center; gap: 6px; padding: 3px 9px; border-radius: 999px; font-size: 11px; line-height: 1.4; white-space: nowrap; }
.status-ok { color: var(--ok-text); background: color-mix(in srgb, var(--ok) 12%, var(--card)); }
.status-bad { color: var(--bad-text); background: color-mix(in srgb, var(--bad) 12%, var(--card)); }
.status-warn { color: var(--warn-text); background: color-mix(in srgb, var(--warn) 12%, var(--card)); }
.status-neutral { color: var(--muted-foreground); background: var(--surface-2); }
.plugin-range { display: block; width: 100%; height: 16px; appearance: none; background: transparent; accent-color: var(--brand); cursor: pointer; }
.plugin-range::-webkit-slider-runnable-track { height: 6px; border-radius: 999px; background: linear-gradient(to right, var(--brand) var(--rollout), var(--surface-2) var(--rollout)); }
.plugin-range::-moz-range-track { height: 6px; border-radius: 999px; background: linear-gradient(to right, var(--brand) var(--rollout), var(--surface-2) var(--rollout)); }
.plugin-range::-webkit-slider-thumb { width: 16px; height: 16px; margin-top: -5px; appearance: none; border: 2px solid var(--card); border-radius: 50%; background: var(--brand); box-shadow: 0 0 0 1px var(--border); }
.plugin-range::-moz-range-thumb { width: 12px; height: 12px; border: 2px solid var(--card); border-radius: 50%; background: var(--brand); box-shadow: 0 0 0 1px var(--border); }
.plugin-range:disabled { cursor: not-allowed; opacity: .5; }
.plugins-page .btn-primary { background: #16161a; border-color: #16161a; color: #fff; }
.plugins-page .btn-secondary { border: 1px solid var(--border); background: var(--card); color: var(--foreground); }
.plugins-page .btn:disabled { cursor: not-allowed; opacity: .5; }
.plugin-range:focus-visible { outline: 2px solid var(--brand); outline-offset: 4px; border-radius: 4px; }
@media (prefers-reduced-motion: reduce) { .install-card { transition: none; } }
</style>
