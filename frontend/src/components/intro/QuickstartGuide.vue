<template>
  <section class="card space-y-4 p-5" aria-labelledby="quickstart-heading">
    <h2 id="quickstart-heading" class="text-lg font-semibold">{{ t('quickstartGuide.title') }}</h2>
    <p class="text-secondary text-sm">{{ t('quickstartGuide.hint') }}</p>
    <button class="btn btn-secondary" :disabled="loading" @click="loadKeys(1)">{{ t('quickstartGuide.loadKeys') }}</button>
    <p v-if="loading" role="status">{{ t('quickstartGuide.loading') }}</p>
    <p v-else-if="error" role="alert">{{ error }}</p>
    <p v-else-if="loaded && !keys.length" role="status">{{ t('quickstartGuide.noKeys') }}</p>
    <template v-if="keys.length || hasNext || page > 1">
      <p class="break-all text-sm">{{ t('quickstartGuide.gatewayUrl') }}{{ root }}</p>
      <label class="flex items-start gap-2 text-sm">
        <input v-model="confirmedRoot" type="checkbox" :true-value="root" :false-value="''" />
        {{ t('quickstartGuide.trustGateway') }}
      </label>
      <label class="block">{{ t('quickstartGuide.keyGroup') }}
        <select v-model="selectedKey" class="input mt-1">
          <option :value="null">{{ t('quickstartGuide.selectKey') }}</option>
          <option v-for="key in keys" :key="key.id" :value="key.id">{{ key.name }} · {{ key.group }}</option>
        </select>
      </label>
      <div class="flex gap-3">
        <button v-if="page > 1" class="btn btn-secondary" :disabled="loading" @click="loadKeys(page - 1)">{{ t('quickstartGuide.previous') }}</button>
        <button v-if="hasNext" class="btn btn-secondary" :disabled="loading" @click="loadKeys(page + 1)">{{ t('quickstartGuide.next') }}</button>
        <button class="btn btn-secondary" :disabled="!selectedKey || confirmedRoot !== root || loading" @click="loadModels">{{ t('quickstartGuide.loadModels') }}</button>
      </div>
    </template>
    <template v-if="catalogLoaded && !loading && !error">
      <p role="status">{{ t('quickstartGuide.loaded') }}</p>
      <p v-if="!models.length" role="status">{{ t('quickstartGuide.noModels') }}</p>
      <template v-else>
        <label class="block">{{ t('quickstartGuide.filter') }}<input v-model="filter" class="input mt-1" type="search" /></label>
        <p v-if="!filteredModels.length" role="status">{{ t('quickstartGuide.noMatches') }}</p>
        <label v-else class="block">{{ t('quickstartGuide.visibleModel') }}
          <select v-model="modelId" class="input mt-1"><option v-for="model in filteredModels" :key="model.id" :value="model.id">{{ model.id }}</option></select>
        </label>
        <template v-if="selectedModel">
          <p v-if="selectedModel.endpoints === null" role="status">{{ t('quickstartGuide.unknownEndpoints') }}</p>
          <p v-else-if="!endpointOptions.length" role="status">{{ t('quickstartGuide.unsupportedEndpoints') }}</p>
          <label v-if="endpointOptions.length" class="block">{{ t('quickstartGuide.protocol') }}
            <select v-model="protocol" class="input mt-1"><option value="">{{ t('quickstartGuide.selectProtocol') }}</option><option v-for="endpoint in endpointOptions" :key="endpoint" :value="endpoint">{{ endpoint }}</option></select>
          </label>
          <template v-if="example">
            <p class="text-secondary break-all text-sm">{{ example.url }}</p>
            <p class="text-secondary text-sm">{{ t('quickstartGuide.billingHint') }}</p>
            <CodeBlock v-for="item in example.examples" :key="item.label" :label="item.label" :code="item.code" />
          </template>
        </template>
      </template>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { keysAPI } from '@/api/keys'
