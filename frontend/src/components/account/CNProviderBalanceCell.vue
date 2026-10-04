<template>
  <div v-if="visible" class="space-y-1">
    <!-- Balance value row: static display (snapshot or probe result) -->
    <div v-if="budget" data-test="project-budget" class="text-xs" aria-live="polite">
      <div>{{ t('admin.accounts.compatibleUsage.projectBudget') }}</div>
      <div v-if="budget.project_id != null && budget.ram_user_id != null">{{ t('admin.accounts.compatibleUsage.project') }}: {{ budget.project_id }} · {{ t('admin.accounts.compatibleUsage.member') }}: {{ budget.ram_user_id }}</div>
      <div>{{ t('admin.accounts.compatibleUsage.consumed') }}: {{ budget.consumed }} {{ budget.currency }}</div>
      <div>{{ t('admin.accounts.compatibleUsage.limit') }}: {{ budget.unlimited ? t('admin.accounts.compatibleUsage.unlimited') : budget.limit == null ? '—' : `${budget.limit} ${budget.currency}` }}</div>
      <div>{{ t(`admin.accounts.compatibleUsage.${budget.period_kind}`) }}: {{ budget.period_start }} – {{ budget.period_end }}</div>
    </div>
    <div v-else class="flex flex-wrap items-center gap-1.5">
      <span
        data-test="cn-provider-balance-value"
        :class="['text-[10px] font-medium leading-4', platformTextClass(account.platform)]"
        :title="t('admin.accounts.cnProviders.balanceProbeTooltip')"
      >
        {{ balanceLabel }}
      </span>

      <!-- Low balance badge (reactive 402/429 marker or probe-detected) -->
      <span
        v-if="balanceLow"
        class="inline-flex items-center rounded bg-red-100 px-1 py-0.5 text-[10px] font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300"
      >
        {{ t('admin.accounts.cnProviders.balanceLow') }}
      </span>
    </div>

    <div v-if="['atlascloud', 'stepfun_api', 'stepfun_api_intl', 'teamorouter', 'teamorouter_com'].includes(account.platform)" class="text-[10px] text-gray-500">{{ t('admin.accounts.compatibleUsage.accountBalance') }}</div>
    <div v-if="stale" class="text-[10px] text-amber-600">{{ t('admin.accounts.compatibleUsage.stale') }}</div>
    <!-- Explicit refresh action (aligned with the OpenAI "Query" / Grok "Probe"
         buttons): the old chip doubled as the data display and a hidden click
         target, which users could not discover. The verb label makes the
         affordance explicit. -->
    <div class="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        data-test="cn-provider-balance-probe"
        class="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading"
        :title="t('admin.accounts.cnProviders.balanceProbeTooltip')"
        @click="handleProbe"
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

    <div v-if="error" role="alert" class="truncate text-[10px] text-red-600 dark:text-red-400" :title="error">
      {{ truncatedError }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { extractApiErrorMessage } from '@/utils/apiError'

import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CNProviderBalanceEntry, CNProviderBalanceResult } from '@/api/admin/cnProviders'
import type { Account } from '@/types'
import { platformTextClass } from '@/utils/platformColors'
import { cnBalanceCellVisible } from './credentialsBuilder'
import { compatibleBalanceVisible, compatibleFailureReason } from './compatibleBalance'
import { compatibleProvider } from '@/constants/compatibleProviders'

const props = defineProps<{
  account: Account
}>()

const { t } = useI18n()

const readMode = (): string => {
  const mode = props.account.credentials?.account_mode
  return typeof mode === 'string' ? mode : ''
}

// Native and compatible PAYG accounts with a verified balance endpoint.
const visible = computed(() => cnBalanceCellVisible(props.account.platform, readMode()) || compatibleBalanceVisible(props.account))

const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<CNProviderBalanceResult | null>(null)
const budget = computed(() => data.value?.kind === 'project_budget' ? data.value.project_budget : undefined)
const stale = computed(() => !!compatibleProvider(props.account.platform) && (budget.value || currentEntries.value.length > 0) && (loading.value || !!error.value || !data.value))
let requestGeneration = 0

