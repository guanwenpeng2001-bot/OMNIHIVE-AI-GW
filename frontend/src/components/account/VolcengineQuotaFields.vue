<template>
  <fieldset :data-test="`${provider || 'volcengine'}-quota-fields`" class="min-w-0 space-y-3 rounded-lg border border-[var(--border)] bg-[var(--card)] p-3 [&_input]:font-mono">
    <legend class="px-1 text-sm font-medium">{{ t(`${prefix}.title`) }}</legend>
    <p class="text-xs text-gray-500">{{ t(`${prefix}.hint`) }}</p>
    <div class="grid gap-3 sm:grid-cols-2">
      <div v-for="field in secretFields" :key="field.key" class="space-y-1">
        <label class="block">
          <span class="input-label">{{ t(`${prefix}.${field.key}`) }}</span>
          <input :data-test="field.key" type="password" autocomplete="new-password" class="input"
            :value="modelValue[field.key]" :disabled="modelValue[field.clear]"
            :placeholder="editing ? t(`${prefix}.keepSecret`) : ''"
            @input="update({ [field.key]: ($event.target as HTMLInputElement).value })" />
        </label>
        <span v-if="field.saved" class="text-xs text-gray-500">{{ t(`${prefix}.saved`) }}</span>
        <label v-if="editing" class="flex items-center gap-2 text-xs text-gray-500">
          <input :data-test="field.clear" type="checkbox" :checked="modelValue[field.clear]"
            @change="update({ [field.clear]: ($event.target as HTMLInputElement).checked })" />
          {{ t(`${prefix}.clearSecret`) }}
        </label>
      </div>
      <label v-if="provider !== 'byteplus'" class="block">
        <span class="input-label">{{ t(`${prefix}.edition`) }}</span>
        <select data-test="quota-edition" class="input" :value="modelValue.edition"
          @change="update({ edition: ($event.target as HTMLSelectElement).value as 'personal' | 'team' })">
          <option value="personal">{{ t(`${prefix}.personal`) }}</option>
          <option value="team">{{ t(`${prefix}.team`) }}</option>
        </select>
      </label>
      <label v-if="modelValue.edition === 'team'" class="block">
        <span class="input-label">{{ t(`${prefix}.seatId`) }}</span>
        <input data-test="quota-seat" type="text" class="input" :value="modelValue.seatId"
          :placeholder="t(`${prefix}.seatHint`)"
          @input="update({ seatId: ($event.target as HTMLInputElement).value })" />
      </label>
    </div>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { VolcengineQuotaDraft } from './credentialsBuilder'

const props = defineProps<{
  modelValue: VolcengineQuotaDraft
  provider?: 'volcengine' | 'byteplus'
  editing?: boolean
  hasAccessKeyId?: boolean
  hasSecretAccessKey?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: VolcengineQuotaDraft] }>()
const { t } = useI18n()
const prefix = computed(() => `admin.accounts.cnProviders.${props.provider || 'volcengine'}Quota`)
const secretFields = computed(() => [
  { key: 'accessKeyId', clear: 'clearAccessKeyId', saved: props.hasAccessKeyId },
  { key: 'secretAccessKey', clear: 'clearSecretAccessKey', saved: props.hasSecretAccessKey }
] as const)
const update = (change: Partial<VolcengineQuotaDraft>) => emit('update:modelValue', { ...props.modelValue, ...change })
</script>
