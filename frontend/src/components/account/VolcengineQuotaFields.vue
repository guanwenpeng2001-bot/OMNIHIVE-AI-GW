<template>
  <fieldset data-test="volcengine-quota-fields" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
    <legend class="px-1 text-sm font-medium">{{ t('admin.accounts.cnProviders.volcengineQuota.title') }}</legend>
    <p class="text-xs text-gray-500">{{ t('admin.accounts.cnProviders.volcengineQuota.hint') }}</p>
    <div class="grid gap-3 sm:grid-cols-2">
      <div v-for="field in secretFields" :key="field.key" class="space-y-1">
        <label class="block">
          <span class="input-label">{{ t(`admin.accounts.cnProviders.volcengineQuota.${field.key}`) }}</span>
          <input :data-test="field.key" type="password" autocomplete="new-password" class="input"
            :value="modelValue[field.key]" :disabled="modelValue[field.clear]"
            :placeholder="editing ? t('admin.accounts.cnProviders.volcengineQuota.keepSecret') : ''"
            @input="update({ [field.key]: ($event.target as HTMLInputElement).value })" />
        </label>
        <span v-if="field.saved" class="text-xs text-gray-500">{{ t('admin.accounts.cnProviders.volcengineQuota.saved') }}</span>
        <label v-if="editing" class="flex items-center gap-2 text-xs text-gray-500">
          <input :data-test="field.clear" type="checkbox" :checked="modelValue[field.clear]"
            @change="update({ [field.clear]: ($event.target as HTMLInputElement).checked })" />
          {{ t('admin.accounts.cnProviders.volcengineQuota.clearSecret') }}
        </label>
      </div>
      <label class="block">
        <span class="input-label">{{ t('admin.accounts.cnProviders.volcengineQuota.edition') }}</span>
        <select data-test="quota-edition" class="input" :value="modelValue.edition"
          @change="update({ edition: ($event.target as HTMLSelectElement).value as 'personal' | 'team' })">
          <option value="personal">{{ t('admin.accounts.cnProviders.volcengineQuota.personal') }}</option>
          <option value="team">{{ t('admin.accounts.cnProviders.volcengineQuota.team') }}</option>
        </select>
      </label>
      <label v-if="modelValue.edition === 'team'" class="block">
        <span class="input-label">{{ t('admin.accounts.cnProviders.volcengineQuota.seatId') }}</span>
        <input data-test="quota-seat" type="text" class="input" :value="modelValue.seatId"
          :placeholder="t('admin.accounts.cnProviders.volcengineQuota.seatHint')"
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
  editing?: boolean
  hasAccessKeyId?: boolean
  hasSecretAccessKey?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: VolcengineQuotaDraft] }>()
const { t } = useI18n()
const secretFields = computed(() => [
  { key: 'accessKeyId', clear: 'clearAccessKeyId', saved: props.hasAccessKeyId },
  { key: 'secretAccessKey', clear: 'clearSecretAccessKey', saved: props.hasSecretAccessKey }
] as const)
const update = (change: Partial<VolcengineQuotaDraft>) => emit('update:modelValue', { ...props.modelValue, ...change })
</script>