const extraKey = (suffix: string) => `${props.account.platform}_${suffix}`

// 落库快照（后端周期探测/响应式写入 account.Extra）。
const snapshotBalance = computed(() => {
  const v = props.account.extra?.[extraKey('balance')]
  return typeof v === 'number' ? v : null
})
const snapshotCurrency = computed(() => {
  const v = props.account.extra?.[extraKey('balance_currency')]
  return typeof v === 'string' ? v : ''
})
// 多币种快照（后端写 <platform>_balances：[{currency, balance}]，deepseek CNY+USD）。
const snapshotBalances = computed<CNProviderBalanceEntry[]>(() => {
  const v = props.account.extra?.[extraKey('balances')]
  if (!Array.isArray(v)) return []
  return v.flatMap((item): CNProviderBalanceEntry[] => {
    if (!item || typeof item !== 'object') return []
    const { currency, balance } = item as Record<string, unknown>
    if (typeof currency !== 'string' || typeof balance !== 'number') return []
    return [{ currency, balance }]
  })
})
const balanceLow = computed(() => props.account.extra?.[extraKey('balance_low')] === true)

// 优先用探测结果，其次落库快照。多币种返回全部明细，否则主币种单条。
const currentEntries = computed<CNProviderBalanceEntry[]>(() => {
  if (data.value && data.value.success) {
    if (data.value.balances && data.value.balances.length > 0) return data.value.balances
    if (typeof data.value.balance === 'number') return [{ currency: data.value.currency || '', balance: data.value.balance }]
    return []
  }
  if (snapshotBalances.value.length > 0) return snapshotBalances.value
  if (snapshotBalance.value != null) {
    return [{ currency: snapshotCurrency.value, balance: snapshotBalance.value }]
  }
  return []
})

const formatEntry = (entry: CNProviderBalanceEntry): string => {
  const fixed = entry.balance >= 100 ? entry.balance.toFixed(0) : entry.balance.toFixed(2)
  return `${entry.currency || (compatibleProvider(props.account.platform) ? '' : '¥')} ${fixed}`.trim()
}

const balanceLabel = computed(() => {
  if (currentEntries.value.length === 0) {
    // 修复:此前误引 admin.accounts.grokBalance(实际嵌套在 usageWindow 下),
    // 未命中时渲染原始 key。CN 供应商使用自己的占位键。
    return t('admin.accounts.cnProviders.balance')
  }
  return currentEntries.value.map(formatEntry).join(' · ')
})

const extractErrorMessage = (error: unknown): string => extractApiErrorMessage(error, t('common.error'))

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const handleProbe = async () => {
  if (loading.value || !visible.value) return
  const generation = ++requestGeneration
  loading.value = true
  error.value = null
  try {
    const result = await adminAPI.cnProviders.queryBalance(props.account.id)
    if (generation !== requestGeneration) return
    // 失败时保留快照展示（仅显示错误行），成功才覆盖。
    if (result.success) {
      data.value = result
    } else {
      error.value = compatibleProvider(props.account.platform)
        ? t(`admin.accounts.compatibleUsage.reasons.${compatibleFailureReason(result)}`)
        : result.error || t('common.error')
    }
  } catch (e) {
    if (generation !== requestGeneration) return
    error.value = compatibleProvider(props.account.platform)
      ? t(`admin.accounts.compatibleUsage.reasons.${compatibleFailureReason({ status_code: (e as { response?: { status?: number } })?.response?.status })}`)
      : extractErrorMessage(e)
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

watch(
  () => [props.account.id, props.account.platform, props.account.type, JSON.stringify(props.account.credentials), props.account.credentials_status?.has_api_key, props.account.extra?.custom_base_url_enabled],
  () => {
    requestGeneration++
    data.value = null
    error.value = null
    loading.value = false
  }
)
onBeforeUnmount(() => { requestGeneration++ })
</script>
