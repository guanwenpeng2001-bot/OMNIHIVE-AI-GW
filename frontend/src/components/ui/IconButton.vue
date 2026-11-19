<script setup lang="ts">
import { computed } from 'vue'
import type { UiButtonVariant, UiSize } from './types'
import { cn } from './utils'

export interface IconButtonProps {
  ariaLabel: string
  label?: string
  variant?: UiButtonVariant
  size?: Exclude<UiSize, 'lg'>
  type?: 'button' | 'submit' | 'reset'
  disabled?: boolean
  loading?: boolean
  active?: boolean
}

const props = withDefaults(defineProps<IconButtonProps>(), {
  label: undefined,
  variant: 'ghost',
  size: 'md',
  type: 'button',
  disabled: false,
  loading: false,
  active: false
})

const resolvedLabel = computed(() => props.ariaLabel || props.label || '')

const emit = defineEmits<{
  click: [event: MouseEvent]
}>()

const isUnavailable = computed(() => props.disabled || props.loading)

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'xs':
      return 'h-6 w-6 rounded-oh-sm text-xs'
    case 'sm':
      return 'h-7 w-7 rounded-oh-sm text-xs'
    default:
      return 'h-9 w-9 rounded-oh-md text-sm'
  }
})

const variantClasses = computed(() => {
  if (props.active) {
    return 'bg-oh-selected text-oh-accent-text border border-oh-accent'
  }
  switch (props.variant) {
    case 'primary':
      return 'bg-oh-accent text-oh-accent-ink border border-transparent hover:bg-oh-accent-hover'
    case 'secondary':
      return 'bg-oh-surface text-oh-secondary border border-oh-default hover:bg-oh-hover hover:text-oh-primary'
    case 'danger':
      return 'bg-oh-status-danger-bg text-oh-status-danger-fg border border-oh-status-danger-border hover:opacity-90'
    case 'outline-accent':
      return 'bg-oh-accent-subtle text-oh-accent-text border border-oh-accent hover:bg-oh-selected'
    default:
      return 'bg-transparent text-oh-secondary border border-transparent hover:bg-oh-hover hover:text-oh-primary'
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
    :aria-label="resolvedLabel"
    :title="resolvedLabel"
    :disabled="isUnavailable"
    :aria-busy="loading ? 'true' : undefined"
    :aria-pressed="active ? 'true' : undefined"
    :class="
      cn(
        'oh-focus-ring inline-flex shrink-0 items-center justify-center transition-colors duration-150',
        sizeClasses,
        variantClasses,
        isUnavailable && 'cursor-not-allowed opacity-50'
      )
    "
    @click="handleClick"
  >
    <span
      v-if="loading"
      class="inline-flex h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-r-transparent"
      aria-hidden="true"
    />
    <slot v-else />
  </button>
</template>
