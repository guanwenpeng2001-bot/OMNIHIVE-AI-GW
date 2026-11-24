<template>
  <AppLayout>
    <section class="plugins-page space-y-4">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold tracking-tight">{{ t('adminPlatform.plugins.pluginsExtensionInventory') }}</h1>
          <p class="mt-1 text-sm text-[var(--muted-foreground)]">
            {{
              t('adminPlatform.plugins.manageHostTransportPluginsOhpluginS2pluginAnd')
            }}
          </p>
        </div>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || actionBusy" @click="loadPlugins">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('adminPlatform.plugins.refresh') }}
        </button>
      </header>

      <div class="plugin-card space-y-2 p-4 text-sm">
        <p>
          {{
            t('adminPlatform.plugins.activeHostCapabilitiesCoverOpenAIOAuthOutbound')
          }}
        </p>
        <p class="text-[var(--muted-foreground)]">
          {{
            t('adminPlatform.plugins.sensitiveLifecycleActionsIncludingInstallationActivationRemoval')
          }}
        </p>
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
        <h2 class="font-semibold">{{ t('adminPlatform.plugins.installPluginArchive') }}</h2>
        <p class="mt-2 text-sm text-[var(--muted-foreground)]">
          {{
            t('adminPlatform.plugins.selectOrDropASingleOhpluginOr')
          }}
        </p>
        <input
          ref="fileInput"
          class="hidden"
          type="file"
          accept=".ohplugin,.s2plugin"
          :aria-label="t('adminPlatform.plugins.selectOhpluginOrS2pluginArchive')"
          @change="handleFileSelected"
        />
        <button type="button" class="btn btn-primary btn-sm mt-3" :disabled="actionBusy" @click="fileInput?.click()">
          <Icon name="upload" size="sm" />
          {{
            uploading
              ? (t('adminPlatform.plugins.installing'))
              : (t('adminPlatform.plugins.selectOhpluginS2pluginFile'))
          }}
        </button>
        <span v-if="dragDepth > 0" class="ml-3 text-sm text-[var(--brand-text)]" role="status">
          {{ t('adminPlatform.plugins.releaseToInstallArchive') }}
        </span>
      </section>

      <section aria-labelledby="installed-plugins-title" :aria-busy="loading">
        <h2 id="installed-plugins-title" class="mb-3 font-semibold">
          {{ t('adminPlatform.plugins.installedTransportPlugins') }}
        </h2>
        <div v-if="pluginLoadError" class="plugin-card flex flex-wrap items-center justify-between gap-3 p-4 text-sm text-[var(--bad-text)]" role="alert">
          <span>{{ pluginLoadError }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadPlugins">
            {{ t('adminPlatform.plugins.retry') }}
          </button>
        </div>
        <p v-else-if="loading && !plugins.length" class="plugin-card p-8 text-center text-sm text-[var(--muted-foreground)]" role="status">
          {{ t('adminPlatform.plugins.loadingInstalledPlugins') }}
        </p>
        <div v-else-if="!plugins.length" class="plugin-card p-8 text-center">
          <p class="font-semibold">{{ t('admin.plugins.empty') }}</p>
          <p class="mt-2 text-sm text-[var(--muted-foreground)]">
            {{ t('adminPlatform.plugins.uploadASignedOhpluginOrS2pluginPackage') }}
          </p>
        </div>
        <div v-if="plugins.length" class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <article v-for="plugin in plugins" :key="plugin.id" class="plugin-card flex flex-col gap-4 p-5">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="break-words font-semibold">{{ plugin.name }}</h3>
                  <span class="plugin-status" :class="stateClass(plugin)">
                    <span aria-hidden="true">●</span>{{ t('adminPlatform.plugins.state.' + pluginState(plugin)) }}
                  </span>
                </div>
                <p class="mt-1 break-all font-mono text-xs text-[var(--muted-foreground)]">{{ plugin.plugin_key }} · {{ t('adminPlatform.plugins.version') }} {{ plugin.version }} · {{ plugin.author || t('adminPlatform.plugins.unknownAuthor') }}</p>
                <p v-if="plugin.description" class="mt-2 break-words text-sm text-[var(--muted-foreground)]">{{ plugin.description }}</p>
              </div>
              <button type="button" class="btn btn-secondary btn-sm shrink-0" :disabled="actionBusy" @click="openConfiguration(plugin)">{{ t('admin.plugins.configure') }}</button>
            </div>
            <div class="grid gap-3 text-sm sm:grid-cols-2">
              <div>
                <h4 class="font-semibold">{{ t('adminPlatform.plugins.compatibility') }}</h4>
                <p class="mt-1" :class="!plugin.compatibility.compatible ? 'text-[var(--bad-text)]' : !plugin.compatibility.tested ? 'text-[var(--warn-text)]' : 'text-[var(--ok-text)]'">{{ t(`admin.plugins.${plugin.compatibility.status}`) }}</p>
                <p class="mt-1 break-words text-xs text-[var(--muted-foreground)]">{{ t('adminPlatform.plugins.host') }} {{ plugin.compatibility.current_omnihive_version ?? plugin.compatibility.current_sub2api_version }} · {{ t('adminPlatform.plugins.required') }} {{ plugin.compatibility.required_omnihive_version ?? plugin.compatibility.required_sub2api_version }} · {{ t('adminPlatform.plugins.recommended') }} {{ (plugin.compatibility.recommended_omnihive_version ?? plugin.compatibility.recommended_sub2api_version) || t('adminPlatform.plugins.unspecified') }}</p>
              </div>
              <div>
                <h4 class="font-semibold">{{ t('adminPlatform.plugins.runtime') }}</h4>
                <p class="mt-1">{{ plugin.runtime_healthy ? t('adminPlatform.plugins.healthy') : t('adminPlatform.plugins.stopped') }} · {{ t('adminPlatform.plugins.signature') }} {{ plugin.signature_status === 'trusted' ? t('adminPlatform.plugins.trusted') : t('adminPlatform.plugins.unsigned') }}</p>
                <p v-if="plugin.last_error" class="mt-1 break-words text-xs text-[var(--bad-text)]">{{ plugin.last_error }}</p>
                <p v-else-if="pluginTests[plugin.id]" class="mt-1 break-words text-xs" :class="pluginTests[plugin.id].success ? 'text-[var(--ok-text)]' : 'text-[var(--bad-text)]'" role="status">{{ pluginTests[plugin.id].success ? t('adminPlatform.plugins.probePassed') : t('adminPlatform.plugins.probeFailed') }} · {{ pluginTests[plugin.id].latency_ms }} ms · {{ pluginTests[plugin.id].message }}</p>
                <p v-else class="mt-1 break-words text-xs text-[var(--muted-foreground)]">{{ plugin.runtime_message || t('adminPlatform.plugins.untested') }}</p>
              </div>
            </div>
            <div class="mt-auto">
              <label :for="`plugin-rollout-${plugin.id}`" class="block text-sm">{{ t('adminPlatform.plugins.traffic') }} <b class="font-mono tabular-nums">{{ rolloutValues[plugin.id] ?? currentRollout(plugin) }}%</b><span v-if="hasEnabledBinding(plugin)" class="text-[var(--muted-foreground)]"> · {{ t('adminPlatform.plugins.adjust') }}</span></label>
              <input :id="`plugin-rollout-${plugin.id}`" :value="rolloutValues[plugin.id] ?? currentRollout(plugin)" :style="{ '--rollout': `${rolloutValues[plugin.id] ?? currentRollout(plugin)}%` }" class="plugin-range mt-3" type="range" min="1" max="100" step="1" :disabled="hasEnabledBinding(plugin) || actionBusy" @input="setRollout(plugin.id, $event)" />
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="testPlugin(plugin)">{{ t('admin.plugins.test') }}</button>
              <button v-if="hasEnabledBinding(plugin)" type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy" @click="disablePlugin(plugin)">{{ t('admin.plugins.disable') }}</button>
              <button v-else type="button" class="btn btn-primary btn-sm" :disabled="actionBusy || plugin.state === 'starting' || !plugin.compatibility.compatible" @click="enablePlugin(plugin)">{{ t('admin.plugins.enable') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="actionBusy || hasEnabledBinding(plugin) || plugin.state === 'starting'" :title="hasEnabledBinding(plugin) ? t('adminPlatform.plugins.disableFirst') : t('adminPlatform.plugins.uninstall')" @click="uninstallPlugin(plugin)">{{ t('admin.plugins.uninstall') }}</button>
              <span v-if="busyID === plugin.id" class="self-center text-xs text-[var(--muted-foreground)]" role="status">{{ t('adminPlatform.plugins.busy') }}</span>
            </div>
          </article>
        </div>
      </section>

      <section class="plugin-card p-4" aria-labelledby="gateway-extensions-title">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h2 id="gateway-extensions-title" class="font-semibold">
              {{ t('adminPlatform.plugins.signedExtensionCapabilityInventory') }}
              <span class="ml-1.5 font-mono text-xs text-[var(--muted-foreground)]">({{ extensions.length }})</span>
            </h2>
            <p class="mt-1 text-sm text-[var(--muted-foreground)]">
              {{
                t('adminPlatform.plugins.liveInventoryOfHostVerifiedExtensionPackages')
              }}
            </p>
          </div>
        </div>

        <div
          v-if="extensionLoadError"
          data-testid="extensions-error-state"
          class="mt-3 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--border)] p-4 text-sm text-[var(--bad-text)]"
          role="alert"
        >
          <span>{{ extensionLoadError }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="loadPlugins">
            {{ t('adminPlatform.plugins.retry') }}
          </button>
        </div>
        <p
          v-else-if="loading && !extensions.length"
          class="py-6 text-center text-sm text-[var(--muted-foreground)]"
          role="status"
        >
          {{ t('adminPlatform.plugins.loadingExtensionCapabilityInventory') }}
        </p>
        <div
          v-else-if="!extensions.length"
          data-testid="extensions-empty-state"
          class="mt-3 rounded-lg border border-dashed border-[var(--border)] p-6 text-center"
        >
          <p class="text-sm font-medium">
            {{ t('adminPlatform.plugins.noSignedExtensionCapabilityPackagesRegistered') }}
          </p>
          <p class="mt-1 text-xs text-[var(--muted-foreground)]">
            {{
              t('adminPlatform.plugins.installAndVerifyASignedOhpluginArchive')
            }}
          </p>
        </div>
        <div v-else class="mt-3 grid grid-cols-1 gap-4 xl:grid-cols-2">
          <article
            v-for="extension in extensions"
            :key="extension.id"
            data-testid="extension-inventory-card"
            class="plugin-card flex flex-col gap-3.5 p-5"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="break-all font-mono text-sm font-semibold">{{ extension.id }}</h3>
                  <span class="plugin-status" :class="extensionStateClass(extension.state)">
                    <span aria-hidden="true">●</span>{{ extensionStateLabel(extension.state) }}
                  </span>
                </div>
                <p class="mt-1 break-all font-mono text-xs text-[var(--muted-foreground)]">
                  SDK v{{ extension.sdk_version }}
                  <template v-if="extension.publisher_id">
                    · {{ t('adminPlatform.plugins.publisher') }} {{ extension.publisher_id }}
                  </template>
                </p>
              </div>
            </div>

            <div v-if="extension.digest" class="flex items-center justify-between gap-2 rounded border border-[var(--border)] bg-[var(--surface-2)] px-2.5 py-1.5 font-mono text-[11px]">
              <span class="truncate text-[var(--muted-foreground)]" :title="extension.digest">
                SHA-256: {{ extension.digest }}
              </span>
              <CopyButton :value="extension.digest" size="xs" :aria-label="t('adminPlatform.plugins.copyArchiveDigest')" />
            </div>

            <div class="grid gap-3 text-xs sm:grid-cols-2">
              <div>
                <h4 class="font-semibold text-[var(--foreground)]">
                  {{ t('adminPlatform.plugins.authorizedActions') }}
                </h4>
                <div v-if="extension.authorized_actions?.length" class="mt-1.5 flex flex-wrap gap-1">
                  <span
                    v-for="act in extension.authorized_actions"
                    :key="act"
                    class="rounded border border-[var(--border)] bg-[var(--surface-2)] px-2 py-0.5 font-mono text-[11px]"
                  >
                    {{ act }}
                  </span>
                </div>
                <p v-else class="mt-1 text-[var(--muted-foreground)]">
                  {{ t('adminPlatform.plugins.noActiveAuthorizedActions') }}
                </p>
              </div>

              <div>
                <h4 class="font-semibold text-[var(--foreground)]">
                  {{ t('adminPlatform.plugins.declaredCapabilities') }}
                </h4>
                <ul v-if="extension.capabilities?.length" class="mt-1.5 space-y-1 font-mono text-[11px] text-[var(--muted-foreground)]">
                  <li v-for="(cap, idx) in extension.capabilities" :key="`${cap.id}-${idx}`">
                    {{ cap.id }} ({{ cap.platform }} / {{ cap.account_type }})
                  </li>
                </ul>
                <p v-else class="mt-1 text-[var(--muted-foreground)]">
                  {{ t('adminPlatform.plugins.noPlatformCapabilitiesDeclared') }}
                </p>
              </div>
            </div>
          </article>
        </div>
      </section>

      <BaseDialog :show="configPlugin !== null" :title="`${configPlugin?.name || ''} · ${t('adminPlatform.plugins.configure')}`" :width="configMode === 'iframe' ? 'full' : 'wide'" :close-on-escape="!configSaving" :show-close-button="!configSaving" @close="closeConfiguration">
        <div v-if="configMode === 'iframe'" class="space-y-3">
          <div class="flex justify-end">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="configSaving" @click="openJsonConfiguration">
              {{ t('adminPlatform.plugins.switchToJSONEditor') }}
            </button>
          </div>
          <div class="relative min-h-[520px] overflow-hidden bg-[var(--surface-2)]" :style="{ height: `${iframeHeight}px` }">
            <p v-if="uiLoading" class="absolute inset-0 flex items-center justify-center text-sm text-[var(--muted-foreground)]" role="status">{{ t('admin.plugins.loadingUI') }}</p>
            <iframe v-if="uiSession" ref="pluginFrame" :src="uiSession.url" sandbox="allow-scripts" referrerpolicy="no-referrer" class="h-full w-full border-0 bg-[var(--card)]" :title="t('adminPlatform.plugins.configTitle', { name: configPlugin?.name || '' })" @load="handlePluginFrameLoad" @error="openJsonConfiguration" />
          </div>
        </div>
        <div v-else class="space-y-3">
          <p class="text-sm text-[var(--muted-foreground)]">
            {{ t('adminPlatform.plugins.editTheFullJSONConfigurationObjectParameters') }}
          </p>
          <p v-if="jsonLoading" class="text-sm" role="status">{{ t('adminPlatform.plugins.loadingConfiguration') }}</p>
          <label for="plugin-config-json" class="block text-sm font-medium">{{ t('adminPlatform.plugins.jsonConfigurationObject') }}</label>
          <textarea id="plugin-config-json" v-model="jsonDraft" class="input min-h-64 w-full font-mono text-sm" rows="14" spellcheck="false" :disabled="jsonLoading || configSaving || !jsonReady" :aria-invalid="!!jsonError" :aria-describedby="jsonError ? 'plugin-config-error' : undefined" />
          <p v-if="jsonError" id="plugin-config-error" class="break-words text-sm text-[var(--bad-text)]" role="alert">{{ jsonError }}</p>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-primary btn-sm" :disabled="jsonLoading || configSaving || !jsonReady" @click="saveJsonConfiguration">
              {{ configSaving ? (t('adminPlatform.plugins.saving')) : (t('adminPlatform.plugins.saveConfiguration')) }}
            </button>
            <button v-if="!jsonReady && !jsonLoading" type="button" class="btn btn-secondary btn-sm" @click="openJsonConfiguration">
              {{ t('adminPlatform.plugins.reload') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="configSaving" @click="closeConfiguration">
              {{ t('adminPlatform.plugins.close') }}
            </button>
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
import type { ExtensionInventoryItem, PluginTestResult } from '@/api/admin/plugins'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
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
const extensions = ref<ExtensionInventoryItem[]>([])
const loading = ref(false)
const uploading = ref(false)
const busyID = ref<number | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const dragDepth = ref(0)
const rolloutValues = ref<Record<number, number>>({})
const pluginTests = ref<Record<number, PluginTestResult>>({})
const pluginLoadError = ref('')
const extensionLoadError = ref('')
const configPlugin = ref<PluginInstallation | null>(null)
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
const actionBusy = computed(() => loading.value || uploading.value || busyID.value !== null || configSaving.value)
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
  } else {
    extensionLoadError.value = errorMessage(extensionResult.reason)
  }
  loading.value = false
}

async function installFiles(files: FileList | File[] | null | undefined): Promise<void> {
  if (actionBusy.value || !files?.length) return
  if (files.length !== 1) {
    appStore.showError(t('adminPlatform.plugins.oneFile'))
    return
  }
  const file = files[0]
  const name = file.name.toLowerCase()
  const validExtension = ['.ohplugin', '.s2plugin'].some(ext => name.endsWith(ext) && name.length > ext.length)
  if (!validExtension || file.size <= 0) {
    appStore.showError(t('adminPlatform.plugins.emptyFile'))
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
    acceptUntested ? t('adminPlatform.plugins.untestedRisk') : '',
    plugin.signature_status !== 'trusted' ? t('adminPlatform.plugins.signatureRisk') : ''
  ].filter(Boolean)
  if (risks.length && !window.confirm(`${risks.join('\n')}\n${t('adminPlatform.plugins.confirmRisk', { name: plugin.name })}`)) return
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

function extensionStateLabel(state: string): string {
  if (state === 'enabled') return t('adminPlatform.plugins.enabled')
  if (state === 'starting') return t('adminPlatform.plugins.starting')
  if (state === 'error' || state === 'incompatible') return t('adminPlatform.plugins.error')
  return t('adminPlatform.plugins.disabled')
}

function extensionStateClass(state: string): string {
  if (state === 'enabled') return 'status-ok'
  if (state === 'starting') return 'status-warn'
  if (state === 'error' || state === 'incompatible') return 'status-bad'
  return 'status-neutral'
}

function resetConfiguration(): void {
  configGeneration++
  window.clearTimeout(frameTimeout)
  clearPendingBridgeRequests()
  pluginFrameLoaded.value = false
  configPlugin.value = null
  uiSession.value = null
  uiLoading.value = false
  jsonDraft.value = ''
  jsonOriginal.value = ''
  jsonError.value = ''
  jsonReady.value = false
  jsonLoading.value = false
}

function canCloseConfiguration(): boolean {
  return !configSaving.value && (jsonDraft.value === jsonOriginal.value || window.confirm(t('adminPlatform.plugins.discard')))
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
    if (!session.url || !session.bridge_token) throw new Error(t('adminPlatform.plugins.uiUnavailable'))
    const url = new URL(session.url, window.location.href)
    if (!['http:', 'https:'].includes(url.protocol) || url.origin !== window.location.origin) throw new Error(t('adminPlatform.plugins.untrustedUrl'))
    uiSession.value = session
  } catch {
    if (generation === configGeneration) await openJsonConfiguration()
  }
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
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(t('adminPlatform.plugins.objectRequired'))
    config = parsed as Record<string, unknown>
  } catch (error: unknown) {
    jsonError.value = error instanceof SyntaxError ? t('adminPlatform.plugins.invalidJson') : errorMessage(error)
    return
  }
  const plugin = configPlugin.value
  if (!plugin) return
  const generation = configGeneration
  configSaving.value = true
  try {
    const saved = await pluginStepUp.run(() => adminAPI.plugins.saveJsonConfig(plugin.id, config))
    if (generation !== configGeneration) return
    jsonDraft.value = JSON.stringify(saved, null, 2)
    jsonOriginal.value = jsonDraft.value
    appStore.showSuccess(t('adminPlatform.plugins.configurationSaved'))
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
        if (actionBusy.value) throw new Error(t('adminPlatform.plugins.otherAction'))
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
        if (actionBusy.value) throw new Error(t('adminPlatform.plugins.otherAction'))
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
  if (!plugin.compatibility.compatible || plugin.state === 'incompatible') return 'incompatible'
  if (plugin.state === 'error' || plugin.last_error) return 'error'
  if (plugin.state === 'starting') return 'starting'
  return hasEnabledBinding(plugin) ? 'enabled' : 'disabled'
}

function stateClass(plugin: PluginInstallation): string {
  const state = pluginState(plugin)
  if (state === 'incompatible' || state === 'error') return 'status-bad'
  if (state === 'enabled') return 'status-ok'
  return state === 'starting' ? 'status-warn' : 'status-neutral'
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
