<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CompatibleProvider } from '@/constants/compatibleProviders'

const props = defineProps<{ provider: CompatibleProvider }>()
const { locale } = useI18n()
const chinese = computed(() => locale?.value?.startsWith('zh') ?? false)
const billing = computed(() => props.provider.billing)
const titles = {
  subscription_first: ['Subscription balance first', '优先使用订阅额度'],
  subscription: ['Supplier subscription', '供应商订阅'],
  recurring: ['Recurring supplier plans', '供应商周期套餐'],
  monthly_credits: ['Monthly subscription credits', '订阅月度额度'],
  coding_plan: ['Supplier Coding Plan', '供应商 Coding Plan'],
  token_plan: ['Supplier Token Plan', '供应商 Token Plan'],
  prepaid_credits: ['Prepaid credits', '预充值余额']
} as const
const title = computed(() => billing.value ? titles[billing.value.behavior]?.[chinese.value ? 1 : 0] : '')
</script>

<template>
  <aside v-if="billing?.selection === 'described'" data-testid="compatible-billing"
    class="flex items-start gap-3 rounded-lg border-2 border-gray-200 bg-gray-50 p-3 text-left text-sm dark:border-dark-600 dark:bg-dark-800">
    <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400" aria-hidden="true">
      <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8">
        <circle cx="12" cy="12" r="9" />
        <path stroke-linecap="round" d="M12 11v6M12 7h.01" />
      </svg>
    </span>
    <div class="min-w-0">
      <p class="font-medium text-gray-900 dark:text-white">{{ title }}</p>
      <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ chinese ? billing.summary_zh : billing.summary }}</p>
      <a :href="billing.evidence_url" target="_blank" rel="noopener noreferrer"
        class="mt-2 inline-flex text-xs text-cyan-600 underline underline-offset-2 dark:text-cyan-400">
        {{ chinese ? '供应商官方说明' : 'Official supplier documentation' }}
      </a>
      <span class="ml-2 text-xs text-gray-500 dark:text-gray-400">
        {{ chinese ? '核实日期：' : 'Checked: ' }}{{ billing.evidence_observed }}
      </span>
    </div>
  </aside>
</template>
