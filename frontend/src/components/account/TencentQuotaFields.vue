<template>
  <fieldset data-test="tencent-quota-fields" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
    <legend class="px-1 text-sm font-medium">{{ t('admin.accounts.tencentQuota.title') }}</legend>
    <p class="text-xs text-gray-500">{{ t('admin.accounts.tencentQuota.hint') }}</p>
    <div v-for="field in secrets" :key="field.key">
      <label class="block">
        <span class="input-label">{{ t(`admin.accounts.tencentQuota.${field.key}`) }}</span>
        <input :data-test="field.key" type="password" autocomplete="new-password" class="input"
          :value="modelValue[field.key]" :disabled="modelValue[field.clear]"
          :placeholder="editing ? t('admin.accounts.usageQuery.keepSecret') : ''"
          @input="update({ [field.key]: ($event.target as HTMLInputElement).value })" />
      </label>
      <span v-if="field.saved" class="text-xs text-gray-500">{{ t('admin.accounts.usageQuery.saved') }}</span>
      <label v-if="editing" class="flex items-center gap-2 text-xs">
        <input :data-test="field.clear" type="checkbox" :checked="modelValue[field.clear]"
          @change="update({ [field.clear]: ($event.target as HTMLInputElement).checked })" />
        {{ t('admin.accounts.usageQuery.clearSecret') }}
      </label>
    </div>
    <label v-for="field in metadata" :key="field" class="block">
      <span class="input-label">{{ t(`admin.accounts.tencentQuota.${field}`) }}</span>
      <input :data-test="field" type="text" class="input" :value="modelValue[field]"
        @input="update({ [field]: ($event.target as HTMLInputElement).value })" />
    </label>
  </fieldset>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TencentQuotaDraft } from './credentialsBuilder'
const props = defineProps<{ modelValue: TencentQuotaDraft; editing?: boolean; hasSecretId?: boolean; hasSecretKey?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: TencentQuotaDraft] }>()
const { t } = useI18n()
const metadata = ['region', 'teamId'] as const
const secrets = computed(() => [
  { key: 'secretId', clear: 'clearSecretId', saved: props.hasSecretId },
  { key: 'secretKey', clear: 'clearSecretKey', saved: props.hasSecretKey }
] as const)
const update = (patch: Partial<TencentQuotaDraft>) => emit('update:modelValue', { ...props.modelValue, ...patch })
</script>
