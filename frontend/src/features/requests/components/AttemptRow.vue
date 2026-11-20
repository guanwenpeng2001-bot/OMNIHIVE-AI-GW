<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui'
import { displayNumber, statusTone, timestamp } from '../presentation'
import type { waterfall } from '../waterfall'

defineProps<{
  row: ReturnType<typeof waterfall>['attempts'][number]
  connectionAccess: boolean
  timezone: string
}>()
const { t, locale } = useI18n()
</script>

<template>
  <li class="grid min-w-0 gap-2 border-b border-oh-subtle py-3 last:border-0 sm:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] sm:items-center">
    <div class="grid gap-1">
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="font-mono text-xs font-semibold text-oh-primary">{{ t('costLedger.requests.attemptOrdinal', { number: row.attempt.number }) }}</span>
        <Badge :tone="statusTone(row.attempt.outcome)" size="xs" dot>{{ t(`costLedger.states.${row.attempt.outcome}`) }}</Badge>
        <Badge v-if="row.attempt.status !== null" size="xs" mono>{{ row.attempt.status }}</Badge>
        <Badge v-if="row.replacement" tone="success" size="xs">{{ t('costLedger.requests.replacement') }}</Badge>
      </div>
      <div class="flex flex-wrap gap-x-2 gap-y-1 text-xs text-oh-muted">
        <span v-if="connectionAccess && (row.attempt.connection_name || row.attempt.connection_id)" class="break-all">{{ row.attempt.connection_name ?? row.attempt.connection_id }}</span>
        <span v-else-if="!connectionAccess">{{ t('costLedger.requests.connectionHidden') }}</span>
        <span>{{ t('costLedger.requests.acceptance', { state: t(`costLedger.states.${row.attempt.acceptance}`) }) }}</span>
      </div>
      <p v-if="row.attempt.reason" class="break-all font-mono text-[11px] text-oh-muted">{{ row.attempt.reason }}</p>
    </div>
    <div class="min-w-0 space-y-1.5">
      <div class="h-2.5 overflow-hidden rounded-oh-xs bg-oh-inset" aria-hidden="true">
        <div
          v-if="row.start !== null"
          class="relative h-full min-w-0.5 rounded-oh-xs"
          :class="row.attempt.abandoned_uncertain || row.width === null ? 'border border-dashed border-oh-accent bg-oh-accent-subtle' : 'bg-oh-accent'"
          :style="{ marginLeft: `${Math.min(row.start, 99.5)}%`, width: `${row.width ?? 0}%` }"
        />
      </div>
      <div class="flex flex-wrap justify-between gap-1.5 font-mono text-[11px] tabular-nums text-oh-muted">
        <time :datetime="row.attempt.started_at || undefined">{{ timestamp(row.attempt.started_at, locale, timezone) }}</time>
        <span>{{ row.attempt.duration_ms === null ? '—' : `${displayNumber(row.attempt.duration_ms, locale)} ms` }}</span>
      </div>
    </div>
  </li>
</template>
