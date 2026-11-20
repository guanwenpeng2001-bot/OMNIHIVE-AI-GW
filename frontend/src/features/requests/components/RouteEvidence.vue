<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui'
import type { Schema } from '../api'
import { statusTone } from '../presentation'

const props = defineProps<{ explanation: Schema['RequestsRouteExplanation']; financialAccess: boolean }>()
const { t } = useI18n()

const isPartial = computed(() => props.explanation.data_completeness.state === 'partial')
const traceNotRecorded = computed(() =>
  props.explanation.data_completeness.missing?.some((item) => item.reason === 'TRACE_NOT_RECORDED') ?? false
)
</script>

<template>
  <div class="space-y-3">
    <div
      v-if="isPartial || traceNotRecorded"
      data-testid="route-completeness-notice"
      class="flex flex-wrap items-center gap-2 rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg/60 px-3 py-2.5 text-xs text-oh-status-warn-fg"
    >
      <Badge tone="warning" size="xs">{{ traceNotRecorded ? 'TRACE_NOT_RECORDED' : t('costLedger.requests.partialEvidence') }}</Badge>
      <span>{{ traceNotRecorded ? t('costLedger.requests.traceNotRecorded') : t('costLedger.requests.routePartial') }}</span>
    </div>

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
    </div>
    <p v-else class="rounded-oh-md border border-dashed border-oh-default bg-oh-subtle px-3 py-4 text-sm text-oh-muted">
      {{ t('costLedger.requests.noRouteTrace') }}
    </p>
  </div>
</template>
