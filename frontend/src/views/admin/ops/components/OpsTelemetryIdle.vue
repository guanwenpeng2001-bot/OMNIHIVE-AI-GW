<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'

const props = withDefaults(defineProps<{
  description: string
  mode?: 'baseline' | 'bars' | 'errors'
  observed?: boolean
}>(), { mode: 'baseline', observed: false })
const { t } = useI18n()
const status = computed(() => props.mode === 'errors' && props.observed
  ? '0 SLA ERRORS · OBSERVED WINDOW'
  : 'IDLE · AWAITING TRAFFIC')
</script>

<template>
  <div class="telemetry-idle" role="status">
    <svg class="telemetry-grid" viewBox="0 0 400 180" preserveAspectRatio="none" aria-hidden="true">
      <path d="M0 20H400 M0 60H400 M0 100H400 M0 140H400 M40 0V160 M120 0V160 M200 0V160 M280 0V160 M360 0V160" class="telemetry-guides" />
      <path d="M0 160H400" class="telemetry-baseline" />
      <g v-if="mode === 'bars'" class="telemetry-bars">
        <rect v-for="index in 24" :key="index" :x="index * 16 - 10" y="156" width="5" height="4" rx="1" />
      </g>
      <g v-else class="telemetry-bars">
        <circle v-for="index in 12" :key="index" :cx="index * 32 - 8" cy="160" r="1.5" />
      </g>
    </svg>
    <div class="telemetry-caption">
      <span class="telemetry-status">{{ status }}</span>
      <EmptyState class="telemetry-copy" :title="t('common.noData')" :description="description">
        <template #icon><span aria-hidden="true"></span></template>
      </EmptyState>
    </div>
    <span class="telemetry-zero" aria-hidden="true">0</span>
  </div>
</template>

<style scoped>
.telemetry-idle {
  position: relative;
  display: grid;
  min-height: 180px;
  width: 100%;
  height: 100%;
  place-items: center;
  overflow: hidden;
}
.telemetry-grid { position: absolute; inset: 0; width: 100%; height: 100%; }
.telemetry-guides { fill: none; stroke: var(--border); stroke-width: .5; stroke-dasharray: 3 5; vector-effect: non-scaling-stroke; }
.telemetry-baseline { fill: none; stroke: var(--brand); stroke-width: 1; opacity: .4; vector-effect: non-scaling-stroke; }
.telemetry-bars { fill: var(--brand); opacity: .4; }
.telemetry-caption { position: relative; max-width: 100%; padding: 16px; text-align: center; }
.telemetry-status {
  display: inline-block;
  max-width: 100%;
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 6px 10px;
  background: var(--card);
  color: var(--muted-foreground);
  font-family: var(--font-mono, monospace);
  font-size: 10px;
  letter-spacing: .04em;
}
.telemetry-copy { padding: 0; margin-top: 10px; min-height: 0; background: transparent; border: 0; }
.telemetry-copy :deep(> div:first-child), .telemetry-copy :deep(.empty-state-title) { display: none; }
.telemetry-copy :deep(.empty-state-description) { margin: 0; color: var(--muted-foreground); font-size: 11px; }
.telemetry-zero { position: absolute; bottom: 5px; left: 0; color: var(--muted-foreground); font: 10px monospace; }
</style>
