<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl text-gray-900 dark:text-gray-100">
      <header class="mb-10 max-w-3xl">
        <p class="mb-3 text-xs font-semibold uppercase tracking-widest text-action dark:text-primary-300">{{ t('helpGuide.eyebrow') }}</p>
        <h1 class="text-3xl font-bold tracking-tight md:text-4xl">{{ t('helpGuide.title') }}</h1>
        <p class="text-secondary mt-4 text-lg leading-8">{{ t('helpGuide.subtitle') }}</p>
      </header>
      <div class="grid items-start gap-8 lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-12">
        <DocSidebar :sections="sections" :active="activeSection" />
        <article class="min-w-0 space-y-12 pb-24">
          <p class="card text-secondary p-5 text-sm leading-7">{{ t('helpGuide.placeholder') }}</p>
          <p v-if="!apiRoot" class="card border-l-4 border-amber-400 p-5 text-sm leading-7 text-amber-700 dark:text-amber-300" role="alert">
            {{ t('helpGuide.apiBaseMissing') }}
          </p>
          <DocSection v-for="section in sections" :id="section.id" :key="section.id" :title="section.title">
            <QuickstartGuide v-if="section.id === 'quick' && authStore.user" />
            <ol v-if="section.steps.length" class="list-decimal space-y-4 pl-6 marker:font-semibold marker:text-action dark:marker:text-primary-300">
              <li v-for="step in section.steps" :key="step" class="pl-2">
                <h3 class="font-semibold">{{ t(`helpGuide.${section.id}.steps.${step}.title`) }}</h3>
                <p class="text-secondary mt-1 text-sm leading-7">{{ t(`helpGuide.${section.id}.steps.${step}.body`) }}</p>
              </li>
            </ol>
            <div v-for="table in section.tables" :key="table.id" class="space-y-3">
              <h3 :id="`${section.id}-${table.id}-title`" class="font-semibold">{{ t(`${table.path}.title`) }}</h3>
              <div class="overflow-x-auto rounded-xl border focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" role="region" tabindex="0" :aria-labelledby="`${section.id}-${table.id}-title`">
                <table class="w-full min-w-[640px] text-left text-sm leading-7" :aria-labelledby="`${section.id}-${table.id}-title`">
                  <thead class="bg-gray-50 dark:bg-dark-800">
                    <tr><th v-for="column in table.columns" :key="column" scope="col" class="whitespace-nowrap px-4 py-3">{{ t(`${table.path}.columns.${column}`) }}</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in table.rows" :key="row" class="border-t align-top">
                      <th scope="row" class="px-4 py-3 font-medium">{{ t(`${table.path}.rows.${row}.${table.columns[0]}`) }}</th>
                      <td v-for="column in table.columns.slice(1)" :key="column" class="text-secondary min-w-40 whitespace-pre-line break-words px-4 py-3">{{ t(`${table.path}.rows.${row}.${column}`) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <ul v-if="section.notes.length" class="text-secondary list-disc space-y-2 pl-5 text-sm leading-7">
              <li v-for="note in section.notes" :key="note">{{ t(`helpGuide.${section.id}.notes.${note}`) }}</li>
            </ul>
            <CodeBlock v-for="example in examples[section.id] || []" :key="example.label" :label="example.label" :code="example.code" />
            <nav v-if="links[section.id]?.length" :aria-label="t('helpGuide.next')" class="flex flex-wrap gap-x-6 gap-y-3 text-sm">
              <RouterLink v-for="link in links[section.id]" :key="link.path" :to="link.path" class="font-medium text-action dark:text-primary-300 underline underline-offset-4">{{ t(link.key) }} →</RouterLink>
            </nav>
          </DocSection>
        </article>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { publicRoot } from '@/components/intro/quickstart'
import AppLayout from '@/components/layout/AppLayout.vue'
import DocSidebar from '@/components/intro/DocSidebar.vue'
import DocSection from '@/components/intro/DocSection.vue'
import CodeBlock from '@/components/intro/CodeBlock.vue'
import QuickstartGuide from '@/components/intro/QuickstartGuide.vue'
const { t, tm } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

/**
 * The manual may only show the address this deployment actually publishes.
 * Without `api_base_url` no example is rendered, because neither a placeholder
 * domain nor the console origin is guaranteed to be the gateway entry.
 */
const apiRoot = computed(() => {
  const configured = appStore.apiBaseUrl?.trim()
  if (!configured) return ''
  try {
    return publicRoot(configured, '')
  } catch {
    return ''
  }
})
const apiBase = computed(() => (apiRoot.value ? `${apiRoot.value}/v1` : ''))
const sectionIds = ['quick', 'keys', 'models', 'clients', 'usage', 'billing', 'faq'] as const
const sections = computed(() => sectionIds.map(id => {
  const tables = tm(`helpGuide.${id}.tables`) as Record<string, { columns: Record<string, unknown>; rows: Record<string, unknown> }>
  return {
    id,
    title: t(`helpGuide.${id}.title`),
    steps: Object.keys(tm(`helpGuide.${id}.steps`)),
    notes: Object.keys(tm(`helpGuide.${id}.notes`)),
    tables: Object.entries(tables).map(([tableId, table]) => ({
      id: tableId,
      path: `helpGuide.${id}.tables.${tableId}`,
      columns: Object.keys(table.columns),
      rows: Object.keys(table.rows)
    }))
  }
}))
const activeSection = ref<string>('quick')
let animationFrame = 0
function updateActiveSection() {
  animationFrame = 0
  const visibleSections = sectionIds.map(id => document.getElementById(id)).filter((element): element is HTMLElement => Boolean(element))
  const preceding = visibleSections.filter(element => element.getBoundingClientRect().top <= 160)
  activeSection.value = preceding[preceding.length - 1]?.id || 'quick'
  if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 8) activeSection.value = 'faq'
}
function scheduleActiveSection() {
  if (!animationFrame) animationFrame = window.requestAnimationFrame(updateActiveSection)
}
onMounted(async () => {
  await nextTick()
  // Native hash links also work when JavaScript scroll tracking is unavailable.
  const targetId = window.location.hash.slice(1)
  if (sectionIds.some(id => id === targetId)) document.getElementById(targetId)?.scrollIntoView()
  updateActiveSection()
  window.addEventListener('scroll', scheduleActiveSection, { passive: true })
  window.addEventListener('resize', scheduleActiveSection, { passive: true })
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', scheduleActiveSection)
  window.removeEventListener('resize', scheduleActiveSection)
  window.cancelAnimationFrame(animationFrame)
})
const relatedLinks: Record<string, { path: string; key: string }[]> = {
  quick: [{ path: '/keys', key: 'nav.apiKeys' }],
  keys: [
    { path: '/keys', key: 'nav.apiKeys' },
    // 真实的帮助入口：密钥用量页既可匿名粘贴外部密钥，也可登录后按 Key ID 查询。
    { path: '/key-usage', key: 'keyUsage.title' }
  ],
  usage: [
    { path: '/usage', key: 'nav.usage' },
    { path: '/key-usage', key: 'keyUsage.title' }
  ],
  billing: [{ path: '/purchase', key: 'nav.buySubscription' }, { path: '/subscriptions', key: 'nav.mySubscriptions' }, { path: '/redeem', key: 'nav.redeem' }, { path: '/orders', key: 'nav.myOrders' }]
}
const links = computed(() => Object.fromEntries(Object.entries(relatedLinks).map(([section, items]) => [section, items.filter(link => {
  // 密钥用量页是公开只读查询（也可登录后按 Key ID 查询），简单模式下同样保留。
  if (link.path === '/keys' || link.path === '/key-usage') return true
  if (authStore.isSimpleMode) return false
  if (link.path === '/purchase' || link.path === '/orders') return isFeatureFlagEnabled(FeatureFlags.payment)
  if (link.path === '/subscriptions') return isFeatureFlagEnabled(FeatureFlags.subscription)
  return true
})])))
const examples = computed((): Record<string, { label: string; code: string }[]> => {
  const base = apiBase.value
  if (!base) return {}
  return {
    quick: [
      { label: 'Bash · environment & models', code: String.raw`export SUB2API_BASE_URL="${base}"
export SUB2API_API_KEY="sk-your-api-key"
curl "$SUB2API_BASE_URL/models" \
  -H "Authorization: Bearer $SUB2API_API_KEY"` },
      { label: 'curl · chat/completions', code: String.raw`curl "$SUB2API_BASE_URL/chat/completions" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID_FROM_LIST","messages":[{"role":"user","content":"Hello"}]}'` }
    ],
    models: [{ label: 'curl · Codex catalog', code: String.raw`curl "${apiRoot.value}/backend-api/codex/models" \
  -H "Authorization: Bearer $SUB2API_API_KEY"` }],
    clients: [
      { label: 'Text · Cursor', code: String.raw`OpenAI Base URL: ${base}
OpenAI API Key: sk-your-api-key
Model: MODEL_ID_FROM_LIST` },
      { label: 'Cline / Roo Code · OpenAI Compatible', code: String.raw`API Provider: OpenAI Compatible
Base URL: ${base}
API Key: sk-your-api-key
Model: MODEL_ID_FROM_LIST` },
      { label: 'Continue · config.yaml', code: String.raw`name: Sub2API
version: 0.0.1
schema: v1
models:
  - name: MODEL_ID_FROM_LIST
    provider: openai
    model: MODEL_ID_FROM_LIST
    apiBase: ${base}
    apiKey: sk-your-api-key` },
      { label: 'JetBrains AI Assistant · OpenAI Compatible', code: String.raw`Settings | Tools | AI Assistant | Providers & API keys
Provider: OpenAI-compatible endpoint
URL: ${base}
API Key: sk-your-api-key` },
      { label: 'Python · OpenAI SDK', code: String.raw`# python -m pip install openai
import os
from openai import OpenAI

client = OpenAI(
    base_url=os.environ["SUB2API_BASE_URL"],
    api_key=os.environ["SUB2API_API_KEY"],
)
response = client.chat.completions.create(
    model="MODEL_ID_FROM_LIST",
    messages=[{"role": "user", "content": "Hello"}],
)
print(response.choices[0].message.content)` },
      { label: 'TOML · ~/.codex/config.toml', code: String.raw`model_provider = "sub2api"
model = "MODEL_ID_FROM_CODEX_LIST"

[model_providers.sub2api]
name = "Sub2API"
base_url = "${base}"
env_key = "SUB2API_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false` },
      { label: 'curl · SSE', code: String.raw`curl -N "$SUB2API_BASE_URL/chat/completions" \
  -H "Authorization: Bearer $SUB2API_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID_FROM_LIST","stream":true,"messages":[{"role":"user","content":"Hello"}]}'` }
    ]
  }
})
</script>
