<template>
  <div
    v-if="visible"
    data-test="cn-provider-quota"
    class="min-w-[220px] space-y-1"
  >
    <!-- Tier rows: 5h + weekly utilization bars (snapshot renders on mount).
         复用账号页 UsageProgressBar：同阈值配色、同倒计时格式。 -->
    <div v-if="data?.success && observedTiers.length" class="space-y-1">
      <UsageProgressBar
        v-for="tier in observedTiers"
        :key="tier.window"
        data-test="cn-provider-quota-tier"
        :label="windowLabel(tier.window)"
        :color="tier.window === 'weekly' ? 'emerald' : 'indigo'"
        :utilization="tier.used_percent"
        :resets-at="tier.reset_at"
      />
    </div>

    <template v-if="account.platform === 'volcengine'">
      <p data-test="quota-source" class="text-[10px] text-gray-500">{{ t('admin.accounts.cnProviders.volcengineQuota.source') }}</p>
      <p v-if="observationTime" data-test="quota-observed-at" class="text-[10px] text-gray-500">{{ t('admin.accounts.cnProviders.observedAt') }}: {{ observationTime }}</p>
      <p v-if="!observedTiers.length && !loading" data-test="quota-unknown" class="text-[10px] text-gray-500">{{ t('admin.accounts.cnProviders.usageUnknown') }}</p>
    </template>

    <template v-if="account.platform === 'commandcode'">
      <p v-if="data?.success" class="text-[10px] text-gray-400">{{ t('admin.accounts.cnProviders.commandCodeAlpha') }}<span v-if="observationTime"> · {{ observationTime }}</span></p>
      <p v-if="data?.plan_level" class="text-[10px] text-gray-500">{{ t('admin.accounts.cnProviders.commandCodePlan') }}: {{ planName }}</p>
      <p v-for="(value, bucket) in data?.credits" :key="bucket" class="text-[10px] text-gray-500">
        {{ t(`admin.accounts.cnProviders.${bucket}`) }}: {{ value.toFixed(2) }}
      </p>
      <p v-if="!data?.success" data-test="commandcode-local-fallback" class="text-[10px] text-gray-400">
        {{ t('admin.accounts.cnProviders.commandCodeLocal') }}
      </p>
    </template>

    <!-- Explicit refresh action (aligned with the OpenAI "Query" / Grok "Probe"
         buttons): a verb label tells users this chip is clickable. The previous
         noun label ("5h/weekly") read as a passive caption and users could not
         discover the manual refresh. -->
    <div class="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        data-test="cn-provider-quota-probe"
        class="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading"
        :aria-busy="loading"
        :title="t('admin.accounts.cnProviders.probeTooltip')"
        @click="handleProbe()"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': loading }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {{ t('admin.accounts.cnProviders.probe') }}
      </button>
    </div>

    <div
      v-if="error"
      role="status"
      class="truncate text-[10px] leading-4 text-red-600 dark:text-red-400"
      :title="error"
    >
      {{ truncatedError }}
    </div>
  </div>
</template>

<script lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

// Shared across remounts; setup-local maps are recreated for every account row.
const commandCodeLastProbeAt = new Map<number, number>()
</script>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CNProviderQuotaProbeResult } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { cnQuotaCellVisible } from './credentialsBuilder'
import UsageProgressBar from './UsageProgressBar.vue'

const props = defineProps<{
  account: Account
}>()

const { t } = useI18n()

const readMode = (): string => {
  const mode = props.account.credentials?.account_mode
  return typeof mode === 'string' ? mode : ''
}

const visible = computed(() => cnQuotaCellVisible(props.account.platform, readMode(), props.account.credentials, props.account.extra?.custom_base_url_enabled))

const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<CNProviderQuotaProbeResult | null>(null)
const observedTiers = computed(() => (data.value?.tiers || []).filter(tier =>
  ['5h', 'weekly', 'monthly'].includes(tier.window) && typeof tier.used_percent === 'number' && Number.isFinite(tier.used_percent)))

// 后端周期任务/手动探测写入的 extra 快照键（<provider>_ 前缀，与后端
// cnQuotaExtraUpdates 对齐）。页面加载即有数据，无需等待探测。
const SNAPSHOT_STALE_MS = 15 * 60 * 1000

// 自动探测去抖窗口与最近一次自动探测时间（模块级，跨实例共享）。
const AUTO_PROBE_DEBOUNCE_MS = 5 * 60 * 1000
const lastAutoProbeAt = new Map<number, number>()

const readExtraNumber = (key: string): number | null => {
  const v = (props.account.extra as Record<string, unknown> | undefined)?.[key]
  return typeof v === 'number' && Number.isFinite(v) ? v : null
}

const readExtraString = (key: string): string => {
  const v = (props.account.extra as Record<string, unknown> | undefined)?.[key]
  return typeof v === 'string' ? v : ''
}

