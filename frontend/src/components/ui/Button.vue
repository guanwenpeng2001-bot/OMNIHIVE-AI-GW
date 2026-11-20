<script setup lang="ts">
import { computed } from 'vue'
import type { UiButtonVariant, UiSize } from './types'
import { cn } from './utils'

export interface ButtonProps {
  variant?: UiButtonVariant
  size?: UiSize
  type?: 'button' | 'submit' | 'reset'
  disabled?: boolean
  loading?: boolean
  block?: boolean
  mono?: boolean
}

const props = withDefaults(defineProps<ButtonProps>(), {
  variant: 'secondary',
  size: 'md',
  type: 'button',
  disabled: false,
  loading: false,
  block: false,
  mono: false
})

const emit = defineEmits<{
  click: [event: MouseEvent]
}>()

const isUnavailable = computed(() => props.disabled || props.loading)

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'xs':
      return 'h-6 px-2 text-xs gap-1 rounded-oh-sm'
    case 'sm':
      return 'h-7 px-2.5 text-xs gap-1.5 rounded-oh-sm'
    case 'lg':
      return 'h-10 px-4 text-sm gap-2 rounded-oh-md'
    default:
      return 'h-9 px-3.5 text-[13px] gap-1.5 rounded-oh-md'
  }
})

const variantClasses = computed(() => {
  switch (props.variant) {
    case 'primary':
      return 'bg-oh-accent text-oh-accent-ink border border-transparent hover:bg-oh-accent-hover active:bg-oh-accent-active font-semibold shadow-oh-xs'
    case 'danger':
      return 'bg-oh-status-danger-bg text-oh-status-danger-fg border border-oh-status-danger-border hover:opacity-90 font-medium'
    case 'outline-accent':
      return 'bg-oh-accent-subtle text-oh-accent-text border border-oh-accent hover:bg-oh-selected font-medium'
    case 'ghost':
      return 'bg-transparent text-oh-secondary border border-transparent hover:bg-oh-hover hover:text-oh-primary font-medium'
    default:
      return 'bg-oh-surface text-oh-primary border border-oh-default hover:bg-oh-hover hover:border-oh-strong font-medium shadow-oh-xs'
  }
})

function handleClick(event: MouseEvent): void {
  if (isUnavailable.value) {
    event.preventDefault()
    return
  }
  emit('click', event)
}
</script>

<template>
  <button
    :type="type"
    :disabled="isUnavailable"
    :aria-busy="loading ? 'true' : undefined"
    :class="
      cn(
        'oh-focus-ring relative inline-flex select-none items-center justify-center whitespace-nowrap transition-colors duration-150',
        sizeClasses,
        variantClasses,
        block && 'w-full',
        mono && 'font-mono oh-tabular',
        isUnavailable && 'cursor-not-allowed opacity-55'
      )
    "
    @click="handleClick"
  >
    <span
      v-if="loading"
      class="inline-flex h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-current border-r-transparent"
      aria-hidden="true"
    />
    <slot v-else name="leading" />
    <span class="truncate">
      <slot />
    </span>
    <slot name="trailing" />
  </button>
</template>
