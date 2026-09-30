<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import type { Account } from '@/types'
import type { ModelMappingEntry, ModelRestrictionError } from '@/composables/useModelWhitelist'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
  modelValue: string[]
  mappings: ModelMappingEntry[]
  presets?: (ModelMappingEntry & { label?: string })[]
}>(), { presets: () => [] })
const emit = defineEmits<{
  'update:modelValue': [models: string[]]
  'update:mappings': [mappings: ModelMappingEntry[]]
  'selection-saved': [account: Account]
}>()
const { t } = useI18n()
const selector = ref<InstanceType<typeof ModelWhitelistSelector>>()
const mappingDetails = ref<HTMLDetailsElement>()
function revealValidation(errors: ModelRestrictionError[]) {
  const error = errors[0]
  if (!error) return
  if (error.code === 'exactIdRequired') {
    selector.value?.revealSelection()
    return
  }
  if (mappingDetails.value) mappingDetails.value.open = true
  void nextTick(() => mappingDetails.value?.querySelectorAll('input')[(error.row - 1) * 2]?.focus())
}
defineExpose({ revealValidation })
function removeMapping(index: number) {
  if (props.mappings.length === 1 && !props.modelValue.length) selector.value?.requestClear()
  else emit('update:mappings', props.mappings.filter((_, position) => position !== index))
}
function updateMapping(index: number, field: keyof ModelMappingEntry, event: Event) {
  const value = (event.target as HTMLInputElement).value
  emit('update:mappings', props.mappings.map((mapping, position) => position === index ? { ...mapping, [field]: value } : mapping))
}
function addMapping(mapping: ModelMappingEntry = { from: '', to: '' }) {
  if (mapping.from && props.mappings.some(existing => existing.from === mapping.from)) return
  emit('update:mappings', [...props.mappings, { from: mapping.from, to: mapping.to }])
}
</script>

<template>
  <section class="min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5" data-testid="account-model-configuration">
    <h3 class="text-base font-semibold">{{ t('admin.accounts.editSections.models') }}</h3>
    <ModelWhitelistSelector ref="selector" v-bind="$attrs" :model-value="modelValue" :model-mappings="mappings" @update:model-value="emit('update:modelValue', $event)" @update:model-mappings="emit('update:mappings', $event)" @selection-saved="emit('selection-saved', $event)" @validation-failed="revealValidation" />
    <details ref="mappingDetails" class="space-y-3 border-t border-gray-200 pt-3 dark:border-dark-600" data-testid="model-mappings">
      <summary class="cursor-pointer rounded-lg py-2 font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">{{ t('admin.accounts.editSections.mappings', { count: mappings.length }) }}</summary>
      <p class="input-hint">{{ t('admin.accounts.mapRequestModels') }}</p>
      <div v-for="(mapping, index) in mappings" :key="index" class="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <input :value="mapping.from" class="input min-w-0" :aria-label="t('admin.accounts.requestModel')" :placeholder="t('admin.accounts.requestModel')" @input="updateMapping(index, 'from', $event)" />
        <input :value="mapping.to" class="input min-w-0" :aria-label="t('admin.accounts.actualModel')" :placeholder="t('admin.accounts.actualModel')" @input="updateMapping(index, 'to', $event)" />
        <button type="button" class="btn btn-secondary" @click="removeMapping(index)">{{ t('common.delete') }}</button>
      </div>
      <button type="button" class="btn btn-secondary" @click="addMapping()">{{ t('admin.accounts.addMapping') }}</button>
      <div v-if="presets.length" class="space-y-2">
        <p class="input-label">{{ t('admin.accounts.editSections.mappingTemplates') }}</p>
        <div class="flex flex-wrap gap-2">
        <button v-for="preset in presets" :key="preset.from" type="button" class="btn btn-secondary max-w-full break-all text-xs" @click="addMapping(preset)">{{ preset.label || preset.from }}</button>
        </div>
      </div>
    </details>
    <slot name="model-rules" />
  </section>
</template>
