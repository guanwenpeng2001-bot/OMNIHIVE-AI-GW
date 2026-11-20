<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui'
import type { Schema } from '../api'
import { statusTone } from '../presentation'

defineProps<{ explanation: Schema['RequestsRouteExplanation']; financialAccess: boolean }>()
const { t } = useI18n()
</script>

<template>
  <div v-if="explanation.trace" class="space-y-3">
    <div class="grid gap-1 rounded-oh-md border border-oh-subtle bg-oh-subtle px-3 py-2.5 text-xs text-oh-secondary">
      <span>{{ t('costLedger.requests.policyVersion') }} <code class="break-all text-oh-primary">{{ explanation.trace.policy_version || '—' }}</code></span>
      <span>{{ t('costLedger.requests.snapshotId') }} <code class="break-all text-oh-primary">{{ explanation.trace.snapshot_id }}</code></span>
      <span v-if="explanation.trace.selected?.model_id">{{ t('costLedger.requests.effectiveModel') }} <code class="break-all text-oh-primary">{{ explanation.trace.selected.model_id }}</code></span>
      <span v-if="financialAccess && explanation.trace.selected?.funding_source">{{ t('costLedger.billing.fundingSource') }} <code>{{ explanation.trace.selected.funding_source }}</code></span>
    </div>
    <ul class="divide-y divide-oh-subtle">
      <li v-for="candidate in explanation.trace.candidates" :key="candidate.id" class="space-y-1.5 py-2.5">
        <div class="flex flex-wrap items-center justify-between gap-1.5">
          <code class="break-all text-xs font-medium text-oh-primary">{{ candidate.id }}</code>
          <Badge v-if="candidate.id === explanation.trace.selected?.candidate_id" tone="accent" size="xs" dot>{{ t('costLedger.states.selected') }}</Badge>
          <Badge v-else :tone="statusTone(candidate.state)" size="xs" dot>{{ t(`costLedger.states.${candidate.state}`) }}</Badge>
        </div>
        <ul v-if="candidate.reasons.length" class="space-y-1 text-xs text-oh-secondary">
          <li v-for="reason in candidate.reasons" :key="`${reason.code}-${reason.stage}`" class="flex flex-wrap items-center gap-x-2">
            <code class="break-all text-[11px] text-oh-accent-text">{{ reason.code }}</code>
            <span>{{ reason.stage }}</span>
          </li>
        </ul>
      </li>
    </ul>
    <p v-if="explanation.data_completeness.state === 'partial'" class="text-xs text-oh-status-warn-fg">{{ t('costLedger.requests.routePartial') }}</p>
  </div>
  <p v-else class="rounded-oh-md border border-dashed border-oh-default bg-oh-subtle px-3 py-4 text-sm text-oh-muted">
    {{ t('costLedger.requests.noRouteTrace') }}
  </p>
</template>
