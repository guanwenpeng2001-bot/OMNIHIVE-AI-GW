<script setup lang="ts">
import { computed } from 'vue'
import Badge from './Badge.vue'
import Skeleton from './Skeleton.vue'
import type { UiSettlementState } from './types'
import { cn, useUiI18n } from './utils'

export interface StatNumberProps {
  value: number | string | null | undefined
  label?: string
  currency?: string
  prefix?: string
  unit?: string
  size?: 'sm' | 'md' | 'lg'
  compact?: boolean
  maximumFractionDigits?: number
  freeWhenZero?: boolean
  freeLabel?: string
  unknownLabel?: string
  settlementState?: UiSettlementState
  delta?: string
  deltaTone?: 'neutral' | 'success' | 'warning' | 'danger'
  exactTooltip?: string
  loading?: boolean
  accent?: boolean
}

const props = withDefaults(defineProps<StatNumberProps>(), {
  label: undefined,
  currency: undefined,
  prefix: undefined,
  unit: undefined,
  size: 'md',
  compact: false,
  maximumFractionDigits: 4,
  freeWhenZero: false,
  freeLabel: undefined,
  unknownLabel: undefined,
  settlementState: undefined,
  delta: undefined,
  deltaTone: 'neutral',
  exactTooltip: undefined,
  loading: false,
  accent: false
})

const { t } = useUiI18n()
const resolvedFreeLabel = computed(() => props.freeLabel ?? t('ui.stat.free'))
const resolvedUnknownLabel = computed(() => props.unknownLabel ?? t('ui.stat.noVerifiedData'))

const isUnknown = computed(() => {
  if (props.value === null || props.value === undefined) return true
  if (typeof props.value === 'number') return !Number.isFinite(props.value)
  return props.value.trim().length === 0
})

const isExplicitZero = computed(() => {
  if (isUnknown.value) return false
  if (typeof props.value === 'number') return props.value === 0
  const trimmed = props.value!.toString().trim()
  return /^0(\.0+)?$/.test(trimmed)
})

const formattedValue = computed(() => {
  if (isUnknown.value) return '—'
  if (props.freeWhenZero && isExplicitZero.value) return resolvedFreeLabel.value

  if (typeof props.value === 'string') {
    return `${props.currency ?? ''}${props.prefix ?? ''}${props.value}`
  }

  const numeric = props.value as number
  const formattedNumber = props.compact
    ? new Intl.NumberFormat('en-US', {
        notation: 'compact',
        maximumFractionDigits: 2
      }).format(numeric)
    : new Intl.NumberFormat('en-US', {
        maximumFractionDigits: props.maximumFractionDigits
      }).format(numeric)

  return `${props.currency ?? ''}${props.prefix ?? ''}${formattedNumber}`
})

const fullValueTitle = computed(() => {
  if (props.exactTooltip) return props.exactTooltip
  if (isUnknown.value) return resolvedUnknownLabel.value
  if (typeof props.value === 'number' && props.compact) {
    return `${props.currency ?? ''}${props.prefix ?? ''}${props.value.toLocaleString('en-US')}${
      props.unit ? ` ${props.unit}` : ''
    }`
  }
  return undefined
})

const settlementMeta = computed(() => {
  switch (props.settlementState) {
    case 'settled':
      return { label: t('ui.stat.settled'), tone: 'success' as const }
    case 'estimated':
      return { label: t('ui.stat.estimated'), tone: 'warning' as const }
    case 'reserved':
      return { label: t('ui.stat.reserved'), tone: 'info' as const }
    case 'pending':
      return { label: t('ui.stat.pendingCheck'), tone: 'unknown' as const }
    default:
      return null
  }
})

const valueSizeClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'text-[13px] leading-[18px] font-medium'
    case 'lg':
      return 'text-[28px] leading-[34px] font-semibold tracking-tight'
    default:
      return 'text-xl leading-7 font-semibold tracking-tight'
  }
})
</script>

<template>
  <Skeleton v-if="loading" :variant="size === 'sm' ? 'line' : 'stat'" />

  <div v-else class="inline-flex min-w-0 flex-col gap-1">
    <span v-if="label" class="oh-eyebrow truncate">{{ label }}</span>

    <div class="flex flex-wrap items-baseline gap-1.5">
      <span
        :title="fullValueTitle"
        :aria-label="isUnknown ? resolvedUnknownLabel : undefined"
        :class="
          cn(
            'oh-tabular',
            valueSizeClasses,
            isUnknown
              ? 'text-oh-muted'
              : accent
                ? 'text-oh-accent-text'
                : 'text-oh-primary'
          )
        "
      >
        {{ formattedValue }}
      </span>

      <span
        v-if="!isUnknown && unit && !(freeWhenZero && isExplicitZero)"
        class="font-mono text-xs text-oh-muted"
      >
        {{ unit }}
      </span>

      <Badge
        v-if="settlementMeta"
        :tone="settlementMeta.tone"
        size="xs"
        mono
      >
        {{ settlementMeta.label }}
      </Badge>
    </div>

    <div v-if="delta || $slots.sublabel" class="flex items-center gap-1.5 text-xs">
      <span
        v-if="delta"
        :class="
          cn(
            'oh-tabular font-medium',
            deltaTone === 'success' && 'text-oh-status-ok-fg',
            deltaTone === 'warning' && 'text-oh-status-warn-fg',
            deltaTone === 'danger' && 'text-oh-status-danger-fg',
            deltaTone === 'neutral' && 'text-oh-muted'
          )
        "
      >
        {{ delta }}
      </span>
      <slot name="sublabel" />
    </div>
  </div>
</template>
