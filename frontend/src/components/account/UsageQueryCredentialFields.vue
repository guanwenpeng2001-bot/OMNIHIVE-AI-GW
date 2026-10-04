<template>
  <fieldset data-test="usage-query-fields" class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-gray-700">
    <legend class="px-1 text-sm font-medium">{{ t('admin.accounts.usageQuery.title') }}</legend>
    <p class="text-xs text-gray-500">{{ t('admin.accounts.usageQuery.hint') }}</p>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.usageQuery.token') }}</span>
      <input data-test="usage-query-token" type="password" autocomplete="new-password" class="input"
        :value="modelValue.token" :disabled="modelValue.clearToken" pattern="[!-~]+"
        :placeholder="editing ? t('admin.accounts.usageQuery.keepSecret') : ''"
        @input="update({ token: ($event.target as HTMLInputElement).value })" />
    </label>
    <span v-if="hasToken" data-test="usage-query-saved" class="text-xs text-gray-500">{{ t('admin.accounts.usageQuery.saved') }}</span>
    <label v-if="editing" class="flex items-center gap-2 text-xs text-gray-500">
      <input data-test="usage-query-clear" type="checkbox" :checked="modelValue.clearToken"
        @change="update({ clearToken: ($event.target as HTMLInputElement).checked })" />
      {{ t('admin.accounts.usageQuery.clearSecret') }}
    </label>
    <label v-if="usageQueryNeedsUserId(platform)" class="block">
      <span class="input-label">{{ t('admin.accounts.usageQuery.userId') }}</span>
      <input data-test="usage-query-user-id" type="text" class="input" :value="modelValue.userId" pattern="[!-~]+"
        @input="update({ userId: ($event.target as HTMLInputElement).value })" />
    </label>
  </fieldset>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { usageQueryNeedsUserId, type UsageQueryDraft } from './credentialsBuilder'
const props = defineProps<{ modelValue: UsageQueryDraft; platform: string; editing?: boolean; hasToken?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: UsageQueryDraft] }>()
const { t } = useI18n()
const update = (patch: Partial<UsageQueryDraft>) => emit('update:modelValue', { ...props.modelValue, ...patch })
</script>