// Restore independent observations; missing windows do not discard plan or credits.
const snapshotData = computed<CNProviderQuotaProbeResult | null>(() => {
  const platform = props.account.platform
  if (platform === 'commandcode' && props.account.extra?.commandcode_usage_supported === false) return null
  const used5h = readExtraNumber(`${platform}_5h_used_percent`)
  const usedWeekly = readExtraNumber(`${platform}_weekly_used_percent`)
  const usedMonthly = readExtraNumber(`${platform}_monthly_used_percent`)
  const tiers: CNProviderQuotaProbeResult['tiers'] = []
  if (used5h != null) {
    tiers.push({ window: '5h', used_percent: used5h, reset_at: readExtraString(`${platform}_5h_reset_at`) || undefined })
  }
  if (usedWeekly != null) {
    tiers.push({ window: 'weekly', used_percent: usedWeekly, reset_at: readExtraString(`${platform}_weekly_reset_at`) || undefined })
  }
  if (usedMonthly != null) {
    tiers.push({ window: 'monthly', used_percent: usedMonthly, reset_at: readExtraString(`${platform}_monthly_reset_at`) || undefined })
  }
  const credits: Record<string, number> = {}
  if (platform === 'commandcode') {
    for (const bucket of ['monthlyCredits', 'purchasedCredits', 'freeCredits']) {
      const value = readExtraNumber(`commandcode_${bucket}`)
      if (value != null) credits[bucket] = value
    }
  }
  const plan = readExtraString(`${platform}_plan_level`)
  if (!tiers.length && !Object.keys(credits).length && !plan) return null
  return { success: true, tiers, credits, plan_level: plan,
    fetched_at: Date.parse(readExtraString(`${platform}_usage_updated_at`)) / 1000 } as CNProviderQuotaProbeResult
})

// 快照是否过期（无更新时间或超过 staleness 窗口）→ 挂载时需要自动探测。
const snapshotIsStale = computed(() => {
  const updatedAt = readExtraString(`${props.account.platform}_usage_updated_at`)
  if (!updatedAt) return true
  const ts = new Date(updatedAt).getTime()
  return Number.isNaN(ts) || Date.now() - ts > SNAPSHOT_STALE_MS
})

// 挂载时：先用持久化快照渲染；快照缺失或过期再自动探测一次（失败显示错误，
// 避免静默失败导致单元格空白无提示）。
onMounted(() => {
  if (!visible.value) return
  data.value = snapshotData.value
  if (!snapshotIsStale.value) return
  // 模块级去抖：列表页每行一个实例，翻页/筛选/刷新会重复挂载；同一账号
  // 短时间内已自动探测过则跳过，避免对上游形成探测风暴。
  const probeTimes = props.account.platform === 'commandcode' ? commandCodeLastProbeAt : lastAutoProbeAt
  const last = probeTimes.get(props.account.id) ?? 0
  if (Date.now() - last < AUTO_PROBE_DEBOUNCE_MS) return
  probeTimes.set(props.account.id, Date.now())
  handleProbe()
})

const extractErrorMessage = (error: unknown): string => extractApiErrorMessage(error, t('common.error'))

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const windowLabel = (window: string) => {
  if (window === 'weekly') return t('admin.accounts.cnProviders.windowWeekly')
  if (window === 'monthly') return t('admin.accounts.cnProviders.windowMonthly')
  return t('admin.accounts.cnProviders.window5h')
}

const planName = computed(() => {
  const id = data.value?.plan_level || ''
  const names: Record<string, string> = { 'individual-goat': 'GOAT', 'individual-pro': 'Pro', 'individual-go': 'Go', 'individual-max': 'Max' }
  return names[id] || id
})
const observationTime = computed(() => {
  const fetchedAt = data.value?.fetched_at
  return fetchedAt && Number.isFinite(fetchedAt) && !Number.isNaN(new Date(fetchedAt * 1000).getTime())
    ? new Date(fetchedAt * 1000).toLocaleString() : ''
})
let observationGeneration = 0
const handleProbe = async () => {
  if (loading.value || !visible.value) return
  const generation = observationGeneration
  const accountID = props.account.id
  loading.value = true
  error.value = null
  try {
    const result = await adminAPI.cnProviders.queryQuota(accountID)
    if (generation !== observationGeneration) return
    // 失败时保留已渲染的快照条形图（仅显示错误行），成功才覆盖。
    if (result.success) {
      data.value = result
    } else {
      if (props.account.platform === 'commandcode') data.value = null
      error.value = props.account.platform === 'volcengine' && result.status && result.status !== 'ok'
        ? t(`admin.accounts.cnProviders.volcengineQuota.status.${result.status}`)
        : result.unsupported && props.account.platform === 'commandcode'
        ? t('admin.accounts.cnProviders.commandCodeUnsupported') : result.error || t('common.error')
    }
  } catch (e) {
    if (generation !== observationGeneration) return
    if (props.account.platform === 'commandcode') data.value = null
    error.value = extractErrorMessage(e)
  } finally {
    if (generation === observationGeneration) loading.value = false
  }
}

watch(
  [() => props.account.id, () => JSON.stringify([props.account.credentials, props.account.credentials_status,
    props.account.extra?.custom_base_url_enabled, props.account.platform === 'volcengine' ? props.account.updated_at : null]),
    () => props.account.platform, () => snapshotData.value],
  (current, previous) => {
    observationGeneration++
    const identityChanged = current.slice(0, 3).some((value, index) => value !== previous[index])
    data.value = identityChanged ? null : snapshotData.value
    error.value = null
    loading.value = false
  }
)
</script>
