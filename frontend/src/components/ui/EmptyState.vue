<script setup lang="ts">
import { computed } from 'vue'
import Button from './Button.vue'
import { cn, useUiI18n } from './utils'

export interface EmptyStateProps {
  title: string
  description?: string
  variant?: 'empty' | 'filtered'
  compact?: boolean
  actionLabel?: string
  clearFilterLabel?: string
}

const props = withDefaults(defineProps<EmptyStateProps>(), {
  description: undefined,
  variant: 'empty',
  compact: false,
  actionLabel: undefined,
  clearFilterLabel: undefined
})

const { t } = useUiI18n()
const resolvedClearFilterLabel = computed(
  () => props.clearFilterLabel ?? t('ui.empty.clearAllFilters')
)

const emit = defineEmits<{
  action: []
  clearFilters: []
}>()
</script>

<template>
  <div
    :class="
      cn(
        'flex flex-col items-center justify-center text-center',
        compact ? 'px-4 py-8' : 'rounded-oh-lg border border-dashed border-oh-default bg-oh-subtle/40 px-6 py-12'
      )
    "
  >
    <div
      class="mb-3.5 inline-flex h-11 w-11 items-center justify-center rounded-oh-md border border-oh-default bg-oh-surface text-oh-muted"
      aria-hidden="true"
    >
      <slot name="icon">
        <svg
          v-if="variant === 'filtered'"
          viewBox="0 0 24 24"
          class="h-5 w-5"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M3.5 5.5h17l-6.5 7.25v5.25l-4 2v-7.25L3.5 5.5z" />
        </svg>
        <svg
          v-else
          viewBox="0 0 28 28"
          class="h-6 w-6"
          fill="none"
          stroke="currentColor"
          stroke-width="1.4"
        >
          <path d="M14 3.5l8.5 4.9v9.8L14 23.1l-8.5-4.9V8.4L14 3.5z" />
          <path d="M14 8.2l4.4 2.55v5.1L14 18.4l-4.4-2.55v-5.1L14 8.2z" stroke="var(--oh-accent)" />
        </svg>
      </slot>
    </div>

    <h3 class="text-sm font-semibold text-oh-primary">
      {{ title }}
    </h3>
    <p v-if="description" class="mt-1 max-w-md text-xs leading-5 text-oh-muted">
      {{ description }}
    </p>

    <div
      v-if="$slots.actions || actionLabel || variant === 'filtered'"
      class="mt-4 flex flex-wrap items-center justify-center gap-2"
    >
      <slot name="actions">
        <Button
          v-if="variant === 'filtered'"
          variant="secondary"
          size="sm"
          @click="emit('clearFilters')"
        >
          {{ resolvedClearFilterLabel }}
        </Button>
        <Button
          v-else-if="actionLabel"
          variant="primary"
          size="sm"
          @click="emit('action')"
        >
          {{ actionLabel }}
        </Button>
      </slot>
    </div>
  </div>
</template>
