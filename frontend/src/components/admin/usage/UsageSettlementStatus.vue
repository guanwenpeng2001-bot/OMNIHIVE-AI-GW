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
const { t } = useI18n()
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
  <section class="card space-y-3 p-4" :aria-label="t('admin.usage.settlement.title')">
    <details class="text-sm">
      <summary class="cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--ring)]">
        <span class="font-semibold">{{ t('admin.usage.settlement.title') }}</span>
        <span v-if="status" class="mt-2 block text-xs leading-6 tabular-nums text-[var(--muted-foreground)]">
          {{ t('admin.usage.settlement.pending') }}: {{ status.pending }} · {{ t('admin.usage.settlement.review') }}: {{ status.needs_review }} ·
          {{ t('admin.usage.settlement.failures') }}: {{ status.persistence_unconfirmed }} · {{ t('admin.usage.settlement.historical') }}: {{ status.historical_zero_review }}
        </span>
      </summary>
      <div class="mt-3 space-y-3">
        <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('admin.usage.settlement.boundary') }}</p>
        <form class="flex flex-wrap items-center gap-2" @submit.prevent="load()">
          <label for="settlement-request-id" class="text-sm">{{ t('admin.usage.settlement.locate') }}</label>
          <input id="settlement-request-id" v-model="requestID" class="input max-w-sm" />
          <button type="submit" class="btn btn-secondary" :disabled="loading">{{ t('admin.usage.settlement.refresh') }}</button>
        </form>
        <div v-if="status && status.needs_review > 0" class="max-h-56 overflow-auto text-sm" aria-live="polite">
          <p v-if="!status.items.length">{{ t('admin.usage.settlement.empty') }}</p>
          <ul v-else class="space-y-2">
            <li v-for="item in status.items" :key="item.id" class="break-all rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3 font-mono text-xs">
              {{ item.request_id }} · key={{ item.api_key_id }} · user={{ item.user_id }} · {{ item.state }} / {{ item.pricing_state }} · {{ item.created_at }}
            </li>
          </ul>
          <button v-if="status.items.length === 100" class="btn btn-secondary mt-2" :disabled="loading" @click="load(status.items[99]!.id)">{{ t('admin.usage.settlement.next') }}</button>
        </div>
        <details v-if="status?.historical_items.length" class="text-sm">
          <summary class="cursor-pointer">{{ t('admin.usage.settlement.historical') }}</summary>
          <ul class="mt-2 max-h-40 space-y-1 overflow-auto font-mono">
            <li v-for="item in status.historical_items" :key="item.id" class="break-all">
              log={{ item.id }} · {{ item.request_id }} · key={{ item.api_key_id }} · user={{ item.user_id }}
            </li>
          </ul>
        </details>
        <p class="text-xs text-gray-500">{{ t('admin.usage.settlement.separate') }}</p>
      </div>
    </details>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ t('admin.usage.settlement.failed') }}</p>
  </section>
</template>
