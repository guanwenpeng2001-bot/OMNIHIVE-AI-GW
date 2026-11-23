<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import Badge from '@/components/ui/Badge.vue'
import { useAppStore, useAuthStore } from '@/stores'
import {
  matchesModalityFilter,
  statusStateLabel,
  statusStateTone
} from '@/features/public/format'
import { toCatalogModelVM } from '@/features/catalog/viewModel'
import {
  listPublicModels,
  getPublicStatus,
  type PublicModelSummary,
  type PublicStatusResponse
} from '@/features/public/api'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const authStore = useAuthStore()
let appStore: ReturnType<typeof useAppStore> | null = null
try {
  appStore = useAppStore()
} catch {
  appStore = null
}
const modelPlazaVisible = computed(
  () => appStore?.cachedPublicSettings?.model_plaza_enabled !== false
)
const modelCountText = ref<string | null>(null)
const providerCountText = ref<string | null>(null)
const sampleModelId = ref('gpt-4o')
const sampleProvider = ref('openai')
const status = ref<PublicStatusResponse | null>(null)
const snippetTab = ref<'curl' | 'openai' | 'anthropic'>('curl')

const loadingCatalog = ref(true)
const loadingStatus = ref(true)

const gatewayOrigin = computed(() => {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'https://api.omnihive.dev'
})

const snippetCode = computed(() => {
  const model = sampleModelId.value
  if (snippetTab.value === 'openai') {
    return `import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '${gatewayOrigin.value}/v1',
  apiKey: process.env.OMNIHIVE_KEY
})

const res = await client.chat.completions.create({
  model: '${model}',
  messages: [{ role: 'user', content: 'hello' }]
})`
  }
  if (snippetTab.value === 'anthropic') {
    return `curl ${gatewayOrigin.value}/v1/messages \\
  -H "x-api-key: $OMNIHIVE_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{"model":"${model}","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}'`
  }
  return `curl ${gatewayOrigin.value}/v1/chat/completions \\
  -H "Authorization: Bearer $OMNIHIVE_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"${model}",
       "messages":[{"role":"user",
                    "content":"hello"}]}'`
})

const features = computed(() => [
  {
    title: isZh.value ? '精确十进制计费与自带密钥 (BYOK)' : 'Exact billing & scoped BYOK',
    body: isZh.value
      ? '高精度十进制费率快照、逐次请求费用回执与对账总账，支持工作空间与项目级自带凭据隔离调度。'
      : 'Decimal pricing, per-attempt receipts, scoped BYOK credentials, and a ledger you can reconcile.'
  },
  {
    title: isZh.value ? '可解释路由与诚实遥测' : 'Explainable routing & honest telemetry',
    body: isZh.value
      ? '完整查看每次请求的候选池、排除原因与最终选中节点；低样本指标明确标记为未观测。'
      : 'See every candidate, exclusion reason, and live TTFT telemetry — low-sample data stays explicitly unobserved.'
  },
  {
    title: isZh.value ? '多协议零侵入切换' : 'Multi-protocol provider freedom',
    body: isZh.value
      ? '通过统一的 OpenAI Chat / Responses、Anthropic Messages 与 Gemini 兼容端点直连主流模型供应商。'
      : 'OpenAI, Anthropic, Gemini, and custom providers behind one wire-compatible inference surface.'
  }
])

const steps = computed(() =>
  isZh.value
    ? ['创建受作用域约束的项目 API Key 或登记 BYOK 凭据', '通过标准 OpenAI / Anthropic / Gemini SDK 发起调用', '在控制台逐条核对路由解释、实测延迟与精确费用回执']
    : ['Create a project-scoped API key or enroll BYOK', 'Call any published model via standard SDKs', 'Inspect route trace, live latency, and exact receipt']
)

function resolveSampleProvider(model?: PublicModelSummary | null): string {
  const explicit = model?.provider?.trim()
  if (explicit && explicit.toLowerCase() !== 'unknown') {
    return explicit
  }
  const identifier = model?.model?.trim() || ''
  if (identifier.includes('/')) {
    const prefix = identifier.split('/')[0]?.trim()
    if (prefix) return prefix
  }
  return 'openai'
}

