<template>
  <div class="stat-card oh-stat-card group relative overflow-hidden" :data-variant="iconVariant">
    <div class="min-w-0 flex-1">
      <div class="flex items-center justify-between gap-2">
        <p class="stat-label truncate text-xs">{{ title }}</p>
        <component v-if="icon" :is="icon" class="h-4 w-4 shrink-0 opacity-45" aria-hidden="true" />
      </div>
      <div class="mt-2 flex items-baseline justify-between gap-2">
        <div class="flex min-w-0 items-baseline gap-2">
          <p class="stat-value font-mono tabular-nums tracking-tight" :title="String(formattedValue)">{{ formattedValue }}</p>
          <span v-if="change !== undefined" :class="['stat-trend oh-trend-pill tabular-nums', trendClass]">
            <Icon
              v-if="changeType !== 'neutral'"
              name="arrowUp"
              size="xs"
              :class="changeType === 'down' && 'rotate-180'"
            />
            {{ formattedChange }}
          </span>
        </div>
        <svg class="oh-stat-sparkline" :class="sparklineColorClass" viewBox="0 0 56 22" fill="none" aria-hidden="true">
          <path :d="sparklinePath" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Component } from 'vue'
import Icon from '@/components/icons/Icon.vue'

type ChangeType = 'up' | 'down' | 'neutral'
type IconVariant = 'primary' | 'success' | 'warning' | 'danger'

interface Props {
  title: string
  value: number | string
  icon?: Component
  iconVariant?: IconVariant
  change?: number
  changeType?: ChangeType
  formatValue?: (value: number | string) => string
}

const props = withDefaults(defineProps<Props>(), {
  changeType: 'neutral',
  iconVariant: 'primary'
})

const formattedValue = computed(() => {
  if (props.formatValue) {
    return props.formatValue(props.value)
  }
  if (typeof props.value === 'number') {
    return props.value.toLocaleString()
  }
  return props.value
})

const formattedChange = computed(() => {
  if (props.change === undefined) return ''
  const absChange = Math.abs(props.change)
  return `${absChange}%`
})

const trendClass = computed(() => {
  const classes: Record<ChangeType, string> = {
    up: 'stat-trend-up',
    down: 'stat-trend-down',
    neutral: 'text-[var(--muted-foreground)]'
  }
  return classes[props.changeType]
})

const sparklineColorClass = computed(() => {
  if (props.changeType === 'up') return 'text-emerald-500'
  if (props.changeType === 'down') return 'text-rose-500'
  const variantMap: Record<IconVariant, string> = {
    primary: 'text-amber-500',
    success: 'text-emerald-500',
    warning: 'text-amber-500',
    danger: 'text-rose-500'
  }
  return variantMap[props.iconVariant]
})

const sparklinePath = computed(() => {
  if (props.changeType === 'down') {
    return 'M2 5 C14 7, 22 12, 34 11 C44 10, 48 16, 54 18'
  }
  return 'M2 17 C12 15, 22 10, 32 12 C42 13, 48 6, 54 4'
})
</script>

<style scoped>
.oh-stat-card {
  border-radius: var(--radius-md, 12px);
  border: 1px solid var(--border, #e9e9ee);
  background: var(--card, #ffffff);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.03), 0 1px 2px rgba(0, 0, 0, 0.02);
  transition: transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1),
    box-shadow 0.22s cubic-bezier(0.2, 0.8, 0.2, 1),
    border-color 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
}

.dark .oh-stat-card {
  border-color: var(--border, #232329);
  background: var(--card, #131316);
}

.oh-stat-card:hover {
  transform: translateY(-2px);
  border-color: color-mix(in srgb, var(--brand, #f59e0b) 42%, var(--border, #e9e9ee));
  box-shadow: 0 12px 28px -6px rgba(0, 0, 0, 0.08),
    0 0 0 1px color-mix(in srgb, var(--brand, #f59e0b) 16%, transparent);
}

.oh-trend-pill {
  padding: 1px 6px;
  border-radius: 9999px;
  background: color-mix(in srgb, currentColor 12%, transparent);
}

.oh-stat-sparkline {
  width: 52px;
  height: 20px;
  flex-shrink: 0;
  opacity: 0.78;
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.oh-stat-card:hover .oh-stat-sparkline {
  opacity: 1;
  transform: scale(1.04);
}
</style>
