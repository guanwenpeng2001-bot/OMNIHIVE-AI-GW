<script setup lang="ts">
import { computed } from 'vue'
import type { UiSelectOption, UiSize } from './types'
import { cn, useUiId } from './utils'

export interface SelectProps {
  modelValue?: string | number | null
  options: UiSelectOption[]
  id?: string
  label?: string
  placeholder?: string
  hint?: string
  error?: string
  size?: Exclude<UiSize, 'xs'>
  disabled?: boolean
  required?: boolean
  mono?: boolean
}

const props = withDefaults(defineProps<SelectProps>(), {
  modelValue: '',
  id: undefined,
  label: undefined,
  placeholder: undefined,
  hint: undefined,
  error: undefined,
  size: 'md',
  disabled: false,
  required: false,
  mono: false
})

const emit = defineEmits<{
  'update:modelValue': [value: string | number]
}>()

const generatedId = useUiId('oh-select')
const selectId = computed(() => props.id || generatedId)
const hintId = computed(() => `${selectId.value}-hint`)
const errorId = computed(() => `${selectId.value}-error`)

const describedBy = computed(() => {
  if (props.error) return errorId.value
  if (props.hint) return hintId.value
  return undefined
})

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'h-7 text-xs pl-2.5 pr-7'
    case 'lg':
      return 'h-10 text-sm pl-3.5 pr-9'
    default:
      return 'h-9 text-sm pl-3 pr-8'
  }
})

function handleChange(event: Event): void {
  const target = event.target as HTMLSelectElement
  const matchedOption = props.options.find((option) => String(option.value) === target.value)
  emit('update:modelValue', matchedOption ? matchedOption.value : target.value)
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label
      v-if="label"
      :for="selectId"
      class="text-xs font-medium text-oh-secondary"
    >
      {{ label }}
      <span v-if="required" class="ml-0.5 text-oh-status-danger-fg" aria-hidden="true">*</span>
    </label>

    <div class="relative">
      <select
        :id="selectId"
        :value="modelValue ?? ''"
        :disabled="disabled"
        :required="required"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy"
        :class="
          cn(
            'oh-focus-ring w-full appearance-none rounded-oh-md border bg-oh-surface text-oh-primary transition-colors duration-150',
            sizeClasses,
            error
              ? 'border-oh-status-danger-border focus:border-oh-status-danger-fg'
              : 'border-oh-default hover:border-oh-strong focus:border-oh-accent',
            mono && 'font-mono oh-tabular text-[13px]',
            disabled && 'cursor-not-allowed bg-oh-subtle opacity-60'
          )
        "
        @change="handleChange"
      >
        <option v-if="placeholder" value="" disabled>
          {{ placeholder }}
        </option>
        <option
          v-for="option in options"
          :key="String(option.value)"
          :value="String(option.value)"
          :disabled="option.disabled"
        >
          {{ option.label }}
        </option>
      </select>

      <span
        class="pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-oh-muted"
        aria-hidden="true"
      >
        <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.75">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 6.25L8 9.75l3.5-3.5" />
        </svg>
      </span>
    </div>

    <p v-if="error" :id="errorId" role="alert" class="text-xs text-oh-status-danger-fg">
      {{ error }}
    </p>
    <p v-else-if="hint" :id="hintId" class="text-xs text-oh-muted">
      {{ hint }}
    </p>
  </div>
</template>
