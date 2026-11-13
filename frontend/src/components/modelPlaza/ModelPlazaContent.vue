<template>
  <div class="plaza-directory space-y-5">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div
      v-if="!embedded"
      class="flex flex-wrap items-end justify-between gap-4 border-b pb-5"
      style="border-color: var(--border, #e9e9ee)"
    >
      <div>
        <div class="mb-2 inline-flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.16em] opacity-65">
          <span class="pulse-dot pulse-ok"></span>
          <span>OMNIHIVE MODEL DIRECTORY</span>
        </div>
        <h1 class="text-2xl font-bold tracking-tight text-[var(--foreground)]  sm:text-3xl">
          {{ t('modelPlaza.title') }}
        </h1>
        <p class="mt-1.5 text-sm text-[var(--muted-foreground)] ">
          {{ t('modelPlaza.description') }}
        </p>
      </div>

      <!-- 顶部汇总统计条 (OpenRouter Models 风格) -->
      <div
        v-if="!loading && !error"
        class="flex flex-wrap items-center gap-6 rounded-xl border bg-[var(--card)] px-4 py-2.5 "
        style="border-color: var(--border, #e9e9ee)"
      >
        <div>
          <div class="text-[11px] font-medium opacity-55">可用分组</div>
          <div class="font-mono text-base font-bold tabular-nums">
            {{ filteredGroups.length }}<span class="text-xs font-normal opacity-50">/{{ totalGroupCount }}</span>
          </div>
        </div>
        <div class="h-6 w-px bg-[var(--surface-2)] "></div>
        <div>
          <div class="text-[11px] font-medium opacity-55">覆盖模型</div>
          <div class="font-mono text-base font-bold tabular-nums">{{ totalUniqueModels }}</div>
        </div>
        <div class="h-6 w-px bg-[var(--surface-2)] "></div>
        <div>
          <div class="text-[11px] font-medium opacity-55">最低倍率</div>
          <div class="font-mono text-base font-bold tabular-nums" style="color: var(--brand-text, #92610a)">
            {{ lowestRateLabel }}
          </div>
        </div>
      </div>
    </div>

    <!-- 后台内嵌形态下的紧凑汇总统计条 -->
    <div
      v-else-if="!loading && !error"
      class="flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-[var(--card)] px-4 py-2.5 text-xs "
      style="border-color: var(--border, #e9e9ee)"
    >
      <div class="flex items-center gap-2 font-medium">
        <span class="pulse-dot pulse-ok"></span>
        <span>蜂巢模型目录实时同步</span>
      </div>
      <div class="flex flex-wrap items-center gap-4 font-mono tabular-nums">
        <span>分组：<strong>{{ filteredGroups.length }}/{{ totalGroupCount }}</strong></span>
        <span>模型：<strong>{{ totalUniqueModels }}</strong></span>
        <span>最低倍率：<strong style="color: var(--brand-text, #92610a)">{{ lowestRateLabel }}</strong></span>
      </div>
    </div>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-xl border bg-[var(--card)] px-5 py-4 text-sm shadow-sm "
      style="border-color: var(--border, #e9e9ee)"
      v-html="descriptionHtml"
    ></div>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs text-[var(--muted-foreground)] "
      style="border-color: var(--border, #e9e9ee); background: var(--card, #ffffff)"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5 text-amber-500" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="rounded-2xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <!-- 筛选区:平台 → 分组 → 倍率 -->
      <PlazaFilterBar
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
      />

      <div class="directory-tabs flex gap-5 overflow-x-auto border-b" role="group" aria-label="模型分类">
        <button v-for="category in categories" :key="category.id" type="button" class="shrink-0 border-b-2 px-1 py-3 text-sm" :class="activeCategory === category.id ? 'border-[var(--brand)] text-[var(--brand-text)]' : 'border-transparent text-[var(--muted-foreground)]'" :aria-pressed="activeCategory === category.id" @click="activeCategory = category.id">
          {{ category.label }} <span class="ml-1 font-mono text-xs tabular-nums">{{ categoryCount(category.id) }}</span>
        </button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-[var(--muted-foreground)]"><strong class="font-mono text-[var(--foreground)]">{{ directory.length }}</strong> 个模型 · {{ visibleGroups.length }} 个分组</p>
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-xs">排序
            <select v-model="sortOrder" class="directory-select rounded-md border px-3 py-2 text-sm" aria-label="模型排序">
              <option value="rate">最低倍率</option><option value="name">模型名称</option><option value="coverage">覆盖分组数</option>
            </select>
          </label>
          <div class="inline-flex rounded-md border p-1" role="group" aria-label="目录视图">
            <button type="button" class="view-switch rounded px-3 py-1.5 text-xs" :aria-pressed="view === 'cards'" @click="view = 'cards'">目录卡片</button>
            <button type="button" class="view-switch rounded px-3 py-1.5 text-xs" :aria-pressed="view === 'groups'" @click="view = 'groups'">分组定价矩阵</button>
          </div>
        </div>
      </div>
      <p class="text-xs leading-6 text-[var(--muted-foreground)]">分类依据模型名称，仅用于检索；协议和能力以分组目录为准。卡片展示最低倍率分组的标准时段价格，完整阶梯、缓存与时段价格可展开查看。</p>
      <p v-if="feedback" role="status" class="text-sm text-[var(--brand-text)]">{{ feedback }}</p>
      <div v-if="directory.length && view === 'cards'" class="grid items-start gap-4 xl:grid-cols-2">
        <article v-for="entry in directory" :key="entry.id" class="directory-card min-w-0 rounded-xl border p-5">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <span class="font-mono text-[11px] text-[var(--muted-foreground)]">{{ entry.model.platform }} / {{ categoryLabel(entry.model.name) }}</span>
              <h2 class="mt-2 break-all text-lg font-semibold tracking-tight">{{ entry.model.name }}</h2>
            </div>
            <button type="button" class="favorite-button shrink-0 rounded-md border px-2.5 py-1.5" :aria-label="`${favorites.includes(entry.id) ? '取消收藏' : '收藏'} ${entry.model.name}`" :aria-pressed="favorites.includes(entry.id)" @click="toggleFavorite(entry.id)">{{ favorites.includes(entry.id) ? '★' : '☆' }}</button>
          </div>
          <div class="my-4 flex flex-wrap gap-2 text-xs">
            <span class="directory-chip rounded border px-2 py-1">{{ entry.offers.length }} 个分组</span>
            <span class="directory-chip rounded border px-2 py-1">{{ billingLabel(entry.model) }}</span>
            <span class="rounded border border-[var(--brand)] px-2 py-1 font-mono text-[var(--brand-text)]">{{ entry.rate }}x 起</span>
          </div>
          <div class="grid grid-cols-2 gap-4 border-y py-3 text-xs">
            <div><span class="text-[var(--muted-foreground)]">输入 / 请求</span><p class="mt-1 font-mono text-base tabular-nums">{{ priceLabel(entry, 'input') }}</p></div>
            <div><span class="text-[var(--muted-foreground)]">输出</span><p class="mt-1 font-mono text-base tabular-nums">{{ priceLabel(entry, 'output') }}</p></div>
          </div>
          <p class="mt-3 text-xs text-[var(--muted-foreground)]">价格分组：{{ entry.offers[0].group.name }}</p>
          <div class="mt-4 flex flex-wrap gap-2">
            <button type="button" class="directory-action" :aria-pressed="selected.includes(entry.id)" :disabled="selected.length >= 3 && !selected.includes(entry.id)" @click="toggleCompare(entry.id)">{{ selected.includes(entry.id) ? '移出对比' : '加入对比' }}</button>
            <button type="button" class="directory-action" @click="copyValue(entry.model.name, '模型 ID 已复制')">复制 ID</button>
          </div>
          <details class="mt-4 border-t pt-3">
            <summary class="cursor-pointer text-xs text-[var(--brand-text)]">查看全部分组与完整定价</summary>
            <div class="mt-4 space-y-3">
              <PlazaGroupSection v-for="offer in entry.offers" :key="offer.group.id" :group="{ ...offer.group, models: [offer.model] }" :favorites="favorites" :compared="selected" :compare-limit-reached="selected.length >= 3" @favorite="toggleFavorite" @compare="toggleCompare" />
            </div>
          </details>
        </article>
      </div>
      <div v-else-if="visibleGroups.length > 0" class="space-y-5">
        <PlazaGroupSection v-for="g in visibleGroups" :key="g.id" :group="g" :favorites="favorites" :compared="selected" :compare-limit-reached="selected.length >= 3" :model-order="directory.map(entry => entry.id)" @favorite="toggleFavorite" @compare="toggleCompare" />
      </div>
      <div
        v-else
        class="rounded-2xl border border-dashed border-[var(--border)] px-5 py-12 text-center text-sm text-[var(--muted-foreground)]  "
      >
        {{ searchActive || activeCategory !== 'all' ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
        <button type="button" class="directory-action mx-auto mt-4 block" @click="resetDirectory">重置全部筛选</button>
      </div>
      <section v-if="selectedEntries.length" class="comparison-tray sticky bottom-4 z-20 rounded-xl border p-4" aria-label="模型对比">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <strong class="text-sm">模型对比 <span class="font-mono">{{ selected.length }}/3</span></strong>
            <button v-for="entry in selectedEntries" :key="entry.id" type="button" class="directory-chip max-w-56 truncate rounded border px-2 py-1 text-xs" :aria-label="`移除 ${entry.model.name}`" @click="toggleCompare(entry.id)">{{ entry.model.name }} ×</button>
          </div>
          <div class="flex gap-2">
            <button type="button" class="directory-action" @click="selected = []; compareOpen = false">清空</button>
            <button type="button" class="directory-action primary-action" :aria-expanded="compareOpen" aria-controls="model-comparison" @click="compareOpen = !compareOpen">{{ compareOpen ? '收起对比' : '展开对比' }}</button>
          </div>
        </div>
        <div v-if="compareOpen" id="model-comparison" class="mt-4 max-h-[65vh] overflow-auto" role="region" aria-label="模型规格对比" tabindex="0" @keydown.esc="compareOpen = false">
          <table class="w-full min-w-[640px] border-collapse text-left text-sm">
            <caption class="mb-3 text-left text-xs text-[var(--muted-foreground)]">按最低倍率分组比较标准价格；不同分组可能采用不同计费模式，请查看完整定价。</caption>
            <thead><tr><th scope="col" class="compare-cell">规格</th><th v-for="entry in selectedEntries" :key="entry.id" scope="col" class="compare-cell break-all">{{ entry.model.name }}</th></tr></thead>
            <tbody>
              <tr><th scope="row" class="compare-cell">所属分组</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell">{{ entry.offers.map(offer => offer.group.name).join(' / ') }}</td></tr>
              <tr><th scope="row" class="compare-cell">最低生效倍率</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell font-mono tabular-nums">{{ entry.rate }}x</td></tr>
              <tr><th scope="row" class="compare-cell">计费模式</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell">{{ [...new Set(entry.offers.map(offer => billingLabel(offer.model)))].join(' / ') }}</td></tr>
              <tr><th scope="row" class="compare-cell">输入 / 请求</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell font-mono tabular-nums">{{ priceLabel(entry, 'input') }}</td></tr>
              <tr><th scope="row" class="compare-cell">输出</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell font-mono tabular-nums">{{ priceLabel(entry, 'output') }}</td></tr>
              <tr><th scope="row" class="compare-cell">接入代码</th><td v-for="entry in selectedEntries" :key="entry.id" class="compare-cell"><div class="flex flex-wrap gap-2"><button type="button" class="directory-action" @click="copyValue(entry.model.name, '模型 ID 已复制')">复制 ID</button><template v-if="['text', 'vision'].includes(categoryOf(entry.model.name))"><button type="button" class="directory-action" :disabled="!apiRoot" @click="copyExample(entry, 'opencode')">OpenCode v2</button><button type="button" class="directory-action" :disabled="!apiRoot" @click="copyExample(entry, 'openai')">OpenAI SDK</button></template><span v-else class="text-xs text-[var(--muted-foreground)]">请按接入指南选择媒体或检索协议</span></div></td></tr>
            </tbody>
          </table>
          <p v-if="!apiRoot" class="mt-3 text-xs text-[var(--muted-foreground)]">本站尚未配置公开网关地址，配置完成后可复制调用代码。</p>
          <p class="mt-3 text-xs text-[var(--muted-foreground)]">代码使用环境变量读取密钥；OpenCode 和 OpenAI SDK 示例适用于支持 Chat Completions 的文本模型。</p>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { publicRoot } from '@/components/intro/quickstart'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')
const view = ref<'cards' | 'groups'>('cards')
const sortOrder = ref<'rate' | 'name' | 'coverage'>('rate')
const categories = [
  { id: 'all', label: '全部' }, { id: 'text', label: '文本 / 推理' },
  { id: 'vision', label: '视觉 / 多模态' }, { id: 'media', label: '图像 / 视频 / 音频' },
  { id: 'embedding', label: '向量 / 重排' }, { id: 'favorites', label: '★ 已收藏' }
]
const initialCategory = new URLSearchParams(window.location.search).get('modality')
const activeCategory = ref(initialCategory === 'image' ? 'media' : categories.some(item => item.id === initialCategory) ? initialCategory! : 'all')
const favorites = ref<string[]>(readFavorites())
const selected = ref<string[]>([])
const compareOpen = ref(false)
const feedback = ref('')
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const apiRoot = computed(() => {
  try { return appStore.apiBaseUrl?.trim() ? publicRoot(appStore.apiBaseUrl, '') : '' } catch { return '' }
})
const modelIdentity = (model: PlazaModel) => JSON.stringify([model.platform, model.name])
function readFavorites(): string[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem('omnihive:model-favorites') || '[]')
    return Array.isArray(stored) ? [...new Set(stored.filter((item): item is string => typeof item === 'string'))] : []
  } catch { return [] }
}
function toggleFavorite(id: string) {
  favorites.value = favorites.value.includes(id) ? favorites.value.filter(item => item !== id) : [...favorites.value, id]
  try { localStorage.setItem('omnihive:model-favorites', JSON.stringify(favorites.value)) }
  catch { feedback.value = '浏览器存储不可用，收藏已保留至本次页面关闭。' }
}
function toggleCompare(id: string) {
  if (selected.value.includes(id)) selected.value = selected.value.filter(item => item !== id)
  else if (selected.value.length < 3) selected.value.push(id)
  else feedback.value = '最多同时对比 3 个模型，请先移除一个模型。'
}
function categoryOf(name: string) {
  if (/embedding|rerank|bge/i.test(name)) return 'embedding'
  if (/-image|flux|sora|video|tts|whisper|dall-e|stable-diffusion|audio|imagen|veo/i.test(name)) return 'media'
  if (/vl|vision|gemini|gpt-4o|claude/i.test(name)) return 'vision'
  return 'text'
}
function categoryLabel(name: string) { return categories.find(item => item.id === categoryOf(name))?.label }
function matchesCategory(entry: DirectoryEntry, category: string) {
  return category === 'all' || (category === 'favorites' ? favorites.value.includes(entry.id) : categoryOf(entry.model.name) === category)
}
interface DirectoryEntry { id: string; model: PlazaModel; rate: number; offers: { group: ModelPlazaGroup; model: PlazaModel }[] }
function modelRate(group: ModelPlazaGroup, model: PlazaModel) {
  if (model.pricing?.billing_mode === 'image' && group.image_rate_independent) return group.image_rate_multiplier ?? 1
  if (model.pricing?.billing_mode === 'video' && group.video_rate_independent) return Math.max(0, group.video_rate_multiplier ?? 1)
  return effectiveRate(group)
}
function buildDirectory(groups: ModelPlazaGroup[]): DirectoryEntry[] {
  const entries = new Map<string, DirectoryEntry>()
  for (const group of groups) for (const model of group.models) {
    const id = modelIdentity(model)
    let entry = entries.get(id)
    if (!entry) { entry = { id, model, rate: modelRate(group, model), offers: [] }; entries.set(id, entry) }
    entry.offers.push({ group, model })
  }
  for (const entry of entries.values()) {
    entry.offers.sort((left, right) => modelRate(left.group, left.model) - modelRate(right.group, right.model) || left.group.name.localeCompare(right.group.name))
    entry.model = entry.offers[0].model
    entry.rate = modelRate(entry.offers[0].group, entry.model)
  }
  return [...entries.values()]
}
const allEntries = computed(() => buildDirectory(props.response?.groups ?? []))
const filteredEntries = computed(() => buildDirectory(filteredGroups.value))
const directory = computed(() => filteredEntries.value.filter(entry => matchesCategory(entry, activeCategory.value)).sort((left, right) => {
  if (sortOrder.value === 'name') return left.model.name.localeCompare(right.model.name)
  if (sortOrder.value === 'coverage') return right.offers.length - left.offers.length || left.model.name.localeCompare(right.model.name)
  return left.rate - right.rate || left.model.name.localeCompare(right.model.name)
}))
const visibleGroups = computed(() => {
  const identities = new Set(directory.value.map(entry => entry.id))
  const groups = filteredGroups.value.map(group => ({ ...group, models: group.models.filter(model => identities.has(modelIdentity(model))) })).filter(group => group.models.length)
  if (sortOrder.value !== 'rate') {
    const order = new Map(directory.value.map((entry, index) => [entry.id, index]))
    groups.sort((left, right) => Math.min(...left.models.map(model => order.get(modelIdentity(model))!)) - Math.min(...right.models.map(model => order.get(modelIdentity(model))!)))
  }
  return groups
})
const selectedEntries = computed(() => selected.value.flatMap(id => allEntries.value.filter(entry => entry.id === id)))
watch(allEntries, entries => { selected.value = selected.value.filter(id => entries.some(entry => entry.id === id)) })
function categoryCount(category: string) { return filteredEntries.value.filter(entry => matchesCategory(entry, category)).length }
function billingLabel(model: PlazaModel) {
  const mode = model.pricing?.billing_mode
  if (!model.pricing) return '计费待配置'
  return mode === 'image' ? '按张' : mode === 'video' ? '按秒' : mode === 'per_request' ? '按次' : '按量 Token'
}
function priceLabel(entry: DirectoryEntry, direction: 'input' | 'output') {
  const pricing = entry.model.pricing
  if (!pricing) return '未配置'
  if (pricing.intervals?.length) return '阶梯定价 · 查看详情'
  const token = !pricing.billing_mode || pricing.billing_mode === 'token'
  if (!token && direction === 'output') return '—'
  const value = token ? (direction === 'input' ? pricing.input_price : pricing.output_price) : pricing.per_request_price
  if (value == null) return '未配置'
  const price = value * entry.rate * (token ? 1_000_000 : 1)
  return `$${price.toLocaleString('en-US', { maximumFractionDigits: 8 })} ${token ? '/ 1M tokens' : pricing.billing_mode === 'image' ? '/ 张' : pricing.billing_mode === 'video' ? '/ 秒' : '/ 次'}`
}
async function copyValue(value: string, success: string) {
  try { feedback.value = await copyToClipboard(value) ? success : '复制失败，请稍后重试。' }
  catch { feedback.value = '复制失败，请稍后重试。' }
}
function copyExample(entry: DirectoryEntry, client: 'opencode' | 'openai') {
  if (!apiRoot.value) return
  const model = entry.model.name
  const code = client === 'opencode'
    ? JSON.stringify({ $schema: 'https://opencode.ai/config.json', providers: { omnihive: { package: '@opencode/ai/providers/openai-compatible', name: 'OmniHive', env: ['OMNIHIVE_API_KEY'], settings: { baseURL: `${apiRoot.value}/v1` }, models: { [model]: { name: model } } } }, model: `omnihive/${model}` }, null, 2)
    : `import os\nfrom openai import OpenAI\n\nclient = OpenAI(base_url=${JSON.stringify(`${apiRoot.value}/v1`)}, api_key=os.environ["OMNIHIVE_API_KEY"])\nresponse = client.chat.completions.create(\n    model=${JSON.stringify(model)},\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.choices[0].message.content)`
  void copyValue(code, `${client === 'opencode' ? 'OpenCode' : 'OpenAI SDK'} 配置已复制`)
}
function resetDirectory() {
  selectedPlatform.value = 'all'
  selectedGroupId.value = 'all'
  selectedRate.value = 'all'
  searchQuery.value = ''
  activeCategory.value = 'all'
}

