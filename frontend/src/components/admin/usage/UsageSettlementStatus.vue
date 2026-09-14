<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'

interface SettlementStatus {
  pending: number
  needs_review: number
  historical_zero_review: number
  persistence_unconfirmed: number
  items: { id: number; request_id: string; api_key_id: number; user_id: number; state: string; pricing_state: string; created_at: string }[]
  historical_items: { id: number; request_id: string; api_key_id: number; user_id: number }[]
}
const { t } = useI18n({ messages: {
  en: { title: 'Settlement recovery', pending: 'Pending', review: 'Needs review', historical: 'Historical zero-cost rows without receipts (review only)', boundary: 'Recovery starts after the first receipt commits. Process death or database failure before that point can lose upstream usage; manual review is required. The failure count below covers this instance since startup.', failures: 'Persistence unconfirmed', locate: 'Request ID', refresh: 'Find / refresh', next: 'Next 100', failed: 'Could not load settlement status', empty: 'No matching receipts', separate: 'Receipt counts are separate from usage totals and exports.' },
  zh: { title: '结算恢复', pending: '待结算', review: '待核查', historical: '无回执历史零费行（仅供人工核查）', boundary: '恢复从首次回执提交成功后开始。此前进程退出或数据库不可写可能丢失上游用量，需人工核查。下方失败数仅为本实例启动以来记录。', failures: '落库未确认', locate: '请求 ID', refresh: '定位 / 刷新', next: '后 100 条', failed: '结算状态加载失败', empty: '没有匹配的回执', separate: '回执数量独立展示，不计入正式用量统计和导出。' }
} })
const status = ref<SettlementStatus | null>(null)
const requestID = ref('')
const loading = ref(false)
const error = ref(false)
async function load(afterID = 0) {
  loading.value = true
  error.value = false
  try {
    const { data } = await apiClient.get<SettlementStatus>('/admin/usage/settlements', { params: { request_id: requestID.value.trim(), after_id: afterID } })
    status.value = data
  } catch { error.value = true } finally { loading.value = false }
}
onMounted(() => { void load() })
</script>

<template>
  <section class="card space-y-3 p-4" :aria-label="t('title')">
    <h2 class="font-semibold">{{ t('title') }}</h2>
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('boundary') }}</p>
    <p v-if="status" class="text-sm">
      {{ t('pending') }}: {{ status.pending }} · {{ t('review') }}: {{ status.needs_review }} ·
      {{ t('failures') }}: {{ status.persistence_unconfirmed }} · {{ t('historical') }}: {{ status.historical_zero_review }}
    </p>
    <form class="flex flex-wrap items-center gap-2" @submit.prevent="load()">
      <label for="settlement-request-id" class="text-sm">{{ t('locate') }}</label>
      <input id="settlement-request-id" v-model="requestID" class="input max-w-sm" />
      <button type="submit" class="btn btn-secondary" :disabled="loading">{{ t('refresh') }}</button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ t('failed') }}</p>
    <div v-else-if="status" class="max-h-56 overflow-auto text-sm" aria-live="polite">
      <p v-if="!status.items.length">{{ t('empty') }}</p>
      <ul v-else class="space-y-2">
        <li v-for="item in status.items" :key="item.id" class="break-all font-mono">
          {{ item.request_id }} · key={{ item.api_key_id }} · user={{ item.user_id }} · {{ item.state }} / {{ item.pricing_state }} · {{ item.created_at }}
        </li>
      </ul>
      <button v-if="status.items.length === 100" class="btn btn-secondary mt-2" :disabled="loading" @click="load(status.items[99]!.id)">{{ t('next') }}</button>
    </div>
    <details v-if="status?.historical_items.length" class="text-sm">
      <summary class="cursor-pointer">{{ t('historical') }}</summary>
      <ul class="mt-2 max-h-40 space-y-1 overflow-auto font-mono">
        <li v-for="item in status.historical_items" :key="item.id" class="break-all">
          log={{ item.id }} · {{ item.request_id }} · key={{ item.api_key_id }} · user={{ item.user_id }}
        </li>
      </ul>
    </details>
    <p class="text-xs text-gray-500">{{ t('separate') }}</p>
  </section>
</template>