onMounted(() => {
  if (modelPlazaVisible.value) {
    void Promise.allSettled([
      listPublicModels({ limit: 100, sort: 'availability' }),
      listPublicModels({ availability_state: 'live', modality: ['text'], limit: 10, sort: 'name' })
    ])
      .then(([catalogResult, liveTextResult]) => {
        const catalogItems =
          catalogResult.status === 'fulfilled' ? (catalogResult.value.items ?? []) : []
        const liveTextItems =
          liveTextResult.status === 'fulfilled' ? (liveTextResult.value.items ?? []) : []
        const isLive = (m: PublicModelSummary) => toCatalogModelVM(m).availability.state === 'live'
        const preferredLive =
          liveTextItems.find(
            (m) => isLive(m) && matchesModalityFilter(m, 'text')
          ) ??
          catalogItems.find(
            (m) => isLive(m) && matchesModalityFilter(m, 'text')
          ) ??
          liveTextItems.find((m) => isLive(m)) ??
          catalogItems.find((m) => isLive(m)) ??
          catalogItems.find((m) => matchesModalityFilter(m, 'text')) ??
          liveTextItems[0] ??
          catalogItems[0]
        if (preferredLive?.model) {
          sampleModelId.value = preferredLive.model
          sampleProvider.value = resolveSampleProvider(preferredLive)
        }
        if (catalogResult.status === 'fulfilled') {
          const page = catalogResult.value
          modelCountText.value = page.next_cursor ? `${catalogItems.length}+` : String(catalogItems.length)
          const providers = new Set(
            catalogItems.map((item) => item.provider).filter((p) => p && p !== 'unknown')
          )
          providerCountText.value =
            page.next_cursor && providers.size > 0 ? `${providers.size}+` : String(providers.size)
        }
      })
      .finally(() => {
        loadingCatalog.value = false
      })
  } else {
    loadingCatalog.value = false
  }

  void getPublicStatus().then((response) => {
    status.value = response
  }).catch(() => {}).finally(() => { loadingStatus.value = false })
})

const ctaTarget = () => (authStore.isAuthenticated ? '/dashboard' : '/login')
</script>

<template>
  <PublicShell>
    <section class="relative overflow-hidden border-b border-oh-default">
      <div class="pointer-events-none absolute inset-y-0 right-0 hidden w-1/2 lg:block" aria-hidden="true">
        <svg viewBox="0 0 400 400" class="h-full w-full text-oh-accent opacity-10">
          <defs>
            <pattern id="comb" width="40" height="34" patternUnits="userSpaceOnUse">
              <path d="M20 1 39 12v22L20 45 1 34V12Z" fill="none" stroke="currentColor" stroke-width="1" />
            </pattern>
          </defs>
          <rect width="400" height="400" fill="url(#comb)" />
        </svg>
      </div>
      <div class="relative grid items-center gap-10 py-14 lg:grid-cols-12 lg:gap-12 lg:py-20 xl:gap-16 xl:py-24">
        <div class="min-w-0 lg:col-span-6">
          <p class="font-mono text-xs uppercase tracking-[0.22em] text-oh-muted">
            {{ isZh ? 'OMNIHIVE · 企业级 AI 推理网关' : 'OMNIHIVE · AI GATEWAY' }}
          </p>
          <h1 class="mt-4 max-w-[18ch] text-4xl font-semibold tracking-tight text-oh-primary sm:text-5xl xl:text-[3.25rem] xl:leading-[1.12]">
            <template v-if="isZh">
              统一接口连接全球模型，<br />专为生产级路由与精确计费打造。
            </template>
            <template v-else>
              One API for every model.<br />Built for teams that bill.
            </template>
          </h1>
          <p class="mt-5 max-w-[60ch] text-base leading-relaxed text-oh-secondary">
            {{
              isZh
                ? '通过单一标准端点统一调度多家模型供应商。透明 Token 费率、可解释路由决策、工作空间 BYOK 托管与逐请求账单核销，告别黑盒转发。'
                : 'Route to multiple providers through a single compatible endpoint. Exact pricing, explainable routing, scoped BYOK, and per-request receipts — no surprises.'
            }}
          </p>
          <div class="mt-8 flex flex-wrap items-center gap-3">
            <RouterLink :to="ctaTarget()" class="oh-focus-ring inline-flex h-10 items-center rounded-oh-md bg-oh-accent px-4 text-sm font-semibold text-oh-accent-ink hover:bg-oh-accent-hover active:bg-oh-accent-active">
              {{ authStore.isAuthenticated ? (isZh ? '进入控制台' : 'Open Console') : (isZh ? '立即开始' : 'Get started') }}
            </RouterLink>
            <RouterLink
              v-if="modelPlazaVisible"
              to="/model-plaza"
              class="oh-focus-ring inline-flex h-10 items-center rounded-oh-md border border-oh-default bg-oh-surface px-4 text-sm font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
            >
              {{ isZh ? '浏览模型广场' : 'Browse models' }}
            </RouterLink>
            <RouterLink
              v-if="modelPlazaVisible"
              to="/pricing"
              class="oh-focus-ring inline-flex h-10 items-center rounded-oh-md border border-transparent px-3 text-sm font-medium text-oh-secondary hover:text-oh-primary"
            >
              {{ isZh ? '查看透明定价 →' : 'View pricing →' }}
            </RouterLink>
          </div>
          <dl class="mt-10 flex flex-wrap gap-8 font-mono text-sm xl:gap-12">
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">{{ isZh ? '已发布模型' : 'Models' }}</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary">
                <Skeleton v-if="loadingCatalog" :label="isZh ? '正在加载模型数量' : 'Loading model count'" />
                <template v-else>{{ modelCountText ?? '—' }}</template>
              </dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">{{ isZh ? '接入供应商' : 'Providers' }}</dt>
              <dd class="mt-1 text-2xl tabular-nums text-oh-primary">
                <Skeleton v-if="loadingCatalog" :label="isZh ? '正在加载供应商数量' : 'Loading provider count'" />
                <template v-else>{{ providerCountText ?? '—' }}</template>
              </dd>
            </div>
            <div>
              <dt class="text-[11px] uppercase tracking-wider text-oh-muted">{{ isZh ? '网关状态' : 'Status' }}</dt>
              <dd class="mt-2">
                <Skeleton v-if="loadingStatus" :label="isZh ? '正在加载网关状态' : 'Loading service status'" />
                <RouterLink v-else to="/status" class="oh-focus-ring inline-flex rounded-oh-sm">
                  <Badge :tone="statusStateTone(status?.overall)" variant="subtle" dot>
                    {{ statusStateLabel(status?.overall, isZh) }}
                  </Badge>
                </RouterLink>
              </dd>
            </div>
          </dl>
        </div>
        <div class="min-w-0 items-center lg:col-span-6 lg:flex lg:justify-end">
          <div class="w-full max-w-xl border border-oh-default bg-oh-surface p-4 font-mono text-xs sm:p-5 xl:max-w-2xl xl:p-6 xl:text-[13px]">
            <div class="mb-3 flex flex-wrap items-center justify-between gap-3 border-b border-oh-subtle pb-3">
              <div class="flex items-center gap-1" role="tablist" :aria-label="isZh ? '调用协议示例' : 'API request examples'">
                <button
                  v-for="tab in [
                    { id: 'curl', label: 'cURL' },
                    { id: 'openai', label: 'OpenAI SDK' },
                    { id: 'anthropic', label: 'Anthropic' }
                  ]"
                  :key="tab.id"
                  type="button"
                  role="tab"
                  :aria-selected="snippetTab === tab.id"
                  :class="[
                    'oh-focus-ring rounded-oh-xs px-2 py-1 font-mono text-[11px] transition-colors',
                    snippetTab === tab.id
                      ? 'bg-oh-accent-subtle font-semibold text-oh-accent-text'
                      : 'text-oh-muted hover:text-oh-primary'
                  ]"
                  @click="snippetTab = tab.id as 'curl' | 'openai' | 'anthropic'"
                >
                  {{ tab.label }}
                </button>
              </div>
              <CopyButton :value="snippetCode" variant="secondary" :aria-label="isZh ? '复制调用示例' : 'Copy API request'" />
            </div>
            <pre class="oh-focus-ring overflow-x-auto leading-6 text-oh-secondary" tabindex="0" :aria-label="isZh ? 'API 调用请求示例' : 'Example API request'"><code>{{ snippetCode }}

