<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import PublicShell from '@/components/layout/PublicShell.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ErrorState from '@/components/ui/ErrorState.vue'
import Input from '@/components/ui/Input.vue'
import SegmentedControl from '@/components/ui/SegmentedControl.vue'
import Tooltip from '@/components/ui/Tooltip.vue'
import { useAuthStore } from '@/stores/auth'
import {
  formatContextTokens,
  formatPublicTimestamp,
  publicTokenPrice,
  publicValue
} from '@/features/public/format'
import { listPublicModels, type ListParams, type PublicModelSummary } from '@/features/public/api'

type ViewMode = 'table' | 'cards'

const { locale } = useI18n()
const isZh = computed(() => String(locale?.value ?? '').startsWith('zh'))
const router = useRouter()
const authStore = useAuthStore()

const models = ref<PublicModelSummary[]>([])
const nextCursor = ref<string | null>(null)
const asOf = ref<string | null>(null)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref<unknown>(null)
const query = ref('')
const sort = ref<NonNullable<ListParams['sort']>>('price_input')
const viewMode = ref<ViewMode>('table')
const selectedProviders = ref<string[]>([])
const selectedModalities = ref<string[]>([])
const providerOptions = ref<string[]>([])
const searchContainerRef = ref<HTMLElement | null>(null)
let requestSeq = 0
let appliedQuery = ''

const modalityChips = computed(() => [
  { value: 'text', label: isZh.value ? '文本 Text' : 'Text' },
  { value: 'image', label: isZh.value ? '视觉 Image' : 'Image' },
  { value: 'audio', label: isZh.value ? '音频 Audio' : 'Audio' },
  { value: 'video', label: isZh.value ? '视频 Video' : 'Video' }
])

const activeFilters = computed(
  () =>
    selectedProviders.value.length > 0 ||
    selectedModalities.value.length > 0 ||
    query.value.trim().length > 0
)

async function load(reset = true): Promise<void> {
  if (!reset && (loading.value || loadingMore.value || !nextCursor.value)) return
  const current = ++requestSeq
  if (reset) {
    appliedQuery = query.value.trim()
    loading.value = true
    loadingMore.value = false
    error.value = null
  } else {
    loadingMore.value = true
  }
  try {
    const page = await listPublicModels({
      q: appliedQuery || undefined,
      provider: selectedProviders.value.length ? selectedProviders.value : undefined,
      modality: selectedModalities.value.length ? selectedModalities.value : undefined,
      sort: sort.value,
      cursor: reset ? undefined : nextCursor.value,
      limit: 100
    })
    if (current !== requestSeq) return
    if (!appliedQuery && !selectedProviders.value.length && !selectedModalities.value.length) {
      const seen = new Set(providerOptions.value)
      for (const item of page.items ?? []) {
        const prov = item.provider?.trim()
        if (prov && prov !== 'unknown') seen.add(prov)
      }
      providerOptions.value = [...seen].sort()
    }
    models.value = reset ? page.items ?? [] : [...models.value, ...(page.items ?? [])]
    nextCursor.value = page.next_cursor ?? null
    asOf.value = page.as_of ?? null
  } catch (err) {
    if (current === requestSeq) error.value = err
  } finally {
    if (current === requestSeq) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

function togglePricingFilter(kind: 'modality' | 'provider', item: string): void {
  const target = kind === 'modality' ? selectedModalities : selectedProviders
  target.value = target.value.includes(item)
    ? target.value.filter((entry) => entry !== item)
    : [...target.value, item]
  void load()
}

function resetFilters(): void {
  query.value = ''
  selectedProviders.value = []
  selectedModalities.value = []
  sort.value = 'price_input'
  void load()
}

function rateText(model: PublicModelSummary, kind: 'input' | 'output'): string {
  if (model.price_state === 'free') return isZh.value ? '免费' : 'Free'
  const rate = kind === 'input' ? model.input_rate : model.output_rate
  return publicTokenPrice(rate, 4, isZh.value)
}

function priceStateBadge(model: PublicModelSummary): { label: string; tone: 'success' | 'info' | 'unknown' | 'warning' } {
  if (model.price_state === 'free') {
    return { label: isZh.value ? '免费额度' : 'Free', tone: 'success' }
  }
  if (model.price_state === 'priced' || model.input_rate || model.output_rate) {
    return { label: isZh.value ? '精确按量' : 'Per-Token', tone: 'info' }
  }
  return { label: isZh.value ? '未声明单价' : 'Unpriced', tone: 'unknown' }
}

function handleShortcut(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    const tag = (document.activeElement as HTMLElement | null)?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA') return
    event.preventDefault()
    searchContainerRef.value?.querySelector('input')?.focus()
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleShortcut)
  void load()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleShortcut)
})
</script>

