<template>
  <component :is="shellComponent" v-bind="shellProps">
    <div class="guide-document w-full text-oh-primary">
      <header class="mb-8 max-w-4xl border-b border-oh-default pb-6">
        <p class="mb-3 font-mono text-xs font-semibold uppercase tracking-widest text-oh-accent-text">{{ t('helpGuide.eyebrow') }}</p>
        <h1 class="text-3xl font-bold tracking-tight md:text-4xl">{{ t('helpGuide.title') }}</h1>
        <p class="mt-4 text-lg leading-8 text-oh-secondary">{{ t('helpGuide.subtitle') }}</p>

        <div
          v-if="!authStore.isSimpleMode"
          data-testid="docs-compatibility-banner"
          class="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-default bg-oh-surface px-4 py-3 text-xs"
        >
          <span class="text-oh-secondary">
            {{ tr('compatBannerText') }}
            <code class="mx-1 rounded-oh-xs border border-oh-subtle bg-oh-subtle px-1.5 py-0.5 font-mono text-[11px] text-oh-primary">.ohplugin v2</code>
          </span>
          <RouterLink
            to="/compatibility"
            data-testid="docs-compatibility-link"
            class="oh-focus-ring shrink-0 font-mono text-xs font-semibold text-oh-accent-text underline underline-offset-4 hover:text-oh-primary"
          >
            {{ tr('compatBannerCta') }}
          </RouterLink>
        </div>

        <nav
          class="mt-6 flex flex-wrap items-center gap-2"
          :aria-label="tr('scenarioNavAria')"
        >
          <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ tr('scenariosLabel') }}
          </span>
          <button
            v-for="scenario in scenarioTabs"
            :key="scenario.id"
            type="button"
            class="oh-focus-ring rounded-full border px-3 py-1 text-xs font-medium transition-colors"
            :class="
              activeScenario === scenario.id
                ? 'border-oh-accent bg-oh-selected text-oh-accent-text'
                : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary'
            "
            :aria-pressed="activeScenario === scenario.id"
            @click="selectScenario(scenario.id)"
          >
            {{ tr(scenario.labelKey) }}
          </button>
        </nav>
      </header>
      <div class="grid items-start gap-8 lg:grid-cols-[220px_minmax(0,1fr)] xl:grid-cols-[220px_minmax(0,1fr)_220px] lg:gap-10">
        <DocSidebar :sections="visibleSections" :active="activeSection" />
        <article class="min-w-0 space-y-12 pb-24">
          <div
            v-if="activeScenario !== 'all'"
            class="flex flex-wrap items-center justify-between gap-3 rounded-oh-md border border-oh-default bg-oh-surface px-4 py-3 text-xs"
          >
            <span class="text-oh-secondary">
              {{ scenarioSummaryText }}
            </span>
            <button
              type="button"
              class="oh-focus-ring font-medium text-oh-accent-text underline underline-offset-4"
              @click="selectScenario('all')"
            >
              {{ tr('showAllChapters') }}
            </button>
          </div>
          <div class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-5 text-sm leading-7 text-oh-secondary"><p>{{ t('helpGuide.placeholder') }}</p><p>{{ t('helpGuide.capability') }}</p></div>
          <p v-if="!apiRoot" class="rounded-oh-md border border-oh-default border-l-4 border-l-[var(--oh-warn-fg)] bg-oh-surface p-5 text-sm leading-7 text-[var(--oh-warn-fg)]" role="alert">
            {{ t('helpGuide.apiBaseMissing') }}
          </p>
          <DocSection v-for="section in visibleSections" :id="section.id" :key="section.id" :title="section.title">
            <p v-if="te(`helpGuide.${section.id}.endpoint`)" class="break-words rounded-oh-sm bg-oh-subtle px-3 py-2 font-mono text-xs leading-6 text-oh-primary">{{ t(`helpGuide.${section.id}.endpoint`) }}</p>
            <p v-if="te(`helpGuide.${section.id}.intro`)" class="text-sm leading-7 text-oh-secondary">{{ t(`helpGuide.${section.id}.intro`) }}</p>
            <QuickstartGuide v-if="section.id === 'quick' && authStore.user" />
            <div
              v-if="section.id === 'quick' && apiRoot && activeProtocolQuickstart"
              data-testid="docs-protocol-quickstart"
              class="space-y-3 rounded-oh-md border border-oh-default bg-oh-surface p-4"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <span class="font-mono text-xs font-semibold uppercase tracking-wider text-oh-muted">
                  {{ tr('wireQuickstartTitle') }}
                </span>
                <div class="flex flex-wrap gap-1.5" role="tablist" :aria-label="tr('wireQuickstartTitle')">
                  <button
                    v-for="proto in protocolQuickstartTabs"
                    :key="proto.id"
                    type="button"
                    role="tab"
                    :aria-selected="selectedQuickProtocol === proto.id"
                    class="oh-focus-ring rounded-oh-xs border px-2.5 py-1 font-mono text-[11px] transition-colors"
                    :class="
                      selectedQuickProtocol === proto.id
                        ? 'border-oh-accent bg-oh-selected font-semibold text-oh-accent-text'
                        : 'border-oh-default bg-oh-subtle text-oh-secondary hover:text-oh-primary'
                    "
                    @click="selectedQuickProtocol = proto.id"
                  >
                    {{ proto.label }}
                  </button>
                </div>
              </div>
              <CodeBlock :label="activeProtocolQuickstart.label" :code="activeProtocolQuickstart.code" />
            </div>
            <ol v-if="section.steps.length" class="list-decimal space-y-4 pl-6 marker:font-semibold marker:text-oh-accent-text">
              <li v-for="step in section.steps" :key="step" class="pl-2">
                <h3 class="font-semibold">{{ t(`helpGuide.${section.id}.steps.${step}.title`) }}</h3>
                <p class="mt-1 text-sm leading-7 text-oh-secondary">{{ t(`helpGuide.${section.id}.steps.${step}.body`) }}</p>
              </li>
            </ol>
            <div v-for="table in section.tables" :key="table.id" class="space-y-3">
              <h3 :id="`${section.id}-${table.id}-title`" class="font-semibold">{{ t(`${table.path}.title`) }}</h3>
              <div class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface" role="region" tabindex="0" :aria-labelledby="`${section.id}-${table.id}-title`">
                <table class="w-full min-w-[640px] text-left text-sm leading-7" :aria-labelledby="`${section.id}-${table.id}-title`">
                  <thead class="bg-oh-subtle">
                    <tr><th v-for="column in table.columns" :key="column" scope="col" class="whitespace-nowrap px-4 py-3">{{ t(`${table.path}.columns.${column}`) }}</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="row in table.rows" :key="row" class="border-t border-oh-subtle align-top">
                      <th scope="row" class="px-4 py-3 font-medium">{{ t(`${table.path}.rows.${row}.${table.columns[0]}`) }}</th>
                      <td v-for="column in table.columns.slice(1)" :key="column" class="min-w-40 whitespace-pre-line break-words px-4 py-3 text-oh-secondary">{{ t(`${table.path}.rows.${row}.${column}`) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <ul v-if="section.notes.length" class="list-disc space-y-2 pl-5 text-sm leading-7 text-oh-secondary">
              <li v-for="note in section.notes" :key="note">{{ t(`helpGuide.${section.id}.notes.${note}`) }}</li>
            </ul>
            <template v-for="(example, index) in examples[section.id] || []" :key="example.label">
              <CodeBlock v-if="index === 0 || section.id === 'quick'" :label="example.label" :code="example.code" />
              <details v-else class="rounded-oh-md border border-oh-default bg-oh-surface p-4">
                <summary class="oh-focus-ring cursor-pointer text-sm font-medium">{{ example.label }}</summary>
                <div class="mt-4"><CodeBlock :label="example.label" :code="example.code" /></div>
              </details>
            </template>
            <nav v-if="section.id === 'clients'" :aria-label="t('helpGuide.references')" class="space-y-2 text-sm">
              <p class="font-medium">{{ t('helpGuide.references') }}</p>
              <div class="flex flex-wrap gap-x-5 gap-y-2"><a v-for="reference in helpClientReferences" :key="reference.url" :href="reference.url" target="_blank" rel="noopener noreferrer" class="text-oh-accent-text underline underline-offset-4">{{ reference.label }}</a></div>
            </nav>
            <nav v-if="links[section.id]?.length" :aria-label="t('helpGuide.next')" class="flex flex-wrap gap-x-6 gap-y-3 text-sm">
              <RouterLink v-for="link in links[section.id]" :key="link.path" :to="link.path" class="font-medium text-oh-accent-text underline underline-offset-4">{{ t(link.key) }} →</RouterLink>
            </nav>
          </DocSection>
          <section id="support" class="scroll-mt-24 pt-2">
            <HumanSupportCard />
          </section>
        </article>
        <aside
          class="hidden self-start xl:sticky xl:top-20 xl:block"
          :aria-label="tr('onThisPageAria')"
        >
          <div class="rounded-oh-md border border-oh-default bg-oh-surface p-3.5">
            <div class="mb-2.5 flex items-center justify-between gap-2">
              <p class="font-mono text-[11px] font-semibold uppercase tracking-widest text-oh-muted">
                {{ tr('onThisPageLabel') }}
              </p>
              <span class="font-mono text-[10px] tabular-nums text-oh-muted">
                {{ readingProgress }}%
              </span>
            </div>
            <div class="mb-3 h-1 w-full overflow-hidden rounded-full bg-oh-subtle">
              <div
                class="h-full bg-oh-accent transition-[width] duration-150"
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
                    ? 'border-oh-accent bg-oh-selected font-semibold text-oh-accent-text'
                    : 'border-transparent text-oh-secondary hover:bg-oh-hover hover:text-oh-primary'
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
        class="oh-focus-ring fixed bottom-6 right-6 z-30 inline-flex items-center gap-1.5 rounded-full border border-oh-default bg-oh-surface px-3.5 py-2 text-xs font-medium text-oh-primary shadow-oh-md transition-colors hover:border-oh-accent hover:text-oh-accent-text"
        :aria-label="tr('backToTopAria')"
        @click="scrollToTop"
      >
        <span aria-hidden="true">↑</span>
        <span>{{ tr('backToTopShort') }}</span>
      </button>
    </div>
  </component>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { helpSectionIds as sectionIds, helpClientReferences } from '@/components/intro/helpSections'
import { guideExamples } from '@/components/intro/helpExamples'
import { publicRoot, requestExample } from '@/components/intro/quickstart'
import AppLayout from '@/components/layout/AppLayout.vue'
import PublicShell from '@/components/layout/PublicShell.vue'
import DocSidebar from '@/components/intro/DocSidebar.vue'
import DocSection from '@/components/intro/DocSection.vue'
import CodeBlock from '@/components/intro/CodeBlock.vue'
import QuickstartGuide from '@/components/intro/QuickstartGuide.vue'
import HumanSupportCard from '@/components/intro/HumanSupportCard.vue'

const GUIDE_LOCAL_MESSAGES = {
  en: {
    scenarioNavAria: 'Filter documentation by scenario',
    scenariosLabel: 'Scenarios',
    scenarioAll: 'All',
    scenarioQuickstart: 'Quick Start',
    scenarioClaudeCode: 'Claude Code',
    scenarioCodex: 'Codex CLI',
    scenarioCursor: 'Cursor',
    scenarioSdk: 'SDK & API',
    showAllChapters: 'Show all chapters',
    showingChaptersPrefix: 'Showing "',
    showingChaptersMid: '" chapters (',
    showingChaptersJoin: ' of ',
    showingChaptersSuffix: ')',
    onThisPageAria: 'On this page',
    onThisPageLabel: 'On this page',
    backToTopAria: 'Back to top',
    backToTopShort: 'Top',
    compatBannerText: 'Wire protocol compatibility matrix, release checksum policy, and plugin migration guide:',
    compatBannerCta: 'Updates & Compatibility →',
    wireQuickstartTitle: 'Multi-Protocol Wire Quickstart'
  },
  zh: {
    scenarioNavAria: '按接入场景筛选文档',
    scenariosLabel: '快捷场景',
    scenarioAll: '全部',
    scenarioQuickstart: '快速开始',
    scenarioClaudeCode: 'Claude Code',
    scenarioCodex: 'Codex CLI',
    scenarioCursor: 'Cursor',
    scenarioSdk: 'SDK 接入',
    showAllChapters: '查看完整手册',
    showingChaptersPrefix: '当前仅展示「',
    showingChaptersMid: '」相关章节（',
    showingChaptersJoin: ' / ',
    showingChaptersSuffix: ' 章）',
    onThisPageAria: '本页章节目录',
    onThisPageLabel: '本页目录',
    backToTopAria: '返回顶部',
    backToTopShort: '返回顶部',
    compatBannerText: '查阅 API 线级协议兼容矩阵、制品校验回滚保障与插件规范迁移说明：',
    compatBannerCta: '更新与兼容性 →',
    wireQuickstartTitle: '四协议直连快速切换'
  }
} as const

type GuideLocalKey = keyof typeof GUIDE_LOCAL_MESSAGES.en

function toMessageFunctions<T extends Record<string, string>>(dict: T) {
  return Object.fromEntries(
    Object.entries(dict).map(([k, v]) => [k, () => v])
  ) as Record<keyof T, () => string>
}

const route = useRoute()
const { t, tm, te, locale } = useI18n({ useScope: 'global' })
const { t: tLocal } = useI18n({
  useScope: 'local',
  messages: {
    en: toMessageFunctions(GUIDE_LOCAL_MESSAGES.en),
    zh: toMessageFunctions(GUIDE_LOCAL_MESSAGES.zh),
    'zh-CN': toMessageFunctions(GUIDE_LOCAL_MESSAGES.zh)
  }
})
const authStore = useAuthStore()
const appStore = useAppStore()
const { canUseBatchImage } = useBatchImageAccess()

function tr(key: GuideLocalKey): string {
  const translated = tLocal(key)
  if (translated && translated !== key) return translated
  const active = String(locale?.value || 'en').toLowerCase()
  const dict = active.startsWith('zh') ? GUIDE_LOCAL_MESSAGES.zh : GUIDE_LOCAL_MESSAGES.en
  return dict[key]
}

const useDirectPublicShell = computed(
  () =>
    route?.meta?.shellTier === 'public' ||
    route?.path === '/docs' ||
    !authStore.isAuthenticated
)
const shellComponent = computed(() => (useDirectPublicShell.value ? PublicShell : AppLayout))
const shellProps = computed(() =>
  useDirectPublicShell.value ? {} : { tier: 'public' as const }
)

type HelpScenarioId = 'all' | 'quickstart' | 'claude-code' | 'codex' | 'cursor' | 'sdk'
type QuickstartProtocolId = 'chat/completions' | 'responses' | 'messages' | 'gemini'

interface HelpScenarioTab {
  id: HelpScenarioId
  labelKey: GuideLocalKey
  sectionIds: readonly string[] | null
}

const scenarioTabs: readonly HelpScenarioTab[] = [
  { id: 'all', labelKey: 'scenarioAll', sectionIds: null },
  {
    id: 'quickstart',
    labelKey: 'scenarioQuickstart',
    sectionIds: ['quick', 'keys', 'models', 'addresses', 'usage', 'billing', 'faq']
  },
  {
    id: 'claude-code',
    labelKey: 'scenarioClaudeCode',
    sectionIds: ['quick', 'keys', 'messages', 'streaming', 'tools', 'reasoning', 'cache', 'clients', 'cc-switch']
  },
  {
    id: 'codex',
    labelKey: 'scenarioCodex',
    sectionIds: ['quick', 'keys', 'responses', 'streaming', 'tools', 'reasoning', 'clients', 'cc-switch']
  },
  {
    id: 'cursor',
    labelKey: 'scenarioCursor',
    sectionIds: ['quick', 'keys', 'chat', 'models', 'streaming', 'tools', 'clients']
  },
  {
    id: 'sdk',
    labelKey: 'scenarioSdk',
    sectionIds: ['addresses', 'clients', 'chat', 'messages', 'responses', 'gemini', 'retrieval', 'resources', 'images', 'async-images', 'media-jobs', 'audio', 'suppliers', 'faq']
  }
]

const protocolQuickstartTabs: readonly {
  id: QuickstartProtocolId
  label: string
  modelPlaceholder: string
}[] = [
  { id: 'chat/completions', label: 'OpenAI Chat (/v1/chat/completions)', modelPlaceholder: 'MODEL_ID_FROM_LIST' },
  { id: 'responses', label: 'OpenAI Responses (/v1/responses)', modelPlaceholder: 'RESPONSES_MODEL_ID_FROM_LIST' },
  { id: 'messages', label: 'Anthropic Messages (/v1/messages)', modelPlaceholder: 'MESSAGES_MODEL_ID_FROM_LIST' },
  { id: 'gemini', label: 'Gemini v1beta (:generateContent)', modelPlaceholder: 'GOOGLE_MODEL_ID_FROM_LIST' }
]

const activeScenario = ref<HelpScenarioId>('all')
const selectedQuickProtocol = ref<QuickstartProtocolId>('chat/completions')
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

const scenarioSummaryText = computed(() => {
  const label = tr(currentScenarioMeta.value.labelKey)
  return `${tr('showingChaptersPrefix')}${label}${tr('showingChaptersMid')}${visibleSections.value.length}${tr('showingChaptersJoin')}${sections.value.length}${tr('showingChaptersSuffix')}`
})

const activeProtocolQuickstart = computed(() => {
  if (!apiRoot.value) return null
  const tab =
    protocolQuickstartTabs.find((entry) => entry.id === selectedQuickProtocol.value) ??
    protocolQuickstartTabs[0]
  return requestExample(apiRoot.value, tab.modelPlaceholder, tab.id).examples[0] ?? null
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
.guide-document { overflow-wrap: anywhere; }
.guide-document :deep(table) { border-collapse: collapse; }
</style>