const searchActive = computed(() => searchQuery.value.trim() !== '')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const platforms = computed(() =>
  [...new Set((props.response?.groups ?? []).map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  (props.response?.groups ?? []).map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: effectiveRate(g)
  }))
)

/** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
const rates = computed(() =>
  [...new Set((props.response?.groups ?? []).map(effectiveRate))].sort((a, b) => a - b)
)

const totalGroupCount = computed(() => (props.response?.groups ?? []).length)

const totalUniqueModels = computed(() => {
  const names = new Set<string>()
  for (const g of props.response?.groups ?? []) {
    for (const m of g.models ?? []) {
      if (m.name) names.add(m.name)
    }
  }
  return names.size
})

const lowestRateLabel = computed(() => {
  if (rates.value.length === 0) return '—'
  return `${rates.value[0]}x`
})

/** 数据刷新后选中的倍率可能不复存在,重置为全部。 */
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

const filteredGroups = computed(() => {
  let groups = props.response?.groups ?? []
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => effectiveRate(g) === selectedRate.value)
  }
  // 模型名搜索:分组内只留命中的模型,整组无命中则隐藏该分组。
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => ({ ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }))
      .filter((g) => g.models.length > 0)
  }
  // 专属倍率会改变生效值,不能只依赖后端按默认倍率的排序。
  return [...groups].sort(
    (a, b) => effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name)
  )
})
</script>

