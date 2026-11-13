<template>
  <div class="w-full">
    <label v-if="label" :for="id" class="input-label mb-1.5 block">
      {{ label }}
      <span v-if="required" class="text-red-500">*</span>
    </label>
    <div class="relative">
      <textarea
        :id="id"
        ref="textAreaRef"
        :value="modelValue"
        :disabled="disabled"
        :required="required"
        :placeholder="placeholderText"
        :readonly="readonly"
        :aria-invalid="error ? true : undefined"
        :aria-describedby="id && (error || hint) ? `${id}-message` : undefined"
        :rows="rows"
        :class="[
          'input w-full min-h-[80px] transition-all duration-200 resize-y',
          error ? 'input-error ring-2 ring-red-500/20' : '',
          disabled ? 'cursor-not-allowed bg-[var(--surface-2)] opacity-60 ' : ''
        ]"
        @input="onInput"
        @change="$emit('change', ($event.target as HTMLTextAreaElement).value)"
        @blur="$emit('blur', $event)"
        @focus="$emit('focus', $event)"
      ></textarea>
    </div>
    <!-- Hint / Error Text -->
    <p v-if="error" :id="id ? `${id}-message` : undefined" class="input-error-text mt-1.5" role="alert">
      {{ error }}
    </p>
    <p v-else-if="hint" :id="id ? `${id}-message` : undefined" class="input-hint mt-1.5">
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

interface Props {
  modelValue: string | null | undefined
  label?: string
  placeholder?: string
  disabled?: boolean
  required?: boolean
  readonly?: boolean
  error?: string
  hint?: string
  id?: string
  rows?: number | string
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  required: false,
  readonly: false,
  rows: 3
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'change', value: string): void
  (e: 'blur', event: FocusEvent): void
  (e: 'focus', event: FocusEvent): void
}>()

const textAreaRef = ref<HTMLTextAreaElement | null>(null)
const placeholderText = computed(() => props.placeholder || '')

const onInput = (event: Event) => {
  const value = (event.target as HTMLTextAreaElement).value
  emit('update:modelValue', value)
}

// Expose focus method
defineExpose({
  focus: () => textAreaRef.value?.focus(),
  select: () => textAreaRef.value?.select()
})
</script>

<style scoped>
.input { background: var(--card); color: var(--foreground); border: 1px solid var(--border); border-radius: 8px; transition: border-color .15s, box-shadow .15s; }
.input::placeholder { color: var(--muted-foreground); opacity: .8; }
.input:focus { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 18%, transparent); }
.input[aria-invalid="true"] { border-color: var(--bad, #dc2626); }
.input[aria-invalid="true"]:focus { box-shadow: 0 0 0 3px color-mix(in srgb, var(--bad, #dc2626) 18%, transparent); }
.input:disabled { background: var(--surface-2); }
.input-label { color: var(--foreground); }
.input-hint { color: var(--muted-foreground); }
@media (prefers-reduced-motion: reduce) { .input { transition: none; } }
</style>
