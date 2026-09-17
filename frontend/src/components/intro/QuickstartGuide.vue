<template>
  <section class="card space-y-4 p-5" aria-labelledby="quickstart-heading">
    <h2 id="quickstart-heading" class="text-lg font-semibold">{{ text('按 Key 生成接入示例', 'Generate examples for your key') }}</h2>
    <p class="text-secondary text-sm">{{ text('仅在点击后读取我的 Key 与模型目录；不执行生成请求。密钥只用于请求头，示例从环境变量读取。', 'Read your keys and model catalogue only after a click; no generation requests run here. Keys stay in request headers and examples read environment variables.') }}</p>
    <button class="btn btn-secondary" :disabled="loading" @click="loadKeys(1)">{{ text('读取我的 Key', 'Load my keys') }}</button>
    <p v-if="loading" role="status">{{ text('正在读取元数据…', 'Loading metadata…') }}</p>
    <p v-else-if="error" role="alert">{{ error }}</p>
    <p v-else-if="loaded && !keys.length" role="status">{{ text('本页没有已绑定分组的可用 Key；可翻页查看，或创建、启用 Key 并绑定分组。', 'No active grouped keys on this page. Check other pages, or create or enable a key and assign a group.') }}</p>
    <template v-if="keys.length || hasNext || page > 1">
      <p class="break-all text-sm">{{ text('网关地址：', 'Gateway URL: ') }}{{ root }}</p>
      <label class="flex items-start gap-2 text-sm">
        <input v-model="confirmedRoot" type="checkbox" :true-value="root" :false-value="''" />
        {{ text('我确认此网关地址可信，同意向它发送所选 Key 以读取模型。', 'I trust this gateway URL and agree to send the selected key to it to read models.') }}
      </label>
      <label class="block">{{ text('我的 Key / 分组', 'My key / group') }}
        <select v-model="selectedKey" class="input mt-1">
          <option :value="null">{{ text('请选择', 'Select a key') }}</option>
          <option v-for="key in keys" :key="key.id" :value="key.id">{{ key.name }} · {{ key.group }}</option>
        </select>
      </label>
      <div class="flex gap-3">
        <button v-if="page > 1" class="btn btn-secondary" :disabled="loading" @click="loadKeys(page - 1)">{{ text('上一页', 'Previous') }}</button>
        <button v-if="hasNext" class="btn btn-secondary" :disabled="loading" @click="loadKeys(page + 1)">{{ text('下一页', 'Next') }}</button>
        <button class="btn btn-secondary" :disabled="!selectedKey || confirmedRoot !== root || loading" @click="loadModels">{{ text('读取该 Key 的模型目录', 'Load models for this key') }}</button>
      </div>
    </template>
    <template v-if="catalogLoaded && !loading && !error">
      <p role="status">{{ text('目录读取成功；账务回执：不适用（未执行生成）。', 'Catalogue loaded; billing receipt: not applicable (no generation executed).') }}</p>
      <p v-if="!models.length" role="status">{{ text('此分组当前没有可见模型。', 'No visible models in this group.') }}</p>
      <template v-else>
        <label class="block">{{ text('筛选模型', 'Filter models') }}<input v-model="filter" class="input mt-1" type="search" /></label>
        <p v-if="!filteredModels.length" role="status">{{ text('筛选无匹配；清除筛选后重试。', 'No filter matches; clear the filter to try again.') }}</p>
        <label v-else class="block">{{ text('可见模型', 'Visible model') }}
          <select v-model="modelId" class="input mt-1"><option v-for="model in filteredModels" :key="model.id" :value="model.id">{{ model.id }}</option></select>
        </label>
        <template v-if="selectedModel">
          <p v-if="selectedModel.endpoints === null" role="status">{{ text('目录未声明端点能力。请按分组文档选择协议；此示例不代表可用性已验证。', 'The catalogue does not declare endpoint capabilities. Choose a protocol using your group documentation; this example is not an availability check.') }}</p>
          <p v-else-if="!endpointOptions.length" role="status">{{ text('未声明本指引支持的端点，暂不生成示例。', 'No supported example endpoints are declared.') }}</p>
          <label v-if="endpointOptions.length" class="block">{{ text('请求协议', 'Request protocol') }}
            <select v-model="protocol" class="input mt-1"><option value="">{{ text('请选择协议', 'Select a protocol') }}</option><option v-for="endpoint in endpointOptions" :key="endpoint" :value="endpoint">{{ endpoint }}</option></select>
          </label>
          <template v-if="example">
            <p class="text-secondary break-all text-sm">{{ example.url }}</p>
            <p class="text-secondary text-sm">{{ text('手动运行示例可能产生费用。HTTP 200 仅表示传输成功；生成后的账务回执未确认时为 pending receipt，请在我的用量核对。', 'Running these examples may incur charges. HTTP 200 is transport success only; an unconfirmed billing receipt remains pending receipt. Check My usage after generation.') }}</p>
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

const { locale } = useI18n()
const text = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en
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
    if (current === revision) error.value = text('无法读取 Key 或公开地址，请重试。', 'Could not load keys or public URL. Retry.')
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
      error.value = text('无权读取模型目录，请核对 Key、分组和访问权限。', 'Model catalogue access denied. Check the key, group and access permissions.')
      return
    }
    if (!response.ok) throw new Error('Catalogue unavailable')
    const payload: unknown = await response.json()
    if (current !== revision) return
    models.value = parseModels(payload)
    catalogLoaded.value = true
  } catch {
    // Do not expose server response bodies, request objects, or credentials in logs/toasts.
    if (current === revision) error.value = text('目录读取失败，请核对分组、公开地址与访问权限后重试。', 'Catalogue failed. Check group, public URL and access, then retry.')
  } finally {
    if (current === revision) loading.value = false
  }
}
</script>