<span class="text-oh-accent-text">HTTP/2 200</span>
<span class="text-oh-muted">x-execution-id:</span> 7ca789de-e632
<span class="text-oh-muted">x-provider:</span> {{ sampleProvider }}</code></pre>
          </div>
        </div>
      </div>
    </section>

    <section class="grid gap-px border-b border-oh-default bg-oh-default sm:grid-cols-3">
      <div v-for="feature in features" :key="feature.title" class="bg-oh-canvas p-6 xl:p-8">
        <h2 class="text-sm font-semibold text-oh-primary">{{ feature.title }}</h2>
        <p class="mt-2 max-w-[48ch] text-sm leading-relaxed text-oh-secondary">{{ feature.body }}</p>
      </div>
    </section>

    <section class="py-12">
      <div class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 class="text-2xl font-semibold text-oh-primary">
            {{ isZh ? '三步接入生产级 AI 网关' : 'Start in three steps' }}
          </h2>
          <p class="mt-2 max-w-[62ch] text-sm text-oh-secondary">
            {{
              isZh
                ? '创建受控密钥，调用标准兼容端点，随后在控制台实时追踪路由轨迹与逐笔账单。'
                : 'Create a key, call the standard endpoint, then watch the request and its receipt.'
            }}
          </p>
        </div>
        <RouterLink
          v-if="modelPlazaVisible"
          to="/model-plaza"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-4 text-sm font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
        >
          {{ isZh ? '查看全部模型' : 'See all models' }}
        </RouterLink>
      </div>
      <ol class="mt-8 grid gap-4 sm:grid-cols-3 xl:gap-6">
        <li v-for="(step, index) in steps" :key="step" class="border border-oh-default p-5 xl:p-6">
          <span class="font-mono text-xs text-oh-muted">0{{ index + 1 }}</span>
          <p class="mt-3 text-sm text-oh-primary">{{ step }}</p>
        </li>
      </ol>
    </section>
  </PublicShell>
</template>
