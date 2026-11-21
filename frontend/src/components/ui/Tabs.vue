<script setup lang="ts">
import { ref } from 'vue'
import type { UiTabItem } from './types'
import { cn, useUiId } from './utils'

export interface TabsProps {
  modelValue: string
  items: UiTabItem[]
  variant?: 'underline' | 'pill'
  ariaLabel?: string
}

const props = withDefaults(defineProps<TabsProps>(), {
  variant: 'underline',
  ariaLabel: 'Section tabs'
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const tablistId = useUiId('oh-tabs')
const buttonRefs = ref<Array<HTMLButtonElement | null>>([])

function setButtonRef(index: number, element: unknown): void {
  buttonRefs.value[index] = element instanceof HTMLButtonElement ? element : null
}

function selectTab(item: UiTabItem): void {
  if (item.disabled) return
  emit('update:modelValue', item.value)
}

function moveFocus(currentIndex: number, step: number): void {
  const total = props.items.length
  if (total === 0) return
  for (let offset = 1; offset <= total; offset += 1) {
    const candidateIndex = (currentIndex + step * offset + total) % total
    const candidate = props.items[candidateIndex]
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
  } else if (event.key === 'Home') {
    event.preventDefault()
    moveFocus(props.items.length - 1, 1)
  } else if (event.key === 'End') {
    event.preventDefault()
    moveFocus(0, -1)
  }
}
</script>

<template>
  <div
    role="tablist"
    :aria-label="ariaLabel"
    :class="
      cn(
        'flex items-center overflow-x-auto',
        variant === 'underline'
          ? 'gap-5 border-b border-oh-default'
          : 'inline-flex gap-1 rounded-oh-md border border-oh-default bg-oh-inset p-1'
      )
    "
  >
    <button
      v-for="(item, index) in items"
      :id="`${tablistId}-tab-${item.value}`"
      :key="item.value"
      :ref="(el) => setButtonRef(index, el)"
      type="button"
      role="tab"
      :disabled="item.disabled"
      :aria-selected="modelValue === item.value ? 'true' : 'false'"
      :tabindex="modelValue === item.value ? 0 : -1"
      :class="
        cn(
          'oh-focus-ring relative inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[13px] transition-colors duration-150',
          variant === 'underline' && [
            'px-0.5 py-2.5 font-medium',
            modelValue === item.value
              ? 'text-oh-primary font-semibold after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:rounded-full after:bg-oh-accent'
              : 'text-oh-muted hover:text-oh-primary'
          ],
          variant === 'pill' && [
            'rounded-oh-sm px-2.5 py-1 text-xs font-medium',
            modelValue === item.value
              ? 'bg-oh-surface text-oh-primary shadow-oh-xs'
              : 'text-oh-muted hover:text-oh-primary'
          ],
          item.disabled && 'cursor-not-allowed opacity-45'
        )
      "
      @click="selectTab(item)"
      @keydown="handleKeydown($event, index)"
    >
      <span>{{ item.label }}</span>
      <span
        v-if="item.count !== undefined"
        :class="
          cn(
            'oh-tabular text-[11px]',
            modelValue === item.value ? 'text-oh-accent-text' : 'text-oh-muted'
          )
        "
      >
        {{ item.count }}
      </span>
      <span
        v-if="item.badge"
        class="rounded-oh-xs border border-oh-accent bg-oh-accent-subtle px-1 py-px font-mono text-[10px] text-oh-accent-text"
      >
        {{ item.badge }}
      </span>
    </button>
  </div>
</template>
