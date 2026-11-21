<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { cn, useUiId } from './utils'

export interface CheckboxProps {
  modelValue?: boolean
  indeterminate?: boolean
  id?: string
  label?: string
  description?: string
  disabled?: boolean
}

const props = withDefaults(defineProps<CheckboxProps>(), {
  modelValue: false,
  indeterminate: false,
  id: undefined,
  label: undefined,
  description: undefined,
  disabled: false
})

const emit = defineEmits<{
  'update:modelValue': [checked: boolean]
}>()

const inputRef = ref<HTMLInputElement | null>(null)
const generatedId = useUiId('oh-checkbox')
const checkboxId = computed(() => props.id || generatedId)
const descriptionId = computed(() => `${checkboxId.value}-desc`)

function syncIndeterminate(): void {
  if (inputRef.value) {
    inputRef.value.indeterminate = props.indeterminate
  }
}

onMounted(syncIndeterminate)
watch(() => props.indeterminate, syncIndeterminate)

function handleChange(event: Event): void {
  if (props.disabled) return
  const target = event.target as HTMLInputElement
  emit('update:modelValue', target.checked)
}
</script>

<template>
  <label
    :for="checkboxId"
    :class="
      cn(
        'group inline-flex items-start gap-2.5 select-none',
        disabled ? 'cursor-not-allowed opacity-55' : 'cursor-pointer'
      )
    "
  >
    <span class="relative mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center">
      <input
        :id="checkboxId"
        ref="inputRef"
        type="checkbox"
        :checked="modelValue"
        :disabled="disabled"
        :aria-checked="indeterminate ? 'mixed' : modelValue ? 'true' : 'false'"
        :aria-describedby="description ? descriptionId : undefined"
        class="peer sr-only"
        @change="handleChange"
      />
      <span
        :class="
          cn(
            'inline-flex h-4 w-4 items-center justify-center rounded-oh-xs border transition-colors duration-150',
            'peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[var(--oh-focus-ring)]',
            modelValue || indeterminate
              ? 'border-oh-accent bg-oh-accent text-oh-accent-ink'
              : 'border-oh-strong bg-oh-surface group-hover:border-oh-accent'
          )
        "
        aria-hidden="true"
      >
        <svg
          v-if="indeterminate"
          viewBox="0 0 12 12"
          class="h-3 w-3"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path stroke-linecap="round" d="M2.5 6h7" />
        </svg>
        <svg
          v-else-if="modelValue"
          viewBox="0 0 12 12"
          class="h-3 w-3"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M2.25 6.25l2.5 2.5 5-5.25" />
        </svg>
      </span>
    </span>

    <span v-if="label || description || $slots.default" class="flex min-w-0 flex-col">
      <span class="text-sm font-medium leading-5 text-oh-primary">
        <slot>{{ label }}</slot>
      </span>
      <span v-if="description" :id="descriptionId" class="text-xs leading-4 text-oh-muted">
        {{ description }}
      </span>
    </span>
  </label>
</template>
