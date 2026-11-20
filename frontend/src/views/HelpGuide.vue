<template>
  <component :is="shellComponent">
    <div class="guide-document w-full text-[var(--foreground)]">
      <header class="mb-8 max-w-4xl border-b border-[var(--border)] pb-6">
        <p class="mb-3 text-xs font-semibold uppercase tracking-widest text-action dark:text-primary-300">{{ t('helpGuide.eyebrow') }}</p>
        <h1 class="text-3xl font-bold tracking-tight md:text-4xl">{{ t('helpGuide.title') }}</h1>
        <p class="text-secondary mt-4 text-lg leading-8">{{ t('helpGuide.subtitle') }}</p>

        <nav
          class="mt-6 flex flex-wrap items-center gap-2"
          :aria-label="isZh ? '按接入场景筛选文档' : 'Filter documentation by scenario'"
        >
          <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-secondary">
            {{ isZh ? '快捷场景' : 'Scenarios' }}
          </span>
          <button
            v-for="scenario in scenarioTabs"
            :key="scenario.id"
            type="button"
            class="rounded-full border px-3 py-1 text-xs font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
            :class="
              activeScenario === scenario.id
                ? 'border-primary-500 bg-primary-500/10 text-action dark:text-primary-300'
                : 'border-[var(--border)] bg-[var(--card)] text-secondary hover:border-primary-500/60 hover:text-[var(--foreground)]'
            "
            :aria-pressed="activeScenario === scenario.id"
            @click="selectScenario(scenario.id)"
          >
            {{ isZh ? scenario.labelZh : scenario.labelEn }}
          </button>
        </nav>
      </header>
      <div class="grid items-start gap-8 lg:grid-cols-[220px_minmax(0,1fr)] xl:grid-cols-[220px_minmax(0,1fr)_220px] lg:gap-10">
        <DocSidebar :sections="visibleSections" :active="activeSection" />
        <article class="min-w-0 space-y-12 pb-24">
          <div
            v-if="activeScenario !== 'all'"
            class="card flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs"
          >
            <span class="text-secondary">
              {{
                isZh
                  ? `当前仅展示「${currentScenarioMeta.labelZh}」相关章节（${visibleSections.length} / ${sections.length} 章）`
                  : `Showing "${currentScenarioMeta.labelEn}" chapters (${visibleSections.length} of ${sections.length})`
              }}
            </span>
            <button
              type="button"
              class="font-medium text-action underline underline-offset-4 dark:text-primary-300"
              @click="selectScenario('all')"
            >
              {{ isZh ? '查看完整手册' : 'Show all chapters' }}
            </button>
          </div>
          <div class="card space-y-3 p-5 text-sm leading-7 text-secondary"><p>{{ t('helpGuide.placeholder') }}</p><p>{{ t('helpGuide.capability') }}</p></div>
          <p v-if="!apiRoot" class="card border-l-4 border-amber-400 p-5 text-sm leading-7 text-amber-700 dark:text-amber-300" role="alert">
            {{ t('helpGuide.apiBaseMissing') }}
          </p>
          <DocSection v-for="section in visibleSections" :id="section.id" :key="section.id" :title="section.title">
            <p v-if="te(`helpGuide.${section.id}.endpoint`)" class="break-words rounded-lg bg-[var(--surface-2)] px-3 py-2 font-mono text-xs leading-6 ">{{ t(`helpGuide.${section.id}.endpoint`) }}</p>
            <p v-if="te(`helpGuide.${section.id}.intro`)" class="text-secondary text-sm leading-7">{{ t(`helpGuide.${section.id}.intro`) }}</p>
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
                  <thead class="bg-[var(--surface-2)] ">
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
            <template v-for="(example, index) in examples[section.id] || []" :key="example.label">
              <CodeBlock v-if="index === 0 || section.id === 'quick'" :label="example.label" :code="example.code" />
              <details v-else class="rounded-xl border p-4">
                <summary class="cursor-pointer text-sm font-medium focus-visible:outline focus-visible:outline-primary-500">{{ example.label }}</summary>
                <div class="mt-4"><CodeBlock :label="example.label" :code="example.code" /></div>
              </details>
            </template>
            <nav v-if="section.id === 'clients'" :aria-label="t('helpGuide.references')" class="space-y-2 text-sm">
              <p class="font-medium">{{ t('helpGuide.references') }}</p>
              <div class="flex flex-wrap gap-x-5 gap-y-2"><a v-for="reference in helpClientReferences" :key="reference.url" :href="reference.url" target="_blank" rel="noopener noreferrer" class="text-action underline underline-offset-4 dark:text-primary-300">{{ reference.label }}</a></div>
            </nav>
            <nav v-if="links[section.id]?.length" :aria-label="t('helpGuide.next')" class="flex flex-wrap gap-x-6 gap-y-3 text-sm">
              <RouterLink v-for="link in links[section.id]" :key="link.path" :to="link.path" class="font-medium text-action dark:text-primary-300 underline underline-offset-4">{{ t(link.key) }} →</RouterLink>
            </nav>
          </DocSection>
        </article>
        <aside
          class="hidden self-start xl:sticky xl:top-20 xl:block"
          :aria-label="isZh ? '本页章节目录' : 'On this page'"
        >
          <div class="rounded-xl border border-[var(--border)] bg-[var(--card)] p-3.5">
            <div class="mb-2.5 flex items-center justify-between gap-2">
              <p class="font-mono text-[11px] font-semibold uppercase tracking-widest text-secondary">
                {{ isZh ? '本页目录' : 'On this page' }}
              </p>
              <span class="font-mono text-[10px] tabular-nums text-secondary">
                {{ readingProgress }}%
              </span>
            </div>
            <div class="mb-3 h-1 w-full overflow-hidden rounded-full bg-[var(--surface-2)]">
              <div
                class="h-full bg-primary-500 transition-[width] duration-150"
                :style="{ width: `${readingProgress}%` }"
              />
            </div>
            <nav class="max-h-[calc(100vh-14rem)] space-y-1 overflow-y-auto pr-1">
              <a
                v-for="section in visibleSections"
                :key="`toc-${section.id}`"
                :href="`#${section.id}`"
                :aria-current="activeSection === section.id ? 'location' : undefined"
                class="block truncate rounded-md border-l-2 px-2.5 py-1.5 text-xs transition-colors"
                :class="
                  activeSection === section.id
                    ? 'border-primary-500 bg-primary-500/10 font-semibold text-action dark:text-primary-300'
                    : 'border-transparent text-secondary hover:bg-[var(--surface-2)] hover:text-[var(--foreground)]'
                "
              >
                {{ section.title }}
              </a>
            </nav>
          </div>
        </aside>
      </div>

      <button
        v-show="showBackToTop"
        type="button"
        class="fixed bottom-6 right-6 z-30 inline-flex items-center gap-1.5 rounded-full border border-[var(--border)] bg-[var(--card)] px-3.5 py-2 text-xs font-medium text-[var(--foreground)] shadow-lg transition-colors hover:border-primary-500 hover:text-action focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:text-primary-300"
        :aria-label="isZh ? '返回顶部' : 'Back to top'"
        @click="scrollToTop"
      >
        <span aria-hidden="true">↑</span>
        <span>{{ isZh ? '返回顶部' : 'Top' }}</span>
      </button>
    </div>
  </component>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { helpSectionIds as sectionIds, helpClientReferences } from '@/components/intro/helpSections'
