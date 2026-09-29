<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import type { ModelMappingEntry } from '@/composables/useModelWhitelist'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
  modelValue: string[]
  mappings: ModelMappingEntry[]
  presets?: (ModelMappingEntry & { label?: string })[]
}>(), { presets: () => [] })
const emit = defineEmits<{
  'update:modelValue': [models: string[]]
  'update:mappings': [mappings: ModelMappingEntry[]]
}>()
const { t } = useI18n()
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
  <section class="min-w-0 space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="account-model-configuration">
    <h3 class="input-label">{{ t('admin.accounts.modelRestriction') }}</h3>
    <ModelWhitelistSelector v-bind="$attrs" :model-value="modelValue" :model-mappings="mappings" @update:model-value="emit('update:modelValue', $event)" />
    <div class="space-y-3">
      <h4 class="input-label">{{ t('admin.accounts.modelMapping') }}</h4>
      <p class="input-hint">{{ t('admin.accounts.mapRequestModels') }}</p>
      <div v-for="(mapping, index) in mappings" :key="index" class="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_auto]">
        <input :value="mapping.from" class="input min-w-0" :aria-label="t('admin.accounts.requestModel')" :placeholder="t('admin.accounts.requestModel')" @input="updateMapping(index, 'from', $event)" />
        <input :value="mapping.to" class="input min-w-0" :aria-label="t('admin.accounts.actualModel')" :placeholder="t('admin.accounts.actualModel')" @input="updateMapping(index, 'to', $event)" />
        <button type="button" class="btn btn-secondary" @click="emit('update:mappings', mappings.filter((_, position) => position !== index))">{{ t('common.delete') }}</button>
      </div>
      <button type="button" class="btn btn-secondary" @click="addMapping()">{{ t('admin.accounts.addMapping') }}</button>
      <div class="flex flex-wrap gap-2">
        <button v-for="preset in presets" :key="preset.from" type="button" class="btn btn-secondary max-w-full break-all text-xs" @click="addMapping(preset)">{{ preset.label || preset.from }}</button>
      </div>
    </div>
  </section>
</template>
