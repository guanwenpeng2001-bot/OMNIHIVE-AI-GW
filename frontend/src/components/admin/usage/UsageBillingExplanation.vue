<template>
  <details class="mt-1 max-w-sm text-xs" data-testid="billing-explanation">
    <summary class="cursor-pointer text-primary-600 dark:text-primary-400">{{ t('usage.evidence.title') }} · {{ values[0] }} · {{ values[2] }}</summary>
    <dl class="mt-2 space-y-1 rounded bg-gray-50 p-2 dark:bg-dark-800">
      <div v-for="(field, index) in billingEvidenceFields" :key="field">
        <dt class="font-medium text-gray-500">{{ t(`usage.evidence.${field}`) }}</dt>
        <dd class="whitespace-pre-wrap break-all text-gray-900 dark:text-gray-200">{{ values[index] }}</dd>
      </div>
    </dl>
  </details>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageLog } from '@/types'
import { billingEvidenceFields, billingExplanationValues } from '@/utils/usageBillingExplanation'

const props = defineProps<{ log: UsageLog }>()
const { t } = useI18n()
const values = computed(() => billingExplanationValues(props.log, t))
</script>
