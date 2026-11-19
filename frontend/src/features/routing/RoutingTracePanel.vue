<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui'
import type { PolicySimulation } from './api'
import { traceSummary } from './policy'

const props = defineProps<{ result: PolicySimulation; expired: boolean }>()
const summary = computed(() => traceSummary(props.result.trace))
const { t, te } = useI18n()

function reason(code: string) {
  const key = 'routing.reason.' + code
  return te(key) ? t(key) : code
}
</script>

<template>
  <section class="routing-panel space-y-4" :aria-label="t('routing.trace')">
    <header class="flex flex-wrap items-baseline justify-between gap-2">
      <h3 class="routing-heading">{{ t('routing.trace') }}</h3>
      <Badge :tone="expired ? 'warning' : 'neutral'">{{ expired ? t('routing.expired') : t('routing.frozen') }}</Badge>
    </header>
    <div class="grid grid-cols-2 gap-2 lg:grid-cols-4">
      <div v-for="metric in [
        { label: t('routing.eligible'), value: summary.ranked.length },
        { label: t('routing.excluded'), value: summary.excluded.length },
        { label: t('routing.impactProjects'), value: result.impact?.projects },
        { label: t('routing.impactKeys'), value: result.impact?.keys }
      ]" :key="metric.label" class="routing-metric">
        <span class="routing-hint">{{ metric.label }}</span>
        <span class="font-mono text-lg font-semibold oh-tabular" :aria-label="metric.value == null ? t('routing.unknown') : undefined">{{ metric.value ?? '—' }}</span>
      </div>
    </div>
    <dl class="grid gap-3 rounded-oh-md border border-oh-default bg-oh-subtle p-3 text-[13px] sm:grid-cols-2">
      <div>
        <dt class="routing-hint">{{ t('routing.predicted') }}</dt>
        <dd class="break-all font-mono oh-tabular">{{ result.trace.selected?.candidate_id || t('routing.noCandidate') }}</dd>
      </div>
      <div>
        <dt class="routing-hint">{{ t('routing.affinity') }}</dt>
        <dd v-if="summary.affinity" class="break-all font-mono oh-tabular">{{ summary.affinity.value || '—' }}</dd>
        <dd v-else class="text-oh-muted" :aria-label="t('routing.affinityUnknown')">— <span class="font-sans">{{ t('routing.affinityUnknown') }}</span></dd>
      </div>
      <div>
        <dt class="routing-hint">{{ t('routing.fundingSource') }}</dt>
        <dd class="break-all font-mono oh-tabular" :aria-label="!result.trace.selected?.funding_source ? t('routing.unknown') : undefined">{{ result.trace.selected?.funding_source || '—' }}</dd>
      </div>
      <div>
        <dt class="routing-hint">{{ t('routing.snapshotAt') }}</dt>
        <dd class="break-all font-mono oh-tabular">{{ result.trace.as_of }}</dd>
      </div>
    </dl>
    <p v-if="result.diff" class="text-xs text-oh-secondary">
      {{ t('routing.diff', { eligible: result.diff.became_eligible.length, excluded: result.diff.became_excluded.length }) }}
      {{ result.diff.selection_changed ? t('routing.selectionChanged') : t('routing.selectionUnchanged') }}
    </p>
    <div v-if="result.trace.uncertainties.length" class="rounded-oh-md border border-oh-status-warn-border bg-oh-status-warn-bg p-3 text-xs text-oh-status-warn-fg">
      <p class="mb-1 font-semibold">{{ t('routing.uncertainty') }}</p>
      <ul class="list-disc space-y-1 pl-4">
        <li v-for="item in result.trace.uncertainties" :key="item">{{ reason(item) }}</li>
      </ul>
    </div>
    <details v-if="result.trace.origins.length" class="text-xs text-oh-secondary">
      <summary class="oh-focus-ring cursor-pointer font-medium">{{ t('routing.origins') }}</summary>
      <ul class="mt-2 space-y-2">
        <li v-for="(origin, index) in result.trace.origins" :key="index" class="break-all font-mono oh-tabular">
          {{ origin.constraint }} · {{ origin.scope_type }} / {{ origin.scope_id }} · {{ origin.version }}
        </li>
      </ul>
    </details>
    <div v-if="result.trace.candidates.length" class="divide-y divide-oh-subtle border-y border-oh-default">
      <article
        v-for="candidate in [...summary.ranked, ...summary.excluded]"
        :key="candidate.id"
        class="grid min-w-0 gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]"
      >
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <span v-if="candidate.rank !== null" class="font-mono text-xs text-oh-accent-text oh-tabular">#{{ candidate.rank }}</span>
          <span class="break-all font-mono text-xs">{{ candidate.id }}</span>
          <Badge :tone="candidate.state === 'eligible' ? 'accent' : 'neutral'">{{ t('routing.' + candidate.state) }}</Badge>
        </div>
        <ul v-if="candidate.reasons.length" class="space-y-1 text-xs text-oh-secondary">
          <li v-for="(item, index) in candidate.reasons.slice(0, 3)" :key="index" class="break-words">
            {{ reason(item.code) }} <span class="ml-1 font-mono text-oh-muted">{{ item.stage }}</span>
          </li>
        </ul>
        <ul v-else-if="candidate.factors.length" class="space-y-1 text-xs text-oh-secondary">
          <li v-for="(factor, index) in candidate.factors" :key="index" class="break-words">
            {{ factor.name }} <b class="font-mono oh-tabular" :aria-label="!factor.value ? t('routing.unknown') : undefined">{{ factor.value || '—' }}</b> {{ factor.unit }}
            <span class="text-oh-muted">{{ factor.source }} · {{ factor.freshness }}</span>
          </li>
        </ul>
      </article>
    </div>
  </section>
</template>