<template>
  <PublicShell>
    <header class="flex flex-wrap items-end justify-between gap-4 border-b border-oh-default pb-6">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.18em] text-oh-muted">
          {{ isZh ? '透明计费 · PRICING' : 'PRICING' }}
        </p>
        <h1 class="mt-3 text-3xl font-semibold tracking-tight text-oh-primary sm:text-4xl">
          {{ isZh ? '透明、精确的百万 Token 定价' : 'Transparent, per-token pricing' }}
        </h1>
        <p class="mt-2 max-w-[68ch] text-sm text-oh-secondary">
          {{
            isZh
              ? '参考费率统一按每百万 (1M) Token 展示，十进制高精度结算；每次请求账单均严格遵循调用时冻结的费率版本。'
              : 'Reference rates per one million tokens with exact decimal settlement. Your gateway invoice always follows the frozen price version of each request.'
          }}
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <RouterLink
          to="/model-plaza"
          class="oh-focus-ring inline-flex h-9 items-center rounded-oh-md border border-oh-default bg-oh-surface px-3.5 text-xs font-medium text-oh-primary hover:border-oh-strong hover:bg-oh-hover"
        >
          {{ isZh ? '浏览模型详情' : 'Explore models' }}
        </RouterLink>
      </div>
    </header>

    <section class="flex flex-col gap-3.5 py-6">
      <div class="flex flex-wrap items-center gap-3">
        <div ref="searchContainerRef" class="w-full sm:w-80">
          <Input
            v-model="query"
            type="search"
            clearable
            :placeholder="isZh ? '筛选模型或供应商费率… (⌘K)' : 'Filter model or provider rates... (⌘K)'"
            @keydown.enter="load()"
            @clear="load()"
          />
        </div>
        <Button variant="secondary" @click="load()">{{ isZh ? '筛选' : 'Filter' }}</Button>

        <div class="ml-auto flex flex-wrap items-center gap-2">
          <label class="sr-only" for="pricing-sort">{{ isZh ? '排序方式' : 'Sort pricing' }}</label>
          <select
            id="pricing-sort"
            v-model="sort"
            class="oh-focus-ring h-9 rounded-oh-md border border-oh-default bg-oh-surface px-3 font-mono text-xs text-oh-primary hover:border-oh-strong"
            @change="load()"
          >
            <option value="price_input">{{ isZh ? '输入单价从低到高' : 'Lowest Input Rate' }}</option>
            <option value="name">{{ isZh ? '按模型名称' : 'Model Name' }}</option>
            <option value="context">{{ isZh ? '按上下文容量' : 'Context Window' }}</option>
          </select>

          <SegmentedControl
            :model-value="viewMode"
            :options="[
              { value: 'table', label: isZh ? '费率表' : 'Table' },
              { value: 'cards', label: isZh ? '卡片' : 'Cards' }
            ]"
            @update:model-value="viewMode = $event as ViewMode"
          />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
          {{ isZh ? '模态' : 'Modality' }}
        </span>
        <button
          v-for="chip in modalityChips"
          :key="chip.value"
          type="button"
          class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
          :class="
            selectedModalities.includes(chip.value)
              ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
              : 'border-oh-default text-oh-secondary hover:border-oh-strong'
          "
          :aria-pressed="selectedModalities.includes(chip.value)"
          @click="togglePricingFilter('modality', chip.value)"
        >
          {{ chip.label }}
        </button>

        <template v-if="providerOptions.length > 0">
          <span class="ml-2 mr-1 font-mono text-[11px] uppercase tracking-wider text-oh-muted">
            {{ isZh ? '供应商' : 'Provider' }}
          </span>
          <button
            v-for="provider in providerOptions"
            :key="provider"
            type="button"
            class="oh-focus-ring rounded-full border px-2.5 py-1 font-mono text-xs transition-colors"
            :class="
              selectedProviders.includes(provider)
                ? 'border-oh-accent bg-oh-accent-subtle font-medium text-oh-accent-text'
                : 'border-oh-default text-oh-secondary hover:border-oh-strong'
            "
            :aria-pressed="selectedProviders.includes(provider)"
            @click="togglePricingFilter('provider', provider)"
          >
            {{ provider }}
          </button>
        </template>

        <button
          v-if="activeFilters"
          type="button"
          class="oh-focus-ring ml-1 rounded-oh-sm text-xs font-medium text-oh-accent-text underline-offset-2 hover:underline"
          @click="resetFilters"
        >
          {{ isZh ? '重置筛选' : 'Clear filters' }}
        </button>

        <span v-if="asOf" class="ml-auto font-mono text-[11px] text-oh-muted">
          {{ isZh ? `更新于 ${formatPublicTimestamp(asOf)}` : `Updated ${formatPublicTimestamp(asOf)}` }}
        </span>
      </div>
    </section>

    <Skeleton v-if="loading" class="py-4" :lines="10" :label="isZh ? '正在加载模型费率' : 'Loading pricing catalog'" />
    <ErrorState
      v-else-if="error"
      :title="isZh ? '费率目录暂时无法加载' : 'Pricing unavailable'"
      :message="extractApiErrorMessage(error, '')"
      :retry-label="isZh ? '重新加载' : 'Retry'"
      @retry="load()"
    />
    <EmptyState
      v-else-if="!models.length"
      :variant="activeFilters ? 'filtered' : 'empty'"
      :title="
        activeFilters
          ? (isZh ? '未找到匹配该筛选条件的模型费率' : 'No model rates match these filters')
          : (isZh ? '暂未发布公开模型费率' : 'No public pricing published yet')
      "
      :description="
        activeFilters
          ? (isZh ? '请清空筛选条件或搜索其他模型名称。' : 'Clear active filters or try another search term.')
          : (isZh
              ? '公开价格表直接映射自已发布的网关模型目录。管理员在控制台完成渠道分组定价与模型广场上架后，实时费率将自动生效。'
              : 'Public pricing projects directly from published catalog rates. Once an administrator configures channel pricing and publishes models, rates appear here automatically.')
      "
      :action-label="
        activeFilters
          ? undefined
          : (authStore.isAuthenticated ? (isZh ? '前往控制台配置定价' : 'Open Console') : (isZh ? '登录控制台' : 'Sign in to Console'))
      "
      :clear-filter-label="isZh ? '清空筛选' : 'Reset filters'"
      @action="router.push(authStore.isAuthenticated ? '/dashboard' : '/login')"
      @clear-filters="resetFilters"
    />

    <template v-else>
      <div
        v-if="viewMode === 'table'"
        class="oh-focus-ring overflow-x-auto rounded-oh-md border border-oh-default bg-oh-surface"
        tabindex="0"
        role="region"
        :aria-label="isZh ? '模型定价一览表' : 'Model pricing table'"
      >
        <table class="w-full min-w-[720px] border-collapse text-sm">
          <thead>
            <tr class="border-b border-oh-default bg-oh-subtle text-left font-mono text-[11px] uppercase tracking-wider text-oh-muted">
              <th class="px-4 py-2.5">{{ isZh ? '模型' : 'Model' }}</th>
              <th class="px-4 py-2.5">{{ isZh ? '计费状态' : 'Billing Mode' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '上下文' : 'Context' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输入 / 1M Tokens' : 'Input / 1M' }}</th>
              <th class="px-4 py-2.5 text-right">{{ isZh ? '输出 / 1M Tokens' : 'Output / 1M' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in models" :key="model.id" class="border-b border-oh-subtle last:border-0 hover:bg-oh-hover">
              <td class="max-w-[20rem] break-all px-4 py-3">
                <RouterLink
                  :to="`/model-plaza/${encodeURIComponent(model.id)}`"
                  class="oh-focus-ring rounded-oh-xs font-medium text-oh-primary hover:text-oh-accent-text"
                >
                  {{ model.display_name || model.model }}
                </RouterLink>
                <div class="mt-0.5 flex items-center gap-1.5 font-mono text-[11px] text-oh-muted">
                  <span>{{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }} · {{ model.model }}</span>
                  <CopyButton :value="model.model" size="xs" :aria-label="isZh ? '复制模型 ID' : 'Copy model ID'" />
                </div>
              </td>
              <td class="px-4 py-3">
                <Badge :tone="priceStateBadge(model).tone" size="xs" variant="subtle">
                  {{ priceStateBadge(model).label }}
                </Badge>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-secondary">
                {{ formatContextTokens(model.context_tokens) }}
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                <span v-if="rateText(model, 'input') !== '—'">{{ rateText(model, 'input') }}</span>
                <Tooltip
                  v-else
                  :content="isZh ? '该模型未按输入 Token 单列费率，点击查看详情中的分段或按次费率' : 'No per-token input rate declared; open model details for band or request pricing'"
                  placement="left"
                >
                  <Badge tone="unknown" size="xs" variant="subtle">{{ isZh ? '详见模型页' : 'See detail' }}</Badge>
                </Tooltip>
              </td>
              <td class="px-4 py-3 text-right font-mono tabular-nums text-oh-primary">
                <span v-if="rateText(model, 'output') !== '—'">{{ rateText(model, 'output') }}</span>
                <Tooltip
                  v-else
                  :content="isZh ? '该模型未按输出 Token 单列费率，点击查看详情中的分段或按次费率' : 'No per-token output rate declared; open model details for band or request pricing'"
                  placement="left"
                >
                  <Badge tone="unknown" size="xs" variant="subtle">{{ isZh ? '详见模型页' : 'See detail' }}</Badge>
                </Tooltip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <RouterLink
          v-for="model in models"
          :key="model.id"
          :to="`/model-plaza/${encodeURIComponent(model.id)}`"
          class="oh-focus-ring group flex flex-col justify-between rounded-oh-md border border-oh-default bg-oh-surface p-4 transition-colors hover:border-oh-strong"
        >
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="truncate font-medium text-oh-primary group-hover:text-oh-accent-text">
                {{ model.display_name || model.model }}
              </p>
              <p class="mt-0.5 truncate font-mono text-[11px] text-oh-muted">
                {{ publicValue(model.provider, isZh ? '未声明' : 'Undeclared') }} · {{ formatContextTokens(model.context_tokens) }}
              </p>
            </div>
            <Badge :tone="priceStateBadge(model).tone" size="xs" variant="subtle">
              {{ priceStateBadge(model).label }}
            </Badge>
          </div>
          <dl class="mt-4 grid grid-cols-2 gap-3 border-t border-oh-subtle pt-3 font-mono text-xs tabular-nums">
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输入 / 1M' : 'Input / 1M' }}</dt>
              <dd class="mt-1 text-sm font-semibold text-oh-primary">{{ rateText(model, 'input') }}</dd>
            </div>
            <div>
              <dt class="text-[10px] uppercase text-oh-muted">{{ isZh ? '输出 / 1M' : 'Output / 1M' }}</dt>
              <dd class="mt-1 text-sm font-semibold text-oh-primary">{{ rateText(model, 'output') }}</dd>
            </div>
          </dl>
        </RouterLink>
      </div>

      <div v-if="nextCursor" class="mt-6 flex justify-center">
        <Button variant="secondary" :loading="loadingMore" @click="load(false)">
          {{ isZh ? '加载更多费率' : 'Load more rates' }}
        </Button>
      </div>
    </template>
  </PublicShell>
</template>