<style scoped>
.plaza-directory { color: var(--foreground); }
.plaza-directory :is(div, section, article, th, td, details) { border-color: var(--border); }
.directory-card, .directory-select { background: var(--card); color: var(--foreground); }
.directory-card { transition: border-color .15s; }
.directory-card:hover { border-color: var(--brand); }
.directory-chip { background: var(--surface-2); }
.directory-action { border: 1px solid var(--border); border-radius: 6px; padding: 6px 10px; font-size: 12px; background: var(--card); color: var(--foreground); }
.directory-action:hover:not(:disabled), .favorite-button:hover { border-color: var(--brand); color: var(--brand-text); }
.view-switch[aria-pressed="true"], .primary-action { background: var(--primary); color: var(--primary-foreground); }
.favorite-button[aria-pressed="true"] { color: var(--brand-text); border-color: var(--brand); }
.directory-action:disabled { opacity: .45; cursor: not-allowed; }
.comparison-tray { background: var(--card); box-shadow: 0 10px 40px #0002; }
.compare-cell { padding: 12px; border: 1px solid var(--border); vertical-align: top; }
.compare-cell:first-child { width: 130px; background: var(--surface-2); }
.plaza-directory :is(button, select, input, summary, [tabindex]):focus-visible { outline: 2px solid var(--brand); outline-offset: 3px; }
@media (prefers-reduced-motion: reduce) { .directory-card { transition: none; } }
.pulse-dot {
  width: 7px;
  height: 7px;
  border-radius: 9999px;
  display: inline-block;
  flex: none;
}

.pulse-ok {
  background: var(--ok, #16a34a);
  color: var(--ok, #16a34a);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok, #16a34a) 22%, transparent);
}

.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-[var(--foreground)] first:mt-0 ;
}

.plaza-description :deep(p) {
  @apply mb-2 text-[var(--foreground)] last:mb-0 ;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-[var(--foreground)] ;
}

.plaza-description :deep(code) {
  @apply rounded bg-[var(--surface-2)] px-1.5 py-0.5 font-mono text-xs ;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-[var(--border)] pl-3 text-[var(--muted-foreground)]  ;
}
</style>
