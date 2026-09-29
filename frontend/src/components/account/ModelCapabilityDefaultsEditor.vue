<template>
  <details :open="error || undefined" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-200">{{ labels.title }}</summary>
    <div class="mt-3 space-y-2">
      <p :id="`${id}-hint`" class="input-hint">{{ labels.hint }}</p>
      <label :for="id" class="input-label">{{ labels.input }}</label>
      <textarea :id="id" :value="modelValue" rows="8" spellcheck="false" autocapitalize="off" autocomplete="off"
        class="input font-mono text-xs" :aria-invalid="!!error" :aria-describedby="`${id}-hint ${id}-error`" :aria-errormessage="error ? `${id}-error` : undefined"
        :placeholder="labels.placeholder" @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)" />
      <p :id="`${id}-error`" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error ? labels.invalid : '' }}</p>
      <details class="text-xs text-secondary">
        <summary class="cursor-pointer">{{ labels.example }}</summary>
        <p class="my-2">{{ labels.binding }}</p>
        <pre class="max-h-64 overflow-auto rounded-lg bg-gray-50 p-3 dark:bg-dark-800">{{ example }}</pre>
      </details>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseModelCapabilityDefaults } from './modelCapabilityDefaults'

const props = defineProps<{ modelValue: string }>()
defineEmits<{ 'update:modelValue': [value: string] }>()
const id = useId()
const { t } = useI18n()
const labels = computed(() => ({
  title: t('capabilityDefaults.title'),
  input: t('capabilityDefaults.input'),
  hint: t('capabilityDefaults.hint'),
  invalid: t('capabilityDefaults.invalid'),
  placeholder: t('capabilityDefaults.placeholder'),
  example: t('capabilityDefaults.example'),
  binding: t('capabilityDefaults.binding')
}))
const example = JSON.stringify({
  schema_version: 1,
  reason: 'Deployment configuration verified by the deployment owner',
  models: {
    'original-private-model-id': {
      task_type: 'text_generation', max_input_tokens: 32768, max_output_tokens: 4096,
      input_modalities: ['text'], input_modalities_status: 'complete', output_modalities: ['text'],
      reasoning: true, reasoning_control: { kind: 'budget', budget_min: 0, budget_max: 2048 },
      supports_function_calling: false
    }
  }
}, null, 2)
const error = computed(() => {
  try { parseModelCapabilityDefaults(props.modelValue); return false } catch { return true }
})
</script>
