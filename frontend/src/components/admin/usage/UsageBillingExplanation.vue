<template>
  <details class="mt-1 max-w-sm text-xs" data-testid="billing-explanation">
    <summary class="cursor-pointer text-[var(--brand-text)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--ring)]">{{ t('usage.evidence.title') }} · {{ values[0] }} · {{ values[2] }}</summary>
    <dl class="mt-2 space-y-2 rounded-lg border border-[var(--border)] bg-[var(--surface-2)] p-3">
      <div v-for="(field, index) in billingEvidenceFields" :key="field">
        <dt class="font-medium text-[var(--muted-foreground)]">{{ t(`usage.evidence.${field}`) }}</dt>
        <dd class="whitespace-pre-wrap break-all text-[var(--foreground)]">{{ values[index] }}</dd>
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
