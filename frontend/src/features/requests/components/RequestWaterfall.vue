<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Card } from '@/components/ui'
import type { Schema } from '../api'
import { displayNumber, timestamp } from '../presentation'
import { waterfall } from '../waterfall'
import AttemptRow from './AttemptRow.vue'

const props = defineProps<{ detail: Schema['RequestsDetail']; connectionAccess: boolean; timezone: string }>()
const { t, locale } = useI18n()
const model = computed(() => waterfall(props.detail))
</script>

<template>
  <Card padding="md" class="min-w-0">
    <section aria-labelledby="wp6-waterfall-heading">
      <div class="flex flex-wrap items-baseline justify-between gap-2 border-b border-oh-subtle pb-3">
        <h2 id="wp6-waterfall-heading" class="text-base font-semibold text-oh-primary">{{ t('wp6.requests.waterfall') }}</h2>
        <p v-if="model.span !== null" class="font-mono text-xs tabular-nums text-oh-muted">
          {{ t('wp6.requests.observedSpan', { duration: displayNumber(model.span, locale) }) }}
        </p>
      </div>
      <ol class="divide-y divide-oh-subtle">
        <li v-for="phase in model.phases" :key="phase.kind" class="grid min-w-0 items-center gap-1 py-2.5 sm:grid-cols-[minmax(0,0.75fr)_minmax(0,1fr)_minmax(0,1fr)] sm:gap-3">
          <div class="flex min-w-0 flex-wrap items-center gap-1.5">
            <span class="h-1.5 w-1.5 rounded-full" :class="phase.event ? 'bg-oh-accent' : 'border border-oh-strong'" aria-hidden="true" />
            <span class="text-sm font-medium text-oh-primary">{{ t(`wp6.milestones.${phase.kind}`) }}</span>
            <span v-if="phase.event?.source === 'upstream_ttft_estimate'" class="text-[11px] text-oh-accent-text">
              {{ t('wp6.requests.estimated') }}
            </span>
          </div>
          <div class="relative h-1.5 rounded-oh-xs bg-oh-inset" aria-hidden="true">
            <span v-if="phase.position !== null" class="absolute inset-y-0 w-1 rounded-oh-xs bg-oh-accent" :style="{ left: `${Math.min(phase.position, 99.5)}%` }" />
          </div>
          <div class="grid justify-items-start text-[11px] text-oh-muted sm:justify-items-end">
            <time v-if="phase.event" :datetime="phase.event.at" class="font-mono tabular-nums">{{ timestamp(phase.event.at, locale, timezone) }}</time>
            <span v-else>{{ t('wp6.requests.unobserved') }}</span>
            <span v-if="phase.relative !== null" class="font-mono tabular-nums">{{ phase.relative >= 0 ? '+' : '' }}{{ displayNumber(phase.relative, locale) }} ms</span>
          </div>
        </li>
      </ol>

      <h3 class="mt-4 text-sm font-semibold text-oh-primary">{{ t('wp6.requests.attempts') }}</h3>
      <ul v-if="model.attempts.length">
        <AttemptRow v-for="row in model.attempts" :key="row.attempt.id" :row="row" :connection-access="connectionAccess" :timezone="timezone" />
      </ul>
      <p v-else class="mt-2 text-sm text-oh-muted">{{ t('wp6.requests.noAttempts') }}</p>

      <div v-if="model.abandoned.length" class="mt-4 rounded-oh-md border border-dashed border-oh-status-warn-border bg-oh-status-warn-bg/40 px-3 pt-3">
        <h3 class="text-sm font-semibold text-oh-primary">{{ t('wp6.requests.abandoned') }}</h3>
        <p class="mt-1 text-xs text-oh-secondary">{{ t('wp6.requests.abandonedDescription') }}</p>
        <ul>
          <AttemptRow v-for="row in model.abandoned" :key="row.attempt.id" :row="row" :connection-access="connectionAccess" :timezone="timezone" />
        </ul>
      </div>
    </section>
  </Card>
</template>
