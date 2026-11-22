<script setup lang="ts">
import { computed, ref } from 'vue'
import type { UiSize } from './types'
import { cn, useUiI18n, useUiId } from './utils'

export interface InputProps {
  modelValue?: string | number | null
  id?: string
  label?: string
  type?: 'text' | 'search' | 'password' | 'email' | 'url' | 'number'
  placeholder?: string
  hint?: string
  error?: string
  size?: Exclude<UiSize, 'xs'>
  disabled?: boolean
  readonly?: boolean
  required?: boolean
  mono?: boolean
  clearable?: boolean
  revealable?: boolean
  autocomplete?: string
  maxlength?: number
}

const props = withDefaults(defineProps<InputProps>(), {
  modelValue: '',
  id: undefined,
  label: undefined,
  type: 'text',
  placeholder: undefined,
  hint: undefined,
  error: undefined,
  size: 'md',
  disabled: false,
  readonly: false,
  required: false,
  mono: false,
  clearable: false,
  revealable: false,
  autocomplete: undefined,
  maxlength: undefined
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
  clear: []
  keydown: [event: KeyboardEvent]
  blur: [event: FocusEvent]
  focus: [event: FocusEvent]
}>()

const inputRef = ref<HTMLInputElement | null>(null)
const revealed = ref(false)
const generatedId = useUiId('oh-input')
const inputId = computed(() => props.id || generatedId)
const hintId = computed(() => `${inputId.value}-hint`)
const errorId = computed(() => `${inputId.value}-error`)
const { t } = useUiI18n()

const effectiveType = computed(() => {
  if (props.type === 'password' && props.revealable && revealed.value) {
    return 'text'
  }
  return props.type
})

const describedBy = computed(() => {
  const ids: string[] = []
  if (props.error) ids.push(errorId.value)
  else if (props.hint) ids.push(hintId.value)
  return ids.length > 0 ? ids.join(' ') : undefined
})

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'sm':
      return 'h-7 text-xs px-2.5'
    case 'lg':
      return 'h-10 text-sm px-3.5'
    default:
      return 'h-9 text-sm px-3'
  }
})

const hasValue = computed(
  () => props.modelValue !== null && props.modelValue !== undefined && String(props.modelValue).length > 0
)

function handleInput(event: Event): void {
  const target = event.target as HTMLInputElement
  emit('update:modelValue', target.value)
}

function handleClear(): void {
  emit('update:modelValue', '')
  emit('clear')
  inputRef.value?.focus()
}

defineExpose({
  focus: () => inputRef.value?.focus(),
  inputRef
})
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <div v-if="label || $slots.labelRight" class="flex items-center justify-between gap-2">
      <label
        v-if="label"
        :for="inputId"
        class="text-xs font-medium text-oh-secondary"
      >
        {{ label }}
        <span v-if="required" class="ml-0.5 text-oh-status-danger-fg" aria-hidden="true">*</span>
      </label>
      <slot name="labelRight" />
    </div>

    <div
      :class="
        cn(
          'group relative flex items-center rounded-oh-md border bg-oh-surface transition-colors duration-150',
          error
            ? 'border-oh-status-danger-border focus-within:border-oh-status-danger-fg'
            : 'border-oh-default hover:border-oh-strong focus-within:border-oh-accent',
          disabled && 'cursor-not-allowed bg-oh-subtle opacity-60'
        )
      "
    >
      <span
        v-if="$slots.prefix"
        class="flex shrink-0 items-center pl-3 text-oh-muted"
      >
        <slot name="prefix" />
      </span>

      <input
        :id="inputId"
        ref="inputRef"
        :type="effectiveType"
        :value="modelValue ?? ''"
        :placeholder="placeholder"
        :disabled="disabled"
        :readonly="readonly"
        :required="required"
        :autocomplete="autocomplete"
        :maxlength="maxlength"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="describedBy"
        :class="
          cn(
            'w-full min-w-0 bg-transparent text-oh-primary placeholder:text-oh-muted focus:outline-none',
            sizeClasses,
            Boolean($slots.prefix) && 'pl-2',
            (clearable || revealable || Boolean($slots.suffix)) && 'pr-2',
            mono && 'font-mono oh-tabular text-[13px]',
            disabled && 'cursor-not-allowed'
          )
        "
        @input="handleInput"
        @keydown="emit('keydown', $event)"
        @blur="emit('blur', $event)"
        @focus="emit('focus', $event)"
      />

      <div
        v-if="(clearable && hasValue && !disabled && !readonly) || (type === 'password' && revealable) || $slots.suffix"
        class="flex shrink-0 items-center gap-1 pr-2 text-oh-muted"
      >
        <button
          v-if="clearable && hasValue && !disabled && !readonly"
          type="button"
          class="oh-focus-ring inline-flex h-5 w-5 items-center justify-center rounded-oh-xs text-oh-muted hover:bg-oh-hover hover:text-oh-primary"
          :aria-label="t('ui.input.clear')"
          @click="handleClear"
        >
          <svg viewBox="0 0 16 16" class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" d="M4.5 4.5l7 7M11.5 4.5l-7 7" />
          </svg>
        </button>

        <button
          v-if="type === 'password' && revealable"
          type="button"
          class="oh-focus-ring inline-flex h-6 items-center rounded-oh-xs px-1.5 font-mono text-[11px] text-oh-secondary hover:bg-oh-hover hover:text-oh-primary"
          :aria-label="revealed ? t('ui.input.hideSecret') : t('ui.input.revealSecret')"
          :aria-pressed="revealed ? 'true' : 'false'"
          @click="revealed = !revealed"
        >
          {{ revealed ? t('ui.input.hide') : t('ui.input.show') }}
        </button>

        <slot name="suffix" />
      </div>
    </div>

    <p
      v-if="error"
      :id="errorId"
      role="alert"
      class="text-xs text-oh-status-danger-fg"
    >
      {{ error }}
    </p>
    <p
      v-else-if="hint"
      :id="hintId"
      class="text-xs text-oh-muted"
    >
      {{ hint }}
    </p>
  </div>
</template>
