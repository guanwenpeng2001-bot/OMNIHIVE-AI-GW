<script setup lang="ts">
import { computed } from 'vue'
import { cn } from './utils'

export interface CardProps {
  as?: string
  variant?: 'default' | 'subtle' | 'interactive' | 'selected'
  padding?: 'none' | 'sm' | 'md' | 'lg'
  eyebrow?: string
  title?: string
  subtitle?: string
}

const props = withDefaults(defineProps<CardProps>(), {
  as: 'section',
  variant: 'default',
  padding: 'md',
  eyebrow: undefined,
  title: undefined,
  subtitle: undefined
})

const emit = defineEmits<{
  click: [event: MouseEvent]
}>()

const variantClasses = computed(() => {
  switch (props.variant) {
    case 'subtle':
      return 'bg-oh-subtle border-oh-subtle'
    case 'interactive':
      return 'bg-oh-surface border-oh-default hover:border-oh-strong hover:bg-oh-hover/40 cursor-pointer transition-colors duration-150 shadow-oh-xs'
    case 'selected':
      return 'bg-oh-selected border-oh-accent shadow-oh-xs'
    default:
      return 'bg-oh-surface border-oh-default shadow-oh-xs'
  }
})

const paddingClasses = computed(() => {
  switch (props.padding) {
    case 'none':
      return 'p-0'
    case 'sm':
      return 'p-3.5'
    case 'lg':
      return 'p-6'
    default:
      return 'p-5'
  }
})
</script>

<template>
  <component
    :is="as"
    :tabindex="variant === 'interactive' ? 0 : undefined"
    :class="
      cn(
        'rounded-oh-lg border text-oh-primary',
        variant === 'interactive' && 'oh-focus-ring',
        variantClasses
      )
    "
    @click="emit('click', $event)"
    @keydown.enter="variant === 'interactive' && emit('click', $event as unknown as MouseEvent)"
  >
    <header
      v-if="eyebrow || title || subtitle || $slots.header || $slots.actions"
      class="flex items-start justify-between gap-4 border-b border-oh-subtle px-5 py-3.5"
    >
      <div class="min-w-0 flex-1">
        <slot name="header">
          <p v-if="eyebrow" class="oh-eyebrow mb-0.5">{{ eyebrow }}</p>
          <h3 v-if="title" class="truncate text-[15px] font-semibold leading-5 text-oh-primary">
            {{ title }}
          </h3>
          <p v-if="subtitle" class="mt-0.5 text-xs text-oh-muted">
            {{ subtitle }}
          </p>
        </slot>
      </div>
      <div v-if="$slots.actions" class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </div>
    </header>

    <div :class="paddingClasses">
      <slot />
    </div>

    <footer
      v-if="$slots.footer"
      class="border-t border-oh-subtle bg-oh-subtle/50 px-5 py-3 rounded-b-oh-lg"
    >
      <slot name="footer" />
    </footer>
  </component>
</template>
