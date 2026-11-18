<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { cn, useUiId } from './utils'

export interface TooltipProps {
  content?: string
  placement?: 'top' | 'bottom' | 'left' | 'right'
  delayMs?: number
  disabled?: boolean
}

const props = withDefaults(defineProps<TooltipProps>(), {
  content: undefined,
  placement: 'top',
  delayMs: 120,
  disabled: false
})

const visible = ref(false)
const tooltipId = useUiId('oh-tooltip')
let openTimer: ReturnType<typeof setTimeout> | null = null

function clearTimer(): void {
  if (openTimer !== null) {
    clearTimeout(openTimer)
    openTimer = null
  }
}

function openTooltip(immediate = false): void {
  if (props.disabled) return
  clearTimer()
  if (immediate || props.delayMs <= 0) {
    visible.value = true
    return
  }
  openTimer = setTimeout(() => {
    visible.value = true
    openTimer = null
  }, props.delayMs)
}

function closeTooltip(): void {
  clearTimer()
  visible.value = false
}

const positionClasses = computed(() => {
  switch (props.placement) {
    case 'bottom':
      return 'top-full left-1/2 -translate-x-1/2 mt-1.5'
    case 'left':
      return 'right-full top-1/2 -translate-y-1/2 mr-1.5'
    case 'right':
      return 'left-full top-1/2 -translate-y-1/2 ml-1.5'
    default:
      return 'bottom-full left-1/2 -translate-x-1/2 mb-1.5'
  }
})

onBeforeUnmount(clearTimer)
</script>

<template>
  <span
    class="relative inline-flex"
    :aria-describedby="visible && (content || $slots.content) ? tooltipId : undefined"
    @mouseenter="openTooltip(false)"
    @mouseleave="closeTooltip"
    @focusin="openTooltip(true)"
    @focusout="closeTooltip"
    @keydown.esc="closeTooltip"
  >
    <slot />
    <span
      v-if="visible && !disabled && (content || $slots.content)"
      :id="tooltipId"
      role="tooltip"
      :class="
        cn(
          'pointer-events-none absolute z-50 max-w-xs whitespace-normal rounded-oh-md border border-oh-default bg-oh-elevated px-2.5 py-1.5 text-xs leading-4 text-oh-primary shadow-oh-md',
          positionClasses
        )
      "
    >
      <slot name="content">{{ content }}</slot>
    </span>
  </span>
</template>