import { apiClient } from '@/api/client'
import type { ApiKey, PublicSettings } from '@/types'
import CodeBlock from './CodeBlock.vue'
import { parseModels, protocols, publicRoot, requestExample, type Protocol, type VisibleModel } from './quickstart'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const keys = ref<{ id: number; name: string; group: string }[]>([])
const selectedKey = ref<number | null>(null)
const models = ref<VisibleModel[]>([])
const modelId = ref('')
const filter = ref('')
const protocol = ref<Protocol | ''>('')
const root = ref('')
const confirmedRoot = ref('')
const page = ref(1)
const hasNext = ref(false)
const loading = ref(false)
const loaded = ref(false)
const catalogLoaded = ref(false)
const error = ref('')
let revision = 0
let controller: AbortController | undefined
const filteredModels = computed(() => models.value.filter(model => model.id.toLowerCase().includes(filter.value.toLowerCase())))
const selectedModel = computed(() => filteredModels.value.find(model => model.id === modelId.value))
const endpointOptions = computed(() => selectedModel.value?.endpoints ?? protocols)
const example = computed(() => selectedModel.value && root.value && protocol.value && endpointOptions.value.includes(protocol.value)
  ? requestExample(root.value, selectedModel.value.id, protocol.value) : null)
watch(filteredModels, (items) => { if (!items.some(item => item.id === modelId.value)) modelId.value = items[0]?.id ?? '' })
watch(selectedModel, (model) => { protocol.value = model?.endpoints?.[0] ?? '' })

function resetModels() {
  revision++
  controller?.abort()
  models.value = []
  modelId.value = ''
  filter.value = ''
  catalogLoaded.value = false
  loading.value = false
  error.value = ''
}
function reset() {
  resetModels()
  keys.value = []
  selectedKey.value = null
  root.value = ''
  confirmedRoot.value = ''
  loaded.value = false
  page.value = 1
  hasNext.value = false
}
watch(() => [auth.user?.id, auth.user?.role, auth.sessionRevision], reset, { flush: 'sync' })
watch(() => app.apiBaseUrl, reset, { flush: 'sync' })
watch(selectedKey, resetModels, { flush: 'sync' })
watch(confirmedRoot, resetModels, { flush: 'sync' })
onBeforeUnmount(reset)

async function loadKeys(targetPage: number) {
  reset()
  const current = revision
  loading.value = true
  controller = new AbortController()
  try {
    const [result, settings] = await Promise.all([
      keysAPI.list(targetPage, 100, { status: 'active' }, { signal: controller.signal }),
      apiClient.get<PublicSettings>('/settings/public', { signal: controller.signal })
    ])
    if (current !== revision) return
    // Only metadata is retained in reactive state; never retain the list's key values.
    keys.value = result.items.filter(key => key.status === 'active' && key.group_id != null)
      .map(key => ({ id: key.id, name: key.name, group: key.group?.name ?? String(key.group_id) }))
    root.value = publicRoot(settings.data.api_base_url, new URL(import.meta.env.BASE_URL, window.location.origin).href)
    page.value = targetPage
    hasNext.value = targetPage * 100 < result.total
    loaded.value = true
  } catch {
    if (current === revision) error.value = t('quickstartGuide.keysFailed')
  } finally {
    if (current === revision) loading.value = false
  }
}
async function loadModels() {
  if (!selectedKey.value || !root.value || confirmedRoot.value !== root.value) return
  resetModels()
  const current = revision
  const id = selectedKey.value
  loading.value = true
  controller = new AbortController()
  try {
    // The existing detail wrapper has no cancellation option; use the same client and endpoint.
    const { data: key } = await apiClient.get<ApiKey>(`/keys/${id}`, { signal: controller.signal })
    if (current !== revision) return
    if (key.status !== 'active' || key.group_id == null) throw new Error('Unavailable key')
    const response = await fetch(`${root.value}/v1/models`, {
      headers: { Authorization: `Bearer ${key.key}` }, signal: controller.signal,
      cache: 'no-store', credentials: 'omit', redirect: 'error', referrerPolicy: 'no-referrer'
    })
    if (current !== revision) return
    if (response.status === 401 || response.status === 403) {
      error.value = t('quickstartGuide.accessDenied')
      return
    }
    if (!response.ok) throw new Error('Catalogue unavailable')
    const payload: unknown = await response.json()
    if (current !== revision) return
    models.value = parseModels(payload)
    catalogLoaded.value = true
  } catch {
    // Do not expose server response bodies, request objects, or credentials in logs/toasts.
    if (current === revision) error.value = t('quickstartGuide.catalogFailed')
  } finally {
    if (current === revision) loading.value = false
  }
}
</script>