import { guideExamples } from '@/components/intro/helpExamples'
import { publicRoot } from '@/components/intro/quickstart'
import AppLayout from '@/components/layout/AppLayout.vue'
import PublicShell from '@/components/layout/PublicShell.vue'
import DocSidebar from '@/components/intro/DocSidebar.vue'
import DocSection from '@/components/intro/DocSection.vue'
import CodeBlock from '@/components/intro/CodeBlock.vue'
import QuickstartGuide from '@/components/intro/QuickstartGuide.vue'
const { t, tm, te, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const { canUseBatchImage } = useBatchImageAccess()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const shellComponent = computed(() => (authStore.isAuthenticated ? AppLayout : PublicShell))

type HelpScenarioId = 'all' | 'quickstart' | 'claude-code' | 'codex' | 'cursor' | 'sdk'

interface HelpScenarioTab {
  id: HelpScenarioId
  labelZh: string
  labelEn: string
  sectionIds: readonly string[] | null
}

const scenarioTabs: readonly HelpScenarioTab[] = [
  { id: 'all', labelZh: '全部', labelEn: 'All', sectionIds: null },
  {
    id: 'quickstart',
    labelZh: '快速开始',
    labelEn: 'Quick Start',
    sectionIds: ['quick', 'keys', 'models', 'addresses', 'usage', 'billing', 'faq']
  },
  {
    id: 'claude-code',
    labelZh: 'Claude Code',
    labelEn: 'Claude Code',
    sectionIds: ['quick', 'keys', 'messages', 'streaming', 'tools', 'reasoning', 'cache', 'clients', 'cc-switch']
  },
  {
    id: 'codex',
    labelZh: 'Codex CLI',
    labelEn: 'Codex CLI',
    sectionIds: ['quick', 'keys', 'responses', 'streaming', 'tools', 'reasoning', 'clients', 'cc-switch']
  },
  {
    id: 'cursor',
    labelZh: 'Cursor',
    labelEn: 'Cursor',
    sectionIds: ['quick', 'keys', 'chat', 'models', 'streaming', 'tools', 'clients']
  },
  {
    id: 'sdk',
    labelZh: 'SDK 接入',
    labelEn: 'SDK & API',
    sectionIds: ['addresses', 'chat', 'messages', 'responses', 'gemini', 'retrieval', 'resources', 'images', 'async-images', 'media-jobs', 'audio', 'suppliers']
  }
]

const activeScenario = ref<HelpScenarioId>('all')
const showBackToTop = ref(false)
const readingProgress = ref(0)

const currentScenarioMeta = computed(
  () => scenarioTabs.find((entry) => entry.id === activeScenario.value) ?? scenarioTabs[0]
)

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
const visibleSections = computed(() => {
  const allowed = currentScenarioMeta.value.sectionIds
  if (!allowed) return sections.value
  const allowedSet = new Set(allowed)
  return sections.value.filter((section) => allowedSet.has(section.id))
})
const activeSection = ref<string>('quick')
let animationFrame = 0
function updateActiveSection() {
  animationFrame = 0
  const currentIds = visibleSections.value.map((section) => section.id)
  const visibleElements = currentIds
    .map((id) => document.getElementById(id))
    .filter((element): element is HTMLElement => Boolean(element))
  const preceding = visibleElements.filter((element) => element.getBoundingClientRect().top <= 160)
  activeSection.value = preceding[preceding.length - 1]?.id || currentIds[0] || 'quick'
  const scrollableHeight = Math.max(0, document.documentElement.scrollHeight - window.innerHeight)
  readingProgress.value = scrollableHeight > 0
    ? Math.min(100, Math.max(0, Math.round((window.scrollY / scrollableHeight) * 100)))
    : 0
  showBackToTop.value = window.scrollY > 400
  if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 8) {
    activeSection.value = currentIds[currentIds.length - 1] || 'quick'
  }
}
function selectScenario(scenarioId: HelpScenarioId) {
  activeScenario.value = scenarioId
  void nextTick(() => {
    const firstId = visibleSections.value[0]?.id
    if (firstId) activeSection.value = firstId
    updateActiveSection()
  })
}
function scrollToTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
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
  'async-images': [{ path: '/batch-image', key: 'nav.batchImage' }],
  resources: [{ path: '/batch-image', key: 'nav.batchImage' }],
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
  if (link.path === '/batch-image') return canUseBatchImage.value
  if (link.path === '/purchase' || link.path === '/orders') return isFeatureFlagEnabled(FeatureFlags.payment)
  if (link.path === '/subscriptions') return isFeatureFlagEnabled(FeatureFlags.subscription)
  return true
})])))
const examples = computed(() => guideExamples(apiRoot.value, appStore.cachedPublicSettings?.site_name?.trim() || appStore.siteName?.trim() || 'OmniHive'))
</script>
<style scoped>
.guide-document { background: var(--background); overflow-wrap: anywhere; }
.guide-document :deep(.text-secondary) { color: var(--muted-foreground); }
.guide-document :deep(.card) { background: var(--card); border: 1px solid var(--border); box-shadow: none; }
.guide-document :deep(table) { border-collapse: collapse; }
.guide-document :deep(:is(th, td)) { border-bottom: 1px solid var(--border); }
.guide-document :deep(thead) { background: var(--surface-2); }
.guide-document :deep(details) { border-color: var(--border); }
</style>
