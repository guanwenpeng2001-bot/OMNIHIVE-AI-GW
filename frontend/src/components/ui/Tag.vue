<script setup lang="ts">
import { cn } from './utils'

export interface TagProps {
  active?: boolean
  removable?: boolean
  disabled?: boolean
  mono?: boolean
  removeLabel?: string
}

withDefaults(defineProps<TagProps>(), {
  active: false,
  removable: false,
  disabled: false,
  mono: false,
  removeLabel: 'Remove filter'
})

const emit = defineEmits<{
  click: [event: MouseEvent]
  remove: []
}>()
</script>

<template>
  <span
    :class="
      cn(
        'inline-flex h-7 items-center gap-1.5 rounded-oh-pill border px-2.5 text-xs font-medium transition-colors duration-150',
        active
          ? 'border-oh-accent bg-oh-accent-subtle text-oh-accent-text'
          : 'border-oh-default bg-oh-surface text-oh-secondary hover:border-oh-strong hover:text-oh-primary',
        mono && 'font-mono oh-tabular',
        disabled && 'opacity-50'
      )
    "
    @click="!disabled && emit('click', $event)"
  >
    <slot name="leading" />
    <span class="truncate"><slot /></span>
    <button
      v-if="removable"
      type="button"
      :disabled="disabled"
      :aria-label="removeLabel"
      class="oh-focus-ring -mr-1 inline-flex h-4 w-4 items-center justify-center rounded-full text-current opacity-70 hover:bg-oh-hover hover:opacity-100"
      @click.stop="!disabled && emit('remove')"
    >
      <svg viewBox="0 0 12 12" class="h-3 w-3" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" d="M3 3l6 6M9 3l-6 6" />
      </svg>
    </button>
  </span>
</template>
