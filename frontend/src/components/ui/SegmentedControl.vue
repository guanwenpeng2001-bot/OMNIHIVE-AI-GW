<script setup lang="ts">
import { ref } from 'vue'
import type { UiSegmentOption } from './types'
import { cn } from './utils'

export interface SegmentedControlProps {
  modelValue: string
  options: UiSegmentOption[]
  size?: 'sm' | 'md'
  ariaLabel?: string
  mono?: boolean
}

const props = withDefaults(defineProps<SegmentedControlProps>(), {
  size: 'md',
  ariaLabel: 'View mode',
  mono: false
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const buttonRefs = ref<Array<HTMLButtonElement | null>>([])

function setButtonRef(index: number, element: unknown): void {
  buttonRefs.value[index] = element instanceof HTMLButtonElement ? element : null
}

function selectOption(option: UiSegmentOption): void {
  if (option.disabled) return
  emit('update:modelValue', option.value)
}

function moveFocus(currentIndex: number, step: number): void {
  const total = props.options.length
  if (total === 0) return
  for (let offset = 1; offset <= total; offset += 1) {
    const candidateIndex = (currentIndex + step * offset + total) % total
    const candidate = props.options[candidateIndex]
    if (!candidate.disabled) {
      buttonRefs.value[candidateIndex]?.focus()
      emit('update:modelValue', candidate.value)
      return
    }
  }
}

function handleKeydown(event: KeyboardEvent, index: number): void {
  if (event.key === 'ArrowRight') {
    event.preventDefault()
    moveFocus(index, 1)
  } else if (event.key === 'ArrowLeft') {
    event.preventDefault()
    moveFocus(index, -1)
  }
}
</script>

<template>
  <div
    role="radiogroup"
    :aria-label="ariaLabel"
    :class="
      cn(
        'inline-flex items-center rounded-oh-md border border-oh-default bg-oh-inset p-0.5 gap-0.5',
        mono && 'font-mono uppercase tracking-wider'
      )
    "
  >
    <button
      v-for="(option, index) in options"
      :key="option.value"
      :ref="(el) => setButtonRef(index, el)"
      type="button"
      role="radio"
      :disabled="option.disabled"
      :aria-checked="modelValue === option.value ? 'true' : 'false'"
      :tabindex="modelValue === option.value ? 0 : -1"
      :class="
        cn(
          'oh-focus-ring inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-oh-sm transition-all duration-150',
          size === 'sm' ? 'h-6 px-2.5 text-[11px]' : 'h-7 px-3 text-xs',
          modelValue === option.value
            ? 'bg-oh-surface text-oh-primary font-semibold shadow-oh-xs border border-oh-subtle'
            : 'border border-transparent text-oh-muted hover:text-oh-primary font-medium',
          option.disabled && 'cursor-not-allowed opacity-45'
        )
      "
      @click="selectOption(option)"
      @keydown="handleKeydown($event, index)"
    >
      <span>{{ option.label }}</span>
      <span
        v-if="option.count !== undefined"
        class="oh-tabular text-[11px] text-oh-muted"
      >
        {{ option.count }}
      </span>
    </button>
  </div>
</template>
