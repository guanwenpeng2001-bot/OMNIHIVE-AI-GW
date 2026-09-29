<template>
  <div class="card" data-testid="profile-billing-timeline">
    <div class="flex items-start justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div>
        <h2 class="text-lg font-medium text-gray-900 dark:text-white">
          {{ t('billing.timeline.title') }}
        </h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('billing.timeline.description') }}
        </p>
      </div>
      <button class="btn btn-secondary btn-sm" :disabled="loading" @click="load(page)">
        {{ t('common.refresh') }}
      </button>
    </div>

    <div class="space-y-4 px-6 py-6">
      <p v-if="error" class="text-sm text-red-500" data-testid="billing-timeline-error">
        {{ error }}
      </p>

      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div
          v-for="summary in summaries"
          :key="summary.key"
          class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-800"
          :data-testid="`billing-total-${summary.key}`"
        >
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ summary.label }}</div>
          <div class="text-sm font-medium" :class="summary.className">{{ summary.value }}</div>
        </div>
      </div>

      <p v-if="!loading && entries.length === 0" class="py-6 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('billing.timeline.empty') }}
      </p>

      <ul v-else class="divide-y divide-gray-100 dark:divide-dark-700">
        <li
          v-for="entry in entries"
          :key="entryKey(entry)"
          class="flex items-center justify-between gap-4 py-3"
          data-testid="billing-entry"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <span class="badge" :class="kindBadgeClass(entry.kind)">
                {{ t(`billing.kind.${entry.kind}`) }}
              </span>
              <span class="text-sm text-gray-500 dark:text-gray-400">
                {{ formatDateTime(entry.occurred_at) }}
              </span>
            </div>
            <div class="mt-1 truncate text-xs text-gray-400 dark:text-gray-500" :data-testid="`billing-detail-${entry.kind}`">
              <span v-if="entry.kind === 'charge' && entry.request_count">
                {{ t('billing.timeline.requests', { count: entry.request_count }) }} ·
              </span>
              <span v-if="entry.detail && entry.detail !== 'rebate_accrue'">{{ entry.detail }} · </span>
              <span v-if="!entry.affects_balance">{{ t('billing.timeline.accruedOnly') }} · </span>
              <span>{{ entry.reference }}</span>
            </div>
          </div>
          <div class="whitespace-nowrap text-sm font-medium" :class="amountClass(entry.amount)" data-testid="billing-amount">
            {{ formatAmount(entry.amount) }}
          </div>
        </li>
      </ul>

      <div v-if="total > pageSize" class="flex items-center justify-between">
        <button class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="load(page - 1)">
          {{ t('common.back') }}
        </button>
        <span class="text-xs text-gray-500 dark:text-gray-400" data-testid="billing-page">
          {{ t('billing.timeline.page', { page, pages: pageCount }) }}
        </span>
        <button class="btn btn-secondary btn-sm" :disabled="page >= pageCount || loading" @click="load(page + 1)">
          {{ t('common.next') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { billingAPI, type UserBillingTimelineEntry, type UserBillingTimelinePage } from '@/api/billing'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()

const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const entries = ref<UserBillingTimelineEntry[]>([])
const totals = ref<UserBillingTimelinePage['totals'] | null>(null)
const loading = ref(false)
const error = ref('')

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

const formatAmount = (amount: number) => `${amount >= 0 ? '+' : '-'}$${Math.abs(amount).toFixed(4)}`
const amountClass = (amount: number) => (amount >= 0 ? 'text-green-600 dark:text-green-400' : 'text-gray-900 dark:text-white')

const kindBadgeClass = (kind: string) =>
  kind === 'recharge' || kind === 'redeem' || kind === 'adjustment'
    ? 'badge-success'
    : kind === 'refund'
      ? 'badge-warning'
      : kind === 'rebate'
        ? 'badge-purple'
        : 'badge-gray'

const summaries = computed(() => [
  {
    key: 'credits',
    label: t('billing.timeline.credits'),
    value: `+$${(totals.value?.credits ?? 0).toFixed(4)}`,
    className: 'text-green-600 dark:text-green-400'
  },
  {
    key: 'debits',
    label: t('billing.timeline.debits'),
    value: `-$${Math.abs(totals.value?.debits ?? 0).toFixed(4)}`,
    className: 'text-gray-900 dark:text-white'
  },
  {
    key: 'net',
    label: t('billing.timeline.net'),
    value: `${(totals.value?.net ?? 0) >= 0 ? '+' : '-'}$${Math.abs(totals.value?.net ?? 0).toFixed(4)}`,
    className: 'text-gray-900 dark:text-white'
  },
  {
    key: 'accrued_rebate',
    label: t('billing.timeline.accruedRebate'),
    value: `$${(totals.value?.accrued_rebate ?? 0).toFixed(4)}`,
    className: 'text-gray-900 dark:text-white'
  }
])

const entryKey = (entry: UserBillingTimelineEntry) => `${entry.kind}-${entry.occurred_at}-${entry.reference}`

const load = async (targetPage: number) => {
  loading.value = true
  error.value = ''
  try {
    const result = await billingAPI.getMyBillingTimeline({ page: targetPage, page_size: pageSize.value })
    page.value = result.page
    pageSize.value = result.page_size
    total.value = result.total
    entries.value = result.entries ?? []
    totals.value = result.totals
  } catch (err) {
    error.value = t('billing.timeline.loadFailed')
    entries.value = []
  } finally {
    loading.value = false
  }
}

onMounted(() => load(1))
</script>
