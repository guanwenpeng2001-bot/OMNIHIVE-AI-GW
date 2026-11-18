<script setup lang="ts">
import { computed } from 'vue'
import type { UiBadgeVariant, UiTone } from './types'
import { cn } from './utils'

export interface BadgeProps {
  tone?: UiTone
  variant?: UiBadgeVariant
  size?: 'xs' | 'sm' | 'md'
  mono?: boolean
  dot?: boolean
}

const props = withDefaults(defineProps<BadgeProps>(), {
  tone: 'neutral',
  variant: 'subtle',
  size: 'sm',
  mono: false,
  dot: false
})

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'xs':
      return 'px-1.5 py-0.5 text-[11px] leading-3.5 gap-1 rounded-oh-xs'
    case 'md':
      return 'px-2.5 py-1 text-xs leading-4 gap-1.5 rounded-oh-sm'
    default:
      return 'px-2 py-0.5 text-xs leading-4 gap-1.5 rounded-oh-sm'
  }
})

const toneClasses = computed(() => {
  if (props.variant === 'solid') {
    switch (props.tone) {
      case 'accent':
        return 'bg-oh-accent text-oh-accent-ink border border-transparent font-semibold'
      case 'success':
        return 'bg-oh-status-ok-fg text-white dark:text-oh-accent-ink border border-transparent font-semibold'
      case 'warning':
        return 'bg-oh-accent text-oh-accent-ink border border-transparent font-semibold'
      case 'danger':
        return 'bg-oh-status-danger-fg text-white border border-transparent font-semibold'
      case 'info':
        return 'bg-oh-status-info-fg text-white border border-transparent font-semibold'
      default:
        return 'bg-oh-primary text-oh-surface border border-transparent font-medium'
    }
  }

  const transparentBg = props.variant === 'outline'
  switch (props.tone) {
    case 'accent':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-accent-subtle',
        'text-oh-accent-text border border-oh-accent'
      )
    case 'success':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-ok-bg',
        'text-oh-status-ok-fg border border-oh-status-ok-border'
      )
    case 'warning':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-warn-bg',
        'text-oh-status-warn-fg border border-oh-status-warn-border'
      )
    case 'danger':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-danger-bg',
        'text-oh-status-danger-fg border border-oh-status-danger-border'
      )
    case 'info':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-info-bg',
        'text-oh-status-info-fg border border-oh-status-info-border'
      )
    case 'unknown':
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-unknown-bg',
        'text-oh-status-unknown-fg border border-dashed border-oh-status-unknown-border'
      )
    default:
      return cn(
        transparentBg ? 'bg-transparent' : 'bg-oh-status-neutral-bg',
        'text-oh-status-neutral-fg border border-oh-status-neutral-border'
      )
  }
})
</script>

<template>
  <span
    :class="
      cn(
        'inline-flex items-center whitespace-nowrap font-medium',
        sizeClasses,
        toneClasses,
        mono && 'font-mono oh-tabular'
      )
    "
  >
    <span
      v-if="dot"
      class="h-1.5 w-1.5 shrink-0 rounded-full bg-current"
      aria-hidden="true"
    />
    <slot name="icon" />
    <span><slot /></span>
  </span>
</template>
